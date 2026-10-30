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
// the term postings: children are aligned by document number and their
// segment locations checked in place, so a DocumentMatch is only built for
// documents that contain the phrase (and, under NextAbove, beat the floor).
type rawPhrase struct {
	indexReader search.Reader
	scorer      search.CompositeScorer
	children    []*TermSearcher // conjunction order, which scoring follows
	phrasePos   []int           // children[i] is phrase term phrasePos[i]
	terms       []string        // phrase terms in phrase order
	postings    []segment.Posting
	locs        [][]segment.Location // per phrase term, current document
	ptr         []int
	scores      []search.DocumentMatch
	scorePtr    []*search.DocumentMatch
	out         []search.FieldTermLocation
	started     bool
	done        bool
}

// newRawPhrase returns nil unless must is an untouched sum conjunction of
// one term searcher per phrase term and no explanation is wanted.
func newRawPhrase(must search.Searcher, terms [][]string, options search.SearcherOptions) *rawPhrase {
	conj, ok := must.(*ConjunctionSearcher)
	if !ok || options.Explain || conj.initialized || len(conj.searchers) != len(terms) {
		return nil
	}
	if _, ok := conj.scorer.(*similarity.CompositeSumScorer); !ok {
		return nil
	}
	n := len(terms)
	r := &rawPhrase{
		scorer:    conj.scorer,
		children:  make([]*TermSearcher, n),
		phrasePos: make([]int, n),
		terms:     make([]string, n),
		postings:  make([]segment.Posting, n),
		locs:      make([][]segment.Location, n),
		ptr:       make([]int, n),
		scores:    make([]search.DocumentMatch, n),
		scorePtr:  make([]*search.DocumentMatch, n),
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
		r.children[i], r.phrasePos[i], r.scorePtr[i] = ts, pos, &r.scores[i]
	}
	return r
}

func (r *rawPhrase) set(i int, p segment.Posting, err error) error {
	if err != nil {
		return err
	}
	if p == nil {
		r.done = true
	}
	r.postings[i] = p
	return nil
}

// next returns the next phrase hit at or after target scoring above floor,
// handing matching hits at or below it to skipped (nil: floor admits all).
func (r *rawPhrase) next(ctx *search.Context, target uint64, floor float64,
	skipped func(float64, int) bool) (*search.DocumentMatch, error) {
	if !r.started {
		r.started = true
		for i, ts := range r.children {
			p, err := ts.reader.Next()
			if err := r.set(i, p, err); err != nil {
				return nil, err
			}
		}
	}
	floorBits := numeric.Float64ToInt64(floor)
	for !r.done {
		maxNum := target
		for _, p := range r.postings {
			maxNum = max(maxNum, p.Number())
		}
		aligned := true
		for i, ts := range r.children {
			if r.postings[i].Number() >= maxNum {
				continue
			}
			p, err := ts.reader.Advance(maxNum)
			if err := r.set(i, p, err); err != nil {
				return nil, err
			}
			if r.done {
				return nil, nil
			}
			if p.Number() != maxNum {
				aligned = false
				break
			}
		}
		if !aligned {
			continue
		}
		var rv *search.DocumentMatch
		if r.out = r.match(r.out[:0]); len(r.out) > 0 {
			for i, ts := range r.children {
				p := r.postings[i]
				r.scores[i].Score = ts.scorer.Score(p.Frequency(), p.Norm())
			}
			score := r.scorer.ScoreComposite(r.scorePtr)
			if numeric.Float64ToInt64(score) > floorBits {
				rv = ctx.DocumentMatchPool.Get()
				rv.SetReader(r.indexReader)
				rv.Number = maxNum
				rv.Score = score
				rv.FieldTermLocations = append(rv.FieldTermLocations[:0], r.out...)
			} else if skipped == nil || !skipped(score, 1) {
				return nil, nil
			}
		}
		for i, ts := range r.children {
			p, err := ts.reader.Next()
			if err := r.set(i, p, err); err != nil {
				return nil, err
			}
		}
		if rv != nil {
			return rv, nil
		}
	}
	return nil, nil
}

// match appends the locations of every phrase occurrence in the aligned
// document to out: term t at position p+t for an occurrence p of term 0,
// in the same field.
func (r *rawPhrase) match(out []search.FieldTermLocation) []search.FieldTermLocation {
	for i, p := range r.postings {
		r.locs[r.phrasePos[i]] = p.Locations()
	}
	for _, l := range r.locs {
		if len(l) == 0 {
			return out
		}
	}
	if r.sortedOneField() {
		return r.mergeSorted(out)
	}
	return r.matchAny(out)
}

// sortedOneField reports whether all locations share one field and each
// term's positions strictly ascend, so a forward merge finds every match.
func (r *rawPhrase) sortedOneField() bool {
	field := r.locs[0][0].Field()
	for _, l := range r.locs {
		prev := math.MinInt
		for _, loc := range l {
			if loc.Field() != field || loc.Pos() <= prev {
				return false
			}
			prev = loc.Pos()
		}
	}
	return true
}

func (r *rawPhrase) mergeSorted(out []search.FieldTermLocation) []search.FieldTermLocation {
	clear(r.ptr)
	for _, l0 := range r.locs[0] {
		p := l0.Pos()
		for t := 1; t < len(r.locs); t++ {
			l, want := r.locs[t], p+t
			for r.ptr[t] < len(l) && l[r.ptr[t]].Pos() < want {
				r.ptr[t]++
			}
			if r.ptr[t] >= len(l) {
				return out // term t is exhausted, no later start can match
			}
			if l[r.ptr[t]].Pos() != want {
				goto nextStart
			}
		}
		out = r.appendLoc(out, 0, l0)
		for t := 1; t < len(r.locs); t++ {
			out = r.appendLoc(out, t, r.locs[t][r.ptr[t]])
		}
	nextStart:
	}
	return out
}

// matchAny is the quadratic match for multi-field or unsorted locations.
func (r *rawPhrase) matchAny(out []search.FieldTermLocation) []search.FieldTermLocation {
	for _, l0 := range r.locs[0] {
		for t := 1; t < len(r.locs); t++ {
			r.ptr[t] = -1
			for j, loc := range r.locs[t] {
				if loc.Field() == l0.Field() && loc.Pos() == l0.Pos()+t {
					r.ptr[t] = j
					break
				}
			}
			if r.ptr[t] < 0 {
				goto nextStart
			}
		}
		out = r.appendLoc(out, 0, l0)
		for t := 1; t < len(r.locs); t++ {
			out = r.appendLoc(out, t, r.locs[t][r.ptr[t]])
		}
	nextStart:
	}
	return out
}

func (r *rawPhrase) appendLoc(out []search.FieldTermLocation, t int, loc segment.Location) []search.FieldTermLocation {
	return append(out, search.FieldTermLocation{
		Field:    loc.Field(),
		Term:     r.terms[t],
		Location: search.Location{Pos: loc.Pos(), Start: loc.Start(), End: loc.End()},
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
