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
	"errors"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/riot/numeric"
)

var errCov = errors.New("cov error")

// covValues are the doc values a covReader serves for every document.
type covValues struct {
	terms map[string][][]byte
	nums  map[string][]int64
	err   error
}

func (v *covValues) visitTerms(visitor segment.DocumentValueVisitor) {
	for field, terms := range v.terms {
		for _, term := range terms {
			visitor(field, term)
		}
	}
}

// covDVR is a plain DocumentValueReader.
type covDVR struct{ *covValues }

func (r covDVR) VisitDocumentValues(_ uint64, visitor segment.DocumentValueVisitor) error {
	if r.err != nil {
		return r.err
	}
	r.visitTerms(visitor)
	return nil
}

// covNumDVR is a NumericDocumentValueReader.
type covNumDVR struct{ covDVR }

func (r covNumDVR) VisitDocumentNumbers(_ uint64, visitor segment.DocumentValueVisitor,
	numVisitor segment.NumericValueVisitor) error {
	if r.err != nil {
		return r.err
	}
	r.visitTerms(visitor)
	for field, nums := range r.nums {
		for _, n := range nums {
			numVisitor(field, n)
		}
	}
	return nil
}

type covReader struct {
	vals      covValues
	numeric   bool
	readerErr error
	stored    map[string][]byte
}

func (r *covReader) DocumentValueReader([]string) (segment.DocumentValueReader, error) {
	if r.readerErr != nil {
		return nil, r.readerErr
	}
	if r.numeric {
		return covNumDVR{covDVR{&r.vals}}, nil
	}
	return covDVR{&r.vals}, nil
}

func (r *covReader) VisitStoredFields(_ uint64, visitor segment.StoredFieldVisitor) error {
	for field, value := range r.stored {
		if !visitor(field, value) {
			break
		}
	}
	return nil
}

func TestLocationSizeAndAdd(t *testing.T) {
	if got := (&Location{}).Size(); got != reflectStaticSizeLocation {
		t.Errorf("Location.Size() = %d, want %d", got, reflectStaticSizeLocation)
	}
	tlm := TermLocationMap{}
	tlm.AddLocation("a", &Location{Pos: 1})
	tlm.AddLocation("a", &Location{Pos: 2})
	if len(tlm["a"]) != 2 || tlm["a"][1].Pos != 2 {
		t.Errorf("AddLocation = %v", tlm["a"])
	}
}

func TestLoadDocumentValues(t *testing.T) {
	five := numeric.MustNewPrefixCodedInt64(5, 0)
	for _, test := range []struct {
		name      string
		reader    *covReader
		wantErr   error
		wantTerms [][]byte
		wantNums  []int64
	}{
		{name: "reader error", reader: &covReader{readerErr: errCov}, wantErr: errCov},
		{name: "visit error", reader: &covReader{vals: covValues{err: errCov}}, wantErr: errCov},
		{
			name:      "terms",
			reader:    &covReader{vals: covValues{terms: map[string][][]byte{"f": {five}}}},
			wantTerms: [][]byte{five},
		},
		{
			name:      "numeric column",
			reader:    &covReader{numeric: true, vals: covValues{nums: map[string][]int64{"f": {5}}}},
			wantTerms: [][]byte{five},
			wantNums:  []int64{5},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			dm := &DocumentMatch{}
			dm.SetReader(test.reader)
			err := dm.LoadDocumentValues(NewSearchContext(0, 0), []string{"f"})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("LoadDocumentValues() error = %v, want %v", err, test.wantErr)
			}
			if err != nil {
				return
			}
			if got := dm.DocValues("f"); !reflect.DeepEqual(got, test.wantTerms) {
				t.Errorf("DocValues() = %v, want %v", got, test.wantTerms)
			}
			got, ok := dm.DocNumbers("f", nil)
			if !reflect.DeepEqual(got, test.wantNums) || ok != (test.wantNums != nil) {
				t.Errorf("DocNumbers() = %v, %v, want %v", got, ok, test.wantNums)
			}
		})
	}
}

func TestDocNumbersWithoutValues(t *testing.T) {
	dm := &DocumentMatch{}
	if dm.hasDocNumbers("f") {
		t.Errorf("hasDocNumbers() = true on a match without values")
	}
	buf := []int64{7}
	if got, ok := dm.DocNumbers("f", buf); ok || !reflect.DeepEqual(got, buf) {
		t.Errorf("DocNumbers() = %v, %v, want %v, false", got, ok, buf)
	}
}

func TestVisitStoredFields(t *testing.T) {
	dm := &DocumentMatch{}
	dm.SetReader(&covReader{stored: map[string][]byte{"title": []byte("x")}})
	var got []string
	err := dm.VisitStoredFields(func(field string, value []byte) bool {
		got = append(got, field+"="+string(value))
		return true
	})
	if err != nil || !reflect.DeepEqual(got, []string{"title=x"}) {
		t.Errorf("VisitStoredFields() = %v, %v", got, err)
	}
}

func TestDocumentMatchSizeAndString(t *testing.T) {
	base := (&DocumentMatch{}).Size()
	dm := &DocumentMatch{
		Number:      3,
		Score:       1.5,
		Explanation: NewExplanation(1, "x"),
		Locations:   FieldTermLocationMap{"f": TermLocationMap{"t": {{Pos: 1}}}},
		SortValue:   [][]byte{[]byte("abc")},
	}
	if got := dm.Size(); got <= base {
		t.Errorf("Size() = %d, want > %d", got, base)
	}
	if got := dm.String(); got != "[3-1.500000]" {
		t.Errorf("String() = %q", got)
	}
}

func TestDocumentMatchComplete(t *testing.T) {
	ftl := func(field, term string, pos int) FieldTermLocation {
		return FieldTermLocation{Field: field, Term: term, Location: Location{Pos: pos, Start: pos, End: pos + 1}}
	}
	for _, test := range []struct {
		name     string
		prealloc []Location
		existing FieldTermLocationMap
		ftls     []FieldTermLocation
		want     map[string]map[string][]int
	}{
		{name: "none", want: map[string]map[string][]int{}},
		{
			name: "ordered",
			ftls: []FieldTermLocation{ftl("a", "x", 1), ftl("a", "x", 2), ftl("b", "y", 1)},
			want: map[string]map[string][]int{"a": {"x": {1, 2}}, "b": {"y": {1}}},
		},
		{
			name:     "dedupe into existing with prealloc",
			prealloc: make([]Location, 0, 8),
			existing: FieldTermLocationMap{"a": TermLocationMap{}},
			ftls:     []FieldTermLocation{ftl("a", "x", 2), ftl("a", "x", 1), ftl("a", "x", 2)},
			want:     map[string]map[string][]int{"a": {"x": {1, 2}}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			dm := &DocumentMatch{Locations: test.existing, FieldTermLocations: test.ftls}
			prealloc := dm.Complete(test.prealloc)
			if len(prealloc) != len(test.ftls) {
				t.Errorf("prealloc len = %d, want %d", len(prealloc), len(test.ftls))
			}
			got := map[string]map[string][]int{}
			for field, tlm := range dm.Locations {
				got[field] = map[string][]int{}
				for term, locs := range tlm {
					for _, loc := range locs {
						got[field][term] = append(got[field][term], loc.Pos)
					}
				}
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("Locations = %v, want %v", got, test.want)
			}
			if len(dm.FieldTermLocations) != 0 {
				t.Errorf("FieldTermLocations not recycled: %v", dm.FieldTermLocations)
			}
		})
	}
}

func TestDocumentMatchCollectionSort(t *testing.T) {
	c := DocumentMatchCollection{{Number: 1, Score: 1}, {Number: 2, Score: 3}, {Number: 3, Score: 2}}
	sort.Sort(c)
	var got []uint64
	for _, dm := range c {
		got = append(got, dm.Number)
	}
	if !reflect.DeepEqual(got, []uint64{2, 3, 1}) {
		t.Errorf("sorted = %v, want [2 3 1]", got)
	}
}

func TestDocValueReaderForReaderError(t *testing.T) {
	ctx := NewSearchContext(0, 0)
	if _, err := ctx.DocValueReaderForReader(&covReader{readerErr: errCov}, nil); !errors.Is(err, errCov) {
		t.Fatalf("error = %v, want %v", err, errCov)
	}
	if ctx.dvReaders != nil {
		t.Errorf("failed reader cached")
	}
}

func TestDocNumber(t *testing.T) {
	for _, test := range []struct {
		name    string
		reader  *covReader
		wantVal int64
		wantOK  bool
		wantErr error
	}{
		{name: "reader error", reader: &covReader{readerErr: errCov}, wantErr: errCov},
		{name: "not numeric", reader: &covReader{}},
		{name: "visit error", reader: &covReader{numeric: true, vals: covValues{err: errCov}}, wantErr: errCov},
		{
			name:    "single value",
			reader:  &covReader{numeric: true, vals: covValues{nums: map[string][]int64{"n": {7}, "other": {1}}}},
			wantVal: 7,
			wantOK:  true,
		},
		{
			name:    "two values",
			reader:  &covReader{numeric: true, vals: covValues{nums: map[string][]int64{"n": {7, 7}}}},
			wantVal: 7,
		},
		{
			name: "term",
			reader: &covReader{numeric: true, vals: covValues{
				nums:  map[string][]int64{"n": {7}},
				terms: map[string][][]byte{"n": {[]byte("x")}, "other": {[]byte("y")}},
			}},
			wantVal: 7,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := NewSearchContext(0, 0)
			dm := &DocumentMatch{}
			dm.SetReader(test.reader)
			for range 2 { // the second call reuses the bound visitors
				v, ok, err := dm.DocNumber(ctx, []string{"n"}, "n")
				if !errors.Is(err, test.wantErr) || ok != test.wantOK || (err == nil && v != test.wantVal) {
					t.Fatalf("DocNumber() = %d, %v, %v; want %d, %v, %v", v, ok, err, test.wantVal, test.wantOK, test.wantErr)
				}
			}
		})
	}
}

func TestContextSize(t *testing.T) {
	empty := (&Context{}).Size()
	ctx := NewSearchContext(2, 0)
	ctx.DocumentMatchPool.avail = append(ctx.DocumentMatchPool.avail, nil)
	if got := ctx.Size(); got <= empty {
		t.Errorf("Size() = %d, want > %d", got, empty)
	}
}

func TestExplanation(t *testing.T) {
	child := NewExplanation(1, "child")
	e := NewExplanation(2, "parent", child)
	if e.Value != 2 || e.Message != "parent" || len(e.Children) != 1 {
		t.Errorf("NewExplanation() = %+v", e)
	}
	if got := e.Size(); got <= child.Size() {
		t.Errorf("Size() = %d, want > %d", got, child.Size())
	}
	if got := e.String(); !strings.Contains(got, `"message": "child"`) {
		t.Errorf("String() = %s", got)
	}
	if got := NewExplanation(math.NaN(), "nan").String(); !strings.HasPrefix(got, "error serializing explanation to json") {
		t.Errorf("String() of NaN = %s", got)
	}
}

func TestDocumentMatchPoolEdges(t *testing.T) {
	p := NewDocumentMatchPool(0, 0)
	if dm := p.Get(); dm == nil {
		t.Fatal("Get() on empty pool = nil")
	}
	p.Put(nil)
	if len(p.avail) != 0 {
		t.Errorf("Put(nil) added to pool: %d", len(p.avail))
	}
}

func TestMergeFieldTermLocations(t *testing.T) {
	loc := func(term string) FieldTermLocation {
		return FieldTermLocation{Field: "f", Term: term, Location: Location{Pos: 1, Start: 2, End: 3}}
	}
	matches := []*DocumentMatch{
		{FieldTermLocations: []FieldTermLocation{loc("a")}},
		{FieldTermLocations: []FieldTermLocation{loc("b"), loc("c")}},
	}
	for _, test := range []struct {
		name string
		dest []FieldTermLocation
	}{
		{name: "grow", dest: []FieldTermLocation{loc("z")}},
		{name: "fits", dest: append(make([]FieldTermLocation, 0, 8), loc("z"))},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := MergeFieldTermLocations(test.dest, matches)
			want := []FieldTermLocation{loc("z"), loc("a"), loc("b"), loc("c")}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("MergeFieldTermLocations() = %v, want %v", got, want)
			}
		})
	}
}

type metricCalc struct {
	countCalc
	v float64
}

func (c *metricCalc) Value() float64 { return c.v }

type metricAgg float64

func (metricAgg) Fields() []string         { return []string{"m"} }
func (a metricAgg) Calculator() Calculator { return &metricCalc{v: float64(a)} }

type durationCalc struct{ countCalc }

func (*durationCalc) Duration() time.Duration { return time.Second }

type durationAgg struct{}

func (durationAgg) Fields() []string       { return nil }
func (durationAgg) Calculator() Calculator { return &durationCalc{} }

type bucketsCalc struct{ countCalc }

func (*bucketsCalc) Buckets() []*Bucket { return []*Bucket{NewBucket("child", nil)} }

type bucketsAgg struct{}

func (bucketsAgg) Fields() []string       { return []string{"b"} }
func (bucketsAgg) Calculator() Calculator { return &bucketsCalc{} }

type unboundCalc struct{ scoreCalc }

func (*unboundCalc) BoundOnly() bool { return false }

type unboundAgg struct{}

func (unboundAgg) Fields() []string       { return nil }
func (unboundAgg) Calculator() Calculator { return &unboundCalc{} }

func TestBucketAccessors(t *testing.T) {
	aggs := Aggregations{}
	aggs.Add("count", metricAgg(4))
	aggs.Add("duration", durationAgg{})
	aggs.Add("tree", bucketsAgg{})
	fields := aggs.Fields()
	sort.Strings(fields)
	if !reflect.DeepEqual(fields, []string{"b", "m"}) {
		t.Errorf("Fields() = %v, want [b m]", fields)
	}
	b := NewBucket("root", aggs)
	if b.Name() != "root" {
		t.Errorf("Name() = %q", b.Name())
	}
	if b.Count() != 4 || b.Duration() != time.Second || b.Metric("count") != 4 {
		t.Errorf("Count() = %d, Duration() = %v, Metric() = %v", b.Count(), b.Duration(), b.Metric("count"))
	}
	if got := b.Buckets("tree"); len(got) != 1 || got[0].Name() != "child" {
		t.Errorf("Buckets() = %v", got)
	}
	for name, got := range map[string]bool{
		"metric of non metric":  b.Metric("tree") != 0,
		"metric missing":        b.Metric("missing") != 0,
		"buckets of non bucket": b.Buckets("count") != nil,
		"buckets missing":       b.Buckets("missing") != nil,
	} {
		if got {
			t.Errorf("%s: want zero value", name)
		}
	}

	wrong := NewBucket("", Aggregations{"count": durationAgg{}, "duration": metricAgg(1)})
	if wrong.Count() != 0 || wrong.Duration() != 0 {
		t.Errorf("mistyped Count() = %d, Duration() = %v, want 0, 0", wrong.Count(), wrong.Duration())
	}
	none := NewBucket("", nil)
	if none.Count() != 0 || none.Duration() != 0 {
		t.Errorf("empty Count() = %d, Duration() = %v, want 0, 0", none.Count(), none.Duration())
	}

	unbound := NewBucket("", Aggregations{"a": scoreAgg{}, "b": unboundAgg{}})
	if !unbound.ScoreOnly() || unbound.BoundOnly() {
		t.Errorf("ScoreOnly() = %v, BoundOnly() = %v, want true, false", unbound.ScoreOnly(), unbound.BoundOnly())
	}
}
