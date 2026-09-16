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

package hnsw

import (
	"math"
	"math/rand"
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/vcaesar/ice/vec"
)

func randomVectors(rng *rand.Rand, n, dim int) [][]float32 {
	vectors := make([][]float32, n)
	for i := range vectors {
		vectors[i] = make([]float32, dim)
		for j := range vectors[i] {
			vectors[i][j] = float32(rng.NormFloat64())
		}
	}
	return vectors
}

func exact(t *testing.T, vectors [][]float32, query []float32, k int, metric vec.Metric, accept func(uint64) bool) []vec.Match {
	t.Helper()
	var all []vec.Match
	for i, v := range vectors {
		if accept != nil && !accept(uint64(i)) {
			continue
		}
		score, err := vec.Score(query, v, metric)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, vec.Match{Number: uint64(i), Score: score})
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].Score > all[j].Score || (all[i].Score == all[j].Score && all[i].Number < all[j].Number)
	})
	if len(all) > k {
		all = all[:k]
	}
	return all
}

func build(t *testing.T, metric vec.Metric, params Params, vectors [][]float32) *Graph {
	t.Helper()
	g, err := New(metric, params)
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range vectors {
		if err := g.Add(uint64(i), v); err != nil {
			t.Fatal(err)
		}
	}
	return g
}

func recall(got, want []vec.Match) float64 {
	wanted := make(map[uint64]struct{}, len(want))
	for _, m := range want {
		wanted[m.Number] = struct{}{}
	}
	hits := 0
	for _, m := range got {
		if _, ok := wanted[m.Number]; ok {
			hits++
		}
	}
	return float64(hits) / float64(len(want))
}

func TestGraphRecall(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	vectors := randomVectors(rng, 2000, 16)
	queries := randomVectors(rng, 20, 16)
	for _, metric := range []vec.Metric{vec.Cosine, vec.DotProduct, vec.L2} {
		g := build(t, metric, Params{}, vectors)
		var total float64
		for _, q := range queries {
			want := exact(t, vectors, q, 10, metric, nil)
			got, err := g.Search(q, 10, 0, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 10 {
				t.Fatalf("%s: got %d matches", metric, len(got))
			}
			for i := 1; i < len(got); i++ {
				if got[i-1].Score < got[i].Score {
					t.Fatalf("%s: results not sorted: %v", metric, got)
				}
			}
			for _, m := range got {
				score, err := vec.Score(q, vectors[m.Number], metric)
				if err != nil || score != m.Score {
					t.Fatalf("%s: score for %d is %v, exact %v %v", metric, m.Number, m.Score, score, err)
				}
			}
			total += recall(got, want)
		}
		if avg := total / float64(len(queries)); avg < 0.9 {
			t.Fatalf("%s: recall@10 %.2f", metric, avg)
		}
	}
}

func TestGraphSmallIsExact(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	vectors := randomVectors(rng, 50, 4)
	g := build(t, vec.L2, Params{M: 4, EfConstruction: 8}, vectors)
	for _, q := range randomVectors(rng, 10, 4) {
		got, err := g.Search(q, 5, 100, nil)
		if err != nil {
			t.Fatal(err)
		}
		if want := exact(t, vectors, q, 5, vec.L2, nil); !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestGraphAcceptFilter(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	vectors := randomVectors(rng, 1000, 8)
	g := build(t, vec.DotProduct, Params{}, vectors)
	accept := func(n uint64) bool { return n%50 == 0 } // 20 of 1000 live
	q := vectors[0]
	got, err := g.Search(q, 5, 0, accept)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("selective filter starved results: %v", got)
	}
	for _, m := range got {
		if !accept(m.Number) {
			t.Fatalf("returned rejected document %d", m.Number)
		}
	}
	if want := exact(t, vectors, q, 5, vec.DotProduct, accept); recall(got, want) < 0.8 {
		t.Fatalf("filtered recall too low: got %v, want %v", got, want)
	}
	none, err := g.Search(q, 5, 0, func(uint64) bool { return false })
	if err != nil || len(none) != 0 {
		t.Fatalf("reject-all: %v %v", none, err)
	}
}

func TestGraphRepeatedIDs(t *testing.T) {
	g, err := New(vec.DotProduct, Params{M: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		id uint64
		v  []float32
	}{{7, []float32{1, 0}}, {7, []float32{4, 0}}, {3, []float32{2, 0}}, {7, []float32{3, 0}}} {
		if err := g.Add(v.id, v.v); err != nil {
			t.Fatal(err)
		}
	}
	got, err := g.Search([]float32{1, 0}, 10, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []vec.Match{{Number: 7, Score: 4}, {Number: 3, Score: 2}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if g.Len() != 4 {
		t.Fatalf("len %d", g.Len())
	}
}

func TestGraphTiesOrderByID(t *testing.T) {
	g := build(t, vec.DotProduct, Params{}, [][]float32{{1, 0}, {1, 0}, {1, 0}, {0, 1}})
	got, err := g.Search([]float32{1, 0}, 2, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []vec.Match{{Number: 0, Score: 1}, {Number: 1, Score: 1}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestGraphDeterministic(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	vectors := randomVectors(rng, 300, 8)
	a := build(t, vec.Cosine, Params{}, vectors)
	b := build(t, vec.Cosine, Params{}, vectors)
	if !reflect.DeepEqual(a.links, b.links) || a.entry != b.entry {
		t.Fatal("graph construction is not deterministic")
	}
}

func TestGraphConcurrentSearch(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	vectors := randomVectors(rng, 500, 8)
	g := build(t, vec.L2, Params{}, vectors)
	queries := randomVectors(rng, 8, 8)
	var wg sync.WaitGroup
	for _, q := range queries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			want := exact(t, vectors, q, 3, vec.L2, nil)
			got, err := g.Search(q, 3, 200, nil)
			if err != nil || recall(got, want) < 0.6 {
				t.Errorf("concurrent search: %v %v", got, err)
			}
		}()
	}
	wg.Wait()
}

func TestGraphValidation(t *testing.T) {
	for _, p := range []Params{{M: 1}, {M: -1}, {EfConstruction: -1}, {EfSearch: -1}} {
		if _, err := New(vec.L2, p); err == nil {
			t.Fatalf("accepted %+v", p)
		}
	}
	if _, err := New("manhattan", Params{}); err == nil {
		t.Fatal("accepted unknown metric")
	}
	if got := (Params{}).WithDefaults(); got != (Params{M: DefaultM, EfConstruction: DefaultEfConstruction, EfSearch: DefaultEfSearch}) {
		t.Fatalf("defaults: %+v", got)
	}
	g, err := New(vec.Cosine, Params{})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := g.Search([]float32{1}, 1, 0, nil); err != nil || got != nil {
		t.Fatalf("empty graph: %v %v", got, err)
	}
	for _, v := range [][]float32{nil, {0, 0}, {float32(math.NaN()), 1}} {
		if err := g.Add(0, v); err == nil {
			t.Fatalf("accepted %v", v)
		}
	}
	if err := g.Add(0, []float32{1, 0}); err != nil {
		t.Fatal(err)
	}
	if err := g.Add(1, []float32{1, 0, 0}); err == nil {
		t.Fatal("accepted dimension mismatch on add")
	}
	if _, err := g.Search([]float32{1, 0, 0}, 1, 0, nil); err == nil {
		t.Fatal("accepted dimension mismatch on search")
	}
	if _, err := g.Search([]float32{1, 0}, 0, 0, nil); err == nil {
		t.Fatal("accepted zero k")
	}
	if _, err := g.Search([]float32{0, 0}, 1, 0, nil); err == nil {
		t.Fatal("accepted zero cosine query")
	}
}
