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

func TestBooleanSearch(t *testing.T) {
	// test 0
	beerTermSearcher, err := NewTermSearcher(baseTestIndexReader, "beer", "desc", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	mustSearcher, err := NewConjunctionSearcher(baseTestIndexReader, []search.Searcher{beerTermSearcher}, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	martyTermSearcher, err := NewTermSearcher(baseTestIndexReader, "marty", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	dustinTermSearcher, err := NewTermSearcher(baseTestIndexReader, "dustin", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	shouldSearcher, err := NewDisjunctionSearcher(baseTestIndexReader, []search.Searcher{martyTermSearcher, dustinTermSearcher}, 0, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	steveTermSearcher, err := NewTermSearcher(baseTestIndexReader, "steve", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	mustNotSearcher, err := NewDisjunctionSearcher(baseTestIndexReader, []search.Searcher{steveTermSearcher}, 0, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	booleanSearcher, err := NewBooleanSearcher(mustSearcher, shouldSearcher, mustNotSearcher, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}

	// test 1
	martyTermSearcher2, err := NewTermSearcher(baseTestIndexReader, "marty", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	dustinTermSearcher2, err := NewTermSearcher(baseTestIndexReader, "dustin", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	shouldSearcher2, err := NewDisjunctionSearcher(baseTestIndexReader, []search.Searcher{martyTermSearcher2, dustinTermSearcher2}, 0, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	steveTermSearcher2, err := NewTermSearcher(baseTestIndexReader, "steve", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	mustNotSearcher2, err := NewDisjunctionSearcher(baseTestIndexReader, []search.Searcher{steveTermSearcher2}, 0, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	booleanSearcher2, err := NewBooleanSearcher(nil, shouldSearcher2, mustNotSearcher2, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}

	// test 2
	steveTermSearcher3, err := NewTermSearcher(baseTestIndexReader, "steve", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	mustNotSearcher3, err := NewDisjunctionSearcher(baseTestIndexReader, []search.Searcher{steveTermSearcher3}, 0, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	booleanSearcher3, err := NewBooleanSearcher(nil, nil, mustNotSearcher3, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}

	// test 3
	beerTermSearcher4, err := NewTermSearcher(baseTestIndexReader, "beer", "desc", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	mustSearcher4, err := NewConjunctionSearcher(baseTestIndexReader, []search.Searcher{beerTermSearcher4}, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	steveTermSearcher4, err := NewTermSearcher(baseTestIndexReader, "steve", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	mustNotSearcher4, err := NewDisjunctionSearcher(baseTestIndexReader, []search.Searcher{steveTermSearcher4}, 0, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	booleanSearcher4, err := NewBooleanSearcher(mustSearcher4, nil, mustNotSearcher4, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}

	// test 4
	beerTermSearcher5, err := NewTermSearcher(baseTestIndexReader, "beer", "desc", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	mustSearcher5, err := NewConjunctionSearcher(baseTestIndexReader, []search.Searcher{beerTermSearcher5}, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	steveTermSearcher5, err := NewTermSearcher(baseTestIndexReader, "steve", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	martyTermSearcher5, err := NewTermSearcher(baseTestIndexReader, "marty", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	mustNotSearcher5, err := NewDisjunctionSearcher(baseTestIndexReader, []search.Searcher{steveTermSearcher5, martyTermSearcher5}, 0, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	booleanSearcher5, err := NewBooleanSearcher(mustSearcher5, nil, mustNotSearcher5, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}

	// test 5
	beerTermSearcher6, err := NewTermSearcher(baseTestIndexReader, "beer", "desc", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	mustSearcher6, err := NewConjunctionSearcher(baseTestIndexReader, []search.Searcher{beerTermSearcher6}, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	martyTermSearcher6, err := NewTermSearcher(baseTestIndexReader, "marty", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	dustinTermSearcher6, err := NewTermSearcher(baseTestIndexReader, "dustin", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	shouldSearcher6, err := NewDisjunctionSearcher(baseTestIndexReader, []search.Searcher{martyTermSearcher6, dustinTermSearcher6}, 2, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	booleanSearcher6, err := NewBooleanSearcher(mustSearcher6, shouldSearcher6, nil, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}

	// test 6
	beerTermSearcher7, err := NewTermSearcher(baseTestIndexReader, "beer", "desc", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	mustSearcher7, err := NewConjunctionSearcher(baseTestIndexReader, []search.Searcher{beerTermSearcher7}, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	booleanSearcher7, err := NewBooleanSearcher(mustSearcher7, nil, nil, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	martyTermSearcher7, err := NewTermSearcher(baseTestIndexReader, "marty", "name", 5.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	conjunctionSearcher7, err := NewConjunctionSearcher(baseTestIndexReader, []search.Searcher{martyTermSearcher7, booleanSearcher7}, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}

	// test 7
	beerTermSearcher8, err := NewTermSearcher(baseTestIndexReader, "beer", "desc", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	mustSearcher8, err := NewConjunctionSearcher(baseTestIndexReader, []search.Searcher{beerTermSearcher8}, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	martyTermSearcher8, err := NewTermSearcher(baseTestIndexReader, "marty", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	dustinTermSearcher8, err := NewTermSearcher(baseTestIndexReader, "dustin", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	shouldSearcher8, err := NewDisjunctionSearcher(baseTestIndexReader, []search.Searcher{martyTermSearcher8, dustinTermSearcher8}, 0, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	steveTermSearcher8, err := NewTermSearcher(baseTestIndexReader, "steve", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	mustNotSearcher8, err := NewDisjunctionSearcher(baseTestIndexReader, []search.Searcher{steveTermSearcher8}, 0, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	booleanSearcher8, err := NewBooleanSearcher(mustSearcher8, shouldSearcher8, mustNotSearcher8, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	dustinTermSearcher8a, err := NewTermSearcher(baseTestIndexReader, "dustin", "name", 5.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	conjunctionSearcher8, err := NewConjunctionSearcher(baseTestIndexReader, []search.Searcher{booleanSearcher8, dustinTermSearcher8a}, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		searcher search.Searcher
		results  []*search.DocumentMatch
	}{
		{
			searcher: booleanSearcher,
			results: []*search.DocumentMatch{
				{
					Number: baseTestIndexReaderDirect.docNumByID("1"),
					Score:  0.8841145775462671,
				},
				{
					Number: baseTestIndexReaderDirect.docNumByID("3"),
					Score:  0.8180668126634709,
				},
				{
					Number: baseTestIndexReaderDirect.docNumByID("4"),
					Score:  0.27080861273520285,
				},
			},
		},
		{
			searcher: booleanSearcher2,
			results: []*search.DocumentMatch{
				{
					Number: baseTestIndexReaderDirect.docNumByID("1"),
					Score:  0.6301338005090412,
				},
				{
					Number: baseTestIndexReaderDirect.docNumByID("3"),
					Score:  0.6301338005090412,
				},
			},
		},
		// no MUST or SHOULD clauses yields no results
		{
			searcher: booleanSearcher3,
			results:  []*search.DocumentMatch{},
		},
		{
			searcher: booleanSearcher4,
			results: []*search.DocumentMatch{
				{
					Number: baseTestIndexReaderDirect.docNumByID("1"),
					Score:  0.25398077703722594,
				},
				{
					Number: baseTestIndexReaderDirect.docNumByID("3"),
					Score:  0.18793301215442973,
				},
				{
					Number: baseTestIndexReaderDirect.docNumByID("4"),
					Score:  0.27080861273520285,
				},
			},
		},
		{
			searcher: booleanSearcher5,
			results: []*search.DocumentMatch{
				{
					Number: baseTestIndexReaderDirect.docNumByID("3"),
					Score:  0.18793301215442973,
				},
				{
					Number: baseTestIndexReaderDirect.docNumByID("4"),
					Score:  0.27080861273520285,
				},
			},
		},
		{
			searcher: booleanSearcher6,
			results:  []*search.DocumentMatch{},
		},
		// test a conjunction query with a nested boolean
		{
			searcher: conjunctionSearcher7,
			results: []*search.DocumentMatch{
				{
					Number: baseTestIndexReaderDirect.docNumByID("1"),
					Score:  3.4046497795824324,
				},
			},
		},
		{
			searcher: conjunctionSearcher8,
			results: []*search.DocumentMatch{
				{
					Number: baseTestIndexReaderDirect.docNumByID("3"),
					Score:  3.968735815208677,
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
					if !scoresCloseEnough(next.Score, test.results[i].Score) {
						t.Errorf("expected result %d to have score %v got  %v for test %d", i, test.results[i].Score, next.Score, testIndex)
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
