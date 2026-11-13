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
	"net"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/aggregations"
)

func TestWithoutDocValuesOptions(t *testing.T) {
	ipv4, err := NewIPv4Field("v4", net.IPv4(10, 0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []*TermField{
		NewNumericField("n", 1), NewDateTimeField("d", time.Unix(1, 0)), NewGeoPointField("g", 1, 2),
		NewBooleanField("b", true), NewIPField("ip", net.IPv4(10, 0, 0, 1)),
		NewKeywordField("k", "x").Sortable().Aggregatable(), NewTextField("t", "x").Sortable(), ipv4.Sortable(),
	} {
		if !f.IndexDocValues() {
			t.Fatalf("%s: no doc values before", f.Name())
		}
		opts := f.FieldOptions
		if f.StoreValue().WithoutDocValues().IndexDocValues() {
			t.Fatalf("%s: doc values after WithoutDocValues", f.Name())
		}
		if want := (opts | Store) &^ (Sortable | Aggregatable); f.FieldOptions != want {
			t.Fatalf("%s: options %b want %b", f.Name(), f.FieldOptions, want)
		}
		if !f.Sortable().IndexDocValues() {
			t.Fatalf("%s: Sortable after WithoutDocValues", f.Name())
		}
	}
}

// Fields of every type with WithoutDocValues still match queries, and
// have nothing to sort or aggregate on, mixed with segments that have them.
func TestWithoutDocValuesSearch(t *testing.T) {
	w := openConfigWriter(t, InMemoryOnlyConfig())
	defer func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	}()
	const n = 200
	for _, off := range []bool{true, false} {
		b := NewBatch()
		for i := range n {
			id := i
			if !off {
				id += n
			}
			fields := []*TermField{
				NewNumericField("n", float64(id)), NewBooleanField("b", id%2 == 0),
				NewIPField("ip", net.IPv4(10, 0, byte(id>>8), byte(id))),
				NewDateTimeField("d", time.Unix(int64(id), 0)),
			}
			d := NewDocument(strconv.Itoa(id))
			for _, f := range fields {
				if off {
					f.WithoutDocValues()
				}
				d.AddField(f)
			}
			b.Insert(d)
		}
		if err := w.Batch(b); err != nil {
			t.Fatal(err)
		}
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	}()
	all := make([]int, 0, 2*n)
	for i := range 2 * n {
		all = append(all, i)
	}
	if got := rangeIDs(t, r, NewNumericRangeQuery(0, 2*n).SetField("n")); !slices.Equal(got, all) {
		t.Fatalf("numeric range %d docs", len(got))
	}
	if got := rangeIDs(t, r, NewDateRangeQuery(time.Unix(0, 0), time.Unix(2*n, 0)).SetField("d")); !slices.Equal(got, all) {
		t.Fatalf("date range %d docs", len(got))
	}
	if got := rangeIDs(t, r, NewBooleanFieldQuery(true).SetField("b")); len(got) != n {
		t.Fatalf("bool query %d docs", len(got))
	}
	if got := rangeIDs(t, r, NewIPRangeQuery("10.0.0.0/8").SetField("ip")); !slices.Equal(got, all) {
		t.Fatalf("ip range %d docs", len(got))
	}

	req := NewTopNSearch(0, NewMatchAllQuery())
	req.AddAggregation("b", aggregations.NewTermsAggregation(search.Field("b"), 10))
	req.AddAggregation("sum", aggregations.Sum(search.Field("n")))
	it, err := r.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if err := drain(it); err != nil {
		t.Fatal(err)
	}
	// only the segment with doc values contributes
	if got, want := it.Aggregations().Metric("sum"), float64(n*n+n*(n-1)/2); got != want {
		t.Fatalf("sum %v want %v", got, want)
	}
	for _, bk := range it.Aggregations().Buckets("b") {
		if bk.Count() != n/2 {
			t.Fatalf("bucket %s count %d want %d", bk.Name(), bk.Count(), n/2)
		}
	}
	// documents without doc values sort as missing: last by default
	hits := collectHits(t, r, NewTopNSearch(2*n, NewMatchAllQuery()).SortBy([]string{"-n"}))
	if id := matchID(t, hits[0]); id != 2*n-1 {
		t.Fatalf("first sorted %d", id)
	}
	if id := matchID(t, hits[n]); id >= n {
		t.Fatalf("doc %d without doc values sorted among those with", id)
	}
}

// Text fields are sortable and aggregatable on their analyzed terms: a
// document sorts by its smallest term, in both directions, and terms
// buckets count the analyzed words.
func TestTextFieldSortable(t *testing.T) {
	w := openConfigWriter(t, InMemoryOnlyConfig())
	defer func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	}()
	b := NewBatch()
	for i, s := range []string{"Zebra crossing", "apple Pie", "mango", "Banana split"} {
		b.Insert(NewDocument(strconv.Itoa(i)).AddField(NewTextField("t", s).Sortable().Aggregatable()))
	}
	b.Insert(NewDocument("4").AddField(NewTextField("t", "kiwi"))) // no doc values asked, gets them: same segment
	if err := w.Batch(b); err != nil {
		t.Fatal(err)
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	}()
	order := func(sortBy string) []int {
		var ids []int
		for _, m := range collectHits(t, r, NewTopNSearch(10, NewMatchAllQuery()).SortBy([]string{sortBy})) {
			ids = append(ids, matchID(t, m))
		}
		return ids
	}
	// smallest terms: crossing, apple, mango, banana, kiwi
	if got := order("t"); !slices.Equal(got, []int{1, 3, 0, 4, 2}) {
		t.Fatalf("asc %v", got)
	}
	// descending reverses the same smallest terms
	if got := order("-t"); !slices.Equal(got, []int{2, 4, 0, 3, 1}) {
		t.Fatalf("desc %v", got)
	}
	req := NewTopNSearch(0, NewMatchAllQuery())
	req.AddAggregation("t", aggregations.NewTermsAggregation(search.Field("t"), 20))
	it, err := r.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if err := drain(it); err != nil {
		t.Fatal(err)
	}
	var words []string
	for _, bk := range it.Aggregations().Buckets("t") {
		words = append(words, bk.Name())
	}
	slices.Sort(words)
	if want := []string{"apple", "banana", "crossing", "kiwi", "mango", "pie", "split", "zebra"}; !slices.Equal(words, want) {
		t.Fatalf("buckets %v", words)
	}
	if got := rangeIDs(t, r, NewMatchQuery("kiwi").SetField("t")); !slices.Equal(got, []int{4}) {
		t.Fatalf("kiwi %v", got)
	}
}

func matchID(t *testing.T, m *search.DocumentMatch) int {
	t.Helper()
	id := -1
	if err := m.VisitStoredFields(func(f string, v []byte) bool {
		if f != "_id" {
			return true
		}
		var err error
		if id, err = strconv.Atoi(string(v)); err != nil {
			t.Fatal(err)
		}
		return false
	}); err != nil {
		t.Fatal(err)
	}
	return id
}
