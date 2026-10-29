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
	"math"
	"reflect"
	"testing"

	"github.com/vcaesar/riot/hnsw"
)

const testDocDropped = math.MaxInt64

func TestMapNodes(t *testing.T) {
	for name, tc := range map[string]struct {
		oldIDs, newNums, ids []uint64
		want                 []uint32
	}{
		"repeated":   {[]uint64{0, 0, 1, 3}, []uint64{5, 6, testDocDropped, 9}, []uint64{2, 5, 5, 6, 9}, []uint32{1, 2, 3, 4}},
		"dropped":    {[]uint64{0, 2}, []uint64{5, 6, testDocDropped}, []uint64{5, 6}, nil},
		"missing":    {[]uint64{0, 1}, []uint64{5, 6}, []uint64{5}, nil},
		"too many":   {[]uint64{0, 0}, []uint64{5}, []uint64{5}, nil},
		"out of map": {[]uint64{4}, []uint64{5}, []uint64{5}, nil},
	} {
		if got := mapNodes(tc.oldIDs, tc.newNums, tc.ids); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: mapping %v, want %v", name, got, tc.want)
		}
	}
}

// mergeFixture is a segment of vectors 0..n-1 with merge sources holding
// graphs of its even and its odd documents, as two merged segments.
func mergeFixture(t *testing.T, n int, buildOdd bool) (merged *segmentWrapper, evenGraph *hnsw.Graph) {
	t.Helper()
	vectors := make([][]float32, n)
	for i := range vectors {
		vectors[i] = []float32{float32(i % 13), float32(i % 7), float32(i)}
	}
	part := func(rem int) (*segmentWrapper, []uint64) {
		var vs [][]float32
		var newNums []uint64
		for i := rem; i < n; i += 2 {
			vs = append(vs, vectors[i])
			newNums = append(newNums, uint64(i)) // #nosec G115 -- i is non-negative.
		}
		return &segmentWrapper{Segment: &storedVectorSegment{vectors: encodedVectors(t, vs...)}, refCounter: noOpRefCounter{}}, newNums
	}
	even, evenNums := part(0)
	odd, oddNums := part(1)
	evenGraph, err := even.vectorGraph(context.Background(), testGraphKey)
	if err != nil {
		t.Fatal(err)
	}
	if buildOdd {
		if _, err := odd.vectorGraph(context.Background(), testGraphKey); err != nil {
			t.Fatal(err)
		}
	}
	merged = &segmentWrapper{Segment: &storedVectorSegment{vectors: encodedVectors(t, vectors...)}}
	merged.setMergeSources(map[uint64]*segmentSnapshot{
		1: {segment: even}, 2: {segment: odd}, 3: nil,
	}, map[uint64][]uint64{1: evenNums, 2: oddNums})
	return merged, evenGraph
}

func TestMergedSegmentGraphReusesLargestSource(t *testing.T) {
	merged, evenGraph := mergeFixture(t, 41, true)
	ids, _, err := segmentVectors(context.Background(), merged.Segment, "v", testGraphKey.metric)
	if err != nil {
		t.Fatal(err)
	}
	// Even documents are one more than odd ones.
	base, mapping := merged.graphBase(testGraphKey, ids)
	if base != evenGraph || len(mapping) != evenGraph.Len() || mapping[1] != 2 {
		t.Fatalf("base %p mapping %v, want the even graph %p", base, mapping, evenGraph)
	}
	if base, _ := merged.graphBase(graphKey{field: "v", metric: testGraphKey.metric, m: 4, efConstruction: 8}, ids); base != nil {
		t.Fatal("a source graph of another key must not be used")
	}

	graph, err := merged.vectorGraph(context.Background(), testGraphKey)
	if err != nil {
		t.Fatal(err)
	}
	if graph.Len() != 41 {
		t.Fatalf("merged graph has %d nodes", graph.Len())
	}
	for i := range 41 {
		got, err := graph.Search([]float32{float32(i % 13), float32(i % 7), float32(i)}, 1, 0, nil)
		if err != nil || len(got) != 1 || got[0].Number != uint64(i) {
			t.Fatalf("document %d: %v %v", i, got, err)
		}
	}
	if err := merged.clearMergeSources(); err != nil {
		t.Fatal(err)
	}
	if base, _ := merged.graphBase(testGraphKey, ids); base != nil {
		t.Fatal("cleared sources must not be used")
	}
}

func TestMergedSegmentGraphSkipsUnbuiltSources(t *testing.T) {
	merged, evenGraph := mergeFixture(t, 40, false)
	ids, _, err := segmentVectors(context.Background(), merged.Segment, "v", testGraphKey.metric)
	if err != nil {
		t.Fatal(err)
	}
	if base, _ := merged.graphBase(testGraphKey, ids); base != evenGraph {
		t.Fatal("only the built source graph is usable")
	}
	pending := &graphEntry{ready: make(chan struct{})}
	if readyGraphOf(pending) != nil || readyGraphOf(nil) != nil {
		t.Fatal("unbuilt entries have no graph")
	}
	close(pending.ready)
	pending.abandoned = true
	if readyGraphOf(pending) != nil {
		t.Fatal("abandoned entries have no graph")
	}
}
