// Copyright (c) 2026 The AtomAI.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package similarity

import (
	"math"
	"reflect"
	"strconv"
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"

	"github.com/vcaesar/riot/search"
)

func TestBM25ComputeNormBounds(t *testing.T) {
	for _, test := range []struct {
		count     string
		bits      uint32
		wantPanic bool
	}{
		{count: "0", bits: 0},
		{count: "1", bits: 1},
		{count: "2147483647", bits: math.MaxInt32},
		{count: "4294967295", bits: math.MaxUint32},
		{count: "-1", wantPanic: true},
		{count: "4294967296", wantPanic: true},
	} {
		t.Run(test.count, func(t *testing.T) {
			count, err := strconv.Atoi(test.count)
			if err != nil {
				if strconv.IntSize == 32 {
					t.Skip("count does not fit in a 32-bit int")
				}
				t.Fatal(err)
			}
			defer func() {
				if got := recover(); (got != nil) != test.wantPanic {
					t.Errorf("panic = %v, want panic = %v", got, test.wantPanic)
				}
			}()
			got := math.Float32bits(NewBM25Similarity().ComputeNorm(count))
			if !test.wantPanic && got != test.bits {
				t.Errorf("norm bits = %d, want %d", got, test.bits)
			}
		})
	}
}

func TestBM25Idf(t *testing.T) {
	sim := NewBM25Similarity()
	tests := []struct {
		docFreq, docCount uint64
		want              float64
	}{
		{docFreq: 1, docCount: 5, want: math.Log(1 + 4.5/1.5)},
		{docFreq: 5000, docCount: 10000, want: math.Ln2},
		{docFreq: 0, docCount: 10, want: math.Log(1 + 10.5/0.5)},
		// docFreq > docCount must not wrap the unsigned subtraction
		{docFreq: 6, docCount: 5, want: math.Log(1 + (-0.5)/6.5)},
	}
	for _, test := range tests {
		got := sim.Idf(test.docFreq, test.docCount)
		if math.Abs(got-test.want) > 1e-12 {
			t.Errorf("Idf(%d, %d) = %v, want %v", test.docFreq, test.docCount, got, test.want)
		}
	}
	// rarer terms must score higher, and common terms must not dominate
	if sim.Idf(1, 10000) <= sim.Idf(5000, 10000) || sim.Idf(5000, 10000) > 1 {
		t.Fatalf("idf ordering broken: rare %v common %v", sim.Idf(1, 10000), sim.Idf(5000, 10000))
	}
}

func TestBM25ScorerMonotone(t *testing.T) {
	idf := NewBM25Similarity().Idf(10, 1000)
	for _, test := range []struct {
		name         string
		boost, k1, b float64
		want         bool
	}{
		{name: "default", boost: 1, k1: 1.2, b: 0.75, want: true},
		{name: "zero boost", boost: 0, k1: 1.2, b: 0.75, want: true},
		{name: "negative boost", boost: -1, k1: 1.2, b: 0.75},
		{name: "negative k1", boost: 1, k1: -1, b: 0.75},
		{name: "b above 1", boost: 1, k1: 1.2, b: 1.5},
		{name: "negative b", boost: 1, k1: 1.2, b: -0.1},
	} {
		s := NewBM25Scorer(test.boost, test.k1, test.b, 10, &search.Explanation{Value: idf})
		if got := s.Monotone(); got != test.want {
			t.Errorf("%s: Monotone() = %v, want %v", test.name, got, test.want)
		}
		if !test.want || test.boost == 0 {
			continue
		}
		// higher freq and shorter docs must never score lower
		long, short := float64(math.Float32frombits(50)), float64(math.Float32frombits(5))
		if s.Score(1, long) > s.Score(5, long) || s.Score(5, long) > s.Score(5, short) {
			t.Errorf("%s: scores not monotone", test.name)
		}
	}
}

type bm25Stats struct{ docFreq, docCount, sumTF uint64 }

func (s bm25Stats) DocumentFrequency() uint64     { return s.docFreq }
func (s bm25Stats) DocumentCount() uint64         { return s.docCount }
func (s bm25Stats) TotalDocumentCount() uint64    { return s.docCount }
func (s bm25Stats) SumTotalTermFrequency() uint64 { return s.sumTF }
func (s bm25Stats) Merge(segment.CollectionStats) {}

// TestBM25ScorerLazyIdf checks Scorer, which defers the idf explanation,
// scores and explains bit-identically to a scorer built from the explanation.
func TestBM25ScorerLazyIdf(t *testing.T) {
	sim := NewBM25Similarity()
	stats := bm25Stats{docFreq: 146, docCount: 100000, sumTF: 1000000}
	lazy := sim.Scorer(1.5, stats, stats).(*BM25Scorer)
	eager := NewBM25Scorer(1.5, sim.k1, sim.b, sim.AverageFieldLength(stats), sim.IdfExplainTerm(stats, stats))
	if lazy.idf != nil {
		t.Fatal("Scorer built the idf explanation up front")
	}
	norm := float64(math.Float32frombits(10))
	for freq := 1; freq <= 4; freq++ {
		if l, e := lazy.Score(freq, norm), eager.Score(freq, norm); l != e {
			t.Fatalf("Score(%d) = %v, want %v", freq, l, e)
		}
		l, e := lazy.Explain(freq, norm), eager.Explain(freq, norm)
		if !reflect.DeepEqual(l, e) {
			t.Fatalf("Explain(%d) = %v, want %v", freq, l, e)
		}
	}
}

// TestBM25ScoreNotFused guards against arm64 fusing freq*normInverse+1 into
// an FMA, which would make scores differ from amd64 in the last bit.
func TestBM25ScoreNotFused(t *testing.T) {
	s := NewBM25Scorer(1, 1.2, 0.75, 7.3, &search.Explanation{Value: 2.1})
	checked := 0
	for docLen := uint32(1); docLen <= 64; docLen++ {
		norm := float64(math.Float32frombits(docLen))
		inv := 1 / (s.k1 * ((1 - s.b) + s.b*float64(docLen)/s.avgDocLen))
		for freq := 1; freq <= 64; freq++ {
			product := float64(float64(freq) * inv)
			if math.FMA(float64(freq), inv, 1) == 1+product {
				continue // fused and unfused agree here
			}
			checked++
			want := s.weight - s.weight/(1+product)
			if got := s.Score(freq, norm); got != want {
				t.Fatalf("Score(%d, %d) = %v, want unfused %v", freq, docLen, got, want)
			}
			if got := s.Explain(freq, norm).Value; got != want {
				t.Fatalf("Explain(%d, %d) = %v, want unfused %v", freq, docLen, got, want)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no input where FMA and unfused rounding differ")
	}
}
