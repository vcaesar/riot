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
	"encoding/binary"
	"fmt"
	"math"
	"unsafe"

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
	cur         []int           // per phrase term, matchOwn's position before ptr
	mask        []uint64        // per phrase term, matchOwnMask's ownMaskWords words
	used        []int           // per phrase term, the words of mask that may be set
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

// ownPosBatcher is implemented by postings iterators that read postings
// whose locations are all of the iterator's field, at positions that do
// not decrease, without decoding them: recs[starts[k]:ends[k]] holds the
// positions of posting k as complete uvarint deltas, until the next read.
// own false, with no posting read, leaves the next postings to
// NextPosBatch (index snapshot iterators).
type ownPosBatcher interface {
	NextOwnPosBatch(docs []uint64, freqs []int, norms []float64, starts, ends []int,
		offs []uint64) (int, []byte, bool, error)
}

// noOffsets is the offsets handle of a posting whose locations carry their
// start and end.
const noOffsets = math.MaxUint64

// phraseCursor holds a child's current batch of postings: posting k has
// the locations [ends[k], ends[k+1]), ends[0] being 0, each with one
// entry in fields and three (pos, start, end) in locs. Only pos is set
// until offs[k] is noOffsets: appendLoc loads start and end of the hits.
// A batch read with ownPos (isOwn) instead has the positions of posting k,
// all in field, encoded in recs[starts[k]:ends[k+1]]: decode appends those
// of a hit to fields and locs, and expand turns the batch into a decoded one.
type phraseCursor struct {
	reader segment.PostingsIterator
	ownPos ownPosBatcher // nil: read with pos
	pos    posBatcher    // nil: read with batch
	batch  locBatcher    // nil: read one posting at a time
	field  string
	recs   []byte
	starts []int
	isOwn  bool
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
func newRawPhrase(must search.Searcher, terms [][]string, field string, options search.SearcherOptions) *rawPhrase {
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
		cur:      make([]int, n),
		mask:     make([]uint64, n*ownMaskWords),
		used:     make([]int, n),
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
		r.cursors[i].init(ts.reader, field)
	}
	return r
}

func (c *phraseCursor) init(reader segment.PostingsIterator, field string) {
	c.reader, c.field = reader, field
	size := 1
	switch r := reader.(type) {
	case posBatcher:
		c.pos, size = r, postingsBatchSize
		c.ownPos, _ = reader.(ownPosBatcher)
	case locBatcher:
		c.batch, size = r, postingsBatchSize
	}
	c.offs = make([]uint64, size)
	c.docs = make([]uint64, size)
	c.freqs = make([]int, size)
	c.norms = make([]float64, size)
	c.ends = make([]int, size+1)
	if c.ownPos != nil {
		c.starts = make([]int, size)
	}
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
	c.n, c.k, c.fields, c.locs, c.recs, c.isOwn = 0, 0, c.fields[:0], c.locs[:0], nil, false
}

// decode appends the locations of posting k of a batch of encoded
// positions to fields and locs, and returns their span.
func (c *phraseCursor) decode(k int) (lo, hi int, err error) {
	lo = len(c.fields)
	var pos uint64
	for rec := c.recs[c.starts[k]:c.ends[k+1]]; len(rec) > 0; {
		delta, w := binary.Uvarint(rec)
		if pos += delta; w <= 0 || pos > math.MaxInt {
			return lo, lo, fmt.Errorf("phrase position out of range")
		}
		rec = rec[w:]
		c.fields = append(c.fields, c.field)
		c.locs = append(c.locs, int(pos), 0, 0)
	}
	return lo, len(c.fields), nil
}

// expand turns a batch of encoded positions into a decoded one, for a
// document whose other cursors hold decoded batches.
func (c *phraseCursor) expand() error {
	c.fields, c.locs = c.fields[:0], c.locs[:0] // drop the hits decode added
	for k := range c.n {
		_, hi, err := c.decode(k)
		if err != nil {
			return err
		}
		c.ends[k+1] = hi
	}
	c.isOwn = false
	return nil
}

// ownDelta decodes the position delta at recs[j:], which is complete, and
// returns it with the offset past it.
func ownDelta(recs []byte, j int) (delta, next int) {
	if b := recs[j]; b < 0x80 {
		return int(b), j + 1
	}
	v, w := binary.Uvarint(recs[j:])
	return int(v), j + w //nolint:gosec // G115: a delta takes at most 9 bytes, 63 bits.
}

// fill replaces the batch with the next postings of the reader.
func (c *phraseCursor) fill() error {
	c.reset()
	var n int
	var fields []string
	var locs []int
	var err error
	if c.ownPos != nil {
		n, c.recs, c.isOwn, err = c.ownPos.NextOwnPosBatch(c.docs, c.freqs, c.norms, c.starts, c.ends[1:], c.offs)
		if err != nil {
			return err
		}
	}
	switch {
	case c.isOwn:
		fields, locs = c.fields, c.locs
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
		} else if _, found := r.match(nil, false); r.err != nil {
			err := r.err
			r.err = nil
			return nil, err
		} else if found {
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
	// the batches stay as they are until a refill; next expands mixed ones
	own, mixed := r.ownBatches()
	if mixed {
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
		// and, in a batch of encoded positions, a one-byte record
		c0 := r.term[0]
		lo0 := c0.ends[c0.k]
		if own {
			lo0 = c0.starts[c0.k]
		}
		if c0.ends[c0.k+1]-lo0 != 1 || c0.freqs[c0.k] != c0.scoredFreq || c0.norms[c0.k] != c0.scoredNorm {
			return doc, run, runScore, false
		}
		var pos0 int
		var field0 string
		if own {
			pos0 = int(c0.recs[lo0])
		} else {
			pos0, field0 = c0.locs[3*lo0], c0.fields[lo0]
		}
		match := true
		for t := 1; t < len(r.term); t++ {
			c := r.term[t]
			k := c.k
			lo := c.ends[k]
			if own {
				lo = c.starts[k]
			}
			if c.ends[k+1]-lo != 1 || c.freqs[k] != c.scoredFreq || c.norms[k] != c.scoredNorm {
				return doc, run, runScore, false
			}
			if own {
				match = match && int(c.recs[lo]) == pos0+t
			} else {
				match = match && c.locs[3*lo] == pos0+t && c.fields[lo] == field0
			}
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
	own, mixed := r.ownBatches()
	if own {
		found, ok := r.matchOwnMask()
		if !ok {
			found = r.matchOwn()
		}
		if !found || !all {
			return out, found
		}
	}
	single := true
	for t, c := range r.term {
		if mixed && c.isOwn {
			if r.err = c.expand(); r.err != nil {
				return out, false
			}
		}
		lo, hi := c.span()
		if own {
			// a hit: only its own locations are decoded, the batch stays
			if lo, hi, r.err = c.decode(c.k); r.err != nil {
				return out, false
			}
		}
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
	n := len(out)
	if out, found, sorted := r.mergeSorted(out, all); sorted {
		return out, found
	}
	return r.matchAny(out[:n], all)
}

// ownBatches reports whether the cursors all hold batches of encoded
// positions, or, mixed, only some do.
func (r *rawPhrase) ownBatches() (own, mixed bool) {
	n := 0
	for _, c := range r.term {
		if c.isOwn {
			n++
		}
	}
	return n == len(r.term), n > 0 && n < len(r.term)
}

// ownMaskWords bounds the positions matchOwnMask handles, 64 per word; a
// power of two.
const ownMaskWords = 16

// matchOwnMask is matchOwn without a branch per position, for records of
// one-byte deltas at positions below 64*ownMaskWords: it sets bit p-t for
// each position p of term t, so a phrase occurrence is a bit that every
// term has set. ok is false when a record is not such, and matchOwn must
// decide.
func (r *rawPhrase) matchOwnMask() (found, ok bool) {
	words := ownMaskWords
	for t, c := range r.term {
		m := r.mask[t*ownMaskWords:][:ownMaskWords]
		clear(m[:r.used[t]])
		p, high := -t, byte(0)
		for _, b := range c.recs[c.starts[c.k]:c.ends[c.k+1]] {
			p += int(b)
			high |= b
			if p >= 0 {
				m[(p>>6)&(ownMaskWords-1)] |= 1 << (p & 63)
			}
		}
		// positions do not decrease, so the last one is in the last word
		n := p>>6 + 1
		if high >= 0x80 || n > ownMaskWords {
			r.used[t] = ownMaskWords // p was not a position: any word may be set
			return false, false
		}
		r.used[t] = max(n, 0)
		words = min(words, r.used[t])
	}
	for w := range words {
		acc := r.mask[w]
		for t := 1; t < len(r.term); t++ {
			acc &= r.mask[t*ownMaskWords+w]
		}
		if acc != 0 {
			return true, true
		}
	}
	return false, true
}

// matchOwn reports whether the aligned document, whose cursors all hold
// encoded positions, has a phrase occurrence. The positions are of one
// field and do not decrease per term, so a forward merge that decodes
// them as it goes finds it.
func (r *rawPhrase) matchOwn() bool {
	for t := 1; t < len(r.term); t++ {
		c := r.term[t]
		// no position is decoded yet: cur is below any position wanted
		r.ptr[t], r.hi[t], r.cur[t] = c.starts[c.k], c.ends[c.k+1], 0
	}
	c0 := r.term[0]
	recs0, j0, hi0 := c0.recs, c0.starts[c0.k], c0.ends[c0.k+1]
	p := 0
starts:
	for j0 < hi0 {
		var d int
		d, j0 = ownDelta(recs0, j0)
		p += d
		for t := 1; t < len(r.term); t++ {
			recs, ptr, hi, cur, want := r.term[t].recs, r.ptr[t], r.hi[t], r.cur[t], p+t
			for cur < want {
				if ptr == hi {
					return false // term t is exhausted, no later start can match
				}
				d, ptr = ownDelta(recs, ptr)
				cur += d
			}
			r.ptr[t], r.cur[t] = ptr, cur
			if cur != want {
				continue starts
			}
		}
		return true
	}
	return false
}

// sameField is f == g without a call when both share their bytes, as the
// locations of one postings list do.
func sameField(f, g string) bool {
	return len(f) == len(g) && (unsafe.StringData(f) == unsafe.StringData(g) || f == g)
}

// mergeSorted is the forward merge of locations that share one field and
// whose positions strictly ascend per term, which it checks as it reads
// them: sorted is false, and out undefined past its input length, when
// they do not, and matchAny must decide. A match it reports is a match
// either way, so it stops at the first one when all is false.
func (r *rawPhrase) mergeSorted(out []search.FieldTermLocation, all bool) (
	_ []search.FieldTermLocation, found, sorted bool) {
	c0 := r.term[0]
	field := c0.fields[r.lo[0]]
	for t, c := range r.term {
		if !sameField(c.fields[r.lo[t]], field) {
			return out, false, false
		}
		r.ptr[t] = r.lo[t] // every location up to ptr[t] is checked
	}
	lo0, hi0, locs0, fields0 := r.lo[0], r.hi[0], c0.locs, c0.fields
	j0 := lo0
starts:
	for ; j0 < hi0; j0++ {
		p := locs0[3*j0]
		if j0 > lo0 && (p <= locs0[3*(j0-1)] || !sameField(fields0[j0], field)) {
			return out, false, false
		}
		for t := 1; t < len(r.term); t++ {
			c, want := r.term[t], p+t
			ptr, hi, locs := r.ptr[t], r.hi[t], c.locs
			v := locs[3*ptr]
			for v < want {
				ptr++
				if ptr >= hi {
					r.ptr[t] = ptr
					break starts // term t is exhausted, no later start can match
				}
				next := locs[3*ptr]
				if next <= v || !sameField(c.fields[ptr], field) {
					return out, false, false
				}
				v = next
			}
			r.ptr[t] = ptr
			if v != want {
				continue starts
			}
		}
		if found = true; !all {
			return out, true, true
		}
		out = r.appendLoc(out, 0, j0)
		for t := 1; t < len(r.term); t++ {
			out = r.appendLoc(out, t, r.ptr[t])
		}
	}
	// what the merge concluded holds only if the locations it did not
	// reach are sorted too
	if !r.sortedFrom(0, j0+1, field) {
		return out, false, false
	}
	for t := 1; t < len(r.term); t++ {
		if !r.sortedFrom(t, r.ptr[t]+1, field) {
			return out, false, false
		}
	}
	return out, found, true
}

// sortedFrom reports whether the locations of term t from j on are in
// field and strictly ascend from the one before j.
func (r *rawPhrase) sortedFrom(t, j int, field string) bool {
	c, hi := r.term[t], r.hi[t]
	for ; j < hi; j++ {
		if c.locs[3*j] <= c.locs[3*(j-1)] || !sameField(c.fields[j], field) {
			return false
		}
	}
	return true
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
		lo, hi := r.lo[t], r.hi[t]
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
