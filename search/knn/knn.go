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

// Package knn adapts exact and approximate top-k vector search to the
// search.Searcher interface so it composes with boolean, sort, collector and
// aggregation code.
package knn

import (
	"context"
	"fmt"
	"sort"

	"github.com/vcaesar/ice/vec"

	"github.com/vcaesar/riot/hnsw"
	"github.com/vcaesar/riot/search"
)

// VectorReader is the optional capability a search.Reader must provide for
// KNN search. *index.Snapshot implements it.
type VectorReader interface {
	SearchVectors(ctx context.Context, field string, query []float32, k int,
		metric vec.Metric, accept func(uint64) bool) ([]vec.Match, error)
}

// ApproximateVectorReader is the optional capability a search.Reader must
// provide for ANN search. *index.Snapshot implements it.
type ApproximateVectorReader interface {
	SearchVectorsANN(ctx context.Context, field string, query []float32, k int,
		metric vec.Metric, params hnsw.Params, accept func(uint64) bool) ([]vec.Match, error)
}

// Validate reports whether the parameters describe a runnable KNN search.
func Validate(field string, query []float32, k int, metric vec.Metric) error {
	if field == "" || k <= 0 {
		return fmt.Errorf("knn search requires a non-empty field and positive k")
	}
	return vec.Validate(query, metric)
}

// Searcher yields the k nearest documents in ascending document number order,
// scored by boost * similarity. The vector scan is deferred to the first Next
// or Advance so it runs after search admission (Config.SearchStartFunc) and
// under the collector's search.Context.Ctx for cancellation.
type Searcher struct {
	reader  search.Reader
	vectors VectorReader
	approx  ApproximateVectorReader
	ann     hnsw.Params
	field   string
	query   []float32
	k       int
	metric  vec.Metric
	boost   float64
	accept  func(uint64) bool
	explain bool

	matches []vec.Match
	ran     bool
	err     error
	pos     int
}

// NewSearcher validates the parameters without scanning. indexReader must
// implement VectorReader. accept may be nil; otherwise it prefilters
// document numbers.
func NewSearcher(indexReader search.Reader, field string, query []float32, k int,
	metric vec.Metric, boost float64, accept func(uint64) bool, options search.SearcherOptions) (*Searcher, error) {
	vectors, ok := indexReader.(VectorReader)
	if !ok {
		return nil, fmt.Errorf("reader %T does not support vector search", indexReader)
	}
	if err := Validate(field, query, k, metric); err != nil {
		return nil, err
	}
	return &Searcher{
		reader:  indexReader,
		vectors: vectors,
		field:   field,
		query:   query,
		k:       k,
		metric:  metric,
		boost:   boost,
		accept:  accept,
		explain: options.Explain,
	}, nil
}

// NewApproximateSearcher is like NewSearcher but searches per-segment HNSW
// graphs tuned by params. indexReader must implement ApproximateVectorReader.
func NewApproximateSearcher(indexReader search.Reader, field string, query []float32, k int,
	metric vec.Metric, params hnsw.Params, boost float64, accept func(uint64) bool,
	options search.SearcherOptions) (*Searcher, error) {
	approx, ok := indexReader.(ApproximateVectorReader)
	if !ok {
		return nil, fmt.Errorf("reader %T does not support approximate vector search", indexReader)
	}
	if err := params.Validate(); err != nil {
		return nil, err
	}
	if err := Validate(field, query, k, metric); err != nil {
		return nil, err
	}
	return &Searcher{
		reader:  indexReader,
		approx:  approx,
		ann:     params,
		field:   field,
		query:   query,
		k:       k,
		metric:  metric,
		boost:   boost,
		accept:  accept,
		explain: options.Explain,
	}, nil
}

func (s *Searcher) run(ctx *search.Context) error {
	if s.ran {
		return s.err
	}
	s.ran = true
	var matches []vec.Match
	var err error
	if s.approx != nil {
		matches, err = s.approx.SearchVectorsANN(ctx.Ctx, s.field, s.query, s.k, s.metric, s.ann, s.accept)
	} else {
		matches, err = s.vectors.SearchVectors(ctx.Ctx, s.field, s.query, s.k, s.metric, s.accept)
	}
	if err != nil {
		s.err = fmt.Errorf("error searching vectors: %w", err)
		return s.err
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Number < matches[j].Number })
	s.matches = matches
	return nil
}

// Size estimates memory for k matches; it is used for admission before the scan runs.
func (s *Searcher) Size() int {
	return reflectStaticSizeSearcher + sizeOfPtr + len(s.query)*sizeOfFloat32 + s.k*reflectStaticSizeMatch
}

// Count returns k until the scan has run, then the number of matches.
func (s *Searcher) Count() uint64 {
	if !s.ran {
		return uint64(s.k) // #nosec G115 -- Validate rejects k <= 0.
	}
	return uint64(len(s.matches))
}

func (s *Searcher) Next(ctx *search.Context) (*search.DocumentMatch, error) {
	if err := s.run(ctx); err != nil {
		return nil, err
	}
	if s.pos >= len(s.matches) {
		return nil, nil
	}
	m := s.matches[s.pos]
	s.pos++
	return s.buildDocumentMatch(ctx, m), nil
}

func (s *Searcher) Advance(ctx *search.Context, number uint64) (*search.DocumentMatch, error) {
	if err := s.run(ctx); err != nil {
		return nil, err
	}
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
		kind := "knn"
		if s.approx != nil {
			kind = "ann"
		}
		rv.Explanation = search.NewExplanation(rv.Score,
			fmt.Sprintf("%s(%s:%s), similarity %f, boost %f", kind, s.field, s.metric, m.Score, s.boost))
	}
	return rv
}
