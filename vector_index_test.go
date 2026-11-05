// Copyright (c) 2026 The AtomAI.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package riot

import (
	"fmt"
	"testing"
	"time"

	"github.com/vcaesar/riot/index"
)

// TestWithVectorIndexPersistsGraphsWithoutSearch checks that a declared
// vector index persists HNSW graphs for indexed segments before any search.
func TestWithVectorIndexPersistsGraphsWithoutSearch(t *testing.T) {
	path := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, path)

	w, err := OpenWriter(DefaultConfig(path).WithVectorIndex("v", L2, ANNParams{M: 4, EfConstruction: 20}))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	}()
	b := NewBatch()
	for i := range 20 {
		d := vectorDocument(t, fmt.Sprint(i), []float32{float32(i), 1})
		b.Update(d.ID(), d)
	}
	if err := w.Batch(b); err != nil {
		t.Fatal(err)
	}

	dir := index.NewFileSystemDirectory(path)
	deadline := time.Now().Add(30 * time.Second)
	for {
		ids, err := dir.List(index.ItemKindGraph)
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("no graph persisted for the declared vector index")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
