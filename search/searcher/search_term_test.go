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

	segment "github.com/vcaesar/bluge_segment_api"

	"github.com/vcaesar/riot/search"
)

func TestTermSearcher(t *testing.T) {
	var queryTerm = "beer"
	var queryField = "desc"
	var queryBoost = 3.0

	docs := []segment.Document{
		&FakeDocument{
			NewFakeField("_id", "a", true, false, false, nil),
			NewFakeField("desc", "beer", false, false, true, nil),
		},
		&FakeDocument{
			NewFakeField("_id", "b", true, false, false, nil),
			NewFakeField("desc", "beer", false, false, true, nil),
		},
		&FakeDocument{
			NewFakeField("_id", "c", true, false, false, nil),
			NewFakeField("desc", "beer", false, false, true, nil),
		},
		&FakeDocument{
			NewFakeField("_id", "d", true, false, false, nil),
			NewFakeField("desc", "beer", false, false, true, nil),
		},
		&FakeDocument{
			NewFakeField("_id", "e", true, false, false, nil),
			NewFakeField("desc", "beer", false, false, true, nil),
		},
		&FakeDocument{
			NewFakeField("_id", "f", true, false, false, nil),
			NewFakeField("desc", "beer", false, false, true, nil),
		},
		&FakeDocument{
			NewFakeField("_id", "g", true, false, false, nil),
			NewFakeField("desc", "beer", false, false, true, nil),
		},
		&FakeDocument{
			NewFakeField("_id", "h", true, false, false, nil),
			NewFakeField("desc", "beer", false, false, true, nil),
		},
		&FakeDocument{
			NewFakeField("_id", "i", true, false, false, nil),
			NewFakeField("desc", "beer", false, false, true, nil),
		},
		&FakeDocument{
			NewFakeField("_id", "j", true, false, false, nil),
			NewFakeField("title", "cat", false, false, true, nil),
		},
	}

	indexReader := newStubIndexReader()
	for _, doc := range docs {
		indexReader.add(doc)
	}

	searcher, err := NewTermSearcher(indexReader, queryTerm, queryField, queryBoost, nil, testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err = searcher.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()

	if searcher.Count() != 9 {
		t.Errorf("expected count of 9, got %d", searcher.Count())
	}

	ctx := &search.Context{
		DocumentMatchPool: search.NewDocumentMatchPool(1, 0),
	}
	docMatch, err := searcher.Next(ctx)
	if err != nil {
		t.Errorf("expected result, got %v", err)
	}
	numberA := indexReader.docNumByID("a")
	if docMatch.Number != numberA {
		t.Errorf("expected result number to be %d, got %d", numberA, docMatch.Number)
	}
	ctx.DocumentMatchPool.Put(docMatch)
	docMatch, err = searcher.Advance(ctx, indexReader.docNumByID("c"))
	if err != nil {
		t.Errorf("expected result, got %v", err)
	}
	numberC := indexReader.docNumByID("c")
	if docMatch.Number != numberC {
		t.Errorf("expected result number to be %d got %d", numberC, docMatch.Number)
	}

	// try advancing past end
	ctx.DocumentMatchPool.Put(docMatch)
	docMatch, err = searcher.Advance(ctx, 999)
	if err != nil {
		t.Fatal(err)
	}
	if docMatch != nil {
		t.Errorf("expected nil, got %v", docMatch)
	}

	// try pushing next past end
	ctx.DocumentMatchPool.Put(docMatch)
	docMatch, err = searcher.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if docMatch != nil {
		t.Errorf("expected nil, got %v", docMatch)
	}
}
