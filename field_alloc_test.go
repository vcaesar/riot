//go:build !race

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
	"testing"
	"unsafe"
)

// Race instrumentation deliberately drops sync.Pool entries, so allocation
// budgets for pooled storage are checked only without -race.
func TestTextFieldReleaseAllocations(t *testing.T) {
	f := NewTextField("body", "the quick brown fox jumps over the lazy dog the end").SearchTermPositions()
	if allocs := testing.AllocsPerRun(50, func() { f.Analyze(0); f.ReleaseAnalysis() }); allocs > 2 {
		t.Fatalf("pooled analyze+release allocates %v per run", allocs)
	}
}

// A document of three short fields is built in a few allocations: the
// document with room for four fields, each field with its short value, and
// the long text's copy.
func TestDocumentBuildAllocations(t *testing.T) {
	body := "common search engine zig document term1 term2 term3 category1 rank1"
	if n := testing.AllocsPerRun(100, func() {
		NewDocument("12345").
			AddField(NewTextField("body", body).StoreValue().SearchTermPositions()).
			AddField(NewNumericField("id", 12345)).
			AddField(NewKeywordField("category", "/catalog/group7").Aggregatable())
	}); n > 6 {
		t.Fatalf("building the document allocates %v, want <= 6", n)
	}
}

// A short field's block holds only the field and its value: a Batch
// retains every document's fields, so analysis room inline in the block
// (a keyword's TokenFreq) cost about 13 MiB per 100K documents.
func TestTermFieldBlockHoldsNoAnalysis(t *testing.T) {
	if got, want := unsafe.Sizeof(termFieldBlock{}), unsafe.Sizeof(TermField{})+termFieldInline; got != want {
		t.Fatalf("termFieldBlock is %d bytes, want %d", got, want)
	}
}

// Keyword analysis makes one term allocation, numeric analysis keeps its
// terms in one block, and a stored text field's copy for analysis is pooled
// with its terms.
func TestFieldAnalyzeAllocations(t *testing.T) {
	kw := NewKeywordField("category", "/catalog/group7")
	if n := testing.AllocsPerRun(100, func() { kw.Analyze(0) }); n > 1 {
		t.Fatalf("keyword Analyze allocates %v, want <= 1", n)
	}
	num := NewNumericField("id", 3.5)
	if n := testing.AllocsPerRun(100, func() { num.Analyze(0) }); n > 1 {
		t.Fatalf("numeric Analyze allocates %v, want <= 1", n)
	}
	text := NewTextField("body", "The Quick brown fox jumps over the lazy dog").StoreValue()
	if n := testing.AllocsPerRun(100, func() { text.Analyze(0); text.ReleaseAnalysis() }); n > 1 {
		t.Fatalf("stored text Analyze allocates %v, want <= 1", n)
	}
}
