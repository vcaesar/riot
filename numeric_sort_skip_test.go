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
	"fmt"
	"slices"
	"strconv"
	"testing"

	"github.com/vcaesar/riot/search"
)

// A numeric first sort skips hits below the evicted floor without loading
// their values; hits, ties (by doc order), missing values and the total
// must match a field sort that loads every value.
func TestNumericSortSkip(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	writer, err := OpenWriter(DefaultConfig(tmpIndexPath))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	const n = 3000
	b := NewBatch()
	for i := range n {
		doc := NewDocument(strconv.Itoa(i))
		if i%97 != 0 { // some docs miss the field
			// negative, fractional and tied values
			doc.AddField(NewNumericField("price", float64(i*7919%500-250)/4))
		}
		if i%89 == 0 { // some docs hold two values
			doc.AddField(NewNumericField("price", float64(i%13)))
		}
		b.Update(doc.ID(), doc)
		if (i+1)%1000 == 0 { // several segments
			if err = writer.Batch(b); err != nil {
				t.Fatal(err)
			}
			b = NewBatch()
		}
	}
	reader, err := writer.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()

	run := func(src search.TextValueSource, desc, missingFirst bool, size, from int) ([]string, uint64) {
		s := search.SortBy(src)
		if desc {
			s.Desc()
		}
		if missingFirst {
			s.MissingFirst()
		}
		req := NewTopNSearch(size, NewMatchAllQuery()).SetFrom(from).
			SortByCustom(search.SortOrder{s}).WithStandardAggregations()
		dmi, err := reader.Search(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		next, err := dmi.Next()
		for err == nil && next != nil {
			err = next.VisitStoredFields(func(field string, value []byte) bool {
				if field == "_id" {
					ids = append(ids, string(value))
				}
				return true
			})
			if err == nil {
				next, err = dmi.Next()
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		return ids, dmi.Aggregations().Count()
	}
	for _, desc := range []bool{false, true} {
		for _, missingFirst := range []bool{false, true} {
			for _, size := range []int{1, 10, 50} {
				name := fmt.Sprintf("desc=%v,missingFirst=%v,size=%d", desc, missingFirst, size)
				got, total := run(search.Field("price"), desc, missingFirst, size, 5)
				want, wantTotal := run(plainField("price"), desc, missingFirst, size, 5)
				if total != n || wantTotal != n {
					t.Fatalf("%s: total %d/%d, want %d", name, total, wantTotal, n)
				}
				if !slices.Equal(got, want) || len(got) != size {
					t.Fatalf("%s: got %v want %v", name, got, want)
				}
			}
		}
	}
}

// A virtual field sharing the sort field's name makes the sort key the
// virtual term, not the column value, so the skip must not use the column.
func TestNumericSortSkipVirtualField(t *testing.T) {
	cfg := InMemoryOnlyConfig().WithVirtualField(NewNumericField("price", -1))
	writer, err := OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	b := NewBatch()
	for i := range 100 {
		doc := NewDocument(strconv.Itoa(i)).
			AddField(NewNumericField("price", float64(i))).
			AddField(NewKeywordField("rank", fmt.Sprintf("%03d", i)).Sortable())
		b.Update(doc.ID(), doc)
	}
	if err = writer.Batch(b); err != nil {
		t.Fatal(err)
	}
	reader, err := writer.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()

	order := search.SortOrder{search.SortBy(search.Field("price")), search.SortBy(search.Field("rank")).Desc()}
	dmi, err := reader.Search(context.Background(), NewTopNSearch(1, NewMatchAllQuery()).SortByCustom(order))
	if err != nil {
		t.Fatal(err)
	}
	next, err := dmi.Next()
	if err != nil || next == nil {
		t.Fatalf("next: %v %v", next, err)
	}
	var id string
	if err = next.VisitStoredFields(func(field string, value []byte) bool {
		if field == "_id" {
			id = string(value)
		}
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if id != "99" {
		t.Fatalf("top hit %q, want 99", id)
	}
}

// plainField is a FieldSource without NumericFieldSource.
type plainField string

func (f plainField) Fields() []string { return []string{string(f)} }

func (f plainField) Value(match *search.DocumentMatch) []byte {
	return search.Field(string(f)).Value(match)
}

func TestSortOrderNumericFirst(t *testing.T) {
	if f, desc, ok := (search.SortOrder{search.SortBy(search.Field("p")).Desc()}).NumericFirst(); !ok || f != "p" || !desc {
		t.Fatalf("field sort: %q %v %v", f, desc, ok)
	}
	for _, o := range []search.SortOrder{nil, {search.SortBy(plainField("p"))}, {search.SortBy(&search.ScoreSource{})}} {
		if _, _, ok := o.NumericFirst(); ok {
			t.Fatalf("%v: numeric first", o)
		}
	}
}
