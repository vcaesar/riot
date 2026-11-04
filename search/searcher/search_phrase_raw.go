//  Copyright (c) 2026 The AtomAI.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 		http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package searcher

import (
	"math"

	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/riot/numeric"
	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/similarity"
)

// rawPhrase serves an exact phrase of distinct single terms straight from
// the term postings: each child's postings are read in batches together
// with their locations, aligned by document number and their positions
// checked in place, so a DocumentMatch is only built for documents that
// contain the phrase (and, under NextAbove, beat the floor).
type rawPhrase struct {
	indexReader search.Reader
	scorer      search.CompositeScorer
	children    []*TermSearcher // conjunction order, which scoring follows
	cursors     []phraseCursor  // children[i] reads through cursors[i]
	term        []*phraseCursor // per phrase term, its child's cursor
	terms       []string        // phrase terms in phrase order
	lo, hi, ptr []int           // per phrase term, location span and merge pointer
	scores      []search.DocumentMatch
	scorePtr    []*search.DocumentMatch
	out         []search.FieldTermLocation
	score       float64 // composite of scores
	nextDoc     uint64  // the next call starts at or after this document
	done        bool
	err         error // of loading offsets in appendLoc
}

// locBatcher is implemented by postings iterators that read many postings
// with their locations per call (index snapshot iterators).
type locBatcher interface {
	NextLocBatch(docs []uint64, freqs []int, norms []float64, ends []int,
		fields []string, locs []int) (int, []string, []int, error)
}

// posBatcher is implemented by postings iterators that read locations
// without their start and end, which LoadOffsets loads on demand, for a
// posting of the last batch with an offsets handle other than noOffsets
// (index snapshot iterators).
type posBatcher interface {
	NextPosBatch(docs []uint64, freqs []int, norms []float64, ends []int, offs []uint64,
		fields []string, locs []int) (int, []string, []int, error)
	LoadOffsets(h uint64, locs []int) error
}

// noOffsets is the offsets handle of a posting whose locations carry their
// start and end.
const noOffsets = math.MaxUint64

// phraseCursor holds a child's current batch of postings: posting k has
// the locations [ends[k], ends[k+1]), ends[0] being 0, each with one
// entry in fields and three (pos, start, end) in locs. Only pos is set
// until offs[k] is noOffsets: appendLoc loads start and end of the hits.
type phraseCursor struct {
	reader segment.PostingsIterator
	pos    posBatcher // nil: read with batch
	batch  locBatcher // nil: read one posting at a time
	offs   []uint64
	docs   []uint64
	freqs  []int
	norms  []float64
	ends   []int
	fields []string
	locs   []int
	n, k   int    // postings in the batch, current posting
	last   uint64 // last document read from reader
	done   bool   // reader exhausted

	// freq and norm of the child's last computed score
	scoredFreq int
	scoredNorm float64
	scored     bool
}

// phraseAdvanceGap is the document gap past the last posting read beyond
// which a cursor seeks with Advance instead of reading the postings between.
const phraseAdvanceGap = 2 * postingsBatchSize

// newRawPhrase returns nil unless must is an untouched sum conjunction of
// one term searcher per phrase term and no explanation is wanted.
func newRawPhrase(must search.Searcher, terms [][]string, options search.SearcherOptions) *rawPhrase {
	conj, ok := must.(*ConjunctionSearcher)
	if !ok || options.Explain || conj.initialized || len(terms) == 0 || len(conj.searchers) != len(terms) {
		return nil
	}
	if _, ok := conj.scorer.(*similarity.CompositeSumScorer); !ok {
		return nil
	}
	n := len(terms)
	r := &rawPhrase{
		scorer:   conj.scorer,
		children: make([]*TermSearcher, n),
		cursors:  make([]phraseCursor, n),
		term:     make([]*phraseCursor, n),
		terms:    make([]string, n),
		lo:       make([]int, n),
		hi:       make([]int, n),
		ptr:      make([]int, n),
		scores:   make([]search.DocumentMatch, n),
		scorePtr: make([]*search.DocumentMatch, n),
	}
	for t, termPos := range terms {
		r.terms[t] = termPos[0]
	}
	for i, child := range conj.searchers {
		ts, ok := child.(*TermSearcher)
		if !ok || ts.options.Explain {
			return nil
		}
		pos := -1
		for t, term := range r.terms {
			if term == ts.queryTerm {
				pos = t
			}
		}
		if pos < 0 {
			return nil
		}
		r.indexReader = ts.indexReader
		r.children[i], r.scorePtr[i] = ts, &r.scores[i]
		r.term[pos] = &r.cursors[i]
	}
	for i, ts := range r.children {
		r.cursors[i].init(ts.reader)
	}
	return r
}

func (c *phraseCursor) init(reader segment.PostingsIterator) {
	c.reader = reader
	size := 1
	switch r := reader.(type) {
	case posBatcher:
		c.pos, size = r, postingsBatchSize
	case locBatcher:
		c.batch, size = r, postingsBatchSize
	}
	c.offs = make([]uint64, size)
	c.docs = make([]uint64, size)
	c.freqs = make([]int, size)
	c.norms = make([]float64, size)
	c.ends = make([]int, size+1)
}

// seek moves c to its first posting at or after target; false: none.
func (c *phraseCursor) seek(target uint64) (bool, error) {
	for {
		for c.k < c.n && c.docs[c.k] < target {
			c.k++
		}
		if c.k < c.n {
			return true, nil
		}
		if c.done {
			return false, nil
		}
		var err error
		if target > c.last+phraseAdvanceGap {
			err = c.advance(target)
		} else {
			err = c.fill()
		}
		if err != nil {
			return false, err
		}
	}
}

func (c *phraseCursor) reset() {
	c.n, c.k, c.fields, c.locs = 0, 0, c.fields[:0], c.locs[:0]
}

// fill replaces the batch with the next postings of the reader.
func (c *phraseCursor) fill() error {
	c.reset()
	var n int
	var fields []string
	var locs []int
	var err error
	switch {
	case c.pos != nil:
		n, fields, locs, err = c.pos.NextPosBatch(c.docs, c.freqs, c.norms, c.ends[1:], c.offs, c.fields, c.locs)
	case c.batch != nil:
		n, fields, locs, err = c.batch.NextLocBatch(c.docs, c.freqs, c.norms, c.ends[1:], c.fields, c.locs)
		for k := range n {
			c.offs[k] = noOffsets
		}
	default:
		p, err := c.reader.Next()
		if err != nil {
			return err
		}
		c.add(p)
		return nil
	}
	c.fields, c.locs = fields, locs
	if err != nil {
		return err
	}
	c.n, c.done = n, n == 0
	if n > 0 {
		c.last = c.docs[n-1]
	}
	return nil
}

// advance replaces the batch with the reader's first posting at or after target.
func (c *phraseCursor) advance(target uint64) error {
	c.reset()
	p, err := c.reader.Advance(target)
	if err != nil {
		return err
	}
	c.add(p)
	return nil
}

// add makes p, nil at the end of the reader, the only posting of the batch.
func (c *phraseCursor) add(p segment.Posting) {
	if p == nil {
		c.done = true
		return
	}
	c.docs[0], c.freqs[0], c.norms[0], c.offs[0] = p.Number(), p.Frequency(), p.Norm(), noOffsets
	for _, l := range p.Locations() {
		c.fields = append(c.fields, l.Field())
		c.locs = append(c.locs, l.Pos(), l.Start(), l.End())
	}
	c.ends[1] = len(c.fields)
	c.n, c.last = 1, c.docs[0]
}

// span returns the location range of the current posting.
func (c *phraseCursor) span() (lo, hi int) {
	return c.ends[c.k], c.ends[c.k+1]
}

// maxSkippedRun caps the skipped hits reported at once, so that the
// collector still checks for cancellation regularly.
const maxSkippedRun = 1024

// next returns the next phrase hit at or after target scoring above floor,
// handing matching hits at or below it to skipped (nil: floor admits all)
// in runs of consecutive hits that share a score.
//
//nolint:gocyclo // hot loop, kept inline for speed
func (r *rawPhrase) next(ctx *search.Context, target uint64, floor float64,
	skipped func(float64, int) bool) (*search.DocumentMatch, error) {
	floorBits := numeric.Float64ToInt64(floor)
	var run int // skipped hits scoring runScore not yet reported
	var runScore float64
	doc := max(target, r.nextDoc)
	for !r.done {
		if skipped != nil {
			var stop bool
			if doc, run, runScore, stop = r.skipBelowFloor(doc, floorBits, run, runScore, skipped); stop {
				return nil, nil
			}
		}
		// leapfrog the cursors onto one document
		for i := 0; i < len(r.cursors); {
			c := &r.cursors[i]
			if c.k >= c.n || c.docs[c.k] < doc {
				// the common case, a cursor already on doc, needs no call
				ok, err := c.seek(doc)
				if err != nil {
					return nil, err
				}
				if !ok {
					r.done = true
					break
				}
			}
			if d := c.docs[c.k]; d > doc {
				doc, i = d, 0
				continue
			}
			i++
		}
		if r.done {
			break
		}
		r.nextDoc = doc + 1
		// scores are functions of freq and norm: recompute only on change
		changed := false
		for i, ts := range r.children {
			c := &r.cursors[i]
			if f, nm := c.freqs[c.k], c.norms[c.k]; !c.scored || f != c.scoredFreq || nm != c.scoredNorm {
				c.scoredFreq, c.scoredNorm, c.scored = f, nm, true
				r.scores[i].Score = ts.scorer.Score(f, nm)
				changed = true
			}
		}
		if changed {
			r.score = r.scorer.ScoreComposite(r.scorePtr)
		}
		score := r.score
		// a hit at or below floor is only checked for a phrase occurrence,
		// without collecting locations
		if skipped == nil || numeric.Float64ToInt64(score) > floorBits {
			var found bool
			if r.out, found = r.match(r.out[:0], true); r.err != nil {
				err := r.err
				r.err = nil
				return nil, err
			} else if found {
				if run > 0 && !skipped(runScore, run) {
					return nil, nil
				}
				rv := ctx.DocumentMatchPool.Get()
				rv.SetReader(r.indexReader)
				rv.Number = doc
				rv.Score = score
				rv.FieldTermLocations = append(rv.FieldTermLocations[:0], r.out...)
				return rv, nil
			}
		} else if _, found := r.match(nil, false); found {
			if run > 0 && (score != runScore || run == maxSkippedRun) {
				if !skipped(runScore, run) {
					return nil, nil
				}
				run = 0
			}
			run, runScore = run+1, score
		}
		doc++
	}
	if run > 0 {
		skipped(runScore, run)
	}
	return nil, nil
}

// skipBelowFloor is next's loop for the common case under a TopN floor:
// it aligns the cursors within their current batches and counts the
// documents whose terms each occur once, in phrase order, with the
// freq/norm of the last computed score, which is at or below the floor.
// It stops, leaving doc for next's general loop, at the first document
// that needs more (a batch refill, another score, several occurrences);
// stop reports that skipped asked to end the search.
//
//nolint:gocyclo // hot loop, kept inline for speed
func (r *rawPhrase) skipBelowFloor(doc uint64, floorBits int64, run int, runScore float64,
	skipped func(float64, int) bool) (_ uint64, _ int, _ float64, stop bool) {
	cs := r.cursors
	for i := range cs {
		if !cs[i].scored {
			return doc, run, runScore, false
		}
	}
	score := r.score
	if numeric.Float64ToInt64(score) > floorBits || (run > 0 && runScore != score) {
		return doc, run, runScore, false
	}
	for {
		for i := 0; i < len(cs); {
			c := &cs[i]
			docs, k := c.docs[:c.n], c.k
			for k < len(docs) && docs[k] < doc {
				k++
			}
			c.k = k
			if k >= len(docs) {
				return doc, run, runScore, false
			}
			if d := docs[k]; d > doc {
				doc, i = d, 0
				continue
			}
			i++
		}
		// one pass over the terms checks freq/norm, a single location
		// and its place in the phrase
		c0 := r.term[0]
		lo0 := c0.ends[c0.k]
		if c0.ends[c0.k+1]-lo0 != 1 || c0.freqs[c0.k] != c0.scoredFreq || c0.norms[c0.k] != c0.scoredNorm {
			return doc, run, runScore, false
		}
		pos0, field0 := c0.locs[3*lo0], c0.fields[lo0]
		match := true
		for t := 1; t < len(r.term); t++ {
			c := r.term[t]
			k := c.k
			lo := c.ends[k]
			if c.ends[k+1]-lo != 1 || c.freqs[k] != c.scoredFreq || c.norms[k] != c.scoredNorm {
				return doc, run, runScore, false
			}
			match = match && c.locs[3*lo] == pos0+t && c.fields[lo] == field0
		}
		if match {
			run, runScore = run+1, score
			if run == maxSkippedRun {
				if !skipped(score, run) {
					return doc, 0, runScore, true
				}
				run = 0
			}
		}
		doc++
		r.nextDoc = doc
	}
}

// match appends the locations of every phrase occurrence in the aligned
// document to out: term t at position p+t for an occurrence p of term 0,
// in the same field. With all false it stops at the first occurrence and
// appends nothing; found reports whether there is one.
func (r *rawPhrase) match(out []search.FieldTermLocation, all bool) (_ []search.FieldTermLocation, found bool) {
	single := true
	for t, c := range r.term {
		lo, hi := c.span()
		if lo == hi {
			return out, false
		}
		r.lo[t], r.hi[t] = lo, hi
		single = single && hi-lo == 1
	}
	if single {
		// one occurrence per term: the phrase is there or not, no merge
		c0, j0 := r.term[0], r.lo[0]
		for t := 1; t < len(r.term); t++ {
			if c, j := r.term[t], r.lo[t]; c.locs[3*j] != c0.locs[3*j0]+t || c.fields[j] != c0.fields[j0] {
				return out, false
			}
		}
		if all {
			for t, j := range r.lo {
				out = r.appendLoc(out, t, j)
			}
		}
		return out, true
	}
	if r.sortedOneField() {
		return r.mergeSorted(out, all)
	}
	return r.matchAny(out, all)
}

// sortedOneField reports whether all locations share one field and each
// term's positions strictly ascend, so a forward merge finds every match.
func (r *rawPhrase) sortedOneField() bool {
	field := r.term[0].fields[r.lo[0]]
	for t, c := range r.term {
		prev := math.MinInt
		for j := r.lo[t]; j < r.hi[t]; j++ {
			if c.fields[j] != field || c.locs[3*j] <= prev {
				return false
			}
			prev = c.locs[3*j]
		}
	}
	return true
}

func (r *rawPhrase) mergeSorted(out []search.FieldTermLocation, all bool) (_ []search.FieldTermLocation, found bool) {
	copy(r.ptr, r.lo)
	c0 := r.term[0]
	for j0 := r.lo[0]; j0 < r.hi[0]; j0++ {
		p := c0.locs[3*j0]
		for t := 1; t < len(r.term); t++ {
			c, want := r.term[t], p+t
			for r.ptr[t] < r.hi[t] && c.locs[3*r.ptr[t]] < want {
				r.ptr[t]++
			}
			if r.ptr[t] >= r.hi[t] {
				return out, found // term t is exhausted, no later start can match
			}
			if c.locs[3*r.ptr[t]] != want {
				goto nextStart
			}
		}
		if found = true; !all {
			return out, true
		}
		out = r.appendLoc(out, 0, j0)
		for t := 1; t < len(r.term); t++ {
			out = r.appendLoc(out, t, r.ptr[t])
		}
	nextStart:
	}
	return out, found
}

// matchAny is the quadratic match for multi-field or unsorted locations.
func (r *rawPhrase) matchAny(out []search.FieldTermLocation, all bool) (_ []search.FieldTermLocation, found bool) {
	c0 := r.term[0]
	for j0 := r.lo[0]; j0 < r.hi[0]; j0++ {
		for t := 1; t < len(r.term); t++ {
			c := r.term[t]
			r.ptr[t] = -1
			for j := r.lo[t]; j < r.hi[t]; j++ {
				if c.fields[j] == c0.fields[j0] && c.locs[3*j] == c0.locs[3*j0]+t {
					r.ptr[t] = j
					break
				}
			}
			if r.ptr[t] < 0 {
				goto nextStart
			}
		}
		if found = true; !all {
			return out, true
		}
		out = r.appendLoc(out, 0, j0)
		for t := 1; t < len(r.term); t++ {
			out = r.appendLoc(out, t, r.ptr[t])
		}
	nextStart:
	}
	return out, found
}

// appendLoc appends location j of phrase term t, loading the start and
// end of the locations of its posting first if need be.
func (r *rawPhrase) appendLoc(out []search.FieldTermLocation, t, j int) []search.FieldTermLocation {
	c := r.term[t]
	if c.offs[c.k] != noOffsets {
		lo, hi := c.span()
		if err := c.pos.LoadOffsets(c.offs[c.k], c.locs[3*lo:3*hi]); err != nil && r.err == nil {
			r.err = err
		}
		c.offs[c.k] = noOffsets
	}
	return append(out, search.FieldTermLocation{
		Field:    c.fields[j],
		Term:     r.terms[t],
		Location: search.Location{Pos: c.locs[3*j], Start: c.locs[3*j+1], End: c.locs[3*j+2]},
	})
}

// NextAbove implements search.ScoreSkipper.
func (s *PhraseSearcher) NextAbove(ctx *search.Context, floor float64,
	skipped func(score float64, n int) bool) (*search.DocumentMatch, error) {
	if s.raw == nil {
		return s.Next(ctx)
	}
	return s.raw.next(ctx, 0, floor, skipped)
}

func (s *PhraseSearcher) nextRaw(ctx *search.Context, target uint64) (*search.DocumentMatch, error) {
	return s.raw.next(ctx, target, math.Inf(-1), nil)
}
