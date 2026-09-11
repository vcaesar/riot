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

func TestRegexpStringSearchScorch(t *testing.T) {
	regexpSearcher, err := NewRegexpStringSearcher(baseTestIndexReader,
		"ma.*", "name", 1.0, nil,
		similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}

	regexpSearcherCo, err := NewRegexpStringSearcher(baseTestIndexReader,
		"co.*", "desc", 1.0, nil,
		similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		searcher  search.Searcher
		num2score map[uint64]float64
	}{
		{
			searcher: regexpSearcher,
			num2score: map[uint64]float64{
				baseTestIndexReaderDirect.docNumByID("1"): 0.7608983788962145,
			},
		},
		{
			searcher: regexpSearcherCo,
			num2score: map[uint64]float64{
				baseTestIndexReaderDirect.docNumByID("2"): 1.0935524440417956,
				baseTestIndexReaderDirect.docNumByID("3"): 1.0935524440417956,
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
			if _, ok := test.num2score[next.Number]; !ok {
				t.Errorf("test %d, found unexpected number = %d, next = %#v", testIndex, next.Number, next)
			} else {
				score := test.num2score[next.Number]
				if next.Score != score {
					t.Errorf("test %d, expected result %d to have score %v got %v,next: %#v",
						testIndex, i, score, next.Score, next)
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
		if len(test.num2score) != i {
			t.Errorf("expected %d results got %d for test %d", len(test.num2score), i, testIndex)
		}
	}
}
