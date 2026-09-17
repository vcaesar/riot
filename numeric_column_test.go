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
	"math"
	"testing"
	"time"

	"github.com/vcaesar/riot/numeric"
	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/aggregations"
)

func TestTermFieldNumericValue(t *testing.T) {
	if v, ok := NewNumericField("n", 3.5).NumericValue(); !ok || numeric.Int64ToFloat64(v) != 3.5 {
		t.Fatalf("numeric: %d %v", v, ok)
	}
	dt := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	if v, ok := NewDateTimeField("d", dt).NumericValue(); !ok || v != dt.UnixNano() {
		t.Fatalf("date: %d %v", v, ok)
	}
	if _, ok := NewGeoPointField("g", 1, 2).NumericValue(); !ok {
		t.Fatal("geo should be numeric")
	}
	for _, f := range []*TermField{NewKeywordField("k", "x"), NewTextField("t", "x"), NewBooleanField("b", true)} {
		if _, ok := f.NumericValue(); ok {
			t.Fatalf("%s should not be numeric", f.Name())
		}
	}
}

// Stats over numeric columns must equal the row-wise (term) computation,
// across in-memory segments, persisted segments, merges and deletes.
func TestNumericColumnStats(t *testing.T) {
	path := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, path)

	w, err := OpenWriter(DefaultConfig(path))
	if err != nil {
		t.Fatal(err)
	}
	const n = 5000
	var wantSum float64
	cats := map[string]float64{}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	batch := NewBatch()
	for i := 0; i < n; i++ {
		price := float64(i%997) - 100.5
		cat := "c" + string(rune('a'+i%5))
		doc := NewDocument(itoa(i)).
			AddField(NewKeywordField("cat", cat).Aggregatable()).
			AddField(NewNumericField("price", price)).
			AddField(NewNumericField("price", price*2)). // multi-valued
			AddField(NewDateTimeField("when", base.Add(time.Duration(i)*time.Hour))).
			AddField(NewGeoPointField("loc", 1.5, 2.5))
		batch.Update(doc.ID(), doc)
		wantSum += price + price*2
		cats[cat] += price + price*2
		if i%1000 == 999 { // several segments
			if err = w.Batch(batch); err != nil {
				t.Fatal(err)
			}
			batch.Reset()
		}
	}
	// delete a few and adjust expectations
	for i := 0; i < n; i += 100 {
		batch.Delete(Identifier(itoa(i)))
		price := float64(i%997) - 100.5
		wantSum -= price + price*2
		cats["c"+string(rune('a'+i%5))] -= price + price*2
	}
	if err = w.Batch(batch); err != nil {
		t.Fatal(err)
	}
	check := func(t *testing.T, r *Reader) {
		t.Helper()
		req := NewTopNSearch(0, NewMatchAllQuery())
		terms := aggregations.NewTermsAggregation(search.Field("cat"), 10)
		terms.AddAggregation("sum", aggregations.Sum(search.Field("price")))
		req.AddAggregation("cats", terms)
		req.AddAggregation("sum", aggregations.Sum(search.Field("price")))
		req.AddAggregation("min", aggregations.Min(search.Field("price")))
		req.AddAggregation("max", aggregations.Max(search.Field("price")))
		req.AddAggregation("count", aggregations.CountMatches())
		req.AddAggregation("when", aggregations.DateRanges(search.Field("when")).
			AddRange(aggregations.NewDateRange(base, base.Add(1000*time.Hour))))
		dmi, err := r.Search(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		drain(dmi)
		aggs := dmi.Aggregations()
		if aggs.Count() != n-n/100 {
			t.Fatalf("count %d", aggs.Count())
		}
		if got := aggs.Metric("sum"); math.Abs(got-wantSum) > 1e-6 {
			t.Fatalf("sum %v want %v", got, wantSum)
		}
		if got := aggs.Metric("min"); got != -201 { // -100.5 * 2
			t.Fatalf("min %v", got)
		}
		if got := aggs.Metric("max"); got != (996-100.5)*2 {
			t.Fatalf("max %v", got)
		}
		for _, b := range aggs.Buckets("cats") {
			if got := b.Metric("sum"); math.Abs(got-cats[b.Name()]) > 1e-6 {
				t.Fatalf("bucket %s sum %v want %v", b.Name(), got, cats[b.Name()])
			}
		}
		whenBuckets := aggs.Buckets("when")
		if len(whenBuckets) != 1 || whenBuckets[0].Count() != 1000-10 {
			t.Fatalf("date range buckets %v", whenBuckets)
		}
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	check(t, r)
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	// reopen: segments come back from disk (possibly merged)
	w, err = OpenWriter(DefaultConfig(path))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	r, err = w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	check(t, r)

	// sorting and stored-value access on a column field still work
	req := NewTopNSearch(3, NewMatchAllQuery()).SortBy([]string{"-price"})
	dmi, err := r.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var prev = math.Inf(1)
	for m, err := dmi.Next(); m != nil && err == nil; m, err = dmi.Next() {
		var got float64
		if err = m.VisitStoredFields(func(field string, value []byte) bool {
			if field == "price" {
				got, _ = DecodeNumericFloat64(value)
			}
			return true
		}); err != nil {
			t.Fatal(err)
		}
		if got > prev {
			t.Fatalf("not sorted desc: %v after %v", got, prev)
		}
		prev = got
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for ; i > 0; i /= 10 {
		b = append([]byte{byte('0' + i%10)}, b...)
	}
	return string(b)
}
