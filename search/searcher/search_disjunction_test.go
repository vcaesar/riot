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

func TestDisjunctionSearch(t *testing.T) {
	martyTermSearcher, err := NewTermSearcher(baseTestIndexReader, "marty", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	dustinTermSearcher, err := NewTermSearcher(baseTestIndexReader, "dustin", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	martyOrDustinSearcher, err := NewDisjunctionSearcher(baseTestIndexReader, []search.Searcher{martyTermSearcher, dustinTermSearcher}, 0, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}

	martyTermSearcher2, err := NewTermSearcher(baseTestIndexReader, "marty", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	dustinTermSearcher2, err := NewTermSearcher(baseTestIndexReader, "dustin", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	martyOrDustinSearcher2, err := NewDisjunctionSearcher(baseTestIndexReader, []search.Searcher{martyTermSearcher2, dustinTermSearcher2}, 0, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}

	raviTermSearcher, err := NewTermSearcher(baseTestIndexReader, "ravi", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	nestedRaviOrMartyOrDustinSearcher, err := NewDisjunctionSearcher(baseTestIndexReader, []search.Searcher{raviTermSearcher, martyOrDustinSearcher2}, 0, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		searcher search.Searcher
		results  []*search.DocumentMatch
	}{
		{
			searcher: martyOrDustinSearcher,
			results: []*search.DocumentMatch{
				{
					Number: baseTestIndexReaderDirect.docNumByID("1"),
					Score:  0.7608983788962145,
				},
				{
					Number: baseTestIndexReaderDirect.docNumByID("3"),
					Score:  0.7608983788962145,
				},
			},
		},
		// test a nested disjunction
		{
			searcher: nestedRaviOrMartyOrDustinSearcher,
			results: []*search.DocumentMatch{
				{
					Number: baseTestIndexReaderDirect.docNumByID("1"),
					Score:  0.7608983788962145,
				},
				{
					Number: baseTestIndexReaderDirect.docNumByID("3"),
					Score:  0.7608983788962145,
				},
				{
					Number: baseTestIndexReaderDirect.docNumByID("4"),
					Score:  0.7608983788962145,
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

func TestDisjunctionAdvance(t *testing.T) {
	martyTermSearcher, err := NewTermSearcher(baseTestIndexReader, "marty", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	dustinTermSearcher, err := NewTermSearcher(baseTestIndexReader, "dustin", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	martyOrDustinSearcher, err := NewDisjunctionSearcher(baseTestIndexReader, []search.Searcher{martyTermSearcher, dustinTermSearcher}, 0, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}

	ctx := &search.Context{
		DocumentMatchPool: search.NewDocumentMatchPool(martyOrDustinSearcher.DocumentMatchPoolSize(), 0),
	}
	match, err := martyOrDustinSearcher.Advance(ctx, baseTestIndexReaderDirect.docNumByID("3"))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if match == nil {
		t.Errorf("expected 3, got nil")
	}
}

func TestDisjunctionSearchTooMany(t *testing.T) {
	// set to max to a low non-zero value
	DisjunctionMaxClauseCount = 2
	defer func() {
		// reset it after the test
		DisjunctionMaxClauseCount = 0
	}()

	martyTermSearcher, err := NewTermSearcher(baseTestIndexReader, "marty", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	dustinTermSearcher, err := NewTermSearcher(baseTestIndexReader, "dustin", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	steveTermSearcher, err := NewTermSearcher(baseTestIndexReader, "steve", "name", 1.0, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewDisjunctionSearcher(baseTestIndexReader, []search.Searcher{martyTermSearcher, dustinTermSearcher, steveTermSearcher}, 0, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err == nil {
		t.Fatal(err)
	}
}
