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
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/RoaringBitmap/roaring/v2"
	segment "github.com/vcaesar/bluge_segment_api"
)

var covLogger = log.New(io.Discard, "", 0)

// covPolicy keeps the latest snapshot and fails Cleanup while fail is set.
type covPolicy struct {
	*KeepNLatestDeletionPolicy
	fail *atomic.Bool
}

func (p *covPolicy) Cleanup(dir Directory) error {
	if p.fail.Load() {
		return errCov
	}
	return p.KeepNLatestDeletionPolicy.Cleanup(dir)
}

func covFailingPolicy(cfg Config, fail *atomic.Bool) Config {
	cfg.DeletionPolicyFunc = func() DeletionPolicy {
		return &covPolicy{KeepNLatestDeletionPolicy: NewKeepNLatestDeletionPolicy(1), fail: fail}
	}
	return cfg
}

// covReleaseDoc counts ReleaseAnalysis calls.
type covReleaseDoc struct {
	segment.Document
	released *atomic.Int64
}

func (d covReleaseDoc) ReleaseAnalysis() { d.released.Add(1) }

// covBuilder fails Add or Build as set, else counts the documents added.
type covBuilder struct {
	n               int
	addErr, buildEr error
}

func (b *covBuilder) Add(segment.Document) error {
	if b.addErr != nil {
		return b.addErr
	}
	b.n++
	return nil
}
func (b *covBuilder) Len() int                                { return b.n }
func (b *covBuilder) Size() int                               { return 0 }
func (b *covBuilder) Build() (segment.Segment, uint64, error) { return nil, 0, b.buildEr }

func covBuilderPlugin(cfg Config, addErr, buildErr error) Config {
	p := covIcePlugin()
	p.NewBuilder = func(func(string, int) float32) SegmentBuilder {
		return &covBuilder{addErr: addErr, buildEr: buildErr}
	}
	return covWithPlugin(cfg, p)
}

// covDocsErrSegment fails DocsMatchingTerms.
type covDocsErrSegment struct{ segment.Segment }

func (s *covDocsErrSegment) DocsMatchingTerms([]segment.Term) (*roaring.Bitmap, error) {
	return nil, errCov
}

func covDocsErrPlugin(cfg Config) Config {
	p := covIcePlugin()
	newSeg, load := p.New, p.Load
	p.New = func(docs []segment.Document, nc func(string, int) float32) (segment.Segment, uint64, error) {
		seg, n, err := newSeg(docs, nc)
		return &covDocsErrSegment{seg}, n, err
	}
	p.Load = func(d *segment.Data) (segment.Segment, error) {
		seg, err := load(d)
		return &covDocsErrSegment{seg}, err
	}
	cfg.MinSegmentsForInMemoryMerge = 100
	return covWithPlugin(cfg, p)
}

func covDocsN(n uint64) []segment.Document {
	docs := make([]segment.Document, n)
	for i := range docs {
		docs[i] = covDocument{NewFakeField("_id", strconv.Itoa(i), true, false, false)}
	}
	return docs
}

func covBatchOf(docs []segment.Document) *Batch {
	b := NewBatch()
	for _, d := range docs {
		b.Insert(d)
	}
	return b
}

func covCount(t *testing.T, w *Writer) uint64 {
	t.Helper()
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	n, err := r.Count()
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestOpenWriterErrors(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	tests := []struct {
		name string
		cfg  func(t *testing.T) Config
	}{
		{"segment type", func(*testing.T) Config { return covConfig().WithSegmentType("nope") }},
		{"setup", func(*testing.T) Config { return covMemDirConfig(func(d *covFaultDir) { d.setup = errCov }) }},
		{"lock", func(*testing.T) Config { return covMemDirConfig(func(d *covFaultDir) { d.lock = errCov }) }},
		{"list snapshots", func(*testing.T) Config {
			return covMemDirConfig(func(d *covFaultDir) {
				d.list = func(k string) error { return covFailKind(k)(ItemKindSnapshot, 0) }
			})
		}},
		{"list segments", func(*testing.T) Config {
			return covMemDirConfig(func(d *covFaultDir) {
				d.list = func(k string) error { return covFailKind(k)(ItemKindSegment, 0) }
			})
		}},
		{"cleanup", func(*testing.T) Config { return covFailingPolicy(covConfig(), &fail) }},
		{"unreadable snapshots", func(t *testing.T) Config {
			path := t.TempDir()
			if err := os.WriteFile(filepath.Join(path, "000000000001"+ItemKindSnapshot), []byte("junk"), 0600); err != nil {
				t.Fatal(err)
			}
			cfg := DefaultConfig(path).WithNormCalc(covNorm)
			cfg.Logger = covLogger
			return cfg
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, err := OpenWriter(tt.cfg(t))
			if err == nil {
				_ = w.Close()
				t.Fatal("expected error")
			}
		})
	}
}

func TestWriterAsyncCleanupErrors(t *testing.T) {
	var fail atomic.Bool
	var asyncErrs atomic.Int64
	cfg := covMemDirConfig(func(d *covFaultDir) {
		d.list = func(k string) error { return covFailKind(k)(ItemKindGraph, 0) }
	})
	cfg = covFailingPolicy(cfg, &fail)
	cfg.Logger = covLogger
	cfg.AsyncError = func(error) { asyncErrs.Add(1) }
	w, err := OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	// the persister cleans up after persisting the first batch, before it
	// takes the second
	for range 2 {
		if err := w.Batch(covBatchOf(covDocsN(2))); err != nil {
			t.Fatal(err)
		}
	}
	if n := covCount(t, w); n != 4 {
		t.Fatalf("count %d", n)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	// Close always cleans up, and reports the failure
	if asyncErrs.Load() == 0 || w.stats.TotOnErrors == 0 {
		t.Fatalf("async errors %d, on errors %d", asyncErrs.Load(), w.stats.TotOnErrors)
	}
}

func TestWriterCloseUnlockError(t *testing.T) {
	w, err := OpenWriter(covMemDirConfig(func(d *covFaultDir) { d.unlock = errCov }))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); !errors.Is(err, errCov) {
		t.Fatalf("got %v", err)
	}
}

func TestWriterCloseReleasesWaiters(t *testing.T) {
	ch := make(chan error, 1)
	var cbErr error
	w := &Writer{
		directory:          NewInMemoryDirectory(),
		closeCh:            make(chan struct{}),
		rootPersisted:      []chan error{ch},
		persistedCallbacks: []func(error){func(err error) { cbErr = err }},
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-ch; !errors.Is(err, segment.ErrClosed) {
		t.Fatalf("waiter got %v", err)
	}
	if !errors.Is(cbErr, segment.ErrClosed) {
		t.Fatalf("callback got %v", cbErr)
	}
}

func TestWriterBatchIndexMode(t *testing.T) {
	tests := []struct {
		name     string
		cfg      Config
		docs     uint64
		released int64
	}{
		{"builder, chunked", covConfig().WithBatchIndexing(2), 5, 5},
		{"builder, multi-part", covConfig().WithBatchIndexing(0), streamAnalyzeDocs + 3, streamAnalyzeDocs + 3},
		{"new, chunked", covConfig().WithSegmentPlugin(covIcePlugin()).WithBatchIndexing(2), 5, 5},
		{"new, whole", covConfig().WithSegmentPlugin(covIcePlugin()).WithBatchIndexing(0), 3, 0},
		{"empty", covConfig().WithBatchIndexing(0), 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var released atomic.Int64
			docs := covDocsN(tt.docs)
			for i := range docs {
				docs[i] = covReleaseDoc{Document: docs[i], released: &released}
			}
			w := covWriter(t, tt.cfg, docs)
			if n := covCount(t, w); n != tt.docs {
				t.Fatalf("count %d, want %d", n, tt.docs)
			}
			if released.Load() != tt.released {
				t.Fatalf("released %d, want %d", released.Load(), tt.released)
			}
			// delete one of them
			b := NewBatch()
			b.Delete(testIdentifier("0"))
			if err := w.Batch(b); err != nil {
				t.Fatal(err)
			}
			want := tt.docs
			if want > 0 {
				want--
			}
			if n := covCount(t, w); n != want {
				t.Fatalf("count after delete %d, want %d", n, want)
			}
		})
	}
}

func TestWriterBatchIndexModeErrors(t *testing.T) {
	var calls atomic.Int64
	failSecondNew := func(cfg Config) Config {
		p := covIcePlugin()
		newSeg := p.New
		p.New = func(docs []segment.Document, nc func(string, int) float32) (segment.Segment, uint64, error) {
			if calls.Add(1) == 2 {
				return nil, 0, errCov
			}
			return newSeg(docs, nc)
		}
		return covWithPlugin(cfg, p)
	}
	tests := []struct {
		name string
		cfg  Config
		docs uint64
	}{
		{"new fails", failSecondNew(covConfig()).WithBatchIndexing(1), 3},
		{"add fails", covBuilderPlugin(covConfig(), errCov, nil).WithBatchIndexing(0), streamAnalyzeDocs + 1},
		{"build fails", covBuilderPlugin(covConfig(), nil, errCov).WithBatchIndexing(0), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := covWriter(t, tt.cfg)
			if err := w.Batch(covBatchOf(covDocsN(tt.docs))); !errors.Is(err, errCov) {
				t.Fatalf("got %v", err)
			}
		})
	}
	t.Run("prepare fails", func(t *testing.T) {
		w := covWriter(t, covDocsErrPlugin(covConfig()).WithBatchIndexing(0), covDocsN(1))
		if err := w.Batch(covBatchOf(covDocsN(1))); !errors.Is(err, errCov) {
			t.Fatalf("got %v", err)
		}
		if w.stats.TotOnErrors == 0 {
			t.Fatal("error not counted")
		}
	})
}

func TestWriterBatchAfterClose(t *testing.T) {
	cfgs := map[string]Config{
		"builder": covConfig().WithBatchIndexing(0),
		"new":     covConfig().WithSegmentPlugin(covIcePlugin()).WithBatchIndexing(0),
		"stream":  covConfig(),
	}
	for name, cfg := range cfgs {
		t.Run(name, func(t *testing.T) {
			w, err := OpenWriter(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			for _, n := range []uint64{0, 1} {
				if err := w.Batch(covBatchOf(covDocsN(n))); !errors.Is(err, segment.ErrClosed) {
					t.Fatalf("%d docs: got %v", n, err)
				}
			}
			if w.MemoryUsed() != 0 {
				t.Fatal("closed writer uses memory")
			}
		})
	}
}

func TestWriterBatchStreamInsertError(t *testing.T) {
	w := covWriter(t, covBuilderPlugin(covConfig(), errCov, nil))
	if err := w.Batch(covBatchOf(covDocsN(2 * streamAnalyzeDocs))); !errors.Is(err, errCov) {
		t.Fatalf("got %v", err)
	}
}

func TestWriterMemoryUsed(t *testing.T) {
	w := covWriter(t, covConfig(), covDocsN(3), covDocsN(2))
	if w.MemoryUsed() == 0 {
		t.Fatal("no memory used")
	}
	tests := []struct {
		persistEpoch, mergeEpoch uint64
		want                     uint64
	}{
		{0, 0, 10},
		{5, 5, 10},
		{4, 0, 1010},
		{0, 4, 2010},
		{4, 4, 3010},
	}
	for _, tt := range tests {
		m := &Writer{root: &Snapshot{epoch: 5, size: 10, refs: 1}}
		m.stats.persistEpoch, m.stats.persistSnapshotSize = tt.persistEpoch, 1000
		m.stats.mergeEpoch, m.stats.mergeSnapshotSize = tt.mergeEpoch, 2000
		if got := m.MemoryUsed(); got != tt.want {
			t.Errorf("epochs %d/%d: got %d, want %d", tt.persistEpoch, tt.mergeEpoch, got, tt.want)
		}
	}
}

func TestPrepareSegmentErrors(t *testing.T) {
	newWriter := func() *Writer {
		w := &Writer{config: covConfig(), directory: NewInMemoryDirectory(), closeCh: make(chan struct{})}
		w.root = covSnapshot(w.config, &segmentSnapshot{segment: covSegment(t)})
		w.root.parent = w
		return w
	}
	ids := []segment.Term{testIdentifier("1")}
	t.Run("docs matching terms", func(t *testing.T) {
		w := newWriter()
		w.root.segment[0].segment = covWrap(&covDocsErrSegment{w.root.segment[0].segment.Segment})
		if _, err := w.prepareSegment(nil, ids, nil); !errors.Is(err, errCov) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("closed", func(t *testing.T) {
		w := newWriter()
		close(w.closeCh)
		if _, err := w.prepareSegment(nil, ids, nil); !errors.Is(err, segment.ErrClosed) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("not applied", func(t *testing.T) {
		w := newWriter()
		w.introductions = make(chan *segmentIntroduction)
		go func() {
			intro := <-w.introductions
			intro.applied <- errCov
		}()
		if applied, err := w.prepareSegment(nil, ids, nil); applied || !errors.Is(err, errCov) {
			t.Fatalf("got %v %v", applied, err)
		}
	})
}

// covPersisted writes docs into a file system index at a fresh path and
// returns its config, with the ids of its snapshot and segments.
func covPersisted(t *testing.T) (cfg Config, path string) {
	path = t.TempDir()
	cfg = DefaultConfig(path).WithNormCalc(covNorm)
	cfg.Logger = covLogger
	w, err := OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Batch(covBatchOf(covDocs())); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return cfg, path
}

func covWithDir(cfg Config, fault func(*covFaultDir)) Config {
	df := cfg.DirectoryFunc
	cfg.DirectoryFunc = func() Directory {
		d := &covFaultDir{Directory: df()}
		fault(d)
		return d
	}
	return cfg
}

func TestOpenReaderErrors(t *testing.T) {
	cfg, path := covPersisted(t)
	snaps, err := NewFileSystemDirectory(path).List(ItemKindSnapshot)
	if err != nil || len(snaps) != 1 {
		t.Fatalf("snapshots %v %v", snaps, err)
	}
	foreign := cfg
	foreign.supportedSegmentPlugins = map[string]map[uint32]*SegmentPlugin{}
	foreign = foreign.WithSegmentPlugin(&SegmentPlugin{Type: "cov", Version: 1}).WithSegmentType("cov").WithSegmentVersion(1)
	tests := []struct {
		name string
		cfg  Config
	}{
		{"segment type", cfg.WithSegmentType("nope")},
		{"missing directory", DefaultConfig(filepath.Join(path, "missing"))},
		{"list", covWithDir(cfg, func(d *covFaultDir) { d.list = func(string) error { return errCov } })},
		{"load snapshot", covWithDir(cfg, func(d *covFaultDir) { d.load = covFailKind(ItemKindSnapshot) })},
		{"close snapshot", covWithDir(cfg, func(d *covFaultDir) { d.closeErr = covFailKind(ItemKindSnapshot) })},
		{"load segment", covWithDir(cfg, func(d *covFaultDir) { d.load = covFailKind(ItemKindSegment) })},
		{"segment plugin load", covFailingLoad(cfg)},
		{"no segment plugin", foreign},
		{"no snapshots", DefaultConfig(t.TempDir())},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cfg.Logger = covLogger
			r, err := OpenReader(tt.cfg)
			if err == nil {
				_ = r.Close()
				t.Fatal("expected error")
			}
		})
	}
	t.Run("skips a bad newer snapshot", func(t *testing.T) {
		newer := filepath.Join(path, NewFileSystemDirectory(path).fileName(ItemKindSnapshot, snaps[0]+1))
		if err := os.WriteFile(newer, []byte("junk"), 0600); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Remove(newer) }()
		r, err := OpenReader(cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = r.Close() }()
		if n, _ := r.Count(); n != 4 {
			t.Fatalf("count %d", n)
		}
	})
}

func TestLoadSnapshotDeletedOutsideSegment(t *testing.T) {
	cfg, path := covPersisted(t)
	dir := NewFileSystemDirectory(path)
	segs, err := dir.List(ItemKindSegment)
	if err != nil || len(segs) != 1 {
		t.Fatalf("segments %v %v", segs, err)
	}
	snaps, err := dir.List(ItemKindSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	seg := covSegment(t)
	bad := &Snapshot{epoch: snaps[0] + 1, segment: []*segmentSnapshot{{
		id: segs[0], segment: seg, deleted: roaring.BitmapOf(100),
	}}}
	if err := dir.Persist(ItemKindSnapshot, bad.epoch, bad, nil); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]Config{
		"plain":         cfg,
		"segment close": covWithDir(cfg, func(d *covFaultDir) { d.closeErr = covFailKind(ItemKindSegment) }),
	} {
		t.Run(name, func(t *testing.T) {
			w := &Writer{config: c, directory: c.DirectoryFunc()}
			if _, err := w.loadSnapshot(bad.epoch); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
