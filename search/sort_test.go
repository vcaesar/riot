//  Copyright (c) 2026 The Bluge Authors.
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

package search

import (
	"bytes"
	"math"
	"testing"

	"github.com/vcaesar/riot/numeric"
)

// scratchSource returns a value backed by a buffer it overwrites on every
// call, like a doc value reader would; Compute must copy it.
type scratchSource struct{ buf []byte }

func (s *scratchSource) Fields() []string { return nil }
func (s *scratchSource) Value(match *DocumentMatch) []byte {
	s.buf = append(s.buf[:0], byte(match.Number&0xff))
	return s.buf
}

func TestSortOrderComputeCopiesValues(t *testing.T) {
	order := SortOrder{SortBy(&scratchSource{})}
	a := &DocumentMatch{Number: 1}
	b := &DocumentMatch{Number: 2}
	order.Compute(a)
	order.Compute(b)
	if !bytes.Equal(a.SortValue[0], []byte{1}) || !bytes.Equal(b.SortValue[0], []byte{2}) {
		t.Fatalf("sort values alias the source scratch buffer: %v %v", a.SortValue, b.SortValue)
	}
}

func TestSortOrderComputeReusesPooledBuffers(t *testing.T) {
	order := SortOrder{SortBy(DocumentScore()).Desc(), SortBy(&scratchSource{})}
	pool := NewDocumentMatchPool(1, len(order))

	m := pool.Get()
	m.Number, m.Score = 7, 1.5
	order.Compute(m)
	if len(m.SortValue) != 2 || len(m.SortValue[0]) != 0 || !bytes.Equal(m.SortValue[1], []byte{7}) {
		t.Fatalf("unexpected sort values %v", m.SortValue)
	}
	order.Complete(m)
	if !bytes.Equal(m.SortValue[0], DocumentScore().Value(m)) {
		t.Fatalf("Complete did not encode the score: %v", m.SortValue)
	}
	first := m.SortValue[0]
	pool.Put(m)

	m = pool.Get()
	m.Number, m.Score = 9, 0.25
	allocs := testing.AllocsPerRun(100, func() {
		m.SortValue = m.SortValue[:0]
		order.Compute(m)
		order.Complete(m)
	})
	if allocs != 0 {
		t.Fatalf("expected 0 allocs on reused match, got %v", allocs)
	}
	if !bytes.Equal(m.SortValue[0], DocumentScore().Value(m)) || !bytes.Equal(m.SortValue[1], []byte{9}) {
		t.Fatalf("unexpected sort values after reuse %v", m.SortValue)
	}
	if &m.SortValue[0][0] != &first[0] {
		t.Fatal("expected the score buffer to be reused across pool round trips")
	}
}

func TestSortOrderScoreOnlyDefersKey(t *testing.T) {
	order := SortOrder{SortBy(DocumentScore()).Desc()}
	pool := NewDocumentMatchPool(1, len(order))

	m := pool.Get()
	m.Score = 1.5
	order.Compute(m)
	if len(m.SortValue) != 0 {
		t.Fatalf("score-only Compute should not touch SortValue, got %v", m.SortValue)
	}
	order.Complete(m)
	if len(m.SortValue) != 1 || !bytes.Equal(m.SortValue[0], DocumentScore().Value(m)) {
		t.Fatalf("Complete = %v, want encoded score", m.SortValue)
	}
	pool.Put(m)

	m = pool.Get()
	m.Score = 0.25
	allocs := testing.AllocsPerRun(100, func() {
		m.SortValue = m.SortValue[:0]
		order.Compute(m)
		order.Complete(m)
	})
	if allocs != 0 {
		t.Fatalf("expected 0 allocs on reused match, got %v", allocs)
	}
	if !bytes.Equal(m.SortValue[0], DocumentScore().Value(m)) {
		t.Fatalf("unexpected sort value after reuse %v", m.SortValue)
	}
}

func TestSortOrderCompareScore(t *testing.T) {
	order := SortOrder{SortBy(DocumentScore()).Desc(), SortBy(&scratchSource{})}
	hi := &DocumentMatch{Number: 1, Score: 2, HitNumber: 1}
	lo := &DocumentMatch{Number: 2, Score: 1, HitNumber: 2}
	tie := &DocumentMatch{Number: 3, Score: 2, HitNumber: 3}
	for _, m := range []*DocumentMatch{hi, lo, tie} {
		order.Compute(m)
	}
	if c := order.Compare(hi, lo); c >= 0 {
		t.Fatalf("desc score: Compare(hi, lo) = %d, want < 0", c)
	}
	if c := order.Compare(lo, hi); c <= 0 {
		t.Fatalf("desc score: Compare(lo, hi) = %d, want > 0", c)
	}
	// equal scores fall through to the secondary sort
	if c := order.Compare(hi, tie); c >= 0 {
		t.Fatalf("tie: Compare(hi, tie) = %d, want < 0 (secondary asc)", c)
	}

	// a search-after match only carries the encoded key
	order.Complete(hi)
	after := &DocumentMatch{SortValue: [][]byte{append([]byte(nil), hi.SortValue[0]...), {1}}}
	order.DecodeScore(after)
	if after.Score != hi.Score {
		t.Fatalf("DecodeScore = %v, want %v", after.Score, hi.Score)
	}
	if c := order.Compare(lo, after); c <= 0 {
		t.Fatalf("Compare(lo, after) = %d, want > 0", c)
	}

	bad := &DocumentMatch{SortValue: [][]byte{{0xff}, {1}}}
	order.DecodeScore(bad)
	if bad.Score != 0 {
		t.Fatalf("invalid key: Score = %v, want 0", bad.Score)
	}
	// a shift-4 key (numeric range term) or a truncated shift-0 key is
	// not a score key: never decode bits from it
	for name, key := range map[string][]byte{
		"shift 4":   numeric.MustNewPrefixCodedInt64(numeric.Float64ToInt64(hi.Score), 4),
		"truncated": hi.SortValue[0][:5],
	} {
		m := &DocumentMatch{SortValue: [][]byte{key, {1}}}
		order.DecodeScore(m)
		if m.Score != 0 {
			t.Fatalf("%s key: Score = %v, want 0", name, m.Score)
		}
	}
}

// TestSortOrderCompareNaNScoreTotalOrder: NaN and signed zero must order
// exactly like their prefix-coded keys so the collector heap stays
// consistent when a custom scorer produces them.
func TestSortOrderCompareNaNScoreTotalOrder(t *testing.T) {
	order := SortOrder{SortBy(DocumentScore())}
	matches := []*DocumentMatch{
		{Score: math.NaN(), HitNumber: 1},
		{Score: 1, HitNumber: 2},
		{Score: math.Inf(-1), HitNumber: 3},
		{Score: math.Copysign(0, -1), HitNumber: 4},
		{Score: 0, HitNumber: 5},
	}
	keyed := func(i, j *DocumentMatch) int {
		if c := bytes.Compare(DocumentScore().Value(i), DocumentScore().Value(j)); c != 0 {
			return c
		}
		switch {
		case i.HitNumber > j.HitNumber:
			return 1
		case i.HitNumber < j.HitNumber:
			return -1
		}
		return 0
	}
	for _, i := range matches {
		for _, j := range matches {
			if got, want := order.Compare(i, j), keyed(i, j); got != want {
				t.Fatalf("Compare(%v, %v) = %d, want %d (keyed)", i.Score, j.Score, got, want)
			}
		}
	}
}

func TestScoreSourceAppendValueMatchesValue(t *testing.T) {
	src := DocumentScore()
	for _, score := range []float64{0, -1, 0.5, 1234.5678} {
		m := &DocumentMatch{Score: score}
		want := src.Value(m)
		if got := src.AppendValue(m, nil); !bytes.Equal(got, want) {
			t.Fatalf("score %v: AppendValue(nil) = %v, want %v", score, got, want)
		}
		buf := make([]byte, 0, 32)
		got := src.AppendValue(m, buf)
		if !bytes.Equal(got, want) || &got[0] != &buf[:1][0] {
			t.Fatalf("score %v: AppendValue did not encode into the supplied buffer", score)
		}
	}
}

func TestMissingTextValueAppendValueFallsBack(t *testing.T) {
	missing := SortBy(FieldSource("nope")).Desc().MissingFirst()
	m := &DocumentMatch{}
	got := missing.source.(TextValueAppender).AppendValue(m, nil)
	if want := missing.Value(m); !bytes.Equal(got, want) {
		t.Fatalf("AppendValue = %v, want replacement %v", got, want)
	}
}
