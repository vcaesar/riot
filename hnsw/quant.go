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
	"fmt"
	"math"
	"slices"

	"github.com/vcaesar/ice/vec"
)

// Quantization selects the vectors a graph compares while it is built and
// traversed. Search always rescores its candidates with the float32 vectors,
// so returned scores stay exact either way.
type Quantization uint8

const (
	// QuantizationInt8, the default, compares one signed byte per value,
	// scaled per vector by its largest magnitude, as Lucene's int8 HNSW does.
	// Codes take a quarter of the float32 size and need no training, so
	// graphs keep growing with Add.
	QuantizationInt8 Quantization = iota
	// QuantizationNone compares the float32 vectors.
	QuantizationNone
)

func (q Quantization) String() string {
	switch q {
	case QuantizationInt8:
		return "int8"
	case QuantizationNone:
		return "none"
	default:
		return fmt.Sprintf("Quantization(%d)", uint8(q))
	}
}

// query is a vector compared with nodes, with what score needs precomputed.
type query struct {
	values []float32
	norm   float64 // cosine: L2 norm
	codes  []int8  // int8: codes, and the value of code 1
	scale  float64
	sqNorm float64 // int8 L2: squared L2 norm
}

// nodeQuery compares node's vector with other nodes, without allocating.
func (g *Graph) nodeQuery(node uint32) query {
	q := query{values: g.vector(node), norm: g.norm(node)}
	if g.quant == QuantizationInt8 {
		q.codes, q.scale = g.code(node), float64(g.scales[node])
		if g.metric == vec.L2 {
			q.sqNorm = g.sqNorms[node]
		}
	}
	return q
}

// newQuery prepares an external vector, quantizing it into buf.
func (g *Graph) newQuery(values []float32, buf []int8) query {
	q := query{values: values}
	if g.metric == vec.Cosine {
		q.norm = norm(values)
	}
	if g.quant == QuantizationInt8 {
		var scale float32
		q.codes, scale = quantize(buf[:0], values)
		q.scale = float64(scale)
		if g.metric == vec.L2 {
			q.sqNorm = squaredNorm(values)
		}
	}
	return q
}

func (g *Graph) code(node uint32) []int8 {
	return g.codes[int(node)*g.dim : int(node+1)*g.dim]
}

// addCodes appends the codes, scale and, for L2, squared norm of vector.
func (g *Graph) addCodes(vector []float32) {
	if g.quant != QuantizationInt8 {
		return
	}
	var scale float32
	g.codes, scale = quantize(g.codes, vector)
	g.scales = append(g.scales, scale)
	if g.metric == vec.L2 {
		g.sqNorms = append(g.sqNorms, squaredNorm(vector))
	}
}

// reserveCodes sizes the per-node arrays for nodes vectors of g.dim.
func (g *Graph) reserveCodes(nodes int) {
	if g.metric == vec.Cosine {
		g.norms = slices.Grow(g.norms, nodes)
	}
	if g.quant != QuantizationInt8 {
		return
	}
	g.codes = slices.Grow(g.codes, nodes*g.dim)
	g.scales = slices.Grow(g.scales, nodes)
	if g.metric == vec.L2 {
		g.sqNorms = slices.Grow(g.sqNorms, nodes)
	}
}

// quantize appends v's codes to dst: v[i] ≈ code[i] * scale, codes in
// [-127, 127]. An all-zero vector has scale 0.
func quantize(dst []int8, v []float32) (codes []int8, scale float32) {
	var absMax float64
	for _, x := range v {
		absMax = max(absMax, math.Abs(float64(x)))
	}
	dst = slices.Grow(dst, len(v))
	if absMax == 0 {
		for range v {
			dst = append(dst, 0)
		}
		return dst, 0
	}
	inv := 127 / absMax
	for _, x := range v {
		dst = append(dst, int8(max(-127, min(127, math.Round(float64(x)*inv)))))
	}
	return dst, float32(absMax / 127)
}

func squaredNorm(v []float32) float64 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	return sum
}

// scoreInt8 approximates exactScore from codes: dot ≈ scales × code dot,
// and L2 expands to the exact squared norms minus twice that dot.
func (g *Graph) scoreInt8(q *query, node uint32) float64 {
	d := float64(dotInt8(q.codes, g.code(node))) * q.scale * float64(g.scales[node])
	switch g.metric {
	case vec.L2:
		return 2*d - q.sqNorm - g.sqNorms[node]
	case vec.Cosine:
		return clamp(d / (q.norm * g.norms[node]))
	default:
		return d
	}
}

// dotInt8 keeps eight int32 sums so the additions pipeline; each holds
// below 2^31 for vectors of up to a million values. len(v) must be at least len(q).
func dotInt8(q, v []int8) int64 {
	v = v[:len(q)]
	var a0, a1, a2, a3, a4, a5, a6, a7 int32
	i := 0
	for ; i+8 <= len(q); i += 8 {
		q8, v8 := q[i:i+8:i+8], v[i:i+8:i+8]
		a0 += int32(q8[0]) * int32(v8[0])
		a1 += int32(q8[1]) * int32(v8[1])
		a2 += int32(q8[2]) * int32(v8[2])
		a3 += int32(q8[3]) * int32(v8[3])
		a4 += int32(q8[4]) * int32(v8[4])
		a5 += int32(q8[5]) * int32(v8[5])
		a6 += int32(q8[6]) * int32(v8[6])
		a7 += int32(q8[7]) * int32(v8[7])
	}
	for ; i < len(q); i++ {
		a0 += int32(q[i]) * int32(v[i])
	}
	return int64(a0) + int64(a1) + int64(a2) + int64(a3) + int64(a4) + int64(a5) + int64(a6) + int64(a7)
}
