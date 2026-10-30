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

type ConjunctionSearcher struct {
	searchers   OrderedSearcherList
	currs       []*search.DocumentMatch
	maxIDIdx    int
	initialized bool
	options     search.SearcherOptions
	scorer      search.CompositeScorer
	indexReader search.Reader
	// raw is set once NextAbove has moved from currs to stepping the term
	// postings directly; see nextRaw.
	raw *rawConjunction
}

func NewConjunctionSearcher(indexReader search.Reader,
	qsearchers []search.Searcher, scorer search.CompositeScorer, options search.SearcherOptions) (
	search.Searcher, error) {
	// build the sorted downstream searchers
	searchers := make(OrderedSearcherList, len(qsearchers))
	copy(searchers, qsearchers)
	sort.Sort(searchers)

	// attempt the "unadorned" conjunction optimization only when we
	// do not need extra information like freq-norm's or term vectors
	if len(searchers) > 1 &&
		options.Score == optionScoringNone && !options.IncludeTermVectors {
		rv, err := optimizeCompositeSearcher("conjunction:unadorned",
			indexReader, searchers, options)
		if err != nil || rv != nil {
			return rv, err
		}
	}

	// build our searcher
	rv := ConjunctionSearcher{
		options:     options,
		searchers:   searchers,
		currs:       make([]*search.DocumentMatch, len(searchers)),
		scorer:      scorer,
		indexReader: indexReader,
	}

	// attempt push-down conjunction optimization when there's >1 searchers
	if len(searchers) > 1 {
		rv, err := optimizeCompositeSearcher("conjunction",
			indexReader, searchers, options)
		if err != nil || rv != nil {
			return rv, err
		}
	}

	return &rv, nil
}

func (s *ConjunctionSearcher) Size() int {
	sizeInBytes := reflectStaticSizeConjunctionSearcher + sizeOfPtr

	for _, entry := range s.searchers {
		sizeInBytes += entry.Size()
	}

	for _, entry := range s.currs {
		if entry != nil {
			sizeInBytes += entry.Size()
		}
	}

	return sizeInBytes
}

func (s *ConjunctionSearcher) initSearchers(ctx *search.Context) error {
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
	s.initialized = true
	return nil
}

func (s *ConjunctionSearcher) Next(ctx *search.Context) (*search.DocumentMatch, error) {
	if s.raw != nil {
		return s.raw.next(ctx, 0, math.Inf(-1), nil)
	}
	if !s.initialized {
		err := s.initSearchers(ctx)
		if err != nil {
			return nil, err
		}
	}
	var rv *search.DocumentMatch
	var err error
OUTER:
	for s.maxIDIdx < len(s.currs) && s.currs[s.maxIDIdx] != nil {
		maxID := s.currs[s.maxIDIdx].Number

		i := 0
		for i < len(s.currs) {
			if s.currs[i] == nil {
				return nil, nil
			}

			if i == s.maxIDIdx {
				i++
				continue
			}

			cmp := docNumberCompare(maxID, s.currs[i].Number)
			if cmp == 0 {
				i++
				continue
			}

			if cmp < 0 {
				// maxID < currs[i], so we found a new maxIDIdx
				s.maxIDIdx = i

				// Stop aligning as soon as a child overshoots: the remaining
				// seeks would target a candidate that can no longer match.
				maxID = s.currs[i].Number
				for x := 0; x < i; x++ {
					err = s.advanceChild(ctx, x, maxID)
					if err != nil {
						return nil, err
					}
					if s.currs[x] == nil {
						return nil, nil
					}
					if s.currs[x].Number > maxID {
						s.maxIDIdx = x
						break
					}
				}

				continue OUTER
			}

			// maxID > currs[i], so need to advance searchers[i]
			err = s.advanceChild(ctx, i, maxID)
			if err != nil {
				return nil, err
			}

			// don't bump i, so that we'll examine the just-advanced
			// currs[i] again
		}

		// if we get here, a doc matched all readers, so score and add it
		rv = s.buildDocumentMatch(s.currs)

		// we know all the searchers are pointing at the same thing
		// so they all need to be bumped
		for i, searcher := range s.searchers {
			if s.currs[i] != rv {
				ctx.DocumentMatchPool.Put(s.currs[i])
			}
			s.currs[i], err = searcher.Next(ctx)
			if err != nil {
				return nil, err
			}
		}

		// don't continue now, wait for the next call to Next()
		break
	}

	return rv, nil
}

func (s *ConjunctionSearcher) Advance(ctx *search.Context, number uint64) (*search.DocumentMatch, error) {
	if s.raw != nil {
		return s.raw.next(ctx, number, math.Inf(-1), nil)
	}
	for i := range s.searchers {
		if s.initialized && s.currs[i] == nil {
			return nil, nil
		}
		if s.currs[i] == nil || s.currs[i].Number < number {
			if err := s.advanceChild(ctx, i, number); err != nil {
				return nil, err
			}
			if s.currs[i] == nil {
				s.initialized = true
				return nil, nil
			}
		}
		// Later children cannot match below an earlier child's seek result.
		number = s.currs[i].Number
		s.maxIDIdx = i
	}
	s.initialized = true
	return s.Next(ctx)
}

func (s *ConjunctionSearcher) advanceChild(ctx *search.Context, i int, number uint64) (err error) {
	if s.currs[i] != nil {
		ctx.DocumentMatchPool.Put(s.currs[i])
	}
	s.currs[i], err = s.searchers[i].Advance(ctx, number)
	return err
}

func (s *ConjunctionSearcher) Count() uint64 {
	// for now return a worst case
	var sum uint64
	for _, searcher := range s.searchers {
		sum += searcher.Count()
	}
	return sum
}

func (s *ConjunctionSearcher) Close() (rv error) {
	for _, searcher := range s.searchers {
		err := searcher.Close()
		if err != nil && rv == nil {
			rv = err
		}
	}
	return rv
}

func (s *ConjunctionSearcher) Min() int {
	return 0
}

func (s *ConjunctionSearcher) DocumentMatchPoolSize() int {
	rv := len(s.currs)
	for _, s := range s.searchers {
		rv += s.DocumentMatchPoolSize()
	}
	return rv
}

func (s *ConjunctionSearcher) buildDocumentMatch(constituents []*search.DocumentMatch) *search.DocumentMatch {
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
// searchers it steps their postings by document number and only builds a
// DocumentMatch for hits scoring above floor; otherwise it is Next.
func (s *ConjunctionSearcher) NextAbove(ctx *search.Context, floor float64,
	skipped func(score float64, n int) bool) (*search.DocumentMatch, error) {
	if s.raw == nil {
		if !s.rawEligible() {
			return s.Next(ctx)
		}
		if !s.initialized {
			if err := s.initSearchers(ctx); err != nil {
				return nil, err
			}
		}
		s.raw = newRawConjunction(s, ctx)
	}
	return s.raw.next(ctx, 0, floor, skipped)
}

func (s *ConjunctionSearcher) rawEligible() bool {
	return rawTermChildren(s.options, s.scorer, s.searchers)
}

// rawTermChildren reports whether children are plain term searchers summed
// by a CompositeSumScorer, with nothing needing their DocumentMatches.
func rawTermChildren(options search.SearcherOptions, scorer search.CompositeScorer,
	children []search.Searcher) bool {
	if options.Explain || options.IncludeTermVectors {
		return false
	}
	if _, ok := scorer.(*similarity.CompositeSumScorer); !ok {
		return false
	}
	for _, child := range children {
		ts, ok := child.(*TermSearcher)
		if !ok || ts.options.Explain || ts.options.IncludeTermVectors {
			return false
		}
	}
	return true
}

// rawConjunction is a conjunction of term postings positioned on
// (number, freq, norm) per child, scored only where all children agree.
type rawConjunction struct {
	indexReader search.Reader
	scorer      search.CompositeScorer
	terms       []*TermSearcher
	nums        []uint64
	freqs       []int
	norms       []float64
	// scores holds the child scores in searcher order, as the composite
	// scorer saw them through DocumentMatches, so sums are bit identical
	scores   []search.DocumentMatch
	scorePtr []*search.DocumentMatch
	done     bool
	// blocks is set when every child can report block bounds and scores
	// monotonically, so windows whose bound loses can be skipped whole;
	// boundEnd is the end of the last window found competitive
	blocks   []blockBoundReader
	boundEnd uint64
	bounds   []search.DocumentMatch
	boundPtr []*search.DocumentMatch
}

// blockBoundReader is implemented by postings iterators that expose the
// freq/norm bounds of the block holding a document and the per-segment
// actual bitmap (index snapshot iterators).
type blockBoundReader interface {
	BlockBound(number uint64) (maxFreq int, minNorm float64, end uint64, ok bool)
	SegmentBitmap(number uint64) (*roaring.Bitmap, uint64)
}

// newRawConjunction takes over s's positioned children: each currs entry
// already holds the child's next hit, scored, which seeds the raw state.
func newRawConjunction(s *ConjunctionSearcher, ctx *search.Context) *rawConjunction {
	n := len(s.searchers)
	r := &rawConjunction{
		indexReader: s.indexReader,
		scorer:      s.scorer,
		terms:       make([]*TermSearcher, n),
		nums:        make([]uint64, n),
		freqs:       make([]int, n),
		norms:       make([]float64, n),
		scores:      make([]search.DocumentMatch, n),
		scorePtr:    make([]*search.DocumentMatch, n),
		blocks:      make([]blockBoundReader, 0, n),
		bounds:      make([]search.DocumentMatch, n),
		boundPtr:    make([]*search.DocumentMatch, n),
	}
	for i, child := range s.searchers {
		ts := child.(*TermSearcher)
		ts.cursor.startBatch(ts.reader, false)
		r.terms[i] = ts
		r.scorePtr[i] = &r.scores[i]
		r.boundPtr[i] = &r.bounds[i]
		if br, ok := ts.reader.(blockBoundReader); ok && monotoneScorer(ts.scorer) {
			r.blocks = append(r.blocks, br)
		}
		if s.currs[i] == nil {
			r.done = true
			continue
		}
		r.nums[i] = s.currs[i].Number
		// freqs[i] < 0 marks a score carried over from currs
		r.freqs[i] = -1
		r.scores[i].Score = s.currs[i].Score
		ctx.DocumentMatchPool.Put(s.currs[i])
		s.currs[i] = nil
	}
	if len(r.blocks) != n || r.scorer.ScoreComposite([]*search.DocumentMatch{{Score: 1}}) < 0 {
		r.blocks = nil // a negative boost would invert the bound
	}
	return r
}

// skipWindow checks the block window starting at number: when the sum of
// the children's block bounds cannot beat floor, every hit in the window
// loses, and it returns the window end and how many hits it holds. ok is
// false when the window may hold a competitive hit or cannot be counted.
func (r *rawConjunction) skipWindow(number uint64, floorBits int64) (end, hits uint64, ok bool) {
	end = math.MaxUint64
	var bm *roaring.Bitmap
	var base uint64
	for i, br := range r.blocks {
		maxFreq, minNorm, blockEnd, found := br.BlockBound(number)
		if !found {
			return 0, 0, false
		}
		end = min(end, blockEnd)
		r.bounds[i].Score = r.terms[i].scorer.Score(maxFreq, minNorm)
		// the conjunction optimization shares one AND'ed bitmap among the
		// children, which counts the window's hits exactly
		segBM, segBase := br.SegmentBitmap(number)
		if segBM == nil || (bm != nil && segBM != bm) {
			return 0, 0, false
		}
		bm, base = segBM, segBase
	}
	bound := r.scorer.ScoreComposite(r.boundPtr)
	if numeric.Float64ToInt64(bound) > floorBits {
		r.boundEnd = end
		return 0, 0, false
	}
	return end, bm.CardinalityInRange(number-base, end-base), true
}

// skipLosingWindow reports a losing block window at number to skipped:
// ok means the window was skipped up to end, stop that skipped said halt.
func (r *rawConjunction) skipLosingWindow(number uint64, floor float64, floorBits int64,
	skipped func(float64, int) bool) (end uint64, stop, ok bool) {
	end, hits, ok := r.skipWindow(number, floorBits)
	if !ok {
		return 0, false, false
	}
	// #nosec G115 -- hits counts postings of one uint32-numbered segment.
	if hits > 0 && !skipped(floor, int(hits)) {
		return 0, true, true
	}
	return end, false, true
}

// align advances the children behind maxNum to it and reports whether all
// of them landed on it; r.done is set when one runs out.
func (r *rawConjunction) align(maxNum uint64) (bool, error) {
	for i, n := range r.nums {
		if n >= maxNum {
			continue
		}
		ts := r.terms[i]
		p, err := ts.cursor.advance(ts.reader, maxNum)
		if err != nil {
			return false, err
		}
		if !r.set(i, p) {
			return false, nil
		}
		if r.nums[i] != maxNum {
			return false, nil
		}
	}
	return true, nil
}

func (r *rawConjunction) set(i int, p segment.Posting) bool {
	if p == nil {
		r.done = true
		return false
	}
	r.nums[i] = p.Number()
	r.freqs[i] = p.Frequency()
	r.norms[i] = p.Norm()
	return true
}

// next returns the next hit at or after target scoring above floor, handing
// the hits it passes over to skipped (nil when floor admits every hit).
func (r *rawConjunction) next(ctx *search.Context, target uint64, floor float64,
	skipped func(float64, int) bool) (*search.DocumentMatch, error) {
	floorBits := numeric.Float64ToInt64(floor)
	for !r.done {
		maxNum := target
		for _, n := range r.nums {
			maxNum = max(maxNum, n)
		}
		if r.blocks != nil && skipped != nil && ctx.SkipBlocks && maxNum >= r.boundEnd {
			end, stop, ok := r.skipLosingWindow(maxNum, floor, floorBits, skipped)
			if stop {
				return nil, nil
			}
			if ok {
				target = end
				continue
			}
		}
		aligned, err := r.align(maxNum)
		if err != nil || r.done {
			return nil, err
		}
		if !aligned {
			continue
		}
		target = 0
		for i := range r.nums {
			if r.freqs[i] >= 0 {
				r.scores[i].Score = r.terms[i].scorer.Score(r.freqs[i], r.norms[i])
			}
		}
		score := r.scorer.ScoreComposite(r.scorePtr)
		var rv *search.DocumentMatch
		if skipped == nil || numeric.Float64ToInt64(score) > floorBits {
			rv = ctx.DocumentMatchPool.Get()
			rv.SetReader(r.indexReader)
			rv.Number = maxNum
			rv.Score = score
		}
		for i, ts := range r.terms {
			p, err := ts.cursor.next(ts.reader)
			if err != nil {
				return nil, err
			}
			r.set(i, p)
		}
		if rv != nil {
			return rv, nil
		}
		if !skipped(score, 1) {
			return nil, nil
		}
	}
	return nil, nil
}
