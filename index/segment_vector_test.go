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

package index

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RoaringBitmap/roaring/v2"
	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/ice/vec"

	"github.com/vcaesar/riot/hnsw"
)

// storedVectorSegment serves encoded vectors under field "v" and counts visits.
type storedVectorSegment struct {
	segment.Segment
	vectors [][]byte
	visits  atomic.Int64
}

func (s *storedVectorSegment) Count() uint64 { return uint64(len(s.vectors)) }

func (s *storedVectorSegment) VisitStoredFields(number uint64, visitor segment.StoredFieldVisitor) error {
	s.visits.Add(1)
	visitor("_id", []byte("doc"))
	visitor("v", s.vectors[number])
	return nil
}

func encodedVectors(t *testing.T, vectors ...[]float32) [][]byte {
	t.Helper()
	rv := make([][]byte, len(vectors))
	for i, v := range vectors {
		data, err := vec.Encode(v)
		if err != nil {
			t.Fatal(err)
		}
		rv[i] = data
	}
	return rv
}

func TestSnapshotSearchVectorsANN(t *testing.T) {
	first := &storedVectorSegment{vectors: encodedVectors(t, []float32{10, 0}, []float32{5, 0}, []float32{4, 0})}
	second := &storedVectorSegment{vectors: encodedVectors(t, []float32{9, 0}, []float32{5, 0}, []float32{3, 0})}
	s := &Snapshot{offsets: []uint64{0, 3}, segment: []*segmentSnapshot{
		{segment: &segmentWrapper{Segment: first}, deleted: roaring.BitmapOf(0)},
		{segment: &segmentWrapper{Segment: second}},
	}}
	search := func(accept func(uint64) bool) []vec.Match {
		t.Helper()
		got, err := s.SearchVectorsANN(context.Background(), "v", []float32{1, 0}, 2, vec.DotProduct, hnsw.Params{}, accept)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	got := search(func(n uint64) bool { return n != 3 })
	if want := []vec.Match{{Number: 1, Score: 5}, {Number: 4, Score: 5}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	exact, err := s.SearchVectors(context.Background(), "v", []float32{1, 0}, 2, vec.DotProduct, nil)
	if err == nil || !strings.Contains(err.Error(), "does not support vector search") {
		t.Fatalf("fake segment must not support exact search: %v %v", exact, err)
	}
	visits := first.visits.Load()
	if got := search(nil); !reflect.DeepEqual(got, []vec.Match{{Number: 3, Score: 9}, {Number: 1, Score: 5}}) {
		t.Fatalf("got %v", got)
	}
	if first.visits.Load() != visits {
		t.Fatal("graph was rebuilt instead of reused")
	}
	// A different metric needs its own graph.
	if _, err := s.SearchVectorsANN(context.Background(), "v", []float32{1, 0}, 2, vec.L2, hnsw.Params{}, nil); err != nil {
		t.Fatal(err)
	}
	if first.visits.Load() == visits {
		t.Fatal("metric change must build a new graph")
	}
	if _, err := s.SearchVectorsANN(context.Background(), "v", []float32{1, 0}, 2, vec.L2, hnsw.Params{M: 1}, nil); err == nil {
		t.Fatal("accepted invalid params")
	}
	if _, err := s.SearchVectorsANN(context.Background(), "v", []float32{1}, 2, vec.L2, hnsw.Params{}, nil); err == nil {
		t.Fatal("accepted dimension mismatch")
	}
}

func TestSnapshotSearchVectorsANNBadStoredValue(t *testing.T) {
	seg := &storedVectorSegment{vectors: [][]byte{[]byte("not a vector")}}
	s := &Snapshot{offsets: []uint64{0}, segment: []*segmentSnapshot{{id: 7, segment: &segmentWrapper{Segment: seg}}}}
	_, err := s.SearchVectorsANN(context.Background(), "v", []float32{1}, 1, vec.L2, hnsw.Params{}, nil)
	if err == nil || !strings.Contains(err.Error(), "segment 7") || !strings.Contains(err.Error(), "document 0") {
		t.Fatalf("expected decode error with location, got %v", err)
	}
	visits := seg.visits.Load()
	if _, again := s.SearchVectorsANN(context.Background(), "v", []float32{1}, 1, vec.L2, hnsw.Params{}, nil); again == nil {
		t.Fatal("expected cached error")
	}
	if seg.visits.Load() != visits {
		t.Fatal("deterministic failure must be cached, not rebuilt")
	}
}

func TestSnapshotSearchVectorsANNCacheCancellation(t *testing.T) {
	vectors := make([][]float32, 64)
	for i := range vectors {
		vectors[i] = []float32{float32(i)}
	}
	seg := &storedVectorSegment{vectors: encodedVectors(t, vectors...)}
	s := &Snapshot{offsets: []uint64{0}, segment: []*segmentSnapshot{
		{segment: &segmentWrapper{Segment: seg}},
	}}
	params := hnsw.Params{EfSearch: len(vectors)}
	if _, err := s.SearchVectorsANN(context.Background(), "v", []float32{0}, 1, vec.L2, params, nil); err != nil {
		t.Fatal(err)
	}
	visits := seg.visits.Load()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	matches, err := s.SearchVectorsANN(ctx, "v", []float32{0}, 1, vec.L2, params, func(uint64) bool {
		calls++
		cancel()
		return true
	})
	if !errors.Is(err, context.Canceled) || matches != nil {
		t.Fatalf("got %v, %v; want cancellation without partial results", matches, err)
	}
	if calls != 1 {
		t.Fatalf("continued filtering after cancellation: %d calls", calls)
	}
	if seg.visits.Load() != visits {
		t.Fatal("cached graph was rebuilt")
	}
	if _, err := s.SearchVectorsANN(context.Background(), "v", []float32{0}, 1, vec.L2, params, nil); err != nil {
		t.Fatalf("search after cancellation: %v", err)
	}
}

func TestSnapshotVectorFilterFastPath(t *testing.T) {
	for _, tc := range []struct {
		name       string
		deleted    *roaring.Bitmap
		accept     func(uint64) bool
		wantFilter bool
	}{
		{name: "no filter"},
		{name: "empty deletion mask", deleted: roaring.New()},
		{name: "deleted", deleted: roaring.BitmapOf(1), wantFilter: true},
		{name: "accept", accept: func(n uint64) bool { return n == 10 }, wantFilter: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Snapshot{offsets: []uint64{10}, segment: []*segmentSnapshot{{deleted: tc.deleted}}}
			_, err := s.searchVectors(context.Background(), "v", []float32{1}, 1, vec.L2, tc.accept,
				func(_ *segmentSnapshot, accept func(uint64) bool) ([]vec.Match, error) {
					if (accept != nil) != tc.wantFilter {
						t.Fatalf("filter present = %v, want %v", accept != nil, tc.wantFilter)
					}
					if accept != nil && (!accept(0) || accept(1)) {
						t.Fatal("incorrect local deletion or global accept mapping")
					}
					return nil, nil
				})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSegmentVectorGraphAbandonedBuildRetries(t *testing.T) {
	seg := &storedVectorSegment{vectors: encodedVectors(t, []float32{1}, []float32{2})}
	w := &segmentWrapper{Segment: seg}
	key := graphKey{field: "v", metric: vec.L2, m: hnsw.DefaultM, efConstruction: hnsw.DefaultEfConstruction}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := w.vectorGraph(ctx, key); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if len(w.graphs) != 0 {
		t.Fatal("abandoned build was cached")
	}
	graph, err := w.vectorGraph(context.Background(), key)
	if err != nil || graph.Len() != 2 {
		t.Fatalf("retry: %v %v", graph, err)
	}
	if again, err := w.vectorGraph(context.Background(), key); err != nil || again != graph {
		t.Fatal("second lookup must return the cached graph")
	}
}
