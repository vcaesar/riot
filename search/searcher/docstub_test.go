//  Copyright (c) 2020 The Bluge Authors.
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

package searcher

import (
	"strings"

	segment "github.com/vcaesar/bluge_segment_api"

	"github.com/vcaesar/riot/numeric"
	"github.com/vcaesar/riot/numeric/geo"
)

type FakeDocument []*FakeField

func (f *FakeDocument) FakeComposite(name string, exclude []string) {
	nff := &FakeField{
		N: name,
	}

	// walk all the fields and build new map of all included data
	reduced := make(map[string][]*FakeLocation)
	reducedNolocs := make(map[string]int)
	f.EachField(func(field segment.Field) {
		// if we should skip this field return
		for _, excluded := range exclude {
			if field.Name() == excluded {
				return
			}
		}

		// for each term/location record entry
		// making copy of location, because we set F
		field.EachTerm(func(term segment.FieldTerm) {
			var numLocs = 0
			term.EachLocation(func(location segment.Location) {
				locCopy := &FakeLocation{
					F: field.Name(),
					P: location.Pos(),
					S: location.Start(),
					E: location.End(),
				}
				reduced[string(term.Term())] = append(reduced[string(term.Term())], locCopy)
				numLocs++
			})
			if numLocs == 0 {
				reducedNolocs[string(term.Term())] = reducedNolocs[string(term.Term())] + term.Frequency()
			}
		})
	})

	// walk the reduced term info across all fields and build the field
	for term, locations := range reduced {
		ft := &FakeTerm{
			T: term,
			F: len(locations),
			L: locations,
		}
		nff.T = append(nff.T, ft)
	}
	for term, freq := range reducedNolocs {
		ft := &FakeTerm{
			T: term,
			F: freq,
		}
		nff.T = append(nff.T, ft)
	}

	// add it to ourself
	*f = append(*f, nff)
}

// assumes we always add the id first
func (f *FakeDocument) Identity() (field string, term []byte) {
	return (*f)[0].N, (*f)[0].V
}

func (f *FakeDocument) Analyze() {

}

func (f *FakeDocument) Timestamp() int64 {
	return 0
}

func (f *FakeDocument) Len() int {
	return len(*f)
}

func (f *FakeDocument) EachField(vf segment.VisitField) {
	for _, ff := range *f {
		vf(ff)
	}
}

type FakeField struct {
	N  string
	T  []*FakeTerm
	V  []byte
	S  bool
	DV bool
	L  int
}

func NewFakeGeoField(name string, lon, lat float64) *FakeField {
	//nolint:gosec // G115: preserve all Morton hash bits in the signed prefix-coding API.
	mHash := int64(geo.MortonHash(lon, lat))
	prefixCoded := numeric.MustNewPrefixCodedInt64(mHash, 0)

	rv := &FakeField{
		N:  name,
		V:  prefixCoded,
		S:  false,
		DV: true,
	}

	terms := addShiftTokens([]numeric.PrefixCoded{prefixCoded}, mHash, 9)
	for _, term := range terms {
		ft := &FakeTerm{
			T: string(term),
			F: 1,
		}
		rv.T = append(rv.T, ft)
	}

	return rv
}

func addShiftTokens(terms []numeric.PrefixCoded, original int64, shiftBy uint) []numeric.PrefixCoded {
	shift := shiftBy
	for shift < 64 {
		shiftEncoded, err := numeric.NewPrefixCodedInt64(original, shift)
		if err != nil {
			break
		}
		terms = append(terms, shiftEncoded)
		shift += shiftBy
	}
	return terms
}

func NewFakeField(name, data string, store, termVec, docVals bool, _ []int) *FakeField {
	rv := &FakeField{
		N:  name,
		V:  []byte(data),
		S:  store,
		DV: docVals,
	}

	// fake tokenize (split on space)
	terms := strings.Split(data, " ")
	rv.L = len(terms)

	// fake reducing repeated terms
	reduced := make(map[string][]*FakeLocation)
	var offset int
	for i, term := range terms {
		fl := &FakeLocation{
			F: "",
			P: i + 1,
			S: offset,
			E: offset + len(term),
		}
		offset += len(term) + 1
		reduced[term] = append(reduced[term], fl)
	}

	// build term list
	for term, locations := range reduced {
		ft := &FakeTerm{
			T: term,
			F: len(locations),
		}
		if termVec {
			ft.L = locations
		}
		rv.T = append(rv.T, ft)
	}

	return rv
}

func (f *FakeField) Name() string {
	return f.N
}

func (f *FakeField) Length() int {
	return f.L
}

func (f *FakeField) EachTerm(vt segment.VisitTerm) {
	for _, t := range f.T {
		vt(t)
	}
}

func (f *FakeField) Value() []byte {
	return f.V
}

func (f *FakeField) Index() bool {
	return true
}

func (f *FakeField) Store() bool {
	return f.S
}

func (f *FakeField) IndexDocValues() bool {
	return f.DV
}

type FakeTerm struct {
	T string
	F int
	L []*FakeLocation
}

func (f *FakeTerm) Term() []byte {
	return []byte(f.T)
}

func (f *FakeTerm) Frequency() int {
	return f.F
}

func (f *FakeTerm) EachLocation(vl segment.VisitLocation) {
	for _, l := range f.L {
		vl(l)
	}
}

type FakeLocation struct {
	F string
	P int
	S int
	E int
}

func (f *FakeLocation) Field() string { return f.F }
func (f *FakeLocation) Pos() int      { return f.P }
func (f *FakeLocation) Start() int    { return f.S }
func (f *FakeLocation) End() int      { return f.E }
func (f *FakeLocation) Size() int     { return 0 }
