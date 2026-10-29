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
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/vcaesar/ice/vec"

	"github.com/vcaesar/riot/hnsw"
)

var testGraphKey = graphKey{field: "v", metric: vec.L2, m: hnsw.DefaultM, efConstruction: hnsw.DefaultEfConstruction}

type graphTestWriter struct {
	*Writer
	path string
	mu   sync.Mutex
	errs []error
}

func newGraphTestWriter(t *testing.T, path string, readOnly bool) *graphTestWriter {
	t.Helper()
	dir := NewFileSystemDirectory(path)
	if err := dir.Setup(readOnly); err != nil {
		t.Fatal(err)
	}
	gw := &graphTestWriter{path: path}
	gw.Writer = &Writer{directory: dir, closeCh: make(chan struct{}), readOnly: readOnly}
	// Test wrappers have no reference counts to release mappings with;
	// segment_graph_mapped_test.go covers VectorStorageDisk.
	gw.config.VectorStorage = VectorStorageMemory
	t.Cleanup(gw.graphPersists.Wait)
	gw.config.AsyncError = func(err error) {
		gw.mu.Lock()
		gw.errs = append(gw.errs, err)
		gw.mu.Unlock()
	}
	return gw
}

func (gw *graphTestWriter) asyncErrors() []error {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	return gw.errs
}

func (gw *graphTestWriter) graphFile(t *testing.T, segmentID uint64) string {
	t.Helper()
	id, ok := graphItemID(segmentID, testGraphKey)
	if !ok {
		t.Fatal("segment id out of range")
	}
	return filepath.Join(gw.path, gw.directory.(*FileSystemDirectory).fileName(ItemKindGraph, id))
}

func testVectorSegment(t *testing.T) *storedVectorSegment {
	return &storedVectorSegment{vectors: encodedVectors(t,
		[]float32{0, 0}, []float32{1, 0}, []float32{0, 1}, []float32{3, 3}, []float32{-2, 1})}
}

func searchGraph(t *testing.T, w *segmentWrapper) []vec.Match {
	t.Helper()
	graph, err := w.vectorGraph(context.Background(), testGraphKey)
	if err != nil {
		t.Fatal(err)
	}
	matches, err := graph.Search([]float32{1, 1}, 3, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if w.writer != nil {
		w.writer.graphPersists.Wait()
	}
	return matches
}

// blockingDirectory holds graph persists until release is closed.
type blockingDirectory struct {
	Directory
	started, release chan struct{}
}

func (d *blockingDirectory) Persist(kind string, id uint64, w WriterTo, closeCh chan struct{}) error {
	if kind == ItemKindGraph {
		close(d.started)
		<-d.release
	}
	return d.Directory.Persist(kind, id, w, closeCh)
}

// TestSegmentVectorGraphPersistAsync checks that search does not wait for the
// graph write, while Writer.close does.
func TestSegmentVectorGraphPersistAsync(t *testing.T) {
	gw := newGraphTestWriter(t, t.TempDir(), false)
	if err := gw.directory.Lock(); err != nil {
		t.Fatal(err)
	}
	path := gw.graphFile(t, 8)
	dir := &blockingDirectory{Directory: gw.directory, started: make(chan struct{}), release: make(chan struct{})}
	gw.directory = dir
	w := &segmentWrapper{Segment: testVectorSegment(t), id: 8, writer: gw.Writer}
	if _, err := w.vectorGraph(context.Background(), testGraphKey); err != nil {
		t.Fatal(err)
	}
	<-dir.started

	closed := make(chan error, 1)
	go func() { closed <- gw.close() }()
	select {
	case err := <-closed:
		t.Fatalf("close returned before the graph persist finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(dir.release)
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("graph started before close must be persisted: %v", err)
	}
	if errs := gw.asyncErrors(); len(errs) != 0 {
		t.Fatalf("unexpected async errors: %v", errs)
	}
}

func TestSegmentVectorGraphPersisted(t *testing.T) {
	gw := newGraphTestWriter(t, t.TempDir(), false)
	seg := testVectorSegment(t)
	want := searchGraph(t, &segmentWrapper{Segment: seg, id: 7, writer: gw.Writer})
	if seg.visits.Load() == 0 {
		t.Fatal("first use must build from stored vectors")
	}
	if _, err := os.Stat(gw.graphFile(t, 7)); err != nil {
		t.Fatalf("graph not persisted: %v", err)
	}

	if graph, _, _ := gw.loadGraph(context.Background(), seg, 7, testGraphKey); graph == nil {
		t.Fatal("persisted graph must load instead of rebuilding")
	}
	seg.visits.Store(0)
	got := searchGraph(t, &segmentWrapper{Segment: seg, id: 7, writer: gw.Writer})
	if seg.visits.Load() > int64(len(seg.vectors)) {
		t.Fatalf("loading read stored fields %d times, want one pass", seg.visits.Load())
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded graph search %v, want %v", got, want)
	}

	// A segment without a writer, or under another key, never touches the item.
	before, err := os.ReadFile(gw.graphFile(t, 7))
	if err != nil {
		t.Fatal(err)
	}
	searchGraph(t, &segmentWrapper{Segment: seg, id: 7})
	other := &segmentWrapper{Segment: seg, id: 7, writer: gw.Writer}
	if _, err := other.vectorGraph(context.Background(), graphKey{field: "v", metric: vec.DotProduct, m: 4, efConstruction: 8}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(gw.graphFile(t, 7))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("graph item was rewritten")
	}
	if errs := gw.asyncErrors(); len(errs) != 0 {
		t.Fatalf("unexpected async errors: %v", errs)
	}
}

// fullGraphItem is a version 1 item, which embeds ids and vectors.
type fullGraphItem graphItem

func (g fullGraphItem) WriteTo(w io.Writer, _ chan struct{}) (int64, error) {
	var header bytes.Buffer
	if _, err := graphItem(g).WriteTo(&header, nil); err != nil {
		return 0, err
	}
	fixed := header.Bytes()[:graphHeaderFixed+len(g.field)]
	binary.LittleEndian.PutUint32(fixed[len(graphMagic):], graphVersionFull)
	fixed = binary.LittleEndian.AppendUint32(fixed, crc32.ChecksumIEEE(fixed))
	n, err := w.Write(fixed)
	if err != nil {
		return int64(n), err
	}
	m, err := g.graph.WriteTo(w)
	return int64(n) + m, err
}

func TestSegmentVectorGraphLoadsFullItem(t *testing.T) {
	gw := newGraphTestWriter(t, t.TempDir(), false)
	seg := testVectorSegment(t)
	graph, err := buildVectorGraph(context.Background(), seg, testGraphKey)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := graphItemID(9, testGraphKey)
	full := fullGraphItem{segmentID: 9, count: seg.Count(), field: "v", graph: graph}
	if err := gw.directory.Persist(ItemKindGraph, id, full, nil); err != nil {
		t.Fatal(err)
	}
	seg.visits.Store(0)
	loaded, _, _ := gw.loadGraph(context.Background(), seg, 9, testGraphKey)
	if loaded == nil || loaded.Len() != graph.Len() || seg.visits.Load() != 0 {
		t.Fatalf("version 1 item must load without stored fields: %v, %d visits", loaded, seg.visits.Load())
	}

	var v1, v2 bytes.Buffer
	if _, err := full.WriteTo(&v1, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := (graphItem(full)).WriteTo(&v2, nil); err != nil {
		t.Fatal(err)
	}
	if v2.Len() >= v1.Len() {
		t.Fatalf("links-only item %d bytes, full %d", v2.Len(), v1.Len())
	}
}

func TestSegmentVectorGraphMismatchRebuilds(t *testing.T) {
	gw := newGraphTestWriter(t, t.TempDir(), false)
	searchGraph(t, &segmentWrapper{Segment: testVectorSegment(t), id: 3, writer: gw.Writer})
	before, err := os.ReadFile(gw.graphFile(t, 3))
	if err != nil {
		t.Fatal(err)
	}
	// Same id, different document count: the stored graph is not this segment's.
	seg := &storedVectorSegment{vectors: encodedVectors(t, []float32{1, 1}, []float32{2, 2})}
	got := searchGraph(t, &segmentWrapper{Segment: seg, id: 3, writer: gw.Writer})
	if seg.visits.Load() == 0 || len(got) != 2 {
		t.Fatalf("mismatched graph must be rebuilt, got %v", got)
	}
	after, err := os.ReadFile(gw.graphFile(t, 3))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("a valid graph of another key must not be overwritten")
	}
}

func TestSegmentVectorGraphCorruptReplaced(t *testing.T) {
	for i, corrupt := range []func([]byte) []byte{
		func(b []byte) []byte { return b[:len(b)/2] },
		func(b []byte) []byte { b[len(b)-1] ^= 1; return b },
		func(b []byte) []byte { b[len(graphMagic)+5] ^= 1; return b },
		func(b []byte) []byte { return append(b, 0) },
	} {
		gw := newGraphTestWriter(t, t.TempDir(), false)
		seg := testVectorSegment(t)
		want := searchGraph(t, &segmentWrapper{Segment: seg, id: 1, writer: gw.Writer})
		path := gw.graphFile(t, 1)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, corrupt(bytes.Clone(data)), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := searchGraph(t, &segmentWrapper{Segment: seg, id: 1, writer: gw.Writer}); !reflect.DeepEqual(got, want) {
			t.Fatalf("rebuilt search %v, want %v", got, want)
		}
		repaired, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(repaired, data) {
			t.Fatalf("corrupt graph %d must be replaced: len %d want %d", i, len(repaired), len(data))
		}
		if errs := gw.asyncErrors(); len(errs) != 0 {
			t.Fatalf("unexpected async errors: %v", errs)
		}
	}
}

func TestSegmentVectorGraphReadOnly(t *testing.T) {
	path := t.TempDir()
	ro := newGraphTestWriter(t, path, true)
	seg := testVectorSegment(t)
	want := searchGraph(t, &segmentWrapper{Segment: seg, id: 2, writer: ro.Writer})
	if _, err := os.Stat(ro.graphFile(t, 2)); !os.IsNotExist(err) {
		t.Fatalf("read-only reader must not persist, stat: %v", err)
	}

	searchGraph(t, &segmentWrapper{Segment: seg, id: 2, writer: newGraphTestWriter(t, path, false).Writer})
	if graph, _, _ := ro.loadGraph(context.Background(), seg, 2, testGraphKey); graph == nil {
		t.Fatal("read-only reader must load persisted graphs")
	}
	if got := searchGraph(t, &segmentWrapper{Segment: seg, id: 2, writer: ro.Writer}); !reflect.DeepEqual(got, want) {
		t.Fatalf("read-only load %v, want %v", got, want)
	}
}

func TestSegmentVectorGraphSkipsClosedWriter(t *testing.T) {
	gw := newGraphTestWriter(t, t.TempDir(), false)
	close(gw.closeCh)
	searchGraph(t, &segmentWrapper{Segment: testVectorSegment(t), id: 4, writer: gw.Writer})
	if _, err := os.Stat(gw.graphFile(t, 4)); !os.IsNotExist(err) {
		t.Fatalf("closed writer must not persist, stat: %v", err)
	}
}

type bytesWriterTo []byte

func (b bytesWriterTo) WriteTo(w io.Writer, _ chan struct{}) (int64, error) {
	n, err := w.Write(b)
	return int64(n), err
}

func TestRemoveOrphanGraphs(t *testing.T) {
	gw := newGraphTestWriter(t, t.TempDir(), false)
	if err := gw.removeOrphanGraphs(); err != nil {
		t.Fatal(err)
	}
	if err := gw.directory.Persist(ItemKindSegment, 5, bytesWriterTo("seg"), nil); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint64{5, 6} {
		searchGraph(t, &segmentWrapper{Segment: testVectorSegment(t), id: id, writer: gw.Writer})
	}
	if err := gw.removeOrphanGraphs(); err != nil {
		t.Fatal(err)
	}
	ids, err := gw.directory.List(ItemKindGraph)
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := graphItemID(5, testGraphKey); len(ids) != 1 || ids[0] != want {
		t.Fatalf("remaining graphs %x, want only segment 5's %x", ids, want)
	}
}

func TestGraphItemID(t *testing.T) {
	a, ok := graphItemID(9, testGraphKey)
	if !ok || a>>graphKeyBits != 9 {
		t.Fatalf("id %x does not encode segment 9", a)
	}
	if b, _ := graphItemID(9, graphKey{field: "w", metric: vec.L2, m: 16, efConstruction: 200}); b == a {
		t.Fatal("different keys should hash apart")
	}
	if _, ok := graphItemID(1<<(64-graphKeyBits), testGraphKey); ok {
		t.Fatal("segment ids beyond the packed range must not persist")
	}
}

func TestSegmentVectorGraphInMemoryDirectory(t *testing.T) {
	w := &Writer{directory: NewInMemoryDirectory(), closeCh: make(chan struct{})}
	seg := testVectorSegment(t)
	searchGraph(t, &segmentWrapper{Segment: seg, id: 1, writer: w})
	seg.visits.Store(0)
	searchGraph(t, &segmentWrapper{Segment: seg, id: 1, writer: w})
	if seg.visits.Load() == 0 {
		t.Fatal("in-memory directories keep no graphs, so each wrapper builds")
	}
	if err := w.removeOrphanGraphs(); err != nil {
		t.Fatal(err)
	}
}
