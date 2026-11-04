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

package tokenizer

import (
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/vcaesar/riot/analysis"
	"github.com/zedsearch/segment"
)

func TestUnicode(t *testing.T) {
	tests := []struct {
		input  []byte
		output analysis.TokenStream
	}{
		{
			[]byte("Hello World"),
			analysis.TokenStream{
				{
					Start:        0,
					End:          5,
					Term:         []byte("Hello"),
					PositionIncr: 1,
					Type:         analysis.AlphaNumeric,
				},
				{
					Start:        6,
					End:          11,
					Term:         []byte("World"),
					PositionIncr: 1,
					Type:         analysis.AlphaNumeric,
				},
			},
		},
		{
			[]byte("steven's"),
			analysis.TokenStream{
				{
					Start:        0,
					End:          8,
					Term:         []byte("steven's"),
					PositionIncr: 1,
					Type:         analysis.AlphaNumeric,
				},
			},
		},
		{
			[]byte("こんにちは世界"),
			analysis.TokenStream{
				{
					Start:        0,
					End:          3,
					Term:         []byte("こ"),
					PositionIncr: 1,
					Type:         analysis.Ideographic,
				},
				{
					Start:        3,
					End:          6,
					Term:         []byte("ん"),
					PositionIncr: 1,
					Type:         analysis.Ideographic,
				},
				{
					Start:        6,
					End:          9,
					Term:         []byte("に"),
					PositionIncr: 1,
					Type:         analysis.Ideographic,
				},
				{
					Start:        9,
					End:          12,
					Term:         []byte("ち"),
					PositionIncr: 1,
					Type:         analysis.Ideographic,
				},
				{
					Start:        12,
					End:          15,
					Term:         []byte("は"),
					PositionIncr: 1,
					Type:         analysis.Ideographic,
				},
				{
					Start:        15,
					End:          18,
					Term:         []byte("世"),
					PositionIncr: 1,
					Type:         analysis.Ideographic,
				},
				{
					Start:        18,
					End:          21,
					Term:         []byte("界"),
					PositionIncr: 1,
					Type:         analysis.Ideographic,
				},
			},
		},
		{
			[]byte("age 25"),
			analysis.TokenStream{
				{
					Start:        0,
					End:          3,
					Term:         []byte("age"),
					PositionIncr: 1,
					Type:         analysis.AlphaNumeric,
				},
				{
					Start:        4,
					End:          6,
					Term:         []byte("25"),
					PositionIncr: 1,
					Type:         analysis.Numeric,
				},
			},
		},
		{
			[]byte("カ"),
			analysis.TokenStream{
				{
					Start:        0,
					End:          3,
					Term:         []byte("カ"),
					PositionIncr: 1,
					Type:         analysis.Ideographic,
				},
			},
		},
	}

	for _, test := range tests {
		tokenizer := NewUnicodeTokenizer()
		actual := tokenizer.Tokenize(test.input)

		if !reflect.DeepEqual(actual, test.output) {
			t.Errorf("Expected %v, got %v for %s", test.output, actual, string(test.input))
		}
	}
}

var sampleLargeInput = []byte(`There are three characteristics of liquids which are relevant to the discussion of a BLEVE:
If a liquid in a sealed container is boiled, the pressure inside the container increases. As the liquid changes to a gas it expands - this expansion in a vented container would cause the gas and liquid to take up more space. In a sealed container the gas and liquid are not able to take up more space and so the pressure rises. Pressurized vessels containing liquids can reach an equilibrium where the liquid stops boiling and the pressure stops rising. This occurs when no more heat is being added to the system (either because it has reached ambient temperature or has had a heat source removed).
The boiling temperature of a liquid is dependent on pressure - high pressures will yield high boiling temperatures, and low pressures will yield low boiling temperatures. A common simple experiment is to place a cup of water in a vacuum chamber, and then reduce the pressure in the chamber until the water boils. By reducing the pressure the water will boil even at room temperature. This works both ways - if the pressure is increased beyond normal atmospheric pressures, the boiling of hot water could be suppressed far beyond normal temperatures. The cooling system of a modern internal combustion engine is a real-world example.
When a liquid boils it turns into a gas. The resulting gas takes up far more space than the liquid did.
Typically, a BLEVE starts with a container of liquid which is held above its normal, atmospheric-pressure boiling temperature. Many substances normally stored as liquids, such as CO2, oxygen, and other similar industrial gases have boiling temperatures, at atmospheric pressure, far below room temperature. In the case of water, a BLEVE could occur if a pressurized chamber of water is heated far beyond the standard 100 °C (212 °F). That container, because the boiling water pressurizes it, is capable of holding liquid water at very high temperatures.
If the pressurized vessel, containing liquid at high temperature (which may be room temperature, depending on the substance) ruptures, the pressure which prevents the liquid from boiling is lost. If the rupture is catastrophic, where the vessel is immediately incapable of holding any pressure at all, then there suddenly exists a large mass of liquid which is at very high temperature and very low pressure. This causes the entire volume of liquid to instantaneously boil, which in turn causes an extremely rapid expansion. Depending on temperatures, pressures and the substance involved, that expansion may be so rapid that it can be classified as an explosion, fully capable of inflicting severe damage on its surroundings.`)

func BenchmarkUnicodeTokenizeEnglishText(b *testing.B) {
	tokenizer := NewUnicodeTokenizer()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		tokenizer.Tokenize(sampleLargeInput)
	}
}

func TestConvertType(t *testing.T) {
	tests := []struct {
		in  int
		out analysis.TokenType
	}{
		{
			segment.Ideo, analysis.Ideographic,
		},
		{
			segment.Kana, analysis.Ideographic,
		},
		{
			segment.Number, analysis.Numeric,
		},
		{
			segment.Letter, analysis.AlphaNumeric,
		},
	}

	for _, test := range tests {
		actual := convertType(test.in)
		if actual != test.out {
			t.Errorf("expected %d, got %d for %d", test.out, actual, test.in)
		}
	}
}

// Short inputs of ordinary words fit the first stream and token block.
func TestUnicodeShortInputAllocations(t *testing.T) {
	input := []byte("common search engine zig document term12 term345 term6 category7 rank42")
	tok := NewUnicodeTokenizer()
	if got := len(tok.Tokenize(input)); got != 10 {
		t.Fatalf("%d tokens, want 10", got)
	}
	if allocs := testing.AllocsPerRun(100, func() { tok.Tokenize(input) }); allocs > 3 {
		t.Fatalf("tokenizing took %v allocations, want at most 3", allocs)
	}
}

// TokenizeInto gives the tokens of Tokenize, also when its scratch held a
// longer input before, and allocates nothing once the scratch is warm.
func TestUnicodeTokenizeInto(t *testing.T) {
	tok := NewUnicodeTokenizer()
	var s Scratch
	long := []byte(strings.Repeat("alpha béta 42 ", 400))
	for _, in := range [][]byte{long, []byte("common search engine"), []byte(""), long} {
		want := tok.Tokenize(in)
		got := tok.TokenizeInto(in, &s)
		if len(got) != len(want) {
			t.Fatalf("%d tokens, want %d", len(got), len(want))
		}
		for i := range want {
			if !reflect.DeepEqual(*got[i], *want[i]) {
				t.Fatalf("token %d: %v, want %v", i, got[i], want[i])
			}
		}
	}
	short := []byte("common search engine zig document term12 term345 term6 category7 rank42")
	if allocs := testing.AllocsPerRun(100, func() { tok.TokenizeInto(short, &s) }); allocs > 1 {
		t.Fatalf("TokenizeInto took %v allocations, want at most 1", allocs)
	}
}

// segmenterTokens tokenizes input with the Unicode segmenter only.
func segmenterTokens(input []byte) analysis.TokenStream {
	t := tokens{input: input, ta: make([]analysis.Token, 4)}
	t.addSegments(0, len(input))
	return t.stream()
}

// The ASCII path, alone or between the spans left to the segmenter, finds
// the words, offsets and types the Unicode segmenter finds.
func TestPlainASCIIMatchesSegmenter(t *testing.T) {
	var alphabet []string
	for b := range byte(0x80) {
		alphabet = append(alphabet, string(rune(b)))
	}
	// underscores, letters, marks, ideographs, kana, digits, spaces and
	// punctuation beyond ASCII, and regional indicators
	other := []string{"_", "é", "\u0301", "°", "中", "文", "カ", "ー", "١", "\u00a0", "\u2019", "\U0001F1FA", "\u200d", "ß"}
	inputs := []string{
		"", " ", "common search engine zig document term5 term100 category3 rank42",
		"42", "4x2", "x42", "42x", "3-4", "a-b", "a/b", "a@b.", "#tag", "(x)", "a\tb\r\nc",
		"\x00a\x7fb", "2026 11 03", "x 7 y8 9z", "can't", "e.g.", "3.14", "1,000", "a:b", "x;1", "1;2",
		"\"q\"", "a.1", "1.a", "a1.b", "1a.2", "a..b", "a.", ".a", "1,", "U.S.A.", "it's.", "12:30", "x'y'z",
		"a_b", "_", "naïve café", "x\u00a0y", "100 °C (212 °F)", "a \u0301b", "中文 text", "e\u0301.g",
		// the double quote joins Hebrew letters (WB7b, WB7c), so it must reach the segmenter
		"צה\"ל", "א\"ב", "x\"y", "ab\"cd", "ג'ירפה", "\"צה\"ל\"", "a\"א\"ב\"c",
		string(sampleLargeInput),
	}
	rng := rand.New(rand.NewPCG(1, 2))
	for range 30000 {
		var in strings.Builder
		for range rng.IntN(24) {
			switch rng.IntN(8) {
			case 0, 1:
				in.WriteString(alphabet[rng.IntN(len(alphabet))])
			case 2, 3:
				in.WriteByte("ab Z09 "[rng.IntN(7)])
			case 4:
				in.WriteString(other[rng.IntN(len(other))])
			default:
				in.WriteByte("x1 -.',:;"[rng.IntN(9)])
			}
		}
		inputs = append(inputs, in.String())
	}
	for _, s := range inputs {
		in := []byte(s)
		want := segmenterTokens(in)
		got := NewUnicodeTokenizer().Tokenize(in)
		if len(got) != len(want) {
			t.Fatalf("%q: %d tokens, segmenter %d", s, len(got), len(want))
		}
		for i := range want {
			if !reflect.DeepEqual(*got[i], *want[i]) {
				t.Fatalf("%q token %d: %+v, segmenter %+v", s, i, *got[i], *want[i])
			}
		}
	}
}

func BenchmarkUnicodeTokenizePlainASCII(b *testing.B) {
	in := []byte("common search engine zig document term5 term100 term1999 category3 rank42")
	b.Run("plain", func(b *testing.B) {
		var s Scratch
		for b.Loop() {
			NewUnicodeTokenizer().TokenizeInto(in, &s)
		}
	})
	b.Run("segmenter", func(b *testing.B) {
		for b.Loop() {
			segmenterTokens(in)
		}
	})
}
