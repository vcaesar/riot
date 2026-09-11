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

package aggregations

import (
	"testing"

	"github.com/vcaesar/riot/search"
)

func TestSingleValueMetricScoreNoAlloc(t *testing.T) {
	calc := Max(search.DocumentScore()).Calculator().(*SingleValueCalculator)
	docs := []*search.DocumentMatch{{Score: 1}, {Score: 3}, {Score: 2}}
	allocs := testing.AllocsPerRun(100, func() {
		for _, d := range docs {
			calc.Consume(d)
		}
	})
	if allocs != 0 {
		t.Fatalf("expected 0 allocs consuming scores, got %v", allocs)
	}
	if calc.Value() != 3 {
		t.Fatalf("max score = %v, want 3", calc.Value())
	}

	sum := Sum(search.DocumentScore()).Calculator().(*SingleValueCalculator)
	for _, d := range docs {
		sum.Consume(d)
	}
	if sum.Value() != 6 {
		t.Fatalf("sum score = %v, want 6", sum.Value())
	}
}
