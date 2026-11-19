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

	"github.com/RoaringBitmap/roaring/v2"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/similarity"
)

func TestConjunctionCovOptimize(t *testing.T) {
	none := noExplain()
	none.Score = optionScoringNone
	tests := []struct {
		name    string
		options search.SearcherOptions
		optErr  error
		want    []uint64
		wantErr bool
	}{
		{name: "unadorned", options: none, want: []uint64{2, 3}},
		{name: "unadorned error", options: none, optErr: errCov, wantErr: true},
		{name: "push down", options: noExplain(), want: []uint64{2, 3}},
		{name: "push down error", options: noExplain(), optErr: errCov, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, b := newCovBatch(covDocs(1, 2, 3), 0), newCovBatch(covDocs(2, 3, 4), 0)
			b.optErr = tc.optErr
			s, err := NewConjunctionSearcher(baseTestIndexReader,
				[]search.Searcher{covTerm(a, tc.options), covTerm(b, tc.options)},
				similarity.NewCompositeSumScorer(), tc.options)
			if tc.wantErr {
				if !errors.Is(err, errCov) {
					t.Fatalf("got %v", err)
				}
				return
			}
			if _, ok := s.(*TermSearcher); err != nil || !ok {
				t.Fatalf("got %T %v", s, err)
			}
			got, err := covDrain(covCtx(), s)
			if err != nil || !slices.Equal(got, tc.want) {
				t.Fatalf("got %v %v", got, err)
			}
		})
	}
}

func newCovConjunction(t *testing.T, children ...search.Searcher) *ConjunctionSearcher {
	t.Helper()
	s, err := NewConjunctionSearcher(baseTestIndexReader, children, similarity.NewCompositeSumScorer(), noExplain())
	if err != nil {
		t.Fatal(err)
	}
	return s.(*ConjunctionSearcher)
}

func TestConjunctionCovErrors(t *testing.T) {
	ctx := covCtx()
	s := newCovConjunction(t, &covSearcher{docs: []uint64{1}, nextErr: errCov, nextErrAt: 1},
		&covSearcher{docs: []uint64{1, 2}})
	if _, err := s.Next(ctx); !errors.Is(err, errCov) {
		t.Fatalf("init: %v", err)
	}

	// a failed Advance leaves positioned children that Next re-reads
	b := &covSearcher{docs: []uint64{1, 2, 3, 4}, advErr: errCov}
	s = newCovConjunction(t, &covSearcher{docs: []uint64{1, 2, 3}}, b)
	if _, err := s.Advance(ctx, 1); !errors.Is(err, errCov) {
		t.Fatalf("advance: %v", err)
	}
	b.advErr = nil
	if got, err := covDrain(ctx, s); err != nil || !slices.Equal(got, []uint64{2, 3}) {
		t.Fatalf("after failed advance: %v %v", got, err)
	}

	s = newCovConjunction(t, &covSearcher{docs: []uint64{1, 2}},
		&covSearcher{docs: []uint64{1, 2, 3}, nextErr: errCov, nextErrAt: 2})
	if _, err := s.Next(ctx); !errors.Is(err, errCov) {
		t.Fatalf("bump: %v", err)
	}

	s = newCovConjunction(t, &covSearcher{docs: []uint64{1}, closeErr: errCov}, &covSearcher{docs: []uint64{1, 2}})
	if s.Min() != 0 {
		t.Fatal("min")
	}
	if _, err := s.Next(ctx); err != nil {
		t.Fatal(err)
	}
	if s.Size() <= reflectStaticSizeConjunctionSearcher {
		t.Fatalf("size %d", s.Size())
	}
	if err := s.Close(); !errors.Is(err, errCov) {
		t.Fatalf("close: %v", err)
	}
}

func TestConjunctionCovRawEligible(t *testing.T) {
	ts := func(o search.SearcherOptions) search.Searcher { return covTerm(&covPostings{}, o) }
	explain := noExplain()
	explain.Explain = true
	vectors := noExplain()
	vectors.IncludeTermVectors = true
	for _, tc := range []struct {
		name     string
		options  search.SearcherOptions
		scorer   search.CompositeScorer
		children []search.Searcher
		want     bool
	}{
		{"terms", noExplain(), similarity.NewCompositeSumScorer(), []search.Searcher{ts(noExplain())}, true},
		{"explain", explain, similarity.NewCompositeSumScorer(), nil, false},
		{"vectors", vectors, similarity.NewCompositeSumScorer(), nil, false},
		{"scorer", noExplain(), similarity.ConstantScorer(1), nil, false},
		{"not a term", noExplain(), similarity.NewCompositeSumScorer(), []search.Searcher{&covSearcher{}}, false},
		{"term explains", noExplain(), similarity.NewCompositeSumScorer(), []search.Searcher{ts(explain)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := rawTermChildren(tc.options, tc.scorer, tc.children); got != tc.want {
				t.Fatalf("got %v", got)
			}
		})
	}
}

func TestConjunctionCovNextAboveFallback(t *testing.T) {
	ctx := covCtx()
	s := newCovConjunction(t, &covSearcher{docs: []uint64{1, 2}}, &covSearcher{docs: []uint64{2, 3, 4}})
	hits, skipped, err := nextAboveAll(t, s, ctx, 100, always)
	if err != nil || !slices.Equal(hits, []uint64{2}) || skipped != 0 {
		t.Fatalf("hits %v skipped %d err %v", hits, skipped, err)
	}
	s = newCovConjunction(t, covTerm(&covPostings{docs: covDocs(1), nextErr: errCov}, noExplain()),
		covTerm(&covPostings{docs: covDocs(1, 2)}, noExplain()))
	if _, err := s.NextAbove(ctx, 0, nil); !errors.Is(err, errCov) {
		t.Fatalf("init: %v", err)
	}
}

// covBlockDocs returns docs 0..n-1 with freq 1, except freq hi from doc hiFrom on.
func covBlockDocs(n int, hiFrom uint64, hi int) []covDoc {
	rv := make([]covDoc, n)
	for i := range rv {
		rv[i] = covDoc{num: uint64(i), freq: 1}
		if rv[i].num >= hiFrom {
			rv[i].freq = hi
		}
	}
	return rv
}

func TestConjunctionCovRawBlocks(t *testing.T) {
	floor := 2 * covBM25().Score(1, 0)
	shared := roaring.New()
	shared.AddRange(0, 8)
	empty := roaring.New()
	tests := []struct {
		name        string
		block       int
		bm          func(i int) *roaring.Bitmap
		noBitmap    bool
		boost       float64
		keep        func(int) bool
		wantHits    []uint64
		wantSkipped int
	}{
		{name: "windows", block: 2, bm: func(int) *roaring.Bitmap { return shared }, keep: always,
			wantHits: []uint64{6, 7}, wantSkipped: 6},
		{name: "windows stop", block: 2, bm: func(int) *roaring.Bitmap { return shared },
			keep: func(int) bool { return false }, wantSkipped: 2},
		{name: "empty windows", block: 2, bm: func(int) *roaring.Bitmap { return empty }, keep: always,
			wantHits: []uint64{6, 7}},
		{name: "no bounds", block: 0, keep: always, wantHits: []uint64{6, 7}, wantSkipped: 6},
		{name: "no bounds stop", block: 0, keep: func(int) bool { return false }, wantSkipped: 1},
		{name: "no bitmap", block: 2, noBitmap: true, keep: always, wantHits: []uint64{6, 7}, wantSkipped: 6},
		{name: "own bitmaps", block: 2, keep: always, wantHits: []uint64{6, 7}, wantSkipped: 6},
		{name: "negative boost", block: 2, bm: func(int) *roaring.Bitmap { return shared }, boost: -1, keep: always,
			wantSkipped: 8},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			children := make([]search.Searcher, 2)
			for i := range children {
				it := newCovBatch(covBlockDocs(8, 6, 5-4*i), tc.block)
				it.noBitmap, it.noOpt = tc.noBitmap, true
				if tc.bm != nil {
					it.bm = tc.bm(i)
				}
				ts := covTerm(it, noExplain())
				ts.scorer = covBM25()
				children[i] = ts
			}
			s := newCovConjunction(t, children...)
			wantFloor := floor
			if tc.boost != 0 {
				s.scorer = similarity.NewCompositeSumScorerWithBoost(tc.boost)
				wantFloor = 0
			}
			ctx := covCtx()
			ctx.SkipBlocks = true
			hits, skipped, err := nextAboveAll(t, s, ctx, wantFloor, tc.keep)
			if err != nil || !slices.Equal(hits, tc.wantHits) || skipped != tc.wantSkipped {
				t.Fatalf("hits %v skipped %d err %v", hits, skipped, err)
			}
			if !tc.keep(0) {
				return
			}
			if dm, err := s.Next(ctx); dm != nil || err != nil {
				t.Fatalf("next after done: %v %v", dm, err)
			}
		})
	}
}

func TestConjunctionCovRawNonMonotone(t *testing.T) {
	pa, pb := newCovBatch(covDocs(1, 2), 1), newCovBatch(covDocs(1, 2), 1)
	pa.noOpt, pb.noOpt = true, true
	a, b := covTerm(pa, noExplain()), covTerm(pb, noExplain())
	a.scorer = freqScorer{}
	s := newCovConjunction(t, a, b)
	ctx := covCtx()
	ctx.SkipBlocks = true
	if hits, _, err := nextAboveAll(t, s, ctx, 0, always); err != nil || !slices.Equal(hits, []uint64{1, 2}) {
		t.Fatalf("hits %v err %v", hits, err)
	}
	if s.raw.blocks != nil {
		t.Fatal("a non monotone scorer keeps block bounds")
	}
}

func TestConjunctionCovRawStepping(t *testing.T) {
	ctx := covCtx()
	inf := math.Inf(-1)
	// Next and Advance go through the raw state once NextAbove built it
	s := newCovConjunction(t, covTerm(&covPostings{docs: covDocs(1, 3, 5, 7, 9)}, noExplain()),
		covTerm(&covPostings{docs: covDocs(1, 2, 3, 6, 7, 9, 10)}, noExplain()))
	dm, err := s.NextAbove(ctx, inf, nil)
	if err != nil || dm.Number != 1 || dm.Score != 2 {
		t.Fatalf("first: %v %v", dm, err)
	}
	if dm, err = s.Next(ctx); err != nil || dm.Number != 3 {
		t.Fatalf("next: %v %v", dm, err)
	}
	if dm, err = s.Advance(ctx, 8); err != nil || dm.Number != 9 {
		t.Fatalf("advance: %v %v", dm, err)
	}
	if dm, err = s.Next(ctx); err != nil || dm != nil {
		t.Fatalf("end: %v %v", dm, err)
	}

	// NextAbove on an initialized conjunction, one of whose children is empty
	s = newCovConjunction(t, covTerm(&covPostings{}, noExplain()), covTerm(&covPostings{docs: covDocs(1)}, noExplain()))
	if dm, err = s.Next(ctx); err != nil || dm != nil {
		t.Fatalf("empty: %v %v", dm, err)
	}
	if dm, err = s.NextAbove(ctx, inf, nil); err != nil || dm != nil {
		t.Fatalf("empty raw: %v %v", dm, err)
	}

	for _, tc := range []struct {
		name string
		a, b *covPostings
	}{
		{"advance error", &covPostings{docs: covDocs(1, 5)}, &covPostings{docs: covDocs(2, 5), advErr: errCov}},
		{"next error", &covPostings{docs: covDocs(1, 2)}, &covPostings{docs: covDocs(1, 2), nextErr: errCov, errAt: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newCovConjunction(t, covTerm(tc.a, noExplain()), covTerm(tc.b, noExplain()))
			if _, err := s.NextAbove(ctx, inf, nil); !errors.Is(err, errCov) {
				t.Fatalf("got %v", err)
			}
		})
	}
	// a child running out while aligning ends the conjunction
	s = newCovConjunction(t, covTerm(&covPostings{docs: covDocs(1, 5)}, noExplain()),
		covTerm(&covPostings{docs: covDocs(2)}, noExplain()))
	if dm, err = s.NextAbove(ctx, inf, nil); err != nil || dm != nil {
		t.Fatalf("exhausted: %v %v", dm, err)
	}
}
