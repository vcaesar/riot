// Copyright (c) 2026 The Bluge Authors.
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
	"strconv"
	"testing"
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
