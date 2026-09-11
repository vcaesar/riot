// Copyright (c) 2026 The Bluge Authors.
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

package bluge

import (
	"context"
	"math"
	"os"
	"reflect"
	"testing"

	"github.com/vcaesar/ice/vec"
)

func TestNewVectorField(t *testing.T) {
	input := []float32{1, 2}
	f, err := NewVectorField("embedding", input)
	if err != nil {
		t.Fatal(err)
	}
	input[0] = 99
	decoded, err := vec.Decode(f.Value())
	if err != nil || !reflect.DeepEqual(decoded, []float32{1, 2}) {
		t.Fatalf("encoding: %v %v", decoded, err)
	}
	if f.Name() != "embedding" || f.FieldOptions != Store || f.NumPlainTextBytes() != 0 {
		t.Fatalf("field: %+v", f)
	}
	if pos := f.Analyze(7); pos != 7 || f.Length() != 0 || len(f.AnalyzedTokenFrequencies()) != 0 {
		t.Fatal("vector leaked tokens")
	}
	composite := NewCompositeField("all", true, nil, nil)
	NewDocument("a").AddField(f).AddField(composite).Analyze()
	if composite.Length() != 1 {
		t.Fatalf("composite length: %d", composite.Length())
	}
	for _, vector := range [][]float32{nil, {}, {float32(math.NaN())}, {float32(math.Inf(1))}, {float32(math.Inf(-1))}} {
		if _, err := NewVectorField("v", vector); err == nil {
			t.Fatalf("accepted %v", vector)
		}
	}
	if _, err := NewVectorField("", []float32{1}); err == nil {
		t.Fatal("accepted empty name")
	}
	stored := NewStoredOnlyField("raw", []byte("not a term"))
	stored.Analyze(0)
	if stored.Length() != 0 {
		t.Fatal("stored-only field leaked tokens")
	}
}

func vectorDocument(t *testing.T, id string, vectors ...[]float32) *Document {
	t.Helper()
	d := NewDocument(id)
	for _, v := range vectors {
		f, err := NewVectorField("v", v)
		if err != nil {
			t.Fatal(err)
		}
		d.AddField(f)
	}
	return d
}

func checkVectorIDs(t *testing.T, r *Reader, want []string, scores []float64) {
	t.Helper()
	matches, err := r.SearchVectors(context.Background(), "v", []float32{1, 0}, 10, DotProduct, nil)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	var gotScores []float64
	for _, m := range matches {
		var id string
		if err := r.VisitStoredFields(m.Number, func(name string, value []byte) bool {
			if name == "_id" {
				id = string(value)
			}
			return true
		}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		gotScores = append(gotScores, m.Score)
	}
	if !reflect.DeepEqual(ids, want) || !reflect.DeepEqual(gotScores, scores) {
		t.Fatalf("got %v %v, want %v %v", ids, gotScores, want, scores)
	}
}

func TestVectorReaderLifecycle(t *testing.T) {
	if err := os.MkdirAll("test", 0755); err != nil {
		t.Fatal(err)
	}
	path, err := os.MkdirTemp("test", "vector-")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupTmpIndexPath(t, path)
	config := DefaultConfig(path)
	w, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if w != nil {
			if err := w.Close(); err != nil {
				t.Error(err)
			}
		}
	}()
	for _, d := range []*Document{vectorDocument(t, "a", []float32{1, 0}, []float32{4, 0}), vectorDocument(t, "b", []float32{2, 0})} {
		if err := w.Update(d.ID(), d); err != nil {
			t.Fatal(err)
		}
	}
	old, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := old.Close(); err != nil {
			t.Error(err)
		}
	}()
	checkVectorIDs(t, old, []string{"a", "b"}, []float64{4, 2})
	matches, err := old.SearchVectors(context.Background(), "v", []float32{1, 0}, 1, DotProduct, func(n uint64) bool {
		var id string
		if err := old.VisitStoredFields(n, func(name string, value []byte) bool {
			if name == "_id" {
				id = string(value)
			}
			return true
		}); err != nil {
			t.Error(err)
		}
		return id == "b"
	})
	if err != nil || len(matches) != 1 || matches[0].Score != 2 {
		t.Fatalf("filter: %v %v", matches, err)
	}
	updated := vectorDocument(t, "a", []float32{-1, 0})
	if err := w.Update(updated.ID(), updated); err != nil {
		t.Fatal(err)
	}
	if err := w.Delete(Identifier("b")); err != nil {
		t.Fatal(err)
	}
	current, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	checkVectorIDs(t, current, []string{"a"}, []float64{-1})
	checkVectorIDs(t, old, []string{"a", "b"}, []float64{4, 2})
	if err := current.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w = nil
	reopened, err := OpenReader(config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	}()
	checkVectorIDs(t, reopened, []string{"a"}, []float64{-1})
	if _, err := reopened.SearchVectors(context.Background(), "v", []float32{1}, 1, L2, nil); err == nil {
		t.Fatal("accepted dimension mismatch")
	}
}
