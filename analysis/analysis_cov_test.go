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
	"reflect"
	"strings"
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"
)

type upperCharFilter struct{}

func (upperCharFilter) Filter(in []byte) []byte { return bytes.ToUpper(in) }

type fieldsTokenizer struct{}

func (fieldsTokenizer) Tokenize(in []byte) TokenStream {
	var rv TokenStream
	for i, f := range bytes.Fields(in) {
		rv = append(rv, &Token{Term: f, PositionIncr: 1, Start: i, End: i + len(f)})
	}
	return rv
}

type dropFirstFilter struct{}

func (dropFirstFilter) Filter(in TokenStream) TokenStream { return in[1:] }

func TestAnalyzerAnalyze(t *testing.T) {
	tests := []struct {
		name     string
		analyzer *Analyzer
		want     []string
	}{
		{"tokenizer only", &Analyzer{Tokenizer: fieldsTokenizer{}}, []string{"a", "b", "c"}},
		{"all stages", &Analyzer{
			CharFilters:  []CharFilter{upperCharFilter{}},
			Tokenizer:    fieldsTokenizer{},
			TokenFilters: []TokenFilter{dropFirstFilter{}},
		}, []string{"B", "C"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, tok := range tt.analyzer.Analyze([]byte("a b c")) {
				got = append(got, string(tok.Term))
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTokenString(t *testing.T) {
	tok := &Token{Term: []byte("x"), Start: 1, End: 2, PositionIncr: 3, Type: Numeric}
	want := "Start: 1  End: 2  PositionIncr: 3  Token: x  Type: 2"
	if got := tok.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTokenLocationAndFreqAccessors(t *testing.T) {
	tfs, _ := TokenFrequencySlice(TokenStream{
		{Term: []byte("a"), PositionIncr: 1, Start: 0, End: 1},
		{Term: []byte("a"), PositionIncr: 1, Start: 2, End: 3},
	}, true, 0)
	tf := &tfs[0]
	tf.Locations[0].FieldVal = "f"
	if tf.Size() <= reflectStaticSizeTokenFreq {
		t.Errorf("unexpected TokenFreq size %d", tf.Size())
	}
	var locs []segment.Location
	tf.EachLocation(func(l segment.Location) { locs = append(locs, l) })
	if len(locs) != 2 {
		t.Fatalf("expected 2 locations, got %d", len(locs))
	}
	l := locs[0]
	if l.Field() != "f" || l.Start() != 0 || l.End() != 1 || l.Pos() != 1 || l.Size() != reflectStaticSizeTokenLocation {
		t.Errorf("unexpected location %+v", l)
	}
	if l := locs[1]; l.Field() != "" || l.Start() != 2 || l.End() != 3 || l.Pos() != 2 {
		t.Errorf("unexpected location %+v", l)
	}
	m := NewTokenFrequencies(tfs)
	if m.Size() <= tf.Size() {
		t.Errorf("unexpected TokenFrequencies size %d", m.Size())
	}
}

func TestMergeOneBytes(t *testing.T) {
	tfs := TokenFrequencies{}
	src := &TokenFreq{TermVal: []byte("a"), frequency: 1, Locations: []TokenLocation{{StartVal: 0, EndVal: 1}}}
	tfs.MergeOneBytes("f1", []byte("a"), src)
	src2 := &TokenFreq{TermVal: []byte("a"), frequency: 2, Locations: []TokenLocation{{StartVal: 2}, {StartVal: 4}}}
	tfs.MergeOneBytes("f2", []byte("a"), src2)
	got := tfs["a"]
	if got.Frequency() != 3 || len(got.Locations) != 3 {
		t.Fatalf("unexpected merged freq %d locs %d", got.Frequency(), len(got.Locations))
	}
	if got.Locations[0].FieldVal != "f1" || got.Locations[2].FieldVal != "f2" {
		t.Errorf("unexpected fields %+v", got.Locations)
	}
	// the first merge must clone, not alias, the source locations
	src.Locations[0].StartVal = 99
	if got.Locations[0].StartVal == 99 {
		t.Errorf("merged locations alias the source")
	}
}

func TestAppendTerm(t *testing.T) {
	var tfs []TokenFreq
	for _, term := range []string{"a", "b", "a"} {
		tfs = AppendTerm(tfs, []byte(term))
	}
	if len(tfs) != 2 || tfs[0].Frequency() != 2 || tfs[1].Frequency() != 1 {
		t.Errorf("unexpected %+v", tfs)
	}
}

func TestTokenFrequencyPooled(t *testing.T) {
	tokens := TokenStream{
		{Term: []byte("x"), PositionIncr: 1, Start: 0, End: 1},
		{Term: []byte("y"), PositionIncr: 1, Start: 2, End: 3},
		{Term: []byte("x"), PositionIncr: 1, Start: 4, End: 5},
	}
	for _, vectors := range []bool{true, false} {
		tfs, pos, blk := TokenFrequencyPooled(tokens, vectors, 10)
		wantPos := 0
		if vectors {
			wantPos = 13
		}
		if len(tfs) != 2 || tfs[0].Frequency() != 2 || pos != wantPos {
			t.Errorf("vectors=%v: tfs %+v pos %d", vectors, tfs, pos)
		}
		if vectors && (len(tfs[0].Locations) != 2 || tfs[0].Locations[1].PositionVal != 13) {
			t.Errorf("unexpected locations %+v", tfs[0].Locations)
		}
		if got := blk.CopyText([]byte("text")); string(got) != "text" {
			t.Errorf("CopyText got %q", got)
		}
		ReleaseFreqBlock(blk)
	}

	// oversized blocks are dropped rather than pooled
	big := make(TokenStream, maxPooledFreqBlock+1)
	for i := range big {
		big[i] = &Token{Term: []byte("z"), PositionIncr: 1}
	}
	tfs, _, blk := TokenFrequencyPooled(big, true, 0)
	if len(tfs) != 1 || tfs[0].Frequency() != len(big) {
		t.Errorf("unexpected big result %d", len(tfs))
	}
	ReleaseFreqBlock(blk)
}

func TestRuneHelpers(t *testing.T) {
	if got := DeleteRune([]rune("abc"), 5); string(got) != "abc" {
		t.Errorf("DeleteRune out of range got %q", string(got))
	}
	if got := TruncateRunes([]byte("héllo"), 2); string(got) != "hél" {
		t.Errorf("TruncateRunes got %q", got)
	}
	tests := []struct {
		in, suffix string
		want       bool
	}{
		{"héllo", "llo", true},
		{"héllo", "éllo", true},
		{"héllo", "xlo", false},
		{"lo", "hello", false},
		{"lo", "", true},
	}
	for _, tt := range tests {
		if got := RunesEndsWith([]rune(tt.in), tt.suffix); got != tt.want {
			t.Errorf("RunesEndsWith(%q, %q) = %v", tt.in, tt.suffix, got)
		}
	}
}

func TestTokenMapLoadErrors(t *testing.T) {
	tm := NewTokenMap()
	if err := tm.LoadFile("does-not-exist.txt"); err == nil {
		t.Error("expected error for missing file")
	}
	tm.LoadBytes([]byte(strings.Join([]string{"a # c", "b | d", "e"}, "\n")))
	if len(tm) != 3 || !tm["a"] || !tm["b"] || !tm["e"] {
		t.Errorf("unexpected token map %v", tm)
	}
}
