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
	"slices"
	"testing"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/similarity"
)

// freqScorer scores a posting its frequency; it is not known to be monotone.
type freqScorer struct{}

func (freqScorer) Score(freq int, _ float64) float64 { return float64(freq) }
func (freqScorer) Explain(freq int, _ float64) *search.Explanation {
	return search.NewExplanation(float64(freq), "freq")
}

// covBM25 is a monotone scorer; with norm 0 it grows with freq only.
func covBM25() *similarity.BM25Scorer {
	return similarity.NewBM25Scorer(1, 1.2, 0.75, 1, &search.Explanation{Value: 1})
}

func noExplain() search.SearcherOptions {
	o := testSearchOptions
	o.Explain = false
	return o
}

func TestTermSearcherCovErrors(t *testing.T) {
	r := newCovReader()
	r.postErr["boom"] = errCov
	if _, err := NewTermSearcher(r, "boom", "desc", 1, nil, testSearchOptions); !errors.Is(err, errCov) {
		t.Fatalf("postings error: %v", err)
	}
	r = newCovReader()
	r.statsErr = errCov
	if _, err := NewTermSearcher(r, "beer", "desc", 1, nil, testSearchOptions); !errors.Is(err, errCov) {
		t.Fatalf("stats error: %v", err)
	}

	ctx := covCtx()
	ts := covTerm(&covPostings{docs: covDocs(1), nextErr: errCov}, testSearchOptions)
	if _, err := ts.Next(ctx); !errors.Is(err, errCov) {
		t.Fatalf("next error: %v", err)
	}
	ts = covTerm(&covPostings{docs: covDocs(1), advErr: errCov}, testSearchOptions)
	if _, err := ts.Advance(ctx, 1); !errors.Is(err, errCov) {
		t.Fatalf("advance error: %v", err)
	}
	if dm, err := covTerm(&covPostings{}, testSearchOptions).Advance(ctx, 1); dm != nil || err != nil {
		t.Fatalf("advance past end: %v %v", dm, err)
	}
}

func TestTermSearcherCovMisc(t *testing.T) {
	ts := covTerm(&covPostings{docs: covDocs(1, 2)}, testSearchOptions)
	if ts.Size() <= 0 || ts.Min() != 0 || ts.Count() != 2 {
		t.Fatalf("size %d min %d count %d", ts.Size(), ts.Min(), ts.Count())
	}
	if octx, err := ts.Optimize("conjunction", nil); octx != nil || err != nil {
		t.Fatalf("optimize on a plain reader: %v %v", octx, err)
	}
	if octx, err := covTerm(newCovBatch(nil, 0), testSearchOptions).Optimize("conjunction", nil); octx == nil || err != nil {
		t.Fatalf("optimize on an optimizable reader: %v %v", octx, err)
	}
	// once the constant score shortcut counted every hit, Next is done
	ctx := covCtx()
	ts = covTerm(&covPostings{docs: covDocs(1, 2)}, noExplain())
	if dm, err := ts.NextAbove(ctx, 1, func(float64, int) bool { return true }); dm != nil || err != nil {
		t.Fatalf("next above: %v %v", dm, err)
	}
	if dm, err := ts.Next(ctx); dm != nil || err != nil {
		t.Fatalf("next after done: %v %v", dm, err)
	}
	p := &batchPosting{}
	p.SetNumber(7)
	if p.Number() != 7 || p.Locations() != nil || p.Size() != 0 {
		t.Fatalf("batch posting: %+v", p)
	}
	for _, tc := range []struct {
		name   string
		scorer search.Scorer
		want   bool
	}{
		{"bm25", covBM25(), true},
		{"constant", similarity.ConstantScorer(2), true},
		{"other", freqScorer{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := monotoneScorer(tc.scorer); got != tc.want {
				t.Fatalf("monotoneScorer = %v", got)
			}
		})
	}
}

// nextAboveAll calls NextAbove until it returns nil, collecting the hits
// and the skipped counts.
func nextAboveAll(t *testing.T, s search.Searcher, ctx *search.Context, floor float64,
	keep func(n int) bool) (hits []uint64, skipped int, err error) {
	t.Helper()
	sk := s.(search.ScoreSkipper)
	for {
		dm, err := sk.NextAbove(ctx, floor, func(_ float64, n int) bool {
			skipped += n
			return keep(skipped)
		})
		if err != nil || dm == nil {
			return hits, skipped, err
		}
		hits = append(hits, dm.Number)
		ctx.DocumentMatchPool.Put(dm)
	}
}

func always(int) bool { return true }

func TestTermSearcherCovNextAbove(t *testing.T) {
	mixed := []covDoc{{num: 1, freq: 1}, {num: 2, freq: 1}, {num: 3, freq: 5}, {num: 4, freq: 5}, {num: 5, freq: 1}}
	tests := []struct {
		name        string
		it          func() *covPostings
		batch       bool
		block       int
		scorer      search.Scorer
		explain     bool
		skipBlocks  bool
		floor       float64
		keep        func(int) bool
		wantHits    []uint64
		wantSkipped int
		wantErr     bool
	}{
		{name: "explain", it: func() *covPostings { return &covPostings{docs: covDocs(1, 2)} },
			scorer: similarity.ConstantScorer(1), explain: true, floor: 5, keep: always, wantHits: []uint64{1, 2}},
		{name: "constant losing", it: func() *covPostings { return &covPostings{docs: covDocs(1, 2, 3)} },
			scorer: similarity.ConstantScorer(1), floor: 1, keep: always, wantSkipped: 3},
		{name: "constant winning", it: func() *covPostings { return &covPostings{docs: covDocs(1, 2)} },
			scorer: similarity.ConstantScorer(1), floor: 0, keep: always, wantHits: []uint64{1, 2}},
		{name: "plain skip", it: func() *covPostings { return &covPostings{docs: mixed} },
			scorer: freqScorer{}, floor: 2, keep: always, wantHits: []uint64{3, 4}, wantSkipped: 3},
		{name: "plain stop", it: func() *covPostings { return &covPostings{docs: mixed} },
			scorer: freqScorer{}, floor: 2, keep: func(int) bool { return false }, wantSkipped: 1},
		{name: "plain error", it: func() *covPostings { return &covPostings{docs: mixed, nextErr: errCov, errAt: 1} },
			scorer: freqScorer{}, floor: 2, keep: always, wantSkipped: 1, wantErr: true},
		{name: "batch skip", it: func() *covPostings { return &covPostings{docs: mixed} }, batch: true,
			scorer: freqScorer{}, floor: 2, keep: always, wantHits: []uint64{3, 4}, wantSkipped: 3},
		{name: "batch stop", it: func() *covPostings { return &covPostings{docs: mixed} }, batch: true,
			scorer: freqScorer{}, floor: 2, keep: func(int) bool { return false }, wantSkipped: 1},
		{name: "blocks", it: func() *covPostings { return &covPostings{docs: mixed} }, batch: true, block: 2,
			scorer: covBM25(), skipBlocks: true, floor: covBM25().Score(1, 0), keep: always,
			wantHits: []uint64{3, 4}, wantSkipped: 3},
		{name: "blocks stop", it: func() *covPostings { return &covPostings{docs: mixed} }, batch: true, block: 2,
			scorer: covBM25(), skipBlocks: true, floor: covBM25().Score(1, 0),
			keep: func(int) bool { return false }, wantSkipped: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			options := noExplain()
			options.Explain = tc.explain
			var ts *TermSearcher
			if tc.batch {
				ts = covTerm(&covBatch{covPostings: tc.it(), block: tc.block}, options)
			} else {
				ts = covTerm(tc.it(), options)
			}
			ts.scorer = tc.scorer
			ctx := covCtx()
			ctx.SkipBlocks = tc.skipBlocks
			hits, skipped, err := nextAboveAll(t, ts, ctx, tc.floor, tc.keep)
			if (err != nil) != tc.wantErr || !slices.Equal(hits, tc.wantHits) || skipped != tc.wantSkipped {
				t.Fatalf("hits %v skipped %d err %v", hits, skipped, err)
			}
			if err := ts.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTermSearcherCovBatchErrors(t *testing.T) {
	ctx := covCtx()
	ctx.SkipBlocks = true
	for _, tc := range []struct {
		name string
		it   *covBatch
	}{
		{"skip", &covBatch{covPostings: &covPostings{docs: covDocs(1)}, block: 1, skipErr: errCov}},
		{"fill", &covBatch{covPostings: &covPostings{docs: covDocs(1)}, batchErr: errCov}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := covTerm(tc.it, noExplain())
			ts.scorer = covBM25()
			if _, err := ts.NextAbove(ctx, 0, func(float64, int) bool { return true }); !errors.Is(err, errCov) {
				t.Fatalf("got %v", err)
			}
		})
	}
	// a total below the postings already read is not trusted
	ts := covTerm(&covPostings{docs: covDocs(1, 2), count: func() uint64 { return 0 }}, noExplain())
	if dm, err := ts.Next(ctx); err != nil || dm.Number != 1 {
		t.Fatalf("next: %v %v", dm, err)
	}
	hits, skipped, err := nextAboveAll(t, ts, ctx, 1, always)
	if err != nil || hits != nil || skipped != 1 {
		t.Fatalf("hits %v skipped %d err %v", hits, skipped, err)
	}
	// a cursor that was advanced counts no remaining hits from the total
	ts = covTerm(newCovBatch(covDocs(1, 2, 3, 4), 0), noExplain())
	if dm, err := ts.Advance(ctx, 2); err != nil || dm.Number != 2 {
		t.Fatalf("advance: %v %v", dm, err)
	}
	hits, skipped, err = nextAboveAll(t, ts, ctx, 1, always)
	if err != nil || hits != nil || skipped != 2 {
		t.Fatalf("hits %v skipped %d err %v", hits, skipped, err)
	}
}

// Advance drains the buffered batch first, then seeks the reader.
func TestTermSearcherCovAdvanceBatch(t *testing.T) {
	docs := make([]uint64, 40)
	for i := range docs {
		docs[i] = uint64(2 * i)
	}
	ts := covTerm(newCovBatch(covDocs(docs...), 0), noExplain())
	ts.scorer = freqScorer{}
	ctx := covCtx()
	skip := func(float64, int) bool { return true }
	dm, err := ts.NextAbove(ctx, 0, skip)
	if err != nil || dm.Number != 0 {
		t.Fatalf("first: %v %v", dm, err)
	}
	ts.cursor.startBatch(ts.reader, false) // already batching: no-op
	for _, tc := range []struct{ target, want uint64 }{{5, 6}, {30, 30}, {60, 60}} {
		dm, err = ts.Advance(ctx, tc.target)
		if err != nil || dm == nil || dm.Number != tc.want {
			t.Fatalf("advance %d: %v %v", tc.target, dm, err)
		}
	}
	if dm, err = ts.Advance(ctx, 1000); dm != nil || err != nil {
		t.Fatalf("past end: %v %v", dm, err)
	}
}
