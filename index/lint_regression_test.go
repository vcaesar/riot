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
	"math"
	"testing"

	"github.com/RoaringBitmap/roaring/v2"
	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/ice/vec"
)

type sizeTestSegment struct {
	segment.Segment
	count uint64
	size  int
}

func (s *sizeTestSegment) Count() uint64 { return s.count }
func (s *sizeTestSegment) Size() int     { return s.size }

func TestSegmentSizesSaturate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value uint64
		want  int64
	}{
		{"zero", 0, 0},
		{"ordinary", 42, 42},
		{"maximum", math.MaxInt64, math.MaxInt64},
		{"overflow", uint64(math.MaxInt64) + 1, math.MaxInt64},
		{"unsigned maximum", math.MaxUint64, math.MaxInt64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &segmentSnapshot{segmentSize: tc.value, segment: &segmentWrapper{
				Segment: &sizeTestSegment{count: tc.value},
			}}
			if s.Bytes() != tc.want || s.FullSize() != tc.want || s.LiveSize() != tc.want {
				t.Fatalf("sizes = (%d, %d, %d), want %d", s.Bytes(), s.FullSize(), s.LiveSize(), tc.want)
			}
		})
	}
	deleted := roaring.BitmapOf(0)
	s := &segmentSnapshot{deleted: deleted, segment: &segmentWrapper{
		Segment: &sizeTestSegment{count: 2, size: math.MaxInt},
	}}
	if s.Size() != math.MaxInt || s.LiveSize() != 1 {
		t.Fatalf("size = %d, live = %d", s.Size(), s.LiveSize())
	}
	s.segment.Segment = &sizeTestSegment{size: 10}
	if size := s.Size(); size < 0 || uint64(size) != 10+deleted.GetSizeInBytes() {
		t.Fatalf("ordinary bitmap size = %d", s.Size())
	}
}

func TestSnapshotSizeAndBlockingEventsSaturate(t *testing.T) {
	for _, tc := range []struct {
		value uint64
		want  int
	}{
		{0, 0}, {42, 42}, {uint64(math.MaxInt), math.MaxInt},
		{uint64(math.MaxInt) + 1, math.MaxInt}, {math.MaxUint64, math.MaxInt},
	} {
		s := &Snapshot{size: tc.value}
		w := &Writer{stats: Stats{TotEventFired: tc.value}}
		if s.Size() != tc.want || w.numEventsBlocking() != tc.want {
			t.Errorf("value %d: size = %d, blocking = %d, want %d", tc.value, s.Size(), w.numEventsBlocking(), tc.want)
		}
	}
	w := &Writer{stats: Stats{TotEventFired: 1, TotEventReturned: math.MaxUint64}}
	if w.numEventsBlocking() != 2 {
		t.Fatal("event counter wraparound must preserve the outstanding count")
	}
}

func TestNegativePersisterThresholdDoesNotNap(t *testing.T) {
	w := &Writer{
		config:    Config{PersisterNapUnderNumFiles: -1, PersisterNapTimeMSec: 1},
		directory: &statsDirectory{},
	}
	epoch, watchers := w.pausePersisterForMergerCatchUp(nil, 2, 1, nil)
	if epoch != 1 || len(watchers) != 0 || w.stats.TotPersisterNapPauseCompleted != 0 {
		t.Fatalf("negative threshold caused a pause: epoch %d, watchers %v, naps %d",
			epoch, watchers, w.stats.TotPersisterNapPauseCompleted)
	}
}

func TestNegativeSegmentSizeDoesNotWrap(t *testing.T) {
	ss := &segmentSnapshot{segment: &segmentWrapper{Segment: &sizeTestSegment{size: -1}}}
	if ss.SegmentSize() != 0 {
		t.Fatalf("negative segment size wrapped to %d", ss.SegmentSize())
	}
	s := &Snapshot{segment: []*segmentSnapshot{ss}}
	s.updateSize()
	if s.Size() != reflectStaticSizeIndexSnapshot {
		t.Fatalf("snapshot size = %d, want %d", s.Size(), reflectStaticSizeIndexSnapshot)
	}
}

func TestPostingsAllAdvanceBeyondUint32(t *testing.T) {
	for _, number := range []uint64{math.MaxUint32, uint64(math.MaxUint32) + 1, math.MaxUint64} {
		i := &postingsIteratorAll{
			snapshot:  &Snapshot{offsets: []uint64{0}, segment: []*segmentSnapshot{{}}},
			iterators: []roaring.IntPeekable{roaring.BitmapOf(0, math.MaxUint32).Iterator()},
		}
		posting, err := i.Advance(number)
		if err != nil {
			t.Fatal(err)
		}
		if number == math.MaxUint32 {
			if posting == nil || posting.Number() != number {
				t.Fatalf("maximum document number not found: %v", posting)
			}
		} else if posting != nil {
			t.Fatalf("advance to %d wrapped to %d", number, posting.Number())
		}
	}
}

func TestUnadornedAdvanceBeyondUint32(t *testing.T) {
	for _, number := range []uint64{math.MaxUint32, uint64(math.MaxUint32) + 1, math.MaxUint64} {
		i := newUnadornedPostingsIteratorFromBitmap(roaring.BitmapOf(0, math.MaxUint32))
		posting, err := i.Advance(number)
		if err != nil {
			t.Fatal(err)
		}
		if number == math.MaxUint32 {
			if posting == nil || posting.Number() != number {
				t.Fatalf("maximum document number not found: %v", posting)
			}
		} else if posting != nil {
			t.Fatalf("advance to %d wrapped to %d", number, posting.Number())
		}
		if next, err := i.Next(); next != nil || err != nil {
			t.Fatalf("exhausted iterator returned %v, %v", next, err)
		}
	}
}

func TestVectorDeletionDoesNotWrap(t *testing.T) {
	const number = uint64(math.MaxUint32) + 1
	s := &Snapshot{offsets: []uint64{0}, segment: []*segmentSnapshot{{
		deleted: roaring.BitmapOf(0),
		segment: &segmentWrapper{Segment: &vectorTestSegment{matches: []vec.Match{{Number: number, Score: 1}}}},
	}}}
	matches, err := s.SearchVectors(context.Background(), "v", []float32{1}, 1, vec.DotProduct, nil)
	if err != nil || len(matches) != 1 || matches[0].Number != number {
		t.Fatalf("matches = %v, err = %v", matches, err)
	}
}

type oneHitTestIterator struct {
	segment.PostingsIterator
	segment.OptimizablePostingsIterator
	docNum uint64
}

func (i *oneHitTestIterator) ActualBitmap() *roaring.Bitmap { return nil }
func (i *oneHitTestIterator) DocNum1Hit() (uint64, bool)    { return i.docNum, true }

func TestDisjunctionDocumentNumberBounds(t *testing.T) {
	for _, docNum := range []uint64{math.MaxUint32, uint64(math.MaxUint32) + 1} {
		s := &Snapshot{parent: &Writer{}, segment: []*segmentSnapshot{{}}}
		itr := &oneHitTestIterator{docNum: docNum}
		o := &optimizeDisjunctionUnadorned{snapshot: s, tfrs: []*postingsIterator{
			{iterators: []segment.PostingsIterator{itr}},
			{iterators: []segment.PostingsIterator{itr}},
		}}
		got, err := o.Finish()
		if err != nil {
			t.Fatal(err)
		}
		if docNum > math.MaxUint32 {
			if got != nil {
				t.Fatal("unrepresentable document number must decline bitmap optimization")
			}
			continue
		}
		if got == nil {
			t.Fatal("maximum uint32 document number should be optimized")
		}
		optimized := got.(*postingsIterator)
		posting, err := optimized.iterators[0].Next()
		if err != nil || posting == nil || posting.Number() != docNum {
			t.Fatalf("optimized posting = %v, error = %v", posting, err)
		}
		if err := got.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
