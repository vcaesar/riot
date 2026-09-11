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

import "testing"

type countCalc struct{ n, finished int }

func (c *countCalc) Consume(*DocumentMatch) { c.n++ }
func (c *countCalc) Finish()                { c.finished++ }
func (c *countCalc) Merge(o Calculator)     { c.n += o.(*countCalc).n }

type countAgg struct{}

func (countAgg) Fields() []string       { return nil }
func (countAgg) Calculator() Calculator { return &countCalc{} }

func TestBucketConsumeAndMerge(t *testing.T) {
	a := NewBucket("a", Aggregations{"x": countAgg{}})
	b := NewBucket("b", Aggregations{"x": countAgg{}, "y": countAgg{}})
	for i := 0; i < 3; i++ {
		a.Consume(&DocumentMatch{})
		b.Consume(&DocumentMatch{})
	}
	a.Merge(b)
	if got := a.Aggregation("x").(*countCalc).n; got != 6 {
		t.Fatalf("merged x = %d, want 6", got)
	}
	// y only existed in b; after the merge a must keep consuming into it
	a.Consume(&DocumentMatch{})
	a.Finish()
	y := a.Aggregation("y").(*countCalc)
	if y.n != 4 || y.finished != 1 {
		t.Fatalf("merged-in y: consumed %d finished %d, want 4 and 1", y.n, y.finished)
	}
	if len(a.Aggregations()) != 2 || len(a.calculators) != 2 {
		t.Fatalf("map/slice out of sync: %d vs %d", len(a.Aggregations()), len(a.calculators))
	}
}
