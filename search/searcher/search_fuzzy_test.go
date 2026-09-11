//  Copyright (c) 2020 Couchbase, Inc.
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
					Score:  0.49475996046483234,
				},
				{
					Number: baseTestIndexReaderDirect.docNumByID("2"),
					Score:  0.3660975084344048,
				},
				{
					Number: baseTestIndexReaderDirect.docNumByID("3"),
					Score:  0.3660975084344048,
				},
				{
					Number: baseTestIndexReaderDirect.docNumByID("4"),
					Score:  0.5275409426390048,
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
					Score:  0.6561314664250772,
				},
			},
		},
		{
			searcher: fuzzySearcherprefix,
			results: []*search.DocumentMatch{
				{
					Number: baseTestIndexReaderDirect.docNumByID("5"),
					Score:  1.2329571465400413,
				},
			},
		},
	}

	for testIndex, test := range tests {
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
