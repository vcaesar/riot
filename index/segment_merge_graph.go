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
	"errors"
	"fmt"
	"maps"
	"runtime"
	"sort"

	"github.com/vcaesar/riot/hnsw"
)

// mergeSource is a segment merged into another: its graphs when the merge
// was introduced, and the new document number of each of its documents.
// It holds a reference on seg, whose mapped graphs must stay valid.
type mergeSource struct {
	seg     *segmentWrapper
	graphs  map[graphKey]*graphEntry
	newNums []uint64
}

// setMergeSources records the graphs of the merged segments in old, so the
// merged segment's graphs can start from the largest instead of a full build.
func (s *segmentWrapper) setMergeSources(old map[uint64]*segmentSnapshot, newNums map[uint64][]uint64) {
	var sources []mergeSource
	for id, ss := range old {
		if ss == nil || newNums[id] == nil {
			continue
		}
		ss.segment.graphMu.Lock()
		graphs := maps.Clone(ss.segment.graphs)
		ss.segment.graphMu.Unlock()
		if len(graphs) > 0 {
			ss.segment.AddRef()
			sources = append(sources, mergeSource{seg: ss.segment, graphs: graphs, newNums: newNums[id]})
		}
	}
	s.graphMu.Lock()
	s.mergeSources = sources
	s.graphMu.Unlock()
}

// clearMergeSources releases the sources once warm-up is done.
func (s *segmentWrapper) clearMergeSources() error {
	s.graphMu.Lock()
	sources := s.mergeSources
	s.mergeSources = nil
	s.graphMu.Unlock()
	var errs []error
	for _, src := range sources {
		if err := src.seg.DecRef(); err != nil {
			errs = append(errs, fmt.Errorf("error releasing merged segment %d: %w", src.seg.id, err))
		}
	}
	return errors.Join(errs...)
}

// buildGraph builds the graph for key, from the largest usable source graph
// when the segment came from a merge, else from scratch.
func (s *segmentWrapper) buildGraph(ctx context.Context, key graphKey) (*hnsw.Graph, error) {
	ids, data, err := segmentVectors(ctx, s.Segment, key.field, key.metric)
	if err != nil {
		return nil, err
	}
	workers := graphWorkers(len(ids))
	if base, mapping := s.graphBase(key, ids); base != nil {
		graph, err := hnsw.BuildFrom(ctx, base, mapping, ids, data, workers)
		if err == nil || ctx.Err() != nil {
			return graph, err
		}
		// The base does not match this segment's vectors: build from scratch.
	}
	return hnsw.Build(ctx, key.metric, key.params(), ids, data, workers)
}

// graphBase returns the largest built source graph for key whose every node
// maps to a node of ids, the merged segment's ascending document numbers,
// with the mapping. Sources with documents dropped by the merge are skipped,
// as removing nodes would disconnect the graph.
func (s *segmentWrapper) graphBase(key graphKey, ids []uint64) (base *hnsw.Graph, mapping []uint32) {
	s.graphMu.Lock()
	sources := s.mergeSources
	s.graphMu.Unlock()
	for _, src := range sources {
		graph := readyGraphOf(src.graphs[key])
		if graph == nil || (base != nil && graph.Len() <= base.Len()) {
			continue
		}
		if m := mapNodes(graph.IDs(), src.newNums, ids); m != nil {
			base, mapping = graph, m
		}
	}
	return base, mapping
}

// readyGraphOf returns e's graph if it is built, without waiting.
func readyGraphOf(e *graphEntry) *hnsw.Graph {
	if e == nil {
		return nil
	}
	select {
	case <-e.ready:
	default:
		return nil
	}
	if e.abandoned || e.err != nil {
		return nil
	}
	return e.graph
}

// mapNodes maps each node of a source graph, by its old document number, to
// the node of the same vector in ids; a document's k-th vector maps to its
// k-th node. It returns nil when a node has no counterpart.
func mapNodes(oldIDs, newNums, ids []uint64) []uint32 {
	mapping := make([]uint32, len(oldIDs))
	occurrence := 0
	for i, old := range oldIDs {
		if old >= uint64(len(newNums)) {
			return nil
		}
		if i > 0 && old == oldIDs[i-1] {
			occurrence++
		} else {
			occurrence = 0
		}
		doc := newNums[old]
		first := sort.Search(len(ids), func(j int) bool { return ids[j] >= doc })
		node := first + occurrence
		if node >= len(ids) || ids[node] != doc {
			return nil
		}
		mapping[i] = uint32(node) // #nosec G115 -- graphs hold at most MaxUint32 nodes.
	}
	return mapping
}

// minParallelBuild is the vector count from which graphs are built by all
// CPUs. Smaller graphs build serially, so a rebuild reproduces them exactly.
const minParallelBuild = 4096

func graphWorkers(n int) int {
	if n >= minParallelBuild {
		return runtime.GOMAXPROCS(0)
	}
	return 1
}
