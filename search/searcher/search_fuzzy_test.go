//  Copyright (c) 2020 Couchbase, Inc.
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

package searcher

import (
	"reflect"
	"testing"

	"github.com/vcaesar/riot/search/similarity"

	"github.com/vcaesar/riot/search"
)

func TestFuzzySearch(t *testing.T) {
	fuzzySearcherbeet, err := NewFuzzySearcher(baseTestIndexReader, "beet", 0, 1, "desc",
		1.0, nil, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}

	fuzzySearcherdouches, err := NewFuzzySearcher(baseTestIndexReader, "douches", 0, 2, "desc",
		1.0, nil, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}

	fuzzySearcheraplee, err := NewFuzzySearcher(baseTestIndexReader, "aplee", 0, 2, "desc",
		1.0, nil, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}

	fuzzySearcherprefix, err := NewFuzzySearcher(baseTestIndexReader, "water", 3, 2, "desc",
		1.0, nil, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		searcher search.Searcher
		results  []*search.DocumentMatch
	}{
		{
			searcher: fuzzySearcherbeet,
			results: []*search.DocumentMatch{
				{
					Number: baseTestIndexReaderDirect.docNumByID("1"),
					Score:  0.19048558277791944,
				},
				{
					Number: baseTestIndexReaderDirect.docNumByID("2"),
					Score:  0.1409497591158223,
				},
				{
					Number: baseTestIndexReaderDirect.docNumByID("3"),
					Score:  0.1409497591158223,
				},
				{
					Number: baseTestIndexReaderDirect.docNumByID("4"),
					Score:  0.20310645955140216,
				},
			},
		},
		{
			searcher: fuzzySearcherdouches,
			results:  []*search.DocumentMatch{},
		},
		{
			searcher: fuzzySearcheraplee,
			results: []*search.DocumentMatch{
				{
					Number: baseTestIndexReaderDirect.docNumByID("3"),
					Score:  0.5433716591324194,
				},
			},
		},
		{
			searcher: fuzzySearcherprefix,
			results: []*search.DocumentMatch{
				{
					Number: baseTestIndexReaderDirect.docNumByID("5"),
					Score:  1.021066668246945,
				},
			},
		},
	}

	for testIndex, test := range tests {
		func() {
			defer func() {
				err := test.searcher.Close()
				if err != nil {
					t.Fatal(err)
				}
			}()

			ctx := &search.Context{
				DocumentMatchPool: search.NewDocumentMatchPool(test.searcher.DocumentMatchPoolSize(), 0),
			}
			next, err := test.searcher.Next(ctx)
			i := 0
			for err == nil && next != nil {
				if i < len(test.results) {
					if next.Number != test.results[i].Number {
						t.Errorf("expected result %d to have number %d got %d for test %d", i, test.results[i].Number, next.Number, testIndex)
					}
					if next.Score != test.results[i].Score {
						t.Errorf("expected result %d to have score %v got %v for test %d", i, test.results[i].Score, next.Score, testIndex)
						t.Logf("scoring explanation: %s", next.Explanation)
					}
				}
				ctx.DocumentMatchPool.Put(next)
				next, err = test.searcher.Next(ctx)
				i++
			}
			if err != nil {
				t.Fatalf("error iterating searcher: %v for test %d", err, testIndex)
			}
			if len(test.results) != i {
				t.Errorf("expected %d results got %d for test %d", len(test.results), i, testIndex)
			}
		}()
	}
}

func TestFuzzySearchLimitErrors(t *testing.T) {
	_, err := NewFuzzySearcher(nil, "water", 3, 3, "desc",
		1.0, nil, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err == nil {
		t.Fatal("`fuzziness exceeds max (2)` error expected")
	}

	_, err = NewFuzzySearcher(nil, "water", 3, -1, "desc",
		1.0, nil, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err == nil {
		t.Fatal("`invalid fuzziness, negative` error expected")
	}
}

// A fuzzy query expanding to a single term is served by that term's
// searcher, with the same hits and scores as the plain term query.
func TestFuzzySingleExpansionIsTermSearcher(t *testing.T) {
	opts := testSearchOptions
	opts.Explain = false
	fs, err := NewFuzzySearcher(baseTestIndexReader, "aplee", 0, 2, "desc",
		1.0, nil, similarity.NewCompositeSumScorer(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	if _, ok := fs.(*TermSearcher); !ok {
		t.Fatalf("got %T, want *TermSearcher", fs)
	}
	boosted, err := NewFuzzySearcher(baseTestIndexReader, "aplee", 0, 2, "desc",
		1.0, nil, similarity.NewCompositeSumScorerWithBoost(2), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer boosted.Close()
	if _, ok := boosted.(*TermSearcher); ok {
		t.Fatal("boosted composite must keep the disjunction")
	}
}

// Fuzziness 0 is the exact term (and used to index an empty automaton list).
func TestFuzzyZeroIsExactTerm(t *testing.T) {
	opts := testSearchOptions
	opts.Explain = false
	for _, tc := range []struct {
		term string
		want int
	}{{"beer", 4}, {"beet", 0}} {
		fs, err := NewFuzzySearcher(baseTestIndexReader, tc.term, 2, 0, "desc",
			1.0, nil, similarity.NewCompositeSumScorer(), opts)
		if err != nil {
			t.Fatal(err)
		}
		ts, err := NewTermSearcher(baseTestIndexReader, tc.term, "desc", 1.0, nil, opts)
		if err != nil {
			t.Fatal(err)
		}
		got, want := drainMatches(t, fs), drainMatches(t, ts)
		if len(got) != tc.want || !reflect.DeepEqual(got, want) {
			t.Fatalf("%s~0: got %v, term searcher %v, want %d hits", tc.term, got, want, tc.want)
		}
	}
}

func drainMatches(t *testing.T, s search.Searcher) (rv []search.DocumentMatch) {
	t.Helper()
	defer func() {
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	ctx := &search.Context{DocumentMatchPool: search.NewDocumentMatchPool(s.DocumentMatchPoolSize(), 0)}
	for {
		m, err := s.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if m == nil {
			return rv
		}
		rv = append(rv, search.DocumentMatch{Number: m.Number, Score: m.Score})
		ctx.DocumentMatchPool.Put(m)
	}
}
