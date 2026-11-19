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

package index

import (
	"errors"
	"math"
	"testing"

	"github.com/RoaringBitmap/roaring/v2"
	segment "github.com/vcaesar/bluge_segment_api"
)

// covPosting is a posting with one location.
type covPosting struct{ n uint64 }

func (p *covPosting) Number() uint64     { return p.n }
func (p *covPosting) SetNumber(n uint64) { p.n = n }
func (p *covPosting) Frequency() int     { return 1 }
func (p *covPosting) Norm() float64      { return 1 }
func (p *covPosting) Size() int          { return 8 }
func (p *covPosting) Locations() []segment.Location {
	return []segment.Location{&FakeLocation{F: "f", P: 1, S: 0, E: 1}}
}

// covPostingsIter iterates docs, then fails with err when it is set.
type covPostingsIter struct {
	docs []uint64
	pos  int
	err  error
}

func (c *covPostingsIter) Next() (segment.Posting, error) {
	if c.pos >= len(c.docs) {
		return nil, c.err
	}
	c.pos++
	return &covPosting{n: c.docs[c.pos-1]}, nil
}

func (c *covPostingsIter) Advance(n uint64) (segment.Posting, error) {
	for c.pos < len(c.docs) && c.docs[c.pos] < n {
		c.pos++
	}
	return c.Next()
}

func (c *covPostingsIter) Size() int     { return 1 }
func (c *covPostingsIter) Empty() bool   { return len(c.docs) == 0 }
func (c *covPostingsIter) Count() uint64 { return uint64(len(c.docs)) }
func (c *covPostingsIter) Close() error  { return nil }

// covBatchErrIter fails every batch read.
type covBatchErrIter struct{ covPostingsIter }

func (c *covBatchErrIter) NextBatch([]uint64, []int, []float64) (int, error) { return 0, errCov }
func (c *covBatchErrIter) NextLocBatch(_ []uint64, _ []int, _ []float64, _ []int,
	fields []string, locs []int) (n int, _ []string, _ []int, err error) {
	return 0, fields, locs, errCov
}
func (c *covBatchErrIter) NextPosBatch(_ []uint64, _ []int, _ []float64, _ []int, _ []uint64,
	fields []string, locs []int) (n int, _ []string, _ []int, err error) {
	return 0, fields, locs, errCov
}
func (c *covBatchErrIter) LoadOffsets(uint64, []int) error { return errCov }
func (c *covBatchErrIter) NextOwnPosBatch([]uint64, []int, []float64, []int, []int, []uint64) (
	n int, recs []byte, own bool, err error) {
	return 0, nil, true, errCov
}

// covBoundIter has blocks of 5 docs bounded by freq 3 and norm 0.5.
type covBoundIter struct {
	covPostingsIter
	ok bool
	bm *roaring.Bitmap
}

func (c *covBoundIter) BlockBound(docNum uint64) (maxFreq int, minNorm float64, end uint64, ok bool) {
	return 3, 0.5, (docNum/5 + 1) * 5, c.ok
}
func (c *covBoundIter) SkipBlocks(func(int, float64) bool) (uint64, error) { return 2, nil }
func (c *covBoundIter) ActualBitmap() *roaring.Bitmap                      { return c.bm }
func (c *covBoundIter) DocNum1Hit() (uint64, bool)                         { return 0, false }
func (c *covBoundIter) ReplaceActual(bm *roaring.Bitmap)                   { c.bm = bm }

func covIters(its ...segment.PostingsIterator) *postingsIterator {
	snap := covFakeSnapshot(len(its), Config{})
	return &postingsIterator{snapshot: snap, iterators: its}
}

func TestPostingsIteratorFallbackBatches(t *testing.T) {
	newIt := func() *postingsIterator {
		return covIters(&covPostingsIter{docs: []uint64{1, 2}}, &covPostingsIter{}, &covPostingsIter{docs: []uint64{3}})
	}
	docs, freqs, norms := make([]uint64, 4), make([]int, 4), make([]float64, 4)
	ends, offs := make([]int, 4), make([]uint64, 4)
	t.Run("NextBatch", func(t *testing.T) {
		it := newIt()
		var got []uint64
		for {
			n, err := it.NextBatch(docs, freqs, norms)
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				break
			}
			got = append(got, docs[:n]...)
		}
		if !covEqual(got, []uint64{1, 2, 23}) || !it.batched || it.currID != 23 {
			t.Fatalf("got %v, batched %v, currID %d", got, it.batched, it.currID)
		}
	})
	t.Run("NextPosBatch", func(t *testing.T) {
		it := newIt()
		var got []uint64
		var fields []string
		var locs []int
		for {
			var n int
			var err error
			n, fields, locs, err = it.NextPosBatch(docs, freqs, norms, ends, offs, fields, locs)
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				break
			}
			for k := range n {
				if offs[k] != noOffsets {
					t.Fatalf("offs[%d] = %d", k, offs[k])
				}
				if err := it.LoadOffsets(offs[k], locs); err != nil {
					t.Fatal(err)
				}
			}
			got = append(got, docs[:n]...)
		}
		if !covEqual(got, []uint64{1, 2, 23}) || len(fields) != 3 || len(locs) != 9 {
			t.Fatalf("got %v fields %v locs %v", got, fields, locs)
		}
	})
	t.Run("NextOwnPosBatch", func(t *testing.T) {
		n, recs, own, err := newIt().NextOwnPosBatch(docs, freqs, norms, ends, ends, offs)
		if n != 0 || recs != nil || own || err != nil {
			t.Fatalf("got %d %v %v %v", n, recs, own, err)
		}
		n, _, own, err = covIters().NextOwnPosBatch(docs, freqs, norms, ends, ends, offs)
		if n != 0 || !own || err != nil {
			t.Fatalf("end: got %d %v %v", n, own, err)
		}
	})
}

func TestPostingsIteratorErrors(t *testing.T) {
	docs, freqs, norms := make([]uint64, 4), make([]int, 4), make([]float64, 4)
	ends, offs := make([]int, 4), make([]uint64, 4)
	failing := func() *postingsIterator { return covIters(&covPostingsIter{err: errCov}) }
	batchErr := func() *postingsIterator { return covIters(&covBatchErrIter{}) }
	tests := []struct {
		name string
		call func() error
	}{
		{"Next", func() error { _, err := failing().Next(); return err }},
		{"Advance", func() error { _, err := failing().Advance(0); return err }},
		{"NextBatch", func() error { _, err := failing().NextBatch(docs, freqs, norms); return err }},
		{"NextBatch batcher", func() error { _, err := batchErr().NextBatch(docs, freqs, norms); return err }},
		{"NextLocBatch", func() error {
			_, _, _, err := failing().NextLocBatch(docs, freqs, norms, ends, nil, nil)
			return err
		}},
		{"NextLocBatch batcher", func() error {
			_, _, _, err := batchErr().NextLocBatch(docs, freqs, norms, ends, nil, nil)
			return err
		}},
		{"NextPosBatch batcher", func() error {
			_, _, _, err := batchErr().NextPosBatch(docs, freqs, norms, ends, offs, nil, nil)
			return err
		}},
		{"NextOwnPosBatch batcher", func() error {
			_, _, _, err := batchErr().NextOwnPosBatch(docs, freqs, norms, ends, ends, offs)
			return err
		}},
		{"LoadOffsets batcher", func() error { return batchErr().LoadOffsets(1, nil) }},
		{"LoadOffsets without batch", func() error {
			it := covIters()
			return it.LoadOffsets(1, nil)
		}},
		{"LoadOffsets without offsets", func() error { return failing().LoadOffsets(1, nil) }},
		{"Advance out of bounds", func() error {
			it := failing()
			it.snapshot.offsets = append(it.snapshot.offsets, 10)
			_, err := it.Advance(15)
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestPostingsIteratorBlocks(t *testing.T) {
	bounded := &covBoundIter{ok: true, bm: roaring.BitmapOf(1)}
	unbounded := &covBoundIter{}
	it := covIters(bounded, &covPostingsIter{}, unbounded)
	tests := []struct {
		number  uint64
		ok      bool
		maxFreq int
		end     uint64
	}{
		{2, true, 3, 5},
		{8, true, 3, 10}, // clipped to the next segment
		{12, false, 0, 0},
		{22, false, 0, 0},
	}
	for _, tt := range tests {
		maxFreq, minNorm, end, ok := it.BlockBound(tt.number)
		if ok != tt.ok || maxFreq != tt.maxFreq || end != tt.end || (ok && minNorm != 0.5) {
			t.Errorf("BlockBound(%d) = %d %v %d %v", tt.number, maxFreq, minNorm, end, ok)
		}
	}
	it.snapshot.offsets = append(it.snapshot.offsets, 30)
	if _, _, _, ok := it.BlockBound(35); ok {
		t.Errorf("BlockBound past the iterators: ok")
	}
	if bm, base := it.SegmentBitmap(35); bm != nil || base != 0 {
		t.Errorf("SegmentBitmap past the iterators: %v %d", bm, base)
	}
	if bm, base := it.SegmentBitmap(3); bm != bounded.bm || base != 0 {
		t.Errorf("SegmentBitmap(3) = %v %d", bm, base)
	}
	if bm, base := it.SegmentBitmap(13); bm != nil || base != 0 {
		t.Errorf("SegmentBitmap(13) = %v %d", bm, base)
	}
	if n, err := it.SkipBlocks(nil); n != 2 || err != nil {
		t.Errorf("SkipBlocks = %d %v", n, err)
	}
	it.segmentOffset = 1
	if n, err := it.SkipBlocks(nil); n != 0 || err != nil {
		t.Errorf("SkipBlocks plain = %d %v", n, err)
	}
	it.segmentOffset = 3
	if n, err := it.SkipBlocks(nil); n != 0 || err != nil {
		t.Errorf("SkipBlocks at end = %d %v", n, err)
	}
}

func TestPostingsIteratorCountSize(t *testing.T) {
	it := covIters(&covPostingsIter{docs: []uint64{1, 2}}, nil, &covPostingsIter{})
	if it.Count() != 2 || it.Empty() {
		t.Fatalf("count %d empty %v", it.Count(), it.Empty())
	}
	if covIters(&covPostingsIter{}).Empty() != true {
		t.Fatal("expected empty")
	}

	seg := covSegment(t)
	snap := covSnapshot(covConfig(), &segmentSnapshot{segment: seg}, &segmentSnapshot{segment: seg})
	pi, err := snap.PostingsIterator([]byte("a"), "f", true, true, true)
	if err != nil {
		t.Fatal(err)
	}
	base := pi.Size()
	if _, err := pi.Next(); err != nil {
		t.Fatal(err)
	}
	if pi.Size() < base || pi.Count() != 6 || pi.Empty() {
		t.Fatalf("size %d (was %d), count %d", pi.Size(), base, pi.Count())
	}
	if err := pi.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPostingsIteratorRealBatches(t *testing.T) {
	seg := covSegment(t)
	snap := covSnapshot(covConfig(), &segmentSnapshot{segment: seg}, &segmentSnapshot{segment: seg})
	open := func(t *testing.T, term string) *postingsIterator {
		pi, err := snap.PostingsIterator([]byte(term), "f", true, true, true)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = pi.Close() })
		return pi.(*postingsIterator)
	}
	docs, freqs, norms := make([]uint64, 8), make([]int, 8), make([]float64, 8)
	starts, ends, offs := make([]int, 8), make([]int, 8), make([]uint64, 8)
	want := []uint64{0, 1, 3, 4, 5, 7}
	t.Run("NextBatch", func(t *testing.T) {
		pi := open(t, "a")
		var got []uint64
		for {
			n, err := pi.NextBatch(docs, freqs, norms)
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				break
			}
			got = append(got, docs[:n]...)
		}
		if !covEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		// a backwards seek after a batch restarts
		p, err := pi.Advance(1)
		if err != nil || p == nil || p.Number() != 1 {
			t.Fatalf("advance: %v %v", p, err)
		}
	})
	t.Run("NextLocBatch", func(t *testing.T) {
		pi := open(t, "a")
		var got []uint64
		var fields []string
		var locs []int
		for {
			var n int
			var err error
			n, fields, locs, err = pi.NextLocBatch(docs, freqs, norms, ends, fields, locs)
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				break
			}
			got = append(got, docs[:n]...)
		}
		if !covEqual(got, want) || len(locs) != 3*len(want) {
			t.Fatalf("got %v locs %v", got, locs)
		}
	})
	t.Run("NextPosBatch", func(t *testing.T) {
		pi := open(t, "a")
		var got []uint64
		var fields []string
		var locs []int
		for {
			var n int
			var err error
			fields, locs = fields[:0], locs[:0]
			n, fields, locs, err = pi.NextPosBatch(docs, freqs, norms, ends, offs, fields, locs)
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				break
			}
			lo := 0
			for k := range n {
				if err := pi.LoadOffsets(offs[k], locs[3*lo:3*ends[k]]); err != nil {
					t.Fatal(err)
				}
				lo = ends[k]
			}
			got = append(got, docs[:n]...)
		}
		if !covEqual(got, want) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("NextOwnPosBatch", func(t *testing.T) {
		pi := open(t, "a")
		var got []uint64
		for {
			n, _, own, err := pi.NextOwnPosBatch(docs, freqs, norms, starts, ends, offs)
			if err != nil {
				t.Fatal(err)
			}
			if !own {
				t.Skip("segment does not store own positions")
			}
			if n == 0 {
				break
			}
			got = append(got, docs[:n]...)
		}
		if !covEqual(got, want) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("restart keeps exclusions", func(t *testing.T) {
		pi := open(t, "a")
		pi.replaceActual(1, roaring.BitmapOf(3))
		if got := covDrain(t, pi); !covEqual(got, []uint64{0, 1, 3, 7}) {
			t.Fatalf("got %v", got)
		}
		p, err := pi.Advance(0)
		if err != nil || p == nil || p.Number() != 0 {
			t.Fatalf("advance: %v %v", p, err)
		}
		if got := covDrain(t, pi); !covEqual(got, []uint64{1, 3, 7}) {
			t.Fatalf("after restart got %v", got)
		}
	})
	t.Run("restart error", func(t *testing.T) {
		pi := open(t, "a")
		if _, err := pi.Next(); err != nil {
			t.Fatal(err)
		}
		failing := covSnapshot(covConfig(), &segmentSnapshot{segment: &segmentWrapper{
			Segment: &covDictSegment{Segment: seg.Segment, err: errCov}, refCounter: noOpRefCounter{}}})
		pi.snapshot = failing
		if _, err := pi.Advance(0); !errors.Is(err, errCov) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("block bounds", func(t *testing.T) {
		pi := open(t, "a")
		if _, _, _, ok := pi.BlockBound(0); !ok {
			t.Skip("segment stores no block bounds")
		}
		if _, err := pi.SkipBlocks(func(int, float64) bool { return false }); err != nil {
			t.Fatal(err)
		}
	})
}

func TestPostingsIteratorErrorsFromSnapshot(t *testing.T) {
	seg := covSegment(t)
	tests := []struct {
		name string
		dict *covDict
	}{
		{"postings list", &covDict{plErr: errCov}},
		{"iterator", &covDict{itrErr: errCov}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := covSnapshot(covConfig(), &segmentSnapshot{segment: &segmentWrapper{
				Segment: &covDictSegment{Segment: seg.Segment, dict: tt.dict}, refCounter: noOpRefCounter{}}})
			if _, err := snap.PostingsIterator([]byte("a"), "f", false, false, false); !errors.Is(err, errCov) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestUnadornedIterators(t *testing.T) {
	bm := roaring.BitmapOf(1, 4, 9)
	it := newUnadornedPostingsIteratorFromBitmap(bm).(*unadornedPostingsIteratorBitmap)
	if it.Size() <= 0 || it.Empty() || it.Count() != 3 || it.Close() != nil || it.ActualBitmap() != bm {
		t.Fatal("unexpected bitmap iterator state")
	}
	if _, ok := it.DocNum1Hit(); ok {
		t.Fatal("bitmap iterator reports a 1-hit")
	}
	p, err := it.Advance(3)
	if err != nil || p.Number() != 4 {
		t.Fatalf("advance: %v %v", p, err)
	}
	p.SetNumber(40)
	if p.Number() != 40 || p.Frequency() != 0 || p.Norm() != 0 || p.Locations() != nil || p.Size() <= 0 {
		t.Fatalf("unexpected posting %v", p)
	}
	if p, _ := it.Advance(10); p != nil {
		t.Fatalf("advance past the end: %v", p)
	}
	it.ReplaceActual(roaring.BitmapOf(2))
	if p, _ := it.Advance(math.MaxUint32 + 1); p != nil {
		t.Fatalf("advance past uint32: %v", p)
	}
	if p, _ := it.Next(); p != nil {
		t.Fatalf("next after overflow: %v", p)
	}

	hit := newUnadornedPostingsIteratorFrom1Hit(5).(*unadornedPostingsIterator1Hit)
	if hit.Size() <= 0 || hit.Empty() || hit.Count() != 1 || hit.Close() != nil {
		t.Fatal("unexpected 1-hit iterator state")
	}
	if p, _ := hit.Advance(2); p == nil || p.Number() != 5 {
		t.Fatalf("advance to the hit: %v", p)
	}
	if p, _ := hit.Next(); p != nil {
		t.Fatalf("hit consumed: %v", p)
	}
	hit = newUnadornedPostingsIteratorFrom1Hit(5).(*unadornedPostingsIterator1Hit)
	if p, _ := hit.Advance(6); p != nil {
		t.Fatalf("advance past the hit: %v", p)
	}
	if p, _ := hit.Next(); p != nil {
		t.Fatalf("hit skipped: %v", p)
	}
}

func TestEmptyPostingsIterator(t *testing.T) {
	e := anEmptyPostingsIterator
	if p, err := e.Next(); p != nil || err != nil {
		t.Fatal("next")
	}
	if p, err := e.Advance(1); p != nil || err != nil {
		t.Fatal("advance")
	}
	if e.Size() != 0 || !e.Empty() || e.Count() != 0 || e.Close() != nil {
		t.Fatal("unexpected empty iterator state")
	}
}

func TestPostingsIteratorAll(t *testing.T) {
	seg := covSegment(t)
	snap := covSnapshot(covConfig(),
		&segmentSnapshot{segment: seg, deleted: roaring.BitmapOf(1)},
		&segmentSnapshot{segment: seg})
	itr, err := snap.postingsIteratorAll("t")
	if err != nil {
		t.Fatal(err)
	}
	all := itr.(*postingsIteratorAll)
	if all.Size() <= 0 || all.Count() != 7 || all.Empty() || all.Close() != nil {
		t.Fatal("unexpected iterator state")
	}
	p, err := all.Advance(1)
	if err != nil || p.Number() != 2 {
		t.Fatalf("advance: %v %v", p, err)
	}
	vp := p.(*virtualPosting)
	vp.SetNumber(9)
	if vp.Term() != "t" || vp.Number() != 9 || vp.Frequency() != 1 || vp.Norm() != 1 || vp.Locations() != nil || vp.Size() != 0 {
		t.Fatalf("unexpected posting %+v", vp)
	}
	if p, err := all.Advance(4 + math.MaxUint32 + 1); p != nil || err != nil {
		t.Fatalf("advance past uint32: %v %v", p, err)
	}
	snap.offsets = append(snap.offsets, 100)
	if _, err := all.Advance(150); err == nil {
		t.Fatal("expected out of bounds error")
	}
	empty, err := covSnapshot(covConfig()).postingsIteratorAll("t")
	if err != nil || !empty.Empty() {
		t.Fatalf("expected empty, got %v", err)
	}
}
