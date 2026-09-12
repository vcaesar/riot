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

// Package knn adapts exact top-k vector search to the search.Searcher
// interface so it composes with boolean, sort, collector and aggregation code.
package knn

import (
	"context"
	"fmt"
	"sort"

	"github.com/vcaesar/ice/vec"

	"github.com/vcaesar/riot/search"
)

// VectorReader is the optional capability a search.Reader must provide for
// KNN search. *index.Snapshot implements it.
type VectorReader interface {
	SearchVectors(ctx context.Context, field string, query []float32, k int,
		metric vec.Metric, accept func(uint64) bool) ([]vec.Match, error)
}

// Searcher yields the k nearest documents in ascending document number order,
// scored by boost * similarity. The vector search runs once in NewSearcher.
type Searcher struct {
	reader  search.Reader
	matches []vec.Match
	pos     int
	field   string
	metric  vec.Metric
	boost   float64
	explain bool
}

// NewSearcher runs the vector search eagerly. indexReader must implement
// VectorReader. accept may be nil; otherwise it prefilters document numbers.
func NewSearcher(ctx context.Context, indexReader search.Reader, field string, query []float32, k int,
	metric vec.Metric, boost float64, accept func(uint64) bool, options search.SearcherOptions) (*Searcher, error) {
	vr, ok := indexReader.(VectorReader)
	if !ok {
		return nil, fmt.Errorf("reader %T does not support vector search", indexReader)
	}
	matches, err := vr.SearchVectors(ctx, field, query, k, metric, accept)
	if err != nil {
		return nil, fmt.Errorf("error searching vectors: %w", err)
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Number < matches[j].Number })
	return &Searcher{
		reader:  indexReader,
		matches: matches,
		field:   field,
		metric:  metric,
		boost:   boost,
		explain: options.Explain,
	}, nil
}

func (s *Searcher) Size() int {
	return reflectStaticSizeSearcher + sizeOfPtr + len(s.matches)*reflectStaticSizeMatch
}

func (s *Searcher) Count() uint64 {
	return uint64(len(s.matches))
}

func (s *Searcher) Next(ctx *search.Context) (*search.DocumentMatch, error) {
	if s.pos >= len(s.matches) {
		return nil, nil
	}
	m := s.matches[s.pos]
	s.pos++
	return s.buildDocumentMatch(ctx, m), nil
}

func (s *Searcher) Advance(ctx *search.Context, number uint64) (*search.DocumentMatch, error) {
	s.pos += sort.Search(len(s.matches)-s.pos, func(i int) bool {
		return s.matches[s.pos+i].Number >= number
	})
	return s.Next(ctx)
}

func (s *Searcher) Close() error {
	return nil
}

func (s *Searcher) Min() int {
	return 0
}

func (s *Searcher) DocumentMatchPoolSize() int {
	return 1
}

func (s *Searcher) buildDocumentMatch(ctx *search.Context, m vec.Match) *search.DocumentMatch {
	rv := ctx.DocumentMatchPool.Get()
	rv.SetReader(s.reader)
	rv.Number = m.Number
	rv.Score = s.boost * m.Score
	if s.explain {
		rv.Explanation = search.NewExplanation(rv.Score,
			fmt.Sprintf("knn(%s:%s), similarity %f, boost %f", s.field, s.metric, m.Score, s.boost))
	}
	return rv
}
