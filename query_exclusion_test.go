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
	"strings"
	"testing"

	"github.com/vcaesar/riot/search/searcher"
)

// exclusionIndex writes the same docs, in several segments with deletions,
// to a writer opened with cfg.
func exclusionIndex(t testing.TB, cfg *Config, docs []string) (*Writer, *Reader) {
	t.Helper()
	w, err := OpenWriter(*cfg)
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

func exclusionDocs(n int) []string {
	r := rand.New(rand.NewSource(7))
	docs := make([]string, n)
	for i := range docs {
		var words []string
		for range 1 + r.Intn(12) {
			words = append(words, fmt.Sprintf("w%d", r.Intn(50)))
		}
		for term, pct := range map[string]int{"rag": 45, "chunk": 35, "edge": 20, "vector": 15} {
			for k := 0; k < 3 && r.Intn(100) < pct; k++ {
				words = append(words, term)
			}
		}
		docs[i] = strings.Join(words, " ")
	}
	// single-doc terms; merged segments may store them 1-hit encoded
	docs[5] += " solo"
	docs[n-2] += " rag chunk lone"
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

// TestBooleanMustNotFoldedIntoPostings: must-nots folded into the musts'
// postings return the same hits, scores and exact totals as the per-hit
// must-not check (conjunction optimization disabled), including top-N
// searches that skip blocks, deletions and 1-hit postings.
func TestBooleanMustNotFoldedIntoPostings(t *testing.T) {
	docs := exclusionDocs(3000)
	cfg := InMemoryOnlyConfig()
	w, r := exclusionIndex(t, &cfg, docs)
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	}()
	refCfg := InMemoryOnlyConfig().DisableOptimizeConjunction()
	rw, rr := exclusionIndex(t, &refCfg, docs)
	defer func() {
		if err := rr.Close(); err != nil {
			t.Error(err)
		}
		if err := rw.Close(); err != nil {
			t.Error(err)
		}
	}()

	term := func(s string) Query { return NewTermQuery(s).SetField("body") }
	for _, tc := range []struct {
		name   string
		q      *BooleanQuery
		folded bool
		empty  bool
	}{
		{"two musts one mustnot", NewBooleanQuery().AddMust(term("rag"), term("chunk")).AddMustNot(term("edge")), true, false},
		{"one must", NewBooleanQuery().AddMust(term("rag")).AddMustNot(term("edge")), true, false},
		{"two mustnots", NewBooleanQuery().AddMust(term("rag"), term("chunk")).AddMustNot(term("edge"), term("vector")), true, false},
		{"single-doc mustnot", NewBooleanQuery().AddMust(term("rag")).AddMustNot(term("lone")), true, false},
		{"single-doc must kept", NewBooleanQuery().AddMust(term("lone")).AddMustNot(term("edge")), true, false},
		{"single-doc must excluded", NewBooleanQuery().AddMust(term("lone"), term("rag")).AddMustNot(term("chunk")), true, true},
		{"missing mustnot", NewBooleanQuery().AddMust(term("rag")).AddMustNot(term("absent")), true, false},
		{"with should", NewBooleanQuery().AddMust(term("rag")).AddShould(term("chunk")).AddMustNot(term("edge")), true, false},
		{"boosted", NewBooleanQuery().AddMust(term("rag"), term("chunk")).AddMustNot(term("edge")).SetBoost(3), true, false},
		{"phrase mustnot", NewBooleanQuery().AddMust(term("rag")).AddMustNot(NewMatchPhraseQuery("rag chunk").SetField("body")), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := searchOptionsFromConfig(r.config, SearchOptions{})
			s, err := tc.q.Searcher(r.reader, opts)
			if err != nil {
				t.Fatal(err)
			}
			_, isBool := s.(*searcher.BooleanSearcher)
			if foldedOnly := tc.folded && len(tc.q.shoulds) == 0 && tc.q.boost == nil; foldedOnly == isBool {
				t.Errorf("searcher %T, folded %v", s, tc.folded)
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			for _, n := range []int{10, len(docs)} {
				for _, explain := range []bool{false, true} {
					got := exclusionSearch(t, r, tc.q, n, explain)
					want := exclusionSearch(t, rr, tc.q, n, explain)
					if got.total != want.total || len(got.scores) != len(want.scores) {
						t.Fatalf("n=%d explain=%v: total %d (%d hits), want %d (%d hits)",
							n, explain, got.total, len(got.scores), want.total, len(want.scores))
					}
					if n == len(docs) && (want.total == 0) != tc.empty {
						t.Fatalf("total %d, want empty %v", want.total, tc.empty)
					}
					checkSameScores(t, n, got.scores, want.scores)
				}
			}
		})
	}
}

// checkSameScores compares the hits; top-N hits can differ only among
// documents tied at the cut-off score.
func checkSameScores(t *testing.T, n int, got, want map[string]float64) {
	t.Helper()
	low := math.Inf(1)
	for _, s := range want {
		low = min(low, s)
	}
	for id, s := range got {
		w, ok := want[id]
		if !ok {
			if math.Abs(s-low) > 1e-9 {
				t.Fatalf("n=%d: extra hit %s (%v)", n, id, s)
			}
			continue
		}
		if math.Abs(s-w) > 1e-9 {
			t.Fatalf("n=%d: hit %s scored %v, want %v", n, id, s, w)
		}
	}
}
