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
	"math"
	"testing"

	"github.com/vcaesar/riot/search/aggregations"

	"github.com/vcaesar/riot/search"
)

func makeMatches(n int, score float64) (rv []*search.DocumentMatch) {
	for i := 1; i <= n; i++ {
		rv = append(rv, &search.DocumentMatch{
			Number: uint64(i),
			Score:  score,
		})
	}
	return rv
}

func TestTop10Scores(t *testing.T) {
	matches := makeMatches(14, 11)
	for i, match := range matches {
		if i%2 != 0 && i < 9 {
			match.Score = 9
		}
	}
	matches[11].Score = 99
	searcher := &stubSearcher{
		matches: matches,
	}

	aggs := make(search.Aggregations)
	aggs.Add("count", aggregations.CountMatches())
	aggs.Add("max_score", aggregations.Max(search.DocumentScore()))

	collector := NewTopNCollector(10, 0, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()})
	dmi, err := collector.Collect(context.Background(), aggs, searcher)
	if err != nil {
		t.Fatal(err)
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
	if err != nil {
		t.Fatalf("error advancing document match iterator: %v", err)
	}

	total, maxScore := getTotalHitsMaxScore(dmi.Aggregations())
	if maxScore != 99.0 {
		t.Errorf("expected max score 99.0, got %f", maxScore)
	}

	if total != 14 {
		t.Errorf("expected 14 total results, got %d", total)
	}

	if len(results) != 10 {
		t.Logf("results: %v", results)
		t.Fatalf("expected 10 results, got %d", len(results))
	}

	if results[0].Number != 12 {
		t.Errorf("expected first result to have number 12, got %d", results[0].Number)
	}

	if results[0].Score != 99.0 {
		t.Errorf("expected highest score to be 99.0, got %f", results[0].Score)
	}

	minScore := 1000.0
	for _, result := range results {
		if result.Score < minScore {
			minScore = result.Score
		}
	}

	if minScore < 10 {
		t.Errorf("expected minimum score to be higher than 10, got %f", minScore)
	}
}

func getTotalHitsMaxScore(bucket *search.Bucket) (total int, topScore float64) {
	total = int(bucket.Aggregations()["count"].(search.MetricCalculator).Value())
	topScore = bucket.Aggregations()["max_score"].(search.MetricCalculator).Value()
	if math.IsInf(topScore, -1) {
		topScore = 0
	}
	return total, topScore
}

func TestTop10ScoresSkip10(t *testing.T) {
	matches := makeMatches(14, 11)
	for i, match := range matches {
		if i%2 != 0 && i < 9 {
			match.Score = 9
		}
	}
	matches[1].Score = 9.5
	matches[11].Score = 99
	searcher := &stubSearcher{
		matches: matches,
	}

	aggs := make(search.Aggregations)
	aggs.Add("count", aggregations.CountMatches())
	aggs.Add("max_score", aggregations.Max(search.DocumentScore()))

	collector := NewTopNCollector(10, 10, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()})
	dmi, err := collector.Collect(context.Background(), aggs, searcher)
	if err != nil {
		t.Fatal(err)
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
	if err != nil {
		t.Fatalf("error advancing document match iterator: %v", err)
	}

	total, maxScore := getTotalHitsMaxScore(dmi.Aggregations())
	if maxScore != 99.0 {
		t.Errorf("expected max score 99.0, got %f", maxScore)
	}
	if total != 14 {
		t.Errorf("expected 14 total results, got %d", total)
	}

	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	if results[0].Number != 2 {
		t.Errorf("expected first result to have number 2, got %d", results[0].Number)
	}

	if results[0].Score != 9.5 {
		t.Errorf("expected highest score to be 9.5, got %f", results[0].Score)
	}
}

func TestTop10ScoresSkip10Only9Hits(t *testing.T) {
	matches := makeMatches(9, 11)
	searcher := &stubSearcher{
		matches: matches,
	}

	aggs := make(search.Aggregations)
	aggs.Add("count", aggregations.CountMatches())
	aggs.Add("max_score", aggregations.Max(search.DocumentScore()))

	collector := NewTopNCollector(10, 10, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()})
	dmi, err := collector.Collect(context.Background(), aggs, searcher)
	if err != nil {
		t.Fatal(err)
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
	if err != nil {
		t.Fatalf("error advancing document match iterator: %v", err)
	}

	total, _ := getTotalHitsMaxScore(dmi.Aggregations())
	if total != 9 {
		t.Errorf("expected 9 total results, got %d", total)
	}

	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestPaginationSameScores(t *testing.T) {
	matches := makeMatches(14, 5)
	searcher := &stubSearcher{
		matches: matches,
	}

	aggs := make(search.Aggregations)
	aggs.Add("count", aggregations.CountMatches())
	aggs.Add("max_score", aggregations.Max(search.DocumentScore()))

	// first get first 5 hits
	collector := NewTopNCollector(5, 0, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()})
	dmi, err := collector.Collect(context.Background(), aggs, searcher)
	if err != nil {
		t.Fatal(err)
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
	if err != nil {
		t.Fatalf("error advancing document match iterator: %v", err)
	}

	total, _ := getTotalHitsMaxScore(dmi.Aggregations())
	if total != 14 {
		t.Errorf("expected 14 total results, got %d", total)
	}

	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}

	firstResults := make(map[uint64]struct{})
	for _, hit := range results {
		firstResults[hit.Number] = struct{}{}
	}

	searcher = &stubSearcher{
		matches: matches,
	}

	// now get next 5 hits
	collector = NewTopNCollector(5, 5, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()})
	dmi, err = collector.Collect(context.Background(), aggs, searcher)
	if err != nil {
		t.Fatal(err)
	}

	results = results[:0]
	result, err = dmi.Next()
	for result != nil && err == nil {
		results = append(results, &search.DocumentMatch{
			Number: result.Number,
			Score:  result.Score,
		})
		result, err = dmi.Next()
	}
	if err != nil {
		t.Fatalf("error advancing document match iterator: %v", err)
	}

	total, _ = getTotalHitsMaxScore(dmi.Aggregations())
	if total != 14 {
		t.Errorf("expected 14 total results, got %d", total)
	}

	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}

	// make sure that none of these hits repeat ones we saw in the top 5
	for _, hit := range results {
		if _, ok := firstResults[hit.Number]; ok {
			t.Errorf("doc number %d is in top 5 and next 5 result sets", hit.Number)
		}
	}
}

func BenchmarkTop10of0Scores(b *testing.B) {
	benchHelper(0, func() search.Collector {
		return NewTopNCollector(10, 0, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()})
	}, b)
}

func BenchmarkTop10of3Scores(b *testing.B) {
	benchHelper(3, func() search.Collector {
		return NewTopNCollector(10, 0, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()})
	}, b)
}

func BenchmarkTop10of10Scores(b *testing.B) {
	benchHelper(10, func() search.Collector {
		return NewTopNCollector(10, 0, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()})
	}, b)
}

func BenchmarkTop10of25Scores(b *testing.B) {
	benchHelper(25, func() search.Collector {
		return NewTopNCollector(10, 0, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()})
	}, b)
}

func BenchmarkTop10of50Scores(b *testing.B) {
	benchHelper(50, func() search.Collector {
		return NewTopNCollector(10, 0, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()})
	}, b)
}

func BenchmarkTop10of10000Scores(b *testing.B) {
	benchHelper(10000, func() search.Collector {
		return NewTopNCollector(10, 0, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()})
	}, b)
}

func BenchmarkTop100of0Scores(b *testing.B) {
	benchHelper(0, func() search.Collector {
		return NewTopNCollector(100, 0, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()})
	}, b)
}

func BenchmarkTop100of3Scores(b *testing.B) {
	benchHelper(3, func() search.Collector {
		return NewTopNCollector(100, 0, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()})
	}, b)
}

func BenchmarkTop100of10Scores(b *testing.B) {
	benchHelper(10, func() search.Collector {
		return NewTopNCollector(100, 0, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()})
	}, b)
}

func BenchmarkTop100of25Scores(b *testing.B) {
	benchHelper(25, func() search.Collector {
		return NewTopNCollector(100, 0, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()})
	}, b)
}

func BenchmarkTop100of50Scores(b *testing.B) {
	benchHelper(50, func() search.Collector {
		return NewTopNCollector(100, 0, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()})
	}, b)
}

func BenchmarkTop100of10000Scores(b *testing.B) {
	benchHelper(10000, func() search.Collector {
		return NewTopNCollector(100, 0, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()})
	}, b)
}

func BenchmarkTop1000of10000Scores(b *testing.B) {
	benchHelper(10000, func() search.Collector {
		return NewTopNCollector(1000, 0, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()})
	}, b)
}

func BenchmarkTop10000of100000Scores(b *testing.B) {
	benchHelper(100000, func() search.Collector {
		return NewTopNCollector(10000, 0, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()})
	}, b)
}

func BenchmarkTop10of100000Scores(b *testing.B) {
	benchHelper(100000, func() search.Collector {
		return NewTopNCollector(10, 0, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()})
	}, b)
}

func BenchmarkTop100of100000Scores(b *testing.B) {
	benchHelper(100000, func() search.Collector {
		return NewTopNCollector(100, 0, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()})
	}, b)
}

func BenchmarkTop1000of100000Scores(b *testing.B) {
	benchHelper(100000, func() search.Collector {
		return NewTopNCollector(1000, 0, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()})
	}, b)
}

func BenchmarkTop10000of1000000Scores(b *testing.B) {
	benchHelper(1000000, func() search.Collector {
		return NewTopNCollector(10000, 0, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()})
	}, b)
}
