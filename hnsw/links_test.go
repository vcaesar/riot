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
	"math/rand"
	"reflect"
	"slices"
	"testing"

	"github.com/vcaesar/ice/vec"
)

func encodeLinks(t *testing.T, g *Graph) []byte {
	t.Helper()
	var buf bytes.Buffer
	n, err := g.WriteLinksTo(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(buf.Len()) {
		t.Fatalf("WriteLinksTo reported %d bytes, wrote %d", n, buf.Len())
	}
	return buf.Bytes()
}

func TestGraphLinksRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	vectors := randomVectors(rng, 300, 16)
	queries := randomVectors(rng, 10, 16)
	for _, metric := range []vec.Metric{vec.Cosine, vec.DotProduct, vec.L2} {
		t.Run(string(metric), func(t *testing.T) {
			g := build(t, metric, Params{M: 6, EfConstruction: 40}, vectors)
			links := encodeLinks(t, g)
			if full := encode(t, g); len(links) >= len(full)/2 {
				t.Fatalf("links-only %d bytes, full %d: expected under half", len(links), len(full))
			}
			r := bytes.NewReader(append(slices.Clone(links), "tail"...))
			loaded, err := ReadLinks(r, slices.Clone(g.ids), slices.Clone(g.data))
			if err != nil {
				t.Fatal(err)
			}
			if r.Len() != len("tail") {
				t.Fatalf("ReadLinks left %d bytes, want %d", r.Len(), len("tail"))
			}
			if !bytes.Equal(encode(t, loaded), encode(t, g)) {
				t.Fatal("links round trip changed the graph")
			}
			for _, q := range queries {
				want, err := g.Search(q, 10, 0, nil)
				if err != nil {
					t.Fatal(err)
				}
				got, err := loaded.Search(q, 10, 0, nil)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("loaded search %v, want %v", got, want)
				}
			}
		})
	}
}

func TestGraphLinksEmptyAndRepeated(t *testing.T) {
	empty, err := New(vec.L2, Params{})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := ReadLinks(bytes.NewReader(encodeLinks(t, empty)), nil, nil)
	if err != nil || loaded.Len() != 0 {
		t.Fatalf("empty graph: %v %v", loaded, err)
	}
	g := build(t, vec.L2, Params{}, [][]float32{{1}, {1}})
	g.ids[1] = g.ids[0] // same id twice
	loaded, err = ReadLinks(bytes.NewReader(encodeLinks(t, g)), slices.Clone(g.ids), slices.Clone(g.data))
	if err != nil || !loaded.repeated {
		t.Fatalf("repeated ids must be restored: %v", err)
	}
}

func TestGraphLinksRejectsMismatchAndCorruption(t *testing.T) {
	g := build(t, vec.Cosine, Params{M: 3}, randomVectors(rand.New(rand.NewSource(2)), 40, 4))
	data := encodeLinks(t, g)
	ids, values := g.ids, g.data
	otherIDs := slices.Clone(ids)
	otherIDs[3]++
	otherValues := slices.Clone(values)
	otherValues[5]++
	for name, tc := range map[string]struct {
		ids    []uint64
		values []float32
	}{
		"ids":         {otherIDs, values},
		"vectors":     {ids, otherValues},
		"short ids":   {ids[1:], values},
		"short data":  {ids, values[1:]},
		"no contents": {nil, nil},
	} {
		if _, err := ReadLinks(bytes.NewReader(data), tc.ids, tc.values); err == nil {
			t.Fatalf("%s mismatch: expected error", name)
		}
	}
	for _, n := range []int{0, 3, 20, len(data) / 2, len(data) - 1} {
		if _, err := ReadLinks(bytes.NewReader(data[:n]), ids, values); err == nil {
			t.Fatalf("truncated to %d bytes: expected error", n)
		}
	}
	for _, i := range []int{0, 5, 40, len(data) / 2, len(data) - 1} {
		corrupt := bytes.Clone(data)
		corrupt[i] ^= 0x40
		if _, err := ReadLinks(bytes.NewReader(corrupt), ids, values); err == nil {
			t.Fatalf("flipped byte %d: expected error", i)
		}
	}
	if _, err := Read(bytes.NewReader(data)); err == nil {
		t.Fatal("Read must reject the links-only layout")
	}
	if _, err := ReadLinks(bytes.NewReader(encode(t, g)), ids, values); err == nil {
		t.Fatal("ReadLinks must reject the full layout")
	}
}

func TestGraphLinksValidatesStructure(t *testing.T) {
	for name, mutate := range map[string]func(g *Graph){
		"neighbor out of range": func(g *Graph) { g.links[1][0][0] = uint32(len(g.ids)) }, // #nosec G115 -- small fixture.
		"self neighbor":         func(g *Graph) { g.links[1][0][0] = 1 },
		"too many neighbors": func(g *Graph) {
			g.links[0][0] = append(g.links[0][0], make([]uint32, g.m0+1-len(g.links[0][0]))...)
		},
		"entry below max level": func(g *Graph) { g.maxLevel++ },
	} {
		t.Run(name, func(t *testing.T) {
			g := build(t, vec.L2, Params{M: 2}, randomVectors(rand.New(rand.NewSource(4)), 60, 2))
			mutate(g)
			if _, err := ReadLinks(bytes.NewReader(encodeLinks(t, g)), g.ids, g.data); err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}

func TestGraphWriteLinksToPropagatesErrors(t *testing.T) {
	g := build(t, vec.L2, Params{}, randomVectors(rand.New(rand.NewSource(5)), 500, 16))
	for _, after := range []int{0, 100, 5000} {
		w := &failWriter{after: after}
		n, err := g.WriteLinksTo(w)
		if err == nil || n != int64(after) {
			t.Fatalf("after %d bytes: reported %d, %v", after, n, err)
		}
	}
}

func BenchmarkReadLinks(b *testing.B) {
	ids, data := flatten(randomVectors(rand.New(rand.NewSource(7)), 20000, 64))
	g, err := Build(context.Background(), vec.Cosine, Params{}, ids, data, 8)
	if err != nil {
		b.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := g.WriteLinksTo(&buf); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ReadLinks(bytes.NewReader(buf.Bytes()), ids, data); err != nil {
			b.Fatal(err)
		}
	}
}

// TestReadLinksArenaAppend checks that growing a loaded graph, whose links
// share arenas, builds exactly what growing the original does.
func TestReadLinksArenaAppend(t *testing.T) {
	rng := rand.New(rand.NewSource(13))
	vectors := randomVectors(rng, 400, 6)
	extra := randomVectors(rng, 200, 6)
	params := Params{M: 4, EfConstruction: 32}
	g := build(t, vec.L2, params, vectors)
	ids, data := flatten(vectors)
	loaded, err := ReadLinks(bytes.NewReader(encodeLinks(t, g)), ids, slices.Clone(data))
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range extra {
		for _, graph := range []*Graph{g, loaded} {
			if err := graph.Add(uint64(len(vectors)+i), v); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !bytes.Equal(encode(t, loaded), encode(t, g)) {
		t.Fatal("adding to a loaded graph diverged from adding to the original")
	}
	if err := loaded.checkLinks(); err != nil {
		t.Fatal(err)
	}
}
