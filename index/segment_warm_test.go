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
	"os"
	"sync"
	"sync/atomic"
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/ice/vec"

	"github.com/vcaesar/riot/hnsw"
)

func readyGraph(t *testing.T, w *segmentWrapper, key graphKey) *hnsw.Graph {
	t.Helper()
	w.graphMu.Lock()
	e, ok := w.graphs[key]
	w.graphMu.Unlock()
	if !ok {
		return nil
	}
	select {
	case <-e.ready:
	default:
		t.Fatal("warm-up finished with a graph still building")
	}
	if e.err != nil || e.abandoned {
		t.Fatalf("warm-up graph failed: %v, abandoned %v", e.err, e.abandoned)
	}
	return e.graph
}

func TestWarmGraphsBuildsSearchedKeys(t *testing.T) {
	gw := newGraphTestWriter(t, t.TempDir(), false)
	seg := testVectorSegment(t)
	w := &segmentWrapper{Segment: seg, refCounter: noOpRefCounter{}, id: 5, writer: gw.Writer}

	gw.warmGraphs(w, nil)
	gw.graphPersists.Wait()
	if seg.visits.Load() != 0 {
		t.Fatal("no key searched yet: nothing to warm")
	}

	gw.noteGraphKey(testGraphKey)
	gw.warmGraphs(w, nil)
	gw.graphPersists.Wait()
	if readyGraph(t, w, testGraphKey) != nil {
		t.Fatal("a segment outside the root must not be warmed")
	}

	gw.root = &Snapshot{parent: gw.Writer, refs: 1, segment: []*segmentSnapshot{{id: 5, segment: w}}}
	gw.warmGraphs(w, nil)
	gw.graphPersists.Wait()
	if readyGraph(t, w, testGraphKey) == nil {
		t.Fatal("searched key was not built in the background")
	}
	if _, err := os.Stat(gw.graphFile(t, 5)); err != nil {
		t.Fatalf("warmed graph not persisted: %v", err)
	}
	visits := seg.visits.Load()
	searchGraph(t, w)
	if seg.visits.Load() != visits {
		t.Fatal("search after warm-up rebuilt the graph")
	}
	if errs := gw.asyncErrors(); len(errs) != 0 {
		t.Fatalf("unexpected async errors: %v", errs)
	}
}

func TestWarmGraphsStopsOnClose(t *testing.T) {
	gw := newGraphTestWriter(t, t.TempDir(), false)
	w := &segmentWrapper{Segment: testVectorSegment(t), refCounter: noOpRefCounter{}, id: 5, writer: gw.Writer}
	gw.root = &Snapshot{parent: gw.Writer, refs: 1, segment: []*segmentSnapshot{{id: 5, segment: w}}}
	gw.noteGraphKey(testGraphKey)
	close(gw.closeCh)
	gw.warmGraphs(w, nil)
	gw.graphPersists.Wait()
	if readyGraph(t, w, testGraphKey) != nil {
		t.Fatal("closed writer must not warm graphs")
	}
}

// TestPersistAdoptsGraphs checks that a persisted copy of an in-memory
// segment reuses its graph and persists it without a rebuild.
func TestPersistAdoptsGraphs(t *testing.T) {
	gw := newGraphTestWriter(t, t.TempDir(), false)
	seg := testVectorSegment(t)
	mem := &segmentWrapper{Segment: seg, refCounter: noOpRefCounter{}}
	graph, err := mem.vectorGraph(context.Background(), testGraphKey)
	if err != nil {
		t.Fatal(err)
	}
	visits := seg.visits.Load()

	disk := &segmentWrapper{Segment: seg, refCounter: noOpRefCounter{}, id: 6, writer: gw.Writer}
	adopted := disk.adoptGraphs(mem)
	if len(adopted) != 1 {
		t.Fatalf("adopted %d graphs, want 1", len(adopted))
	}
	gw.warmGraphs(disk, adopted)
	gw.graphPersists.Wait()
	if got := readyGraph(t, disk, testGraphKey); got != graph {
		t.Fatal("persisted segment must share the in-memory graph")
	}
	if seg.visits.Load() != visits {
		t.Fatal("adopted graph was rebuilt")
	}
	if _, err := os.Stat(gw.graphFile(t, 6)); err != nil {
		t.Fatalf("adopted graph not persisted: %v", err)
	}
	if again := disk.adoptGraphs(mem); len(again) != 0 {
		t.Fatal("existing entries must not be replaced")
	}
}

// blockingVectorSegment blocks its first stored-field visit until release.
type blockingVectorSegment struct {
	*storedVectorSegment
	started, release chan struct{}
	once             sync.Once
}

func (s *blockingVectorSegment) VisitStoredFields(number uint64, visitor segment.StoredFieldVisitor) error {
	s.once.Do(func() {
		close(s.started)
		<-s.release
	})
	return s.storedVectorSegment.VisitStoredFields(number, visitor)
}

type countingRefCounter struct{ refs atomic.Int64 }

func (c *countingRefCounter) AddRef()       { c.refs.Add(1) }
func (c *countingRefCounter) DecRef() error { c.refs.Add(-1); return nil }

// TestWarmGraphsCanceledWhenSegmentLeavesRoot checks that a build for a
// segment merged away stops and releases the segment instead of finishing.
func TestWarmGraphsCanceledWhenSegmentLeavesRoot(t *testing.T) {
	gw := newGraphTestWriter(t, t.TempDir(), false)
	seg := &blockingVectorSegment{storedVectorSegment: testVectorSegment(t),
		started: make(chan struct{}), release: make(chan struct{})}
	refs := &countingRefCounter{}
	w := &segmentWrapper{Segment: seg, refCounter: refs, id: 5, writer: gw.Writer}
	gw.root = &Snapshot{parent: gw.Writer, refs: 1, segment: []*segmentSnapshot{{id: 5, segment: w}}}
	gw.noteGraphKey(testGraphKey)

	gw.warmGraphs(w, nil)
	<-seg.started
	gw.replaceRoot(&Snapshot{parent: gw.Writer, refs: 1}, nil, nil)
	close(seg.release)
	gw.graphPersists.Wait()

	if visits := seg.visits.Load(); visits >= int64(len(seg.vectors)) {
		t.Fatalf("build ran to completion (%d visits) after its segment left the root", visits)
	}
	if readyGraph(t, w, testGraphKey) != nil {
		t.Fatal("canceled build must not be cached")
	}
	if n := refs.refs.Load(); n != -1 {
		t.Fatalf("segment refs %d, want the warm-up's ref and the root's released (-1)", n)
	}
	if _, err := os.Stat(gw.graphFile(t, 5)); !os.IsNotExist(err) {
		t.Fatalf("canceled build must not persist, stat: %v", err)
	}
}

func TestPruneAndMoveWarmups(t *testing.T) {
	gw := newGraphTestWriter(t, t.TempDir(), false)
	mem, disk, other := &segmentWrapper{}, &segmentWrapper{}, &segmentWrapper{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := gw.trackWarmup(mem, cancel)

	gw.moveWarmups(map[*segmentWrapper]*segmentWrapper{mem: disk})
	if !gw.warmupMoved(w, mem) {
		t.Fatal("warm-up must follow the persisted copy")
	}
	gw.pruneWarmups(&Snapshot{segment: []*segmentSnapshot{{segment: disk}}})
	if ctx.Err() != nil {
		t.Fatal("warm-up of a live persisted copy was canceled")
	}
	gw.pruneWarmups(&Snapshot{segment: []*segmentSnapshot{{segment: other}}})
	if ctx.Err() == nil {
		t.Fatal("warm-up of a removed segment must be canceled")
	}
	gw.untrackWarmup(w)
	if len(gw.warmups) != 0 {
		t.Fatal("finished warm-up still tracked")
	}
	gw.pruneWarmups(nil)
}

func TestAdoptedAbandonedGraphRetries(t *testing.T) {
	seg := &storedVectorSegment{vectors: encodedVectors(t, []float32{1}, []float32{2})}
	key := graphKey{field: "v", metric: vec.L2, m: hnsw.DefaultM, efConstruction: hnsw.DefaultEfConstruction}
	abandoned := &graphEntry{ready: make(chan struct{}), err: context.Canceled, abandoned: true}
	close(abandoned.ready)
	mem := &segmentWrapper{Segment: seg, graphs: map[graphKey]*graphEntry{key: abandoned}}
	disk := &segmentWrapper{Segment: seg}
	disk.adoptGraphs(mem)
	graph, err := disk.vectorGraph(context.Background(), key)
	if err != nil || graph.Len() != 2 {
		t.Fatalf("adopted abandoned build must be retried: %v %v", graph, err)
	}
	if mem.graphs[key] != abandoned {
		t.Fatal("the adopter must only drop its own reference")
	}
}
