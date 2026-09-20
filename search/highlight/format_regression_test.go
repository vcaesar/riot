// Copyright (c) 2026 The Bluge Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package highlight

import (
	"bytes"
	"strings"
	"testing"
)

func TestFragmentFormatterRanges(t *testing.T) {
	for _, test := range []struct {
		name      string
		fragment  Fragment
		locations TermLocations
		html      string
		ansi      string
	}{
		{name: "empty"},
		{
			name:     "unmatched escaped text",
			fragment: Fragment{Orig: []byte("<&é>\"'"), End: 7},
			html:     "&lt;&amp;é&gt;&#34;&#39;",
			ansi:     "<&é>\"'",
		},
		{
			name:      "nil and out of fragment ranges",
			fragment:  Fragment{Orig: []byte("xx<&é>zz"), Start: 2, End: 7},
			locations: TermLocations{nil, {Start: 0, End: 2}, {Start: 2, End: 7}, {Start: 7, End: 9}},
			html:      "<em>&lt;&amp;é&gt;</em>",
			ansi:      FgRed + "<&é>" + Reset,
		},
		{
			name:      "overlap adjacent and trailing text",
			fragment:  Fragment{Orig: []byte("abcdef"), End: 6},
			locations: TermLocations{{Start: 0, End: 2}, nil, {Start: 1, End: 3}, {Start: 2, End: 4}},
			html:      "<em>ab</em><em>cd</em>ef",
			ansi:      FgRed + "ab" + Reset + FgRed + "cd" + Reset + "ef",
		},
		{
			name:      "range crosses fragment end",
			fragment:  Fragment{Orig: []byte("abcdef"), Start: 1, End: 4},
			locations: TermLocations{{Start: 2, End: 5}},
			html:      "bcd",
			ansi:      "bcd",
		},
		{
			name:      "zero width range",
			fragment:  Fragment{Orig: []byte("ab"), End: 2},
			locations: TermLocations{{Start: 1, End: 1}},
			html:      "a<em></em>b",
			ansi:      "a" + FgRed + Reset + "b",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			orig := append([]byte(nil), test.fragment.Orig...)
			for _, formatter := range []struct {
				name      string
				formatter FragmentFormatter
				want      string
			}{
				{"HTML", NewHTMLFragmentFormatterTags("<em>", "</em>"), test.html},
				{"ANSI", NewANSIFragmentFormatterColor(FgRed), test.ansi},
			} {
				t.Run(formatter.name, func(t *testing.T) {
					for i := 0; i < 2; i++ {
						if got := formatter.formatter.Format(&test.fragment, test.locations); got != formatter.want {
							t.Fatalf("got %q, want %q", got, formatter.want)
						}
					}
					if !bytes.Equal(orig, test.fragment.Orig) {
						t.Fatal("formatter changed source text")
					}
				})
			}
		})
	}
}

func TestFragmentFormatterDense(t *testing.T) {
	const count = 256
	orig := []byte(strings.Repeat("<&é> ", count))
	fragment := &Fragment{Orig: orig, End: len(orig)}
	locations := make(TermLocations, count)
	for i := range locations {
		locations[i] = &TermLocation{Start: i * 6, End: i*6 + 5}
	}
	if got, want := NewHTMLFragmentFormatter().Format(fragment, locations),
		strings.Repeat("<mark>&lt;&amp;é&gt;</mark> ", count); got != want {
		t.Fatalf("HTML output differs: got %q, want %q", got, want)
	}
	if got, want := NewANSIFragmentFormatter().Format(fragment, locations),
		strings.Repeat(BgYellow+"<&é>"+Reset+" ", count); got != want {
		t.Fatalf("ANSI output differs: got %q, want %q", got, want)
	}
}
