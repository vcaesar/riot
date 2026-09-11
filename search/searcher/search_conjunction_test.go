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

func TestConjunctionSearch(t *testing.T) {
	// test 0
	beerTermSearcher, err := NewTermSearcher(baseTestIndexReader, "beer", "desc", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	martyTermSearcher, err := NewTermSearcher(baseTestIndexReader, "marty", "name", 5.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	beerAndMartySearcher, err := NewConjunctionSearcher(baseTestIndexReader, []search.Searcher{beerTermSearcher, martyTermSearcher}, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}

	// test 1
	angstTermSearcher, err := NewTermSearcher(baseTestIndexReader, "angst", "desc", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	beerTermSearcher2, err := NewTermSearcher(baseTestIndexReader, "beer", "desc", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	angstAndBeerSearcher, err := NewConjunctionSearcher(baseTestIndexReader, []search.Searcher{angstTermSearcher, beerTermSearcher2}, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}

	// test 2
	beerTermSearcher3, err := NewTermSearcher(baseTestIndexReader, "beer", "desc", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	jackTermSearcher, err := NewTermSearcher(baseTestIndexReader, "jack", "name", 5.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	beerAndJackSearcher, err := NewConjunctionSearcher(baseTestIndexReader, []search.Searcher{beerTermSearcher3, jackTermSearcher}, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}

	// test 3
	beerTermSearcher4, err := NewTermSearcher(baseTestIndexReader, "beer", "desc", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	misterTermSearcher, err := NewTermSearcher(baseTestIndexReader, "mister", "title", 5.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	beerAndMisterSearcher, err := NewConjunctionSearcher(baseTestIndexReader, []search.Searcher{beerTermSearcher4, misterTermSearcher}, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}

	// test 4
	couchbaseTermSearcher, err := NewTermSearcher(baseTestIndexReader, "couchbase", "street", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	misterTermSearcher2, err := NewTermSearcher(baseTestIndexReader, "mister", "title", 5.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	couchbaseAndMisterSearcher, err := NewConjunctionSearcher(baseTestIndexReader, []search.Searcher{couchbaseTermSearcher, misterTermSearcher2}, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}

	// test 5
	beerTermSearcher5, err := NewTermSearcher(baseTestIndexReader, "beer", "desc", 5.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	couchbaseTermSearcher2, err := NewTermSearcher(baseTestIndexReader, "couchbase", "street", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	misterTermSearcher3, err := NewTermSearcher(baseTestIndexReader, "mister", "title", 5.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	couchbaseAndMisterSearcher2, err := NewConjunctionSearcher(baseTestIndexReader, []search.Searcher{couchbaseTermSearcher2, misterTermSearcher3}, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	beerAndCouchbaseAndMisterSearcher, err := NewConjunctionSearcher(baseTestIndexReader, []search.Searcher{beerTermSearcher5, couchbaseAndMisterSearcher2}, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		searcher search.Searcher
		results  []*search.DocumentMatch
	}{
		{
			searcher: beerAndMartySearcher,
			results: []*search.DocumentMatch{
				{
					Number: baseTestIndexReaderDirect.docNumByID("1"),
					Score:  4.464171841767515,
				},
			},
		},
		{
			searcher: angstAndBeerSearcher,
			results: []*search.DocumentMatch{
				{
					Number: baseTestIndexReaderDirect.docNumByID("2"),
					Score:  1.5816824552876687,
				},
			},
		},
		{
			searcher: beerAndJackSearcher,
			results:  []*search.DocumentMatch{},
		},
		{
			searcher: beerAndMisterSearcher,
			results: []*search.DocumentMatch{
				{
					Number: baseTestIndexReaderDirect.docNumByID("2"),
					Score:  0.7916104490288789,
				},
				{
					Number: baseTestIndexReaderDirect.docNumByID("3"),
					Score:  0.7916104490288789,
				},
			},
		},
		{
			searcher: couchbaseAndMisterSearcher,
			results: []*search.DocumentMatch{
				{
					Number: baseTestIndexReaderDirect.docNumByID("2"),
					Score:  0.3863538726893489,
				},
			},
		},
		{
			searcher: beerAndCouchbaseAndMisterSearcher,
			results: []*search.DocumentMatch{
				{
					Number: baseTestIndexReaderDirect.docNumByID("2"),
					Score:  2.8270039289187148,
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
				DocumentMatchPool: search.NewDocumentMatchPool(10, 0),
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

// FIXME find a way to bring back optimization tests
