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
	"reflect"
	"testing"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/searcher"
	"github.com/vcaesar/riot/search/similarity"
)

// multiTermIndex indexes numDocs documents whose "tag" field holds the
// unique term "tNNN" plus "shared" so a prefix expands to numDocs terms.
func multiTermIndex(t *testing.T, numDocs int) *Reader {
	t.Helper()
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	b := NewBatch()
	for i := 0; i < numDocs; i++ {
		doc := NewDocument(fmt.Sprintf("%d", i)).
			AddField(NewTextField("tag", fmt.Sprintf("t%03d shared", i)).SearchTermPositions())
		b.Update(doc.ID(), doc)
	}
	if err = w.Batch(b); err != nil {
		t.Fatal(err)
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	return r
}

func collectHits(t *testing.T, r *Reader, req SearchRequest) []*search.DocumentMatch {
	t.Helper()
	dmi, err := r.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var hits []*search.DocumentMatch
	next, err := dmi.Next()
	for err == nil && next != nil {
		hits = append(hits, next)
		next, err = dmi.Next()
	}
	if err != nil {
		t.Fatal(err)
	}
	return hits
}

// TestPrefixQueryConstantScore checks that a prefix expanding to more
// than MultiTermConstantScoreThreshold terms is served as a constant-score
// bitmap union: same hits, score == boost, exact count; and that the
// scored disjunction is kept when locations are requested.
func TestPrefixQueryConstantScore(t *testing.T) {
	const numDocs = 40
	r := multiTermIndex(t, numDocs)

	q := NewPrefixQuery("t").SetField("tag").SetBoost(2.5)
	hits := collectHits(t, r, NewTopNSearch(numDocs, q).WithStandardAggregations())
	if len(hits) != numDocs {
		t.Fatalf("got %d hits, want %d", len(hits), numDocs)
	}
	for _, hit := range hits {
		if hit.Score != 2.5 {
			t.Fatalf("doc %d score = %v, want constant boost 2.5", hit.Number, hit.Score)
		}
	}

	// below the threshold every expanded term is scored
	q = NewPrefixQuery("t00").SetField("tag")
	hits = collectHits(t, r, NewTopNSearch(numDocs, q))
	if len(hits) != 10 {
		t.Fatalf("got %d hits, want 10", len(hits))
	}
	if hits[0].Score == 1 {
		t.Fatalf("expected BM25 score for a small expansion, got %v", hits[0].Score)
	}

	// requesting locations needs the per-term searchers
	q = NewPrefixQuery("t").SetField("tag")
	hits = collectHits(t, r, NewTopNSearch(numDocs, q).IncludeLocations())
	if len(hits) != numDocs {
		t.Fatalf("got %d hits, want %d", len(hits), numDocs)
	}
	if len(hits[0].Locations) == 0 || hits[0].Score == 1 {
		t.Fatalf("expected scored hit with locations, got score %v locations %v", hits[0].Score, hits[0].Locations)
	}

	// disabling the threshold restores the scored disjunction
	prev := searcher.MultiTermConstantScoreThreshold
	searcher.MultiTermConstantScoreThreshold = 0
	defer func() { searcher.MultiTermConstantScoreThreshold = prev }()
	hits = collectHits(t, r, NewTopNSearch(numDocs, q))
	if len(hits) != numDocs || hits[0].Score == 1 {
		t.Fatalf("got %d hits, first score %v; want %d scored hits", len(hits), hits[0].Score, numDocs)
	}
}

// TestTermRangeQueryConstantScore covers the [][]byte multi-term entry
// point through a term range wider than the threshold.
func TestTermRangeQueryConstantScore(t *testing.T) {
	const numDocs = 40
	r := multiTermIndex(t, numDocs)

	q := NewTermRangeInclusiveQuery("t000", "t029", true, true).SetField("tag")
	hits := collectHits(t, r, NewTopNSearch(numDocs, q).WithStandardAggregations())
	if len(hits) != 30 {
		t.Fatalf("got %d hits, want 30", len(hits))
	}
	for _, hit := range hits {
		if hit.Score != 1 {
			t.Fatalf("doc %d score = %v, want constant 1", hit.Number, hit.Score)
		}
	}
}

// TestMultiTermConstantScoreBatches drives the constant-score path with
// DisjunctionMaxClauseCount smaller than the expansion and limit=false,
// so the bitmap union is folded batch by batch through the previous
// batch's unadorned searcher.
func TestMultiTermConstantScoreBatches(t *testing.T) {
	const numDocs = 40
	r := multiTermIndex(t, numDocs)

	prevMax, prevThreshold := searcher.DisjunctionMaxClauseCount, searcher.MultiTermConstantScoreThreshold
	searcher.DisjunctionMaxClauseCount, searcher.MultiTermConstantScoreThreshold = 7, 4
	defer func() {
		searcher.DisjunctionMaxClauseCount, searcher.MultiTermConstantScoreThreshold = prevMax, prevThreshold
	}()

	terms := make([]string, 0, 26)
	for i := 0; i < 25; i++ {
		terms = append(terms, fmt.Sprintf("t%03d", i))
	}
	terms = append(terms, "shared") // in every doc: union must still count each doc once
	opts := searchOptionsFromConfig(r.config, SearchOptions{})
	s, err := searcher.NewMultiTermSearcher(r.reader, terms, "tag", 3, nil, similarity.NewCompositeSumScorer(), opts, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := s.Count(); got != numDocs {
		t.Fatalf("Count = %d, want exact union %d", got, numDocs)
	}
	ctx := search.NewSearchContext(s.DocumentMatchPoolSize(), 0)
	var n int
	next, err := s.Next(ctx)
	for err == nil && next != nil {
		if next.Score != 3 {
			t.Fatalf("doc %d score = %v, want boost 3", next.Number, next.Score)
		}
		n++
		ctx.DocumentMatchPool.Put(next)
		next, err = s.Next(ctx)
	}
	if err != nil {
		t.Fatal(err)
	}
	if n != numDocs {
		t.Fatalf("iterated %d hits, want %d", n, numDocs)
	}
}

// TestPhraseQueryExactPathEndToEnd runs exact phrases through a real
// index where the exact position merge sees multi-field candidates
// (composite _all carries the source field names), multi-valued fields
// (the phrase must not span values) and Advance from a conjunction.
func TestPhraseQueryExactPathEndToEnd(t *testing.T) {
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	docs := []*Document{
		NewDocument("1").
			AddField(NewTextField("title", "quick brown fox").SearchTermPositions()).
			AddField(NewTextField("body", "the fox is quick").SearchTermPositions()).
			AddField(NewCompositeFieldExcluding("_all", nil)),
		NewDocument("2").
			AddField(NewTextField("title", "brown quick").SearchTermPositions()).
			AddField(NewTextField("body", "nothing quick here brown").SearchTermPositions()).
			AddField(NewTextField("body", "quick brown").SearchTermPositions()). // second value, phrase inside it
			AddField(NewCompositeFieldExcluding("_all", nil)),
		NewDocument("3").
			AddField(NewTextField("title", "ends with quick").SearchTermPositions()).
			AddField(NewTextField("title", "brown starts the next value").SearchTermPositions()).
			AddField(NewCompositeFieldExcluding("_all", nil)),
	}
	b := NewBatch()
	for _, d := range docs {
		b.Update(d.ID(), d)
	}
	if err = w.Batch(b); err != nil {
		t.Fatal(err)
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	ids := func(hits []*search.DocumentMatch) []string {
		var out []string
		for _, h := range hits {
			err := h.VisitStoredFields(func(field string, value []byte) bool {
				if field == "_id" {
					out = append(out, string(value))
				}
				return true
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		return out
	}

	// _all: doc 1 (title), doc 2 (second body value); doc 3 spans values
	hits := collectHits(t, r, NewTopNSearch(10, NewMatchPhraseQuery("quick brown").SetField("_all")).IncludeLocations())
	if got := ids(hits); len(got) != 2 || got[0] == "3" || got[1] == "3" {
		t.Fatalf("_all phrase hits = %v, want docs 1 and 2", got)
	}
	for _, h := range hits {
		var n int
		for _, tlm := range h.Locations {
			n += len(tlm["quick"]) + len(tlm["brown"])
		}
		if n != 2 {
			t.Fatalf("expected exactly the matched pair of locations, got %v", h.Locations)
		}
	}

	// body only: doc 2, via the second value; conjunction forces Advance
	q := NewBooleanQuery().AddMust(
		NewMatchQuery("nothing").SetField("body"),
		NewMatchPhraseQuery("quick brown").SetField("body"))
	if got := ids(collectHits(t, r, NewTopNSearch(10, q))); !reflect.DeepEqual(got, []string{"2"}) {
		t.Fatalf("conjunction with phrase = %v, want [2]", got)
	}
	// title only: doc 1; doc 3 must not match across values
	if got := ids(collectHits(t, r, NewTopNSearch(10, NewMatchPhraseQuery("quick brown").SetField("title")))); !reflect.DeepEqual(got, []string{"1"}) {
		t.Fatalf("title phrase = %v, want [1]", got)
	}
}
