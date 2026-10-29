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
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"math"
	"math/rand"
	"slices"
	"testing"

	"github.com/vcaesar/ice/vec"
)

func TestQuantize(t *testing.T) {
	for name, tc := range map[string]struct {
		v         []float32
		codes     []int8
		wantScale float32
	}{
		"zero":     {[]float32{0, 0}, []int8{0, 0}, 0},
		"extremes": {[]float32{-2, 2, 1}, []int8{-127, 127, 64}, 2.0 / 127},
		"tiny":     {[]float32{1e-30, -1e-30}, []int8{127, -127}, 1e-30 / 127},
	} {
		codes, scale := quantize(nil, tc.v)
		if !slices.Equal(codes, tc.codes) || math.Abs(float64(scale-tc.wantScale)) > 1e-6*float64(tc.wantScale) {
			t.Errorf("%s: codes %v scale %v, want %v %v", name, codes, scale, tc.codes, tc.wantScale)
		}
	}
	// Each value is recovered within half a step.
	v := randomVectors(rand.New(rand.NewSource(1)), 1, 300)[0]
	codes, scale := quantize(nil, v)
	for i, x := range v {
		if d := math.Abs(float64(x) - float64(codes[i])*float64(scale)); d > float64(scale)/2*1.0001 {
			t.Fatalf("value %d: %v decodes %v off", i, x, d)
		}
	}
}

func TestDotInt8(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for dim := 0; dim <= 17; dim++ {
		q, v := make([]int8, dim), make([]int8, dim)
		var want int64
		for i := range q {
			q[i], v[i] = int8(rng.Intn(255)-127), int8(rng.Intn(255)-127) // #nosec G115 -- in [-127, 127].
			want += int64(q[i]) * int64(v[i])
		}
		if got := dotInt8(q, v); got != want {
			t.Fatalf("dim %d: %d, want %d", dim, got, want)
		}
	}
}

func TestInt8ScoreCloseToExact(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for _, metric := range []vec.Metric{vec.Cosine, vec.DotProduct, vec.L2} {
		vectors := randomVectors(rng, 50, 64)
		g := build(t, metric, Params{}, vectors)
		q := g.newQuery(vectors[0], nil)
		for node, v := range vectors {
			exact, approx := g.exactScore(&q, uint32(node)), g.score(&q, uint32(node))
			// Errors scale with the vectors' magnitudes, not the score.
			scale := norm(vectors[0]) * norm(v)
			switch metric {
			case vec.Cosine:
				scale = 1
			case vec.L2:
				scale = squaredNorm(vectors[0]) + squaredNorm(v)
			}
			if math.Abs(exact-approx) > 0.01*scale {
				t.Fatalf("%s node %d: int8 %v, exact %v", metric, node, approx, exact)
			}
		}
	}
}

// Int8 traversal keeps recall close to float32 traversal, and search
// returns exact scores either way.
func TestInt8RecallAndExactScores(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	vectors := randomVectors(rng, 2000, 32)
	queries := randomVectors(rng, 30, 32)
	for _, metric := range []vec.Metric{vec.Cosine, vec.DotProduct, vec.L2} {
		total := map[Quantization]float64{}
		for _, quant := range []Quantization{QuantizationInt8, QuantizationNone} {
			g := build(t, metric, Params{Quantization: quant}, vectors)
			for _, q := range queries {
				got, err := g.Search(q, 10, 0, nil)
				if err != nil {
					t.Fatal(err)
				}
				want := exact(t, vectors, q, 10, metric, nil)
				total[quant] += recall(got, want)
				for _, m := range got {
					score, err := vec.Score(q, vectors[m.Number], metric)
					if err != nil || score != m.Score {
						t.Fatalf("%s %v: doc %d score %v, exact %v (%v)", metric, quant, m.Number, m.Score, score, err)
					}
				}
			}
		}
		i8, f32 := total[QuantizationInt8]/30, total[QuantizationNone]/30
		if i8 < 0.9 || i8 < f32-0.05 {
			t.Fatalf("%s: int8 recall@10 %.3f, float32 %.3f", metric, i8, f32)
		}
	}
}

func TestQuantizationValidation(t *testing.T) {
	if _, err := New(vec.L2, Params{Quantization: QuantizationNone + 1}); err == nil {
		t.Fatal("unknown quantization accepted")
	}
	if got := (Params{}).WithDefaults().Quantization; got != QuantizationInt8 {
		t.Fatalf("default quantization %v", got)
	}
}

// Graphs persisted before quantization, versions 1 and 2, still read, as
// float32 graphs.
func TestReadLegacyVersions(t *testing.T) {
	vectors := randomVectors(rand.New(rand.NewSource(5)), 100, 4)
	g := build(t, vec.L2, Params{M: 4, EfConstruction: 16, Quantization: QuantizationNone}, vectors)
	full, err := Read(bytes.NewReader(legacy(t, encode(t, g), legacyFormatVersion)))
	if err != nil {
		t.Fatal(err)
	}
	if full.Params().Quantization != QuantizationNone || !bytes.Equal(encode(t, full), encode(t, g)) {
		t.Fatal("legacy full graph differs")
	}
	links, err := ReadLinks(bytes.NewReader(legacy(t, encodeLinks(t, g), legacyLinksVersion)), g.ids, g.data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encode(t, links), encode(t, g)) {
		t.Fatal("legacy links graph differs")
	}
}

// legacy rewrites a stream into the given pre-quantization version by
// dropping the quantization field and recomputing the checksum.
func legacy(t *testing.T, b []byte, version uint32) []byte {
	t.Helper()
	le := binary.LittleEndian
	at := len(formatMagic) + 4 + 4 + int(le.Uint32(b[len(formatMagic)+4:])) + 3*8
	if le.Uint32(b[at:]) != uint32(QuantizationNone) {
		t.Fatal("legacy streams hold float32 graphs")
	}
	out := append(append([]byte(nil), b[:at]...), b[at+4:len(b)-4]...)
	le.PutUint32(out[len(formatMagic):], version)
	return le.AppendUint32(out, crc32.ChecksumIEEE(out))
}

// BenchmarkScoreKernels compares the traversal kernels per vector pair.
func BenchmarkScoreKernels(b *testing.B) {
	for _, dim := range []int{64, 128, 768} {
		vs := randomVectors(rand.New(rand.NewSource(6)), 2, dim)
		cq, _ := quantize(nil, vs[0])
		cv, _ := quantize(nil, vs[1])
		b.Run(fmt.Sprintf("float32/%d", dim), func(b *testing.B) {
			for b.Loop() {
				sinkF = dot32(vs[0], vs[1])
			}
		})
		b.Run(fmt.Sprintf("int8/%d", dim), func(b *testing.B) {
			for b.Loop() {
				sinkI = dotInt8(cq, cv)
			}
		})
	}
}

var (
	sinkF float32
	sinkI int64
)
