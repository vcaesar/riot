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
	"errors"
	"math"
	"slices"
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/similarity"
)

func TestDisjunctionCovOptimize(t *testing.T) {
	none := noExplain()
	none.Score = optionScoringNone
	tests := []struct {
		name    string
		min     int
		optErr  error
		noOpt   bool
		plain   bool // the second child cannot be optimized
		wantMin int
		wantErr bool
	}{
		{name: "merged"},
		{name: "merged keeps min", min: 1, wantMin: 1},
		{name: "error", optErr: errCov, wantErr: true},
		{name: "declined", noOpt: true},
		{name: "not optimizable", plain: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := newCovBatch(covDocs(1, 3), 0)
			a.optErr, a.noOpt = tc.optErr, tc.noOpt
			var b search.Searcher = covTerm(newCovBatch(covDocs(2, 3), 0), none)
			if tc.plain {
				b = &covSearcher{docs: docs(2, 3)}
			}
			s, err := NewDisjunctionSearcher(baseTestIndexReader, []search.Searcher{covTerm(a, none), b}, tc.min,
				similarity.NewCompositeSumScorer(), none)
			if tc.wantErr {
				if !errors.Is(err, errCov) {
					t.Fatalf("got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, slice := s.(*DisjunctionSliceSearcher); slice != (tc.noOpt || tc.plain) || s.Min() != tc.wantMin {
				t.Fatalf("got %T min %d", s, s.Min())
			}
			if got, err := covDrain(covCtx(), s); err != nil || !slices.Equal(got, docs(1, 2, 3)) {
				t.Fatalf("got %v %v", got, err)
			}
		})
	}
}

// useHeap makes every disjunction of more than one clause a heap.
func useHeap(t *testing.T) {
	t.Helper()
	prev := DisjunctionHeapTakeover
	DisjunctionHeapTakeover = 1
	t.Cleanup(func() { DisjunctionHeapTakeover = prev })
}

func newCovDisjunction(t *testing.T, minMatches int, options search.SearcherOptions,
	children ...search.Searcher) search.Searcher {
	t.Helper()
	s, err := NewDisjunctionSearcher(baseTestIndexReader, children, minMatches, similarity.NewCompositeSumScorer(), options)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func covChildren() []search.Searcher {
	return []search.Searcher{&covSearcher{docs: docs(1, 3, 5)}, &covSearcher{docs: docs(3, 4)}, &covSearcher{docs: docs(5, 6)}}
}

func TestDisjunctionHeapCov(t *testing.T) {
	useHeap(t)
	explain := noExplain()
	explain.Explain = true
	for _, tc := range []struct {
		name    string
		min     int
		options search.SearcherOptions
		want    []uint64
	}{
		{"any", 0, noExplain(), docs(1, 3, 4, 5, 6)},
		{"two", 2, noExplain(), docs(3, 5)},
		{"explain", 0, explain, docs(1, 3, 4, 5, 6)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newCovDisjunction(t, tc.min, tc.options, covChildren()...)
			h, ok := s.(*DisjunctionHeapSearcher)
			if !ok || h.Min() != tc.min || h.Count() != 7 || h.DocumentMatchPoolSize() != 6 {
				t.Fatalf("got %T", s)
			}
			got, err := covDrain(covCtx(), s)
			if err != nil || !slices.Equal(got, tc.want) {
				t.Fatalf("got %v %v", got, err)
			}
		})
	}

	ctx := covCtx()
	s := newCovDisjunction(t, 0, noExplain(), covChildren()...)
	for _, tc := range []struct{ target, want uint64 }{{4, 4}, {5, 5}, {6, 6}} {
		dm, err := s.Advance(ctx, tc.target)
		if err != nil || dm == nil || dm.Number != tc.want {
			t.Fatalf("advance %d: %v %v", tc.target, dm, err)
		}
		if s.Size() <= reflectStaticSizeDisjunctionHeapSearcher {
			t.Fatalf("size %d", s.Size())
		}
	}
	if dm, err := s.Advance(ctx, 7); dm != nil || err != nil {
		t.Fatalf("past end: %v %v", dm, err)
	}

	h := &DisjunctionHeapSearcher{heap: []*searcherCurr{{}, {curr: &search.DocumentMatch{}}}}
	if !h.Less(0, 1) || h.Less(1, 0) {
		t.Fatal("a nil current match sorts first")
	}
}

func TestDisjunctionHeapCovErrors(t *testing.T) {
	useHeap(t)
	tests := []struct {
		name    string
		bad     *covSearcher
		advance bool
	}{
		{name: "init", bad: &covSearcher{docs: docs(1), nextErr: errCov, nextErrAt: 1}},
		{name: "init on advance", bad: &covSearcher{docs: docs(1), nextErr: errCov, nextErrAt: 1}, advance: true},
		{name: "next", bad: &covSearcher{docs: docs(1, 2), nextErr: errCov, nextErrAt: 2}},
		{name: "advance", bad: &covSearcher{docs: docs(1, 2), advErr: errCov}, advance: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newCovDisjunction(t, 0, noExplain(), tc.bad, &covSearcher{docs: docs(1, 2)})
			var err error
			if tc.advance {
				_, err = s.Advance(covCtx(), 2)
			} else {
				_, err = s.Next(covCtx())
			}
			if !errors.Is(err, errCov) {
				t.Fatalf("got %v", err)
			}
		})
	}
	s := newCovDisjunction(t, 0, noExplain(), &covSearcher{closeErr: errCov}, &covSearcher{})
	if err := s.Close(); !errors.Is(err, errCov) {
		t.Fatalf("close: %v", err)
	}

	prev := DisjunctionMaxClauseCount
	DisjunctionMaxClauseCount = 1
	defer func() { DisjunctionMaxClauseCount = prev }()
	if _, err := NewDisjunctionSearcher(baseTestIndexReader, covChildren(), 0, similarity.NewCompositeSumScorer(),
		noExplain()); err == nil {
		t.Fatal("too many clauses")
	}
}

func TestDisjunctionCovOptimizeSingle(t *testing.T) {
	ts := covTerm(newCovBatch(covDocs(1), 0), noExplain())
	heap, err := newDisjunctionHeapSearcher([]search.Searcher{ts}, 0, similarity.NewCompositeSumScorer(), noExplain(), true)
	if err != nil {
		t.Fatal(err)
	}
	slice, err := newDisjunctionSliceSearcher([]search.Searcher{ts}, 0, similarity.NewCompositeSumScorer(), noExplain(), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range []segment.Optimizable{heap, slice} {
		if octx, err := o.Optimize("conjunction", nil); octx == nil || err != nil {
			t.Fatalf("%T: %v %v", o, octx, err)
		}
	}
	plain := []search.Searcher{&covSearcher{}}
	heap, _ = newDisjunctionHeapSearcher(plain, 0, similarity.NewCompositeSumScorer(), noExplain(), true)
	slice, _ = newDisjunctionSliceSearcher(plain, 0, similarity.NewCompositeSumScorer(), noExplain(), true)
	for _, o := range []segment.Optimizable{heap, slice} {
		if octx, err := o.Optimize("conjunction", nil); octx != nil || err != nil {
			t.Fatalf("%T: %v %v", o, octx, err)
		}
	}
}

func TestDisjunctionSliceCovErrors(t *testing.T) {
	ctx := covCtx()
	// a failed init is retried by the next call
	s := newCovDisjunction(t, 0, noExplain(), &covSearcher{docs: docs(1, 2, 3)},
		&covSearcher{nextErr: errCov, nextErrAt: 1})
	for range 2 {
		if _, err := s.Next(ctx); !errors.Is(err, errCov) {
			t.Fatalf("init: %v", err)
		}
	}
	for _, tc := range []struct {
		name    string
		bad     *covSearcher
		advance bool
	}{
		{name: "next", bad: &covSearcher{docs: docs(1, 2), nextErr: errCov, nextErrAt: 2}},
		{name: "init on advance", bad: &covSearcher{docs: docs(1), nextErr: errCov, nextErrAt: 1}, advance: true},
		{name: "advance", bad: &covSearcher{docs: docs(1, 2), advErr: errCov}, advance: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newCovDisjunction(t, 0, noExplain(), tc.bad, &covSearcher{docs: docs(1, 2, 3)})
			var err error
			if tc.advance {
				_, err = s.Advance(covCtx(), 2)
			} else {
				_, err = s.Next(covCtx())
			}
			if !errors.Is(err, errCov) {
				t.Fatalf("got %v", err)
			}
		})
	}
	s = newCovDisjunction(t, 0, noExplain(), &covSearcher{docs: docs(1), closeErr: errCov}, &covSearcher{docs: docs(1, 2)})
	if _, err := s.Next(ctx); err != nil {
		t.Fatal(err)
	}
	if s.Size() <= reflectStaticSizeDisjunctionSliceSearcher {
		t.Fatalf("size %d", s.Size())
	}
	if err := s.Close(); !errors.Is(err, errCov) {
		t.Fatalf("close: %v", err)
	}
	// NextAbove over searchers that are not plain terms is Next
	s = newCovDisjunction(t, 0, noExplain(), covChildren()...)
	if hits, skipped, err := nextAboveAll(t, s, ctx, 100, always); err != nil ||
		!slices.Equal(hits, docs(1, 3, 4, 5, 6)) || skipped != 0 {
		t.Fatalf("hits %v skipped %d err %v", hits, skipped, err)
	}
}

func covTermChildren(lists ...*covPostings) []search.Searcher {
	rv := make([]search.Searcher, len(lists))
	for i, l := range lists {
		rv[i] = covTerm(l, noExplain())
	}
	return rv
}

func TestDisjunctionSliceCovRawStart(t *testing.T) {
	inf := math.Inf(-1)
	for _, tc := range []struct {
		name string
		call func(s search.Searcher, ctx *search.Context) (*search.DocumentMatch, error)
	}{
		{"next", func(s search.Searcher, ctx *search.Context) (*search.DocumentMatch, error) { return s.Next(ctx) }},
		{"advance", func(s search.Searcher, ctx *search.Context) (*search.DocumentMatch, error) {
			return s.Advance(ctx, 0)
		}},
		{"next above", func(s search.Searcher, ctx *search.Context) (*search.DocumentMatch, error) {
			return s.(search.ScoreSkipper).NextAbove(ctx, inf, nil)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := covCtx()
			s := newCovDisjunction(t, 0, noExplain(),
				covTermChildren(&covPostings{docs: covDocs(1, 4)}, &covPostings{nextErr: errCov})...)
			if _, err := tc.call(s, ctx); !errors.Is(err, errCov) {
				t.Fatalf("start: %v", err)
			}
			s = newCovDisjunction(t, 0, noExplain(),
				covTermChildren(&covPostings{docs: covDocs(1, 4, 6)}, &covPostings{docs: covDocs(2, 4)})...)
			dm, err := tc.call(s, ctx)
			if err != nil || dm.Number != 1 {
				t.Fatalf("first: %v %v", dm, err)
			}
			if dm, err = s.Advance(ctx, 3); err != nil || dm.Number != 4 || dm.Score != 2 {
				t.Fatalf("advance: %v %v", dm, err)
			}
			if got, err := covDrain(ctx, s); err != nil || !slices.Equal(got, docs(6)) {
				t.Fatalf("rest: %v %v", got, err)
			}
		})
	}
}

func TestDisjunctionSliceCovRawSeeded(t *testing.T) {
	ctx := covCtx()
	s := newCovDisjunction(t, 0, noExplain(),
		covTermChildren(&covPostings{docs: covDocs(1, 4, 6)}, &covPostings{docs: covDocs(2, 4)}, &covPostings{})...)
	d := s.(*DisjunctionSliceSearcher)
	// children already positioned by the general path seed the raw state
	if err := d.initSearchers(ctx); err != nil {
		t.Fatal(err)
	}
	hits, skipped, err := nextAboveAll(t, d, ctx, 1, always)
	if err != nil || !slices.Equal(hits, docs(4)) || skipped != 3 {
		t.Fatalf("hits %v skipped %d err %v", hits, skipped, err)
	}
}

func TestDisjunctionSliceCovRawErrors(t *testing.T) {
	inf := math.Inf(-1)
	for _, tc := range []struct {
		name   string
		a, b   *covPostings
		target uint64
	}{
		{"advance", &covPostings{docs: covDocs(1, 5)}, &covPostings{docs: covDocs(2, 5), advErr: errCov}, 3},
		{"step", &covPostings{docs: covDocs(1, 5)}, &covPostings{docs: covDocs(1, 5), nextErr: errCov, errAt: 1}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := covCtx()
			s := newCovDisjunction(t, 0, noExplain(), covTermChildren(tc.a, tc.b)...).(*DisjunctionSliceSearcher)
			if err := s.startRaw(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := s.raw.next(ctx, tc.target, inf, nil); !errors.Is(err, errCov) {
				t.Fatalf("got %v", err)
			}
		})
	}
	// a child that runs out on an Advance drops out
	ctx := covCtx()
	s := newCovDisjunction(t, 0, noExplain(),
		covTermChildren(&covPostings{docs: covDocs(1, 9)}, &covPostings{docs: covDocs(2)})...)
	if dm, err := s.Advance(ctx, 5); err != nil || dm.Number != 9 {
		t.Fatalf("advance: %v %v", dm, err)
	}
	// skipped hits, and a skipped that stops the search
	for _, tc := range []struct {
		keep        func(int) bool
		wantHits    []uint64
		wantSkipped int
	}{{always, nil, 3}, {func(int) bool { return false }, nil, 1}} {
		s := newCovDisjunction(t, 0, noExplain(),
			covTermChildren(&covPostings{docs: covDocs(1, 3)}, &covPostings{docs: covDocs(2)})...)
		hits, skipped, err := nextAboveAll(t, s, ctx, 1, tc.keep)
		if err != nil || !slices.Equal(hits, tc.wantHits) || skipped != tc.wantSkipped {
			t.Fatalf("hits %v skipped %d err %v", hits, skipped, err)
		}
	}
}

func TestDisjunctionSliceCovRawBlocks(t *testing.T) {
	floor := covBM25().Score(1, 0)
	evens := []covDoc{{num: 0, freq: 1}, {num: 2, freq: 1}, {num: 4, freq: 1}, {num: 6, freq: 5}}
	odds := []covDoc{{num: 1, freq: 1}, {num: 3, freq: 1}, {num: 5, freq: 1}, {num: 7, freq: 1}}
	same := covBlockDocs(4, 4, 1)
	tests := []struct {
		name        string
		a, b        []covDoc
		blockA      int
		noBitmap    bool
		scorerB     search.Scorer
		boost       float64
		floor       float64
		keep        func(int) bool
		wantHits    []uint64
		wantSkipped int
	}{
		{name: "disjoint", a: evens, b: odds, blockA: 2, floor: floor, keep: always,
			wantHits: docs(6), wantSkipped: 7},
		{name: "disjoint stop", a: evens, b: odds, blockA: 2, floor: floor,
			keep: func(int) bool { return false }, wantSkipped: 3},
		{name: "union", a: same, b: same, blockA: 2, floor: 2 * floor, keep: always, wantSkipped: 4},
		{name: "no bounds", a: evens, b: odds, floor: floor, keep: always, wantHits: docs(6), wantSkipped: 7},
		{name: "no bitmap", a: evens, b: odds, blockA: 2, noBitmap: true, floor: floor, keep: always,
			wantHits: docs(6), wantSkipped: 7},
		{name: "negative bound", a: evens, b: odds, blockA: 2, scorerB: similarity.ConstantScorer(-1),
			floor: floor, keep: always, wantHits: docs(6), wantSkipped: 7},
		{name: "non monotone", a: evens, b: odds, blockA: 2, scorerB: freqScorer{}, floor: floor, keep: always,
			wantHits: docs(1, 3, 5, 6, 7), wantSkipped: 3},
		{name: "negative boost", a: evens, b: odds, blockA: 2, boost: -1, floor: 0, keep: always, wantSkipped: 8},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, b := newCovBatch(tc.a, tc.blockA), newCovBatch(tc.b, 2)
			a.noBitmap = tc.noBitmap
			ta, tb := covTerm(a, noExplain()), covTerm(b, noExplain())
			ta.scorer, tb.scorer = covBM25(), covBM25()
			if tc.scorerB != nil {
				tb.scorer = tc.scorerB
			}
			scorer := similarity.NewCompositeSumScorer()
			if tc.boost != 0 {
				scorer = similarity.NewCompositeSumScorerWithBoost(tc.boost)
			}
			s, err := NewDisjunctionSearcher(baseTestIndexReader, []search.Searcher{ta, tb}, 0, scorer, noExplain())
			if err != nil {
				t.Fatal(err)
			}
			ctx := covCtx()
			ctx.SkipBlocks = true
			hits, skipped, err := nextAboveAll(t, s, ctx, tc.floor, tc.keep)
			if err != nil || !slices.Equal(hits, tc.wantHits) || skipped != tc.wantSkipped {
				t.Fatalf("hits %v skipped %d err %v", hits, skipped, err)
			}
		})
	}
}
