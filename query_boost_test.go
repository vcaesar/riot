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
	"math"
	"sync"
	"testing"
	"time"
)

func boostTestReader(t *testing.T) *Reader {
	t.Helper()
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Errorf("error closing writer: %v", err)
		}
	})
	doc := NewDocument("a").
		AddField(NewTextField("body", "quick brown fox")).
		AddField(NewNumericField("age", 25)).
		AddField(NewDateTimeField("born", time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC))).
		AddField(NewGeoPointField("loc", 10, 10))
	other := NewDocument("b").AddField(NewTextField("body", "lazy dog"))
	batch := NewBatch()
	batch.Update(doc.ID(), doc)
	batch.Update(other.ID(), other)
	if err = w.Batch(batch); err != nil {
		t.Fatal(err)
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Errorf("error closing reader: %v", err)
		}
	})
	return r
}

func topScore(t *testing.T, r *Reader, q Query) float64 {
	t.Helper()
	dmi, err := r.Search(context.Background(), NewTopNSearch(10, q))
	if err != nil {
		t.Fatal(err)
	}
	next, err := dmi.Next()
	if err != nil {
		t.Fatal(err)
	}
	if next == nil {
		t.Fatal("expected a hit")
	}
	return next.Score
}

func TestRangeQueriesApplyBoost(t *testing.T) {
	r := boostTestReader(t)
	tests := []struct {
		name string
		q    Query
	}{
		{"date", NewDateRangeQuery(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{}).
			SetField("born").SetBoost(4)},
		{"numeric", NewNumericRangeQuery(20, 30).SetField("age").SetBoost(4)},
		{"geo bbox", NewGeoBoundingBoxQuery(0, 20, 20, 0).SetField("loc").SetBoost(4)},
		{"geo bbox dateline", NewGeoBoundingBoxQuery(5, 20, -170, 0).SetField("loc").SetBoost(4)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := topScore(t, r, tt.q); got != 4 {
				t.Fatalf("expected score 4, got %f", got)
			}
		})
	}
}

func TestNumericRangeQueryBoostChangeAfterSearch(t *testing.T) {
	r := boostTestReader(t)
	q := NewNumericRangeQuery(20, 30).SetField("age").SetBoost(2)
	if got := topScore(t, r, q); got != 2 {
		t.Fatalf("expected score 2, got %f", got)
	}
	q.SetBoost(3)
	if got := topScore(t, r, q); got != 3 {
		t.Fatalf("expected score 3 after SetBoost, got %f", got)
	}
	if q.scorer != nil {
		t.Fatal("Searcher must not mutate the query")
	}
}

func TestRangeQueriesConcurrentSearch(t *testing.T) {
	r := boostTestReader(t)
	qs := []Query{
		NewNumericRangeQuery(20, 30).SetField("age"),
		NewDateRangeQuery(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{}).SetField("born"),
		NewGeoBoundingBoxQuery(0, 20, 20, 0).SetField("loc"),
	}
	var wg sync.WaitGroup
	for _, q := range qs {
		for range 4 {
			wg.Go(func() {
				dmi, err := r.Search(context.Background(), NewTopNSearch(10, q))
				if err != nil {
					t.Error(err)
					return
				}
				if _, err := dmi.Next(); err != nil {
					t.Error(err)
				}
			})
		}
	}
	wg.Wait()
}

func TestMatchQueryBoostAppliedOnce(t *testing.T) {
	r := boostTestReader(t)
	for _, op := range []MatchQueryOperator{MatchQueryOperatorOr, MatchQueryOperatorAnd} {
		for _, fuzz := range []int{0, 1} {
			base := topScore(t, r, NewMatchQuery("quick fox").SetField("body").
				SetOperator(op).SetFuzziness(fuzz))
			boosted := topScore(t, r, NewMatchQuery("quick fox").SetField("body").
				SetOperator(op).SetFuzziness(fuzz).SetBoost(3))
			if math.Abs(boosted-3*base) > 1e-9 {
				t.Errorf("op %d fuzziness %d: expected %f, got %f", op, fuzz, 3*base, boosted)
			}
		}
	}
}
