// Copyright (c) 2026 The AtomAI.
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
	"math/rand"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring/v2"
	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/ice/vec"
)

type vectorTestSegment struct {
	segment.Segment
	matches []vec.Match
	err     error
}

func (s *vectorTestSegment) SearchVectors(_ context.Context, _ string, _ []float32, k int,
	_ vec.Metric, accept func(uint64) bool) ([]vec.Match, error) {
	if s.err != nil {
		return nil, s.err
	}
	var rv []vec.Match
	for _, m := range s.matches {
		if accept == nil || accept(m.Number) {
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
	// Segments run concurrently, so only the set of numbers is fixed.
	sort.Slice(seen, func(a, b int) bool { return seen[a] < seen[b] })
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

func TestSnapshotSearchVectorsMergeOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for iter := 0; iter < 200; iter++ {
		s := &Snapshot{}
		var live []vec.Match
		var running uint64
		for segs := rng.Intn(4); segs >= 0; segs-- {
			count := rng.Intn(6)
			deleted := roaring.New()
			var matches []vec.Match
			for n := 0; n < count; n++ {
				m := vec.Match{Number: uint64(n), Score: float64(rng.Intn(3))}
				matches = append(matches, m)
				if rng.Intn(3) == 0 {
					deleted.Add(uint32(n))
					continue
				}
				live = append(live, vec.Match{Number: running + m.Number, Score: m.Score})
			}
			sort.SliceStable(matches, func(a, b int) bool { return matches[a].Score > matches[b].Score })
			s.segment = append(s.segment, &segmentSnapshot{
				segment: &segmentWrapper{Segment: &vectorTestSegment{matches: matches}}, deleted: deleted,
			})
			s.offsets = append(s.offsets, running)
			running += uint64(len(matches))
		}
		k := 1 + rng.Intn(5)
		got, err := s.SearchVectors(context.Background(), "v", []float32{1}, k, vec.DotProduct, nil)
		if err != nil {
			t.Fatal(err)
		}
		sort.Slice(live, func(a, b int) bool {
			if live[a].Score == live[b].Score {
				return live[a].Number < live[b].Number
			}
			return live[a].Score > live[b].Score
		})
		want := live
		if len(want) > k {
			want = want[:k]
		}
		if len(want) == 0 {
			want = nil
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("iter %d k=%d: got %v, want %v", iter, k, got, want)
		}
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

// TestSnapshotSearchVectorsParallelSegments checks that many segments give
// the serial result, that accept never runs concurrently, and that the
// error of the first failing segment wins.
func TestSnapshotSearchVectorsParallelSegments(t *testing.T) {
	const segments, perSegment = 16, 50
	s := &Snapshot{}
	var all []vec.Match
	for i := range segments {
		var matches []vec.Match
		for n := range perSegment {
			score := float64((i*perSegment + n) * 7919 % 1000)
			matches = append(matches, vec.Match{Number: uint64(n), Score: score})
			all = append(all, vec.Match{Number: uint64(i*perSegment + n), Score: score})
		}
		sort.Slice(matches, func(a, b int) bool { return matches[a].Score > matches[b].Score })
		s.offsets = append(s.offsets, uint64(i*perSegment))
		s.segment = append(s.segment, &segmentSnapshot{id: uint64(i), segment: &segmentWrapper{Segment: &vectorTestSegment{matches: matches}}})
	}
	sort.Slice(all, func(a, b int) bool {
		if all[a].Score == all[b].Score {
			return all[a].Number < all[b].Number
		}
		return all[a].Score > all[b].Score
	})
	var inside, overlaps int
	got, err := s.SearchVectors(context.Background(), "v", []float32{1}, 20, vec.DotProduct, func(uint64) bool {
		inside++
		if inside > 1 {
			overlaps++
		}
		inside--
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, all[:20]) {
		t.Fatalf("got %v, want %v", got, all[:20])
	}
	if overlaps != 0 {
		t.Fatalf("accept ran concurrently %d times", overlaps)
	}

	t.Run("concurrent accept", func(t *testing.T) {
		if runtime.GOMAXPROCS(0) < 2 {
			t.Skip("segments run one at a time with GOMAXPROCS 1")
		}
		// Each call waits for another segment's, which deadlocks under a shared lock.
		var inside atomic.Int32
		both := make(chan struct{})
		_, err := s.searchSegments(context.Background(), func(uint64) bool {
			if inside.Add(1) == 2 {
				close(both)
			}
			select {
			case <-both:
				return true
			case <-time.After(10 * time.Second):
				t.Error("accept calls were serialized")
				return false
			}
		}, true, func(_ *segmentSnapshot, accept func(uint64) bool) ([]vec.Match, error) {
			accept(0)
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	first, later := errors.New("first"), errors.New("later")
	s.segment[3].segment.Segment.(*vectorTestSegment).err = first
	s.segment[12].segment.Segment.(*vectorTestSegment).err = later
	for range 20 {
		if _, err := s.SearchVectors(context.Background(), "v", []float32{1}, 5, vec.DotProduct, nil); !errors.Is(err, first) {
			t.Fatalf("err %v, want the first failing segment's", err)
		}
	}
}
