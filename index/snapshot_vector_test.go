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
	"testing"

	"github.com/RoaringBitmap/roaring/v2"
	segment "github.com/blugelabs/bluge_segment_api"
	"github.com/blugelabs/ice/vec"
)

type vectorTestSegment struct {
	segment.Segment
	matches []vec.Match
	err     error
}

func (s *vectorTestSegment) SearchVectors(ctx context.Context, field string, query []float32, k int,
	metric vec.Metric, accept func(uint64) bool) ([]vec.Match, error) {
	if s.err != nil {
		return nil, s.err
	}
	var rv []vec.Match
	for _, m := range s.matches {
		if accept(m.Number) {
			rv = append(rv, m)
		}
		if len(rv) == k {
			break
		}
	}
	return rv, nil
}

func TestSnapshotSearchVectors(t *testing.T) {
	s := &Snapshot{offsets: []uint64{0, 3}, segment: []*segmentSnapshot{
		{segment: &segmentWrapper{Segment: &vectorTestSegment{matches: []vec.Match{
			{Number: 0, Score: 10}, {Number: 1, Score: 5}, {Number: 2, Score: 4},
		}}}, deleted: roaring.BitmapOf(0)},
		{segment: &segmentWrapper{Segment: &vectorTestSegment{matches: []vec.Match{
			{Number: 0, Score: 9}, {Number: 1, Score: 5}, {Number: 2, Score: 3},
		}}}},
	}}
	var seen []uint64
	got, err := s.SearchVectors(context.Background(), "v", []float32{1}, 2, vec.DotProduct, func(n uint64) bool {
		seen = append(seen, n)
		return n != 3
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []vec.Match{{Number: 1, Score: 5}, {Number: 4, Score: 5}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if !reflect.DeepEqual(seen, []uint64{1, 2, 3, 4, 5}) {
		t.Fatalf("filter numbers: %v", seen)
	}
	failure := errors.New("segment failure")
	s.segment[1].segment.Segment.(*vectorTestSegment).err = failure
	if got, err = s.SearchVectors(context.Background(), "v", []float32{1}, 2, vec.DotProduct, nil); !errors.Is(err, failure) || got != nil {
		t.Fatalf("partial results or lost error: %v %v", got, err)
	}
	s.segment[1].segment.Segment = &segmentWrapper{}
	if _, err = s.SearchVectors(context.Background(), "v", []float32{1}, 2, vec.DotProduct, nil); err == nil || !strings.Contains(err.Error(), "does not support vector search") {
		t.Fatalf("unsupported plugin: %v", err)
	}
}

func TestSnapshotSearchVectorsValidation(t *testing.T) {
	s := &Snapshot{}
	for _, tc := range []struct {
		field  string
		query  []float32
		k      int
		metric vec.Metric
	}{
		{"", []float32{1}, 1, vec.L2}, {"v", nil, 1, vec.L2},
		{"v", []float32{1}, 0, vec.L2}, {"v", []float32{1}, -1, vec.L2},
		{"v", []float32{1}, 1, "unknown"}, {"v", []float32{0}, 1, vec.Cosine},
	} {
		if _, err := s.SearchVectors(context.Background(), tc.field, tc.query, tc.k, tc.metric, nil); err == nil {
			t.Fatalf("accepted invalid request: %+v", tc)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.SearchVectors(ctx, "v", []float32{1}, 1, vec.L2, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
