//  Copyright (c) 2026 The AtomAI.
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

// A bucket keeps more aggregations than its inline storage holds, finds
// them by name, and keeps a map built by Aggregations in step with Merge.
func TestBucketBeyondInline(t *testing.T) {
	aggs := Aggregations{"a": countAgg{}, "b": countAgg{}, "c": countAgg{}}
	b := NewBucket("", aggs)
	b.Consume(&DocumentMatch{})
	for name := range aggs {
		if got := b.Aggregation(name).(*countCalc).n; got != 1 {
			t.Fatalf("%s consumed %d, want 1", name, got)
		}
	}
	if b.Aggregation("missing") != nil || b.Metric("missing") != 0 || b.Buckets("missing") != nil {
		t.Fatal("missing aggregation found")
	}
	m := b.Aggregations()
	other := NewBucket("", Aggregations{"a": countAgg{}, "d": countAgg{}})
	other.Consume(&DocumentMatch{})
	b.Merge(other)
	if len(m) != 4 || m["d"] != b.Aggregation("d") {
		t.Fatalf("map not updated by Merge: %v", m)
	}
	if got := b.Aggregation("a").(*countCalc).n; got != 2 {
		t.Fatalf("merged a = %d, want 2", got)
	}
}

type scoreCalc struct{ countCalc }

func (*scoreCalc) ScoreOnly() bool                 { return true }
func (c *scoreCalc) ConsumeScore(_ float64, n int) { c.n += n }
func (*scoreCalc) BoundOnly() bool                 { return true }

type scoreAgg struct{}

func (scoreAgg) Fields() []string       { return nil }
func (scoreAgg) Calculator() Calculator { return &scoreCalc{} }

// ScoreOnly holds only while every aggregation, merged ones included,
// reads just the score.
func TestBucketScoreOnly(t *testing.T) {
	if !NewBucket("", nil).ScoreOnly() {
		t.Fatal("empty bucket is not score only")
	}
	b := NewBucket("", Aggregations{"s": scoreAgg{}, "t": scoreAgg{}, "u": scoreAgg{}})
	if !b.ScoreOnly() || !b.BoundOnly() {
		t.Fatal("score aggregations not score only")
	}
	b.ConsumeScore(1, 3)
	if got := b.Aggregation("u").(*scoreCalc).n; got != 3 {
		t.Fatalf("ConsumeScore counted %d, want 3", got)
	}
	b.Merge(NewBucket("", Aggregations{"c": countAgg{}}))
	if b.ScoreOnly() {
		t.Fatal("bucket with a document aggregation is score only")
	}
}

func TestNewBucketAllocations(t *testing.T) {
	aggs := Aggregations{"count": scoreAgg{}}
	// the bucket and the one calculator
	if n := testing.AllocsPerRun(100, func() { NewBucket("", aggs) }); n > 2 {
		t.Fatalf("NewBucket allocates %v, want <= 2", n)
	}
}
