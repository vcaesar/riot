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

	"github.com/RoaringBitmap/roaring/v2"
	segment "github.com/vcaesar/bluge_segment_api"

	"github.com/vcaesar/riot/numeric"
	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/similarity"
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
	if s.raw == nil && !s.initialized && s.rawEligible() {
		if err := s.startRaw(ctx); err != nil {
			return nil, err
		}
	}
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
	if s.raw == nil && !s.initialized && s.rawEligible() {
		if err := s.startRaw(ctx); err != nil {
			return nil, err
		}
	}
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
		if !s.rawEligible() {
			return s.Next(ctx)
		}
		if err := s.startRaw(ctx); err != nil {
			return nil, err
		}
	}
	return s.raw.next(ctx, 0, floor, skipped)
}

func (s *DisjunctionSliceSearcher) rawEligible() bool {
	return s.min <= 1 && rawTermChildren(s.options, s.scorer, s.searchers)
}

func (s *DisjunctionSliceSearcher) startRaw(ctx *search.Context) error {
	raw, err := newRawDisjunction(s, ctx)
	if err != nil {
		return err
	}
	s.raw = raw
	return nil
}

// rawDisjunction is a disjunction of term postings positioned on
// (number, freq, norm) per child; children are summed in searcher order,
// as updateMatches orders them, so sums are bit identical to
// CompositeSumScorer.ScoreComposite.
type rawDisjunction struct {
	indexReader search.Reader
	boost       float64
	terms       []*TermSearcher
	heads       []rawHead
	// blocks is set when every child can report block bounds and scores
	// monotonically, so windows whose bound loses can be counted from the
	// postings bitmaps and skipped whole; boundEnd is the end of the last
	// window found competitive against floor boundFloor, which the floor
	// may rise above before boundEnd
	blocks     []blockBoundReader
	boundEnd   uint64
	boundFloor int64
	// bms are the live children's segment bitmaps of the last window,
	// disjoint whether no two of them share a document, which bounds every
	// hit by a single child, and union their union once a window needed it
	bms      []*roaring.Bitmap
	disjoint bool
	union    *roaring.Bitmap
}

// rawHead is a child's current posting.
type rawHead struct {
	num   uint64
	freq  int // < 0: score carried over from a DocumentMatch
	norm  float64
	score float64
	live  bool
}

// newRawDisjunction takes over s's children: positioned ones seed the raw
// state from their current DocumentMatch, otherwise each reads its first
// posting.
func newRawDisjunction(s *DisjunctionSliceSearcher, ctx *search.Context) (*rawDisjunction, error) {
	n := len(s.searchers)
	r := &rawDisjunction{
		boost:  s.scorer.(*similarity.CompositeSumScorer).Boost(),
		terms:  make([]*TermSearcher, n),
		heads:  make([]rawHead, n),
		blocks: make([]blockBoundReader, 0, n),
	}
	for i, child := range s.searchers {
		ts := child.(*TermSearcher)
		r.indexReader = ts.indexReader
		r.terms[i] = ts
		ts.cursor.startBatch(ts.reader, false)
		if br, ok := ts.reader.(blockBoundReader); ok && monotoneScorer(ts.scorer) {
			r.blocks = append(r.blocks, br)
		}
		if !s.initialized {
			if err := r.stepNext(i); err != nil {
				return nil, err
			}
			continue
		}
		if s.currs[i] == nil {
			continue
		}
		r.heads[i] = rawHead{num: s.currs[i].Number, freq: -1, score: s.currs[i].Score, live: true}
		ctx.DocumentMatchPool.Put(s.currs[i])
		s.currs[i] = nil
	}
	if len(r.blocks) != n || r.boost < 0 {
		r.blocks = nil // a negative boost would invert the bound
	}
	return r, nil
}

// skipWindow checks the block window starting at number, where every live
// child is positioned at or after number: when no hit in it can beat floor
// it returns the window end and how many hits it holds. ok is false when
// the window may hold a competitive hit or cannot be counted.
func (r *rawDisjunction) skipWindow(number uint64, floorBits int64) (end, hits uint64, ok bool) {
	end = math.MaxUint64
	var sum, top float64
	var base uint64
	bms, same := r.bms[:0], true
	for i := range r.heads {
		if !r.heads[i].live {
			continue
		}
		br := r.blocks[i]
		maxFreq, minNorm, blockEnd, found := br.BlockBound(number)
		if !found {
			return 0, 0, false
		}
		bm, segBase := br.SegmentBitmap(number)
		if bm == nil {
			return 0, 0, false
		}
		end, base = min(end, blockEnd), segBase
		// summed in searcher order, as next sums the hit scores; a hit may
		// lack this child, which then adds 0, so a negative bound counts as 0
		b := max(r.terms[i].scorer.Score(maxFreq, minNorm), 0)
		sum, top = sum+b, max(top, b)
		// bms reuses r.bms: compare each slot before overwriting it
		same = same && len(bms) < len(r.bms) && r.bms[len(bms)] == bm
		bms = append(bms, bm)
	}
	if !same || len(bms) != len(r.bms) {
		r.disjoint, r.union = disjointBitmaps(bms), nil
	}
	r.bms = bms
	bound := sum
	if r.disjoint {
		bound = top
	}
	if numeric.Float64ToInt64(bound*r.boost) > floorBits {
		r.boundEnd, r.boundFloor = end, floorBits
		return 0, 0, false
	}
	lo, hi := number-base, end-base
	if r.disjoint {
		for _, bm := range bms {
			hits += bm.CardinalityInRange(lo, hi)
		}
	} else {
		if r.union == nil {
			r.union = roaring.FastOr(bms...)
		}
		hits = r.union.CardinalityInRange(lo, hi)
	}
	return end, hits, true
}

func disjointBitmaps(bms []*roaring.Bitmap) bool {
	for i, a := range bms {
		for _, b := range bms[i+1:] {
			if a.Intersects(b) {
				return false
			}
		}
	}
	return true
}

func (r *rawDisjunction) step(i int, p segment.Posting, err error) error {
	if err != nil {
		return err
	}
	h := &r.heads[i]
	if p == nil {
		h.live = false
		return nil
	}
	h.num, h.freq, h.norm, h.live = p.Number(), p.Frequency(), p.Norm(), true
	return nil
}

// stepNext moves child i to its next posting, straight from its batch
// when one is buffered.
func (r *rawDisjunction) stepNext(i int) error {
	c := &r.terms[i].cursor
	if b := c.batch; b != nil && !c.done && b.pos < b.n {
		r.heads[i] = rawHead{num: b.docs[b.pos], freq: b.freqs[b.pos], norm: b.norms[b.pos], live: true}
		b.pos++
		c.seen++
		return nil
	}
	p, err := c.next(r.terms[i].reader)
	return r.step(i, p, err)
}

func (r *rawDisjunction) next(ctx *search.Context, target uint64, floor float64,
	skipped func(float64, int) bool) (*search.DocumentMatch, error) {
	floorBits := numeric.Float64ToInt64(floor)
	for {
		minNum, found, err := r.first(target)
		if err != nil || !found {
			return nil, err
		}
		if r.blocks != nil && skipped != nil && ctx.SkipBlocks &&
			(minNum >= r.boundEnd || floorBits > r.boundFloor) {
			if end, hits, ok := r.skipWindow(minNum, floorBits); ok {
				// #nosec G115 -- hits counts postings of one uint32-numbered segment.
				if hits > 0 && !skipped(floor, int(hits)) {
					return nil, nil
				}
				target = end
				continue
			}
		}
		target = 0
		score, err := r.scoreAt(minNum)
		if err != nil {
			return nil, err
		}
		if skipped == nil || numeric.Float64ToInt64(score) > floorBits {
			rv := ctx.DocumentMatchPool.Get()
			rv.SetReader(r.indexReader)
			rv.Number = minNum
			rv.Score = score
			return rv, nil
		}
		if !skipped(score, 1) {
			return nil, nil
		}
	}
}

// first advances the children behind target to it and returns the
// smallest document number they are on; found is false once all are done.
func (r *rawDisjunction) first(target uint64) (minNum uint64, found bool, err error) {
	for i := range r.heads {
		h := &r.heads[i]
		if !h.live {
			continue
		}
		if h.num < target {
			ts := r.terms[i]
			p, err := ts.cursor.advance(ts.reader, target)
			if err := r.step(i, p, err); err != nil {
				return 0, false, err
			}
			if !h.live {
				continue
			}
		}
		if !found || h.num < minNum {
			minNum, found = h.num, true
		}
	}
	return minNum, found, nil
}

// scoreAt sums, in searcher order, the scores of the children on number
// and moves them to their next postings.
func (r *rawDisjunction) scoreAt(number uint64) (float64, error) {
	var score float64
	for i := range r.heads {
		h := &r.heads[i]
		if !h.live || h.num != number {
			continue
		}
		if h.freq >= 0 {
			score += r.terms[i].scorer.Score(h.freq, h.norm)
		} else {
			score += h.score
		}
		if err := r.stepNext(i); err != nil {
			return 0, err
		}
	}
	return score * r.boost, nil
}
