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
	"math"
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/searcher"
	"github.com/vcaesar/riot/search/similarity"
)

// exclusionIndex writes docs in several segments, then deletes every 37th.
func exclusionIndex(t testing.TB, docs []string) (*Writer, *Reader) {
	t.Helper()
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	const perBatch = 700
	for start := 0; start < len(docs); start += perBatch {
		b := NewBatch()
		for i := start; i < min(start+perBatch, len(docs)); i++ {
			id := fmt.Sprint(i)
			b.Insert(NewDocument(id).AddField(NewTextField("body", docs[i]).SearchTermPositions()))
		}
		if err = w.Batch(b); err != nil {
			t.Fatal(err)
		}
	}
	del := NewBatch()
	for i := 0; i < len(docs); i += 37 {
		del.Delete(Identifier(fmt.Sprint(i)))
	}
	if err = w.Batch(del); err != nil {
		t.Fatal(err)
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	return w, r
}

// exclusionDocs is deterministic: terms are drawn in slice order.
func exclusionDocs(n int) []string {
	r := rand.New(rand.NewSource(7))
	terms := []struct {
		term string
		pct  int
	}{{"rag", 45}, {"chunk", 35}, {"edge", 20}, {"vector", 15}}
	docs := make([]string, n)
	for i := range docs {
		var words []string
		for range 1 + r.Intn(12) {
			words = append(words, fmt.Sprintf("w%d", r.Intn(50)))
		}
		for _, tp := range terms {
			for k := 0; k < 3 && r.Intn(100) < tp.pct; k++ {
				words = append(words, tp.term)
			}
		}
		docs[i] = strings.Join(words, " ")
	}
	// single-doc terms; merged segments may store them 1-hit encoded
	docs[5] += " solo"
	docs[n-2] += " rag chunk lone"
	docs[n-4] += " rag chunk edge gone"
	return docs
}

type exclusionResult struct {
	total  uint64
	scores map[string]float64
}

func exclusionSearch(t *testing.T, r *Reader, q Query, n int, explain bool) exclusionResult {
	t.Helper()
	req := NewTopNSearch(n, q).WithStandardAggregations()
	if explain {
		req.ExplainScores()
	}
	dmi, err := r.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	rv := exclusionResult{scores: map[string]float64{}}
	m, err := dmi.Next()
	for err == nil && m != nil {
		score := m.Score
		err = m.VisitStoredFields(func(field string, value []byte) bool {
			if field == "_id" {
				rv.scores[string(value)] = score
			}
			return true
		})
		if err == nil {
			m, err = dmi.Next()
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	rv.total = dmi.Aggregations().Count()
	return rv
}

type exclusionCase struct {
	name                    string
	musts, shoulds, mustNot []Query
	boost                   float64
	sumScorer               bool
	folded, empty           bool
}

func (c *exclusionCase) query(withMustNot bool) *BooleanQuery {
	q := NewBooleanQuery().AddMust(c.musts...).AddShould(c.shoulds...)
	if withMustNot {
		q.AddMustNot(c.mustNot...)
	}
	if c.boost != 0 {
		q.SetBoost(c.boost)
	}
	if c.sumScorer {
		q.scorer = similarity.NewCompositeSumScorer()
	}
	return q
}

// TestBooleanMustNotFoldedIntoPostings: must-nots folded into the musts'
// postings return exactly the query without them minus the documents any
// must-not matches, with the same scores and exact totals, for top-N
// searches that skip blocks, explain, deletions and single-doc terms. The
// reference runs on the same reader, so the BM25 stats are shared (two
// indexes would not: their merges drop deleted docs at different times).
func TestBooleanMustNotFoldedIntoPostings(t *testing.T) {
	docs := exclusionDocs(3000)
	w, r := exclusionIndex(t, docs)
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	}()

	term := func(s string) Query { return NewTermQuery(s).SetField("body") }
	phrase := NewMatchPhraseQuery("rag chunk").SetField("body")
	for _, tc := range []exclusionCase{
		{name: "two musts one mustnot", musts: []Query{term("rag"), term("chunk")}, mustNot: []Query{term("edge")}, folded: true},
		{name: "one must", musts: []Query{term("rag")}, mustNot: []Query{term("edge")}, folded: true},
		{name: "two mustnots", musts: []Query{term("rag"), term("chunk")}, mustNot: []Query{term("edge"), term("vector")}, folded: true},
		{name: "single-doc mustnot", musts: []Query{term("rag")}, mustNot: []Query{term("lone")}, folded: true},
		{name: "single-doc must kept", musts: []Query{term("lone")}, mustNot: []Query{term("edge")}, folded: true},
		{name: "single-doc must excluded", musts: []Query{term("gone"), term("rag")}, mustNot: []Query{term("chunk")},
			folded: true, empty: true},
		{name: "same term twice", musts: []Query{term("rag"), term("rag")}, mustNot: []Query{term("edge")}, folded: true},
		{name: "missing mustnot", musts: []Query{term("rag")}, mustNot: []Query{term("absent")}, folded: true},
		{name: "with should", musts: []Query{term("rag")}, shoulds: []Query{term("chunk")}, mustNot: []Query{term("edge")}},
		{name: "boosted", musts: []Query{term("rag"), term("chunk")}, mustNot: []Query{term("edge")}, boost: 3},
		{name: "custom scorer", musts: []Query{term("rag")}, mustNot: []Query{term("edge")}, sumScorer: true},
		{name: "phrase mustnot", musts: []Query{term("rag")}, mustNot: []Query{phrase}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := tc.query(true).Searcher(r.reader, searchOptionsFromConfig(r.config, SearchOptions{}))
			if err != nil {
				t.Fatal(err)
			}
			if _, isBool := s.(*searcher.BooleanSearcher); tc.folded == isBool {
				t.Errorf("searcher %T, want folded %v", s, tc.folded)
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}

			excluded := exclusionSearch(t, r, NewBooleanQuery().AddShould(tc.mustNot...), len(docs), false).scores
			for _, explain := range []bool{false, true} {
				want := exclusionSearch(t, r, tc.query(false), len(docs), explain).scores
				for id := range excluded {
					delete(want, id)
				}
				if (len(want) == 0) != tc.empty {
					t.Fatalf("%d expected hits, want empty %v", len(want), tc.empty)
				}
				for _, n := range []int{10, len(docs)} {
					got := exclusionSearch(t, r, tc.query(true), n, explain)
					if got.total != uint64(len(want)) {
						t.Fatalf("n=%d explain=%v: total %d, want %d", n, explain, got.total, len(want))
					}
					checkTopScores(t, n, got.scores, want)
				}
			}
		})
	}
}

// checkTopScores checks that got holds min(n, len(want)) of the best hits
// of want with their scores; hits tied at the cut-off may be any of them.
func checkTopScores(t *testing.T, n int, got, want map[string]float64) {
	t.Helper()
	if len(got) != min(n, len(want)) {
		t.Fatalf("n=%d: %d hits, want %d", n, len(got), min(n, len(want)))
	}
	if len(got) == 0 {
		return
	}
	scores := make([]float64, 0, len(want))
	for _, s := range want {
		scores = append(scores, s)
	}
	slices.Sort(scores)
	slices.Reverse(scores)
	cutoff := scores[len(got)-1]
	for id, s := range got {
		w, ok := want[id]
		if !ok || math.Abs(s-w) > 1e-9 {
			t.Fatalf("n=%d: hit %s scored %v, want %v (expected hit: %v)", n, id, s, w, ok)
		}
		if s < cutoff-1e-9 {
			t.Fatalf("n=%d: hit %s scored %v below the cut-off %v", n, id, s, cutoff)
		}
	}
}

// TestExcludedPostingsSurviveBackwardSeek: a backwards Advance rebuilds a
// postings iterator; the exclusion must be re-applied, not dropped.
func TestExcludedPostingsSurviveBackwardSeek(t *testing.T) {
	w, r := exclusionIndex(t, exclusionDocs(3000))
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	}()
	opts := searchOptionsFromConfig(r.config, SearchOptions{})
	termSearcher := func(text string) search.Searcher {
		t.Helper()
		s, err := NewTermQuery(text).SetField("body").Searcher(r.reader, opts)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	ctx := search.NewSearchContext(8, 0)
	drain := func(s search.Searcher, first *search.DocumentMatch) (nums []uint64) {
		t.Helper()
		var err error
		for m := first; m != nil; m, err = s.Next(ctx) {
			nums = append(nums, m.Number)
			ctx.DocumentMatchPool.Put(m)
		}
		if err != nil {
			t.Fatal(err)
		}
		return nums
	}

	edge := termSearcher("edge")
	first, err := edge.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	edgeDocs := drain(edge, first)
	if err = edge.Close(); err != nil {
		t.Fatal(err)
	}

	rag := termSearcher("rag")
	conj, err := searcher.NewConjunctionSearcher(r.reader, []search.Searcher{rag}, similarity.NewCompositeSumScorer(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conj.Close(); err != nil {
			t.Error(err)
		}
	}()
	mustNot := termSearcher("edge")
	if ok, err := searcher.ExcludeFromConjunction(conj, mustNot); err != nil || !ok {
		t.Fatalf("exclusion not applied: %v %v", ok, err)
	}
	if err = mustNot.Close(); err != nil {
		t.Fatal(err)
	}

	if first, err = rag.Next(ctx); err != nil {
		t.Fatal(err)
	}
	before := drain(rag, first)
	again, err := rag.Advance(ctx, 0) // backwards: rebuilds the iterator
	if err != nil {
		t.Fatal(err)
	}
	after := drain(rag, again)
	if len(before) == 0 || !slices.Equal(before, after) {
		t.Fatalf("after a backwards seek read %d docs, before %d", len(after), len(before))
	}
	for _, n := range after {
		if slices.Contains(edgeDocs, n) {
			t.Fatalf("excluded doc %d returned", n)
		}
	}
}
