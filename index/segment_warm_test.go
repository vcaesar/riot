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
	"testing"

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
