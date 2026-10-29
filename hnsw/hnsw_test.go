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

package hnsw

import (
	"context"
	"errors"
	"fmt"
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

// Several vectors of one document must not fill the beam and starve others.
func TestGraphRepeatedIDsDoNotStarveBeam(t *testing.T) {
	g, err := New(vec.DotProduct, Params{})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		id uint64
		v  []float32
	}{{7, []float32{10, 0}}, {7, []float32{9, 0}}, {7, []float32{8, 0}}, {3, []float32{5, 0}}, {4, []float32{1, 0}}} {
		if err := g.Add(v.id, v.v); err != nil {
			t.Fatal(err)
		}
	}
	for _, accept := range []func(uint64) bool{nil, func(id uint64) bool { return id != 4 }} {
		got, err := g.Search([]float32{1, 0}, 2, 2, accept)
		if err != nil {
			t.Fatal(err)
		}
		if want := []vec.Match{{Number: 7, Score: 10}, {Number: 3, Score: 5}}; !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestCandHeapKeep(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		for _, ef := range []int{1, 4, 64} {
			t.Run(fmt.Sprintf("indexed=%v/ef=%d", indexed, ef), func(t *testing.T) {
				rng := rand.New(rand.NewSource(9))
				g := &Graph{ids: make([]uint64, 512)}
				for i := range g.ids {
					g.ids[i] = uint64(i) << 40
					if indexed {
						g.ids[i] = (rng.Uint64() % 32) << 40
					}
				}
				h := &candHeap{g: g}
				if indexed {
					h.ids = make(map[uint64]int)
				}
				check := func() {
					t.Helper()
					if len(h.items) > ef || (indexed && len(h.ids) != len(h.items)) {
						t.Fatalf("heap size %d, index size %d, ef %d", len(h.items), len(h.ids), ef)
					}
					for i, c := range h.items {
						if i > 0 && h.before(c, h.items[(i-1)/2]) {
							t.Fatalf("heap order violated at %d: %v", i, h.items)
						}
						if indexed {
							if pos, ok := h.ids[g.ids[c.node]]; !ok || pos != i {
								t.Fatalf("node %d at %d, index %d, present %v", c.node, i, pos, ok)
							}
						}
					}
				}
				best := make(map[uint64]cand)
				var want []cand
				for _, node := range rng.Perm(len(g.ids)) {
					// #nosec G115 -- node is a permutation index in the 512-element slice.
					c := cand{node: uint32(node), score: float64(rng.Intn(21) - 10)}
					id := g.ids[node]
					if old, ok := best[id]; !ok || g.better(c, old) {
						best[id] = c
					}
					h.keep(c, ef)
					check()
					want = want[:0]
					for _, c := range best {
						want = append(want, c)
					}
					sort.Slice(want, func(i, j int) bool { return g.better(want[i], want[j]) })
					want = want[:min(ef, len(want))]
					got := append([]cand(nil), h.items...)
					sort.Slice(got, func(i, j int) bool { return g.better(got[i], got[j]) })
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("after keeping %v: got %v, want %v", c, got, want)
					}
				}
				for i := len(want) - 1; i >= 0; i-- {
					if got := h.pop(); got != want[i] {
						t.Fatalf("pop: got %v, want %v", got, want[i])
					}
					check()
				}
			})
		}
	}
}

func TestGraphRepeatedIDsConcurrentSearch(t *testing.T) {
	g, err := New(vec.DotProduct, Params{})
	if err != nil {
		t.Fatal(err)
	}
	ids := []uint64{9, 4, 7, 9, 4, 2, 7}
	values := []float32{2, 4, 3, 6, 8, 8, 9}
	for i, id := range ids {
		if err := g.Add(id, []float32{values[i]}); err != nil {
			t.Fatal(err)
		}
	}
	g.entry, g.maxLevel = 0, 0
	g.links = [][][]uint32{{{1}}, {{2}}, {{3}}, {{4}}, {{5}}, {{6}}, {{}}}
	for _, tc := range []struct {
		name   string
		query  []float32
		accept func(uint64) bool
		want   []vec.Match
	}{
		{"replacement and reinsertion", []float32{1}, nil, []vec.Match{{Number: 7, Score: 9}, {Number: 2, Score: 8}}},
		{"filtered", []float32{1}, func(id uint64) bool { return id != 4 }, []vec.Match{{Number: 7, Score: 9}, {Number: 2, Score: 8}}},
		{"negative scores", []float32{-1}, nil, []vec.Match{{Number: 9, Score: -2}, {Number: 7, Score: -3}}},
		{"reject all", []float32{1}, func(uint64) bool { return false }, []vec.Match{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for range 20 {
				got, err := g.Search(tc.query, 2, 2, tc.accept)
				if err != nil || !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("got %v, error %v; want %v", got, err, tc.want)
				}
			}
		})
	}
}

// A tie at the beam boundary must resolve by ascending id, like exact search.
func TestGraphTieAtBeamBoundary(t *testing.T) {
	g, err := New(vec.DotProduct, Params{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint64{5, 2} {
		if err := g.Add(id, []float32{1, 0}); err != nil {
			t.Fatal(err)
		}
	}
	g.entry, g.maxLevel = 0, 0
	g.links = [][][]uint32{{{1}}, {{0}}}
	got, err := g.Search([]float32{1, 0}, 1, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []vec.Match{{Number: 2, Score: 1}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func BenchmarkGraphBuild(b *testing.B) {
	rng := rand.New(rand.NewSource(7))
	vectors := randomVectors(rng, 2000, 32)
	b.ReportAllocs()
	for b.Loop() {
		g, err := New(vec.Cosine, Params{})
		if err != nil {
			b.Fatal(err)
		}
		for i, v := range vectors {
			if err := g.Add(uint64(i), v); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkGraphSearch(b *testing.B) {
	rng := rand.New(rand.NewSource(6))
	vectors := randomVectors(rng, 5000, 32)
	g, err := New(vec.Cosine, Params{})
	if err != nil {
		b.Fatal(err)
	}
	for i, v := range vectors {
		if err := g.Add(uint64(i), v); err != nil {
			b.Fatal(err)
		}
	}
	queries := randomVectors(rng, 64, 32)
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		if _, err := g.Search(queries[i%len(queries)], 10, 0, nil); err != nil {
			b.Fatal(err)
		}
		i++
	}
}

// Keep traversal fixed to measure duplicate bookkeeping independently of recall.
func BenchmarkGraphRepeatedIDBeam(b *testing.B) {
	for _, n := range []int{100, 10000} {
		b.Run(fmt.Sprintf("%d_docs", n), func(b *testing.B) {
			g, err := New(vec.DotProduct, Params{})
			if err != nil {
				b.Fatal(err)
			}
			g.dim, g.repeated = 1, true
			g.ids = make([]uint64, 2*n)
			g.data = make([]float32, 2*n)
			g.links = make([][][]uint32, 2*n)
			for i := range g.ids {
				g.ids[i] = uint64(i / 2)
				g.data[i] = float32((i/2)*7919%n + 1)
				g.links[i] = make([][]uint32, 1)
				if i > 0 {
					g.links[i-1][0] = []uint32{uint32(i)}
				}
			}
			b.ReportAllocs()
			for b.Loop() {
				got, err := g.Search([]float32{1}, n, 2*n, nil)
				if err != nil || len(got) != n {
					b.Fatalf("got %d matches, error %v; want %d", len(got), err, n)
				}
			}
		})
	}
}

// The unrolled kernels must match vec.Score exactly, including tail dimensions.
func TestGraphScoreMatchesVecScore(t *testing.T) {
	rng := rand.New(rand.NewSource(8))
	for _, metric := range []vec.Metric{vec.Cosine, vec.DotProduct, vec.L2} {
		for dim := 1; dim <= 9; dim++ {
			vectors := randomVectors(rng, 20, dim)
			vectors = append(vectors, vectors[0]) // cosine 1 must clamp, not exceed
			g := build(t, metric, Params{}, vectors)
			q := g.newQuery(vectors[0], nil)
			for node, v := range vectors {
				want, err := vec.Score(vectors[0], v, metric)
				if err != nil {
					t.Fatal(err)
				}
				if got := g.exactScore(&q, uint32(node)); got != want {
					t.Fatalf("%s dim %d node %d: score %v, vec.Score %v", metric, dim, node, got, want)
				}
			}
		}
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

func TestParamsMOverflow(t *testing.T) {
	for _, m := range []int{math.MaxInt / 2, math.MaxInt/2 + 1, math.MaxInt} {
		p := Params{M: m}
		wantError := m > math.MaxInt/2
		if err := p.Validate(); (err != nil) != wantError {
			t.Fatalf("M=%d: Validate error %v, want error %v", m, err, wantError)
		}
		g, err := New(vec.L2, p)
		if (err != nil) != wantError {
			t.Fatalf("M=%d: New error %v, want error %v", m, err, wantError)
		}
		if !wantError && g.m0 != 2*m {
			t.Fatalf("M=%d: level-0 limit %d", m, g.m0)
		}
	}
}

func TestGraphLargeValidM(t *testing.T) {
	for _, m := range []int{DefaultM, 1 << 20, math.MaxInt / 2} {
		t.Run(fmt.Sprint(m), func(t *testing.T) {
			g, err := New(vec.L2, Params{M: m})
			if err != nil {
				t.Fatal(err)
			}
			vectors := [][]float32{{1}, {2}, {3}}
			for i, v := range vectors {
				if err := g.Add(uint64(i), v); err != nil {
					t.Fatal(err)
				}
				for _, neighbors := range g.links[i] {
					if cap(neighbors) > i+1 {
						t.Fatalf("node %d reserved %d neighbors with only %d candidates", i, cap(neighbors), i)
					}
				}
			}
			got, err := g.Search([]float32{1}, 3, 3, nil)
			if err != nil {
				t.Fatal(err)
			}
			if want := exact(t, vectors, []float32{1}, 3, vec.L2, nil); !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v, want %v", got, want)
			}
		})
	}
}

func TestGraphRejectedBridges(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values [][]float32
		links  [][][]uint32
		reject map[uint64]bool
		want   vec.Match
	}{
		{
			name:   "rejected entry below full beam",
			values: [][]float32{{-1}, {10}, {-2}, {20}},
			links:  [][][]uint32{{{1, 2}}, {{}}, {{3}}, {{}}},
			reject: map[uint64]bool{0: true, 2: true},
			want:   vec.Match{Number: 3, Score: 20},
		},
		{
			name:   "pruned accepted candidate before pending bridges",
			values: [][]float32{{5}, {6}, {10}, {-1}, {-2}, {20}},
			links:  [][][]uint32{{{1, 2, 3}}, {{}}, {{}}, {{4}}, {{5}}, {{}}},
			reject: map[uint64]bool{3: true, 4: true},
			want:   vec.Match{Number: 5, Score: 20},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := build(t, vec.DotProduct, Params{}, tc.values)
			g.entry, g.maxLevel, g.links = 0, 0, tc.links
			calls := make(map[uint64]int)
			got, err := g.Search([]float32{1}, 1, 1, func(id uint64) bool {
				calls[id]++
				return !tc.reject[id]
			})
			if err != nil || !reflect.DeepEqual(got, []vec.Match{tc.want}) {
				t.Fatalf("got %v, error %v, want %v", got, err, tc.want)
			}
			for id, count := range calls {
				if count != 1 {
					t.Errorf("accept(%d) called %d times", id, count)
				}
			}
		})
	}
}

func TestGraphFilteredSearchKeepsAcceptedBeamPruning(t *testing.T) {
	g := build(t, vec.DotProduct, Params{}, [][]float32{{5}, {0}, {20}})
	g.entry, g.maxLevel = 0, 0
	g.links = [][][]uint32{{{1}}, {{2}}, {{}}}
	calls := 0
	got, err := g.Search([]float32{1}, 1, 1, func(uint64) bool {
		calls++
		return true
	})
	want := []vec.Match{{Number: 0, Score: 5}}
	if err != nil || !reflect.DeepEqual(got, want) || calls != 2 {
		t.Fatalf("got %v, error %v, filter calls %d; want %v and 2 calls", got, err, calls, want)
	}
}

// Cancel at a specified polling point, without scheduling or timer dependencies.
// Tests use this context synchronously.
type cancelOnCheckContext struct {
	context.Context
	cancel    context.CancelFunc
	remaining int
}

func (c *cancelOnCheckContext) Err() error {
	c.remaining--
	if c.remaining == 0 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestGraphSearchContextCancellation(t *testing.T) {
	for _, empty := range []bool{false, true} {
		g := build(t, vec.DotProduct, Params{}, nil)
		if !empty {
			if err := g.Add(0, []float32{1}); err != nil {
				t.Fatal(err)
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		got, err := g.SearchContext(ctx, []float32{1}, 1, 1, nil)
		if got != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("pre-canceled (empty=%v): got %v, error %v", empty, got, err)
		}
	}

	t.Run("during filter on final neighbor", func(t *testing.T) {
		g := build(t, vec.DotProduct, Params{}, [][]float32{{1}, {2}})
		g.entry, g.maxLevel = 0, 0
		g.links = [][][]uint32{{{1}}, {{}}}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		got, err := g.SearchContext(ctx, []float32{1}, 1, 1, func(id uint64) bool {
			if id == 1 {
				cancel()
			}
			return false
		})
		if got != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, error %v", got, err)
		}
	})

	for _, descent := range []bool{false, true} {
		name := "layer repeated neighbors"
		if descent {
			name = "greedy descent neighbors"
		}
		t.Run(name, func(t *testing.T) {
			g := build(t, vec.DotProduct, Params{}, [][]float32{{1}})
			g.entry, g.maxLevel = 0, 0
			g.links = [][][]uint32{{{0, 0, 0}}}
			if descent {
				g.maxLevel = 1
				g.links[0] = append(g.links[0], []uint32{0, 0, 0})
			}
			base, cancel := context.WithCancel(context.Background())
			defer cancel()
			// Search entry, helper entry, expansion, first neighbor, second neighbor.
			ctx := &cancelOnCheckContext{Context: base, cancel: cancel, remaining: 5}
			filterCalls := 0
			got, err := g.SearchContext(ctx, []float32{1}, 1, 1, func(uint64) bool {
				filterCalls++
				return true
			})
			if got != nil || !errors.Is(err, context.Canceled) || ctx.remaining != 0 {
				t.Fatalf("got %v, error %v, remaining checks %d", got, err, ctx.remaining)
			}
			wantCalls := 1
			if descent {
				wantCalls = 0
			}
			if filterCalls != wantCalls {
				t.Fatalf("filter calls %d, want %d", filterCalls, wantCalls)
			}
		})
	}
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

func TestGraphApproxScoreCloseToExact(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	for _, metric := range []vec.Metric{vec.Cosine, vec.DotProduct, vec.L2} {
		for _, dim := range []int{1, 7, 8, 9, 64, 769} {
			vectors := randomVectors(rng, 10, dim)
			g := build(t, metric, Params{Quantization: QuantizationNone}, vectors)
			q := g.newQuery(vectors[0], nil)
			for node := range vectors {
				exact, approx := g.exactScore(&q, uint32(node)), g.score(&q, uint32(node))
				if math.Abs(exact-approx) > 1e-4*math.Max(1, math.Abs(exact)) {
					t.Fatalf("%s dim %d node %d: approx %v, exact %v", metric, dim, node, approx, exact)
				}
			}
		}
	}
}

func TestScanCheaper(t *testing.T) {
	for _, tc := range []struct {
		seen, accepted, n int
		want              bool
	}{
		{seen: scanMinVisits - 1, accepted: 0, n: 10, want: false}, // too few samples
		{seen: scanMinVisits, accepted: 0, n: 1000, want: true},    // 64 > 1000/16
		{seen: scanMinVisits, accepted: 0, n: 1100, want: false},   // 64 < 1100/16
		{seen: 1000, accepted: 10, n: 10000, want: true},           // 1% accepted: limit 725
		{seen: 1000, accepted: 500, n: 10000, want: false},         // 50% deleted: limit 5625
		{seen: 6000, accepted: 3000, n: 10000, want: true},
		{seen: 5000, accepted: 5000, n: 10000, want: false},         // unfiltered share never scans first
		{seen: 1 << 20, accepted: 1 << 19, n: 1 << 30, want: false}, // seen*seen exceeds int32
	} {
		if got := scanCheaper(tc.seen, tc.accepted, tc.n); got != tc.want {
			t.Errorf("scanCheaper(%d, %d, %d) = %v, want %v", tc.seen, tc.accepted, tc.n, got, tc.want)
		}
	}
}

// scanFixture links rejected nodes 0..rejected-1 in a chain from the entry and
// leaves the accepted nodes after them unreachable, so only a scan finds them.
func scanFixture(t *testing.T, rejected, n uint64) (g *Graph, vectors [][]float32, accept func(uint64) bool) {
	t.Helper()
	vectors = make([][]float32, n)
	for i := range vectors {
		vectors[i] = []float32{float32(i)}
	}
	g = build(t, vec.DotProduct, Params{}, vectors)
	g.entry, g.maxLevel = 0, 0
	for i := range g.links {
		g.links[i] = [][]uint32{{}}
		if uint64(i+1) < rejected {
			g.links[i][0] = []uint32{uint32(i + 1)}
		}
	}
	return g, vectors, func(id uint64) bool { return id >= rejected }
}

func TestGraphFilteredSearchFallsBackToScan(t *testing.T) {
	g, vectors, accept := scanFixture(t, 190, 200)
	calls := 0
	got, err := g.Search([]float32{1}, 3, 3, func(id uint64) bool {
		calls++
		return accept(id)
	})
	if want := exact(t, vectors, []float32{1}, 3, vec.DotProduct, accept); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, error %v, want %v", got, err, want)
	}
	if calls != scanMinVisits+len(vectors) {
		t.Fatalf("filter calls %d, want %d visits then a %d node scan", calls, scanMinVisits, len(vectors))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls = 0
	_, err = g.SearchContext(ctx, []float32{1}, 3, 3, func(id uint64) bool {
		calls++
		if calls > scanMinVisits {
			cancel()
		}
		return accept(id)
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled scan: error %v", err)
	}
}

func TestGraphFilteredSearchScanKeepsBestPerID(t *testing.T) {
	g, _, _ := scanFixture(t, 190, 200)
	g.repeated = true
	for i := uint64(190); i < 200; i++ {
		g.ids[i] = 190 + (i-190)/2 // pairs of nodes share an id
	}
	got, err := g.Search([]float32{1}, 2, 2, func(id uint64) bool { return id >= 190 })
	want := []vec.Match{{Number: 194, Score: 199}, {Number: 193, Score: 197}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, error %v, want %v", got, err, want)
	}
}

func BenchmarkGraphFilteredSearch(b *testing.B) {
	rng := rand.New(rand.NewSource(10))
	vectors := randomVectors(rng, 20000, 32)
	g, err := New(vec.Cosine, Params{})
	if err != nil {
		b.Fatal(err)
	}
	for i, v := range vectors {
		if err := g.Add(uint64(i), v); err != nil {
			b.Fatal(err)
		}
	}
	queries := randomVectors(rng, 64, 32)
	for _, every := range []uint64{100, 2} {
		b.Run(fmt.Sprintf("1_in_%d_accepted", every), func(b *testing.B) {
			accept := func(id uint64) bool { return id%every == 0 }
			b.ReportAllocs()
			i := 0
			for b.Loop() {
				if _, err := g.Search(queries[i%len(queries)], 10, 0, accept); err != nil {
					b.Fatal(err)
				}
				i++
			}
		})
	}
}
