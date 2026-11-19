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
	"io"
	"slices"
	"strconv"
	"testing"

	"github.com/RoaringBitmap/roaring/v2"
	segment "github.com/vcaesar/bluge_segment_api"
	iceV1 "github.com/vcaesar/ice"
)

var errCov = errors.New("injected failure")

func covNorm(string, int) float32 { return 1 }

// covNumField is a doc value field holding a single int64, which ice keeps
// in a numeric column.
type covNumField struct {
	*FakeField
	v int64
}

func (f *covNumField) NumericValue() (int64, bool) { return f.v, true }

type covDocument []segment.Field

func (d covDocument) Analyze()         {}
func (d covDocument) Timestamp() int64 { return 0 }
func (d covDocument) EachField(vf segment.VisitField) {
	for _, f := range d {
		vf(f)
	}
}

// covDoc returns a document with _id id, text field f and numeric field n.
func covDoc(id, text string, num int64) segment.Document {
	return covDocument{
		NewFakeField("_id", id, true, false, false),
		NewFakeField("f", text, true, true, true),
		&covNumField{FakeField: NewFakeField("n", strconv.FormatInt(num, 10), false, false, true), v: num},
	}
}

// covDocs are the documents of covSegment: f holds a {0,1,3} b {0,2}
// c {2,3} and the 1-hit d {1}; n holds 10, 20, 30, 40.
func covDocs() []segment.Document {
	return []segment.Document{
		covDoc("0", "a b", 10),
		covDoc("1", "a d", 20),
		covDoc("2", "b c", 30),
		covDoc("3", "a c", 40),
	}
}

func covConfig() Config {
	return InMemoryOnlyConfig().WithNormCalc(covNorm)
}

// covSegment builds docs into an in-memory ice segment.
func covSegment(t testing.TB, docs ...segment.Document) *segmentWrapper {
	t.Helper()
	if len(docs) == 0 {
		docs = covDocs()
	}
	seg, _, err := iceV1.DefaultOptions().New(docs, covNorm)
	if err != nil {
		t.Fatalf("building segment: %v", err)
	}
	return &segmentWrapper{Segment: seg, refCounter: noOpRefCounter{}}
}

func covWrap(seg segment.Segment) *segmentWrapper {
	return &segmentWrapper{Segment: seg, refCounter: noOpRefCounter{}}
}

// covSnapshot is a snapshot over segs, without a running writer.
func covSnapshot(cfg Config, segs ...*segmentSnapshot) *Snapshot {
	s := &Snapshot{parent: &Writer{config: cfg}, segment: segs, refs: 1}
	var running uint64
	for i, seg := range segs {
		seg.id = uint64(i + 1)
		s.offsets = append(s.offsets, running)
		running += seg.segment.Count()
	}
	return s
}

// covDrain returns the numbers of the postings of itr.
func covDrain(t testing.TB, itr segment.PostingsIterator) []uint64 {
	t.Helper()
	var rv []uint64
	for {
		p, err := itr.Next()
		if err != nil {
			t.Fatalf("next: %v", err)
		}
		if p == nil {
			return rv
		}
		rv = append(rv, p.Number())
	}
}

func covEqual(a, b []uint64) bool { return slices.Equal(a, b) }

// covWriter opens a writer on cfg and applies one batch per docs slice;
// the writer is closed when the test ends.
func covWriter(t testing.TB, cfg Config, batches ...[]segment.Document) *Writer {
	t.Helper()
	w, err := OpenWriter(cfg)
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Errorf("close writer: %v", err)
		}
	})
	for _, docs := range batches {
		b := NewBatch()
		for _, doc := range docs {
			b.Insert(doc)
		}
		if err := w.Batch(b); err != nil {
			t.Fatalf("batch: %v", err)
		}
	}
	return w
}

// covDict is a dictionary whose postings lists and dense OR are scripted.
type covDict struct {
	segment.Dictionary
	plErr  error
	itr    segment.PostingsIterator
	itrErr error
	orOK   bool
	orErr  error
}

func (d *covDict) PostingsList([]byte, *roaring.Bitmap, segment.PostingsList) (segment.PostingsList, error) {
	if d.plErr != nil {
		return nil, d.plErr
	}
	return &covPostingsList{d: d}, nil
}

func (d *covDict) OrPostingsDense(segment.Automaton, []byte, []byte, []uint64) (bool, error) {
	return d.orOK, d.orErr
}

type covPostingsList struct {
	segment.PostingsList
	d *covDict
}

func (p *covPostingsList) Iterator(bool, bool, bool, segment.PostingsIterator) (segment.PostingsIterator, error) {
	return p.d.itr, p.d.itrErr
}

func (p *covPostingsList) Count() uint64 { return 0 }
func (p *covPostingsList) Size() int     { return 0 }

// covDictSegment is a real segment whose Dictionary is replaced.
type covDictSegment struct {
	segment.Segment
	dict segment.Dictionary
	err  error
}

func (s *covDictSegment) Dictionary(string) (segment.Dictionary, error) { return s.dict, s.err }

// covFaultDir wraps a Directory, failing the operations whose hooks return
// an error.
type covFaultDir struct {
	Directory
	persist func(kind string, id uint64) error
	// afterPersist runs once an item persisted successfully
	afterPersist func(kind string, id uint64)
	load         func(kind string, id uint64) error
	// closeErr, when set, is returned by the closers of loaded items
	closeErr func(kind string, id uint64) error
	remove   func(kind string, id uint64) error
	list     func(kind string) error
	sync     func() error
	stats    func() (uint64, uint64)
	setup    error
	lock     error
	unlock   error
}

func (d *covFaultDir) Persist(kind string, id uint64, w WriterTo, closeCh chan struct{}) error {
	if d.persist != nil {
		if err := d.persist(kind, id); err != nil {
			return err
		}
	}
	err := d.Directory.Persist(kind, id, w, closeCh)
	if err == nil && d.afterPersist != nil {
		d.afterPersist(kind, id)
	}
	return err
}

func (d *covFaultDir) Load(kind string, id uint64) (*segment.Data, io.Closer, error) {
	if d.load != nil {
		if err := d.load(kind, id); err != nil {
			return nil, nil, err
		}
	}
	data, closer, err := d.Directory.Load(kind, id)
	if err != nil || d.closeErr == nil {
		return data, closer, err
	}
	return data, closerFunc(func() error {
		if closer != nil {
			_ = closer.Close()
		}
		return d.closeErr(kind, id)
	}), nil
}

func (d *covFaultDir) Remove(kind string, id uint64) error {
	if d.remove != nil {
		if err := d.remove(kind, id); err != nil {
			return err
		}
	}
	return d.Directory.Remove(kind, id)
}

func (d *covFaultDir) List(kind string) ([]uint64, error) {
	if d.list != nil {
		if err := d.list(kind); err != nil {
			return nil, err
		}
	}
	return d.Directory.List(kind)
}

func (d *covFaultDir) Sync() error {
	if d.sync != nil {
		if err := d.sync(); err != nil {
			return err
		}
	}
	return d.Directory.Sync()
}

func (d *covFaultDir) Stats() (numFilesOnDisk, numBytesUsedDisk uint64) {
	if d.stats != nil {
		return d.stats()
	}
	return d.Directory.Stats()
}

func (d *covFaultDir) Setup(readOnly bool) error {
	if d.setup != nil {
		return d.setup
	}
	return d.Directory.Setup(readOnly)
}

func (d *covFaultDir) Lock() error {
	if d.lock != nil {
		return d.lock
	}
	return d.Directory.Lock()
}

func (d *covFaultDir) Unlock() error {
	if d.unlock != nil {
		return d.unlock
	}
	return d.Directory.Unlock()
}

// covFailKind fails every operation on items of kind.
func covFailKind(kind string) func(string, uint64) error {
	return func(k string, _ uint64) error {
		if k == kind {
			return errCov
		}
		return nil
	}
}
