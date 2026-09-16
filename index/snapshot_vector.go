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
	"math"
	"sort"

	"github.com/vcaesar/ice/vec"

	"github.com/vcaesar/riot/hnsw"
)

type vectorSearcher interface {
	SearchVectors(context.Context, string, []float32, int, vec.Metric, func(uint64) bool) ([]vec.Match, error)
}

// SearchVectors searches each segment with its deletion mask and selects the
// global top-k, ordered by descending score then ascending document number.
// accept is optional and receives global snapshot-local numbers, serially.
func (i *Snapshot) SearchVectors(ctx context.Context, field string, query []float32, k int,
	metric vec.Metric, accept func(uint64) bool) ([]vec.Match, error) {
	return i.searchVectors(ctx, field, query, k, metric, accept,
		func(seg *segmentSnapshot, accept func(uint64) bool) ([]vec.Match, error) {
			// The wrapper embeds the base interface, which hides optional methods.
			searcher, ok := seg.segment.Segment.(vectorSearcher)
			if !ok {
				return nil, fmt.Errorf("segment %d (%T) does not support vector search", seg.id, seg.segment.Segment)
			}
			return searcher.SearchVectors(ctx, field, query, k, metric, accept)
		})
}

// SearchVectorsANN is the approximate counterpart of SearchVectors. Each
// segment is searched through an HNSW graph built from its stored vectors on
// first use and cached for the segment's lifetime, keyed by field, metric and
// construction parameters. Deleted and rejected documents are filtered during
// graph traversal. Results may miss some true neighbors; scores are exact.
func (i *Snapshot) SearchVectorsANN(ctx context.Context, field string, query []float32, k int,
	metric vec.Metric, params hnsw.Params, accept func(uint64) bool) ([]vec.Match, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}
	params = params.WithDefaults()
	key := graphKey{field: field, metric: metric, m: params.M, efConstruction: params.EfConstruction}
	return i.searchVectors(ctx, field, query, k, metric, accept,
		func(seg *segmentSnapshot, accept func(uint64) bool) ([]vec.Match, error) {
			graph, err := seg.segment.vectorGraph(ctx, key)
			if err != nil {
				return nil, err
			}
			return graph.Search(query, k, params.EfSearch, accept)
		})
}

func (i *Snapshot) searchVectors(ctx context.Context, field string, query []float32, k int,
	metric vec.Metric, accept func(uint64) bool,
	searchSegment func(*segmentSnapshot, func(uint64) bool) ([]vec.Match, error)) ([]vec.Match, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if field == "" || k <= 0 {
		return nil, fmt.Errorf("vector search requires a non-empty field and positive k")
	}
	if err := vec.Validate(query, metric); err != nil {
		return nil, err
	}
	var results []vec.Match
	for index, seg := range i.segment {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		offset := i.offsets[index]
		matches, err := searchSegment(seg, func(number uint64) bool {
			if seg.deleted != nil && number <= math.MaxUint32 && seg.deleted.Contains(uint32(number)) {
				return false
			}
			return accept == nil || accept(offset+number)
		})
		if err != nil {
			return nil, fmt.Errorf("error searching vectors in segment %d: %w", seg.id, err)
		}
		for _, match := range matches {
			match.Number += offset
			results = append(results, match)
		}
		sort.Slice(results, func(a, b int) bool {
			if results[a].Score == results[b].Score {
				return results[a].Number < results[b].Number
			}
			return results[a].Score > results[b].Score
		})
		if len(results) > k {
			results = results[:k]
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}
