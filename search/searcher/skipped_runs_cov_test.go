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
	"slices"
	"testing"

	"github.com/vcaesar/riot/search"
)

// TestSkippedRunsCov: term, AND and OR searchers hand losing hits to
// skipped in runs under SkipBlocks, each bounded by its best score and by
// maxSkippedRun, flushed before a hit is returned, and one at a time
// otherwise; a skipped that says stop ends the search at that report.
func TestSkippedRunsCov(t *testing.T) {
	const losers = maxSkippedRun + 100
	// losers, one winning hit, then a tail of losers
	list := covBlockDocs(losers+4, uint64(losers), 5)
	list[losers+1].freq, list[losers+2].freq, list[losers+3].freq = 1, 1, 1
	total := losers + 3
	child := func() *TermSearcher {
		it := newCovBatch(slices.Clone(list), 0)
		it.noOpt = true
		ts := covTerm(it, noExplain())
		ts.scorer = freqScorer{} // not monotone: no block bounds
		return ts
	}
	for _, tc := range []struct {
		name     string
		searcher func() search.Searcher
		floor    float64
		maxRun   int
		runs     []int // nil: only bounded by maxRun
	}{
		{"term", func() search.Searcher { return child() }, 1, postingsBatchSize, nil},
		{"and", func() search.Searcher { return newCovConjunction(t, child(), child()) }, 2, maxSkippedRun,
			[]int{maxSkippedRun, 100, 3}},
		{"or", func() search.Searcher { return newCovDisjunction(t, 0, noExplain(), child(), child()) }, 2,
			maxSkippedRun, []int{maxSkippedRun, 100, 3}},
	} {
		run := func(skipBlocks bool, stopAt int) (hits []uint64, runs []int) {
			t.Helper()
			ctx := covCtx()
			ctx.SkipBlocks = skipBlocks
			sk, skipped := tc.searcher().(search.ScoreSkipper), 0
			for {
				dm, err := sk.NextAbove(ctx, tc.floor, func(bound float64, n int) bool {
					if bound != tc.floor {
						t.Errorf("%s: run bound %v, want %v", tc.name, bound, tc.floor)
					}
					runs = append(runs, n)
					skipped += n
					return skipped != stopAt
				})
				if err != nil {
					t.Fatal(err)
				}
				if dm == nil {
					return hits, runs
				}
				hits = append(hits, dm.Number)
				ctx.DocumentMatchPool.Put(dm)
			}
		}
		sum := func(runs []int) (n int) {
			for _, k := range runs {
				n += k
			}
			return n
		}
		t.Run(tc.name+"/runs", func(t *testing.T) {
			hits, runs := run(true, -1)
			if !slices.Equal(hits, docs(uint64(losers))) || sum(runs) != total || slices.Max(runs) > tc.maxRun ||
				(tc.runs != nil && !slices.Equal(runs, tc.runs)) {
				t.Fatalf("hits %v, %d skipped in runs %v", hits, sum(runs), runs)
			}
		})
		t.Run(tc.name+"/one by one", func(t *testing.T) {
			hits, runs := run(false, -1)
			if !slices.Equal(hits, docs(uint64(losers))) || len(runs) != total || slices.Max(runs) != 1 {
				t.Fatalf("hits %v, %d reports for %d skipped", hits, len(runs), sum(runs))
			}
			if hits, runs = run(false, 1); hits != nil || !slices.Equal(runs, []int{1}) {
				t.Fatalf("stop: hits %v runs %v", hits, runs)
			}
		})
		t.Run(tc.name+"/stop", func(t *testing.T) {
			_, all := run(true, -1)
			// at the first full run, and at the flush before the hit
			for _, stopAt := range []int{all[0], losers} {
				hits, runs := run(true, stopAt)
				if hits != nil || sum(runs) != stopAt {
					t.Fatalf("stop at %d: hits %v, %d skipped in runs %v", stopAt, hits, sum(runs), runs)
				}
			}
		})
	}
}
