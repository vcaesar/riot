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

import (
	"reflect"
	"testing"
)

// TestDocumentMatchResetClearsAndKeeps pins Reset's contract: every per-hit
// field is zeroed, every buffer keeps its backing array, and the bound
// visitors survive so LoadDocumentValues does not re-bind them.
func TestDocumentMatchResetClearsAndKeeps(t *testing.T) {
	dm := &DocumentMatch{
		Number:             7,
		Score:              1.5,
		Explanation:        &Explanation{},
		Locations:          FieldTermLocationMap{"body": nil},
		HitNumber:          3,
		SortValue:          [][]byte{[]byte("a")},
		FieldTermLocations: []FieldTermLocation{{Field: "body"}},
	}
	v := dm.vals()
	v.docNumbers = []docNumber{{field: "year", value: 2000}}
	v.numScratch = []float64{1}
	v.termScratch = [][]byte{[]byte("x")}
	v.termBytes = []byte("x")
	v.addDocValue("category", []byte("go"))
	sortCap, ftlCap, numCap, bytesCap := cap(dm.SortValue), cap(dm.FieldTermLocations), cap(v.docNumbers), cap(v.termBytes)

	if dm.Reset() != dm {
		t.Fatal("Reset must return the receiver")
	}

	if dm.values != v {
		t.Fatal("Reset dropped the loaded values")
	}
	if dm.reader != nil || dm.Number != 0 || dm.Score != 0 || dm.Explanation != nil ||
		dm.Locations != nil || dm.HitNumber != 0 {
		t.Fatalf("per-hit fields not cleared: %+v", dm)
	}
	for name, n := range map[string]int{
		"SortValue": len(dm.SortValue), "FieldTermLocations": len(dm.FieldTermLocations),
		"docNumbers": len(v.docNumbers), "numScratch": len(v.numScratch),
		"termScratch": len(v.termScratch), "termBytes": len(v.termBytes),
		"docValues[category]": len(v.fieldDocValues("category")),
	} {
		if n != 0 {
			t.Errorf("%s has len %d after Reset", name, n)
		}
	}
	if cap(dm.SortValue) != sortCap || cap(dm.FieldTermLocations) != ftlCap ||
		cap(v.docNumbers) != numCap || cap(v.termBytes) != bytesCap {
		t.Error("Reset dropped a buffer's backing array")
	}
	if len(v.docValues) == 0 {
		t.Error("Reset dropped the docValues entries")
	}
	for name, values := range map[string][][]byte{
		"terms": v.termScratch, "docValues": v.fieldDocValues("category"),
	} {
		for _, value := range values[:cap(values)] {
			if value != nil {
				t.Errorf("Reset retained %s bytes", name)
			}
		}
	}
	for _, value := range v.docNumbers[:cap(v.docNumbers)] {
		if value != (docNumber{}) {
			t.Error("Reset retained a numeric field reference")
		}
	}
	for _, value := range dm.FieldTermLocations[:cap(dm.FieldTermLocations)] {
		if !reflect.DeepEqual(value, FieldTermLocation{}) {
			t.Error("Reset retained a field term location")
		}
	}
	if v.termVisitor == nil || v.numVisitor == nil {
		t.Error("Reset dropped the bound visitors")
	}

	// the field list is the contract: adding a field means deciding whether
	// Reset clears or keeps it, and updating this test
	if n := reflect.TypeOf(DocumentMatch{}).NumField(); n != 9 {
		t.Fatalf("DocumentMatch has %d fields; update Reset and this test", n)
	}
	if n := reflect.TypeOf(matchValues{}).NumField(); n != 7 {
		t.Fatalf("matchValues has %d fields; update reset and this test", n)
	}
}

// TestDocumentMatchValuesLazy checks a match that never loads values does
// not allocate them, and that its accessors still read as empty.
func TestDocumentMatchValuesLazy(t *testing.T) {
	dm := &DocumentMatch{}
	if dm.DocValues("f") != nil {
		t.Fatal("DocValues of an unloaded match")
	}
	if got, ok := dm.DocNumbers("f", nil); ok || got != nil {
		t.Fatalf("DocNumbers of an unloaded match: %v %v", got, ok)
	}
	dm.Reset()
	if dm.values != nil {
		t.Fatal("reading an unloaded match allocated its values")
	}
	if n := Field("f").Numbers(dm); len(n) != 0 {
		t.Fatalf("Numbers of an unloaded match: %v", n)
	}
	if size := reflect.TypeOf(DocumentMatch{}).Size(); size > 128 {
		t.Fatalf("DocumentMatch is %d bytes; the pool allocates one per candidate", size)
	}
}

// BenchmarkDocumentMatchReset runs once per pooled candidate on the search
// hot path (DocumentMatchPool.Put); it must stay allocation-free and in the
// low single-digit ns range.
func BenchmarkDocumentMatchReset(b *testing.B) {
	dm := &DocumentMatch{}
	dm.vals()
	b.ReportAllocs()
	for b.Loop() {
		dm.Number = 1
		dm.Reset()
	}
}
