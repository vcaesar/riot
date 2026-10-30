//  Copyright (c) 2020 Couchbase, Inc.
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
	"sort"

	segment "github.com/vcaesar/bluge_segment_api"

	"github.com/vcaesar/riot/numeric"
	"github.com/vcaesar/riot/search"
)

type DisjunctionSliceSearcher struct {
	searchers    OrderedSearcherList
	numSearchers int
	currs        []*search.DocumentMatch
	scorer       search.CompositeScorer
	min          int
	matching     []*search.DocumentMatch
	matchingIdxs []int
	initialized  bool
	options      search.SearcherOptions
	// raw is set once NextAbove steps the term postings directly
	raw *rawDisjunction
}

func newDisjunctionSliceSearcher(qsearchers []search.Searcher, minMatches int, scorer search.CompositeScorer,
	options search.SearcherOptions, limit bool) (
	*DisjunctionSliceSearcher, error) {
	if limit && tooManyClauses(len(qsearchers)) {
		return nil, tooManyClausesErr("", len(qsearchers))
	}
	// build the downstream searchers
	searchers := make(OrderedSearcherList, len(qsearchers))
	copy(searchers, qsearchers)
	// sort the searchers
	sort.Sort(sort.Reverse(searchers))
	// build our searcher
	rv := DisjunctionSliceSearcher{
		searchers:    searchers,
		numSearchers: len(searchers),
		currs:        make([]*search.DocumentMatch, len(searchers)),
		scorer:       scorer,
		min:          minMatches,
		matching:     make([]*search.DocumentMatch, len(searchers)),
		matchingIdxs: make([]int, len(searchers)),
		options:      options,
	}
	return &rv, nil
}

func (s *DisjunctionSliceSearcher) Size() int {
	sizeInBytes := reflectStaticSizeDisjunctionSliceSearcher + sizeOfPtr

	for _, entry := range s.searchers {
		sizeInBytes += entry.Size()
	}

	for _, entry := range s.currs {
		if entry != nil {
			sizeInBytes += entry.Size()
		}
	}

	for _, entry := range s.matching {
		if entry != nil {
			sizeInBytes += entry.Size()
		}
	}

	sizeInBytes += len(s.matchingIdxs) * sizeOfInt

	return sizeInBytes
}

func (s *DisjunctionSliceSearcher) initSearchers(ctx *search.Context) error {
	var err error
	// get all searchers pointing at their first match
	for i, searcher := range s.searchers {
		if s.currs[i] != nil {
			ctx.DocumentMatchPool.Put(s.currs[i])
		}
		s.currs[i], err = searcher.Next(ctx)
		if err != nil {
			return err
		}
	}

	err = s.updateMatches()
	if err != nil {
		return err
	}

	s.initialized = true
	return nil
}

func (s *DisjunctionSliceSearcher) updateMatches() error {
	matching := s.matching[:0]
	matchingIdxs := s.matchingIdxs[:0]

	for i := 0; i < len(s.currs); i++ {
		curr := s.currs[i]
		if curr == nil {
			continue
		}

		if len(matching) > 0 {
			cmp := docNumberCompare(curr.Number, matching[0].Number)
			if cmp > 0 {
				continue
			}

			if cmp < 0 {
				matching = matching[:0]
				matchingIdxs = matchingIdxs[:0]
			}
		}

		matching = append(matching, curr)
		matchingIdxs = append(matchingIdxs, i)
	}

	s.matching = matching
	s.matchingIdxs = matchingIdxs

	return nil
}

func (s *DisjunctionSliceSearcher) Next(ctx *search.Context) (
	*search.DocumentMatch, error) {
	if s.raw != nil {
		return s.raw.next(ctx, 0, math.Inf(-1), nil)
	}
	if !s.initialized {
		err := s.initSearchers(ctx)
		if err != nil {
			return nil, err
		}
	}
	var err error
	var rv *search.DocumentMatch

	found := false
	for !found && len(s.matching) > 0 {
		if len(s.matching) >= s.min {
			found = true
			// score this match
			rv = s.buildDocumentMatch(s.matching)
		}

		// invoke next on all the matching searchers
		for _, i := range s.matchingIdxs {
			searcher := s.searchers[i]
			if s.currs[i] != rv {
				ctx.DocumentMatchPool.Put(s.currs[i])
			}
			s.currs[i], err = searcher.Next(ctx)
			if err != nil {
				return nil, err
			}
		}

		err = s.updateMatches()
		if err != nil {
			return nil, err
		}
	}

	return rv, nil
}

func (s *DisjunctionSliceSearcher) Advance(ctx *search.Context,
	number uint64) (*search.DocumentMatch, error) {
	if s.raw != nil {
		return s.raw.next(ctx, number, math.Inf(-1), nil)
	}
	if !s.initialized {
		err := s.initSearchers(ctx)
		if err != nil {
			return nil, err
		}
	}
	// get all searchers pointing at their first match
	var err error
	for i, searcher := range s.searchers {
		if s.currs[i] != nil {
			if s.currs[i].Number >= number {
				continue
			}
			ctx.DocumentMatchPool.Put(s.currs[i])
		}
		s.currs[i], err = searcher.Advance(ctx, number)
		if err != nil {
			return nil, err
		}
	}

	err = s.updateMatches()
	if err != nil {
		return nil, err
	}

	return s.Next(ctx)
}

func (s *DisjunctionSliceSearcher) Count() uint64 {
	// for now return a worst case
	var sum uint64
	for _, searcher := range s.searchers {
		sum += searcher.Count()
	}
	return sum
}

func (s *DisjunctionSliceSearcher) Close() (rv error) {
	for _, searcher := range s.searchers {
		err := searcher.Close()
		if err != nil && rv == nil {
			rv = err
		}
	}
	return rv
}

func (s *DisjunctionSliceSearcher) Min() int {
	return s.min
}

func (s *DisjunctionSliceSearcher) DocumentMatchPoolSize() int {
	rv := len(s.currs)
	for _, s := range s.searchers {
		rv += s.DocumentMatchPoolSize()
	}
	return rv
}

// a disjunction searcher implements the index.Optimizable interface
// but only activates on an edge case where the disjunction is a
// wrapper around a single Optimizable child searcher
func (s *DisjunctionSliceSearcher) Optimize(kind string, octx segment.OptimizableContext) (
	segment.OptimizableContext, error) {
	if len(s.searchers) == 1 {
		o, ok := s.searchers[0].(segment.Optimizable)
		if ok {
			return o.Optimize(kind, octx)
		}
	}

	return nil, nil
}

func docNumberCompare(a, b uint64) int {
	if a < b {
		return -1
	} else if a > b {
		return 1
	}
	return 0
}

func (s *DisjunctionSliceSearcher) buildDocumentMatch(constituents []*search.DocumentMatch) *search.DocumentMatch {
	rv := constituents[0]
	if s.options.Explain {
		rv.Explanation = s.scorer.ExplainComposite(constituents)
		rv.Score = rv.Explanation.Value
	} else {
		rv.Score = s.scorer.ScoreComposite(constituents)
	}

	rv.FieldTermLocations = search.MergeFieldTermLocations(
		rv.FieldTermLocations, constituents[1:])

	return rv
}

// NextAbove implements search.ScoreSkipper. For a sum of plain term
// searchers it steps their postings directly and only builds a
// DocumentMatch for hits scoring above floor; otherwise it is Next.
func (s *DisjunctionSliceSearcher) NextAbove(ctx *search.Context, floor float64,
	skipped func(score float64, n int) bool) (*search.DocumentMatch, error) {
	if s.raw == nil {
		if s.min > 1 || !rawTermChildren(s.options, s.scorer, s.searchers) {
			return s.Next(ctx)
		}
		if !s.initialized {
			if err := s.initSearchers(ctx); err != nil {
				return nil, err
			}
		}
		s.raw = newRawDisjunction(s, ctx)
	}
	return s.raw.next(ctx, 0, floor, skipped)
}

// rawDisjunction is a disjunction of term postings positioned on
// (number, freq, norm) per child; children are scored in searcher order,
// as updateMatches orders them, so composite sums are bit identical.
type rawDisjunction struct {
	indexReader search.Reader
	scorer      search.CompositeScorer
	terms       []*TermSearcher
	live        []bool
	nums        []uint64
	freqs       []int // < 0: score carried over from a DocumentMatch
	norms       []float64
	scores      []search.DocumentMatch
	matching    []*search.DocumentMatch
	matchIdx    []int
}

func newRawDisjunction(s *DisjunctionSliceSearcher, ctx *search.Context) *rawDisjunction {
	n := len(s.searchers)
	r := &rawDisjunction{
		scorer:   s.scorer,
		terms:    make([]*TermSearcher, n),
		live:     make([]bool, n),
		nums:     make([]uint64, n),
		freqs:    make([]int, n),
		norms:    make([]float64, n),
		scores:   make([]search.DocumentMatch, n),
		matching: make([]*search.DocumentMatch, 0, n),
		matchIdx: make([]int, 0, n),
	}
	for i, child := range s.searchers {
		ts := child.(*TermSearcher)
		r.indexReader = ts.indexReader
		r.terms[i] = ts
		ts.cursor.startBatch(ts.reader, false)
		if s.currs[i] == nil {
			continue
		}
		r.live[i] = true
		r.nums[i] = s.currs[i].Number
		r.freqs[i] = -1
		r.scores[i].Score = s.currs[i].Score
		ctx.DocumentMatchPool.Put(s.currs[i])
		s.currs[i] = nil
	}
	return r
}

func (r *rawDisjunction) step(i int, p segment.Posting, err error) error {
	if err != nil {
		return err
	}
	if p == nil {
		r.live[i] = false
		return nil
	}
	r.nums[i], r.freqs[i], r.norms[i] = p.Number(), p.Frequency(), p.Norm()
	return nil
}

func (r *rawDisjunction) next(ctx *search.Context, target uint64, floor float64,
	skipped func(float64, int) bool) (*search.DocumentMatch, error) {
	floorBits := numeric.Float64ToInt64(floor)
	for {
		minNum, found := uint64(0), false
		for i, ts := range r.terms {
			if !r.live[i] {
				continue
			}
			if r.nums[i] < target {
				p, err := ts.cursor.advance(ts.reader, target)
				if err := r.step(i, p, err); err != nil {
					return nil, err
				}
				if !r.live[i] {
					continue
				}
			}
			if !found || r.nums[i] < minNum {
				minNum, found = r.nums[i], true
			}
		}
		if !found {
			return nil, nil
		}
		target = 0
		r.matching, r.matchIdx = r.matching[:0], r.matchIdx[:0]
		for i, ts := range r.terms {
			if !r.live[i] || r.nums[i] != minNum {
				continue
			}
			if r.freqs[i] >= 0 {
				r.scores[i].Score = ts.scorer.Score(r.freqs[i], r.norms[i])
			}
			r.matching = append(r.matching, &r.scores[i])
			r.matchIdx = append(r.matchIdx, i)
		}
		score := r.scorer.ScoreComposite(r.matching)
		for _, i := range r.matchIdx {
			ts := r.terms[i]
			p, err := ts.cursor.next(ts.reader)
			if err := r.step(i, p, err); err != nil {
				return nil, err
			}
		}
		if numeric.Float64ToInt64(score) > floorBits {
			rv := ctx.DocumentMatchPool.Get()
			rv.SetReader(r.indexReader)
			rv.Number = minNum
			rv.Score = score
			return rv, nil
		}
		if skipped == nil || !skipped(score, 1) {
			return nil, nil
		}
	}
}
