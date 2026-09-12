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

package riot

import (
	"context"
	"errors"
	"math"
	"os"
	"reflect"
	"sort"
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

func knnIDs(t *testing.T, r *Reader, q Query, explain bool) (ids []string, scores []float64) {
	t.Helper()
	req := NewTopNSearch(10, q)
	if explain {
		req = req.ExplainScores()
	}
	itr, err := r.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	for {
		m, err := itr.Next()
		if err != nil {
			t.Fatal(err)
		}
		if m == nil {
			return ids, scores
		}
		if explain && m.Explanation == nil {
			t.Fatal("missing explanation")
		}
		var id string
		if err := m.VisitStoredFields(func(name string, value []byte) bool {
			if name == "_id" {
				id = string(value)
			}
			return true
		}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		scores = append(scores, m.Score)
	}
}

func TestKNNQuery(t *testing.T) {
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	}()
	for _, tc := range []struct {
		id, color string
		vector    []float32
	}{
		{"a", "red", []float32{1, 0}},
		{"b", "blue", []float32{3, 0}},
		{"c", "red", []float32{2, 0}},
		{"d", "blue", []float32{-1, 0}},
	} {
		d := vectorDocument(t, tc.id, tc.vector).AddField(NewTextField("color", tc.color))
		if err := w.Update(d.ID(), d); err != nil {
			t.Fatal(err)
		}
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	}()

	t.Run("top k by dot product with boost", func(t *testing.T) {
		q := NewKNNQuery("v", []float32{1, 0}, 2).SetMetric(DotProduct).SetBoost(2)
		ids, scores := knnIDs(t, r, q, true)
		if !reflect.DeepEqual(ids, []string{"b", "c"}) || !reflect.DeepEqual(scores, []float64{6, 4}) {
			t.Fatalf("got %v %v", ids, scores)
		}
	})
	t.Run("hybrid must with text query", func(t *testing.T) {
		q := NewBooleanQuery().
			AddMust(NewKNNQuery("v", []float32{1, 0}, 3).SetMetric(DotProduct)).
			AddMust(NewTermQuery("red").SetField("color"))
		ids, _ := knnIDs(t, r, q, false)
		if !reflect.DeepEqual(ids, []string{"c", "a"}) {
			t.Fatalf("got %v", ids)
		}
	})
	t.Run("should with text query sums scores", func(t *testing.T) {
		q := NewBooleanQuery().
			AddShould(NewKNNQuery("v", []float32{1, 0}, 1).SetMetric(DotProduct)).
			AddShould(NewTermQuery("red").SetField("color"))
		ids, _ := knnIDs(t, r, q, false)
		if len(ids) != 3 || ids[0] != "b" {
			t.Fatalf("got %v", ids)
		}
	})
	t.Run("cosine default", func(t *testing.T) {
		ids, scores := knnIDs(t, r, NewKNNQuery("v", []float32{1, 0}, 4), false)
		if len(ids) != 4 {
			t.Fatalf("got %v %v", ids, scores)
		}
		// a, b, c are collinear with the query: a three-way tie at 1, order unspecified.
		tied := append([]string(nil), ids[:3]...)
		sort.Strings(tied)
		if !reflect.DeepEqual(tied, []string{"a", "b", "c"}) || ids[3] != "d" ||
			!reflect.DeepEqual(scores, []float64{1, 1, 1, -1}) {
			t.Fatalf("got %v %v", ids, scores)
		}
	})
	t.Run("validate", func(t *testing.T) {
		for name, q := range map[string]*KNNQuery{
			"empty field":  NewKNNQuery("", []float32{1}, 1),
			"zero k":       NewKNNQuery("v", []float32{1}, 0),
			"empty vector": NewKNNQuery("v", nil, 1),
			"bad metric":   NewKNNQuery("v", []float32{1}, 1).SetMetric("manhattan"),
			"cosine zero":  NewKNNQuery("v", []float32{0, 0}, 1),
			"nonfinite":    NewKNNQuery("v", []float32{float32(math.NaN())}, 1),
		} {
			if q.Validate() == nil {
				t.Errorf("%s: expected validation error", name)
			}
			if _, err := r.Search(context.Background(), NewTopNSearch(1, q)); err == nil {
				t.Errorf("%s: expected search error", name)
			}
		}
		if _, err := r.Search(context.Background(), NewTopNSearch(1, NewKNNQuery("v", []float32{1}, 1))); err == nil {
			t.Error("expected dimension mismatch error")
		}
	})
}

func openKNNReader(t *testing.T, config Config) *Reader {
	t.Helper()
	w, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	d := vectorDocument(t, "a", []float32{1, 0})
	if err := w.Update(d.ID(), d); err != nil {
		t.Fatal(err)
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	return r
}

func TestKNNQueryAdmissionBeforeScan(t *testing.T) {
	rejected := errors.New("rejected")
	r := openKNNReader(t, InMemoryOnlyConfig().WithSearchStartFunc(func(uint64) error { return rejected }))
	q := NewKNNQuery("v", []float32{1}, 1) // dimension mismatch: only fails if the scan runs
	if _, err := r.Search(context.Background(), NewTopNSearch(1, q)); !errors.Is(err, rejected) {
		t.Fatalf("admission must reject before the vector scan runs, got %v", err)
	}
}

func TestKNNQueryCancelled(t *testing.T) {
	r := openKNNReader(t, InMemoryOnlyConfig())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := r.Search(ctx, NewTopNSearch(1, NewKNNQuery("v", []float32{1, 0}, 1)))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
