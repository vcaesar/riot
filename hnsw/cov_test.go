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
	"encoding/binary"
	"errors"
	"hash/crc32"
	"math"
	"math/rand"
	"testing"

	"github.com/vcaesar/ice/vec"
)

// headerLen is the length of a stream header up to and including the level
// generator state.
func headerLen(t *testing.T, g *Graph, version uint32) int {
	t.Helper()
	b, err := g.appendHeader(make([]byte, 0, 128), version)
	if err != nil {
		t.Fatal(err)
	}
	return len(b)
}

// withSum appends the CRC-32 of body, as Map expects.
func withSum(body []byte) []byte {
	out := bytes.Clone(body)
	return binary.LittleEndian.AppendUint32(out, crc32.ChecksumIEEE(out))
}

func smallGraph(t *testing.T, metric vec.Metric, quant Quantization) *Graph {
	t.Helper()
	return build(t, metric, Params{M: 2, EfConstruction: 8, Quantization: quant},
		randomVectors(rand.New(rand.NewSource(11)), 24, 3))
}

func TestQuantizationString(t *testing.T) {
	for _, tc := range []struct {
		q    Quantization
		want string
	}{
		{QuantizationInt8, "int8"},
		{QuantizationNone, "none"},
		{Quantization(9), "Quantization(9)"},
	} {
		if got := tc.q.String(); got != tc.want {
			t.Errorf("%d: got %q, want %q", tc.q, got, tc.want)
		}
	}
}

func TestGraphIDsAndCompareEqual(t *testing.T) {
	g := smallGraph(t, vec.L2, QuantizationNone)
	ids := g.IDs()
	if len(ids) != g.Len() || ids[0] != 0 || ids[len(ids)-1] != uint64(g.Len()-1) { // #nosec G115 -- small fixture.
		t.Fatalf("IDs %v", ids)
	}
	c := cand{node: 3, score: 1}
	if got := g.compare(c, c); got != 0 {
		t.Fatalf("compare equal: %d", got)
	}
}

func TestVisitedSetEpochWraps(t *testing.T) {
	v := &visitedSet{marks: []uint32{7, 7}, epoch: math.MaxUint32}
	v.reset(2)
	if v.epoch != 1 || v.marks[0] != 0 || v.marks[1] != 0 {
		t.Fatalf("epoch %d marks %v", v.epoch, v.marks)
	}
}

func TestGraphReadTruncated(t *testing.T) {
	for _, quant := range []Quantization{QuantizationInt8, QuantizationNone} {
		g := smallGraph(t, vec.Cosine, quant)
		full := encode(t, g)
		links := encodeLinks(t, g)
		mapped := encodeMapped(t, g)
		for n := range len(full) {
			if _, err := Read(bytes.NewReader(full[:n])); err == nil {
				t.Fatalf("%v: Read of %d/%d bytes: expected error", quant, n, len(full))
			}
		}
		for n := range len(links) {
			if _, err := ReadLinks(bytes.NewReader(links[:n]), g.ids, g.data); err == nil {
				t.Fatalf("%v: ReadLinks of %d/%d bytes: expected error", quant, n, len(links))
			}
		}
		body := mapped[:len(mapped)-4]
		for n := range len(body) {
			if _, err := Map(withSum(body[:n])); err == nil {
				t.Fatalf("%v: Map of %d/%d bytes: expected error", quant, n, len(body))
			}
		}
		if _, err := Map(withSum(append(bytes.Clone(body), 0))); err == nil {
			t.Fatalf("%v: Map with a trailing byte: expected error", quant)
		}
		// magic, version, metric, M, EfConstruction, EfSearch, quantization
		params := len(formatMagic) + 4 + 4 + len(g.metric) + 3*8 + 4
		for n := range params {
			if _, _, err := ReadParams(bytes.NewReader(full[:n])); err == nil {
				t.Fatalf("%v: ReadParams of %d bytes: expected error", quant, n)
			}
		}
	}
}

func TestGraphReadInvalidHeader(t *testing.T) {
	g := smallGraph(t, vec.L2, QuantizationNone)
	full := encode(t, g)
	le := binary.LittleEndian
	metricAt := len(formatMagic) + 4
	paramsAt := metricAt + 4 + len(g.metric)
	quantAt := paramsAt + 3*8
	dimAt := quantAt + 4
	nodesAt := dimAt + 4
	stateAt := nodesAt + 8 + 4 + 4
	empty := encode(t, build(t, vec.L2, Params{}, nil))
	tests := []struct {
		name   string
		data   []byte
		mutate func(b []byte)
	}{
		{"zero M", full, func(b []byte) { le.PutUint64(b[paramsAt:], 0) }},
		{"long metric", full, func(b []byte) { le.PutUint32(b[metricAt:], maxShortBytes+1) }},
		{"unknown metric", full, func(b []byte) { copy(b[metricAt+4:], "zz") }},
		{"unknown quantization", full, func(b []byte) { le.PutUint32(b[quantAt:], 7) }},
		{"too many nodes", full, func(b []byte) { le.PutUint64(b[nodesAt:], math.MaxUint32+1) }},
		{"bad generator state", full, func(b []byte) { b[stateAt+4] ^= 0xff }},
		{"empty graph with dimension", empty, func(b []byte) { le.PutUint32(b[dimAt:], 3) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := bytes.Clone(tt.data)
			tt.mutate(b)
			if _, err := Read(bytes.NewReader(b)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestGraphLinksMalformedBlocks(t *testing.T) {
	g := smallGraph(t, vec.L2, QuantizationNone)
	links := encodeLinks(t, g)
	start := headerLen(t, g, linksVersion) + 4 // after the content sum
	levels := byte(len(g.links[0]))            // #nosec G115 -- below maxLevels.
	tests := []struct {
		name  string
		block []byte
	}{
		{"uvarint overflow", bytes.Repeat([]byte{0xff}, 10)},
		{"malformed neighbor", []byte{3, levels, 2, 0x80}},
		{"neighbor overflow", binary.AppendUvarint([]byte{7, levels, 1}, math.MaxUint32+1)},
		{"short block", []byte{9, levels}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := append(bytes.Clone(links[:start]), tt.block...)
			if _, err := ReadLinks(bytes.NewReader(b), g.ids, g.data); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestGraphInvalidVectorsOnRead(t *testing.T) {
	g := smallGraph(t, vec.L2, QuantizationNone)
	g.data[0] = float32(math.NaN())
	links := encodeLinks(t, g)
	if _, err := ReadLinks(bytes.NewReader(links), g.ids, g.data); err == nil {
		t.Fatal("ReadLinks: expected invalid vector error")
	}
	g = smallGraph(t, vec.L2, QuantizationNone)
	g.links[1][0][0] = 1
	if _, err := Map(encodeMapped(t, g)); err == nil {
		t.Fatal("Map: expected invalid neighbor error")
	}
}

func TestMapRepeatedIDs(t *testing.T) {
	g, err := New(vec.L2, Params{M: 2})
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range []uint64{1, 2, 2, 3} {
		if err := g.Add(id, []float32{float32(i), 1}); err != nil {
			t.Fatal(err)
		}
	}
	m, err := Map(encodeMapped(t, g))
	if err != nil {
		t.Fatal(err)
	}
	if !m.repeated {
		t.Fatal("mapped graph lost repeated ids")
	}
	got, err := m.Search([]float32{1.5, 1}, 4, 0, nil)
	if err != nil || len(got) != 3 {
		t.Fatalf("got %v, err %v, want 3 unique ids", got, err)
	}
}

func TestGraphWritersPropagateErrors(t *testing.T) {
	// Links and the ids and vectors each exceed the write buffer, so
	// failures land in every loop.
	vectors := randomVectors(rand.New(rand.NewSource(12)), 3000, 8)
	g := build(t, vec.L2, Params{M: 16, EfConstruction: 16}, vectors)
	writers := map[string]func(*failWriter) (int64, error){
		"WriteTo":       func(w *failWriter) (int64, error) { return g.WriteTo(w) },
		"WriteLinksTo":  func(w *failWriter) (int64, error) { return g.WriteLinksTo(w) },
		"WriteMappedTo": func(w *failWriter) (int64, error) { return g.WriteMappedTo(w) },
	}
	for name, write := range writers {
		t.Run(name, func(t *testing.T) {
			size, err := write(&failWriter{after: math.MaxInt})
			if err != nil {
				t.Fatal(err)
			}
			for after := int64(0); after < size; after += max(size/37, 1) {
				for _, a := range []int64{after, size - 1} {
					n, err := write(&failWriter{after: int(a)})
					if err == nil || n != a {
						t.Fatalf("fail after %d of %d: n %d err %v", a, size, n, err)
					}
				}
			}
		})
	}
}

func TestBuildFromErrors(t *testing.T) {
	vectors := randomVectors(rand.New(rand.NewSource(13)), 40, 4)
	ids, data := flatten(vectors)
	base, err := Build(context.Background(), vec.L2, Params{M: 4}, ids[:20], data[:80], 1)
	if err != nil {
		t.Fatal(err)
	}
	mapping := make([]uint32, 20)
	for i := range mapping {
		mapping[i] = uint32(i) // #nosec G115 -- small fixture.
	}
	if _, err := BuildFrom(context.Background(), base, mapping, ids, data[:5], 1); err == nil {
		t.Fatal("bad data: expected error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := BuildFrom(ctx, base, mapping, ids, data, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: err %v", err)
	}
}

func TestInsertCanceledDuringDescent(t *testing.T) {
	g := build(t, vec.L2, Params{M: 2}, randomVectors(rand.New(rand.NewSource(14)), 64, 3))
	if g.maxLevel == 0 {
		t.Fatal("fixture needs an upper level")
	}
	vector := []float32{0.5, 0.5, 0.5}
	node := uint32(len(g.ids)) // #nosec G115 -- small fixture.
	g.ids = append(g.ids, uint64(node))
	g.data = append(g.data, vector...)
	g.addCodes(vector)
	g.links = append(g.links, make([][]uint32, 1))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := g.insert(ctx, node, &g.scratch); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v, want canceled", err)
	}
}

func TestSearchCanceledByFilter(t *testing.T) {
	g := build(t, vec.L2, Params{M: 4}, randomVectors(rand.New(rand.NewSource(15)), 64, 3))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	accept := func(uint64) bool {
		cancel()
		return true
	}
	if _, err := g.SearchContext(ctx, []float32{0, 0, 0}, 3, 0, accept); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v, want canceled", err)
	}
}

func TestScanCanceledByFilter(t *testing.T) {
	g, _, accept := scanFixture(t, 190, scanCheckEvery+100)
	calls := 0
	count := func(id uint64) bool {
		calls++
		return accept(id)
	}
	if _, err := g.Search([]float32{1}, 1, 1, count); err != nil {
		t.Fatal(err)
	}
	if calls <= g.Len() {
		t.Fatalf("fixture must fall back to a scan: %d accept calls for %d nodes", calls, g.Len())
	}
	searchCalls := calls - g.Len()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls = 0
	cancelInScan := func(id uint64) bool {
		calls++
		if calls == searchCalls+1 {
			cancel()
		}
		return accept(id)
	}
	if _, err := g.SearchContext(ctx, []float32{1}, 1, 1, cancelInScan); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v, want canceled", err)
	}
}

func TestContentSumSpansChunks(t *testing.T) {
	ids := make([]uint64, readChunk/8+3)
	data := make([]float32, readChunk/4+5)
	var b []byte
	for i := range ids {
		ids[i] = uint64(i) * 7
		b = binary.LittleEndian.AppendUint64(b, ids[i])
	}
	for i := range data {
		data[i] = float32(i) / 3
		b = binary.LittleEndian.AppendUint32(b, math.Float32bits(data[i]))
	}
	if got, want := contentSum(ids, data), crc32.ChecksumIEEE(b); got != want {
		t.Fatalf("contentSum %x, want %x", got, want)
	}
}
