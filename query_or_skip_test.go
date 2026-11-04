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
	"strings"
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/aggregations"
	"github.com/vcaesar/riot/search/similarity"
)

// orSkipDocs: alpha and beta never share a document, gamma overlaps both,
// and omni and half are dense enough for several postings blocks; lengths
// and frequencies vary so scores do too, with many ties at the top.
func orSkipDocs(n int) []string {
	docs := make([]string, n)
	for i := range docs {
		words := make([]string, 0, 16)
		for k := range 1 + i%9 {
			words = append(words, fmt.Sprintf("w%d", (i+k)%13))
		}
		switch i % 7 {
		case 0:
			words = append(words, "alpha")
			if i%3 == 0 {
				words = append(words, "alpha")
			}
		case 3:
			words = append(words, "beta")
		}
		if i%5 == 0 {
			words = append(words, "gamma")
		}
		words = append(words, "omni")
		if i%2 == 0 {
			words = append(words, "half")
		}
		docs[i] = strings.Join(words, " ")
	}
	return docs
}

func orSkipIndex(t *testing.T, docs []string, config Config, deletes bool) *Reader {
	t.Helper()
	w := openConfigWriter(t, config)
	b := NewBatch()
	for i, body := range docs {
		b.Insert(NewDocument(fmt.Sprint(i)).AddField(NewTextField("body", body)))
	}
	if err := w.Batch(b); err != nil {
		t.Fatal(err)
	}
	if deletes {
		del := NewBatch()
		for i := 0; i < len(docs); i += 37 {
			del.Delete(Identifier(fmt.Sprint(i)))
		}
		if err := w.Batch(del); err != nil {
			t.Fatal(err)
		}
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	return r
}

// orSkipSearch runs a top-n search counting every match, which lets a
// disjunction of terms skip block windows once its results are full.
func orSkipSearch(t *testing.T, r *Reader, q Query, n int) exclusionResult {
	t.Helper()
	req := NewTopNSearch(n, q)
	req.AddAggregation("count", aggregations.CountMatches())
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

// TestDisjunctionSkipsLosingWindows: a top-N disjunction of terms that
// counts its matches returns the best hits with their scores and the exact
// total, whether its children are disjoint or overlap, over one or many
// segments, with deletions and a boost.
func TestDisjunctionSkipsLosingWindows(t *testing.T) {
	docs := orSkipDocs(3000)
	term := func(s string) Query { return NewTermQuery(s).SetField("body") }
	or := func(boost float64, terms ...string) func() Query {
		return func() Query {
			q := NewBooleanQuery().SetMinShould(1)
			for _, s := range terms {
				q.AddShould(term(s))
			}
			if boost != 0 {
				q.SetBoost(boost)
			}
			return q
		}
	}
	queries := []struct {
		name  string
		query func() Query
	}{
		{"disjoint", or(0, "alpha", "beta")},
		{"overlapping", or(0, "alpha", "gamma")},
		{"three", or(0, "beta", "gamma", "alpha")},
		{"boosted", or(2.5, "alpha", "beta")},
		{"missing term", or(0, "alpha", "absent")},
		{"dense overlapping", or(0, "omni", "half")},
	}
	for _, ix := range []struct {
		name    string
		config  Config
		deletes bool
	}{
		{"one segment", InMemoryOnlyConfig().WithBatchIndexing(0), false},
		{"segments", InMemoryOnlyConfig().WithBatchIndexing(700), false},
		{"segments with deletes", InMemoryOnlyConfig().WithBatchIndexing(700), true},
	} {
		r := orSkipIndex(t, docs, ix.config, ix.deletes)
		for _, qc := range queries {
			t.Run(ix.name+"/"+qc.name, func(t *testing.T) {
				// every hit fits the results, so nothing is skipped
				want := orSkipSearch(t, r, qc.query(), len(docs))
				if want.total == 0 || want.total != uint64(len(want.scores)) {
					t.Fatalf("reference total %d for %d hits", want.total, len(want.scores))
				}
				for _, n := range []int{1, 3, 10} {
					got := orSkipSearch(t, r, qc.query(), n)
					if got.total != want.total {
						t.Fatalf("n=%d: total %d, want %d", n, got.total, want.total)
					}
					checkTopScores(t, n, got.scores, want.scores)
				}
			})
		}
	}
}

// TestDisjunctionWindowSkipCounts: with a floor no hit can beat, the raw
// disjunction counts whole block windows from the postings bitmaps, and the
// counts add up to the matches.
func TestDisjunctionWindowSkipCounts(t *testing.T) {
	docs := orSkipDocs(3000)
	for _, ix := range []struct {
		name    string
		config  Config
		deletes bool
	}{
		{"one segment", InMemoryOnlyConfig().WithBatchIndexing(0), false},
		{"segments with deletes", InMemoryOnlyConfig().WithBatchIndexing(700), true},
	} {
		r := orSkipIndex(t, docs, ix.config, ix.deletes)
		for _, terms := range [][]string{{"alpha", "beta"}, {"alpha", "gamma"}, {"omni", "half"}} {
			t.Run(ix.name+"/"+strings.Join(terms, "+"), func(t *testing.T) {
				q := NewBooleanQuery().SetMinShould(1).
					AddShould(NewTermQuery(terms[0]).SetField("body"), NewTermQuery(terms[1]).SetField("body"))
				want := orSkipSearch(t, r, q, len(docs)).total
				s, err := q.Searcher(r.reader, searchOptionsFromConfig(r.config, SearchOptions{}))
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := s.Close(); err != nil {
						t.Error(err)
					}
				}()
				skipper, ok := s.(search.ScoreSkipper)
				if !ok {
					t.Fatalf("searcher %T is not a ScoreSkipper", s)
				}
				ctx := search.NewSearchContext(s.DocumentMatchPoolSize()+1, 0)
				ctx.SkipBlocks = true
				var counted uint64
				var windows int
				m, err := skipper.NextAbove(ctx, math.MaxFloat64, func(_ float64, n int) bool {
					counted += uint64(n) // #nosec G115 -- n > 0 counts postings
					if n > 1 {
						windows++
					}
					return true
				})
				if err != nil || m != nil {
					t.Fatalf("NextAbove = %v, %v; want no hit above the floor", m, err)
				}
				if counted != want || windows == 0 {
					t.Fatalf("skipped %d hits in %d windows, want %d hits in windows", counted, windows, want)
				}
			})
		}
	}
}

type constantTestSimilarity struct{}

func (constantTestSimilarity) ComputeNorm(n int) float32 {
	//nolint:gosec // G115: encode the low 32 norm bits, matching the Float32bits decoder.
	return math.Float32frombits(uint32(n))
}
func (constantTestSimilarity) Scorer(boost float64, _ segment.CollectionStats,
	_ segment.TermStats) search.Scorer {
	return similarity.ConstantScorer(boost)
}

// TestDisjunctionSkipNegativeChild: a child scoring below 0 lowers the hits
// it matches, so the hit lacking it must not be skipped by the block bound.
func TestDisjunctionSkipNegativeChild(t *testing.T) {
	docs := make([]string, 3000)
	for i := range docs {
		docs[i] = "alpha beta"
	}
	docs[100] = "alpha"
	config := InMemoryOnlyConfig().WithBatchIndexing(0)
	config.DefaultSimilarity = constantTestSimilarity{}
	r := orSkipIndex(t, docs, config, false)
	q := NewBooleanQuery().SetMinShould(1).AddShould(
		NewTermQuery("alpha").SetField("body").SetBoost(10),
		NewTermQuery("beta").SetField("body").SetBoost(-9))
	got := orSkipSearch(t, r, q, 1)
	if got.total != 3000 || len(got.scores) != 1 || got.scores["100"] != 10 {
		t.Fatalf("got %+v, want doc 100 scoring 10 of 3000", got)
	}
}
