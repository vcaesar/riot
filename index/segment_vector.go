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
	"io"

	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/ice/vec"

	"github.com/vcaesar/riot/hnsw"
)

// graphKey identifies one graph per field, metric and construction parameters.
type graphKey struct {
	field             string
	metric            vec.Metric
	m, efConstruction int
	quantization      hnsw.Quantization
}

func newGraphKey(field string, metric vec.Metric, params hnsw.Params) graphKey {
	return graphKey{field: field, metric: metric, m: params.M, efConstruction: params.EfConstruction,
		quantization: params.Quantization}
}

func (key graphKey) params() hnsw.Params {
	return hnsw.Params{M: key.m, EfConstruction: key.efConstruction, Quantization: key.quantization}
}

type graphEntry struct {
	ready     chan struct{}
	graph     *hnsw.Graph
	err       error
	abandoned bool
}

// vectorGraph returns the cached HNSW graph for key, loading it from the
// directory or building it from stored vectors on first use. Segments are
// immutable, so a graph stays valid for the wrapper's lifetime; deletions are
// filtered at search time. A build abandoned by cancellation is not cached,
// so a later caller retries. Built graphs of persisted segments are persisted
// after waiters are released.
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
			var persist bool
			if s.writer != nil {
				var mapping io.Closer
				e.graph, mapping, persist = s.writer.loadGraph(ctx, s.Segment, s.id, key)
				if mapping != nil {
					s.keepGraphMapping(mapping)
				}
			}
			if e.graph == nil {
				e.graph, e.err = s.buildGraph(ctx, key)
			}
			if e.err != nil && ctx.Err() != nil {
				e.abandoned = true
				s.forgetGraph(key, e)
			}
			close(e.ready)
			if persist && e.err == nil {
				s.writer.persistGraph(s.id, s.Count(), key, e.graph)
			}
			return e.graph, e.err
		}
		select {
		case <-e.ready:
			if e.abandoned {
				// An entry adopted from another segment is only removed there.
				s.forgetGraph(key, e)
				continue
			}
			return e.graph, e.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (s *segmentWrapper) forgetGraph(key graphKey, e *graphEntry) {
	s.graphMu.Lock()
	if s.graphs[key] == e {
		delete(s.graphs, key)
	}
	s.graphMu.Unlock()
}

// keepGraphMapping holds a graph item's mapping, which its graph uses in
// place, until the segment closes.
func (s *segmentWrapper) keepGraphMapping(c io.Closer) {
	s.graphMu.Lock()
	s.graphMappings = append(s.graphMappings, c)
	s.graphMu.Unlock()
}

// closeGraphMappings releases the mappings once the segment's last
// reference is gone, so no graph using them is reachable.
func (s *segmentWrapper) closeGraphMappings() error {
	s.graphMu.Lock()
	mappings := s.graphMappings
	s.graphMappings = nil
	s.graphMu.Unlock()
	var errs []error
	for _, c := range mappings {
		if err := c.Close(); err != nil {
			errs = append(errs, fmt.Errorf("error closing vector graph of segment %d: %w", s.id, err))
		}
	}
	return errors.Join(errs...)
}

func buildVectorGraph(ctx context.Context, seg segment.Segment, key graphKey) (*hnsw.Graph, error) {
	return (&segmentWrapper{Segment: seg}).buildGraph(ctx, key)
}

// segmentVectors returns the document numbers and node-major values of
// field's vectors in document order, the order graphs are built in. Each
// vector must be valid for metric and share the first one's dimension.
func segmentVectors(ctx context.Context, seg segment.Segment, field string,
	metric vec.Metric) (ids []uint64, data []float32, err error) {
	ids = make([]uint64, 0, seg.Count())
	err = visitVectors(ctx, seg, field, func(number uint64, vector []float32) error {
		if err := vec.Validate(vector, metric); err != nil {
			return err
		}
		if data == nil {
			data = make([]float32, 0, int(seg.Count())*len(vector)) // #nosec G115 -- a segment's count fits in memory.
		} else if dim := len(data) / len(ids); len(vector) != dim {
			return fmt.Errorf("vector dimension mismatch: graph %d, vector %d", dim, len(vector))
		}
		ids = append(ids, number)
		data = append(data, vector...)
		return nil
	})
	return ids, data, err
}

// visitVectors passes each stored vector of field to fn in document order.
func visitVectors(ctx context.Context, seg segment.Segment, field string, fn func(uint64, []float32) error) error {
	for number := uint64(0); number < seg.Count(); number++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		var valueErr error
		err := seg.VisitStoredFields(number, func(name string, data []byte) bool {
			if name != field {
				return true
			}
			var vector []float32
			if vector, valueErr = vec.Decode(data); valueErr != nil {
				return false
			}
			valueErr = fn(number, vector)
			return valueErr == nil
		})
		if err != nil {
			return fmt.Errorf("vector graph document %d: %w", number, err)
		}
		if valueErr != nil {
			return fmt.Errorf("vector graph document %d field %q: %w", number, field, valueErr)
		}
	}
	return nil
}
