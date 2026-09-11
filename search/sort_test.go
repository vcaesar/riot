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
	"testing"
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
	want0 := DocumentScore().Value(m)
	if !bytes.Equal(m.SortValue[0], want0) || !bytes.Equal(m.SortValue[1], []byte{7}) {
		t.Fatalf("unexpected sort values %v", m.SortValue)
	}
	first := m.SortValue[0]
	pool.Put(m)

	m = pool.Get()
	m.Number, m.Score = 9, 0.25
	allocs := testing.AllocsPerRun(100, func() {
		m.SortValue = m.SortValue[:0]
		order.Compute(m)
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
