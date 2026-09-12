// Copyright (c) 2026 The Bluge Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package knn

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/ice/vec"

	"github.com/vcaesar/riot/search"
)

// stubReader satisfies search.Reader; only SearchVectors does anything.
type stubReader struct {
	matches []vec.Match
	err     error
	field   string
	k       int
	accept  func(uint64) bool
}

func (s *stubReader) SearchVectors(_ context.Context, field string, _ []float32, k int,
	_ vec.Metric, accept func(uint64) bool) ([]vec.Match, error) {
	s.field, s.k, s.accept = field, k, accept
	return s.matches, s.err
}

func (s *stubReader) DocumentValueReader([]string) (segment.DocumentValueReader, error) {
	return nil, nil
}
func (s *stubReader) VisitStoredFields(uint64, segment.StoredFieldVisitor) error { return nil }
func (s *stubReader) CollectionStats(string) (segment.CollectionStats, error)    { return nil, nil }
func (s *stubReader) DictionaryLookup(string) (segment.DictionaryLookup, error)  { return nil, nil }
func (s *stubReader) DictionaryIterator(string, segment.Automaton, []byte, []byte) (segment.DictionaryIterator, error) {
	return nil, nil
}
func (s *stubReader) PostingsIterator([]byte, string, bool, bool, bool) (segment.PostingsIterator, error) {
	return nil, nil
}
func (s *stubReader) Close() error { return nil }

type noVectorReader struct{ *stubReader }

func (noVectorReader) SearchVectors() {}

func newSearcher(t *testing.T, r search.Reader, boost float64, explain bool) *Searcher {
	t.Helper()
	s, err := NewSearcher(context.Background(), r, "v", []float32{1, 0}, 3, vec.DotProduct, boost, nil,
		search.SearcherOptions{Explain: explain})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func drain(t *testing.T, s *Searcher) (numbers []uint64, scores []float64) {
	t.Helper()
	ctx := search.NewSearchContext(s.DocumentMatchPoolSize(), 0)
	for {
		m, err := s.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if m == nil {
			return numbers, scores
		}
		numbers = append(numbers, m.Number)
		scores = append(scores, m.Score)
	}
}

func TestSearcherOrderAndScore(t *testing.T) {
	r := &stubReader{matches: []vec.Match{{Number: 7, Score: 0.9}, {Number: 2, Score: 0.5}, {Number: 4, Score: 0.7}}}
	s := newSearcher(t, r, 2, false)
	defer func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	}()
	if r.field != "v" || r.k != 3 || r.accept != nil {
		t.Fatalf("reader called with field %q k %d accept %v", r.field, r.k, r.accept != nil)
	}
	if s.Count() != 3 || s.Min() != 0 || s.Size() <= 0 {
		t.Fatalf("count %d min %d size %d", s.Count(), s.Min(), s.Size())
	}
	numbers, scores := drain(t, s)
	if !reflect.DeepEqual(numbers, []uint64{2, 4, 7}) || !reflect.DeepEqual(scores, []float64{1, 1.4, 1.8}) {
		t.Fatalf("got %v %v", numbers, scores)
	}
}

func TestSearcherAdvance(t *testing.T) {
	r := &stubReader{matches: []vec.Match{{Number: 2, Score: 1}, {Number: 4, Score: 1}, {Number: 7, Score: 1}}}
	ctx := search.NewSearchContext(1, 0)
	for _, tc := range []struct {
		name   string
		steps  []uint64
		expect []uint64 // 0 means nil
	}{
		{"exact", []uint64{4}, []uint64{4}},
		{"between", []uint64{3, 5}, []uint64{4, 7}},
		{"behind never rewinds", []uint64{7, 1}, []uint64{7, 0}},
		{"past end", []uint64{8}, []uint64{0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSearcher(t, r, 1, false)
			for i, n := range tc.steps {
				m, err := s.Advance(ctx, n)
				if err != nil {
					t.Fatal(err)
				}
				var got uint64
				if m != nil {
					got = m.Number
				}
				if got != tc.expect[i] {
					t.Fatalf("advance(%d) = %d, want %d", n, got, tc.expect[i])
				}
			}
		})
	}
}

func TestSearcherExplain(t *testing.T) {
	r := &stubReader{matches: []vec.Match{{Number: 1, Score: 0.5}}}
	s := newSearcher(t, r, 3, true)
	m, err := s.Next(search.NewSearchContext(1, 0))
	if err != nil || m == nil || m.Explanation == nil {
		t.Fatalf("match %v err %v", m, err)
	}
	if m.Explanation.Value != 1.5 || !strings.Contains(m.Explanation.Message, "knn(v:dot_product)") {
		t.Fatalf("explanation %s", m.Explanation)
	}
}

func TestSearcherPrefilter(t *testing.T) {
	r := &stubReader{}
	accept := func(n uint64) bool { return n%2 == 0 }
	_, err := NewSearcher(context.Background(), r, "v", []float32{1}, 1, vec.L2, 1, accept, search.SearcherOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r.accept == nil || !r.accept(2) || r.accept(3) {
		t.Fatal("accept not forwarded to reader")
	}
}

func TestSearcherErrors(t *testing.T) {
	boom := errors.New("boom")
	_, err := NewSearcher(context.Background(), &stubReader{err: boom}, "v", []float32{1}, 1, vec.L2, 1, nil,
		search.SearcherOptions{})
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped reader error, got %v", err)
	}
	_, err = NewSearcher(context.Background(), noVectorReader{&stubReader{}}, "v", []float32{1}, 1, vec.L2, 1, nil,
		search.SearcherOptions{})
	if err == nil || !strings.Contains(err.Error(), "does not support vector search") {
		t.Fatalf("expected unsupported reader error, got %v", err)
	}
}
