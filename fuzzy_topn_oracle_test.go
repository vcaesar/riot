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
	"math/rand/v2"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/aggregations"
)

// osaDistance is the restricted Damerau-Levenshtein (optimal string
// alignment) distance, the metric of the transposition-enabled Levenshtein
// automata used by fuzzy queries.
func osaDistance(a, b string) int {
	rows := make([][]int, len(a)+1)
	for i := range rows {
		rows[i] = make([]int, len(b)+1)
		rows[i][0] = i
	}
	for j := range rows[0] {
		rows[0][j] = j
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			rows[i][j] = min(rows[i-1][j]+1, rows[i][j-1]+1, rows[i-1][j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				rows[i][j] = min(rows[i][j], rows[i-2][j-2]+1)
			}
		}
	}
	return rows[len(a)][len(b)]
}

type oracleDoc struct {
	tokens []string
	alive  bool
}

type oracleIndex struct {
	r     *Reader
	docs  []oracleDoc
	vocab []string
}

// newOracleIndex indexes random documents over every word of {p,q,r} of
// length 1..4 in several batches (segments), deleting some documents in
// later batches, and keeps a shadow copy of every live document.
func newOracleIndex(t *testing.T) *oracleIndex {
	t.Helper()
	var vocab []string
	for n := 1; n <= 4; n++ {
		words := []string{""}
		for range n {
			var next []string
			for _, w := range words {
				for _, c := range "pqr" {
					next = append(next, w+string(c))
				}
			}
			words = next
		}
		vocab = append(vocab, words...)
	}
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	rng := rand.New(rand.NewPCG(7, 11))
	o := &oracleIndex{vocab: vocab}
	for batchNo := range 6 {
		// A batch delete applies to earlier segments only, not to its own inserts.
		earlier := len(o.docs)
		batch := NewBatch()
		for range 60 {
			// Varied lengths and repeated tokens give varied BM25 scores.
			tokens := make([]string, 1+rng.IntN(8))
			for i := range tokens {
				if rng.IntN(2) == 0 {
					tokens[i] = vocab[rng.IntN(12)]
				} else {
					tokens[i] = vocab[rng.IntN(len(vocab))]
				}
			}
			id := strconv.Itoa(len(o.docs))
			batch.Insert(NewDocument(id).
				AddField(NewTextField("body", strings.Join(tokens, " "))).
				AddField(NewNumericField("n", float64(len(o.docs))).Aggregatable()))
			o.docs = append(o.docs, oracleDoc{tokens: tokens, alive: true})
		}
		for d := batchNo; d < earlier; d += 9 {
			batch.Delete(Identifier(strconv.Itoa(d)))
			o.docs[d].alive = false
		}
		if err := w.Batch(batch); err != nil {
			t.Fatal(err)
		}
	}
	o.r, err = w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := o.r.Close(); err != nil {
			t.Error(err)
		}
	})
	return o
}

// expected returns the IDs of live documents with a matching token.
func (o *oracleIndex) expected(match func(token string) bool) map[string]bool {
	rv := map[string]bool{}
	for id, d := range o.docs {
		if d.alive && slices.ContainsFunc(d.tokens, match) {
			rv[strconv.Itoa(id)] = true
		}
	}
	return rv
}

type rankedHit struct {
	id    string
	score float64
}

func rankedHits(t *testing.T, it search.DocumentMatchIterator) []rankedHit {
	t.Helper()
	var hits []rankedHit
	for {
		m, err := it.Next()
		if err != nil {
			t.Fatal(err)
		}
		if m == nil {
			return hits
		}
		hit := rankedHit{score: m.Score}
		err = m.VisitStoredFields(func(field string, value []byte) bool {
			if field == "_id" {
				hit.id = string(value)
			}
			return true
		})
		if err != nil {
			t.Fatal(err)
		}
		hits = append(hits, hit)
	}
}

func (o *oracleIndex) all(t *testing.T, q Query) map[string]bool {
	t.Helper()
	it, err := o.r.Search(context.Background(), NewAllMatches(q))
	if err != nil {
		t.Fatal(err)
	}
	rv := map[string]bool{}
	for _, h := range rankedHits(t, it) {
		rv[h.id] = true
	}
	return rv
}

type topNMode int

const (
	topNFast    topNMode = iota // score-only aggregations: skip paths allowed
	topNSlow                    // a field aggregation disables the skip path
	topNExplain                 // explanations force the general searchers
)

func (o *oracleIndex) topN(t *testing.T, q Query, n, from int, mode topNMode) (hits []rankedHit, total uint64) {
	t.Helper()
	req := NewTopNSearch(n, q).SetFrom(from).WithStandardAggregations()
	switch mode {
	case topNSlow:
		req.AddAggregation("sum", aggregations.Sum(search.Field("n")))
	case topNExplain:
		req.ExplainScores()
	}
	it, err := o.r.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	hits = rankedHits(t, it)
	return hits, it.Aggregations().Count()
}

// checkTopN verifies the total against the oracle and that every top-N
// window is identical (IDs and score bits) across the fast, slow and
// explain paths, contains only expected documents and is ranked.
func (o *oracleIndex) checkTopN(t *testing.T, name string, q Query, want map[string]bool) {
	t.Helper()
	for _, n := range []int{1, 10, 1000} {
		for _, from := range []int{0, 3, 50} {
			fast, fastTotal := o.topN(t, q, n, from, topNFast)
			slow, slowTotal := o.topN(t, q, n, from, topNSlow)
			explain, explainTotal := o.topN(t, q, n, from, topNExplain)
			if fastTotal != uint64(len(want)) || slowTotal != fastTotal || explainTotal != fastTotal {
				t.Fatalf("%s n=%d from=%d: total fast %d slow %d explain %d, oracle %d",
					name, n, from, fastTotal, slowTotal, explainTotal, len(want))
			}
			if !reflect.DeepEqual(fast, slow) || !reflect.DeepEqual(fast, explain) {
				t.Fatalf("%s n=%d from=%d: hits differ\nfast    %v\nslow    %v\nexplain %v",
					name, n, from, fast, slow, explain)
			}
			if wantLen := min(n, max(len(want)-from, 0)); len(fast) != wantLen {
				t.Fatalf("%s n=%d from=%d: %d hits, want %d", name, n, from, len(fast), wantLen)
			}
			for i, h := range fast {
				if !want[h.id] {
					t.Fatalf("%s: unexpected hit %s", name, h.id)
				}
				if i > 0 && h.score > fast[i-1].score {
					t.Fatalf("%s: hit %d score %v above previous %v", name, i, h.score, fast[i-1].score)
				}
			}
		}
	}
}

// TestFuzzyMatchesEditDistanceOracle checks fuzzy expansion through the
// Levenshtein automata against a brute-force edit distance over a dense
// vocabulary, across segments with deletions, fuzziness 0 to 2 and
// prefix lengths, and checks the ranked windows of the fast paths.
func TestFuzzyMatchesEditDistanceOracle(t *testing.T) {
	o := newOracleIndex(t)
	targets := []string{"p", "pq", "qp", "pqr", "rqp", "qqqq", "prpr", "pqrrq", "qrpqr", "xyz"}
	for _, target := range targets {
		for _, fuzziness := range []int{0, 1, 2} {
			for _, prefix := range []int{0, 1, 2} {
				// The required prefix is cut to the target when longer.
				required := target[:min(prefix, len(target))]
				want := o.expected(func(tok string) bool {
					return strings.HasPrefix(tok, required) && osaDistance(tok, target) <= fuzziness
				})
				q := NewFuzzyQuery(target).SetField("body").SetFuzziness(fuzziness).SetPrefix(prefix)
				name := target + "~" + strconv.Itoa(fuzziness) + "/p" + strconv.Itoa(prefix)
				if got := o.all(t, q); !reflect.DeepEqual(got, want) {
					t.Fatalf("%s: matched %d docs, oracle %d", name, len(got), len(want))
				}
				o.checkTopN(t, name, q, want)
			}
		}
	}
}

// TestTermTopNMatchesOracle checks term and prefix top-N windows and exact
// totals across segments with deletions against the shadow documents.
func TestTermTopNMatchesOracle(t *testing.T) {
	o := newOracleIndex(t)
	for _, term := range []string{"p", "q", "r", "pq", "rrr", "pqrp", "absent"} {
		want := o.expected(func(tok string) bool { return tok == term })
		o.checkTopN(t, "term "+term, NewTermQuery(term).SetField("body"), want)
	}
	// "p" expands to more than MultiTermConstantScoreThreshold terms, "pqr" to two.
	for _, prefix := range []string{"p", "pq", "pqr", "pqrr"} {
		want := o.expected(func(tok string) bool { return strings.HasPrefix(tok, prefix) })
		q := NewPrefixQuery(prefix).SetField("body")
		if got := o.all(t, q); !reflect.DeepEqual(got, want) {
			t.Fatalf("prefix %s: matched %d docs, oracle %d", prefix, len(got), len(want))
		}
		o.checkTopN(t, "prefix "+prefix, q, want)
	}
}

// TestFuzzyCountsRunesNotBytes: "é" is two bytes, so a byte distance would
// reject "cafe" for café~1 and accept "caf" only at distance 2.
func TestFuzzyCountsRunesNotBytes(t *testing.T) {
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	}()
	batch := NewBatch()
	for i, s := range []string{"café", "cafe", "caff", "caf", "cafés", "ca", "naïve"} {
		batch.Insert(NewDocument(strconv.Itoa(i)).AddField(NewTextField("body", s)))
	}
	if err := w.Batch(batch); err != nil {
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
	o := &oracleIndex{r: r}
	for _, tc := range []struct {
		term      string
		fuzziness int
		want      []string
	}{
		{"café", 0, []string{"0"}},
		{"café", 1, []string{"0", "1", "2", "3", "4"}},
		{"café", 2, []string{"0", "1", "2", "3", "4", "5"}},
		{"naive", 1, []string{"6"}},
	} {
		want := map[string]bool{}
		for _, id := range tc.want {
			want[id] = true
		}
		q := NewFuzzyQuery(tc.term).SetField("body").SetFuzziness(tc.fuzziness)
		if got := o.all(t, q); !reflect.DeepEqual(got, want) {
			t.Errorf("%s~%d: got %v, want %v", tc.term, tc.fuzziness, got, want)
		}
	}
}
