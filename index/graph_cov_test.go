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
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/ice/vec"

	"github.com/vcaesar/riot/hnsw"
)

// covGraphHeader is an item header of version for segment 1, count 5 and
// field v; crc false corrupts its checksum.
func covGraphHeader(version, fieldLen uint32, crc bool) []byte {
	le := binary.LittleEndian
	h := append([]byte(nil), graphMagic...)
	h = le.AppendUint32(h, version)
	h = le.AppendUint64(h, 1)
	h = le.AppendUint64(h, 5)
	h = le.AppendUint32(h, fieldLen)
	h = append(h, "v"...)
	sum := crc32.ChecksumIEEE(h)
	if !crc {
		sum++
	}
	return le.AppendUint32(h, sum)
}

func TestReadGraphHeaderErrors(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"short", []byte("RIOT")},
		{"magic", append([]byte("NOTHNSW!"), make([]byte, graphHeaderFixed)...)},
		{"version", covGraphHeader(9, 1, true)},
		{"field length", covGraphHeader(graphVersion, maxGraphField+1, true)},
		{"short field", covGraphHeader(graphVersion, 50, true)},
		{"checksum", covGraphHeader(graphVersion, 1, false)},
		{"mapped padding", covGraphHeader(graphVersionMapped, 1, true)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := readGraphHeader(bytes.NewReader(tt.data)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	item, version, err := readGraphHeader(bytes.NewReader(covGraphHeader(graphVersion, 1, true)))
	if err != nil || version != graphVersion || !item.matches(1, 5, "v") {
		t.Fatalf("header %+v %d %v", item, version, err)
	}
}

func covGraph(t *testing.T, key graphKey) *hnsw.Graph {
	t.Helper()
	g, err := buildVectorGraph(context.Background(), testVectorSegment(t), key)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestGraphItemErrors(t *testing.T) {
	if _, err := (graphItem{field: "v"}).WriteTo(covFailWriter{}, nil); err == nil {
		t.Fatal("expected header write error")
	}
	if err := checkGraphIDs(covGraph(t, testGraphKey), 3); err == nil {
		t.Fatal("expected out of range document")
	}
	// a mapped graph read from a file that went away
	f, err := os.CreateTemp(t.TempDir(), "graph")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(make([]byte, 16)); err != nil {
		t.Fatal(err)
	}
	data, err := segment.NewDataFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	lr := &io.LimitedReader{N: 8}
	if _, err := readGraph(context.Background(), data, lr, graphVersionMapped, testVectorSegment(t), testGraphKey, false); err == nil {
		t.Fatal("expected read error")
	}
	// version 2 reads the segment's vectors
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readGraph(ctx, data, lr, graphVersion, testVectorSegment(t), testGraphKey, false); err == nil {
		t.Fatal("expected vectors error")
	}
}

func TestLoadGraphBranches(t *testing.T) {
	seg := testVectorSegment(t)
	t.Run("segment id out of range", func(t *testing.T) {
		gw := newGraphTestWriter(t, t.TempDir(), false)
		if g, m, persist := gw.loadGraph(context.Background(), seg, 1<<(64-graphKeyBits), testGraphKey); g != nil || m != nil || persist {
			t.Fatal("expected nothing")
		}
	})
	t.Run("load error", func(t *testing.T) {
		gw := newGraphTestWriter(t, t.TempDir(), false)
		gw.directory = &covFaultDir{Directory: gw.directory, load: covFailKind(ItemKindGraph)}
		if g, _, persist := gw.loadGraph(context.Background(), seg, 1, testGraphKey); g != nil || persist {
			t.Fatal("expected nothing")
		}
		if len(gw.asyncErrors()) != 1 {
			t.Fatalf("errors %v", gw.asyncErrors())
		}
	})
	t.Run("other parameters", func(t *testing.T) {
		gw := newGraphTestWriter(t, t.TempDir(), false)
		other := testGraphKey
		other.m = 8
		id, _ := graphItemID(1, testGraphKey)
		item := graphItem{segmentID: 1, count: seg.Count(), field: "v", graph: covGraph(t, other)}
		if err := gw.directory.Persist(ItemKindGraph, id, item, nil); err != nil {
			t.Fatal(err)
		}
		if g, _, persist := gw.loadGraph(context.Background(), seg, 1, testGraphKey); g != nil || persist {
			t.Fatal("expected a mismatch")
		}
	})
}

type covCloser struct{ err error }

func (c covCloser) Close() error { return c.err }

func TestGraphItemHousekeeping(t *testing.T) {
	gw := newGraphTestWriter(t, t.TempDir(), true)
	if gw.discardGraph(1, errCov) {
		t.Fatal("read-only writer replaced a graph")
	}
	gw.readOnly = false
	gw.directory = &covFaultDir{Directory: gw.directory, remove: covFailKind(ItemKindGraph)}
	if gw.discardGraph(1, errCov) {
		t.Fatal("replaced a graph it could not remove")
	}
	gw.closeGraphItem(1, nil)
	gw.closeGraphItem(1, covCloser{errCov})
	if n := len(gw.asyncErrors()); n != 3 {
		t.Fatalf("errors %v", gw.asyncErrors())
	}

	// persists that cannot be addressed are dropped, failures reported
	long := testGraphKey
	long.field = strings.Repeat("f", maxGraphField+1)
	gw.persistGraph(1<<(64-graphKeyBits), 5, testGraphKey, nil)
	gw.persistGraph(1, 5, long, nil)
	gw.directory = &covFaultDir{Directory: gw.directory, persist: covFailKind(ItemKindGraph)}
	gw.persistGraph(1, 5, testGraphKey, covGraph(t, testGraphKey))
	gw.graphPersists.Wait()
	if n := len(gw.asyncErrors()); n != 4 {
		t.Fatalf("errors %v", gw.asyncErrors())
	}

	w := &segmentWrapper{id: 1}
	w.keepGraphMapping(covCloser{errCov})
	if err := w.closeGraphMappings(); err == nil {
		t.Fatal("expected mapping close error")
	}
}

func TestRemoveOrphanGraphsErrors(t *testing.T) {
	path := t.TempDir()
	dir := NewFileSystemDirectory(path)
	if err := os.WriteFile(filepath.Join(path, dir.fileName(ItemKindGraph, 9<<graphKeyBits)), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		fault func(*covFaultDir)
	}{
		{"list segments", func(d *covFaultDir) { d.list = func(k string) error { return covFailKind(k)(ItemKindSegment, 0) } }},
		{"remove", func(d *covFaultDir) { d.remove = covFailKind(ItemKindGraph) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &covFaultDir{Directory: dir}
			tt.fault(d)
			if err := (&Writer{directory: d}).removeOrphanGraphs(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestPersistedGraphKeys(t *testing.T) {
	path := t.TempDir()
	gw := newGraphTestWriter(t, path, false)
	dir := gw.directory.(*FileSystemDirectory)
	graph := covGraph(t, testGraphKey)
	for _, seg := range []uint64{1, 2} {
		id, _ := graphItemID(seg, testGraphKey)
		if err := dir.Persist(ItemKindGraph, id, graphItem{segmentID: seg, count: 5, field: "v", graph: graph}, nil); err != nil {
			t.Fatal(err)
		}
	}
	// an item under the id of another segment, one cut after its header
	// and one not an item at all
	id, _ := graphItemID(1, testGraphKey)
	var item bytes.Buffer
	if _, err := (graphItem{segmentID: 1, count: 5, field: "v", graph: graph}).WriteTo(&item, nil); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[uint64][]byte{
		7<<graphKeyBits | 1:                      item.Bytes(),
		8<<graphKeyBits | 2:                      covGraphHeader(graphVersion, 1, true),
		9<<graphKeyBits | 3:                      []byte("junk"),
		id&(1<<graphKeyBits-1) | 5<<graphKeyBits: item.Bytes(),
	} {
		if err := os.WriteFile(filepath.Join(path, dir.fileName(ItemKindGraph, name)), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if keys := gw.persistedGraphKeys(); len(keys) != 1 || keys[0] != testGraphKey {
		t.Fatalf("keys %v", keys)
	}
	gw.directory = &covFaultDir{Directory: dir, closeErr: covFailKind(ItemKindGraph)}
	if keys := gw.persistedGraphKeys(); len(keys) != 1 {
		t.Fatalf("keys %v", keys)
	}
	gw.directory = &covFaultDir{Directory: dir, list: func(string) error { return errCov }}
	if keys := gw.persistedGraphKeys(); keys != nil {
		t.Fatalf("keys %v", keys)
	}
	// without a root there is nothing to warm
	gw.warmPersistedGraphs()
	if len(gw.asyncErrors()) == 0 {
		t.Fatal("errors not reported")
	}
}

// covFailingRefs fails DecRef.
type covFailingRefs struct{}

func (covFailingRefs) AddRef()       {}
func (covFailingRefs) DecRef() error { return errCov }

func TestWarmGraphsErrors(t *testing.T) {
	gw := newGraphTestWriter(t, t.TempDir(), false)
	src := &segmentWrapper{Segment: testVectorSegment(t), refCounter: covFailingRefs{}}
	seg := &segmentWrapper{Segment: testVectorSegment(t), refCounter: covFailingRefs{},
		mergeSources: []mergeSource{{seg: src}}}
	// no keys: the merge sources are released at once
	gw.warmGraphs(seg, nil)
	if n := len(gw.asyncErrors()); n != 1 {
		t.Fatalf("errors %v", gw.asyncErrors())
	}

	bad := &segmentWrapper{Segment: &storedVectorSegment{vectors: [][]byte{{1, 2, 3}}}, refCounter: covFailingRefs{}}
	gw.root = &Snapshot{parent: gw.Writer, refs: 1, segment: []*segmentSnapshot{{segment: bad}}, offsets: []uint64{0}}
	gw.noteGraphKey(testGraphKey)
	gw.warmGraphs(bad, nil)
	gw.graphPersists.Wait()
	// the build and the release fail
	if n := len(gw.asyncErrors()); n != 3 {
		t.Fatalf("errors %v", gw.asyncErrors())
	}
}

// covVisitErrSegment fails VisitStoredFields.
type covVisitErrSegment struct{ storedVectorSegment }

func (s *covVisitErrSegment) VisitStoredFields(uint64, segment.StoredFieldVisitor) error {
	return errCov
}

func TestSegmentVectorsErrors(t *testing.T) {
	ctx := context.Background()
	zero := &storedVectorSegment{vectors: encodedVectors(t, []float32{0, 0})}
	if _, _, err := segmentVectors(ctx, zero, "v", vec.Cosine); err == nil {
		t.Fatal("expected invalid cosine vector")
	}
	failing := &covVisitErrSegment{storedVectorSegment{vectors: encodedVectors(t, []float32{1, 0})}}
	if _, _, err := segmentVectors(ctx, failing, "v", vec.L2); err == nil {
		t.Fatal("expected visit error")
	}
	// a waiter gives up on a graph another caller is building
	w := &segmentWrapper{Segment: testVectorSegment(t), graphs: map[graphKey]*graphEntry{
		testGraphKey: {ready: make(chan struct{})},
	}}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := w.vectorGraph(canceled, testGraphKey); err == nil {
		t.Fatal("expected canceled wait")
	}
}

// covCancelSegment cancels the search it serves.
type covCancelSegment struct {
	segment.Segment
	cancel context.CancelFunc
}

func (s *covCancelSegment) SearchVectors(context.Context, string, []float32, int, vec.Metric,
	func(uint64) bool) ([]vec.Match, error) {
	s.cancel()
	return nil, nil
}

func TestSearchVectorsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	seg := covSegment(t)
	snap := covSnapshot(covConfig(), &segmentSnapshot{segment: covWrap(&covCancelSegment{Segment: seg.Segment, cancel: cancel})})
	if _, err := snap.SearchVectors(ctx, "v", []float32{1, 0}, 1, vec.L2, nil); err == nil {
		t.Fatal("expected canceled search")
	}

	two := covSnapshot(covConfig(), &segmentSnapshot{segment: seg}, &segmentSnapshot{segment: seg})
	if _, err := two.searchSegments(ctx, nil, false, nil); err == nil {
		t.Fatal("expected canceled segments")
	}
	var accepted []uint64
	_, err := two.searchSegments(context.Background(), func(n uint64) bool {
		accepted = append(accepted, n)
		return true
	}, true, func(ss *segmentSnapshot, accept func(uint64) bool) ([]vec.Match, error) {
		if ss != two.segment[0] {
			return nil, nil
		}
		accept(1)
		return nil, nil
	})
	if err != nil || len(accepted) != 1 || accepted[0] != 1 {
		t.Fatalf("accepted %v, %v", accepted, err)
	}
}
