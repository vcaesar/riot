//  Copyright (c) 2020 Couchbase, Inc.
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

package collector

import (
	"context"
	"math/rand"
	"testing"

	"github.com/vcaesar/riot/search/aggregations"

	"github.com/vcaesar/riot/search"
)

type createCollector func() search.Collector

var extResults []*search.DocumentMatch

func benchHelper(numOfMatches int, cc createCollector, b *testing.B) {
	matches := make([]*search.DocumentMatch, 0, numOfMatches)
	for i := 0; i < numOfMatches; i++ {
		matches = append(matches, &search.DocumentMatch{
			Number: uint64(i),
			Score:  rand.Float64(),
		})
	}

	b.ResetTimer()

	for run := 0; run < b.N; run++ {
		searcher := &stubSearcher{
			matches: matches,
		}
		collector := cc()
		aggs := make(search.Aggregations)
		aggs.Add("count", aggregations.CountMatches())
		aggs.Add("max_score", aggregations.Max(search.DocumentScore()))
		dmi, err := collector.Collect(context.Background(), aggs, searcher)
		if err != nil {
			b.Fatal(err)
		}

		var results search.DocumentMatchCollection
		result, err := dmi.Next()
		for result != nil && err == nil {
			results = append(results, &search.DocumentMatch{
				Number: result.Number,
				Score:  result.Score,
			})
			result, err = dmi.Next()
		}
		extResults = results
		if err != nil {
			b.Fatalf("error advancing document match iterator: %v", err)
		}
	}
}
