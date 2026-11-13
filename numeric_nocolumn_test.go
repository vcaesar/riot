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
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/aggregations"
)

func TestWithoutNumericColumnField(t *testing.T) {
	for _, f := range []*TermField{
		NewNumericField("n", 3.5),
		NewDateTimeField("d", time.Unix(5, 0)),
		NewGeoPointField("g", 1, 2),
	} {
		if _, ok := f.WithoutNumericColumn().NumericValue(); ok {
			t.Fatalf("%s still has a numeric column value", f.Name())
		}
		if v, err := DecodeNumericFloat64(f.Value()); err != nil || math.IsNaN(v) {
			t.Fatalf("%s value changed: %v %v", f.Name(), v, err)
		}
	}
	if f := NewKeywordField("k", "x").WithoutNumericColumn(); f.noColumn {
		t.Fatal("keyword field marked")
	}
	f := NewNumericField("n", 3.25).WithoutNumericColumn()
	f.Analyze(0)
	if _, ok := f.AnalyzedTokenFrequencies()["3.25"]; !ok {
		t.Fatal("decimal term lost")
	}
}

// Fields without a numeric column, mixed with segments that have one,
// still answer ranges, sorts and aggregations, before and after reopen.
func TestWithoutNumericColumnSearch(t *testing.T) {
	path := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, path)
	w := openConfigWriter(t, DefaultConfig(path))
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	value := func(i int) float64 { return float64(i-1500) / 4 }
	const n = 3000
	var wantSum float64
	for lo := 0; lo < n; lo += 1000 {
		b := NewBatch()
		for i := lo; i < lo+1000; i++ {
			v := NewNumericField("v", value(i))
			d := NewDateTimeField("d", base.Add(time.Duration(i)*time.Minute))
			if lo != 1000 { // the middle segment keeps its columns
				v.WithoutNumericColumn()
				d.WithoutNumericColumn()
			}
			b.Insert(NewDocument(strconv.Itoa(i)).AddField(v).AddField(d))
			wantSum += value(i)
		}
		if err := w.Batch(b); err != nil {
			t.Fatal(err)
		}
	}
	check := func(r *Reader) {
		t.Helper()
		for _, c := range [][2]float64{{-375, 374.75}, {-10, 10}, {0, 0}, {200, 9999}} {
			var want []int
			for i := range n {
				if v := value(i); v >= c[0] && v <= c[1] {
					want = append(want, i)
				}
			}
			q := NewNumericRangeInclusiveQuery(c[0], c[1], true, true).SetField("v")
			if got := rangeIDs(t, r, q); !slices.Equal(got, want) {
				t.Fatalf("range %v: %d docs want %d", c, len(got), len(want))
			}
		}
		got := rangeIDs(t, r, NewDateRangeQuery(base.Add(990*time.Minute), base.Add(2010*time.Minute)).SetField("d"))
		if len(got) != 1020 || got[0] != 990 || got[len(got)-1] != 2009 {
			t.Fatalf("date range %d docs", len(got))
		}
		if got := rangeIDs(t, r, NewTermQuery("2.5").SetField("v")); !slices.Equal(got, []int{1510}) {
			t.Fatalf("decimal term %v", got)
		}

		req := NewTopNSearch(3, NewMatchAllQuery()).SortBy([]string{"-v"})
		req.AddAggregation("sum", aggregations.Sum(search.Field("v")))
		req.AddAggregation("when", aggregations.DateRanges(search.Field("d")).
			AddRange(aggregations.NewDateRange(base, base.Add(1500*time.Minute))))
		it, err := r.Search(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		var top []string
		for m, err := it.Next(); m != nil || err != nil; m, err = it.Next() {
			if err != nil {
				t.Fatal(err)
			}
			if err = m.VisitStoredFields(func(f string, v []byte) bool {
				if f == "_id" {
					top = append(top, string(v))
				}
				return true
			}); err != nil {
				t.Fatal(err)
			}
		}
		if !slices.Equal(top, []string{"2999", "2998", "2997"}) {
			t.Fatalf("sort -v top %v", top)
		}
		if got := it.Aggregations().Metric("sum"); math.Abs(got-wantSum) > 1e-6 {
			t.Fatalf("sum %v want %v", got, wantSum)
		}
		if b := it.Aggregations().Buckets("when"); len(b) != 1 || b[0].Count() != 1500 {
			t.Fatalf("date range buckets %v", b)
		}
		if err := r.VisitNumbers("v", func(uint64, float64) error { return nil }); err == nil {
			t.Fatal("VisitNumbers read a field without a numeric column")
		}
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	check(r)
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}

	w = openConfigWriter(t, DefaultConfig(path))
	defer func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	}()
	if r, err = w.Reader(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	}()
	check(r)
}

func TestNumericFieldDecimalTermFormat(t *testing.T) {
	for _, c := range []struct {
		fmt  byte
		prec int
		v    float64
		want string
	}{
		{'f', -1, 3.25, "3.25"},
		{'f', 3, 3.25, "3.250"},
		{'e', -1, 1e21, "1e+21"},
		{'g', -1, 0.0000001, "1e-07"},
	} {
		f := NewNumericField("n", c.v).WithDecimalTermFormat(c.fmt, c.prec)
		f.Analyze(0)
		if _, ok := f.AnalyzedTokenFrequencies()[c.want]; !ok || f.AnalyzedLength() != 2 {
			t.Fatalf("%c %d %v: terms %v", c.fmt, c.prec, c.v, f.AnalyzedTokenFrequencies())
		}
		// the token stream path (with positions) formats the same way
		toks := f.analyzer.Analyze(f.Value())
		if len(toks) != 2 || string(toks[1].Term) != c.want {
			t.Fatalf("%c %d %v: tokens %v", c.fmt, c.prec, c.v, toks)
		}
	}
	if NewNumericField("n", 1).WithDecimalTermFormat('f', 2).analyzer == numberAnalyzer {
		t.Fatal("shared analyzer reused for a custom format")
	}
	off := NewNumericField("n", 1).WithoutDecimalTerm()
	off.WithDecimalTermFormat('f', 2)
	if off.analyzer.(*numericAnalyzer).emitOriginal {
		t.Fatal("format re-enabled the decimal term")
	}
	if d := NewDateTimeField("d", time.Unix(1, 0)).WithDecimalTermFormat('f', 2); d.analyzer != dateTimeAnalyzer {
		t.Fatal("date field got a decimal term")
	}

	w := openConfigWriter(t, InMemoryOnlyConfig())
	defer func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	}()
	b := NewBatch()
	b.Insert(NewDocument("1").AddField(NewNumericField("v", 3.25).WithDecimalTermFormat('f', 3)))
	b.Insert(NewDocument("2").AddField(NewNumericField("v", 3.25)))
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
	if got := rangeIDs(t, r, NewTermQuery("3.250").SetField("v")); !slices.Equal(got, []int{1}) {
		t.Fatalf("3.250 matched %v", got)
	}
	if got := rangeIDs(t, r, NewTermQuery("3.25").SetField("v")); !slices.Equal(got, []int{2}) {
		t.Fatalf("3.25 matched %v", got)
	}
	if got := rangeIDs(t, r, NewNumericRangeQuery(3, 4).SetField("v")); !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("range matched %v", got)
	}
}
