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
	"context"
	"encoding/binary"
	"os"
	"reflect"
	"testing"
)

// mappedWrapper is a persisted segment whose last reference releases its
// graph mappings, as loadSegment wires it.
func mappedWrapper(seg *storedVectorSegment, id uint64, w *Writer) *segmentWrapper {
	rv := &segmentWrapper{Segment: seg, id: id, writer: w}
	rv.refCounter = &closeOnLastRefCounter{closer: closerFunc(rv.closeGraphMappings), refs: 1}
	return rv
}

func mappings(w *segmentWrapper) int {
	w.graphMu.Lock()
	defer w.graphMu.Unlock()
	return len(w.graphMappings)
}

func itemVersion(t *testing.T, path string) uint32 {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return binary.LittleEndian.Uint32(b[len(graphMagic):])
}

func TestSegmentGraphDiskStorage(t *testing.T) {
	gw := newGraphTestWriter(t, t.TempDir(), false)
	gw.config.VectorStorage = VectorStorageDisk
	seg := testVectorSegment(t)
	first := mappedWrapper(seg, 7, gw.Writer)
	want := searchGraph(t, first)
	if err := first.DecRef(); err != nil {
		t.Fatal(err)
	}
	if v := itemVersion(t, gw.graphFile(t, 7)); v != graphVersionMapped {
		t.Fatalf("disk storage persisted version %d", v)
	}

	// Reopened, the graph is mapped in place without reading stored fields.
	seg.visits.Store(0)
	second := mappedWrapper(seg, 7, gw.Writer)
	if got := searchGraph(t, second); !reflect.DeepEqual(got, want) {
		t.Fatalf("mapped graph search %v, want %v", got, want)
	}
	if seg.visits.Load() != 0 || mappings(second) != 1 {
		t.Fatalf("%d stored-field visits, %d mappings kept", seg.visits.Load(), mappings(second))
	}
	if err := second.DecRef(); err != nil {
		t.Fatal(err)
	}
	if mappings(second) != 0 {
		t.Fatal("the last reference must release the mapping")
	}

	// Memory storage reads the same item onto the heap.
	gw.config.VectorStorage = VectorStorageMemory
	heap := mappedWrapper(seg, 7, gw.Writer)
	if got := searchGraph(t, heap); !reflect.DeepEqual(got, want) || mappings(heap) != 0 || seg.visits.Load() != 0 {
		t.Fatalf("memory storage: %v, %d mappings, %d visits", got, mappings(heap), seg.visits.Load())
	}
	if errs := gw.asyncErrors(); len(errs) != 0 {
		t.Fatalf("unexpected async errors: %v", errs)
	}
}

// Items persisted under the other storage stay readable.
func TestSegmentGraphDiskStorageReadsLinksItem(t *testing.T) {
	gw := newGraphTestWriter(t, t.TempDir(), false)
	seg := testVectorSegment(t)
	want := searchGraph(t, &segmentWrapper{Segment: seg, id: 3, writer: gw.Writer})
	if v := itemVersion(t, gw.graphFile(t, 3)); v != graphVersion {
		t.Fatalf("memory storage persisted version %d", v)
	}
	gw.config.VectorStorage = VectorStorageDisk
	seg.visits.Store(0)
	w := mappedWrapper(seg, 3, gw.Writer)
	if got := searchGraph(t, w); !reflect.DeepEqual(got, want) || mappings(w) != 0 || seg.visits.Load() == 0 {
		t.Fatalf("links item under disk storage: %v, %d mappings, %d visits", got, mappings(w), seg.visits.Load())
	}
}

func TestSegmentGraphMappedCorruptRebuilt(t *testing.T) {
	gw := newGraphTestWriter(t, t.TempDir(), false)
	gw.config.VectorStorage = VectorStorageDisk
	seg := testVectorSegment(t)
	first := mappedWrapper(seg, 4, gw.Writer)
	searchGraph(t, first)
	if err := first.DecRef(); err != nil {
		t.Fatal(err)
	}
	path := gw.graphFile(t, 4)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)-20] ^= 0xff
	if err := os.WriteFile(path, b, 0o600); err != nil { // #nosec G703 -- the test's temp directory.
		t.Fatal(err)
	}
	graph, mapping, persist := gw.loadGraph(context.Background(), seg, 4, testGraphKey)
	if graph != nil || mapping != nil || !persist {
		t.Fatalf("corrupt item: graph %v, mapping %v, persist %v", graph, mapping, persist)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("corrupt item kept: %v", err)
	}
}

type countingRefs struct{ refs int }

func (c *countingRefs) AddRef() { c.refs++ }

func (c *countingRefs) DecRef() error {
	c.refs--
	return nil
}

// Merge sources pin their segments, whose mapped graphs BuildFrom reads,
// until warm-up releases them once.
func TestMergeSourcesHoldReferences(t *testing.T) {
	built := &countingRefs{}
	unbuilt := &countingRefs{}
	src := &segmentWrapper{Segment: testVectorSegment(t), refCounter: built}
	if _, err := src.vectorGraph(context.Background(), testGraphKey); err != nil {
		t.Fatal(err)
	}
	empty := &segmentWrapper{Segment: testVectorSegment(t), refCounter: unbuilt}
	merged := &segmentWrapper{Segment: testVectorSegment(t)}
	merged.setMergeSources(map[uint64]*segmentSnapshot{1: {segment: src}, 2: {segment: empty}},
		map[uint64][]uint64{1: {0, 1, 2, 3, 4}, 2: {0, 1, 2, 3, 4}})
	if built.refs != 1 || unbuilt.refs != 0 {
		t.Fatalf("refs %d and %d, want 1 and 0", built.refs, unbuilt.refs)
	}
	for range 2 {
		if err := merged.clearMergeSources(); err != nil {
			t.Fatal(err)
		}
	}
	if built.refs != 0 {
		t.Fatalf("refs %d after release", built.refs)
	}
}
