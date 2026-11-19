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

package collector

import (
	"context"
	"errors"
	"reflect"
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/riot/numeric"
	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/aggregations"
)

var errCov = errors.New("cov error")

// numReader stores int64 values of field "n" as a typed numeric column.
type numReader struct {
	vals      map[uint64][]int64
	terms     map[uint64]bool // doc also has a term in "n"
	failDoc   uint64
	fail      bool
	readerErr error
}

func (r *numReader) DocumentValueReader([]string) (segment.DocumentValueReader, error) {
	if r.readerErr != nil {
		return nil, r.readerErr
	}
	return r, nil
}

func (r *numReader) VisitDocumentValues(n uint64, visitor segment.DocumentValueVisitor) error {
	return r.VisitDocumentNumbers(n, visitor, func(field string, v int64) {
		visitor(field, numeric.MustNewPrefixCodedInt64(v, 0))
	})
}

func (r *numReader) VisitDocumentNumbers(n uint64, visitor segment.DocumentValueVisitor,
	numVisitor segment.NumericValueVisitor) error {
	if r.fail && n == r.failDoc {
		return errCov
	}
	for _, v := range r.vals[n] {
		numVisitor("n", v)
	}
	if r.terms[n] {
		visitor("n", []byte("x"))
	}
	return nil
}

func (r *numReader) VisitStoredFields(uint64, segment.StoredFieldVisitor) error { return nil }

// covSearcher is a stubSearcher that sets a reader on its matches, can fail
// and can run a hook before each Next.
type covSearcher struct {
	stubSearcher
	reader search.MatchReader
	err    error
	onNext func()
}

func (s *covSearcher) Next(ctx *search.Context) (*search.DocumentMatch, error) {
	if s.onNext != nil {
		s.onNext()
	}
	if s.err != nil {
		return nil, s.err
	}
	m, err := s.stubSearcher.Next(ctx)
	if m != nil && s.reader != nil {
		m.SetReader(s.reader)
	}
	return m, err
}

func drain(t *testing.T, iter search.DocumentMatchIterator) []uint64 {
	t.Helper()
	var docs []uint64
	for {
		m, err := iter.Next()
		if err != nil {
			t.Fatal(err)
		}
		if m == nil {
			return docs
		}
		docs = append(docs, m.Number)
	}
}

func scoredMatches(scores ...float64) []*search.DocumentMatch {
	rv := make([]*search.DocumentMatch, len(scores))
	for i, s := range scores {
		rv[i] = &search.DocumentMatch{Number: uint64(i), Score: s}
	}
	return rv
}

func TestAllCollectorSizes(t *testing.T) {
	c := NewAllCollector()
	if c.Size() != 0 || c.BackingSize() != 0 {
		t.Errorf("Size() = %d, BackingSize() = %d, want 0, 0", c.Size(), c.BackingSize())
	}
}

func TestAllIteratorErrors(t *testing.T) {
	sumAggs := make(search.Aggregations)
	sumAggs.Add("sum", aggregations.Sum(search.Field("n")))
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, test := range []struct {
		name     string
		ctx      context.Context
		aggs     search.Aggregations
		searcher *covSearcher
		wantErr  error
	}{
		{
			name:     "canceled",
			ctx:      canceled,
			aggs:     make(search.Aggregations),
			searcher: &covSearcher{stubSearcher: stubSearcher{matches: scoredMatches(1)}},
			wantErr:  context.Canceled,
		},
		{
			name:     "searcher error",
			ctx:      context.Background(),
			aggs:     make(search.Aggregations),
			searcher: &covSearcher{err: errCov},
			wantErr:  errCov,
		},
		{
			name: "load values error",
			ctx:  context.Background(),
			aggs: sumAggs,
			searcher: &covSearcher{
				stubSearcher: stubSearcher{matches: scoredMatches(1)},
				reader:       &numReader{readerErr: errCov},
			},
			wantErr: errCov,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			iter, err := NewAllCollector().Collect(test.ctx, test.aggs, test.searcher)
			if err != nil {
				t.Fatal(err)
			}
			if m, err := iter.Next(); m != nil || !errors.Is(err, test.wantErr) {
				t.Fatalf("Next() = %v, %v, want nil, %v", m, err, test.wantErr)
			}
			if m, err := iter.Next(); m != nil || err != nil {
				t.Fatalf("Next() after done = %v, %v, want nil, nil", m, err)
			}
		})
	}
}

func TestTopNIteratorError(t *testing.T) {
	iter := &TopNIterator{err: errCov}
	if m, err := iter.Next(); m != nil || !errors.Is(err, errCov) {
		t.Fatalf("Next() = %v, %v, want nil, %v", m, err, errCov)
	}
}

func TestTopNCollectorSizes(t *testing.T) {
	order := search.SortOrder{search.SortBy(search.Field("n"))}
	for _, test := range []struct {
		name        string
		size, skip  int
		backingSize int
	}{
		{name: "small", size: 3, skip: 2, backingSize: 6},
		{name: "capped", size: PreAllocSizeSkipCap, skip: 5, backingSize: PreAllocSizeSkipCap + 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := NewTopNCollector(test.size, test.skip, order)
			if got := c.BackingSize(); got != test.backingSize {
				t.Errorf("BackingSize() = %d, want %d", got, test.backingSize)
			}
			if got := c.Size(); got <= reflectStaticSizeTopNCollector {
				t.Errorf("Size() = %d, want > %d", got, reflectStaticSizeTopNCollector)
			}
		})
	}
}

func TestTopNSkipBeyondSliceHits(t *testing.T) {
	order := search.SortOrder{search.SortBy(search.DocumentScore()).Desc()}
	iter, err := NewTopNCollector(3, 5, order).Collect(context.Background(), make(search.Aggregations),
		&stubSearcher{matches: scoredMatches(1, 2)})
	if err != nil {
		t.Fatal(err)
	}
	if docs := drain(t, iter); len(docs) != 0 {
		t.Errorf("docs = %v, want none", docs)
	}
}

func TestTopNCollectorAfter(t *testing.T) {
	after := [][]byte{numeric.MustNewPrefixCodedInt64(numeric.Float64ToInt64(5), 0)}
	for _, test := range []struct {
		name    string
		reverse bool
		want    []uint64
	}{
		{name: "forward", want: []uint64{3, 2, 1, 0}},
		{name: "reverse", reverse: true, want: []uint64{0, 1, 2, 3}},
	} {
		t.Run(test.name, func(t *testing.T) {
			order := search.SortOrder{search.SortBy(search.DocumentScore()).Desc()}
			c := NewTopNCollectorAfter(10, order, after, test.reverse)
			if c.searchAfter.Score != 5 {
				t.Fatalf("decoded search after score = %v, want 5", c.searchAfter.Score)
			}
			iter, err := c.Collect(context.Background(), make(search.Aggregations),
				&stubSearcher{matches: scoredMatches(1, 2, 3, 4, 5, 6, 7)})
			if err != nil {
				t.Fatal(err)
			}
			if docs := drain(t, iter); !reflect.DeepEqual(docs, test.want) {
				t.Errorf("docs = %v, want %v", docs, test.want)
			}
		})
	}
}

func TestTopNCollectCancelled(t *testing.T) {
	order := search.SortOrder{search.SortBy(search.DocumentScore()).Desc()}
	t.Run("before first hit", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := NewTopNCollector(3, 0, order).Collect(ctx, make(search.Aggregations),
			&stubSearcher{matches: scoredMatches(1)})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	})
	t.Run("during first hit", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s := &covSearcher{stubSearcher: stubSearcher{matches: scoredMatches(1, 2)}, onNext: cancel}
		_, err := NewTopNCollector(3, 0, order).Collect(ctx, make(search.Aggregations), s)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	})
}

func numMatches(n int) []*search.DocumentMatch {
	rv := make([]*search.DocumentMatch, n)
	for i := range rv {
		rv[i] = &search.DocumentMatch{Number: uint64(i), Score: 1}
	}
	return rv
}

func TestTopNNumericSort(t *testing.T) {
	for _, test := range []struct {
		name  string
		sort  *search.Sort
		vals  map[uint64][]int64
		terms map[uint64]bool
		want  []uint64
	}{
		{
			name: "ascending",
			sort: search.SortBy(search.Field("n")),
			// doc 3 sets the floor, doc 4 has two values, doc 5 a term
			vals:  map[uint64][]int64{0: {5}, 1: {2}, 2: {7}, 3: {9}, 4: {1, 8}, 5: {0}, 6: {20}, 7: {3}},
			terms: map[uint64]bool{5: true},
			want:  []uint64{4, 1, 7}, // doc 5 sorts by its term
		},
		{
			name: "descending",
			sort: search.SortBy(search.Field("n")).Desc(),
			vals: map[uint64][]int64{0: {5}, 1: {2}, 2: {7}, 3: {1}, 4: {-3}, 5: {6}, 6: {9}, 7: {3}},
			want: []uint64{6, 2, 5},
		},
		{
			name: "missing value evicted first",
			sort: search.SortBy(search.Field("n")),
			vals: map[uint64][]int64{0: {5}, 1: {2}, 2: {7}, 4: {1}, 5: {4}, 6: {30}, 7: {6}},
			want: []uint64{4, 1, 5},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			aggs := make(search.Aggregations)
			aggs.Add("count", aggregations.CountMatches())
			s := &covSearcher{
				stubSearcher: stubSearcher{matches: numMatches(8)},
				reader:       &numReader{vals: test.vals, terms: test.terms},
			}
			iter, err := NewTopNCollector(3, 0, search.SortOrder{test.sort}).Collect(context.Background(), aggs, s)
			if err != nil {
				t.Fatal(err)
			}
			if docs := drain(t, iter); !reflect.DeepEqual(docs, test.want) {
				t.Errorf("docs = %v, want %v", docs, test.want)
			}
			if got := iter.Aggregations().Count(); got != 8 {
				t.Errorf("count = %d, want 8", got)
			}
		})
	}
}

func TestTopNNumericSortErrors(t *testing.T) {
	order := search.SortOrder{search.SortBy(search.Field("n"))}
	for _, test := range []struct {
		name   string
		reader *numReader
	}{
		{name: "load values", reader: &numReader{readerErr: errCov}},
		{
			name:   "doc number after floor",
			reader: &numReader{vals: map[uint64][]int64{0: {1}, 1: {2}, 2: {3}, 3: {4}}, fail: true, failDoc: 4},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := &covSearcher{stubSearcher: stubSearcher{matches: numMatches(6)}, reader: test.reader}
			_, err := NewTopNCollector(3, 0, order).Collect(context.Background(), make(search.Aggregations), s)
			if !errors.Is(err, errCov) {
				t.Fatalf("err = %v, want %v", err, errCov)
			}
		})
	}
}

func TestTopNSetLowestWithoutSortValue(t *testing.T) {
	hc := &TopNCollector{numSort: true, numFloorSet: true}
	d := &search.DocumentMatch{}
	hc.setLowest(d)
	if hc.lowestMatchOutsideResults != d || hc.numFloorSet {
		t.Errorf("setLowest = %v, floor set %v; want d, false", hc.lowestMatchOutsideResults, hc.numFloorSet)
	}
}

func TestTopNFieldSortOutsideResults(t *testing.T) {
	reader := &fieldTrackingReader{}
	s := &fieldTrackingSearcher{stubSearcher: stubSearcher{matches: numMatches(5)}, reader: reader}
	order := search.SortOrder{search.SortBy(search.Field("v")), search.SortBy(search.DocumentScore())}
	iter, err := NewTopNCollector(2, 0, order).Collect(context.Background(), make(search.Aggregations), s)
	if err != nil {
		t.Fatal(err)
	}
	// equal keys are ordered by hit number
	if docs := drain(t, iter); !reflect.DeepEqual(docs, []uint64{0, 1}) {
		t.Errorf("docs = %v, want [0 1]", docs)
	}
}
