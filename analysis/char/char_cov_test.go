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

package char

import (
	"testing"
	"unicode/utf8"
)

func TestAsciiFoldingAllBMPRunes(t *testing.T) {
	filter := NewASCIIFoldingFilter()
	for r := rune(0x80); r <= 0xFFFF; r++ {
		if r >= 0xD800 && r <= 0xDFFF {
			continue
		}
		out := filter.Filter([]byte(string(r)))
		if len(out) == 0 || !utf8.Valid(out) {
			t.Fatalf("rune %U folded to invalid output %q", r, out)
		}
		if utf8.RuneCount(out) > maxRuneExpansion {
			t.Fatalf("rune %U expanded beyond %d runes: %q", r, maxRuneExpansion, out)
		}
	}
}

func TestAsciiFoldingSpotChecks(t *testing.T) {
	tests := map[string]string{
		"À":  "A",
		"ß":  "ss",
		"Ǆ":  "DZ",
		"⑽":  "(10)",
		"～":  "~",
		"＿":  "_",
		"中":  "中",
		"aé": "ae",
	}
	filter := NewASCIIFoldingFilter()
	for in, want := range tests {
		if got := string(filter.Filter([]byte(in))); got != want {
			t.Errorf("fold(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHTMLAndZeroWidthCharFilters(t *testing.T) {
	tests := []struct {
		name   string
		filter *RegexpCharFilter
		input  string
		want   string
	}{
		{"html", NewHTMLCharFilter(), "<b>bold</b>", " bold "},
		{"html attrs", NewHTMLCharFilter(), `<a href="x">l</a>`, " l "},
		{"zwnj", NewZeroWidthNonJoinerCharFilter(), "a\u200Cb", "a b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(tt.filter.Filter([]byte(tt.input))); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
