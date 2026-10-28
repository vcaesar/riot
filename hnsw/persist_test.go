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
	"errors"
	"math"
	"math/rand"
	"reflect"
	"testing"

	"github.com/vcaesar/ice/vec"
)

func encode(t *testing.T, g *Graph) []byte {
	t.Helper()
	var buf bytes.Buffer
	n, err := g.WriteTo(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(buf.Len()) {
		t.Fatalf("WriteTo reported %d bytes, wrote %d", n, buf.Len())
	}
	return buf.Bytes()
}

func decode(t *testing.T, data []byte) *Graph {
	t.Helper()
	g, err := Read(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestGraphPersistRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	vectors := randomVectors(rng, 300, 12)
	queries := randomVectors(rng, 10, 12)
	for _, metric := range []vec.Metric{vec.Cosine, vec.DotProduct, vec.L2} {
		t.Run(string(metric), func(t *testing.T) {
			g := build(t, metric, Params{M: 6, EfConstruction: 40, EfSearch: 30}, vectors)
			data := encode(t, g)
			loaded := decode(t, data)
			if loaded.Metric() != metric || loaded.Params() != g.Params() || loaded.Len() != g.Len() {
				t.Fatalf("loaded %s %+v %d, want %s %+v %d",
					loaded.Metric(), loaded.Params(), loaded.Len(), metric, g.Params(), g.Len())
			}
			if again := encode(t, loaded); !bytes.Equal(again, data) {
				t.Fatal("re-encoding a loaded graph changed its bytes")
			}
			accept := func(id uint64) bool { return id%3 != 0 }
			for _, q := range queries {
				for _, filter := range []func(uint64) bool{nil, accept} {
					want, err := g.Search(q, 10, 0, filter)
					if err != nil {
						t.Fatal(err)
					}
					got, err := loaded.Search(q, 10, 0, filter)
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("loaded search %v, want %v", got, want)
					}
				}
			}
		})
	}
}

func TestGraphPersistContinuesBuild(t *testing.T) {
	vectors := randomVectors(rand.New(rand.NewSource(3)), 200, 8)
	params := Params{M: 4, EfConstruction: 32}
	whole := build(t, vec.L2, params, vectors)

	half := build(t, vec.L2, params, vectors[:100])
	resumed := decode(t, encode(t, half))
	for i, v := range vectors[100:] {
		if err := resumed.Add(uint64(100+i), v); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(encode(t, resumed), encode(t, whole)) {
		t.Fatal("adding after a round trip must build the same graph")
	}
}

func TestGraphPersistEmptyAndRepeated(t *testing.T) {
	empty, err := New(vec.DotProduct, Params{})
	if err != nil {
		t.Fatal(err)
	}
	loaded := decode(t, encode(t, empty))
	if loaded.Len() != 0 || loaded.Params() != (Params{}).WithDefaults() {
		t.Fatalf("empty graph: len %d params %+v", loaded.Len(), loaded.Params())
	}
	if got, err := loaded.Search([]float32{1}, 1, 0, nil); err != nil || got != nil {
		t.Fatalf("empty search: %v %v", got, err)
	}
	if err := loaded.Add(5, []float32{1, 2}); err != nil {
		t.Fatal(err)
	}

	g, err := New(vec.L2, Params{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint64{1, 1, 0} {
		if err := g.Add(id, []float32{float32(id)}); err != nil {
			t.Fatal(err)
		}
	}
	loaded = decode(t, encode(t, g))
	if !loaded.repeated {
		t.Fatal("repeated ids must be restored")
	}
	got, err := loaded.Search([]float32{1}, 3, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []vec.Match{{Number: 1, Score: 0}, {Number: 0, Score: -1}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestGraphReadConsumesExactly(t *testing.T) {
	g := build(t, vec.L2, Params{}, randomVectors(rand.New(rand.NewSource(1)), 20, 3))
	r := bytes.NewReader(append(encode(t, g), "tail"...))
	if _, err := Read(r); err != nil {
		t.Fatal(err)
	}
	if rest := r.Len(); rest != len("tail") {
		t.Fatalf("Read left %d bytes, want %d", rest, len("tail"))
	}
}

func TestGraphReadRejectsCorruption(t *testing.T) {
	g := build(t, vec.Cosine, Params{M: 3}, randomVectors(rand.New(rand.NewSource(2)), 40, 4))
	data := encode(t, g)
	for _, n := range []int{0, 3, 20, len(data) / 2, len(data) - 1} {
		if _, err := Read(bytes.NewReader(data[:n])); err == nil {
			t.Fatalf("truncated to %d bytes: expected error", n)
		}
	}
	for _, i := range []int{0, 5, 40, len(data) / 2, len(data) - 1} {
		corrupt := bytes.Clone(data)
		corrupt[i] ^= 0x40
		if _, err := Read(bytes.NewReader(corrupt)); err == nil {
			t.Fatalf("flipped byte %d: expected error", i)
		}
	}
}

// TestGraphReadValidatesStructure writes structurally invalid graphs with
// valid checksums, as a buggy or foreign writer could.
func TestGraphReadValidatesStructure(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(g *Graph)
	}{
		{"neighbor out of range", func(g *Graph) { g.links[1][0][0] = uint32(len(g.ids)) }}, // #nosec G115 -- small fixture.
		{"self neighbor", func(g *Graph) { g.links[1][0][0] = 1 }},
		{"neighbor missing level", func(g *Graph) {
			for node := range g.links {
				if len(g.links[node]) > 1 && len(g.links[node][1]) > 0 {
					for n := range g.links {
						if len(g.links[n]) == 1 && n != node {
							g.links[node][1][0] = uint32(n)
							return
						}
					}
				}
			}
			t.Fatal("fixture needs an upper level")
		}},
		{"too many neighbors", func(g *Graph) {
			g.links[0][0] = append(g.links[0][0], make([]uint32, g.m0+1-len(g.links[0][0]))...)
		}},
		{"invalid vector", func(g *Graph) { g.data[0] = float32(math.NaN()) }},
		{"entry below max level", func(g *Graph) { g.maxLevel++ }},
		{"entry out of range", func(g *Graph) { g.entry = uint32(len(g.ids)) }}, // #nosec G115 -- small fixture.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := build(t, vec.L2, Params{M: 2}, randomVectors(rand.New(rand.NewSource(4)), 60, 2))
			tt.mutate(g)
			var buf bytes.Buffer
			if _, err := g.WriteTo(&buf); err != nil {
				t.Fatal(err)
			}
			if _, err := Read(&buf); err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}

type failWriter struct{ after int }

func (w *failWriter) Write(p []byte) (int, error) {
	if len(p) > w.after {
		n := w.after
		w.after = 0
		return n, errors.New("disk full")
	}
	w.after -= len(p)
	return len(p), nil
}

func TestGraphWriteToPropagatesErrors(t *testing.T) {
	g := build(t, vec.L2, Params{}, randomVectors(rand.New(rand.NewSource(5)), 500, 16))
	for _, after := range []int{0, 100, 5000} {
		w := &failWriter{after: after}
		n, err := g.WriteTo(w)
		if err == nil {
			t.Fatalf("after %d bytes: expected error", after)
		}
		if n != int64(after) {
			t.Fatalf("after %d bytes: reported %d", after, n)
		}
	}
}

func BenchmarkGraphRead(b *testing.B) {
	vectors := randomVectors(rand.New(rand.NewSource(1)), 5000, 64)
	g, err := New(vec.Cosine, Params{})
	if err != nil {
		b.Fatal(err)
	}
	for i, v := range vectors {
		if err := g.Add(uint64(i), v); err != nil {
			b.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if _, err := g.WriteTo(&buf); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(buf.Len()))
	for b.Loop() {
		if _, err := Read(bytes.NewReader(buf.Bytes())); err != nil {
			b.Fatal(err)
		}
	}
}
