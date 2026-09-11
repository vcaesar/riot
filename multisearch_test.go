//  Copyright (c) 2020 The Bluge Authors.
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
	"testing"
)

func TestMultiSearch(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter1, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	doc := NewDocument("a").
		AddField(NewKeywordField("name", "index-a"))

	err = indexWriter1.Update(doc.ID(), doc)
	if err != nil {
		t.Fatalf("error updating: %v", err)
	}

	indexReader1, err := indexWriter1.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	tmpIndexPath2 := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath2)

	config2 := DefaultConfig(tmpIndexPath2)
	indexWriter2, err := OpenWriter(config2)
	if err != nil {
		t.Fatal(err)
	}

	doc2 := NewDocument("b").
		AddField(NewKeywordField("name", "index-b"))

	err = indexWriter2.Update(doc.ID(), doc2)
	if err != nil {
		t.Fatalf("error updating: %v", err)
	}

	indexReader2, err := indexWriter2.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	q := NewPrefixQuery("index-").SetField("name")
	req := NewTopNSearch(10, q).WithStandardAggregations()

	dmi, err := MultiSearch(context.Background(), req, indexReader1, indexReader2)
	if err != nil {
		t.Fatalf("error starting multisearch: %v", err)
	}
	var hitCount int
	next, err := dmi.Next()
	for err == nil && next != nil {
		hitCount++
		next, err = dmi.Next()
	}
	if err != nil {
		t.Fatalf("error iterating results")
	}

	if hitCount != 2 {
		t.Errorf("expected 2 hits, got %d", hitCount)
	}

	err = indexReader1.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = indexReader2.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = indexWriter1.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = indexWriter2.Close()
	if err != nil {
		t.Fatal(err)
	}
}
