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

package collector

import (
	"context"
	"math"
	"reflect"
	"sort"
	"testing"

	"github.com/vcaesar/riot/numeric"

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

// TestTopNScoreDescFastPath checks the score-only shortcut keeps exactly the
// hits the generic SortOrder.Compare ordering would, including ties, signed
// zeros and NaN.
func TestTopNScoreDescFastPath(t *testing.T) {
	scores := []float64{1, 2, 2, 0, math.Copysign(0, -1), math.NaN(), 3, 2, 1, 5, 5, -1, math.Inf(1), 2, 0, 4, 4, 1}
	order := search.SortOrder{search.SortBy(search.DocumentScore()).Desc()}
	if !order.ScoreDescOnly() {
		t.Fatal("-_score should take the fast path")
	}
	for _, size := range []int{1, 3, 5, 10, 12} {
		matches := make([]*search.DocumentMatch, len(scores))
		want := make([]*search.DocumentMatch, len(scores))
		for i, s := range scores {
			matches[i] = &search.DocumentMatch{Number: uint64(i), Score: s}
			want[i] = &search.DocumentMatch{Number: uint64(i), Score: s, HitNumber: i + 1}
		}
		sort.SliceStable(want, func(i, j int) bool { return order.Compare(want[i], want[j]) < 0 })

		dmi, err := NewTopNCollector(size, 0, order).
			Collect(context.Background(), make(search.Aggregations), &stubSearcher{matches: matches})
		if err != nil {
			t.Fatal(err)
		}
		for i := range size {
			got, err := dmi.Next()
			if err != nil || got == nil {
				t.Fatalf("size %d hit %d: %v, %v", size, i, got, err)
			}
			if got.Number != want[i].Number {
				t.Fatalf("size %d hit %d: doc %d, want %d", size, i, got.Number, want[i].Number)
			}
		}
	}
}

// skipSearcher is a stubSearcher that also implements search.ScoreSkipper.
type skipSearcher struct {
	stubSearcher
	built int
}

func (s *skipSearcher) NextAbove(ctx *search.Context, floor float64,
	skipped func(float64, int) bool) (*search.DocumentMatch, error) {
	for s.index < len(s.matches) {
		m := s.matches[s.index]
		if numeric.Float64ToInt64(m.Score) > numeric.Float64ToInt64(floor) {
			s.built++
			return s.Next(ctx)
		}
		s.index++
		if !skipped(m.Score, 1) {
			return nil, nil
		}
	}
	return nil, nil
}

func collectAll(ctx context.Context, t *testing.T, searcher search.Collectible, size int) ([]uint64, *search.Bucket) {
	t.Helper()
	aggs := make(search.Aggregations)
	aggs.Add("count", aggregations.CountMatches())
	aggs.Add("max_score", aggregations.Max(search.DocumentScore()))
	aggs.Add("duration", aggregations.Duration())
	dmi, err := NewTopNCollector(size, 0, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()}).
		Collect(ctx, aggs, searcher)
	if err != nil {
		t.Fatal(err)
	}
	var docs []uint64
	for {
		m, err := dmi.Next()
		if err != nil {
			t.Fatal(err)
		}
		if m == nil {
			return docs, dmi.Aggregations()
		}
		docs = append(docs, m.Number)
	}
}

func TestTopNScoreSkipperMatchesPlainSearcher(t *testing.T) {
	scores := []float64{3, 1, 4, 1, 5, 9, 2, 6, 5, 3, 5, 8, 9, 7, 9, 3, 2, 3, 8, 4, 6, 2, 6, 4, 3, 3}
	matches := make([]*search.DocumentMatch, len(scores))
	for i, s := range scores {
		matches[i] = &search.DocumentMatch{Number: uint64(i), Score: s}
	}
	for _, size := range []int{0, 1, 3, 10, 30} {
		wantDocs, wantAggs := collectAll(context.Background(), t, &stubSearcher{matches: matches}, size)
		skipper := &skipSearcher{stubSearcher: stubSearcher{matches: matches}}
		gotDocs, gotAggs := collectAll(context.Background(), t, skipper, size)
		if !reflect.DeepEqual(gotDocs, wantDocs) {
			t.Fatalf("size %d: docs %v, want %v", size, gotDocs, wantDocs)
		}
		if gotAggs.Count() != wantAggs.Count() || gotAggs.Metric("max_score") != wantAggs.Metric("max_score") {
			t.Fatalf("size %d: count %d max %v, want %d %v", size, gotAggs.Count(), gotAggs.Metric("max_score"),
				wantAggs.Count(), wantAggs.Metric("max_score"))
		}
		if size > 0 && size < len(scores) && skipper.built >= len(scores)-size {
			t.Fatalf("size %d: built %d matches, the skip path was not used", size, skipper.built)
		}
	}
}

// hitCountAgg reads every DocumentMatch, so it is not score-only.
type hitCountAgg struct{ n *int }

func (a hitCountAgg) Fields() []string              { return nil }
func (a hitCountAgg) Calculator() search.Calculator { return a }
func (a hitCountAgg) Consume(*search.DocumentMatch) { *a.n++ }
func (a hitCountAgg) Finish()                       {}
func (a hitCountAgg) Merge(search.Calculator)       {}

func TestTopNScoreSkipperNotUsedForDocumentAggregations(t *testing.T) {
	matches := makeMatches(50, 1)
	skipper := &skipSearcher{stubSearcher: stubSearcher{matches: matches}}
	var seen int
	aggs := make(search.Aggregations)
	aggs.Add("count", aggregations.CountMatches())
	aggs.Add("hits", hitCountAgg{n: &seen})
	if _, err := NewTopNCollector(5, 0, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()}).
		Collect(context.Background(), aggs, skipper); err != nil {
		t.Fatal(err)
	}
	if skipper.built != 0 || seen != len(matches) {
		t.Fatalf("built %d via skip path, aggregation saw %d of %d hits", skipper.built, seen, len(matches))
	}
}

func TestTopNScoreSkipperHonorsCancel(t *testing.T) {
	matches := makeMatches(5*CheckDoneEvery, 1)
	ctx, cancel := context.WithCancel(context.Background())
	skipper := &skipSearcher{stubSearcher: stubSearcher{matches: matches}}
	// cancel once the results are full so only the skip loop can notice
	cancelAfter := &cancelAfterSearcher{skipSearcher: skipper, cancel: cancel, after: 11}
	_, err := NewTopNCollector(10, 0, search.SortOrder{search.SortBy(search.DocumentScore()).Desc()}).
		Collect(ctx, make(search.Aggregations), cancelAfter)
	if err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if skipper.index == len(matches) {
		t.Fatalf("skip loop kept going after cancel")
	}
}

type cancelAfterSearcher struct {
	*skipSearcher
	cancel func()
	after  int
}

func (c *cancelAfterSearcher) Next(ctx *search.Context) (*search.DocumentMatch, error) {
	if c.index+1 >= c.after {
		c.cancel()
	}
	return c.skipSearcher.Next(ctx)
}
