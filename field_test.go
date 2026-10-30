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
	// 16 prefix-coded tokens plus the original value "3.4"
	if numTokens != 17 {
		t.Errorf("expected 17 tokens, got %d ", numTokens)
	}
	tokenFreqs := nf.AnalyzedTokenFrequencies()
	if len(tokenFreqs) != 17 {
		t.Errorf("expected 17 token freqs, got %d", len(tokenFreqs))
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
	if got := df.AnalyzedLength(); got != 16 {
		t.Errorf("expected 16 datetime tokens, got %d", got)
	}

	gf := NewGeoPointField("loc", 0.0015, 0.0015)
	_ = gf.Analyze(0)
	for term := range gf.AnalyzedTokenFrequencies() {
		if _, err := strconv.ParseFloat(term, 64); err == nil {
			t.Errorf("unexpected decimal token %q in geo field", term)
		}
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
