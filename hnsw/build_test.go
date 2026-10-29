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
	"bytes"
	"context"
	"errors"
	"math"
	"math/rand"
	"testing"

	"github.com/vcaesar/ice/vec"
)

func flatten(vectors [][]float32) (ids []uint64, data []float32) {
	for i, v := range vectors {
		ids = append(ids, uint64(i))
		data = append(data, v...)
	}
	return ids, data
}

func TestBuildSerialEqualsAdd(t *testing.T) {
	vectors := randomVectors(rand.New(rand.NewSource(4)), 300, 8)
	for _, metric := range []vec.Metric{vec.Cosine, vec.DotProduct, vec.L2} {
		params := Params{M: 6, EfConstruction: 40}
		want := encode(t, build(t, metric, params, vectors))
		ids, data := flatten(vectors)
		g, err := Build(context.Background(), metric, params, ids, data, 1)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(encode(t, g), want) {
			t.Fatalf("%s: one-worker Build differs from Add", metric)
		}
	}
}

func TestBuildParallelRecall(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	vectors := randomVectors(rng, 3000, 16)
	queries := randomVectors(rng, 30, 16)
	for _, metric := range []vec.Metric{vec.Cosine, vec.DotProduct, vec.L2} {
		ids, data := flatten(vectors)
		g, err := Build(context.Background(), metric, Params{}, ids, data, 8)
		if err != nil {
			t.Fatal(err)
		}
		if g.Len() != len(vectors) || g.build != nil {
			t.Fatalf("%s: len %d, build state kept %v", metric, g.Len(), g.build != nil)
		}
		if err := g.checkLinks(); err != nil {
			t.Fatalf("%s: %v", metric, err)
		}
		var total float64
		for _, q := range queries {
			got, err := g.Search(q, 10, 0, nil)
			if err != nil {
				t.Fatal(err)
			}
			total += recall(got, exact(t, vectors, q, 10, metric, nil))
		}
		if avg := total / float64(len(queries)); avg < 0.9 {
			t.Fatalf("%s: parallel build recall@10 %.2f", metric, avg)
		}

		// The graph persists and keeps growing like any other.
		var links bytes.Buffer
		if _, err := g.WriteLinksTo(&links); err != nil {
			t.Fatal(err)
		}
		loaded, err := ReadLinks(&links, ids, data)
		if err != nil {
			t.Fatal(err)
		}
		if err := loaded.Add(uint64(len(ids)), queries[0]); err != nil {
			t.Fatal(err)
		}
	}
}

// TestBuildParallelContention builds with a small M, so upper levels fill to
// mmax and neighbors shrink while inserts publish them; under -race it
// catches unlocked reads of published links. No list may repeat a node.
func TestBuildParallelContention(t *testing.T) {
	vectors := randomVectors(rand.New(rand.NewSource(6)), 4000, 4)
	ids, data := flatten(vectors)
	for range 3 {
		g, err := Build(context.Background(), vec.L2, Params{M: 2, EfConstruction: 8}, ids, data, 16)
		if err != nil {
			t.Fatal(err)
		}
		if err := g.checkLinks(); err != nil {
			t.Fatal(err)
		}
		for node, levels := range g.links {
			for l, links := range levels {
				seen := make(map[uint32]bool, len(links))
				for _, n := range links {
					if seen[n] {
						t.Fatalf("node %d level %d: neighbor %d repeated in %v", node, l, n, links)
					}
					seen[n] = true
				}
			}
		}
	}
}

func TestBuildRepeatedIDs(t *testing.T) {
	g, err := Build(context.Background(), vec.L2, Params{}, []uint64{1, 1, 0}, []float32{1, 1, 0}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if !g.repeated {
		t.Fatal("repeated ids must be detected")
	}
	got, err := g.Search([]float32{1}, 3, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Number != 1 {
		t.Fatalf("got %v, want ids 1 then 0 once each", got)
	}
}

func TestBuildCancel(t *testing.T) {
	vectors := randomVectors(rand.New(rand.NewSource(5)), 2000, 8)
	ids, data := flatten(vectors)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, workers := range []int{1, 4} {
		if _, err := Build(ctx, vec.L2, Params{}, ids, data, workers); !errors.Is(err, context.Canceled) {
			t.Fatalf("%d workers: err %v, want canceled", workers, err)
		}
	}
}

func TestBuildValidation(t *testing.T) {
	for name, tc := range map[string]struct {
		ids  []uint64
		data []float32
	}{
		"values without ids": {nil, []float32{1}},
		"uneven split":       {[]uint64{0, 1}, []float32{1, 2, 3}},
		"no values":          {[]uint64{0}, nil},
		"invalid vector":     {[]uint64{0, 1}, []float32{1, float32(math.NaN())}},
	} {
		if _, err := Build(context.Background(), vec.L2, Params{}, tc.ids, tc.data, 2); err == nil {
			t.Fatalf("%s: want an error", name)
		}
	}
	if _, err := Build(context.Background(), "nope", Params{}, nil, nil, 2); err == nil {
		t.Fatal("unsupported metric: want an error")
	}
	for _, n := range []int{0, 1} {
		ids, data := flatten(randomVectors(rand.New(rand.NewSource(1)), n, 3))
		g, err := Build(context.Background(), vec.L2, Params{}, ids, data, 4)
		if err != nil || g.Len() != n {
			t.Fatalf("%d vectors: %v, len %d", n, err, g.Len())
		}
	}
}

func BenchmarkBuild(b *testing.B) {
	ids, data := flatten(randomVectors(rand.New(rand.NewSource(7)), 5000, 32))
	for _, workers := range []int{1, 8} {
		b.Run(map[int]string{1: "serial", 8: "8_workers"}[workers], func(b *testing.B) {
			for b.Loop() {
				if _, err := Build(context.Background(), vec.Cosine, Params{}, ids, data, workers); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestBuildFromReusesBase(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	vectors := randomVectors(rng, 3000, 16)
	queries := randomVectors(rng, 30, 16)
	ids, data := flatten(vectors)
	// The base holds every third vector, as a segment merged with others would.
	var baseVectors [][]float32
	var mapping []uint32
	for i := 0; i < len(vectors); i += 3 {
		baseVectors = append(baseVectors, vectors[i])
		mapping = append(mapping, uint32(i))
	}
	for _, metric := range []vec.Metric{vec.Cosine, vec.L2} {
		base := build(t, metric, Params{M: 8, EfConstruction: 64}, baseVectors)
		before := encode(t, base)
		for _, workers := range []int{1, 8} {
			g, err := BuildFrom(context.Background(), base, mapping, ids, data, workers)
			if err != nil {
				t.Fatal(err)
			}
			if g.Len() != len(vectors) || g.Params() != base.Params() || g.Metric() != metric {
				t.Fatalf("%s: len %d params %+v", metric, g.Len(), g.Params())
			}
			if err := g.checkLinks(); err != nil {
				t.Fatalf("%s: %v", metric, err)
			}
			var total float64
			for _, q := range queries {
				got, err := g.Search(q, 10, 0, nil)
				if err != nil {
					t.Fatal(err)
				}
				total += recall(got, exact(t, vectors, q, 10, metric, nil))
			}
			if avg := total / float64(len(queries)); avg < 0.9 {
				t.Fatalf("%s %d workers: recall@10 %.2f", metric, workers, avg)
			}
		}
		if !bytes.Equal(encode(t, base), before) {
			t.Fatalf("%s: BuildFrom modified the base graph", metric)
		}
	}
}

func TestBuildFromRejectsBadMapping(t *testing.T) {
	vectors := randomVectors(rand.New(rand.NewSource(12)), 10, 4)
	ids, data := flatten(vectors)
	base := build(t, vec.L2, Params{}, vectors[:3])
	for name, mapping := range map[string][]uint32{
		"short":     {0, 1},
		"repeated":  {0, 1, 1},
		"range":     {0, 1, 10},
		"different": {0, 2, 1},
	} {
		if _, err := BuildFrom(context.Background(), base, mapping, ids, data, 2); err == nil {
			t.Fatalf("%s mapping: want an error", name)
		}
	}
	other := build(t, vec.L2, Params{}, randomVectors(rand.New(rand.NewSource(1)), 3, 5))
	if _, err := BuildFrom(context.Background(), other, []uint32{0, 1, 2}, ids, data, 2); err == nil {
		t.Fatal("dimension mismatch: want an error")
	}
}

func BenchmarkBuildFrom(b *testing.B) {
	vectors := randomVectors(rand.New(rand.NewSource(7)), 5000, 32)
	ids, data := flatten(vectors)
	baseIDs, baseData := flatten(vectors[:4500])
	base, err := Build(context.Background(), vec.Cosine, Params{}, baseIDs, baseData, 8)
	if err != nil {
		b.Fatal(err)
	}
	mapping := make([]uint32, len(baseIDs))
	for i := range mapping {
		mapping[i] = uint32(i)
	}
	for b.Loop() {
		if _, err := BuildFrom(context.Background(), base, mapping, ids, data, 8); err != nil {
			b.Fatal(err)
		}
	}
}
