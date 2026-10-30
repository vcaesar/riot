//  Copyright (c) 2020 Couchbase, Inc.
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
	"fmt"
	"sync/atomic"

	"github.com/RoaringBitmap/roaring/v2"
	segment "github.com/vcaesar/bluge_segment_api"
)

type postingsIterator struct {
	term               []byte
	field              string
	snapshot           *Snapshot
	dicts              []segment.Dictionary
	postings           []segment.PostingsList
	iterators          []segment.PostingsIterator
	segmentOffset      int
	includeFreq        bool
	includeNorm        bool
	includeTermVectors bool
	currPosting        segment.Posting
	currID             uint64
	recycle            bool
	// batched is set once NextBatch has moved the iterators, standing in
	// for currPosting when Advance checks for a backwards seek
	batched bool
	// actuals holds, per segment, the bitmap an exclusion optimization
	// replaced the actual bitmap with (nil: untouched), so a backwards
	// seek, which rebuilds the iterators, cannot bring excluded docs back
	actuals []*roaring.Bitmap
}

// replaceActual makes segment seg iterate bm and remembers it for restarts.
func (i *postingsIterator) replaceActual(seg int, bm *roaring.Bitmap) {
	if i.actuals == nil {
		i.actuals = make([]*roaring.Bitmap, len(i.iterators))
	}
	i.actuals[seg] = bm
	i.iterators[seg].(segment.OptimizablePostingsIterator).ReplaceActual(bm)
}

func (i *postingsIterator) Size() int {
	sizeInBytes := reflectStaticSizeIndexSnapshotTermFieldReader + sizeOfPtr +
		len(i.term) +
		len(i.field) +
		sizeOfInt

	for _, entry := range i.postings {
		sizeInBytes += entry.Size()
	}

	for _, entry := range i.iterators {
		sizeInBytes += entry.Size()
	}

	if i.currPosting != nil {
		sizeInBytes += i.currPosting.Size()
	}

	return sizeInBytes
}

func (i *postingsIterator) Next() (segment.Posting, error) {
	// find the next hit
	for i.segmentOffset < len(i.iterators) {
		next, err := i.iterators[i.segmentOffset].Next()
		if err != nil {
			return nil, err
		}
		if next != nil {
			rvNumber := next.Number() + i.snapshot.offsets[i.segmentOffset]
			next.SetNumber(rvNumber)
			i.currID = rvNumber
			i.currPosting = next
			return next, nil
		}
		i.segmentOffset++
	}
	return nil, nil
}

func (i *postingsIterator) Advance(number uint64) (segment.Posting, error) {
	// FIXME do something better
	// for now, if we need to seek backwards, then restart from the beginning
	if (i.currPosting != nil || i.batched) && i.currID >= number {
		i2, err := i.snapshot.PostingsIterator(i.term, i.field,
			i.includeFreq, i.includeNorm, i.includeTermVectors)
		if err != nil {
			return nil, err
		}
		// adopt the fresh iterator's state and hand our old buffers to it
		// before recycling it; recycling i itself would leave an iterator
		// that is still in use sitting in the snapshot's free list
		i2p := i2.(*postingsIterator)
		*i, *i2p = *i2p, *i
		for seg, bm := range i2p.actuals {
			if bm != nil {
				i.replaceActual(seg, bm)
			}
		}
		i2p.actuals = nil
		if err := i2p.Close(); err != nil {
			return nil, err
		}
	}
	segIndex, ldocNum := i.snapshot.segmentIndexAndLocalDocNumFromGlobal(number)
	if segIndex >= len(i.snapshot.segment) {
		return nil, fmt.Errorf("computed segment index %d out of bounds %d",
			segIndex, len(i.snapshot.segment))
	}
	// skip directly to the target segment
	i.segmentOffset = segIndex
	next, err := i.iterators[i.segmentOffset].Advance(ldocNum)
	if err != nil {
		return nil, err
	}
	if next == nil {
		// we jumped directly to the segment that should have contained it
		// but it wasn't there, so reuse Next() which should correctly
		// get the next hit after it (we moved i.segmentOffset)
		return i.Next()
	}

	rvNumber := next.Number() + i.snapshot.offsets[i.segmentOffset]
	next.SetNumber(rvNumber)
	i.currID = rvNumber
	i.currPosting = next
	return next, nil
}

// postingsBatcher is implemented by segment postings iterators that can
// read many postings per call (ice.PostingsIterator).
type postingsBatcher interface {
	NextBatch(docs []uint64, freqs []int, norms []float64) (int, error)
}

// NextBatch reads up to len(docs) postings, numbered across the snapshot,
// into docs, freqs and norms; 0 means the end. Locations are not read.
func (i *postingsIterator) NextBatch(docs []uint64, freqs []int, norms []float64) (int, error) {
	for i.segmentOffset < len(i.iterators) {
		it := i.iterators[i.segmentOffset]
		n := 0
		if b, ok := it.(postingsBatcher); ok {
			var err error
			if n, err = b.NextBatch(docs, freqs, norms); err != nil {
				return 0, err
			}
		} else {
			for n < len(docs) {
				p, err := it.Next()
				if err != nil {
					return 0, err
				}
				if p == nil {
					break
				}
				docs[n], freqs[n], norms[n] = p.Number(), p.Frequency(), p.Norm()
				n++
			}
		}
		if n > 0 {
			off := i.snapshot.offsets[i.segmentOffset]
			for k := range n {
				docs[k] += off
			}
			i.currID, i.batched = docs[n-1], true
			return n, nil
		}
		i.segmentOffset++
	}
	return 0, nil
}

// blockSkipper is implemented by segment postings iterators that can skip
// postings blocks by their freq/norm bounds (ice.PostingsIterator).
type blockSkipper interface {
	SkipBlocks(competitive func(maxFreq int, minNorm float64) bool) (uint64, error)
}

// SkipBlocks skips the postings blocks of the current segment, from the
// next posting on, whose bounds competitive rejects, and returns how many
// postings it skipped.
func (i *postingsIterator) SkipBlocks(competitive func(maxFreq int, minNorm float64) bool) (uint64, error) {
	if i.segmentOffset >= len(i.iterators) {
		return 0, nil
	}
	if b, ok := i.iterators[i.segmentOffset].(blockSkipper); ok {
		return b.SkipBlocks(competitive)
	}
	return 0, nil
}

// blockBounder is implemented by segment postings iterators that expose
// the freq/norm bounds of the block holding a document (ice.PostingsIterator).
type blockBounder interface {
	BlockBound(docNum uint64) (maxFreq int, minNorm float64, end uint64, ok bool)
}

// BlockBound returns the largest freq and smallest norm of the postings
// block holding the snapshot document number, and the first number past
// that block (clipped to its segment). ok is false when no bound is known.
func (i *postingsIterator) BlockBound(number uint64) (maxFreq int, minNorm float64, end uint64, ok bool) {
	segIndex, local := i.snapshot.segmentIndexAndLocalDocNumFromGlobal(number)
	if segIndex < 0 || segIndex >= len(i.iterators) {
		return 0, 0, 0, false
	}
	b, isBounder := i.iterators[segIndex].(blockBounder)
	if !isBounder {
		return 0, 0, 0, false
	}
	maxFreq, minNorm, end, ok = b.BlockBound(local)
	if !ok {
		return 0, 0, 0, false
	}
	off := i.snapshot.offsets[segIndex]
	end += off
	if segIndex+1 < len(i.snapshot.offsets) {
		end = min(end, i.snapshot.offsets[segIndex+1])
	}
	return maxFreq, minNorm, end, true
}

// SegmentBitmap returns the actual postings bitmap of the segment holding
// the snapshot document number, in segment-local numbers, and that
// segment's first snapshot number; nil when it has none.
func (i *postingsIterator) SegmentBitmap(number uint64) (bm *roaring.Bitmap, base uint64) {
	segIndex, _ := i.snapshot.segmentIndexAndLocalDocNumFromGlobal(number)
	if segIndex < 0 || segIndex >= len(i.iterators) {
		return nil, 0
	}
	o, ok := i.iterators[segIndex].(segment.OptimizablePostingsIterator)
	if !ok {
		return nil, 0
	}
	return o.ActualBitmap(), i.snapshot.offsets[segIndex]
}

func (i *postingsIterator) Count() uint64 {
	var rv uint64
	if len(i.postings) == 0 {
		// an unadorned (optimized) iterator has bitmaps but no postings lists
		for _, itr := range i.iterators {
			if itr != nil {
				rv += itr.Count()
			}
		}
		return rv
	}
	for _, posting := range i.postings {
		rv += posting.Count()
	}
	return rv
}

func (i *postingsIterator) Empty() bool {
	count := i.Count()
	return count == 0
}

func (i *postingsIterator) Close() error {
	if i.snapshot != nil {
		atomic.AddUint64(&i.snapshot.parent.stats.TotTermSearchersFinished, uint64(1))
		i.snapshot.recyclePostingsIterator(i)
	}
	return nil
}
