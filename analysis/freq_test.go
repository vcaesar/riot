//  Copyright (c) 2020 Couchbase, Inc.
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

package analysis

import (
	"reflect"
	"testing"
)

func TestTokenFrequency(t *testing.T) {
	tokens := TokenStream{
		&Token{
			Term:         []byte("water"),
			PositionIncr: 1,
			Start:        0,
			End:          5,
		},
		&Token{
			Term:         []byte("water"),
			PositionIncr: 1,
			Start:        6,
			End:          11,
		},
	}
	expectedResult := TokenFrequencies{
		"water": &TokenFreq{
			TermVal: []byte("water"),
			Locations: []*TokenLocation{
				{
					PositionVal: 1,
					StartVal:    0,
					EndVal:      5,
				},
				{
					PositionVal: 2,
					StartVal:    6,
					EndVal:      11,
				},
			},
			frequency: 2,
		},
	}
	result, _ := TokenFrequency(tokens, true, 0)
	if !reflect.DeepEqual(result, expectedResult) {
		t.Errorf("expected %#v, got %#v", expectedResult, result)
	}
}

func TestTokenFrequenciesMergeAll(t *testing.T) {
	tf1 := TokenFrequencies{
		"water": &TokenFreq{
			TermVal: []byte("water"),
			Locations: []*TokenLocation{
				{
					PositionVal: 1,
					StartVal:    0,
					EndVal:      5,
				},
				{
					PositionVal: 2,
					StartVal:    6,
					EndVal:      11,
				},
			},
		},
	}
	tf2 := TokenFrequencies{
		"water": &TokenFreq{
			TermVal: []byte("water"),
			Locations: []*TokenLocation{
				{
					PositionVal: 1,
					StartVal:    0,
					EndVal:      5,
				},
				{
					PositionVal: 2,
					StartVal:    6,
					EndVal:      11,
				},
			},
		},
	}
	expectedResult := TokenFrequencies{
		"water": &TokenFreq{
			TermVal: []byte("water"),
			Locations: []*TokenLocation{
				{
					PositionVal: 1,
					StartVal:    0,
					EndVal:      5,
				},
				{
					PositionVal: 2,
					StartVal:    6,
					EndVal:      11,
				},
				{
					FieldVal:    "tf2",
					PositionVal: 1,
					StartVal:    0,
					EndVal:      5,
				},
				{
					FieldVal:    "tf2",
					PositionVal: 2,
					StartVal:    6,
					EndVal:      11,
				},
			},
		},
	}
	tf1.MergeAll("tf2", tf2)
	if !reflect.DeepEqual(tf1, expectedResult) {
		t.Errorf("expected %#v, got %#v", expectedResult, tf1)
	}
}

func TestTokenFrequenciesMergeAllLeftEmpty(t *testing.T) {
	tf1 := TokenFrequencies{}
	tf2 := TokenFrequencies{
		"water": &TokenFreq{
			TermVal: []byte("water"),
			Locations: []*TokenLocation{
				{
					PositionVal: 1,
					StartVal:    0,
					EndVal:      5,
				},
				{
					PositionVal: 2,
					StartVal:    6,
					EndVal:      11,
				},
			},
		},
	}
	expectedResult := TokenFrequencies{
		"water": &TokenFreq{
			TermVal: []byte("water"),
			Locations: []*TokenLocation{
				{
					FieldVal:    "tf2",
					PositionVal: 1,
					StartVal:    0,
					EndVal:      5,
				},
				{
					FieldVal:    "tf2",
					PositionVal: 2,
					StartVal:    6,
					EndVal:      11,
				},
			},
		},
	}
	tf1.MergeAll("tf2", tf2)
	if !reflect.DeepEqual(tf1, expectedResult) {
		t.Errorf("expected %#v, got %#v", expectedResult, tf1)
	}
}

// TestTokenFrequencyBlockAllocation checks a second location for a repeated
// term does not clobber the neighbouring term's location pointer, which
// would happen if the shared pointer block were appended to in place.
func TestTokenFrequencyBlockAllocation(t *testing.T) {
	tokens := TokenStream{
		{Term: []byte("a"), Start: 0, End: 1, PositionIncr: 1},
		{Term: []byte("b"), Start: 2, End: 3, PositionIncr: 1},
		{Term: []byte("a"), Start: 4, End: 5, PositionIncr: 1},
		{Term: []byte("c"), Start: 6, End: 7, PositionIncr: 1},
	}
	tfs, pos := TokenFrequency(tokens, true, 0)
	if pos != 4 || len(tfs) != 3 {
		t.Fatalf("got pos %d, %d terms", pos, len(tfs))
	}
	want := map[string][]int{"a": {1, 3}, "b": {2}, "c": {4}}
	for term, positions := range want {
		tf := tfs[term]
		if tf.Frequency() != len(positions) || len(tf.Locations) != len(positions) {
			t.Fatalf("term %q: freq %d, %d locations, want %d", term, tf.Frequency(), len(tf.Locations), len(positions))
		}
		for i, p := range positions {
			if tf.Locations[i].Pos() != p {
				t.Fatalf("term %q location %d: pos %d, want %d", term, i, tf.Locations[i].Pos(), p)
			}
		}
	}
}

// TestTokenFrequenciesMergeAllDoesNotAliasSource checks that appending to a
// merged term never writes into the source field's location slice.
func TestTokenFrequenciesMergeAllDoesNotAliasSource(t *testing.T) {
	src, _ := TokenFrequency(TokenStream{
		{Term: []byte("x"), Start: 0, End: 1, PositionIncr: 1},
		{Term: []byte("x"), Start: 2, End: 3, PositionIncr: 1},
		{Term: []byte("x"), Start: 4, End: 5, PositionIncr: 1},
	}, true, 0)
	srcLocs := src["x"].Locations
	if cap(srcLocs) <= len(srcLocs) {
		t.Skip("source slice has no spare capacity; aliasing cannot be observed")
	}

	composite := TokenFrequencies{}
	composite.MergeAll("f1", src)
	other, _ := TokenFrequency(TokenStream{{Term: []byte("x"), Start: 9, End: 10, PositionIncr: 1}}, true, 0)
	composite.MergeAll("f2", other)

	if got := len(composite["x"].Locations); got != 4 {
		t.Fatalf("composite has %d locations, want 4", got)
	}
	if len(src["x"].Locations) != 3 || srcLocs[:cap(srcLocs)][3] != nil {
		t.Fatal("merge wrote into the source field's spare capacity")
	}
	for _, l := range src["x"].Locations {
		if l.Field() != "f1" {
			t.Fatalf("source location field = %q, want f1", l.Field())
		}
	}
}

func TestTokenFrequenciesMergeAllExistingTermsNoAlloc(t *testing.T) {
	src, _ := TokenFrequency(TokenStream{{Term: []byte("x"), End: 1, PositionIncr: 1}}, false, 0)
	composite := TokenFrequencies{}
	composite.MergeAll("f", src)
	allocs := testing.AllocsPerRun(50, func() { composite.MergeAll("f", src) })
	if allocs != 0 {
		t.Fatalf("merging only known terms allocated %v times, want 0", allocs)
	}
	if composite["x"].Frequency() != 52 {
		t.Fatalf("frequency = %d, want 52", composite["x"].Frequency())
	}
}
