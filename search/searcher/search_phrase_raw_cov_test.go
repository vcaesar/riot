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
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/similarity"
)

// covLocPostings reads postings with their locations in batches.
type covLocPostings struct {
	*covPostings
	batchErr error
}

func (p *covLocPostings) read(docs []uint64, freqs []int, norms []float64, ends []int, offs []uint64,
	fields []string, locs []int, offsets bool) (n int, _ []string, _ []int) {
	for n < len(docs) && p.i < len(p.docs) {
		d := p.docs[p.i]
		docs[n], freqs[n], norms[n] = d.num, d.freq, d.norm
		for _, l := range d.locs {
			fields = append(fields, l.Field())
			if offsets {
				locs = append(locs, l.Pos(), l.Start(), l.End())
			} else {
				locs = append(locs, l.Pos(), 0, 0)
			}
		}
		ends[n] = len(fields)
		if offs != nil {
			offs[n] = uint64(p.i) //nolint:gosec // an index
		}
		n++
		p.i++
	}
	return n, fields, locs
}

func (p *covLocPostings) NextLocBatch(docs []uint64, freqs []int, norms []float64, ends []int,
	fields []string, locs []int) (n int, _ []string, _ []int, err error) {
	if p.batchErr != nil {
		return 0, fields, locs, p.batchErr
	}
	n, fields, locs = p.read(docs, freqs, norms, ends, nil, fields, locs, true)
	return n, fields, locs, nil
}

// covPosPostings reads locations without their offsets, which LoadOffsets
// loads by the posting's index.
type covPosPostings struct {
	*covLocPostings
	loadErr error
}

func (p *covPosPostings) NextPosBatch(docs []uint64, freqs []int, norms []float64, ends []int, offs []uint64,
	fields []string, locs []int) (n int, _ []string, _ []int, err error) {
	if p.batchErr != nil {
		return 0, fields, locs, p.batchErr
	}
	n, fields, locs = p.read(docs, freqs, norms, ends, offs, fields, locs, false)
	return n, fields, locs, nil
}

func (p *covPosPostings) LoadOffsets(h uint64, locs []int) error {
	if p.loadErr != nil {
		return p.loadErr
	}
	d := p.docs[h]
	if len(locs) != 3*len(d.locs) {
		return fmt.Errorf("posting %d: %d locations, want %d", h, len(locs)/3, len(d.locs))
	}
	for j, l := range d.locs {
		locs[3*j+1], locs[3*j+2] = l.Start(), l.End()
	}
	return nil
}

// covOwnPostings reads positions as uvarint deltas; bad replaces the
// record of a document.
type covOwnPostings struct {
	*covPosPostings
	ownErr error
	notOwn bool
	bad    map[uint64][]byte
}

func (p *covOwnPostings) NextOwnPosBatch(docs []uint64, freqs []int, norms []float64, starts, ends []int,
	offs []uint64) (n int, recs []byte, own bool, err error) {
	if p.ownErr != nil {
		return 0, nil, false, p.ownErr
	}
	if p.notOwn {
		return 0, nil, false, nil
	}
	for n < len(docs) && p.i < len(p.docs) {
		d := p.docs[p.i]
		docs[n], freqs[n], norms[n], starts[n] = d.num, d.freq, d.norm, len(recs)
		if rec, ok := p.bad[d.num]; ok {
			recs = append(recs, rec...)
		} else {
			prev := 0
			for _, l := range d.locs {
				recs = binary.AppendUvarint(recs, uint64(l.Pos()-prev)) //nolint:gosec // positions ascend
				prev = l.Pos()
			}
		}
		ends[n], offs[n] = len(recs), uint64(p.i) //nolint:gosec // an index
		n++
		p.i++
	}
	return n, recs, true, nil
}

// pl returns locations in field body at pos, each spanning [10*pos, 10*pos+3).
func pl(pos ...int) []segment.Location {
	rv := make([]segment.Location, len(pos))
	for i, p := range pos {
		rv[i] = &stubTermFieldVector{field: "body", pos: p, start: 10 * p, end: 10*p + 3}
	}
	return rv
}

// pd is a posting of a phrase term whose freq is its number of locations.
func pd(num uint64, locs []segment.Location) covDoc {
	return covDoc{num: num, freq: len(locs), locs: locs}
}

type phraseKind string

const (
	kindPlain phraseKind = "plain"
	kindLoc   phraseKind = "loc"
	kindPos   phraseKind = "pos"
	kindOwn   phraseKind = "own"
)

func phraseIt(kind phraseKind, docs []covDoc) segment.PostingsIterator {
	p := &covPostings{docs: docs}
	switch kind {
	case kindLoc:
		return &covLocPostings{covPostings: p}
	case kindPos:
		return &covPosPostings{covLocPostings: &covLocPostings{covPostings: p}}
	case kindOwn:
		return &covOwnPostings{covPosPostings: &covPosPostings{covLocPostings: &covLocPostings{covPostings: p}}}
	}
	return p
}

// covPhrase is the exact phrase "x y" over field body, whose terms read its.
// Each term scores its freq.
func covPhrase(t *testing.T, x, y segment.PostingsIterator) *PhraseSearcher {
	t.Helper()
	r := newCovReader()
	r.its["x"], r.its["y"] = x, y
	s, err := NewMultiPhraseSearcher(r, [][]string{{"x"}, {"y"}}, "body", freqScorer{}, noExplain())
	if err != nil {
		t.Fatal(err)
	}
	if s.raw == nil {
		t.Fatal("expected a raw phrase")
	}
	return s
}

// expectPhrase finds, by brute force, the documents where y follows x in
// body, split by whether freq x + freq y beats floor.
func expectPhrase(x, y []covDoc, floor float64) (hits []uint64, skipped int) {
	for _, dx := range x {
		i := slices.IndexFunc(y, func(d covDoc) bool { return d.num == dx.num })
		if i < 0 {
			continue
		}
		found := false
		for _, lx := range dx.locs {
			for _, ly := range y[i].locs {
				found = found || (lx.Field() == "body" && ly.Field() == "body" && ly.Pos() == lx.Pos()+1)
			}
		}
		if !found {
			continue
		}
		if float64(dx.freq+y[i].freq) > floor {
			hits = append(hits, dx.num)
		} else {
			skipped++
		}
	}
	return hits, skipped
}

// phraseMix has single and repeated occurrences, misses, a document only
// one term has, and freqs that change the score.
func phraseMix() (x, y []covDoc) {
	x = []covDoc{pd(1, pl(1)), pd(2, pl(1)), pd(3, pl(1, 5)), pd(4, pl(2)),
		pd(5, pl(1)), pd(6, pl(1)), pd(7, pl(3, 7)), pd(8, pl(1)),
		pd(10, pl(4)), pd(11, pl(4)), pd(12, pl(1))}
	y = []covDoc{pd(1, pl(2)), pd(2, pl(3)), pd(3, pl(6)), pd(5, pl(2)),
		pd(6, pl(2)), pd(7, pl(4, 8)), pd(8, pl(2)), pd(9, pl(1)),
		pd(10, pl(5)), pd(11, pl(5)), pd(12, pl(2, 9))}
	return x, y
}

func TestRawPhraseCovKinds(t *testing.T) {
	x, y := phraseMix()
	kinds := [][2]phraseKind{{kindPlain, kindPlain}, {kindLoc, kindLoc}, {kindPos, kindPos}, {kindOwn, kindOwn},
		{kindOwn, kindPos}, {kindLoc, kindOwn}}
	for _, k := range kinds {
		for _, floor := range []float64{math.Inf(-1), 2, 3, 100} {
			t.Run(fmt.Sprintf("%s-%s/%v", k[0], k[1], floor), func(t *testing.T) {
				s := covPhrase(t, phraseIt(k[0], x), phraseIt(k[1], y))
				wantHits, wantSkipped := expectPhrase(x, y, floor)
				ctx := covCtx()
				ctx.SkipBlocks = true
				hits, skipped, err := nextAboveAll(t, s, ctx, floor, always)
				if err != nil || !slices.Equal(hits, wantHits) || skipped != wantSkipped {
					t.Fatalf("hits %v skipped %d err %v, want %v %d", hits, skipped, err, wantHits, wantSkipped)
				}
			})
		}
		t.Run(fmt.Sprintf("%s-%s/locations", k[0], k[1]), func(t *testing.T) {
			s := covPhrase(t, phraseIt(k[0], x), phraseIt(k[1], y))
			dm, err := s.Next(covCtx())
			if err != nil || dm == nil || dm.Number != 1 {
				t.Fatalf("got %v %v", dm, err)
			}
			want := []search.FieldTermLocation{
				{Field: "body", Term: "x", Location: search.Location{Pos: 1, Start: 10, End: 13}},
				{Field: "body", Term: "y", Location: search.Location{Pos: 2, Start: 20, End: 23}},
			}
			if !slices.Equal(dm.FieldTermLocations, want) {
				t.Fatalf("locations %v", dm.FieldTermLocations)
			}
		})
	}
}

// Under a floor, a hit is reported after the run of losing hits before it,
// and a skipped that says stop ends the search there.
func TestRawPhraseCovRuns(t *testing.T) {
	x, y := phraseMix()
	for _, kind := range []phraseKind{kindLoc, kindOwn} {
		t.Run(string(kind), func(t *testing.T) {
			s := covPhrase(t, phraseIt(kind, x), phraseIt(kind, y))
			hits, skipped, err := nextAboveAll(t, s, covCtx(), 2, func(int) bool { return false })
			if err != nil || hits != nil || skipped != 1 {
				t.Fatalf("hits %v skipped %d err %v", hits, skipped, err)
			}
			// 5, 6 and 8 are runs of losing hits, which stop before 7 and 10
			s = covPhrase(t, phraseIt(kind, x), phraseIt(kind, y))
			ctx := covCtx()
			for _, want := range []uint64{3, 7} {
				dm, err := s.NextAbove(ctx, 2, func(float64, int) bool { return true })
				if err != nil || dm == nil || dm.Number != want {
					t.Fatalf("want %d: %v %v", want, dm, err)
				}
			}
			stops := 0
			dm, err := s.NextAbove(ctx, 2, func(float64, int) bool { stops++; return false })
			if err != nil || dm != nil || stops != 1 {
				t.Fatalf("stop: %v %v %d", dm, err, stops)
			}
		})
	}
}

// longPhrase has n documents that each hold the phrase once, at
// positions 1 and 2, or reps times when reps > 1.
func longPhrase(n, reps int) (x, y []covDoc) {
	for d := range uint64(n) { //nolint:gosec // small
		var px, py []int
		for r := range reps {
			px, py = append(px, 1+3*r), append(py, 2+3*r)
		}
		x, y = append(x, pd(d, pl(px...))), append(y, pd(d, pl(py...)))
	}
	return x, y
}

// Runs of losing hits are reported at most maxSkippedRun at a time, from
// the fast single-occurrence loop and from the general one.
func TestRawPhraseCovLongRuns(t *testing.T) {
	n := maxSkippedRun + 100
	for _, tc := range []struct {
		kind, kindY phraseKind
		reps        int
	}{{kindLoc, kindLoc, 1}, {kindOwn, kindOwn, 1}, {kindOwn, kindPos, 1}, {kindLoc, kindLoc, 2}} {
		t.Run(fmt.Sprintf("%s-%s/%d", tc.kind, tc.kindY, tc.reps), func(t *testing.T) {
			x, y := longPhrase(n, tc.reps)
			var runs []int
			s := covPhrase(t, phraseIt(tc.kind, x), phraseIt(tc.kindY, y))
			dm, err := s.NextAbove(covCtx(), 100, func(_ float64, k int) bool { runs = append(runs, k); return true })
			if err != nil || dm != nil {
				t.Fatalf("got %v %v", dm, err)
			}
			sum := 0
			for _, k := range runs {
				if k > maxSkippedRun {
					t.Fatalf("run of %d", k)
				}
				sum += k
			}
			if sum != n {
				t.Fatalf("skipped %d of %d in %v", sum, n, runs)
			}
			runs = nil
			s = covPhrase(t, phraseIt(tc.kind, x), phraseIt(tc.kindY, y))
			dm, err = s.NextAbove(covCtx(), 100, func(_ float64, k int) bool { runs = append(runs, k); return false })
			if err != nil || dm != nil || len(runs) != 1 || runs[0] > maxSkippedRun {
				t.Fatalf("stop: %v %v %v", dm, err, runs)
			}
		})
	}
}

// A cursor far behind the others seeks instead of reading every posting.
func TestRawPhraseCovSeek(t *testing.T) {
	x := []covDoc{pd(1, pl(1)), pd(5000, pl(1))}
	var y []covDoc
	for d := uint64(1); d <= 200; d++ {
		y = append(y, pd(d, pl(2)))
	}
	y = append(y, pd(5000, pl(2)))
	for _, kind := range []phraseKind{kindPlain, kindLoc} {
		t.Run(string(kind), func(t *testing.T) {
			s := covPhrase(t, phraseIt(kind, x), phraseIt(kind, y))
			if got, err := covDrain(covCtx(), s); err != nil || !slices.Equal(got, docs(1, 5000)) {
				t.Fatalf("got %v %v", got, err)
			}
			s = covPhrase(t, phraseIt(kind, x), phraseIt(kind, y))
			if dm, err := s.Advance(covCtx(), 3000); err != nil || dm.Number != 5000 {
				t.Fatalf("advance: %v %v", dm, err)
			}
		})
	}
	// the plain cursor reads one posting at a time, seeking past its end
	s := covPhrase(t, phraseIt(kindPlain, x[:1]), phraseIt(kindPlain, y))
	if dm, err := s.Advance(covCtx(), 3000); err != nil || dm != nil {
		t.Fatalf("advance past end: %v %v", dm, err)
	}
}

func TestRawPhraseCovErrors(t *testing.T) {
	x, y := phraseMix()
	big := binary.AppendUvarint(binary.AppendUvarint(nil, 2), 1<<63)
	tests := []struct {
		name  string
		x, y  func() segment.PostingsIterator
		floor float64
	}{
		{name: "plain next", x: func() segment.PostingsIterator { return &covPostings{docs: x, nextErr: errCov} },
			y: func() segment.PostingsIterator { return phraseIt(kindPlain, y) }},
		{name: "loc batch", x: func() segment.PostingsIterator {
			return &covLocPostings{covPostings: &covPostings{docs: x}, batchErr: errCov}
		}, y: func() segment.PostingsIterator { return phraseIt(kindLoc, y) }},
		{name: "pos batch", x: func() segment.PostingsIterator {
			it := phraseIt(kindPos, x).(*covPosPostings)
			it.batchErr = errCov
			return it
		}, y: func() segment.PostingsIterator { return phraseIt(kindPos, y) }},
		{name: "own batch", x: func() segment.PostingsIterator {
			it := phraseIt(kindOwn, x).(*covOwnPostings)
			it.ownErr = errCov
			return it
		}, y: func() segment.PostingsIterator { return phraseIt(kindOwn, y) }},
		{name: "seek", x: func() segment.PostingsIterator {
			return phraseIt(kindPlain, []covDoc{pd(1, pl(1)), pd(5000, pl(1))})
		}, y: func() segment.PostingsIterator {
			return &covPostings{docs: []covDoc{pd(1, pl(2)), pd(2, pl(2)), pd(5000, pl(2))},
				advErr: errCov}
		}},
		{name: "offsets", x: func() segment.PostingsIterator {
			it := phraseIt(kindPos, x).(*covPosPostings)
			it.loadErr = errCov
			return it
		}, y: func() segment.PostingsIterator { return phraseIt(kindPos, y) }},
		{name: "decode", x: func() segment.PostingsIterator { return phraseIt(kindOwn, x) },
			y: func() segment.PostingsIterator {
				it := phraseIt(kindOwn, y).(*covOwnPostings)
				it.bad = map[uint64][]byte{1: big}
				return it
			}},
		{name: "expand", x: func() segment.PostingsIterator { return phraseIt(kindPos, x) },
			y: func() segment.PostingsIterator {
				it := phraseIt(kindOwn, y).(*covOwnPostings)
				it.bad = map[uint64][]byte{1: big}
				return it
			}},
		{name: "expand below floor", floor: 100, x: func() segment.PostingsIterator { return phraseIt(kindPos, x) },
			y: func() segment.PostingsIterator {
				it := phraseIt(kindOwn, y).(*covOwnPostings)
				it.bad = map[uint64][]byte{1: big}
				return it
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := covPhrase(t, tc.x(), tc.y())
			floor := tc.floor
			if floor == 0 {
				floor = math.Inf(-1)
			}
			if _, _, err := nextAboveAll(t, s, covCtx(), floor, always); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
	// an own batch may hand over to positions read with their offsets
	it := phraseIt(kindOwn, y).(*covOwnPostings)
	it.notOwn = true
	s := covPhrase(t, phraseIt(kindOwn, x), it)
	want, _ := expectPhrase(x, y, math.Inf(-1))
	if got, err := covDrain(covCtx(), s); err != nil || !slices.Equal(got, want) {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestNewRawPhraseCovDeclines(t *testing.T) {
	ts := func(term string) *TermSearcher {
		s := covTerm(&covPostings{}, noExplain())
		s.queryTerm = term
		return s
	}
	terms := [][]string{{"x"}, {"y"}}
	for _, tc := range []struct {
		name string
		conj *ConjunctionSearcher
	}{
		{"scorer", &ConjunctionSearcher{searchers: OrderedSearcherList{ts("x"), ts("y")}, scorer: similarity.ConstantScorer(1)}},
		{"not a term", &ConjunctionSearcher{searchers: OrderedSearcherList{ts("x"), &covSearcher{}},
			scorer: similarity.NewCompositeSumScorer()}},
		{"other term", &ConjunctionSearcher{searchers: OrderedSearcherList{ts("x"), ts("z")},
			scorer: similarity.NewCompositeSumScorer()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if r := newRawPhrase(tc.conj, terms, "body", noExplain()); r != nil {
				t.Fatal("expected no raw phrase")
			}
		})
	}
}
