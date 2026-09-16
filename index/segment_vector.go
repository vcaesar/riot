// Copyright (c) 2026 The Bluge Authors.
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
	"fmt"

	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/ice/vec"

	"github.com/vcaesar/riot/hnsw"
)

// graphKey identifies one graph per field, metric and construction parameters.
type graphKey struct {
	field             string
	metric            vec.Metric
	m, efConstruction int
}

type graphEntry struct {
	ready     chan struct{}
	graph     *hnsw.Graph
	err       error
	abandoned bool
}

// vectorGraph returns the cached HNSW graph for key, building it from stored
// vectors on first use. Segments are immutable, so a graph stays valid for
// the wrapper's lifetime; deletions are filtered at search time. A build
// abandoned by cancellation is not cached, so a later caller retries.
func (s *segmentWrapper) vectorGraph(ctx context.Context, key graphKey) (*hnsw.Graph, error) {
	for {
		s.graphMu.Lock()
		if s.graphs == nil {
			s.graphs = make(map[graphKey]*graphEntry)
		}
		e, ok := s.graphs[key]
		if !ok {
			e = &graphEntry{ready: make(chan struct{})}
			s.graphs[key] = e
		}
		s.graphMu.Unlock()
		if !ok {
			e.graph, e.err = buildVectorGraph(ctx, s.Segment, key)
			if e.err != nil && ctx.Err() != nil {
				e.abandoned = true
				s.graphMu.Lock()
				delete(s.graphs, key)
				s.graphMu.Unlock()
			}
			close(e.ready)
			return e.graph, e.err
		}
		select {
		case <-e.ready:
			if e.abandoned {
				continue
			}
			return e.graph, e.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func buildVectorGraph(ctx context.Context, seg segment.Segment, key graphKey) (*hnsw.Graph, error) {
	graph, err := hnsw.New(key.metric, hnsw.Params{M: key.m, EfConstruction: key.efConstruction})
	if err != nil {
		return nil, err
	}
	for number := uint64(0); number < seg.Count(); number++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var valueErr error
		err := seg.VisitStoredFields(number, func(name string, data []byte) bool {
			if name != key.field {
				return true
			}
			var vector []float32
			if vector, valueErr = vec.Decode(data); valueErr != nil {
				return false
			}
			valueErr = graph.Add(number, vector)
			return valueErr == nil
		})
		if err != nil {
			return nil, fmt.Errorf("vector graph document %d: %w", number, err)
		}
		if valueErr != nil {
			return nil, fmt.Errorf("vector graph document %d field %q: %w", number, key.field, valueErr)
		}
	}
	return graph, nil
}
