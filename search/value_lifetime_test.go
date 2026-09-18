//  Copyright (c) 2026 The Bluge Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 		http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package search

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/vcaesar/riot/numeric"
)

func TestFieldSourceValueLifetime(t *testing.T) {
	for _, typed := range []bool{false, true} {
		name := "terms"
		if typed {
			name = "typed"
		}
		t.Run(name, func(t *testing.T) {
			dm := &DocumentMatch{}
			for hit := 0; hit < 3; hit++ {
				for field, values := range map[string][]float64{"outer": {1, 2}, "inner": {100, 200}} {
					for _, value := range values {
						n := numeric.Float64ToInt64(value + float64(hit))
						if typed {
							dm.addDocNumber(field, n)
						} else {
							dm.addDocValue(field, numeric.MustNewPrefixCodedInt64(n, 0))
						}
					}
				}
				outerTerms := Field("outer").Values(dm)
				outerNumbers := Field("outer").Numbers(dm)
				wantTerms := make([][]byte, len(outerTerms))
				for i, term := range outerTerms {
					wantTerms[i] = bytes.Clone(term)
				}
				wantNumbers := []float64{1 + float64(hit), 2 + float64(hit)}
				// Exercise both spare capacity and backing-array growth, including repeated access.
				for i := 0; i < 16; i++ {
					Field("inner").Values(dm)
					Field("inner").Numbers(dm)
					Field("outer").Values(dm)
					Field("outer").Numbers(dm)
					if !reflect.DeepEqual(outerTerms, wantTerms) {
						t.Fatalf("outer terms overwritten: %v", outerTerms)
					}
					if !reflect.DeepEqual(outerNumbers, wantNumbers) {
						t.Fatalf("outer numbers overwritten: %v", outerNumbers)
					}
				}
				if len(Field("missing").Numbers(dm)) != 0 || len(Field("missing").Values(dm)) != 0 {
					t.Fatal("missing field returned values")
				}
				dm.Reset()
			}
		})
	}
}

func BenchmarkFieldSourceNestedValues(b *testing.B) {
	dm := &DocumentMatch{}
	b.ReportAllocs()
	for b.Loop() {
		dm.addDocNumber("outer", numeric.Float64ToInt64(1))
		dm.addDocNumber("outer", numeric.Float64ToInt64(2))
		dm.addDocNumber("inner", numeric.Float64ToInt64(100))
		dm.addDocNumber("inner", numeric.Float64ToInt64(200))
		for range Field("outer").Values(dm) {
			Field("inner").Values(dm)
		}
		for range Field("outer").Numbers(dm) {
			Field("inner").Numbers(dm)
		}
		dm.Reset()
	}
}
