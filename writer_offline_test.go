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
	"fmt"
	"testing"
)

func TestOfflineWriter(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	b, err := OpenOfflineWriter(config, 2, 2)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 10; i++ {
		doc := NewDocument(fmt.Sprintf("%d", i)).
			AddField(NewKeywordField("name", "hello"))
		err = b.Insert(doc)
		if err != nil {
			t.Fatal(err)
		}
	}

	err = b.Close()
	if err != nil {
		t.Fatal(err)
	}

	indexReader, err := OpenReader(config)
	if err != nil {
		t.Fatalf("error opening index: %v", err)
	}
	defer func() {
		err = indexReader.Close()
		if err != nil {
			t.Errorf("error closing index: %v", err)
		}
	}()

	docCount, err := indexReader.Count()
	if err != nil {
		t.Errorf("error checking doc count: %v", err)
	}
	if docCount != 10 {
		t.Errorf("expected doc count to be 10, got %d", docCount)
	}

	q := NewTermQuery("hello")
	q.SetField("name")
	req := NewTopNSearch(10, q).WithStandardAggregations()
	res, err := indexReader.Search(context.Background(), req)
	if err != nil {
		t.Errorf("error searching index: %v", err)
	}
	if res.Aggregations().Count() != 10 {
		t.Errorf("expected 10 search hits, got %d", res.Aggregations().Count())
	}
}
