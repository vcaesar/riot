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
	"reflect"
	"testing"

	"github.com/vcaesar/riot/search/similarity"

	"github.com/vcaesar/riot/search"
)

func TestPhraseSearch(t *testing.T) {
	soptions := search.SearcherOptions{
		SimilarityForField: func(_ string) search.Similarity {
			return similarity.NewBM25Similarity()
		},
		Explain:            true,
		IncludeTermVectors: true,
	}

	phraseSearcher, err := NewMultiPhraseSearcher(baseTestIndexReader, [][]string{{"angst"}, {"beer"}}, "desc", nil, soptions)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		searcher   search.Searcher
		results    []*search.DocumentMatch
		locations  map[string]map[string][]search.Location
		fieldterms [][2]string
	}{
		{
			searcher: phraseSearcher,
			results: []*search.DocumentMatch{
				{
					Number: baseTestIndexReaderDirect.docNumByID("2"),
					Score:  1.5816824552876687,
				},
			},
			locations:  map[string]map[string][]search.Location{"desc": {"beer": {{Pos: 2, Start: 6, End: 10}}, "angst": {{Pos: 1, Start: 0, End: 5}}}},
			fieldterms: [][2]string{{"desc", "beer"}, {"desc", "angst"}},
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
				next.Complete(nil)
				if i < len(test.results) {
					if next.Number != test.results[i].Number {
						t.Errorf("expected result %d to have number %d got %d for test %d\n", i, test.results[i].Number, next.Number, testIndex)
					}
					if next.Score != test.results[i].Score {
						t.Errorf("expected result %d to have score %v got %v for test %d\n", i, test.results[i].Score, next.Score, testIndex)
						t.Logf("scoring explanation: %s\n", next.Explanation)
					}
					for _, ft := range test.fieldterms {
						locs := next.Locations[ft[0]][ft[1]]
						explocs := test.locations[ft[0]][ft[1]]
						if len(explocs) != len(locs) {
							t.Fatalf("expected result %d to have %d Locations (%#v) but got %d (%#v) for test %d with field %q and term %q\n", i, len(explocs), explocs, len(locs), locs, testIndex, ft[0], ft[1])
						}
						for ind, exploc := range explocs {
							if !reflect.DeepEqual(*locs[ind], exploc) {
								t.Errorf("expected result %d to have Location %v got %v for test %d\n", i, exploc, locs[ind], testIndex)
							}
						}
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

func TestMultiPhraseSearch(t *testing.T) {
	soptions := search.SearcherOptions{
		SimilarityForField: func(_ string) search.Similarity {
			return similarity.NewBM25Similarity()
		},
		Explain:            true,
		IncludeTermVectors: true,
	}

	tests := []struct {
		phrase [][]string
		docids []uint64
	}{
		{
			phrase: [][]string{{"angst", "what"}, {"beer"}},
			docids: []uint64{baseTestIndexReaderDirect.docNumByID("2")},
		},
	}

	for i, test := range tests {
		searcher, err := NewMultiPhraseSearcher(baseTestIndexReader, test.phrase, "desc", nil, soptions)
		if err != nil {
			t.Error(err)
		}
		ctx := &search.Context{
			DocumentMatchPool: search.NewDocumentMatchPool(searcher.DocumentMatchPoolSize(), 0),
		}
		next, err := searcher.Next(ctx)
		var actualIDs []uint64
		for err == nil && next != nil {
			actualIDs = append(actualIDs, next.Number)
			ctx.DocumentMatchPool.Put(next)
			next, err = searcher.Next(ctx)
		}
		if err != nil {
			t.Fatalf("error iterating searcher: %v for test %d", err, i)
		}
		if !reflect.DeepEqual(test.docids, actualIDs) {
			t.Fatalf("expected ids: %v, got %v", test.docids, actualIDs)
		}

		err = searcher.Close()
		if err != nil {
			t.Error(err)
		}

		err = baseTestIndexReader.Close()
		if err != nil {
			t.Error(err)
		}
	}
}

func TestSloppyMultiPhraseSearch(t *testing.T) {
	soptions := search.SearcherOptions{
		SimilarityForField: func(_ string) search.Similarity {
			return similarity.NewBM25Similarity()
		},
		Explain:            true,
		IncludeTermVectors: true,
	}

	tests := []struct {
		phrase [][]string
		docids []uint64
		slop   int
	}{
		{
			phrase: [][]string{{"angst"}, {"beer"}, {"database"}},
			docids: []uint64{},
			slop:   0,
		},
		{
			phrase: [][]string{{"angst"}, {"beer"}, {"database"}},
			docids: []uint64{baseTestIndexReaderDirect.docNumByID("2")},
			slop:   1,
		},
		{
			phrase: [][]string{{"apple", "angst"}, {"dank"}},
			docids: []uint64{baseTestIndexReaderDirect.docNumByID("3")},
			slop:   2,
		},
		// test interchanged words
		// document 2 has the phrase "angst beer", searching for "beer angst" requires slop=2 to match
		{
			phrase: [][]string{{"beer"}, {"angst"}},
			docids: []uint64{},
			slop:   0,
		},
		{
			phrase: [][]string{{"beer"}, {"angst"}},
			docids: []uint64{},
			slop:   1,
		},
		{
			phrase: [][]string{{"beer"}, {"angst"}},
			docids: []uint64{baseTestIndexReaderDirect.docNumByID("2")},
			slop:   2,
		},
	}

	for i, test := range tests {
		searcher, err := NewSloppyMultiPhraseSearcher(baseTestIndexReader, test.phrase, "desc", test.slop, nil, soptions)
		if err != nil {
			t.Error(err)
		}
		ctx := &search.Context{
			DocumentMatchPool: search.NewDocumentMatchPool(searcher.DocumentMatchPoolSize(), 0),
		}
		next, err := searcher.Next(ctx)
		actualIDs := []uint64{}
		for err == nil && next != nil {
			actualIDs = append(actualIDs, next.Number)
			ctx.DocumentMatchPool.Put(next)
			next, err = searcher.Next(ctx)
		}
		if err != nil {
			t.Fatalf("error iterating searcher: %v for test %d", err, i)
		}
		if !reflect.DeepEqual(test.docids, actualIDs) {
			t.Fatalf("test case %d: expected ids: %v, got %v", i, test.docids, actualIDs)
		}

		err = searcher.Close()
		if err != nil {
			t.Error(err)
		}

		err = baseTestIndexReader.Close()
		if err != nil {
			t.Error(err)
		}
	}
}

func TestFindPhrasePaths(t *testing.T) {
	tests := []struct {
		phrase [][]string
		tlm    search.TermLocationMap
		paths  []phrasePath
	}{
		// simplest matching case
		{
			phrase: [][]string{{"cat"}, {"dog"}},
			tlm: search.TermLocationMap{
				"cat": search.Locations{
					&search.Location{
						Pos: 1,
					},
				},
				"dog": search.Locations{
					&search.Location{
						Pos: 2,
					},
				},
			},
			paths: []phrasePath{
				{
					phrasePart{"cat", &search.Location{Pos: 1}},
					phrasePart{"dog", &search.Location{Pos: 2}},
				},
			},
		},
		// second term missing, no match
		{
			phrase: [][]string{{"cat"}, {"dog"}},
			tlm: search.TermLocationMap{
				"cat": search.Locations{
					&search.Location{
						Pos: 1,
					},
				},
			},
			paths: nil,
		},
		// second term exists but in wrong position
		{
			phrase: [][]string{{"cat"}, {"dog"}},
			tlm: search.TermLocationMap{
				"cat": search.Locations{
					&search.Location{
						Pos: 1,
					},
				},
				"dog": search.Locations{
					&search.Location{
						Pos: 3,
					},
				},
			},
			paths: nil,
		},
		// matches multiple times
		{
			phrase: [][]string{{"cat"}, {"dog"}},
			tlm: search.TermLocationMap{
				"cat": search.Locations{
					&search.Location{
						Pos: 1,
					},
					&search.Location{
						Pos: 8,
					},
				},
				"dog": search.Locations{
					&search.Location{
						Pos: 2,
					},
					&search.Location{
						Pos: 9,
					},
				},
			},
			paths: []phrasePath{
				{
					phrasePart{"cat", &search.Location{Pos: 1}},
					phrasePart{"dog", &search.Location{Pos: 2}},
				},
				{
					phrasePart{"cat", &search.Location{Pos: 8}},
					phrasePart{"dog", &search.Location{Pos: 9}},
				},
			},
		},
		// match over gaps
		{
			phrase: [][]string{{"cat"}, {""}, {"dog"}},
			tlm: search.TermLocationMap{
				"cat": search.Locations{
					&search.Location{
						Pos: 1,
					},
				},
				"dog": search.Locations{
					&search.Location{
						Pos: 3,
					},
				},
			},
			paths: []phrasePath{
				{
					phrasePart{"cat", &search.Location{Pos: 1}},
					phrasePart{"dog", &search.Location{Pos: 3}},
				},
			},
		},
		// match with leading ""
		{
			phrase: [][]string{{""}, {"cat"}, {"dog"}},
			tlm: search.TermLocationMap{
				"cat": search.Locations{
					&search.Location{
						Pos: 2,
					},
				},
				"dog": search.Locations{
					&search.Location{
						Pos: 3,
					},
				},
			},
			paths: []phrasePath{
				{
					phrasePart{"cat", &search.Location{Pos: 2}},
					phrasePart{"dog", &search.Location{Pos: 3}},
				},
			},
		},
		// match with trailing ""
		{
			phrase: [][]string{{"cat"}, {"dog"}, {""}},
			tlm: search.TermLocationMap{
				"cat": search.Locations{
					&search.Location{
						Pos: 2,
					},
				},
				"dog": search.Locations{
					&search.Location{
						Pos: 3,
					},
				},
			},
			paths: []phrasePath{
				{
					phrasePart{"cat", &search.Location{Pos: 2}},
					phrasePart{"dog", &search.Location{Pos: 3}},
				},
			},
		},
	}

	for i, test := range tests {
		actualPaths := findPhrasePaths(0, test.phrase, test.tlm, nil, 0, nil)
		if !reflect.DeepEqual(actualPaths, test.paths) {
			t.Fatalf("expected: %v got %v for test %d", test.paths, actualPaths, i)
		}
	}
}

func TestFindPhrasePathsSloppy(t *testing.T) {
	tlm := search.TermLocationMap{
		"one": search.Locations{
			&search.Location{
				Pos: 1,
			},
		},
		"two": search.Locations{
			&search.Location{
				Pos: 2,
			},
		},
		"three": search.Locations{
			&search.Location{
				Pos: 3,
			},
		},
		"four": search.Locations{
			&search.Location{
				Pos: 4,
			},
		},
		"five": search.Locations{
			&search.Location{
				Pos: 5,
			},
		},
	}

	tests := []struct {
		phrase [][]string
		paths  []phrasePath
		slop   int
		tlm    search.TermLocationMap
	}{
		// no match
		{
			phrase: [][]string{{"one"}, {"five"}},
			slop:   2,
		},
		// should match
		{
			phrase: [][]string{{"one"}, {"five"}},
			slop:   3,
			paths: []phrasePath{
				{
					phrasePart{"one", &search.Location{Pos: 1}},
					phrasePart{"five", &search.Location{Pos: 5}},
				},
			},
		},
		// slop 0 finds exact match
		{
			phrase: [][]string{{"four"}, {"five"}},
			slop:   0,
			paths: []phrasePath{
				{
					phrasePart{"four", &search.Location{Pos: 4}},
					phrasePart{"five", &search.Location{Pos: 5}},
				},
			},
		},
		// slop 0 does not find exact match (reversed)
		{
			phrase: [][]string{{"two"}, {"one"}},
			slop:   0,
		},
		// slop 1 finds exact match
		{
			phrase: [][]string{{"one"}, {"two"}},
			slop:   1,
			paths: []phrasePath{
				{
					phrasePart{"one", &search.Location{Pos: 1}},
					phrasePart{"two", &search.Location{Pos: 2}},
				},
			},
		},
		// slop 1 *still* does not find exact match (reversed) requires at least 2
		{
			phrase: [][]string{{"two"}, {"one"}},
			slop:   1,
		},
		// slop 2 does finds exact match reversed
		{
			phrase: [][]string{{"two"}, {"one"}},
			slop:   2,
			paths: []phrasePath{
				{
					phrasePart{"two", &search.Location{Pos: 2}},
					phrasePart{"one", &search.Location{Pos: 1}},
				},
			},
		},
		// slop 2 not enough for this
		{
			phrase: [][]string{{"three"}, {"one"}},
			slop:   2,
		},
		// slop should be cumulative
		{
			phrase: [][]string{{"one"}, {"three"}, {"five"}},
			slop:   2,
			paths: []phrasePath{
				{
					phrasePart{"one", &search.Location{Pos: 1}},
					phrasePart{"three", &search.Location{Pos: 3}},
					phrasePart{"five", &search.Location{Pos: 5}},
				},
			},
		},
		// should require 6
		{
			phrase: [][]string{{"five"}, {"three"}, {"one"}},
			slop:   5,
		},
		// so lets try 6
		{
			phrase: [][]string{{"five"}, {"three"}, {"one"}},
			slop:   6,
			paths: []phrasePath{
				{
					phrasePart{"five", &search.Location{Pos: 5}},
					phrasePart{"three", &search.Location{Pos: 3}},
					phrasePart{"one", &search.Location{Pos: 1}},
				},
			},
		},
		// test an append() related edge case, where append()'s
		// current behavior needs to be called 3 times starting from a
		// nil slice before it grows to a slice with extra capacity --
		// hence, 3 initial terms of ark, bat, cat
		{
			phrase: [][]string{
				{"ark"}, {"bat"}, {"cat"}, {"dog"},
			},
			slop: 1,
			paths: []phrasePath{
				{
					phrasePart{"ark", &search.Location{Pos: 1}},
					phrasePart{"bat", &search.Location{Pos: 2}},
					phrasePart{"cat", &search.Location{Pos: 3}},
					phrasePart{"dog", &search.Location{Pos: 4}},
				},
				{
					phrasePart{"ark", &search.Location{Pos: 1}},
					phrasePart{"bat", &search.Location{Pos: 2}},
					phrasePart{"cat", &search.Location{Pos: 3}},
					phrasePart{"dog", &search.Location{Pos: 5}},
				},
			},
			tlm: search.TermLocationMap{ // ark bat cat dog dog
				"ark": search.Locations{
					&search.Location{Pos: 1},
				},
				"bat": search.Locations{
					&search.Location{Pos: 2},
				},
				"cat": search.Locations{
					&search.Location{Pos: 3},
				},
				"dog": search.Locations{
					&search.Location{Pos: 4},
					&search.Location{Pos: 5},
				},
			},
		},
		// test that we don't see multiple hits from the same location
		{
			phrase: [][]string{
				{"cat"}, {"dog"}, {"dog"},
			},
			slop: 1,
			paths: []phrasePath{
				{
					phrasePart{"cat", &search.Location{Pos: 1}},
					phrasePart{"dog", &search.Location{Pos: 2}},
					phrasePart{"dog", &search.Location{Pos: 3}},
				},
			},
			tlm: search.TermLocationMap{ // cat dog dog
				"cat": search.Locations{
					&search.Location{Pos: 1},
				},
				"dog": search.Locations{
					&search.Location{Pos: 2},
					&search.Location{Pos: 3},
				},
			},
		},
		// test that we don't see multiple hits from the same location
		{
			phrase: [][]string{
				{"cat"}, {"dog"},
			},
			slop: 10,
			paths: []phrasePath{
				{
					phrasePart{"cat", &search.Location{Pos: 1}},
					phrasePart{"dog", &search.Location{Pos: 2}},
				},
				{
					phrasePart{"cat", &search.Location{Pos: 1}},
					phrasePart{"dog", &search.Location{Pos: 4}},
				},
				{
					phrasePart{"cat", &search.Location{Pos: 3}},
					phrasePart{"dog", &search.Location{Pos: 2}},
				},
				{
					phrasePart{"cat", &search.Location{Pos: 3}},
					phrasePart{"dog", &search.Location{Pos: 4}},
				},
			},
			tlm: search.TermLocationMap{ // cat dog cat dog
				"cat": search.Locations{
					&search.Location{Pos: 1},
					&search.Location{Pos: 3},
				},
				"dog": search.Locations{
					&search.Location{Pos: 2},
					&search.Location{Pos: 4},
				},
			},
		},
	}

	for i, test := range tests {
		tlmToUse := test.tlm
		if tlmToUse == nil {
			tlmToUse = tlm
		}
		actualPaths := findPhrasePaths(0, test.phrase, tlmToUse, nil, test.slop, nil)
		if !reflect.DeepEqual(actualPaths, test.paths) {
			t.Fatalf("expected: %v got %v for test %d", test.paths, actualPaths, i)
		}
	}
}

func TestFindPhrasePathsSloppyPalyndrome(t *testing.T) {
	tlm := search.TermLocationMap{
		"one": search.Locations{
			&search.Location{
				Pos: 1,
			},
			&search.Location{
				Pos: 5,
			},
		},
		"two": search.Locations{
			&search.Location{
				Pos: 2,
			},
			&search.Location{
				Pos: 4,
			},
		},
		"three": search.Locations{
			&search.Location{
				Pos: 3,
			},
		},
	}

	tests := []struct {
		phrase [][]string
		paths  []phrasePath
		slop   int
	}{
		// search non palyndrone, exact match
		{
			phrase: [][]string{{"two"}, {"three"}},
			slop:   0,
			paths: []phrasePath{
				{
					phrasePart{"two", &search.Location{Pos: 2}},
					phrasePart{"three", &search.Location{Pos: 3}},
				},
			},
		},
		// same with slop 2 (not required) (find it twice)
		{
			phrase: [][]string{{"two"}, {"three"}},
			slop:   2,
			paths: []phrasePath{
				{
					phrasePart{"two", &search.Location{Pos: 2}},
					phrasePart{"three", &search.Location{Pos: 3}},
				},
				{
					phrasePart{"two", &search.Location{Pos: 4}},
					phrasePart{"three", &search.Location{Pos: 3}},
				},
			},
		},
		// palyndrone reversed
		{
			phrase: [][]string{{"three"}, {"two"}},
			slop:   2,
			paths: []phrasePath{
				{
					phrasePart{"three", &search.Location{Pos: 3}},
					phrasePart{"two", &search.Location{Pos: 2}},
				},
				{
					phrasePart{"three", &search.Location{Pos: 3}},
					phrasePart{"two", &search.Location{Pos: 4}},
				},
			},
		},
	}

	for i, test := range tests {
		actualPaths := findPhrasePaths(0, test.phrase, tlm, nil, test.slop, nil)
		if !reflect.DeepEqual(actualPaths, test.paths) {
			t.Fatalf("expected: %v got %v for test %d", test.paths, actualPaths, i)
		}
	}
}

func TestFindMultiPhrasePaths(t *testing.T) {
	tlm := search.TermLocationMap{
		"cat": search.Locations{
			&search.Location{
				Pos: 1,
			},
		},
		"dog": search.Locations{
			&search.Location{
				Pos: 2,
			},
		},
		"frog": search.Locations{
			&search.Location{
				Pos: 3,
			},
		},
	}

	tests := []struct {
		phrase [][]string
		paths  []phrasePath
	}{
		// simplest, one of two possible terms matches
		{
			phrase: [][]string{{"cat", "rat"}, {"dog"}},
			paths: []phrasePath{
				{
					phrasePart{"cat", &search.Location{Pos: 1}},
					phrasePart{"dog", &search.Location{Pos: 2}},
				},
			},
		},
		// two possible terms, neither work
		{
			phrase: [][]string{{"cat", "rat"}, {"chicken"}},
		},
		// two possible terms, one works, but out of position with next
		{
			phrase: [][]string{{"cat", "rat"}, {"frog"}},
		},
		// matches multiple times, with different pairing
		{
			phrase: [][]string{{"cat", "dog"}, {"dog", "frog"}},
			paths: []phrasePath{
				{
					phrasePart{"cat", &search.Location{Pos: 1}},
					phrasePart{"dog", &search.Location{Pos: 2}},
				},
				{
					phrasePart{"dog", &search.Location{Pos: 2}},
					phrasePart{"frog", &search.Location{Pos: 3}},
				},
			},
		},
		// multi-match over a gap
		{
			phrase: [][]string{{"cat", "rat"}, {""}, {"frog"}},
			paths: []phrasePath{
				{
					phrasePart{"cat", &search.Location{Pos: 1}},
					phrasePart{"frog", &search.Location{Pos: 3}},
				},
			},
		},
		// multi-match over a gap (same as before, but with empty term list)
		{
			phrase: [][]string{{"cat", "rat"}, {}, {"frog"}},
			paths: []phrasePath{
				{
					phrasePart{"cat", &search.Location{Pos: 1}},
					phrasePart{"frog", &search.Location{Pos: 3}},
				},
			},
		},
		// multi-match over a gap (same once again, but nil term list)
		{
			phrase: [][]string{{"cat", "rat"}, nil, {"frog"}},
			paths: []phrasePath{
				{
					phrasePart{"cat", &search.Location{Pos: 1}},
					phrasePart{"frog", &search.Location{Pos: 3}},
				},
			},
		},
	}

	for i, test := range tests {
		actualPaths := findPhrasePaths(0, test.phrase, tlm, nil, 0, nil)
		if !reflect.DeepEqual(actualPaths, test.paths) {
			t.Fatalf("expected: %v got %v for test %d", test.paths, actualPaths, i)
		}
	}
}
