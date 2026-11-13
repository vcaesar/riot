//go:build !race

// Copyright (c) 2026 The AtomAI.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package riot

import (
	"context"
	"testing"

	"github.com/vcaesar/riot/search/aggregations"
)

// A rare term top 10 with a count on one segment has a small fixed
// allocation cost per search (33 before the collector, bucket, context and
// stats were folded together).
func TestRareTermSearchAllocations(t *testing.T) {
	w := openConfigWriter(t, InMemoryOnlyConfig().WithBatchIndexing(0))
	b := NewBatch()
	for i := range 5000 {
		b.Insert(streamTestDoc(i))
	}
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
	req := NewTopNSearch(10, NewTermQuery("term42").SetField("body"))
	req.AddAggregation("count", aggregations.CountMatches())
	search := func() {
		it, err := r.Search(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		for m, err := it.Next(); m != nil || err != nil; m, err = it.Next() {
			if err != nil {
				t.Fatal(err)
			}
		}
		if got := it.Aggregations().Count(); got != 52 {
			t.Fatalf("%d hits, want 52", got)
		}
	}
	if n := testing.AllocsPerRun(200, search); n > 21 {
		t.Fatalf("search allocates %v, want <= 21", n)
	}
}
