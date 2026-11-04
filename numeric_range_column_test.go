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
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vcaesar/riot/numeric"
	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/aggregations"
	"github.com/vcaesar/riot/search/searcher"
)

func rangeIDs(t *testing.T, r *Reader, q Query) []int {
	t.Helper()
	it, err := r.Search(context.Background(), NewAllMatches(q))
	if err != nil {
		t.Fatal(err)
	}
	var ids []int
	for m, err := it.Next(); m != nil || err != nil; m, err = it.Next() {
		if err != nil {
			t.Fatal(err)
		}
		var id int
		if err := m.VisitStoredFields(func(f string, v []byte) bool {
			if f == "_id" {
				id, err = strconv.Atoi(string(v))
				return false
			}
			return true
		}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// Numeric and date ranges, served from the numeric column, must match a
// brute-force filter across several segments, deletes and a persisted index.
func TestNumericRangeFromColumnMatchesOracle(t *testing.T) {
	path := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, path)
	w, err := OpenWriter(DefaultConfig(path))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	value := func(i int) float64 { return float64(i-1500) / 4 } // negatives and fractions
	const n = 3000
	for lo := 0; lo < n; lo += 1000 { // three segments
		b := NewBatch()
		for i := lo; i < lo+1000; i++ {
			b.Insert(NewDocument(strconv.Itoa(i)).
				AddField(NewNumericField("v", value(i))).
				AddField(NewDateTimeField("d", base.Add(time.Duration(i)*time.Minute))))
		}
		if err := w.Batch(b); err != nil {
			t.Fatal(err)
		}
	}
	deleted := map[int]bool{}
	del := NewBatch()
	for i := 7; i < n; i += 97 {
		del.Delete(Identifier(strconv.Itoa(i)))
		deleted[i] = true
	}
	if err := w.Batch(del); err != nil {
		t.Fatal(err)
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	oracle := func(keep func(i int) bool) []int {
		var ids []int
		for i := range n {
			if !deleted[i] && keep(i) {
				ids = append(ids, i)
			}
		}
		return ids
	}
	for _, c := range []struct {
		lo, hi       float64
		incLo, incHi bool
	}{
		{-375, 374.75, true, true}, {-10, 10, true, true}, {-10, 10, false, false},
		{0, 0, true, true}, {0.25, 0.5, false, true}, {200, 9999, true, true},
		{-9999, -374, true, false}, {5, 4, true, true}, {MinNumeric, 0, true, true},
	} {
		q := NewNumericRangeInclusiveQuery(c.lo, c.hi, c.incLo, c.incHi).SetField("v")
		want := oracle(func(i int) bool {
			v := value(i)
			return (v > c.lo || c.incLo && v == c.lo) && (v < c.hi || c.incHi && v == c.hi)
		})
		if got := rangeIDs(t, r, q); !slices.Equal(got, want) {
			t.Fatalf("range %+v: %d docs want %d", c, len(got), len(want))
		}
	}
	start, end := base.Add(990*time.Minute), base.Add(2010*time.Minute)
	got := rangeIDs(t, r, NewDateRangeQuery(start, end).SetField("d"))
	want := oracle(func(i int) bool { return i >= 990 && i < 2010 })
	if !slices.Equal(got, want) {
		t.Fatalf("date range: %d docs want %d", len(got), len(want))
	}
	// an exact term query on the shift-0 term still finds the value
	if got := rangeIDs(t, r, NewTermQuery("2.5").SetField("v")); !slices.Equal(got, oracle(func(i int) bool { return value(i) == 2.5 })) {
		t.Fatalf("original value term: %v", got)
	}
}

// VisitNumbers scans live column values only, across segments and deletes,
// and reports global doc numbers that VisitStoredFields resolves.
func TestReaderVisitNumbers(t *testing.T) {
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for lo := 0; lo < 2000; lo += 1000 {
		b := NewBatch()
		for i := lo; i < lo+1000; i++ {
			d := NewDocument(strconv.Itoa(i))
			if i%10 != 3 { // some docs lack the field
				d.AddField(NewNumericField("v", float64(i)/2))
			}
			b.Insert(d)
		}
		if err := w.Batch(b); err != nil {
			t.Fatal(err)
		}
	}
	del := NewBatch()
	del.Delete(Identifier("5"))
	del.Delete(Identifier("1500"))
	if err := w.Batch(del); err != nil {
		t.Fatal(err)
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var wantSum float64
	wantN := 0
	for i := range 2000 {
		if i%10 != 3 && i != 5 && i != 1500 {
			wantSum += float64(i) / 2
			wantN++
		}
	}
	var sum float64
	n := 0
	err = r.VisitNumbers("v", func(doc uint64, v float64) error {
		if err := r.VisitStoredFields(doc, func(f string, val []byte) bool {
			if f == "_id" {
				id, _ := strconv.Atoi(string(val))
				if float64(id)/2 != v {
					t.Errorf("doc %d id %d value %v", doc, id, v)
				}
				return false
			}
			return true
		}); err != nil {
			return err
		}
		sum += v
		n++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != wantN || sum != wantSum {
		t.Fatalf("n=%d sum=%v want %d %v", n, sum, wantN, wantSum)
	}
	if err := r.VisitNumbers("missing", func(uint64, float64) error { t.Fatal("visited"); return nil }); err != nil {
		t.Fatal(err)
	}
	// the block scan yields exactly the per-value scan
	type pair struct {
		doc uint64
		v   float64
	}
	var perValue, perBlock []pair
	if err := r.VisitNumbers("v", func(doc uint64, v float64) error {
		perValue = append(perValue, pair{doc, v})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.VisitNumberBlocks("v", func(base uint64, docs []uint64, vals []float64) error {
		if (docs != nil && len(docs) != len(vals)) || len(vals) == 0 {
			t.Fatalf("block docs %d vals %d", len(docs), len(vals))
		}
		for i := range vals {
			doc := base + uint64(i)
			if docs != nil {
				doc = docs[i]
			}
			perBlock = append(perBlock, pair{doc, vals[i]})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(perValue, perBlock) {
		t.Fatalf("block scan %d values differ from per-value scan %d", len(perBlock), len(perValue))
	}
	var intN int
	if err := r.VisitInt64Blocks("v", func(_ uint64, _ []uint64, vals []int64) error {
		for _, v := range vals {
			if numeric.Int64ToFloat64(v) != perValue[intN].v {
				t.Fatalf("int block value %d", intN)
			}
			intN++
		}
		return nil
	}); err != nil || intN != len(perValue) {
		t.Fatalf("int blocks n=%d err=%v", intN, err)
	}
}

// Phrase matching reads locations whose varints are one byte (fast path)
// and several bytes (positions and offsets past 127) alike.
func TestPhraseLongDocumentLocations(t *testing.T) {
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	filler := strings.Repeat("filler ", 300)
	bodies := []string{
		"search engine",               // short: one-byte varints
		filler + "search engine",      // positions/offsets > 127
		filler + "engine search",      // out of order: no phrase
		"search " + filler + "engine", // far apart: no phrase
	}
	b := NewBatch()
	for i, body := range bodies {
		b.Insert(NewDocument(strconv.Itoa(i)).AddField(NewTextField("body", body).SearchTermPositions()))
	}
	if err := w.Batch(b); err != nil {
		t.Fatal(err)
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if got := rangeIDs(t, r, NewMatchPhraseQuery("search engine").SetField("body")); !slices.Equal(got, []int{0, 1}) {
		t.Fatalf("phrase docs %v want [0 1]", got)
	}
}

// Once the top-N is full, phrase hits below the floor are only checked for
// an occurrence; docs holding both terms out of order must not be counted.
func TestPhraseCountBelowFloorChecksOccurrence(t *testing.T) {
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	b := NewBatch()
	want := 0
	for i := range 500 {
		body := "search engine fast"
		switch i % 3 {
		case 1:
			body = "engine search fast" // both terms, no phrase
		case 2:
			body = "search fast engine search engine" // phrase, longer doc
		}
		if i%3 != 1 {
			want++
		}
		b.Insert(NewDocument(strconv.Itoa(i)).AddField(NewTextField("body", body).SearchTermPositions()))
	}
	if err := w.Batch(b); err != nil {
		t.Fatal(err)
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	q := NewMatchPhraseQuery("search engine").SetField("body")
	req := NewTopNSearch(3, q)
	req.AddAggregation("count", aggregations.CountMatches())
	it, err := r.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	hits := 0
	for m, err := it.Next(); m != nil || err != nil; m, err = it.Next() {
		if err != nil {
			t.Fatal(err)
		}
		if len(m.Locations) == 0 && len(m.FieldTermLocations) == 0 {
			t.Fatal("top hit without phrase locations")
		}
		hits++
	}
	if got := it.Aggregations().Count(); got != uint64(want) || hits != 3 {
		t.Fatalf("count=%d hits=%d want %d 3", got, hits, want)
	}
	if got := rangeIDs(t, r, q); len(got) != want {
		t.Fatalf("all matches %d want %d", len(got), want)
	}
}

// A conjunction where one term's postings are a subset of the other's keeps
// the subset term on its own postings; hits, counts and scores must match
// the unoptimized per-term check, with and without deletes.
func TestConjunctionSubsetTermKeepsResults(t *testing.T) {
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	b := NewBatch()
	for i := range 2000 {
		body := "common filler"
		if i%16 == 7 {
			body = "common rare rare"
		}
		b.Insert(NewDocument(strconv.Itoa(i)).AddField(NewTextField("body", body)))
	}
	if err := w.Batch(b); err != nil {
		t.Fatal(err)
	}
	check := func(deleted map[int]bool) {
		r, err := w.Reader()
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		q := NewBooleanQuery().
			AddMust(NewTermQuery("common").SetField("body")).
			AddMust(NewTermQuery("rare").SetField("body"))
		var want []int
		for i := range 2000 {
			if i%16 == 7 && !deleted[i] {
				want = append(want, i)
			}
		}
		if got := rangeIDs(t, r, q); !slices.Equal(got, want) {
			t.Fatalf("AND: %d docs want %d", len(got), len(want))
		}
		req := NewTopNSearch(5, q)
		req.AddAggregation("count", aggregations.CountMatches())
		it, err := r.Search(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		var first float64
		for m, err := it.Next(); m != nil || err != nil; m, err = it.Next() {
			if err != nil {
				t.Fatal(err)
			}
			if first == 0 {
				first = m.Score
			} else if m.Score != first {
				t.Fatalf("equal docs scored %v and %v", first, m.Score)
			}
		}
		if got := it.Aggregations().Count(); got != uint64(len(want)) || first <= 0 {
			t.Fatalf("count %d want %d, score %v", got, len(want), first)
		}
	}
	check(nil)
	del := NewBatch()
	del.Delete(Identifier("7"))
	del.Delete(Identifier("8"))
	if err := w.Batch(del); err != nil {
		t.Fatal(err)
	}
	check(map[int]bool{7: true})
}

// Term vectors, which a search with locations asks for, must not keep a
// numeric range off the column: numeric terms carry no locations, and the
// term path would OR one term searcher per distinct value in the range.
func TestNumericRangeWithTermVectorsUsesColumn(t *testing.T) {
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	b := NewBatch()
	for i := range 2000 {
		b.Insert(NewDocument(strconv.Itoa(i)).AddField(NewNumericField("v", float64(i))))
	}
	if err := w.Batch(b); err != nil {
		t.Fatal(err)
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	q := NewNumericRangeQuery(10, 1500).SetField("v")
	s, err := q.Searcher(r.reader, search.SearcherOptions{IncludeTermVectors: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.(*searcher.TermSearcher); !ok {
		t.Fatalf("range with term vectors searched by %T, want the column's *searcher.TermSearcher", s)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	it, err := r.Search(context.Background(), NewAllMatches(q).IncludeLocations())
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for m, err := it.Next(); m != nil || err != nil; m, err = it.Next() {
		if err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n != 1490 {
		t.Fatalf("%d hits with locations, want 1490", n)
	}
}

// A numeric range matches as the exact term does when the numeric column
// covers other documents than the indexed terms of the field.
func TestNumericRangeColumnAgreesWithTerms(t *testing.T) {
	for _, batch := range []bool{false, true} {
		for _, mixed := range []bool{false, true} {
			config := InMemoryOnlyConfig()
			if batch {
				config = config.WithBatchIndexing(0)
			}
			w := openConfigWriter(t, config)
			b := NewBatch()
			f := NewNumericField("v", 5)
			f.FieldOptions = Sortable | Aggregatable
			if mixed {
				b.Insert(NewDocument("0").AddField(NewNumericField("v", 5)))
				f.FieldOptions = Index
			}
			b.Insert(NewDocument("1").AddField(f))
			if err := w.Batch(b); err != nil {
				t.Fatal(err)
			}
			r, err := w.Reader()
			if err != nil {
				t.Fatal(err)
			}
			term := rangeIDs(t, r, NewTermQuery("5").SetField("v"))
			got := rangeIDs(t, r, NewNumericRangeQuery(4, 6).SetField("v"))
			if !slices.Equal(got, term) {
				t.Errorf("batch=%v mixed=%v: range %v, term %v", batch, mixed, got, term)
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
}
