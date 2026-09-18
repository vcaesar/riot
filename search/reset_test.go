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
		docNumbers:         []docNumber{{field: "year", value: 2000}},
		numScratch:         []float64{1},
		termScratch:        [][]byte{[]byte("x")},
		termBytes:          []byte("x"),
	}
	dm.addDocValue("category", []byte("go"))
	dm.termVisitor, dm.numVisitor = dm.addDocValue, dm.addDocNumber
	sortCap, ftlCap, numCap, bytesCap := cap(dm.SortValue), cap(dm.FieldTermLocations), cap(dm.docNumbers), cap(dm.termBytes)

	if dm.Reset() != dm {
		t.Fatal("Reset must return the receiver")
	}

	if dm.reader != nil || dm.Number != 0 || dm.Score != 0 || dm.Explanation != nil ||
		dm.Locations != nil || dm.HitNumber != 0 {
		t.Fatalf("per-hit fields not cleared: %+v", dm)
	}
	for name, n := range map[string]int{
		"SortValue": len(dm.SortValue), "FieldTermLocations": len(dm.FieldTermLocations),
		"docNumbers": len(dm.docNumbers), "numScratch": len(dm.numScratch),
		"termScratch": len(dm.termScratch), "termBytes": len(dm.termBytes),
		"docValues[category]": len(dm.docValues["category"]),
	} {
		if n != 0 {
			t.Errorf("%s has len %d after Reset", name, n)
		}
	}
	if cap(dm.SortValue) != sortCap || cap(dm.FieldTermLocations) != ftlCap ||
		cap(dm.docNumbers) != numCap || cap(dm.termBytes) != bytesCap {
		t.Error("Reset dropped a buffer's backing array")
	}
	if dm.docValues == nil {
		t.Error("Reset dropped the docValues map")
	}
	for name, values := range map[string][][]byte{
		"terms": dm.termScratch, "docValues": dm.docValues["category"],
	} {
		for _, value := range values[:cap(values)] {
			if value != nil {
				t.Errorf("Reset retained %s bytes", name)
			}
		}
	}
	for _, value := range dm.docNumbers[:cap(dm.docNumbers)] {
		if value != (docNumber{}) {
			t.Error("Reset retained a numeric field reference")
		}
	}
	for _, value := range dm.FieldTermLocations[:cap(dm.FieldTermLocations)] {
		if !reflect.DeepEqual(value, FieldTermLocation{}) {
			t.Error("Reset retained a field term location")
		}
	}
	if dm.termVisitor == nil || dm.numVisitor == nil {
		t.Error("Reset dropped the bound visitors")
	}

	// the field list is the contract: adding a field means deciding whether
	// Reset clears or keeps it, and updating this test
	if n := reflect.TypeOf(DocumentMatch{}).NumField(); n != 15 {
		t.Fatalf("DocumentMatch has %d fields; update Reset and this test", n)
	}
}

// BenchmarkDocumentMatchReset runs once per pooled candidate on the search
// hot path (DocumentMatchPool.Put); it must stay allocation-free and in the
// low single-digit ns range.
func BenchmarkDocumentMatchReset(b *testing.B) {
	dm := &DocumentMatch{}
	dm.termVisitor, dm.numVisitor = dm.addDocValue, dm.addDocNumber
	b.ReportAllocs()
	for b.Loop() {
		dm.Number = 1
		dm.Reset()
	}
}
