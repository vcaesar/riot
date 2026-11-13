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

package riot

import (
	"bytes"
	"math"
	"strconv"
	"testing"
	"time"

	segment "github.com/vcaesar/bluge_segment_api"

	"github.com/vcaesar/riot/analysis"
	"github.com/vcaesar/riot/numeric"
)

func TestIndexingOptions(t *testing.T) {
	tests := []struct {
		options            FieldOptions
		isIndexed          bool
		isStored           bool
		includeTermVectors bool
		docValues          bool
	}{
		{
			options:            Index | Store | SearchTermPositions,
			isIndexed:          true,
			isStored:           true,
			includeTermVectors: true,
			docValues:          false,
		},
		{
			options:            Index | Store | HighlightMatches,
			isIndexed:          true,
			isStored:           true,
			includeTermVectors: true,
			docValues:          false,
		},
		{
			options:            Index | Store | SearchTermPositions | HighlightMatches,
			isIndexed:          true,
			isStored:           true,
			includeTermVectors: true,
			docValues:          false,
		},
		{
			options:            Index | SearchTermPositions,
			isIndexed:          true,
			isStored:           false,
			includeTermVectors: true,
			docValues:          false,
		},
		{
			options:            Index | HighlightMatches,
			isIndexed:          true,
			isStored:           false,
			includeTermVectors: true,
			docValues:          false,
		},
		{
			options:            Index | SearchTermPositions | HighlightMatches,
			isIndexed:          true,
			isStored:           false,
			includeTermVectors: true,
			docValues:          false,
		},
		{
			options:            Store | SearchTermPositions,
			isIndexed:          false,
			isStored:           true,
			includeTermVectors: true,
			docValues:          false,
		},
		{
			options:            Store | HighlightMatches,
			isIndexed:          false,
			isStored:           true,
			includeTermVectors: true,
			docValues:          false,
		},
		{
			options:            Store | SearchTermPositions | HighlightMatches,
			isIndexed:          false,
			isStored:           true,
			includeTermVectors: true,
			docValues:          false,
		},
		{
			options:            Index,
			isIndexed:          true,
			isStored:           false,
			includeTermVectors: false,
			docValues:          false,
		},
		{
			options:            Store,
			isIndexed:          false,
			isStored:           true,
			includeTermVectors: false,
			docValues:          false,
		},
		{
			options:            Sortable,
			isIndexed:          false,
			isStored:           false,
			includeTermVectors: false,
			docValues:          true,
		},
		{
			options:            Aggregatable,
			isIndexed:          false,
			isStored:           false,
			includeTermVectors: false,
			docValues:          true,
		},
		{
			options:            Sortable | Aggregatable,
			isIndexed:          false,
			isStored:           false,
			includeTermVectors: false,
			docValues:          true,
		},
		{
			options:            Index | Store | SearchTermPositions | Sortable,
			isIndexed:          true,
			isStored:           true,
			includeTermVectors: true,
			docValues:          true,
		},
		{
			options:            Index | Store | SearchTermPositions | Aggregatable,
			isIndexed:          true,
			isStored:           true,
			includeTermVectors: true,
			docValues:          true,
		},
		{
			options:            Index | Store | SearchTermPositions | Sortable | Aggregatable,
			isIndexed:          true,
			isStored:           true,
			includeTermVectors: true,
			docValues:          true,
		},
		{
			options:            Index | Store | HighlightMatches | Sortable,
			isIndexed:          true,
			isStored:           true,
			includeTermVectors: true,
			docValues:          true,
		},
		{
			options:            Index | Store | HighlightMatches | Aggregatable,
			isIndexed:          true,
			isStored:           true,
			includeTermVectors: true,
			docValues:          true,
		},
		{
			options:            Index | Store | HighlightMatches | Sortable | Aggregatable,
			isIndexed:          true,
			isStored:           true,
			includeTermVectors: true,
			docValues:          true,
		},
		{
			options:            Index | Store | SearchTermPositions | HighlightMatches | Sortable,
			isIndexed:          true,
			isStored:           true,
			includeTermVectors: true,
			docValues:          true,
		},
		{
			options:            Index | Store | SearchTermPositions | HighlightMatches | Aggregatable,
			isIndexed:          true,
			isStored:           true,
			includeTermVectors: true,
			docValues:          true,
		},
		{
			options:            Index | Store | SearchTermPositions | HighlightMatches | Sortable | Aggregatable,
			isIndexed:          true,
			isStored:           true,
			includeTermVectors: true,
			docValues:          true,
		},
	}

	for _, test := range tests {
		actuallyIndexed := test.options.Index()
		if actuallyIndexed != test.isIndexed {
			t.Errorf("expected indexed to be %v, got %v for %d", test.isIndexed, actuallyIndexed, test.options)
		}
		actuallyStored := test.options.Store()
		if actuallyStored != test.isStored {
			t.Errorf("expected stored to be %v, got %v for %d", test.isStored, actuallyStored, test.options)
		}
		actuallyIncludeTermVectors := test.options.IncludeLocations()
		if actuallyIncludeTermVectors != test.includeTermVectors {
			t.Errorf("expected includeTermVectors to be %v, got %v for %d", test.includeTermVectors, actuallyIncludeTermVectors, test.options)
		}
		actuallyDocValues := test.options.IndexDocValues()
		if actuallyDocValues != test.docValues {
			t.Errorf("expected docValue to be %v, got %v for %d", test.docValues, actuallyDocValues, test.options)
		}
	}
}

func TestNumericField(t *testing.T) {
	nf := NewNumericField("age", 3.4)
	_ = nf.Analyze(0)
	numTokens := nf.AnalyzedLength()
	// one exact prefix-coded token plus the original value "3.4"
	if numTokens != 2 {
		t.Errorf("expected 2 tokens, got %d ", numTokens)
	}
	tokenFreqs := nf.AnalyzedTokenFrequencies()
	if len(tokenFreqs) != 2 {
		t.Errorf("expected 2 token freqs, got %d", len(tokenFreqs))
	}
	if _, ok := tokenFreqs["3.4"]; !ok {
		t.Errorf("expected original value token \"3.4\" to be indexed")
	}
}

func TestGeoPointField(t *testing.T) {
	gf := NewGeoPointField("loc", 0.0015, 0.0015)
	_ = gf.Analyze(0)
	numTokens := gf.analyzedLength
	if numTokens != 8 {
		t.Errorf("expected 8 tokens, got %d", numTokens)
	}
	tokenFreqs := gf.AnalyzedTokenFrequencies()
	if len(tokenFreqs) != 8 {
		t.Errorf("expected 8 token freqs, got %d", len(tokenFreqs))
	}
}

// geo points are morton hashes, their decimal form is meaningless as a term
func TestDateTimeAndGeoFieldsOmitOriginalValueToken(t *testing.T) {
	df := NewDateTimeField("when", time.Unix(0, 1234567890))
	_ = df.Analyze(0)
	// one exact prefix-coded term; date ranges are served from the column
	if got := df.AnalyzedLength(); got != 1 {
		t.Errorf("expected 1 datetime token, got %d", got)
	}

	gf := NewGeoPointField("loc", 0.0015, 0.0015)
	_ = gf.Analyze(0)
	for term := range gf.AnalyzedTokenFrequencies() {
		if _, err := strconv.ParseFloat(term, 64); err == nil {
			t.Errorf("unexpected decimal token %q in geo field", term)
		}
	}
}

// The decimal text term is on by default; WithoutDecimalTerm drops only it.
func TestNumericFieldWithoutDecimalTerm(t *testing.T) {
	on := NewNumericField("n", 3.25)
	off := NewNumericField("n", 3.25).WithoutDecimalTerm()
	on.Analyze(0)
	off.Analyze(0)
	if _, ok := on.AnalyzedTokenFrequencies()["3.25"]; !ok {
		t.Fatal("default numeric field lacks the decimal term")
	}
	if _, ok := off.AnalyzedTokenFrequencies()["3.25"]; ok {
		t.Fatal("WithoutDecimalTerm still emits the decimal term")
	}
	if got, want := off.AnalyzedLength(), on.AnalyzedLength()-1; got != want {
		t.Fatalf("tokens %d, want %d", got, want)
	}
	if v, ok := off.NumericValue(); !ok || v != numeric.Float64ToInt64(3.25) {
		t.Fatalf("numeric value %d %v", v, ok)
	}
	if tf := NewTextField("t", "x"); tf.WithoutDecimalTerm().analyzer != tf.analyzer {
		t.Fatal("text field analyzer changed")
	}
}

func TestAddShiftTokensMatchesPrefixCoding(t *testing.T) {
	for _, v := range []int64{0, -1, 42, math.MinInt64, math.MaxInt64} {
		for _, shiftBy := range []uint{4, 8, 16, 63} {
			tokens := addShiftTokens(nil, v, shiftBy, analysis.Numeric)
			i := 0
			for shift := shiftBy; shift < 64; shift += shiftBy {
				want, err := numeric.NewPrefixCodedInt64(v, shift)
				if err != nil {
					t.Fatal(err)
				}
				tok := tokens[i]
				if !bytes.Equal(tok.Term, want) || tok.End != len(want) || tok.Start != 0 ||
					tok.PositionIncr != 0 || tok.Type != analysis.Numeric {
					t.Fatalf("v=%d shiftBy=%d token %d = %+v, want term %v", v, shiftBy, i, *tok, want)
				}
				i++
			}
			if len(tokens) != i {
				t.Fatalf("v=%d shiftBy=%d: %d tokens, want %d", v, shiftBy, len(tokens), i)
			}
			// terms must not share bytes
			tokens[0].Term[len(tokens[0].Term)-1] ^= 0xff
			if i > 1 && !bytes.Equal(tokens[1].Term, mustPrefixCoded(numeric.NewPrefixCodedInt64(v, 2*shiftBy))) {
				t.Fatalf("v=%d shiftBy=%d: terms overlap", v, shiftBy)
			}
		}
	}
}

func mustPrefixCoded(p numeric.PrefixCoded, err error) numeric.PrefixCoded {
	if err != nil {
		panic(err)
	}
	return p
}

// The numeric analyzer emits the exact value, the decimal original and any
// shift tokens from one token block and one stream allocation.
func TestNumericAnalyzerTokens(t *testing.T) {
	input := []byte(mustPrefixCoded(numeric.NewPrefixCodedInt64(numeric.Float64ToInt64(3.25), 0)))
	for _, shiftBy := range []uint{0, 16} {
		a := &numericAnalyzer{tokenType: analysis.Numeric, shiftBy: shiftBy, emitOriginal: true}
		tokens := a.Analyze(input)
		want := 2
		if shiftBy > 0 {
			want += int(63 / shiftBy)
		}
		if len(tokens) != want {
			t.Fatalf("shiftBy=%d: %d tokens, want %d", shiftBy, len(tokens), want)
		}
		if tok := tokens[0]; !bytes.Equal(tok.Term, input) || tok.End != len(input) ||
			tok.PositionIncr != 1 || tok.Type != analysis.Numeric {
			t.Fatalf("value token %+v", *tok)
		}
		if tok := tokens[1]; string(tok.Term) != "3.25" || tok.End != 4 ||
			tok.PositionIncr != 0 || tok.Type != analysis.AlphaNumeric {
			t.Fatalf("original token %+v", *tok)
		}
		if shiftBy > 0 && !bytes.Equal(tokens[2].Term,
			mustPrefixCoded(numeric.NewPrefixCodedInt64(numeric.Float64ToInt64(3.25), shiftBy))) {
			t.Fatalf("shift token %+v", *tokens[2])
		}
		if allocs := testing.AllocsPerRun(100, func() { a.Analyze(input) }); shiftBy == 0 && allocs > 3 {
			t.Fatalf("numeric analysis took %v allocations, want at most 3", allocs)
		}
	}

	kf := NewKeywordField("cat", "group7")
	tokens := kf.baseAnalayze(analysis.AlphaNumeric)
	if len(tokens) != 1 || string(tokens[0].Term) != "group7" || tokens[0].End != 6 || tokens[0].PositionIncr != 1 {
		t.Fatalf("keyword tokens %+v", tokens)
	}
	if allocs := testing.AllocsPerRun(100, func() { kf.baseAnalayze(analysis.AlphaNumeric) }); allocs > 1 {
		t.Fatalf("keyword analysis took %v allocations, want 1", allocs)
	}
}

// Text fields analyzed with pooled token storage keep their own terms and
// locations when later fields reuse that storage.
func TestTextFieldPooledAnalysis(t *testing.T) {
	a := NewTextField("a", "Alpha beta alpha").SearchTermPositions().HighlightMatches()
	b := NewTextField("b", "gamma delta epsilon zeta eta theta").SearchTermPositions()
	a.Analyze(0)
	b.Analyze(0)
	tfs := a.AnalyzedTokenFrequencies()
	if len(tfs) != 2 || a.AnalyzedLength() != 3 {
		t.Fatalf("terms %v length %d", tfs, a.AnalyzedLength())
	}
	alpha := tfs["alpha"]
	if alpha == nil || alpha.Frequency() != 2 || len(alpha.Locations) != 2 {
		t.Fatalf("alpha %+v", alpha)
	}
	if l := alpha.Locations[1]; l.Pos() != 3 || l.Start() != 11 || l.End() != 16 {
		t.Fatalf("second alpha location %+v", l)
	}
	if beta := tfs["beta"]; beta == nil || beta.Locations[0].Start() != 6 {
		t.Fatalf("beta %+v", beta)
	}
}

// The numeric fast path gives the same terms, length and position as
// running the analyzer and counting its token stream.
func TestNumericAnalyzeFastPath(t *testing.T) {
	fields := map[string]*TermField{
		"default":        NewNumericField("n", 3.4),
		"no decimal":     NewNumericField("n", -7).WithoutDecimalTerm(),
		"positions":      NewNumericField("n", 12).SearchTermPositions(),
		"integral value": NewNumericField("n", 1e6),
	}
	for name, f := range fields {
		t.Run(name, func(t *testing.T) {
			tokens := f.analyzer.Analyze(f.value)
			want, wantPos := analysis.TokenFrequencySlice(tokens, f.IncludeLocations(), 0)
			gotPos := f.Analyze(0)
			if gotPos != wantPos || f.Length() != len(tokens) || len(f.analyzedTerms) != len(want) {
				t.Fatalf("pos %d/%d length %d/%d terms %d/%d", gotPos, wantPos, f.Length(), len(tokens), len(f.analyzedTerms), len(want))
			}
			for i := range want {
				g, w := &f.analyzedTerms[i], &want[i]
				if !bytes.Equal(g.Term(), w.Term()) || g.Frequency() != w.Frequency() || len(g.Locations) != len(w.Locations) {
					t.Fatalf("term %d: got %q x%d want %q x%d", i, g.Term(), g.Frequency(), w.Term(), w.Frequency())
				}
			}
		})
	}
}

// Re-analyzing or releasing a text field recycles its term storage, and the
// recycled storage gives the same terms as fresh analysis.
func TestTextFieldReleaseRecycles(t *testing.T) {
	text := "the quick brown fox jumps over the lazy dog the end"
	f := NewTextField("body", text).SearchTermPositions()
	ref := NewTextField("body", text).SearchTermPositions()
	ref.Analyze(0)
	want := dumpTerms(ref)
	for range 3 {
		f.Analyze(0)
		if got := dumpTerms(f); got != want {
			t.Fatalf("got %s\nwant %s", got, want)
		}
		f.ReleaseAnalysis()
		if f.analyzedTerms != nil || f.freqBlock != nil {
			t.Fatal("release left analysis behind")
		}
	}
}

func dumpTerms(f *TermField) string {
	var b bytes.Buffer
	for i := range f.analyzedTerms {
		tf := &f.analyzedTerms[i]
		b.WriteString(string(tf.Term()) + ":" + strconv.Itoa(tf.Frequency()))
		for _, l := range tf.Locations {
			b.WriteString("," + strconv.Itoa(l.Pos()) + "/" + strconv.Itoa(l.Start()))
		}
		b.WriteString(" ")
	}
	return b.String()
}

func TestFieldOffsetsOnlyWhenHighlighting(t *testing.T) {
	analyzers := map[string]Analyzer{"standard": nil, "custom": whitespaceAnalyzer{}}
	tests := []struct {
		name       string
		highlight  bool
		wantOffset bool
	}{
		{name: "positions", wantOffset: false},
		{name: "highlight", highlight: true, wantOffset: true},
	}
	for an, analyzer := range analyzers {
		for _, tt := range tests {
			t.Run(an+"/"+tt.name, func(t *testing.T) {
				f := NewTextField("body", "quick brown quick").SearchTermPositions()
				if analyzer != nil {
					f.WithAnalyzer(analyzer)
				}
				if tt.highlight {
					f.HighlightMatches()
				}
				f.Analyze(0)
				defer f.ReleaseAnalysis()
				var positions, offsets int
				f.EachTerm(func(term segment.FieldTerm) {
					term.EachLocation(func(l segment.Location) {
						positions += l.Pos()
						offsets += l.Start() + l.End()
					})
				})
				if positions != 1+2+3 {
					t.Fatalf("positions sum %d, want 6", positions)
				}
				if got := offsets > 0; got != tt.wantOffset {
					t.Fatalf("offsets sum %d, want offsets %v", offsets, tt.wantOffset)
				}
			})
		}
	}
	kw := NewKeywordField("tag", "a b").SearchTermPositions()
	kw.Analyze(0)
	kw.EachTerm(func(term segment.FieldTerm) {
		term.EachLocation(func(l segment.Location) {
			if l.Start() != 0 || l.End() != 0 || l.Pos() != 1 {
				t.Fatalf("keyword location %d %d-%d, want pos 1 without offsets", l.Pos(), l.Start(), l.End())
			}
		})
	})
}

type whitespaceAnalyzer struct{}

func (whitespaceAnalyzer) Analyze(input []byte) analysis.TokenStream {
	var tokens analysis.TokenStream
	for i, start := 0, 0; i <= len(input); i++ {
		if i < len(input) && input[i] != ' ' {
			continue
		}
		if i > start {
			tokens = append(tokens, &analysis.Token{Term: input[start:i], Start: start, End: i, PositionIncr: 1, Type: analysis.AlphaNumeric})
		}
		start = i + 1
	}
	return tokens
}
