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

package aggregations

import (
	"reflect"
	"testing"
	"time"

	"github.com/vcaesar/riot/numeric/geo"
	"github.com/vcaesar/riot/search"
)

// stubSource returns fixed values whatever the match.
type stubSource struct {
	texts  [][]byte
	nums   []float64
	dates  []time.Time
	points []*geo.Point
}

func (s *stubSource) Fields() []string                             { return []string{"f"} }
func (s *stubSource) Values(*search.DocumentMatch) [][]byte        { return s.texts }
func (s *stubSource) Numbers(*search.DocumentMatch) []float64      { return s.nums }
func (s *stubSource) Dates(*search.DocumentMatch) []time.Time      { return s.dates }
func (s *stubSource) GeoPoints(*search.DocumentMatch) []*geo.Point { return s.points }

func TestFilterSources(t *testing.T) {
	d0 := time.Unix(0, 0)
	d1 := time.Unix(100, 0)
	p0 := &geo.Point{Lon: 1, Lat: 2}
	p1 := &geo.Point{Lon: -1, Lat: -2}
	src := &stubSource{
		texts:  [][]byte{[]byte("a"), []byte("bb")},
		nums:   []float64{1, 5, 10},
		dates:  []time.Time{d0, d1},
		points: []*geo.Point{p0, p1},
	}
	match := &search.DocumentMatch{}
	for _, test := range []struct {
		name   string
		fields []string
		got    func() any
		want   any
	}{
		{
			name:   "text",
			fields: FilterText(src, nil).Fields(),
			got: func() any {
				return FilterText(src, func(b []byte) bool { return len(b) > 1 }).Values(match)
			},
			want: [][]byte{[]byte("bb")},
		},
		{
			name:   "numeric",
			fields: FilterNumeric(src, nil).Fields(),
			got: func() any {
				return FilterNumeric(src, func(f float64) bool { return f >= 5 }).Numbers(match)
			},
			want: []float64{5, 10},
		},
		{
			name:   "date",
			fields: FilterDate(src, nil).Fields(),
			got: func() any {
				return FilterDate(src, func(d time.Time) bool { return d.After(d0) }).Dates(match)
			},
			want: []time.Time{d1},
		},
		{
			name:   "geo point",
			fields: FilterGeoPoint(src, nil).Fields(),
			got: func() any {
				return FilterGeoPoint(src, func(p *geo.Point) bool { return p.Lat > 0 }).GeoPoints(match)
			},
			want: []*geo.Point{p0},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !reflect.DeepEqual(test.fields, []string{"f"}) {
				t.Errorf("Fields() = %v, want [f]", test.fields)
			}
			if got := test.got(); !reflect.DeepEqual(got, test.want) {
				t.Errorf("filtered = %v, want %v", got, test.want)
			}
		})
	}
}

func bucketCounts(buckets []*search.Bucket) map[string]uint64 {
	rv := make(map[string]uint64, len(buckets))
	for _, b := range buckets {
		rv[b.Name()] = b.Count()
	}
	return rv
}

func TestDateRanges(t *testing.T) {
	t1 := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	src := &stubSource{dates: []time.Time{t1.Add(-time.Hour), t1, t2, t2.Add(time.Hour)}}

	agg := DateRanges(src).
		AddRange(NewNamedDateRange("before", time.Time{}, t1)).
		AddRange(NewDateRange(t1, t2)).
		AddRange(NewNamedDateRange("after", t2, time.Time{})).
		AddAggregation("max_score", Max(search.DocumentScore()))
	if got := agg.Fields(); !reflect.DeepEqual(got, []string{"f"}) {
		t.Errorf("Fields() = %v", got)
	}

	calc := agg.Calculator().(*DateRangeCalculator)
	calc.Consume(&search.DocumentMatch{Score: 2})
	other := agg.Calculator().(*DateRangeCalculator)
	other.Consume(&search.DocumentMatch{Score: 3})
	calc.Merge(other)
	calc.Merge(DateRanges(src).Calculator()) // bucket count mismatch is ignored
	calc.Merge(Duration().Calculator())      // other calculator types are ignored
	calc.Finish()

	middle := "[" + t1.Format(time.RFC3339) + "," + t2.Format(time.RFC3339) + ")"
	want := map[string]uint64{"before": 2, middle: 2, "after": 4}
	if got := bucketCounts(calc.Buckets()); !reflect.DeepEqual(got, want) {
		t.Errorf("bucket counts = %v, want %v", got, want)
	}
	if got := calc.Buckets()[0].Metric("max_score"); got != 3 {
		t.Errorf("max_score = %v, want 3", got)
	}
}

func TestRangeUnnamed(t *testing.T) {
	agg := Ranges(&stubSource{nums: []float64{1, 2, 7}}).AddRange(Range(0, 5))
	calc := agg.Calculator().(*RangeCalculator)
	calc.Consume(&search.DocumentMatch{})
	calc.Finish()
	want := map[string]uint64{"[0.000000,5.000000)": 2}
	if got := bucketCounts(calc.Buckets()); !reflect.DeepEqual(got, want) {
		t.Errorf("bucket counts = %v, want %v", got, want)
	}
}

func TestDurationMetric(t *testing.T) {
	m := Duration()
	if m.Fields() != nil {
		t.Errorf("Fields() = %v, want nil", m.Fields())
	}
	calc := m.Calculator().(*DurationCalculator)
	if !calc.ScoreOnly() || !calc.BoundOnly() {
		t.Errorf("ScoreOnly() = %v, BoundOnly() = %v, want true, true", calc.ScoreOnly(), calc.BoundOnly())
	}
	calc.Consume(&search.DocumentMatch{})
	calc.ConsumeScore(1, 2)
	calc.Merge(m.Calculator())
	calc.Finish()
	if calc.Duration() < 0 {
		t.Errorf("Duration() = %v, want >= 0", calc.Duration())
	}

	bucket := search.NewBucket("", search.Aggregations{"duration": m})
	bucket.Finish()
	if bucket.Duration() < 0 {
		t.Errorf("bucket Duration() = %v, want >= 0", bucket.Duration())
	}
}

func TestCardinality(t *testing.T) {
	src := &stubSource{texts: [][]byte{[]byte("a"), []byte("b"), []byte("a")}}
	m := Cardinality(src)
	if got := m.Fields(); !reflect.DeepEqual(got, []string{"f"}) {
		t.Errorf("Fields() = %v", got)
	}
	calc := m.Calculator().(*CardinalityCalculator)
	calc.Consume(&search.DocumentMatch{})
	other := Cardinality(&stubSource{texts: [][]byte{[]byte("c")}}).Calculator().(*CardinalityCalculator)
	other.Consume(&search.DocumentMatch{})
	calc.Merge(other)
	calc.Merge(Duration().Calculator())
	calc.Finish()
	if got := calc.Value(); got != 3 {
		t.Errorf("Value() = %v, want 3", got)
	}
}

func TestQuantiles(t *testing.T) {
	m := Quantiles(&stubSource{nums: []float64{1, 2, 3}})
	if err := m.SetCompression(0.5); err == nil {
		t.Errorf("SetCompression(0.5) error = nil, want error")
	}
	if err := m.SetCompression(50); err != nil {
		t.Fatalf("SetCompression(50) error = %v", err)
	}
	calc := m.Calculator().(*QuantilesCalculator)
	calc.Consume(&search.DocumentMatch{})
	other := Quantiles(&stubSource{nums: []float64{4, 5}}).Calculator().(*QuantilesCalculator)
	other.Consume(&search.DocumentMatch{})
	calc.Merge(other)
	calc.Merge(Duration().Calculator())
	calc.Finish()
	for _, test := range []struct {
		percent float64
		want    float64
		wantErr bool
	}{
		{percent: -0.1, wantErr: true},
		{percent: 1.1, wantErr: true},
		{percent: 0, want: 1},
		{percent: 1, want: 5},
	} {
		got, err := calc.Quantile(test.percent)
		if (err != nil) != test.wantErr || got != test.want {
			t.Errorf("Quantile(%v) = %v, %v; want %v, error %v", test.percent, got, err, test.want, test.wantErr)
		}
	}
}

func TestSingleValueCountMax(t *testing.T) {
	calc := MaxStartingAt(countSource, 0).Calculator().(*SingleValueCalculator)
	if !calc.ScoreOnly() || !calc.BoundOnly() {
		t.Errorf("ScoreOnly() = %v, BoundOnly() = %v, want true, true", calc.ScoreOnly(), calc.BoundOnly())
	}
	calc.ConsumeScore(9, 4)
	if got := calc.Value(); got != 1 {
		t.Errorf("Value() = %v, want 1", got)
	}
	if got := countSource.Numbers(nil); !reflect.DeepEqual(got, []float64{1}) {
		t.Errorf("countingSource.Numbers() = %v, want [1]", got)
	}
	calc.Merge(Duration().Calculator())
	if got := calc.Value(); got != 1 {
		t.Errorf("Value() after foreign merge = %v, want 1", got)
	}
}

func TestWeightedAvg(t *testing.T) {
	for _, test := range []struct {
		name   string
		weight *stubSource
		want   float64
	}{
		{name: "weighted", weight: &stubSource{nums: []float64{3}}, want: 3},
		{name: "empty weight", weight: &stubSource{}, want: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := WeightedAvg(&stubSource{nums: []float64{2, 4}}, test.weight)
			if got := m.Fields(); !reflect.DeepEqual(got, []string{"f", "f"}) {
				t.Errorf("Fields() = %v, want [f f]", got)
			}
			calc := m.Calculator().(*WeightedAvgCalculator)
			calc.Consume(&search.DocumentMatch{})
			calc.Finish()
			if got := calc.Value(); got != test.want {
				t.Errorf("Value() = %v, want %v", got, test.want)
			}
		})
	}
}

// docSource returns the term at the match number.
type docSource []string

func (s docSource) Fields() []string { return nil }
func (s docSource) Values(d *search.DocumentMatch) [][]byte {
	return [][]byte{[]byte(s[d.Number])}
}

func TestTermsAscendingAndOther(t *testing.T) {
	agg := NewTermsAggregation(docSource{"b", "a", "b"}, 5)
	agg.AddAggregation("max_score", Max(search.DocumentScore()))
	agg.desc = false
	calc := agg.Calculator().(*TermsCalculator)
	for i := range 3 {
		calc.Consume(&search.DocumentMatch{Number: uint64(i), Score: float64(i + 2)})
	}
	calc.Finish()
	var names []string
	for _, b := range calc.Buckets() {
		names = append(names, b.Name())
	}
	if !reflect.DeepEqual(names, []string{"a", "b"}) {
		t.Errorf("buckets = %v, want [a b]", names)
	}
	if calc.Other() != 0 {
		t.Errorf("Other() = %d, want 0", calc.Other())
	}
	if got := calc.Buckets()[1].Metric("max_score"); got != 4 {
		t.Errorf("max_score = %v, want 4", got)
	}
}
