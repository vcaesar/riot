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

package searcher

import (
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/similarity"
)

// sloppyDocs has "x y" at positions 1 and 2 of documents 1, 3, 5 and 7.
func sloppyDocs() (x, y []covDoc) {
	for _, d := range []uint64{1, 3, 5, 7} {
		x, y = append(x, pd(d, pl(1))), append(y, pd(d, pl(2)))
	}
	return x, y
}

func newSloppy(t *testing.T, x, y *covPostings) *PhraseSearcher {
	t.Helper()
	r := newCovReader()
	r.its["x"], r.its["y"] = x, y
	s, err := NewSloppyMultiPhraseSearcher(r, [][]string{{"x"}, {"y"}}, "body", 1, similarity.ConstantScorer(1), noExplain())
	if err != nil {
		t.Fatal(err)
	}
	if s.raw != nil {
		t.Fatal("a sloppy phrase is not raw")
	}
	return s
}

func TestPhraseSearcherCovBuildErrors(t *testing.T) {
	tests := []struct {
		name     string
		terms    [][]string
		maxCount int
		setup    func(r *covReader)
	}{
		{name: "term", terms: [][]string{{"x"}, {"boom"}}},
		{name: "position term", terms: [][]string{{"x"}, {"y", "", "boom"}}},
		{name: "position", terms: [][]string{{"x"}, {"y", "z"}}, maxCount: 1},
		{name: "conjunction", terms: [][]string{{"x"}, {"y"}}, setup: func(r *covReader) {
			b := newCovBatch(nil, 0)
			b.optErr = errCov
			r.its["x"] = b
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setInt(t, &DisjunctionMaxClauseCount, tc.maxCount)
			r := newCovReader()
			x, y := sloppyDocs()
			r.its["x"], r.its["y"], r.its["z"] = &covPostings{docs: x}, &covPostings{docs: y}, &covPostings{docs: y}
			r.postErr["boom"] = errCov
			if tc.setup != nil {
				tc.setup(r)
			}
			if _, err := NewSloppyMultiPhraseSearcher(r, tc.terms, "body", 0, nil, noExplain()); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestPhraseSearcherCovSloppy(t *testing.T) {
	ctx := covCtx()
	x, y := sloppyDocs()
	s := newSloppy(t, &covPostings{docs: x}, &covPostings{docs: y})
	if s.Min() != 0 || s.Count() != 8 { // the conjunction counts the sum of its terms
		t.Fatalf("min %d count %d", s.Min(), s.Count())
	}
	hits, skipped, err := nextAboveAll(t, s, ctx, 100, always)
	if err != nil || !slices.Equal(hits, docs(1, 3, 5, 7)) || skipped != 0 {
		t.Fatalf("hits %v skipped %d err %v", hits, skipped, err)
	}

	s = newSloppy(t, &covPostings{docs: x}, &covPostings{docs: y})
	if dm, err := s.Advance(ctx, 2); err != nil || dm.Number != 3 {
		t.Fatalf("first advance: %v %v", dm, err)
	}
	if s.Size() <= reflectStaticSizePhraseSearcher {
		t.Fatalf("size %d", s.Size())
	}
	for _, tc := range []struct{ target, want uint64 }{{6, 7}} {
		if dm, err := s.Advance(ctx, tc.target); err != nil || dm == nil || dm.Number != tc.want {
			t.Fatalf("advance %d: %v %v", tc.target, dm, err)
		}
	}
	if dm, err := s.Advance(ctx, 9); err != nil || dm != nil {
		t.Fatalf("past end: %v %v", dm, err)
	}

	// an Advance to a document at or before the next candidate reads on
	s = newSloppy(t, &covPostings{docs: x}, &covPostings{docs: y})
	if dm, err := s.Next(ctx); err != nil || dm.Number != 1 {
		t.Fatalf("next: %v %v", dm, err)
	}
	if dm, err := s.Advance(ctx, 2); err != nil || dm.Number != 3 {
		t.Fatalf("advance to the candidate: %v %v", dm, err)
	}
}

func TestPhraseSearcherCovErrors(t *testing.T) {
	x, y := sloppyDocs()
	tests := []struct {
		name    string
		x, y    *covPostings
		advance bool
		first   bool // read a hit before the failing call
	}{
		{name: "init", x: &covPostings{docs: x, nextErr: errCov}, y: &covPostings{docs: y}},
		{name: "init on advance", x: &covPostings{docs: x, nextErr: errCov}, y: &covPostings{docs: y}, advance: true},
		{name: "next", x: &covPostings{docs: x, nextErr: errCov, errAt: 2}, y: &covPostings{docs: y}},
		{name: "advance", x: &covPostings{docs: x}, y: &covPostings{docs: y, advErr: errCov}, advance: true, first: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := covCtx()
			s := newSloppy(t, tc.x, tc.y)
			if tc.first {
				if _, err := s.Next(ctx); err != nil {
					t.Fatal(err)
				}
			}
			var err error
			if tc.advance {
				_, err = s.Advance(ctx, 6)
			} else {
				_, err = s.Next(ctx)
			}
			if !errors.Is(err, errCov) {
				t.Fatalf("got %v", err)
			}
		})
	}
	s := newSloppy(t, &covPostings{docs: x, closeErr: errCov}, &covPostings{docs: y})
	if err := s.Close(); !errors.Is(err, errCov) {
		t.Fatalf("close: %v", err)
	}
}

func TestPhrasePathCovStrings(t *testing.T) {
	a, b := &search.Location{Pos: 1}, &search.Location{Pos: 2}
	p := phrasePath{{term: "x", loc: a}, {term: "y", loc: b}}
	if got := p.String(); got != "[[x &{1 0 0}], [y &{2 0 0}]]" {
		t.Fatalf("got %s", got)
	}
	tlm := search.TermLocationMap{}
	p.MergeInto(tlm)
	if len(tlm["x"]) != 1 || tlm["y"][0] != b {
		t.Fatalf("got %v", tlm)
	}
	// locations of terms outside the phrase are ignored
	s := &PhraseSearcher{terms: [][]string{{"x"}, {"y"}}, exactLocs: make([][]int, 2), exactPtr: make([]int, 2)}
	in := []search.FieldTermLocation{{Field: "body", Term: "z"}, {Field: "body", Term: "x", Location: search.Location{Pos: 1}}}
	if !s.exactFieldLocations(in, "body") || !slices.Equal(s.exactLocs[0], []int{1}) || len(s.exactLocs[1]) != 0 {
		t.Fatalf("got %v", s.exactLocs)
	}
}

// skipBelowFloor leaves a mix of encoded and decoded batches to next,
// which decodes them.
func TestRawPhraseCovSkipMixed(t *testing.T) {
	x, y := phraseMix()
	s := covPhrase(t, phraseIt(kindOwn, x), phraseIt(kindPos, y))
	r := s.raw
	for i := range r.cursors {
		if err := r.cursors[i].fill(); err != nil {
			t.Fatal(err)
		}
		r.cursors[i].scored = true
	}
	if own, mixed := r.ownBatches(); own || !mixed {
		t.Fatalf("own %v mixed %v", own, mixed)
	}
	doc, run, _, stop := r.skipBelowFloor(1, math.MaxInt64, 0, 0, func(float64, int) bool { return true })
	if doc != 1 || run != 0 || stop {
		t.Fatalf("doc %d run %d stop %v", doc, run, stop)
	}
}
