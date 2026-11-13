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
	"fmt"
	"sync/atomic"
	"time"

	segment "github.com/vcaesar/bluge_segment_api"
)

// DefaultStreamMemoryBudget is the Stream buffer size used for a
// non-positive budget. Building a segment takes about as much again, so
// a small budget bounds indexing memory with no loss of speed (16 MiB:
// 100K documents index in the same CPU time as 64 MiB, in a third of the
// memory).
const DefaultStreamMemoryBudget = 16 << 20

// DefaultBatchStreamMemoryBudget is the budget Writer.Batch uses in
// IndexModeStream when Config.StreamMemoryBudget is not positive. A Batch
// already holds all of its documents, so a larger budget costs it little,
// while it builds a 100K-document bulk load into one segment instead of
// several that then merge and slow every query until they do.
const DefaultBatchStreamMemoryBudget = 128 << 20

// streamAnalyzeDocs is how many documents a Stream analyzes at a time;
// the next part is analyzed while the current one is added.
const streamAnalyzeDocs = 2048

// Stream indexes documents incrementally with bounded memory, like
// riot-go's IndexWriter: each document is analyzed and packed into a flat
// buffer, so its analysis objects are freed right away, and the buffer is
// built into a segment once it reaches the memory budget. With a persistent
// directory each such segment is written to its own file and memory-mapped
// right away, as Tantivy's writer does, so it holds no heap and Commit
// publishes it without rewriting it. Commit publishes every segment, and
// the deletions, as one snapshot, with the semantics of a single Batch. A
// Stream is not safe for concurrent use.
type Stream struct {
	w         *Writer
	budget    int
	pending   []segment.Document // not yet analyzed
	analyzing []segment.Document // being analyzed; done reports it
	done      <-chan error
	builder   SegmentBuilder // when the plugin has one; else buf
	buf       *packedBuffer
	segments  []*segmentWrapper
	ids       []segment.Term
	updates   uint64
}

// NewStream returns a Stream over s that flushes at memoryBudget bytes of
// packed documents; a non-positive budget uses Config.StreamMemoryBudget,
// or DefaultStreamMemoryBudget when that is not positive either.
func (s *Writer) NewStream(memoryBudget int) *Stream {
	if memoryBudget <= 0 {
		memoryBudget = s.config.StreamMemoryBudget
	}
	if memoryBudget <= 0 {
		memoryBudget = DefaultStreamMemoryBudget
	}
	st := &Stream{w: s, budget: memoryBudget}
	st.resetBuffer()
	return st
}

// resetBuffer starts an empty segment buffer: the plugin's builder, which
// inverts documents as they are added, or a packed buffer for New.
func (st *Stream) resetBuffer() {
	if nb := st.w.segPlugin.NewBuilder; nb != nil {
		st.builder = nb(st.w.config.NormCalc)
		return
	}
	if st.buf == nil {
		st.buf = newPackedBuffer()
	} else {
		st.buf.reset()
	}
}

func (st *Stream) bufferAdd(doc segment.Document) error {
	if st.builder != nil {
		return st.builder.Add(doc)
	}
	return st.buf.add(doc)
}

func (st *Stream) bufferSize() int {
	if st.builder != nil {
		return st.builder.Size()
	}
	return st.buf.size()
}

func (st *Stream) bufferLen() int {
	if st.builder != nil {
		return st.builder.Len()
	}
	return int(st.buf.docs.n)
}

func (st *Stream) bufferBuild() (*segmentWrapper, uint64, error) {
	if st.builder == nil {
		return st.w.newSegment(st.buf.documents())
	}
	seg, n, err := st.builder.Build()
	return &segmentWrapper{Segment: seg, refCounter: noOpRefCounter{}}, n, err
}

// Insert adds doc; it is searchable after Commit. Nil documents are ignored.
// On error, everything added since the last Commit is discarded.
func (st *Stream) Insert(doc segment.Document) error {
	if doc == nil {
		return nil
	}
	st.pending = append(st.pending, doc)
	if len(st.pending) < streamAnalyzeDocs {
		return nil
	}
	return st.pack()
}

// Update deletes the committed documents with id at Commit and inserts doc;
// a nil id (a document without _id) only inserts doc.
func (st *Stream) Update(id segment.Term, doc segment.Document) error {
	if id != nil {
		st.ids = append(st.ids, id)
	}
	return st.Insert(doc)
}

// Delete deletes the committed documents with id at Commit; a nil id deletes
// nothing.
func (st *Stream) Delete(id segment.Term) {
	if id != nil {
		st.ids = append(st.ids, id)
	}
}

// pack starts analyzing the pending documents and meanwhile moves the
// documents analyzed by the previous pack into the buffer, so analysis on
// the workers overlaps the single-threaded adding; on error the
// uncommitted work is discarded.
func (st *Stream) pack() (err error) {
	defer func() {
		if err != nil {
			st.discard(true)
		}
	}()
	if err := st.wait(); err != nil {
		return err
	}
	prev := st.analyzing
	st.analyzing, st.pending = st.pending, prev
	if len(st.analyzing) > 0 {
		st.done = st.w.analyzeAsync(st.analyzing)
	}
	// the budget is checked per document so a buffer overshoots it by at
	// most one document
	for _, doc := range prev {
		if err := st.bufferAdd(doc); err != nil {
			return err
		}
		if st.bufferSize() >= st.budget {
			if err := st.flush(); err != nil {
				return err
			}
		}
	}
	// the caller may still hold the documents (a Batch does)
	releaseAnalysis(prev)
	clear(prev)
	st.updates += uint64(len(prev))
	st.pending = prev[:0]
	return nil
}

// wait waits for the documents being analyzed, if any.
func (st *Stream) wait() error {
	if st.done == nil {
		return nil
	}
	err := <-st.done
	st.done = nil
	return err
}

// flush builds the buffered documents into an unpublished segment.
func (st *Stream) flush() error {
	if st.bufferLen() == 0 {
		return nil
	}
	start := time.Now()
	seg, bufBytes, err := st.bufferBuild()
	if err != nil {
		return err
	}
	atomic.AddUint64(&st.w.stats.newSegBufBytesAdded, bufBytes)
	atomic.AddUint64(&st.w.stats.newSegBufBytesRemoved, bufBytes)
	st.resetBuffer()
	if !isVolatile(st.w.directory) {
		if seg, err = st.w.persistFlushed(seg); err != nil {
			return err
		}
	}
	//nolint:gosec // G115: elapsed time uses the monotonic clock from time.Now and is nonnegative.
	atomic.AddUint64(&st.w.stats.TotIndexTime, uint64(time.Since(start)))
	st.segments = append(st.segments, seg)
	return nil
}

// persistFlushed writes seg to a new segment file and returns it reopened
// from the file, memory-mapped; seg is closed either way.
func (s *Writer) persistFlushed(seg *segmentWrapper) (*segmentWrapper, error) {
	id := atomic.AddUint64(&s.nextSegmentID, 1)
	err := s.directory.Persist(ItemKindSegment, id, seg.Segment, s.closeCh)
	err = errors.Join(err, seg.Close())
	if err != nil {
		return nil, errors.Join(err, s.directory.Remove(ItemKindSegment, id))
	}
	loaded, err := s.loadSegment(id, s.segPlugin)
	if err != nil {
		return nil, s.removeUnloadable(id, err)
	}
	return loaded, nil
}

// Commit publishes everything added since the last Commit. On error the
// uncommitted work is discarded.
func (st *Stream) Commit() error {
	return st.commit(nil, false)
}

// commit publishes the stream; always introduces even an empty commit, as
// Batch does, and persistedCallback is the Batch's.
func (st *Stream) commit(persistedCallback func(error), always bool) (err error) {
	// once applied the root owns the segments, so they must not be closed
	// nor their files removed; a persist error leaves them searchable and
	// the persister retries them with the next snapshot
	applied := false
	defer func() {
		if err != nil {
			if applied {
				st.segments = nil
			}
			st.discard(true)
		}
	}()
	for len(st.pending) > 0 || len(st.analyzing) > 0 {
		if err := st.pack(); err != nil {
			return err
		}
	}
	if err := st.flush(); err != nil {
		return err
	}
	if !always && len(st.segments) == 0 && len(st.ids) == 0 {
		return nil
	}
	w := st.w
	start := time.Now()
	defer func() {
		w.fireEvent(EventKindBatchIntroduction, time.Since(start))
	}()
	w.fireEvent(EventKindBatchIntroductionStart, 0)
	if applied, err = w.prepareSegment(st.segments, st.ids, nil, persistedCallback); err != nil {
		atomic.AddUint64(&w.stats.TotOnErrors, 1)
		return err
	}
	if len(st.segments) == 0 {
		atomic.AddUint64(&w.stats.TotBatchesEmpty, 1)
	}
	atomic.AddUint64(&w.stats.TotUpdates, st.updates)
	atomic.AddUint64(&w.stats.TotDeletes, uint64(len(st.ids)))
	atomic.AddUint64(&w.stats.TotBatches, 1)
	st.segments, st.ids, st.updates = nil, nil, 0
	return nil
}

// Discard drops everything added since the last Commit, and the files of
// its flushed segments.
func (st *Stream) Discard() {
	st.discard(true)
}

func (st *Stream) discard(removeFiles bool) {
	// the analysis must not outlive the stream's hold on its documents
	errs := []error{st.wait()}
	for _, seg := range st.segments {
		errs = append(errs, seg.Close())
		if removeFiles && seg.persisted {
			errs = append(errs, st.w.directory.Remove(ItemKindSegment, seg.id))
		}
	}
	if err := errors.Join(errs...); err != nil {
		st.w.fireAsyncError(fmt.Errorf("discarding stream segments: %w", err))
	}
	clear(st.pending)
	clear(st.analyzing)
	st.pending, st.analyzing = st.pending[:0], st.analyzing[:0]
	st.segments, st.ids, st.updates = nil, nil, 0
	st.buf = nil
	st.resetBuffer()
}
