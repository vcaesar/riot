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

package riot

import (
	"bytes"
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"

	"github.com/vcaesar/riot/numeric"
)

func fieldTerms(f Field) map[string]int {
	terms := map[string]int{}
	f.EachTerm(func(ft segment.FieldTerm) { terms[string(ft.Term())] = ft.Frequency() })
	return terms
}

// Short and long values are copied the same way, and appending to one
// field's value never reaches another's.
func TestOwnedFieldValues(t *testing.T) {
	for _, v := range []string{"", "a", "/catalog/group7", "exactly-16-bytes", "seventeen-bytes-x", "a much longer keyword value"} {
		for _, f := range []*TermField{NewKeywordField("k", v), NewTextField("t", v)} {
			if string(f.Value()) != v || f.NumPlainTextBytes() != len(v) {
				t.Fatalf("%s value %q, want %q", f.Name(), f.Value(), v)
			}
			if cap(f.Value()) != len(v) && len(v) <= termFieldInline {
				t.Fatalf("%q: inline value cap %d exposes the block", v, cap(f.Value()))
			}
		}
	}
	a, b := NewKeywordField("k", "aa"), NewKeywordField("k", "bb")
	_ = append(a.Value(), 'x')
	if string(b.Value()) != "bb" || string(a.Value()) != "aa" {
		t.Fatalf("values %q %q", a.Value(), b.Value())
	}
}

// Keyword terms match SingleTokenFrequency's, with and without term
// vectors, and survive a release and re-analysis.
func TestKeywordInlineTerm(t *testing.T) {
	kw := NewKeywordField("category", "/catalog/group7")
	if got := kw.Analyze(5); got != 0 || kw.Length() != 1 {
		t.Fatalf("Analyze = %d, length %d", got, kw.Length())
	}
	if terms := fieldTerms(kw); len(terms) != 1 || terms["/catalog/group7"] != 1 {
		t.Fatalf("terms %v", terms)
	}
	kw.ReleaseAnalysis()
	if len(fieldTerms(kw)) != 0 {
		t.Fatal("terms kept after release")
	}
	kw.Analyze(0)
	if terms := fieldTerms(kw); terms["/catalog/group7"] != 1 {
		t.Fatalf("re-analyzed terms %v", terms)
	}
	vec := NewKeywordField("category", "x").SearchTermPositions()
	if got := vec.Analyze(5); got != 6 {
		t.Fatalf("keyword with positions ends at %d, want 6", got)
	}
	var locs int
	vec.EachTerm(func(ft segment.FieldTerm) {
		ft.EachLocation(func(segment.Location) { locs++ })
	})
	if locs != 1 {
		t.Fatalf("%d locations, want 1", locs)
	}
}

// A numeric field's value and terms are those of the prefix coding and
// the decimal text.
func TestNumericFieldInline(t *testing.T) {
	for _, v := range []float64{0, -1, 3.5, 12345, 1e300} {
		f := NewNumericField("id", v)
		want := numeric.MustNewPrefixCodedInt64(numeric.Float64ToInt64(v), 0)
		if !bytes.Equal(f.Value(), want) || cap(f.Value()) != len(want) {
			t.Fatalf("%v: value %x cap %d, want %x", v, f.Value(), cap(f.Value()), want)
		}
		f.Analyze(0)
		terms := fieldTerms(f)
		dec := numberAnalyzer.decimal(v)
		if len(terms) != 2 || terms[string(want)] != 1 || terms[string(dec)] != 1 {
			t.Fatalf("%v: terms %v", v, terms)
		}
	}
	plain := NewNumericField("id", 7).WithoutDecimalTerm()
	plain.Analyze(0)
	if terms := fieldTerms(plain); len(terms) != 1 {
		t.Fatalf("without decimal: %d terms", len(terms))
	}
}

// A stored text field analyzes a pooled copy: the stored value keeps its
// case, and recycling the copy for another field leaves both intact.
func TestStoredTextPooledCopy(t *testing.T) {
	a := NewTextField("body", "Alpha BRAVO Charlie").StoreValue()
	a.Analyze(0)
	if string(a.Value()) != "Alpha BRAVO Charlie" {
		t.Fatalf("stored value changed to %q", a.Value())
	}
	if terms := fieldTerms(a); terms["alpha"] != 1 || terms["bravo"] != 1 || terms["charlie"] != 1 {
		t.Fatalf("terms %v", terms)
	}
	a.ReleaseAnalysis()
	b := NewTextField("body", "Delta ECHO").StoreValue()
	b.Analyze(0)
	if terms := fieldTerms(b); terms["delta"] != 1 || terms["echo"] != 1 || len(terms) != 2 {
		t.Fatalf("terms %v", terms)
	}
	if string(a.Value()) != "Alpha BRAVO Charlie" || string(b.Value()) != "Delta ECHO" {
		t.Fatalf("stored values %q %q", a.Value(), b.Value())
	}
	a.Analyze(0)
	if terms := fieldTerms(a); terms["alpha"] != 1 || len(terms) != 3 {
		t.Fatalf("re-analyzed terms %v", terms)
	}
	if terms := fieldTerms(b); terms["delta"] != 1 || len(terms) != 2 {
		t.Fatalf("b terms after a re-analyzed: %v", terms)
	}
}

// NewDocument's inline field room grows like any slice.
func TestDocumentBeyondInlineFields(t *testing.T) {
	d := NewDocument("1")
	for i := range 2 * documentInline {
		d.AddField(NewKeywordField("f", string(rune('a'+i))))
	}
	if len(*d) != 2*documentInline+1 || string((*d)[0].Value()) != "1" {
		t.Fatalf("len %d id %q", len(*d), (*d)[0].Value())
	}
	for i, f := range (*d)[1:] {
		if string(f.Value()) != string(rune('a'+i)) {
			t.Fatalf("field %d = %q", i, f.Value())
		}
	}
}
