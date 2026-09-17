//  Copyright (c) 2026 The Bluge Authors.
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
	rnd := rand.New(rand.NewSource(1))
	batch := NewBatch()
	for i := 0; i < n; i++ {
		doc := NewDocument(fmt.Sprintf("%d", i)).
			AddField(NewKeywordField("category", fmt.Sprintf("cat%d", rnd.Intn(16))).Aggregatable()).
			AddField(NewNumericField("price", float64(rnd.Intn(10000))).Aggregatable())
		batch.Update(doc.ID(), doc)
		if i%10000 == 0 {
			if err = w.Batch(batch); err != nil {
				b.Fatal(err)
			}
			batch.Reset()
		}
	}
	if err = w.Batch(batch); err != nil {
		b.Fatal(err)
	}
	r, err := w.Reader()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = r.Close(); _ = w.Close() })
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
		drain(dmi)
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
		drain(dmi)
		if dmi.Aggregations().Metric("sum") <= 0 {
			b.Fatal("no sum")
		}
	}
	b.ReportMetric(float64(n)*float64(b.N)/b.Elapsed().Seconds()/1e6, "Mdocs/s")
}

func drain(dmi search.DocumentMatchIterator) {
	for m, err := dmi.Next(); m != nil && err == nil; m, err = dmi.Next() {
		_ = m
	}
}
