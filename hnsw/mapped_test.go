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
	"hash/crc32"
	"math/rand"
	"reflect"
	"testing"
	"unsafe"

	"github.com/vcaesar/ice/vec"
)

func encodeMapped(t *testing.T, g *Graph) []byte {
	t.Helper()
	var buf bytes.Buffer
	n, err := g.WriteMappedTo(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(buf.Len()) {
		t.Fatalf("WriteMappedTo reported %d bytes, wrote %d", n, buf.Len())
	}
	return buf.Bytes()
}

// aligned copies b to a buffer whose start is offset bytes past 8-byte alignment.
func aligned(b []byte, offset int) []byte {
	words := make([]uint64, (len(b)+offset)/8+1)
	buf := unsafe.Slice((*byte)(unsafe.Pointer(&words[0])), len(words)*8)
	out := buf[offset : offset+len(b)]
	copy(out, b)
	return out
}

func TestMapRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	vectors := randomVectors(rng, 300, 9)
	queries := randomVectors(rng, 5, 9)
	for _, metric := range []vec.Metric{vec.Cosine, vec.DotProduct, vec.L2} {
		for _, quant := range []Quantization{QuantizationInt8, QuantizationNone} {
			g := build(t, metric, Params{M: 6, EfConstruction: 40, Quantization: quant}, vectors)
			want := encode(t, g)
			for _, offset := range []int{0, 1, 4} {
				b := aligned(encodeMapped(t, g), offset)
				m, err := Map(b)
				if err != nil {
					t.Fatalf("%s %v offset %d: %v", metric, quant, offset, err)
				}
				if !bytes.Equal(encode(t, m), want) {
					t.Fatalf("%s %v offset %d: mapped graph differs", metric, quant, offset)
				}
				for _, q := range queries {
					got, err := m.Search(q, 5, 0, nil)
					if err != nil {
						t.Fatal(err)
					}
					exp, err := g.Search(q, 5, 0, nil)
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got, exp) {
						t.Fatalf("%s %v: mapped results %v, want %v", metric, quant, got, exp)
					}
				}
			}
		}
	}
}

// An aligned buffer is used in place; Add then copies instead of writing
// into it.
func TestMapInPlaceAndAdd(t *testing.T) {
	vectors := randomVectors(rand.New(rand.NewSource(2)), 200, 8)
	g := build(t, vec.L2, Params{}, vectors[:150])
	b := aligned(encodeMapped(t, g), 0)
	m, err := Map(b)
	if err != nil {
		t.Fatal(err)
	}
	inside := func(p unsafe.Pointer) bool {
		start := uintptr(unsafe.Pointer(&b[0]))
		return uintptr(p) >= start && uintptr(p) < start+uintptr(len(b))
	}
	if !inside(unsafe.Pointer(&m.data[0])) || !inside(unsafe.Pointer(&m.codes[0])) || !inside(unsafe.Pointer(&m.ids[0])) {
		t.Fatal("aligned arrays were copied")
	}
	before := crc32.ChecksumIEEE(b)
	for i, v := range vectors[150:] {
		if err := m.Add(uint64(150+i), v); err != nil {
			t.Fatal(err)
		}
		if err := g.Add(uint64(150+i), v); err != nil {
			t.Fatal(err)
		}
	}
	if crc32.ChecksumIEEE(b) != before {
		t.Fatal("Add wrote into the mapped buffer")
	}
	if !bytes.Equal(encode(t, m), encode(t, g)) {
		t.Fatal("graph grown after Map differs")
	}
}

func TestMapEmpty(t *testing.T) {
	g, err := New(vec.Cosine, Params{})
	if err != nil {
		t.Fatal(err)
	}
	m, err := Map(encodeMapped(t, g))
	if err != nil {
		t.Fatal(err)
	}
	if m.Len() != 0 {
		t.Fatalf("len %d", m.Len())
	}
}

func TestMapRejectsCorruption(t *testing.T) {
	g := build(t, vec.Cosine, Params{M: 4}, randomVectors(rand.New(rand.NewSource(3)), 50, 4))
	good := encodeMapped(t, g)
	for i := range len(good) {
		b := bytes.Clone(good)
		b[i] ^= 0x40
		if _, err := Map(b); err == nil {
			t.Fatalf("flipped byte %d accepted", i)
		}
	}
	for n := range len(good) {
		if _, err := Map(good[:n]); err == nil {
			t.Fatalf("%d of %d bytes accepted", n, len(good))
		}
	}
	// Other layouts are not mapped.
	if _, err := Map(encode(t, g)); err == nil {
		t.Fatal("WriteTo stream mapped")
	}
	metric, params, err := ReadParams(bytes.NewReader(good))
	if err != nil || metric != vec.Cosine || params != g.Params() {
		t.Fatalf("ReadParams %v %+v %v", metric, params, err)
	}
}
