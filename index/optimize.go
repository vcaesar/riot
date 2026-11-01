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
	"math"
	"slices"
	"sync/atomic"

	"github.com/RoaringBitmap/roaring/v2"
	segment "github.com/vcaesar/bluge_segment_api"
)

func (i *postingsIterator) Optimize(kind string,
	octx segment.OptimizableContext) (segment.OptimizableContext, error) {
	if i.snapshot.parent.config.OptimizeConjunction && kind == "conjunction" {
		return i.optimizeConjunction(octx)
	}

	if i.snapshot.parent.config.OptimizeConjunction && (kind == optimizeExclusionInclude || kind == optimizeExclusionExclude) {
		return i.optimizeExclusion(kind == optimizeExclusionExclude, octx)
	}

	if i.snapshot.parent.config.OptimizeConjunctionUnadorned && kind == "conjunction:unadorned" {
		return i.optimizeConjunctionUnadorned(octx)
	}

	if i.snapshot.parent.config.OptimizeDisjunctionUnadorned && kind == "disjunction:unadorned" {
		return i.optimizeDisjunctionUnadorned(octx)
	}

	return nil, nil
}

// ----------------------------------------------------------------

func (i *postingsIterator) optimizeConjunction(
	octx segment.OptimizableContext) (segment.OptimizableContext, error) {
	if octx == nil {
		octx = &optimizeConjunction{snapshot: i.snapshot}
	}

	o, ok := octx.(*optimizeConjunction)
	if !ok {
		return octx, nil
	}

	if o.snapshot != i.snapshot {
		return nil, fmt.Errorf("tried to optimize conjunction across different snapshots")
	}

	o.tfrs = append(o.tfrs, i)

	return o, nil
}

type optimizeConjunction struct {
	snapshot *Snapshot

	tfrs []*postingsIterator
}

func (o *optimizeConjunction) Finish() (segment.PostingsIterator, error) {
	if len(o.tfrs) <= 1 {
		return nil, nil
	}

	for i := range o.snapshot.segment {
		itr0, ok := o.tfrs[0].iterators[i].(segment.OptimizablePostingsIterator)
		if !ok || itr0.ActualBitmap() == nil {
			continue
		}

		itr1, ok := o.tfrs[1].iterators[i].(segment.OptimizablePostingsIterator)
		if !ok || itr1.ActualBitmap() == nil {
			continue
		}

		bm := roaring.And(itr0.ActualBitmap(), itr1.ActualBitmap())

		for _, tfr := range o.tfrs[2:] {
			itr, ok := tfr.iterators[i].(segment.OptimizablePostingsIterator)
			if !ok || itr.ActualBitmap() == nil {
				continue
			}

			bm.And(itr.ActualBitmap())
		}

		// in this conjunction optimization, the postings iterators
		// will all share the same AND'ed together actual bitmap.  The
		// regular conjunction searcher machinery will still be used,
		// but the underlying bitmap will be smaller.
		for _, tfr := range o.tfrs {
			itr, ok := tfr.iterators[i].(segment.OptimizablePostingsIterator)
			if ok && itr.ActualBitmap() != nil {
				itr.ReplaceActual(bm)
			}
		}
	}

	return nil, nil
}

// ----------------------------------------------------------------

// Optimize kinds of the exclusion optimization: the conjunction's terms
// register as includes, the must-not terms as excludes.
const (
	optimizeExclusionInclude = "exclusion:include"
	optimizeExclusionExclude = "exclusion:exclude"
)

// optimizeExclusion collects the include (a conjunction's terms) and
// exclude (must-not terms) iterators of an exclusion optimization.
func (i *postingsIterator) optimizeExclusion(exclude bool,
	octx segment.OptimizableContext) (segment.OptimizableContext, error) {
	if octx == nil {
		octx = &optimizeExclusion{snapshot: i.snapshot}
	}
	o, ok := octx.(*optimizeExclusion)
	if !ok {
		return nil, nil
	}
	if o.snapshot != i.snapshot {
		return nil, fmt.Errorf("tried to optimize exclusion across different snapshots")
	}
	if exclude {
		o.excludes = append(o.excludes, i)
	} else {
		o.includes = append(o.includes, i)
	}
	return o, nil
}

// optimizeExclusion removes the documents of the excludes from the actual
// bitmap the includes iterate, like Lucene's ReqExclBulkScorer masks
// excluded documents before the required scorer sees them. The includes
// keep their freq/norm and block bounds, and share one bitmap per segment,
// so a block-max conjunction still counts skipped windows exactly.
type optimizeExclusion struct {
	snapshot *Snapshot
	includes []*postingsIterator
	excludes []*postingsIterator
	excluded bool
}

// Excluded reports whether Finish removed the excludes from the includes.
func (o *optimizeExclusion) Excluded() bool { return o.excluded }

// segmentDocs classifies one segment's postings: a bitmap, a single doc
// (1-hit), or neither for no docs; ok is false when they cannot be read.
func segmentDocs(itr segment.PostingsIterator) (bm *roaring.Bitmap, doc uint64, oneHit, ok bool) {
	if itr.Empty() {
		return nil, 0, false, true
	}
	o, ok := itr.(segment.OptimizablePostingsIterator)
	if !ok {
		return nil, 0, false, false
	}
	if doc, oneHit = o.DocNum1Hit(); oneHit {
		return nil, doc, true, doc <= math.MaxUint32
	}
	return o.ActualBitmap(), 0, false, true
}

// includedDocs ANDs the includes' documents of segment seg into bm, which
// owned marks as allocated here, and returns in hit the doc that 1-hit
// includes pin it to; empty means the segment matches nothing.
func (o *optimizeExclusion) includedDocs(seg int) (bm *roaring.Bitmap, hit []uint32, owned, empty, ok bool) {
	for _, tfr := range o.includes {
		b, d, oneHit, ok := segmentDocs(tfr.iterators[seg])
		switch {
		case !ok:
			return nil, nil, false, false, false
		case oneHit:
			if len(hit) > 0 && uint64(hit[0]) != d {
				return nil, nil, false, true, true
			}
			hit = []uint32{uint32(d)} // #nosec G115 -- segmentDocs checked d <= MaxUint32
		case b == nil:
			return nil, nil, false, true, true
		case bm == nil:
			bm = b
		case bm == b: // the conjunction optimization already shares one
		case owned:
			bm.And(b)
		default:
			bm, owned = roaring.And(bm, b), true
		}
	}
	if len(hit) > 0 && bm != nil && !bm.Contains(hit[0]) {
		return nil, nil, false, true, true
	}
	return bm, hit, owned, bm != nil && bm.IsEmpty(), true
}

// excludedDocs appends the excludes' bitmaps and 1-hit docs of segment seg.
func (o *optimizeExclusion) excludedDocs(seg int, bms []*roaring.Bitmap, docs []uint32) (
	[]*roaring.Bitmap, []uint32, bool) {
	for _, tfr := range o.excludes {
		b, d, oneHit, ok := segmentDocs(tfr.iterators[seg])
		switch {
		case !ok:
			return nil, nil, false
		case oneHit:
			docs = append(docs, uint32(d)) // #nosec G115 -- segmentDocs checked d <= MaxUint32
		case b != nil:
			bms = append(bms, b)
		}
	}
	return bms, docs, true
}

func excludesDoc(doc uint32, bms []*roaring.Bitmap, docs []uint32) bool {
	return slices.Contains(docs, doc) || slices.ContainsFunc(bms, func(b *roaring.Bitmap) bool { return b.Contains(doc) })
}

// andNotAll removes the excluded docs from bm, copying it once unless
// owned; nil means none of them were in bm.
func andNotAll(bm *roaring.Bitmap, owned bool, bms []*roaring.Bitmap, docs []uint32) *roaring.Bitmap {
	changed := false
	for _, e := range bms {
		switch {
		case !bm.Intersects(e):
			continue
		case owned:
			bm.AndNot(e)
		default:
			bm, owned = roaring.AndNot(bm, e), true
		}
		changed = true
	}
	for _, d := range docs {
		if !bm.Contains(d) {
			continue
		}
		if !owned {
			bm, owned = bm.Clone(), true
		}
		bm.Remove(d)
		changed = true
	}
	if !changed {
		return nil
	}
	return bm
}

// Finish applies the exclusion to every segment, or to none when one
// segment cannot take it; Excluded reports which.
func (o *optimizeExclusion) Finish() (segment.PostingsIterator, error) {
	if len(o.includes) == 0 || len(o.excludes) == 0 {
		return nil, nil
	}
	replaced := make([]*roaring.Bitmap, len(o.snapshot.segment))
	var exclBMs []*roaring.Bitmap
	var exclDocs []uint32
	for seg := range o.snapshot.segment {
		bm, hit, owned, empty, ok := o.includedDocs(seg)
		if !ok {
			return nil, nil
		}
		if empty {
			continue
		}
		if exclBMs, exclDocs, ok = o.excludedDocs(seg, exclBMs[:0], exclDocs[:0]); !ok {
			return nil, nil
		}
		if len(hit) == 0 {
			replaced[seg] = andNotAll(bm, owned, exclBMs, exclDocs)
			continue
		}
		if !excludesDoc(hit[0], exclBMs, exclDocs) {
			continue
		}
		if bm == nil {
			// only 1-hit includes, whose single doc cannot be dropped
			return nil, nil
		}
		// a bitmap include iterating nothing empties the conjunction
		replaced[seg] = roaring.New()
	}
	for seg, bm := range replaced {
		if bm == nil {
			continue
		}
		for _, tfr := range o.includes {
			if b, _, _, _ := segmentDocs(tfr.iterators[seg]); b != nil {
				tfr.replaceActual(seg, bm)
			}
		}
	}
	o.excluded = true
	return nil, nil
}

// ----------------------------------------------------------------

// An "unadorned" conjunction optimization is appropriate when
// additional or subsidiary information like freq-norm's and
// term-vectors are not required, and instead only the internal-id's
// are needed.
func (i *postingsIterator) optimizeConjunctionUnadorned(
	octx segment.OptimizableContext) (segment.OptimizableContext, error) {
	if octx == nil {
		octx = &optimizeConjunctionUnadorned{snapshot: i.snapshot}
	}

	o, ok := octx.(*optimizeConjunctionUnadorned)
	if !ok {
		return nil, nil
	}

	if o.snapshot != i.snapshot {
		return nil, fmt.Errorf("tried to optimize unadorned conjunction across different snapshots")
	}

	o.tfrs = append(o.tfrs, i)

	return o, nil
}

type optimizeConjunctionUnadorned struct {
	snapshot *Snapshot

	tfrs []*postingsIterator
}

var optimizeConjunctionUnadornedTerm = []byte("<conjunction:unadorned>")

// Finish of an unadorned conjunction optimization will compute a
// termFieldReader with an "actual" bitmap that represents the
// constituent bitmaps AND'ed together.  This termFieldReader cannot
// provide any freq-norm or termVector associated information.
func (o *optimizeConjunctionUnadorned) Finish() (rv segment.PostingsIterator, err error) {
	if len(o.tfrs) <= 1 {
		return nil, nil
	}

	// We use an artificial term and field because the optimized
	// termFieldReader can represent multiple terms and fields.
	oTFR := o.snapshot.unadornedPostingsIterator(optimizeConjunctionUnadornedTerm)

	var actualBMs []*roaring.Bitmap // Collected from regular posting lists.

OUTER:
	for i := range o.snapshot.segment {
		actualBMs = actualBMs[:0]

		var docNum1HitLast uint64
		var docNum1HitLastOk bool

		for _, tfr := range o.tfrs {
			if tfr.iterators[i].Empty() {
				// An empty postings iterator means the entire AND is empty.
				oTFR.iterators[i] = anEmptyPostingsIterator
				continue OUTER
			}

			itr, ok := tfr.iterators[i].(segment.OptimizablePostingsIterator)
			if !ok {
				// We only optimize postings iterators that support this operation.
				return nil, nil
			}

			// If the postings iterator is "1-hit" optimized, then we
			// can perform several optimizations up-front here.
			docNum1Hit, ok := itr.DocNum1Hit()
			if ok {
				if docNum1HitLastOk && docNum1HitLast != docNum1Hit {
					// The docNum1Hit doesn't match the previous
					// docNum1HitLast, so the entire AND is empty.
					oTFR.iterators[i] = anEmptyPostingsIterator
					continue OUTER
				}

				docNum1HitLast = docNum1Hit
				docNum1HitLastOk = true

				continue
			}

			if itr.ActualBitmap() == nil {
				// An empty actual bitmap means the entire AND is empty.
				oTFR.iterators[i] = anEmptyPostingsIterator
				continue OUTER
			}

			// Collect the actual bitmap for more processing later.
			actualBMs = append(actualBMs, itr.ActualBitmap())
		}

		if docNum1HitLastOk {
			// We reach here if all the 1-hit optimized posting
			// iterators had the same 1-hit docNum, so we can check if
			// our collected actual bitmaps also have that docNum.
			for _, bm := range actualBMs {
				if !bm.Contains(uint32(docNum1HitLast)) {
					// The docNum1Hit isn't in one of our actual
					// bitmaps, so the entire AND is empty.
					oTFR.iterators[i] = anEmptyPostingsIterator
					continue OUTER
				}
			}

			// The actual bitmaps and docNum1Hits all contain or have
			// the same 1-hit docNum, so that's our AND'ed result.
			oTFR.iterators[i] = newUnadornedPostingsIteratorFrom1Hit(docNum1HitLast)

			continue OUTER
		}

		if len(actualBMs) == 0 {
			// If we've collected no actual bitmaps at this point,
			// then the entire AND is empty.
			oTFR.iterators[i] = anEmptyPostingsIterator
			continue OUTER
		}

		if len(actualBMs) == 1 {
			// If we've only 1 actual bitmap, then that's our result.
			oTFR.iterators[i] = newUnadornedPostingsIteratorFromBitmap(actualBMs[0])

			continue OUTER
		}

		// Else, AND together our collected bitmaps as our result.
		bm := roaring.And(actualBMs[0], actualBMs[1])

		for _, actualBM := range actualBMs[2:] {
			bm.And(actualBM)
		}

		oTFR.iterators[i] = newUnadornedPostingsIteratorFromBitmap(bm)
	}

	atomic.AddUint64(&o.snapshot.parent.stats.TotTermSearchersStarted, uint64(1))
	return oTFR, nil
}

// ----------------------------------------------------------------

// An "unadorned" disjunction optimization is appropriate when
// additional or subsidiary information like freq-norm's and
// term-vectors are not required, and instead only the internal-id's
// are needed.
func (i *postingsIterator) optimizeDisjunctionUnadorned(
	octx segment.OptimizableContext) (segment.OptimizableContext, error) {
	if octx == nil {
		octx = &optimizeDisjunctionUnadorned{snapshot: i.snapshot}
	}

	o, ok := octx.(*optimizeDisjunctionUnadorned)
	if !ok {
		return nil, nil
	}

	if o.snapshot != i.snapshot {
		return nil, fmt.Errorf("tried to optimize unadorned disjunction across different snapshots")
	}

	o.tfrs = append(o.tfrs, i)

	return o, nil
}

type optimizeDisjunctionUnadorned struct {
	snapshot *Snapshot

	tfrs []*postingsIterator
}

var optimizeDisjunctionUnadornedTerm = []byte("<disjunction:unadorned>")

const preferHeapOr = 2

// Finish of an unadorned disjunction optimization will compute a
// termFieldReader with an "actual" bitmap that represents the
// constituent bitmaps OR'ed together.  This termFieldReader cannot
// provide any freq-norm or termVector associated information.
func (o *optimizeDisjunctionUnadorned) Finish() (rv segment.PostingsIterator, err error) {
	if len(o.tfrs) <= 1 {
		return nil, nil
	}

	// We use an artificial term and field because the optimized
	// termFieldReader can represent multiple terms and fields.
	oTFR := o.snapshot.unadornedPostingsIterator(optimizeDisjunctionUnadornedTerm)

	var docNums []uint32            // Collected docNum's from 1-hit posting lists.
	var actualBMs []*roaring.Bitmap // Collected from regular posting lists.

	for i := range o.snapshot.segment {
		docNums = docNums[:0]
		actualBMs = actualBMs[:0]

		for _, tfr := range o.tfrs {
			itr, ok := tfr.iterators[i].(segment.OptimizablePostingsIterator)
			if !ok {
				return nil, nil
			}

			docNum, ok := itr.DocNum1Hit()
			if ok {
				if docNum > math.MaxUint32 {
					return nil, nil
				}
				docNums = append(docNums, uint32(docNum))
				continue
			}

			if itr.ActualBitmap() != nil {
				actualBMs = append(actualBMs, itr.ActualBitmap())
			}
		}

		var bm *roaring.Bitmap
		if len(actualBMs) > preferHeapOr {
			// FastOr accumulates lazily into one bitmap; HeapOr
			// materializes an intermediate bitmap per pairwise OR
			bm = roaring.FastOr(actualBMs...)
		} else if len(actualBMs) == preferHeapOr {
			bm = roaring.Or(actualBMs[0], actualBMs[1])
		} else if len(actualBMs) == 1 {
			bm = actualBMs[0].Clone()
		}

		if bm == nil {
			bm = roaring.New()
		}

		bm.AddMany(docNums)

		oTFR.iterators[i] = newUnadornedPostingsIteratorFromBitmap(bm)
	}

	atomic.AddUint64(&o.snapshot.parent.stats.TotTermSearchersStarted, uint64(1))
	return oTFR, nil
}

// DocsMatchingTerms returns an unadorned iterator, without freq, norm or
// locations, over the live documents containing any of terms in field. The
// postings come straight from each segment dictionary and are ORed once
// per segment. It returns nil when a segment cannot expose its postings
// bitmaps, so the caller must fall back.
func (i *Snapshot) DocsMatchingTerms(field string, terms [][]byte) (segment.PostingsIterator, error) {
	if !i.parent.config.OptimizeDisjunctionUnadorned {
		return nil, nil
	}
	rv := i.unadornedPostingsIterator(optimizeDisjunctionUnadornedTerm)
	var docNums []uint32
	var bms []*roaring.Bitmap
	for j, seg := range i.segment {
		dict, err := seg.segment.Dictionary(field)
		if err != nil {
			return nil, err
		}
		docNums, bms = docNums[:0], bms[:0]
		for _, term := range terms {
			pl, err := dict.PostingsList(term, nil, nil)
			if err != nil {
				return nil, err
			}
			it, err := pl.Iterator(false, false, false, nil)
			if err != nil {
				return nil, err
			}
			o, ok := it.(segment.OptimizablePostingsIterator)
			if !ok {
				return nil, nil
			}
			if docNum, ok := o.DocNum1Hit(); ok {
				if docNum > math.MaxUint32 {
					return nil, nil
				}
				docNums = append(docNums, uint32(docNum))
			} else if bm := o.ActualBitmap(); bm != nil {
				bms = append(bms, bm)
			}
		}
		bm := orBitmaps(bms, seg.segment.Count())
		bm.AddMany(docNums)
		if seg.deleted != nil {
			bm.AndNot(seg.deleted)
		}
		rv.iterators[j] = newUnadornedPostingsIteratorFromBitmap(bm)
	}
	atomic.AddUint64(&i.parent.stats.TotTermSearchersStarted, 1)
	return rv, nil
}

// orBitmaps ORs the postings bitmaps of one segment holding numDocs
// documents. Many small array containers make roaring's unions quadratic,
// so more than two bitmaps are ORed into a dense word slice instead, which
// is linear in the postings plus numDocs/64.
func orBitmaps(bms []*roaring.Bitmap, numDocs uint64) *roaring.Bitmap {
	switch len(bms) {
	case 0:
		return roaring.New()
	case 1:
		return bms[0].Clone()
	case preferHeapOr:
		return roaring.Or(bms[0], bms[1])
	}
	dense := make([]uint64, (numDocs+63)/64)
	var buf []uint32
	for i, bm := range bms {
		if !bm.IsEmpty() && uint64(bm.Maximum()) >= numDocs {
			// postings past the segment size: not expected, stay correct
			return roaring.FastOr(bms...)
		}
		if i == 0 {
			// WriteDenseTo copies bitmap containers over dense rather than
			// ORing them, so it is only safe into the still-zero slice
			bm.WriteDenseTo(dense)
			continue
		}
		buf = orDense(dense, bm, buf)
	}
	return roaring.FromDense(dense, false)
}

// orDense sets the bits of bm in dense, using buf as scratch.
func orDense(dense []uint64, bm *roaring.Bitmap, buf []uint32) []uint32 {
	if buf == nil {
		buf = make([]uint32, 256)
	}
	it := bm.ManyIterator()
	for n := it.NextMany(buf); n > 0; n = it.NextMany(buf) {
		for _, x := range buf[:n] {
			dense[x>>6] |= 1 << (x & 63)
		}
	}
	return buf
}

// termsIterating is implemented by segment dictionaries that can list
// terms without reading their postings (ice.Dictionary).
type termsIterating interface {
	TermsIterator(a segment.Automaton, start, end []byte) segment.DictionaryIterator
}

// TermsIterator is DictionaryIterator for callers that only need terms:
// entry counts are 0 wherever a segment can skip reading postings.
func (i *Snapshot) TermsIterator(field string, automaton segment.Automaton, start, end []byte) (
	segment.DictionaryIterator, error) {
	return i.newDictionary(field, func(d segment.Dictionary) segment.DictionaryIterator {
		if t, ok := d.(termsIterating); ok {
			return t.TermsIterator(automaton, start, end)
		}
		return d.Iterator(automaton, start, end)
	}, false)
}

// densePostingsOr is implemented by segment dictionaries that can OR the
// postings of a term range into a bitset in one pass (ice.Dictionary).
type densePostingsOr interface {
	OrPostingsDense(a segment.Automaton, start, end []byte, dense []uint64) (bool, error)
}

// DocsMatchingRange is DocsMatchingTerms for every term in [start, end)
// that automaton accepts, ORed in one dictionary pass per segment. It
// returns nil when a segment cannot, so the caller must fall back.
func (i *Snapshot) DocsMatchingRange(field string, automaton segment.Automaton, start, end []byte) (
	segment.PostingsIterator, error) {
	if !i.parent.config.OptimizeDisjunctionUnadorned {
		return nil, nil
	}
	rv := i.unadornedPostingsIterator(optimizeDisjunctionUnadornedTerm)
	for j, seg := range i.segment {
		dict, err := seg.segment.Dictionary(field)
		if err != nil {
			return nil, err
		}
		or, ok := dict.(densePostingsOr)
		if !ok {
			return nil, nil
		}
		dense := make([]uint64, (seg.segment.Count()+63)/64)
		ok, err = or.OrPostingsDense(automaton, start, end, dense)
		if err != nil || !ok {
			return nil, err
		}
		bm := roaring.FromDense(dense, false)
		if seg.deleted != nil {
			bm.AndNot(seg.deleted)
		}
		rv.iterators[j] = newUnadornedPostingsIteratorFromBitmap(bm)
	}
	atomic.AddUint64(&i.parent.stats.TotTermSearchersStarted, 1)
	return rv, nil
}
