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

package index

import (
	"strconv"
	"testing"
	"time"

	"github.com/vcaesar/ice/vec"

	"github.com/vcaesar/riot/hnsw"
)

func TestVectorIndexValidate(t *testing.T) {
	tests := []struct {
		name string
		v    VectorIndex
		ok   bool
	}{
		{"defaults", VectorIndex{Field: "v", Metric: vec.L2}, true},
		{"custom", VectorIndex{Field: "v", Metric: vec.Cosine, Params: hnsw.Params{M: 8, EfConstruction: 50}}, true},
		{"no field", VectorIndex{Metric: vec.L2}, false},
		{"bad metric", VectorIndex{Field: "v", Metric: "manhattan"}, false},
		{"bad params", VectorIndex{Field: "v", Metric: vec.DotProduct, Params: hnsw.Params{M: 1}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.v.validate(); (err == nil) != tt.ok {
				t.Fatalf("validate() = %v, want ok %v", err, tt.ok)
			}
		})
	}
}

func TestVectorIndexKeyMatchesSearch(t *testing.T) {
	v := VectorIndex{Field: "v", Metric: vec.L2, Params: hnsw.Params{EfSearch: 7, ConcurrentAccept: true}}
	if v.key() != testGraphKey {
		t.Fatalf("key %v, want the search key %v", v.key(), testGraphKey)
	}
}

func TestWithVectorIndexDoesNotAlias(t *testing.T) {
	base := InMemoryOnlyConfig().WithVectorIndex("a", vec.L2, hnsw.Params{})
	a := base.WithVectorIndex("b", vec.L2, hnsw.Params{})
	b := base.WithVectorIndex("c", vec.L2, hnsw.Params{})
	if len(base.VectorIndexes) != 1 || a.VectorIndexes[1].Field != "b" || b.VectorIndexes[1].Field != "c" {
		t.Fatalf("configs share declarations: %v %v %v", base.VectorIndexes, a.VectorIndexes, b.VectorIndexes)
	}
}

func TestOpenWriterRejectsInvalidVectorIndex(t *testing.T) {
	if _, err := OpenWriter(InMemoryOnlyConfig().WithVectorIndex("", vec.L2, hnsw.Params{})); err == nil {
		t.Fatal("invalid vector index accepted")
	}
}

// waitGraphs waits until every segment of the writer's root has a built
// graph for key, without searching.
func waitGraphs(t *testing.T, idx *Writer, key graphKey) int {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		root := idx.currentSnapshot()
		built := 0
		for _, ss := range root.segment {
			ss.segment.graphMu.Lock()
			e, ok := ss.segment.graphs[key]
			ss.segment.graphMu.Unlock()
			if !ok {
				continue
			}
			select {
			case <-e.ready:
				if e.err != nil {
					t.Fatalf("graph build failed: %v", e.err)
				}
				if !e.abandoned {
					built++
				}
			default:
			}
		}
		n := len(root.segment)
		if err := root.Close(); err != nil {
			t.Fatal(err)
		}
		if n > 0 && built == n {
			return n
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d segment graphs built", built, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestDeclaredVectorIndexBuildsAtIndexTime checks that a declared vector
// index builds graphs for new segments before any search.
func TestDeclaredVectorIndexBuildsAtIndexTime(t *testing.T) {
	cfg := InMemoryOnlyConfig().WithNormCalc(func(string, int) float32 { return 1 }).
		WithVectorIndex("v", vec.L2, hnsw.Params{})
	idx, err := OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := idx.Close(); err != nil {
			t.Error(err)
		}
	}()
	for i := range 3 {
		encoded, err := vec.Encode([]float32{float32(i), 1})
		if err != nil {
			t.Fatal(err)
		}
		id := strconv.Itoa(i)
		b := NewBatch()
		b.Update(testIdentifier(id), &FakeDocument{
			NewFakeField("_id", id, true, false, false),
			NewFakeField("v", string(encoded), true, false, false),
		})
		if err = idx.Batch(b); err != nil {
			t.Fatal(err)
		}
	}
	waitGraphs(t, idx, testGraphKey)

	r, err := idx.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	}()
	got, err := r.SearchVectorsANN(t.Context(), "v", []float32{2, 1}, 1, vec.L2, hnsw.Params{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Score != 0 {
		t.Fatalf("nearest match %v, want exact hit", got)
	}
}
