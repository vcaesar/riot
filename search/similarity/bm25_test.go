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
