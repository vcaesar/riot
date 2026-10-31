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
	"sync/atomic"
	"testing"
)

// addCountingRefCounter counts AddRef calls only.
type addCountingRefCounter struct{ adds atomic.Int64 }

func (c *addCountingRefCounter) AddRef()       { c.adds.Add(1) }
func (c *addCountingRefCounter) DecRef() error { return nil }

// sizedVectorSegment adds the Size updateSize needs to storedVectorSegment.
type sizedVectorSegment struct{ *storedVectorSegment }

func (sizedVectorSegment) Size() int { return 0 }

// TestIntroduceMergeKeepsGraphSources checks that the merged segment sees
// the merged-away segments as graph sources, although ProcessSegmentNow
// empties segmentMerge.old while building the new root.
func TestIntroduceMergeKeepsGraphSources(t *testing.T) {
	vectors := [][]float32{{1, 0}, {2, 0}, {3, 0}, {4, 0}}
	old := map[uint64]*segmentSnapshot{}
	root := &Snapshot{refs: 1}
	refs := []*addCountingRefCounter{{}, {}}
	for i, rc := range refs {
		seg := &segmentWrapper{
			Segment:    sizedVectorSegment{&storedVectorSegment{vectors: encodedVectors(t, vectors[2*i:2*i+2]...)}},
			refCounter: rc,
		}
		if _, err := seg.vectorGraph(context.Background(), testGraphKey); err != nil {
			t.Fatal(err)
		}
		ss := &segmentSnapshot{id: uint64(i + 1), segment: seg}
		root.segment = append(root.segment, ss)
		root.offsets = append(root.offsets, uint64(2*i))
		old[ss.id] = ss
	}
	w := &Writer{closeCh: make(chan struct{}), root: root}
	root.parent = w

	merged := &segmentWrapper{
		Segment:    sizedVectorSegment{&storedVectorSegment{vectors: encodedVectors(t, vectors...)}},
		refCounter: noOpRefCounter{},
	}
	sm := &segmentMerge{
		id:            3,
		old:           old,
		oldNewDocNums: map[uint64][]uint64{1: {0, 1}, 2: {2, 3}},
		new:           merged,
		notifyCh:      make(chan *mergeTaskIntroStatus, 1),
	}
	w.introduceMerge(sm, 2)

	status := <-sm.notifyCh
	if status.skipped || len(status.snapshot.segment) != 1 {
		t.Fatalf("expected the merged segment alone in the root, got %+v", status)
	}
	// setMergeSources takes a ref on each source it records
	for i, rc := range refs {
		if rc.adds.Load() == 0 {
			t.Errorf("segment %d was not recorded as a graph source", i+1)
		}
	}
}
