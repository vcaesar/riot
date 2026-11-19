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

package search

import (
	"bytes"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/vcaesar/riot/numeric"
	"github.com/vcaesar/riot/numeric/geo"
)

func prefixCoded(v int64, shift uint) []byte {
	return numeric.MustNewPrefixCodedInt64(v, shift)
}

func termMatch(field string, terms ...[]byte) *DocumentMatch {
	dm := &DocumentMatch{}
	for _, term := range terms {
		dm.vals().addDocValue(field, term)
	}
	return dm
}

func numberMatch(field string, nums ...int64) *DocumentMatch {
	dm := &DocumentMatch{}
	for _, n := range nums {
		dm.vals().addDocNumber(field, n)
	}
	return dm
}

func TestFieldSourceTermValues(t *testing.T) {
	f := Field("f")
	if got := f.NumericField(); got != "f" {
		t.Errorf("NumericField() = %q", got)
	}
	five := prefixCoded(numeric.Float64ToInt64(5.5), 0)
	dm := termMatch("f", five, prefixCoded(9, 4), []byte("x"))
	if got := f.Numbers(dm); !reflect.DeepEqual(got, []float64{5.5}) {
		t.Errorf("Numbers() = %v, want [5.5]", got)
	}
	if got := f.Value(dm); !bytes.Equal(got, five) {
		t.Errorf("Value() = %v, want %v", got, five)
	}
	if got := f.Number(dm); got != 5.5 {
		t.Errorf("Number() = %v, want 5.5", got)
	}
	if got := f.Number(&DocumentMatch{}); !math.IsNaN(got) {
		t.Errorf("Number() of missing = %v, want NaN", got)
	}
	if got := f.Value(&DocumentMatch{}); got != nil {
		t.Errorf("Value() of missing = %v, want nil", got)
	}
}

func TestFieldSourceDates(t *testing.T) {
	d1 := time.Unix(0, 1_600_000_000_000_000_000)
	d2 := time.Unix(0, 1_700_000_000_000_000_000)
	for _, test := range []struct {
		name string
		dm   *DocumentMatch
		want []time.Time
	}{
		{name: "missing", dm: &DocumentMatch{}},
		{name: "typed", dm: numberMatch("d", d1.UnixNano(), d2.UnixNano()), want: []time.Time{d1, d2}},
		{name: "terms", dm: termMatch("d", prefixCoded(d1.UnixNano(), 0), prefixCoded(1, 8), []byte("x")), want: []time.Time{d1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := Field("d").Dates(test.dm); !reflect.DeepEqual(got, test.want) {
				t.Errorf("Dates() = %v, want %v", got, test.want)
			}
			var want time.Time
			if len(test.want) > 0 {
				want = test.want[0]
			}
			if got := Field("d").Date(test.dm); !got.Equal(want) {
				t.Errorf("Date() = %v, want %v", got, want)
			}
		})
	}
}

func TestFieldSourceGeoPoints(t *testing.T) {
	hash := int64(geo.MortonHash(10, 20)) //nolint:gosec // G115: test round trip of the Morton bits
	for _, test := range []struct {
		name string
		dm   *DocumentMatch
		want int
	}{
		{name: "missing", dm: &DocumentMatch{}},
		{name: "typed", dm: numberMatch("p", hash, hash), want: 2},
		{name: "terms", dm: termMatch("p", prefixCoded(hash, 0), prefixCoded(1, 8), []byte("x")), want: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			points := Field("p").GeoPoints(test.dm)
			if len(points) != test.want {
				t.Fatalf("GeoPoints() = %v, want %d points", points, test.want)
			}
			for _, p := range points {
				if math.Abs(p.Lon-10) > 1e-6 || math.Abs(p.Lat-20) > 1e-6 {
					t.Errorf("point = %+v, want lon 10 lat 20", p)
				}
			}
			first := Field("p").GeoPoint(test.dm)
			if (first == nil) != (test.want == 0) {
				t.Errorf("GeoPoint() = %v", first)
			}
		})
	}
}

func TestScoreSourceAccessors(t *testing.T) {
	s := DocumentScore()
	dm := &DocumentMatch{Score: 2.5}
	want := prefixCoded(numeric.Float64ToInt64(2.5), 0)
	if len(s.Fields()) != 0 {
		t.Errorf("Fields() = %v", s.Fields())
	}
	if !bytes.Equal(s.Value(dm), want) || !reflect.DeepEqual(s.Values(dm), [][]byte{want}) {
		t.Errorf("Value() = %v, Values() = %v, want %v", s.Value(dm), s.Values(dm), want)
	}
	if s.Number(dm) != 2.5 || !reflect.DeepEqual(s.Numbers(dm), []float64{2.5}) {
		t.Errorf("Number() = %v, Numbers() = %v", s.Number(dm), s.Numbers(dm))
	}
}

// nilAppender is a TextValueAppender that never has a value.
type nilAppender struct{}

func (nilAppender) Fields() []string                          { return []string{"n"} }
func (nilAppender) Value(*DocumentMatch) []byte               { return nil }
func (nilAppender) AppendValue(*DocumentMatch, []byte) []byte { return nil }

func TestMissingTextValue(t *testing.T) {
	dm := &DocumentMatch{}
	for _, test := range []struct {
		name    string
		primary TextValueSource
		want    string
	}{
		{name: "primary", primary: ConstantTextValueSource("p"), want: "p"},
		{name: "missing", primary: Field("f"), want: "r"},
		{name: "appender missing", primary: nilAppender{}, want: "r"},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := MissingTextValue(test.primary, ConstantTextValueSource("r"))
			if got := m.Value(dm); string(got) != test.want {
				t.Errorf("Value() = %q, want %q", got, test.want)
			}
			if got := m.AppendValue(dm, []byte("old")); string(got) != test.want {
				t.Errorf("AppendValue() = %q, want %q", got, test.want)
			}
		})
	}
	if got := MissingTextValue(Field("a"), Field("b")).Fields(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("Fields() = %v, want [a b]", got)
	}
}

func TestMissingSources(t *testing.T) {
	present := numberMatch("a", 5)
	absent := numberMatch("b", 7)
	hash := int64(geo.MortonHash(1, 2)) //nolint:gosec // G115: test round trip of the Morton bits
	geoPresent := numberMatch("a", hash)

	num := MissingNumeric(Field("a"), Field("b"))
	date := MissingDate(Field("a"), Field("b"))
	points := MissingGeoPoints(Field("a"), Field("b"))
	for name, fields := range map[string][]string{
		"numeric": num.Fields(), "date": date.Fields(), "geo": points.Fields(),
	} {
		if !reflect.DeepEqual(fields, []string{"a", "b"}) {
			t.Errorf("%s Fields() = %v, want [a b]", name, fields)
		}
	}
	if got := num.Numbers(present); !reflect.DeepEqual(got, []float64{numeric.Int64ToFloat64(5)}) {
		t.Errorf("numeric primary = %v", got)
	}
	if got := num.Numbers(absent); !reflect.DeepEqual(got, []float64{numeric.Int64ToFloat64(7)}) {
		t.Errorf("numeric replacement = %v", got)
	}
	if got := date.Numbers(present); !reflect.DeepEqual(got, []time.Time{time.Unix(0, 5)}) {
		t.Errorf("date primary = %v", got)
	}
	if got := date.Numbers(absent); !reflect.DeepEqual(got, []time.Time{time.Unix(0, 7)}) {
		t.Errorf("date replacement = %v", got)
	}
	if got := points.GeoPoints(geoPresent); len(got) != 1 {
		t.Errorf("geo primary = %v", got)
	}
	if got := points.GeoPoints(&DocumentMatch{}); len(got) != 0 {
		t.Errorf("geo replacement = %v", got)
	}
}

func TestFilterTextSource(t *testing.T) {
	f := FilterText(Field("f"), func(b []byte) bool { return len(b) > 1 })
	if got := f.Fields(); !reflect.DeepEqual(got, []string{"f"}) {
		t.Errorf("Fields() = %v", got)
	}
	dm := termMatch("f", []byte("a"), []byte("bb"))
	if got := f.Values(dm); !reflect.DeepEqual(got, [][]byte{[]byte("bb")}) {
		t.Errorf("Values() = %q", got)
	}
}

func TestRemoveNumericPaddedTerms(t *testing.T) {
	n0 := prefixCoded(1, 0)
	n8 := prefixCoded(1, 8)
	for _, test := range []struct {
		name string
		in   [][]byte
		want [][]byte
	}{
		{name: "empty"},
		{name: "numeric", in: [][]byte{n0}, want: [][]byte{n0}},
		{name: "mixed", in: [][]byte{n0, []byte("x")}, want: [][]byte{n0, []byte("x")}},
		{name: "shifted", in: [][]byte{n8}, want: [][]byte{n8}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := RemoveNumericPaddedTerms(test.in); !reflect.DeepEqual(got, test.want) {
				t.Errorf("RemoveNumericPaddedTerms() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestPointDistanceSource(t *testing.T) {
	hash := int64(geo.MortonHash(0, 1)) //nolint:gosec // G115: test round trip of the Morton bits
	dm := numberMatch("p", hash)
	origin := NewConstantGeoPointSource(geo.Point{})
	if origin.Fields() != nil {
		t.Errorf("ConstantGeoPointSource.Fields() = %v", origin.Fields())
	}
	if p := origin.GeoPoint(nil); p.Lon != 0 || p.Lat != 0 {
		t.Errorf("GeoPoint() = %+v", p)
	}
	d := NewGeoPointDistanceSource(Field("p"), origin, geo.Meter)
	if got := d.Fields(); !reflect.DeepEqual(got, []string{"p"}) {
		t.Errorf("Fields() = %v, want [p]", got)
	}
	dist := d.Number(dm)
	if dist < 110_000 || dist > 112_000 {
		t.Errorf("Number() = %v, want about one degree in meters", dist)
	}
	if got := d.Numbers(dm); !reflect.DeepEqual(got, []float64{dist}) {
		t.Errorf("Numbers() = %v", got)
	}
	want := prefixCoded(numeric.Float64ToInt64(dist), 0)
	if !bytes.Equal(d.Value(dm), want) || !reflect.DeepEqual(d.Values(dm), [][]byte{want}) {
		t.Errorf("Value() = %v, Values() = %v, want %v", d.Value(dm), d.Values(dm), want)
	}
}

func TestConstantTextValueSource(t *testing.T) {
	c := ConstantTextValueSource("k")
	if c.Fields() != nil || string(c.Value(nil)) != "k" {
		t.Errorf("Fields() = %v, Value() = %q", c.Fields(), c.Value(nil))
	}
}
