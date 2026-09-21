//  Copyright (c) 2026 The Riot Authors.
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
	"errors"
	"fmt"
	"math/rand"
	"testing"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/aggregations"
)

// Measures "SELECT category, count(*), sum(price), avg(price) GROUP BY category"
// style stats over the doc-values (column) store, per-hit path.
func benchStatsIndex(b *testing.B, n int) *Reader {
	b.Helper()
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := w.Close(); err != nil {
			b.Error(err)
		}
	})
	rnd := rand.New(rand.NewSource(1))
	batch := NewBatch()
	for i := 0; i < n; i++ {
		doc := NewDocument(fmt.Sprintf("%d", i)).
			AddField(NewKeywordField("category", fmt.Sprintf("cat%d", rnd.Intn(16))).Aggregatable()).
			AddField(NewNumericField("price", float64(rnd.Intn(10000))).Aggregatable())
		batch.Update(doc.ID(), doc)
		if (i+1)%10000 == 0 {
			if err = w.Batch(batch); err != nil {
				b.Fatal(err)
			}
			batch.Reset()
		}
	}
	if n%10000 != 0 {
		if err = w.Batch(batch); err != nil {
			b.Fatal(err)
		}
	}
	r, err := w.Reader()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := r.Close(); err != nil {
			b.Error(err)
		}
	})
	return r
}

func BenchmarkStatsGroupBy(b *testing.B) {
	const n = 200_000
	r := benchStatsIndex(b, n)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := NewTopNSearch(0, NewMatchAllQuery())
		terms := aggregations.NewTermsAggregation(search.Field("category"), 16)
		terms.AddAggregation("sum", aggregations.Sum(search.Field("price")))
		terms.AddAggregation("avg", aggregations.Avg(search.Field("price")))
		req.AddAggregation("cats", terms)
		req.AddAggregation("count", aggregations.CountMatches())
		dmi, err := r.Search(context.Background(), req)
		if err != nil {
			b.Fatal(err)
		}
		if err := drain(dmi); err != nil {
			b.Fatal(err)
		}
		if c := dmi.Aggregations().Count(); c != n {
			b.Fatalf("count %d != %d", c, n)
		}
	}
	b.ReportMetric(float64(n)*float64(b.N)/b.Elapsed().Seconds()/1e6, "Mdocs/s")
}

func BenchmarkStatsSumOnly(b *testing.B) {
	const n = 200_000
	r := benchStatsIndex(b, n)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := NewTopNSearch(0, NewMatchAllQuery())
		req.AddAggregation("sum", aggregations.Sum(search.Field("price")))
		dmi, err := r.Search(context.Background(), req)
		if err != nil {
			b.Fatal(err)
		}
		if err := drain(dmi); err != nil {
			b.Fatal(err)
		}
		if dmi.Aggregations().Metric("sum") <= 0 {
			b.Fatal("no sum")
		}
	}
	b.ReportMetric(float64(n)*float64(b.N)/b.Elapsed().Seconds()/1e6, "Mdocs/s")
}

type drainMockIterator struct {
	matches  int
	err      error
	errMatch *search.DocumentMatch
	calls    int
}

func (m *drainMockIterator) Next() (*search.DocumentMatch, error) {
	m.calls++
	if m.calls <= m.matches {
		return &search.DocumentMatch{}, nil
	}
	return m.errMatch, m.err
}

func (m *drainMockIterator) Aggregations() *search.Bucket { return nil }

func TestDrain(t *testing.T) {
	wantErr := errors.New("iterator failed")
	for _, tt := range []struct {
		name     string
		matches  int
		err      error
		errMatch *search.DocumentMatch
	}{
		{name: "empty"},
		{name: "matches", matches: 2},
		{name: "immediate error", err: wantErr},
		{name: "error after matches", matches: 2, err: wantErr},
		{name: "match with error", err: wantErr, errMatch: &search.DocumentMatch{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mock := &drainMockIterator{matches: tt.matches, err: tt.err, errMatch: tt.errMatch}
			if err := drain(mock); !errors.Is(err, tt.err) {
				t.Fatalf("drain error %v, want %v", err, tt.err)
			}
			if mock.calls != tt.matches+1 {
				t.Fatalf("Next calls %d, want %d", mock.calls, tt.matches+1)
			}
		})
	}
}

func drain(dmi search.DocumentMatchIterator) error {
	for {
		m, err := dmi.Next()
		if err != nil {
			return err
		}
		if m == nil {
			return nil
		}
	}
}
