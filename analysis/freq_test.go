//  Copyright (c) 2020 Couchbase, Inc.
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

package analysis

import (
	"bytes"
	"math"
	"reflect"
	"strconv"
	"testing"
	"unsafe"
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
			Locations: []TokenLocation{
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
			Locations: []TokenLocation{
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
			Locations: []TokenLocation{
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
			Locations: []TokenLocation{
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
			Locations: []TokenLocation{
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
			Locations: []TokenLocation{
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
	if len(src["x"].Locations) != 3 || srcLocs[:cap(srcLocs)][3] != (TokenLocation{}) {
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

// TestTokenFrequencySliceDedupPaths covers the linear scan (short streams)
// and the pooled map (long streams), with and without term vectors, and
// checks a reused scratch map carries nothing over between calls.
func TestTokenFrequencySliceDedupPaths(t *testing.T) {
	stream := func(n int) TokenStream {
		var tokens TokenStream
		for i := range n {
			term := []byte{byte('a' + i%7), byte('a' + i%3)}
			tokens = append(tokens, &Token{Term: term, Start: 3 * i, End: 3*i + 2, PositionIncr: 1})
		}
		return tokens
	}
	for _, n := range []int{0, 1, linearDedupMax, linearDedupMax + 1, 200} {
		for _, vectors := range []bool{false, true} {
			tokens := stream(n)
			tfs, pos := TokenFrequencySlice(tokens, vectors, 10)
			wantPos := 0
			if vectors {
				wantPos = 10 + n
			}
			if pos != wantPos {
				t.Fatalf("n=%d vectors=%v: pos %d, want %d", n, vectors, pos, wantPos)
			}
			freq := map[string]int{}
			var order []string
			for _, tok := range tokens {
				if freq[string(tok.Term)] == 0 {
					order = append(order, string(tok.Term))
				}
				freq[string(tok.Term)]++
			}
			if len(tfs) != len(order) {
				t.Fatalf("n=%d: %d terms, want %d", n, len(tfs), len(order))
			}
			total := 0
			for i, tf := range tfs {
				if string(tf.Term()) != order[i] || tf.Frequency() != freq[order[i]] {
					t.Fatalf("n=%d term %d: %q x%d, want %q x%d", n, i, tf.Term(), tf.Frequency(), order[i], freq[order[i]])
				}
				if vectors != (len(tf.Locations) == tf.Frequency()) && n > 0 {
					t.Fatalf("n=%d term %q: %d locations for freq %d", n, tf.Term(), len(tf.Locations), tf.Frequency())
				}
				for _, loc := range tf.Locations {
					if tok := tokens[loc.Pos()-11]; string(tok.Term) != order[i] || tok.Start != loc.Start() {
						t.Fatalf("n=%d term %q: location %+v points at %q", n, tf.Term(), loc, tok.Term)
					}
				}
				total += tf.Frequency()
			}
			if total != n {
				t.Fatalf("n=%d: frequencies sum to %d", n, total)
			}
		}
	}
}

func TestNewTokenFrequenciesPointsIntoSlice(t *testing.T) {
	tfs, _ := TokenFrequencySlice(TokenStream{{Term: []byte("x")}, {Term: []byte("y")}}, false, 0)
	m := NewTokenFrequencies(tfs)
	if len(m) != 2 || m["x"] != &tfs[0] || m["y"] != &tfs[1] {
		t.Fatalf("map %v does not index the slice", m)
	}
}

// Each term's locations are a capped window of one field block, so
// appending to one term never overwrites its neighbour's locations.
func TestTokenFrequencyLocationWindows(t *testing.T) {
	if s := unsafe.Sizeof(TokenLocation{}); s != 32 {
		t.Fatalf("TokenLocation is %d bytes, want 32", s)
	}
	tfs, _ := TokenFrequencySlice(TokenStream{
		{Term: []byte("a"), Start: 0, End: 1, PositionIncr: 1},
		{Term: []byte("b"), Start: 2, End: 3, PositionIncr: 1},
		{Term: []byte("a"), Start: 4, End: 5, PositionIncr: 1},
	}, true, 0)
	a, b := &tfs[0], &tfs[1]
	if len(a.Locations) != 2 || cap(a.Locations) != 2 || len(b.Locations) != 1 {
		t.Fatalf("windows a=%d/%d b=%d", len(a.Locations), cap(a.Locations), len(b.Locations))
	}
	a.Locations = append(a.Locations, TokenLocation{PositionVal: 99})
	if b.Locations[0].Pos() != 2 || b.Locations[0].Start() != 2 {
		t.Fatalf("append to a clobbered b: %+v", b.Locations[0])
	}
}

func TestLoc32(t *testing.T) {
	for v, want := range map[int]int32{0: 0, 7: 7, math.MaxInt32: math.MaxInt32, -3: -3} {
		if got := Loc32(v); got != want {
			t.Fatalf("Loc32(%d) = %d", v, got)
		}
	}
	if strconv.IntSize == 64 {
		for _, v := range []int{math.MaxInt32 + 1, math.MinInt32 - 1} {
			if got := Loc32(v); got != -1 {
				t.Fatalf("Loc32(%d) = %d, want -1", v, got)
			}
		}
	}
}

// SingleTokenFrequency matches TokenFrequencySlice of the one-token stream
// a keyword field used to build, with and without term vectors.
func TestSingleTokenFrequency(t *testing.T) {
	for _, term := range [][]byte{[]byte("doc-42"), {}} {
		for _, vectors := range []bool{false, true} {
			stream := TokenStream{{Term: term, End: len(term), PositionIncr: 1, Type: AlphaNumeric}}
			want, wantPos := TokenFrequencySlice(stream, vectors, 7)
			got, gotPos := SingleTokenFrequency(term, vectors, 7)
			if gotPos != wantPos || len(got) != 1 || len(want) != 1 {
				t.Fatalf("%q vectors=%v: pos %d/%d len %d/%d", term, vectors, gotPos, wantPos, len(got), len(want))
			}
			g, w := got[0], want[0]
			if !bytes.Equal(g.TermVal, w.TermVal) || g.Frequency() != w.Frequency() ||
				!reflect.DeepEqual(g.Locations, w.Locations) || cap(g.Locations) != len(g.Locations) {
				t.Fatalf("%q vectors=%v: got %+v want %+v", term, vectors, g, w)
			}
		}
	}
}
