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
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"github.com/vcaesar/riot/index"
)

// TestSearchVectorsANNPersistedGraphs reopens a disk index and checks that
// graphs persisted by a writer are reused by later writers and readers.
func TestSearchVectorsANNPersistedGraphs(t *testing.T) {
	path := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, path)
	config := DefaultConfig(path)

	rng := rand.New(rand.NewSource(9))
	random := func() []float32 {
		v := make([]float32, 6)
		for i := range v {
			v[i] = float32(rng.NormFloat64())
		}
		return v
	}
	w, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}
	b := NewBatch()
	for i := range 60 {
		d := vectorDocument(t, fmt.Sprint(i), random())
		b.Update(d.ID(), d)
	}
	if err := w.Batch(b); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	query := random()
	params := ANNParams{M: 4, EfConstruction: 20, EfSearch: 100}
	search := func(r *Reader) []VectorMatch {
		t.Helper()
		defer func() {
			if err := r.Close(); err != nil {
				t.Error(err)
			}
		}()
		matches, err := r.SearchVectorsANN(context.Background(), "v", query, 5, L2, params, nil)
		if err != nil {
			t.Fatal(err)
		}
		return matches
	}
	graphs := func() []uint64 {
		t.Helper()
		ids, err := index.NewFileSystemDirectory(path).List(index.ItemKindGraph)
		if err != nil {
			t.Fatal(err)
		}
		return ids
	}

	// Segments loaded from disk by a writer persist the graphs they build.
	var want []VectorMatch
	for round := range 2 {
		w, err = OpenWriter(config)
		if err != nil {
			t.Fatal(err)
		}
		r, err := w.Reader()
		if err != nil {
			t.Fatal(err)
		}
		got := search(r)
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if round == 0 {
			want = got
		} else if !reflect.DeepEqual(got, want) {
			t.Fatalf("reopened search %v, want %v", got, want)
		}
		if len(graphs()) == 0 {
			t.Fatal("expected a persisted graph item")
		}
	}

	before := graphs()
	r, err := OpenReader(config)
	if err != nil {
		t.Fatal(err)
	}
	if got := search(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("read-only search %v, want %v", got, want)
	}
	if after := graphs(); !reflect.DeepEqual(after, before) {
		t.Fatalf("read-only reader changed graph items: %x -> %x", before, after)
	}
}
