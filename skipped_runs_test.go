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

package riot

import (
	"context"
	"testing"
)

// TestSkippedRunsKeepStandardAggregations: term, AND and OR searchers
// report losing hits to the collector in runs; a top-1 search with the
// standard aggregations must still count every match and keep the best
// score, as a search whose results hold every hit.
func TestSkippedRunsKeepStandardAggregations(t *testing.T) {
	docs := orSkipDocs(3000)
	term := func(s string) Query { return NewTermQuery(s).SetField("body") }
	queries := map[string]func() Query{
		"term":  func() Query { return term("w3") },
		"dense": func() Query { return term("omni") },
		"and":   func() Query { return NewBooleanQuery().AddMust(term("omni"), term("half")) },
		"or":    func() Query { return NewBooleanQuery().SetMinShould(1).AddShould(term("omni"), term("half")) },
		"or3": func() Query {
			return NewBooleanQuery().SetMinShould(1).AddShould(term("w1"), term("w2"), term("gamma"))
		},
	}
	stats := func(r *Reader, q Query, n int) (count uint64, maxScore float64) {
		t.Helper()
		dmi, err := r.Search(context.Background(), NewTopNSearch(n, q).WithStandardAggregations())
		if err != nil {
			t.Fatal(err)
		}
		for m, err := dmi.Next(); m != nil || err != nil; m, err = dmi.Next() {
			if err != nil {
				t.Fatal(err)
			}
		}
		return dmi.Aggregations().Count(), dmi.Aggregations().Metric("max_score")
	}
	for _, ix := range []struct {
		name    string
		config  Config
		deletes bool
	}{
		{"one segment", InMemoryOnlyConfig().WithBatchIndexing(0), false},
		{"segments with deletes", InMemoryOnlyConfig().WithBatchIndexing(700), true},
	} {
		r := orSkipIndex(t, docs, ix.config, ix.deletes)
		for name, q := range queries {
			wantCount, wantMax := stats(r, q(), len(docs))
			if wantCount == 0 {
				t.Fatalf("%s/%s: no matches", ix.name, name)
			}
			for _, n := range []int{1, 10} {
				if count, maxScore := stats(r, q(), n); count != wantCount || maxScore != wantMax {
					t.Errorf("%s/%s n=%d: count %d, max_score %v; want %d, %v",
						ix.name, name, n, count, maxScore, wantCount, wantMax)
				}
			}
		}
	}
}
