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
	"sync/atomic"

	"github.com/RoaringBitmap/roaring/v2"
	segment "github.com/vcaesar/bluge_segment_api"
)

// numericColumnRanger is implemented by segments that keep a typed numeric
// column (ice); ok is false when field has no column in the segment.
type numericColumnRanger interface {
	NumericRange(field string, lo, hi int64) (bm *roaring.Bitmap, ok bool, err error)
}

// NumericRange returns an unadorned iterator over the live documents with a
// value of field in [lo, hi]. Each segment answers from its numeric column
// when it has one, else by ORing the exact-value terms [loTerm, hiTermEnd)
// from its dictionary. It returns nil when a segment can do neither, so the
// caller must fall back.
func (i *Snapshot) NumericRange(field string, lo, hi int64, loTerm, hiTermEnd []byte) (
	segment.PostingsIterator, error) {
	rv := i.unadornedPostingsIterator(optimizeDisjunctionUnadornedTerm)
	for j, seg := range i.segment {
		var bm *roaring.Bitmap
		if nr, ok := seg.segment.Segment.(numericColumnRanger); ok {
			colBM, ok, err := nr.NumericRange(field, lo, hi)
			if err != nil {
				return nil, err
			}
			if ok {
				bm = colBM
			}
		}
		if bm == nil {
			dict, err := seg.segment.Dictionary(field)
			if err != nil {
				return nil, err
			}
			or, ok := dict.(densePostingsOr)
			if !ok {
				return nil, nil
			}
			dense := make([]uint64, (seg.segment.Count()+63)/64)
			ok, err = or.OrPostingsDense(nil, loTerm, hiTermEnd, dense)
			if err != nil || !ok {
				return nil, err
			}
			bm = roaring.FromDense(dense, false)
		}
		if seg.deleted != nil {
			bm.AndNot(seg.deleted)
		}
		rv.iterators[j] = newUnadornedPostingsIteratorFromBitmap(bm)
	}
	atomic.AddUint64(&i.parent.stats.TotTermSearchersStarted, 1)
	return rv, nil
}
