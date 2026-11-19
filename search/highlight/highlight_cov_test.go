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

package highlight

import (
	"reflect"
	"strings"
	"testing"

	"github.com/vcaesar/riot/search"
)

func TestOverlapsBothDirections(t *testing.T) {
	for _, test := range []struct {
		name       string
		a, b       [2]int
		wantResult bool
	}{
		{name: "b starts inside a", a: [2]int{0, 5}, b: [2]int{3, 8}, wantResult: true},
		{name: "a starts inside b", a: [2]int{3, 8}, b: [2]int{0, 5}, wantResult: true},
		{name: "disjoint", a: [2]int{0, 2}, b: [2]int{5, 8}, wantResult: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fa := &Fragment{Start: test.a[0], End: test.a[1]}
			fb := &Fragment{Start: test.b[0], End: test.b[1]}
			if got := fa.Overlaps(fb); got != test.wantResult {
				t.Errorf("Fragment.Overlaps = %v, want %v", got, test.wantResult)
			}
			ta := &TermLocation{Start: test.a[0], End: test.a[1]}
			tb := &TermLocation{Start: test.b[0], End: test.b[1]}
			if got := ta.Overlaps(tb); got != test.wantResult {
				t.Errorf("TermLocation.Overlaps = %v, want %v", got, test.wantResult)
			}
		})
	}
}

func TestSimpleFragmenterEdgeCases(t *testing.T) {
	long := []byte(strings.Repeat("a", defaultFragmentSize+10))
	for _, test := range []struct {
		name  string
		frag  *SimpleFragmenter
		orig  []byte
		ot    TermLocations
		wants []*Fragment
	}{
		{
			name:  "no terms short text",
			frag:  NewSimpleFragmenter(),
			orig:  []byte("short"),
			wants: []*Fragment{{Orig: []byte("short"), Start: 0, End: 5}},
		},
		{
			name:  "no terms long text",
			frag:  NewSimpleFragmenter(),
			orig:  long,
			wants: []*Fragment{{Orig: long, Start: 0, End: defaultFragmentSize}},
		},
		{
			name: "invalid rune after start",
			frag: NewSimpleFragmenterSized(5),
			orig: []byte("ab\xffcd"),
			ot:   TermLocations{{Term: "x", Start: 1, End: 2}},
		},
		{
			name: "start out of bounds",
			frag: NewSimpleFragmenterSized(5),
			orig: []byte("abc"),
			ot:   TermLocations{{Term: "x", Start: 10, End: 12}},
		},
		{
			name: "invalid rune before start",
			frag: NewSimpleFragmenterSized(5),
			orig: []byte("a\xffbc"),
			ot:   TermLocations{{Term: "x", Start: 2, End: 3}},
		},
		{
			name: "invalid rune while centering",
			frag: NewSimpleFragmenterSized(3),
			orig: []byte("abcd\xffefgh"),
			ot:   TermLocations{{Term: "e", Start: 5, End: 6}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := test.frag.Fragment(test.orig, test.ot)
			if !reflect.DeepEqual(got, test.wants) {
				t.Errorf("Fragment() = %+v, want %+v", got, test.wants)
			}
		})
	}
}

func TestHighlighterConstructors(t *testing.T) {
	tlm := search.TermLocationMap{"fox": {{Pos: 1, Start: 4, End: 7}}}
	orig := []byte("the fox ran")
	for _, test := range []struct {
		name string
		h    *SimpleHighlighter
		want string
	}{
		{name: "ansi", h: NewANSIHighlighter(), want: "the " + DefaultAnsiHighlight + "fox" + reset + " ran"},
		{name: "ansi color", h: NewANSIHighlighterColor("\x1b[31m"), want: "the \x1b[31mfox" + reset + " ran"},
		{name: "html", h: NewHTMLHighlighter(), want: "the <mark>fox</mark> ran"},
		{name: "html tags", h: NewHTMLHighlighterTags("<b>", "</b>"), want: "the <b>fox</b> ran"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.h.BestFragment(tlm, orig); got != test.want {
				t.Errorf("BestFragment() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestBestFragmentsEdgeCases(t *testing.T) {
	formatter := NewHTMLFragmentFormatterTags("[", "]")
	for _, test := range []struct {
		name  string
		frag  Fragmenter
		tlm   search.TermLocationMap
		orig  string
		num   int
		wants []string
	}{
		{
			name:  "no fragments",
			frag:  NewSimpleFragmenterSized(5),
			tlm:   search.TermLocationMap{"x": {{Start: 1, End: 2}}},
			orig:  "a\xffbc",
			num:   1,
			wants: []string{},
		},
		{
			name:  "fewer fragments than requested",
			frag:  NewSimpleFragmenterSized(100),
			tlm:   search.TermLocationMap{"fox": {{Start: 4, End: 7}}},
			orig:  "the fox",
			num:   3,
			wants: []string{"the [fox]"},
		},
		{
			name: "overlapping last candidate",
			frag: NewSimpleFragmenterSized(100),
			tlm: search.TermLocationMap{
				"quick": {{Pos: 1, Start: 4, End: 9}},
				"fox":   {{Pos: 2, Start: 10, End: 13}},
			},
			orig:  "the quick fox",
			num:   2,
			wants: []string{"the [quick] [fox]"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := NewSimpleHighlighter(test.frag, formatter, DefaultSeparator)
			got := h.BestFragments(test.tlm, []byte(test.orig), test.num)
			if !reflect.DeepEqual(got, test.wants) {
				t.Errorf("BestFragments() = %q, want %q", got, test.wants)
			}
			if test.num == 1 && len(test.wants) == 0 {
				if got := h.BestFragment(test.tlm, []byte(test.orig)); got != "" {
					t.Errorf("BestFragment() = %q, want empty", got)
				}
			}
		})
	}
}
