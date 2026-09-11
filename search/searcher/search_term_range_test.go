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

func TestTermRangeSearch(t *testing.T) {
	tests := []struct {
		min          []byte
		max          []byte
		inclusiveMin bool
		inclusiveMax bool
		field        string
		want         []uint64
	}{
		{
			min:          []byte("marty"),
			max:          []byte("marty"),
			field:        "name",
			inclusiveMin: true,
			inclusiveMax: true,
			want: []uint64{
				baseTestIndexReaderDirect.docNumByID("1"),
			},
		},
		{
			min:          []byte("marty"),
			max:          []byte("ravi"),
			field:        "name",
			inclusiveMin: true,
			inclusiveMax: true,
			want: []uint64{
				baseTestIndexReaderDirect.docNumByID("1"),
				baseTestIndexReaderDirect.docNumByID("4"),
			},
		},
		// inclusive max false should exclude ravi
		{
			min:          []byte("marty"),
			max:          []byte("ravi"),
			field:        "name",
			inclusiveMin: true,
			inclusiveMax: false,
			want: []uint64{
				baseTestIndexReaderDirect.docNumByID("1"),
			},
		},
		// inclusive max false should remove last/only item
		{
			min:          []byte("martz"),
			max:          []byte("ravi"),
			field:        "name",
			inclusiveMin: true,
			inclusiveMax: false,
			want:         nil,
		},
		// inclusive min false should remove marty
		{
			min:          []byte("marty"),
			max:          []byte("ravi"),
			field:        "name",
			inclusiveMin: false,
			inclusiveMax: true,
			want: []uint64{
				baseTestIndexReaderDirect.docNumByID("4"),
			},
		},
		// inclusive min false should remove first/only item
		{
			min:          []byte("marty"),
			max:          []byte("rav"),
			field:        "name",
			inclusiveMin: false,
			inclusiveMax: true,
			want:         nil,
		},
		// max nil sees everything after marty
		{
			min:          []byte("marty"),
			max:          nil,
			field:        "name",
			inclusiveMin: true,
			inclusiveMax: true,
			want: []uint64{
				baseTestIndexReaderDirect.docNumByID("1"),
				baseTestIndexReaderDirect.docNumByID("2"),
				baseTestIndexReaderDirect.docNumByID("4"),
			},
		},
		// min nil sees everything before ravi
		{
			min:          nil,
			max:          []byte("ravi"),
			field:        "name",
			inclusiveMin: true,
			inclusiveMax: true,
			want: []uint64{
				baseTestIndexReaderDirect.docNumByID("1"),
				baseTestIndexReaderDirect.docNumByID("3"),
				baseTestIndexReaderDirect.docNumByID("4"),
				baseTestIndexReaderDirect.docNumByID("5"),
			},
		},
		// min and max nil sees everything
		{
			min:          nil,
			max:          nil,
			field:        "name",
			inclusiveMin: true,
			inclusiveMax: true,
			want: []uint64{
				baseTestIndexReaderDirect.docNumByID("1"),
				baseTestIndexReaderDirect.docNumByID("2"),
				baseTestIndexReaderDirect.docNumByID("3"),
				baseTestIndexReaderDirect.docNumByID("4"),
				baseTestIndexReaderDirect.docNumByID("5"),
			},
		},
		// min and max nil sees everything, even with inclusiveMin false
		{
			min:          nil,
			max:          nil,
			field:        "name",
			inclusiveMin: false,
			inclusiveMax: true,
			want: []uint64{
				baseTestIndexReaderDirect.docNumByID("1"),
				baseTestIndexReaderDirect.docNumByID("2"),
				baseTestIndexReaderDirect.docNumByID("3"),
				baseTestIndexReaderDirect.docNumByID("4"),
				baseTestIndexReaderDirect.docNumByID("5"),
			},
		},
		// min and max nil sees everything, even with inclusiveMax false
		{
			min:          nil,
			max:          nil,
			field:        "name",
			inclusiveMin: true,
			inclusiveMax: false,
			want: []uint64{
				baseTestIndexReaderDirect.docNumByID("1"),
				baseTestIndexReaderDirect.docNumByID("2"),
				baseTestIndexReaderDirect.docNumByID("3"),
				baseTestIndexReaderDirect.docNumByID("4"),
				baseTestIndexReaderDirect.docNumByID("5"),
			},
		},
		// min and max nil sees everything, even with both false
		{
			min:          nil,
			max:          nil,
			field:        "name",
			inclusiveMin: false,
			inclusiveMax: false,
			want: []uint64{
				baseTestIndexReaderDirect.docNumByID("1"),
				baseTestIndexReaderDirect.docNumByID("2"),
				baseTestIndexReaderDirect.docNumByID("3"),
				baseTestIndexReaderDirect.docNumByID("4"),
				baseTestIndexReaderDirect.docNumByID("5"),
			},
		},
		// min and max non-nil, but match 0 terms
		{
			min:          []byte("martz"),
			max:          []byte("rav"),
			field:        "name",
			inclusiveMin: true,
			inclusiveMax: true,
			want:         nil,
		},
		// min and max same (and term exists), both exlusive
		{
			min:          []byte("marty"),
			max:          []byte("marty"),
			field:        "name",
			inclusiveMin: false,
			inclusiveMax: false,
			want:         nil,
		},
	}

	for _, test := range tests {
		searcher, err := NewTermRangeSearcher(baseTestIndexReader, test.min, test.max,
			test.inclusiveMin, test.inclusiveMax, test.field,
			1.0, nil, similarity.NewCompositeSumScorer(), testSearchOptions)
		if err != nil {
			t.Fatal(err)
		}

		var got []uint64
		ctx := &search.Context{
			DocumentMatchPool: search.NewDocumentMatchPool(
				searcher.DocumentMatchPoolSize(), 0),
		}
		next, err := searcher.Next(ctx)
		i := 0
		for err == nil && next != nil {
			got = append(got, next.Number)
			ctx.DocumentMatchPool.Put(next)
			next, err = searcher.Next(ctx)
			i++
		}
		if err != nil {
			t.Fatalf("error iterating searcher: %v", err)
		}
		if !reflect.DeepEqual(got, test.want) {
			t.Errorf("expected: %v, got %v for test %#v", test.want, got, test)
		}
	}
}
