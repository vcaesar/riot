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

package riot

import (
	"context"
	"fmt"

	"github.com/vcaesar/ice/vec"

	"github.com/vcaesar/riot/hnsw"
	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/knn"
)

// Metric specifies vector similarity, independently of text scoring.
type Metric = vec.Metric

const (
	Cosine     = vec.Cosine
	DotProduct = vec.DotProduct
	L2         = vec.L2
)

// VectorMatch contains a snapshot-local document number and similarity score.
// Number can be passed to the same Reader's VisitStoredFields.
type VectorMatch = vec.Match

// ANNParams tunes approximate search: M and EfConstruction shape the
// per-segment HNSW graph, EfSearch bounds the search beam. Zero fields take
// the defaults in package hnsw.
type ANNParams = hnsw.Params

// NewVectorField copies a finite, nonempty vector into a stored-only field.
// It emits no text terms or doc values. Use a dedicated field name for vectors
// of a single dimension; search reports incompatible stored values as errors.
func NewVectorField(name string, vector []float32) (*TermField, error) {
	if name == "" {
		return nil, fmt.Errorf("vector field name must be non-empty")
	}
	encoded, err := vec.Encode(vector)
	if err != nil {
		return nil, err
	}
	return &TermField{FieldOptions: Store, name: name, value: encoded}, nil
}

// SearchVectors performs exact top-k vector search over live documents.
// Scores are cosine similarity, dot product, or negative squared L2 distance;
// higher is better, with ties ordered by ascending document number. Repeated
// values use the best score per document. k must be positive, and query must
// be finite, nonempty, and match the stored dimension (nonzero for cosine).
// accept may be nil; otherwise it receives snapshot-local document numbers
// before ranking. Calls to accept are serial. Numbers are valid only for this
// reader. Unsupported segment plugins return an error, not partial results.
// This API does not combine vector scores with text scores or use ANN.
func (r *Reader) SearchVectors(ctx context.Context, field string, query []float32, k int,
	metric Metric, accept func(uint64) bool) ([]VectorMatch, error) {
	return r.reader.SearchVectors(ctx, field, query, k, metric, accept)
}

// SearchVectorsANN is the approximate counterpart of SearchVectors. Each
// segment is searched through an HNSW graph built from its stored vectors on
// first use and cached for the segment's lifetime, keyed by field, metric,
// M and EfConstruction; the first search after opening or merging pays the
// build. Results may miss some true neighbors, but every returned score is
// exact, and deleted or rejected documents are never returned.
func (r *Reader) SearchVectorsANN(ctx context.Context, field string, query []float32, k int,
	metric Metric, params ANNParams, accept func(uint64) bool) ([]VectorMatch, error) {
	return r.reader.SearchVectorsANN(ctx, field, query, k, metric, params, accept)
}

// KNNQuery matches the k nearest documents to a vector, scored by
// boost * similarity, so it composes with BooleanQuery, sorting and
// aggregations. The vector scan runs on first use under the context passed
// to Reader.Search, after Config.SearchStartFunc admission. SetANN switches
// from an exact scan to approximate search.
type KNNQuery struct {
	field  string
	vector []float32
	k      int
	metric Metric
	boost  *boost
	ann    *ANNParams
}

// NewKNNQuery creates a cosine KNNQuery over field.
func NewKNNQuery(field string, vector []float32, k int) *KNNQuery {
	return &KNNQuery{field: field, vector: vector, k: k, metric: Cosine}
}

func (q *KNNQuery) SetField(f string) *KNNQuery {
	q.field = f
	return q
}

func (q *KNNQuery) Field() string {
	return q.field
}

func (q *KNNQuery) SetMetric(m Metric) *KNNQuery {
	q.metric = m
	return q
}

func (q *KNNQuery) Metric() Metric {
	return q.metric
}

func (q *KNNQuery) SetBoost(b float64) *KNNQuery {
	boostVal := boost(b)
	q.boost = &boostVal
	return q
}

func (q *KNNQuery) Boost() float64 {
	return q.boost.Value()
}

func (q *KNNQuery) K() int {
	return q.k
}

func (q *KNNQuery) Vector() []float32 {
	return q.vector
}

// SetANN enables approximate search with params; see Reader.SearchVectorsANN.
func (q *KNNQuery) SetANN(params ANNParams) *KNNQuery {
	q.ann = &params
	return q
}

// ANN returns the approximate search parameters, or nil for exact search.
func (q *KNNQuery) ANN() *ANNParams {
	return q.ann
}

func (q *KNNQuery) Searcher(i search.Reader, options search.SearcherOptions) (search.Searcher, error) {
	if q.ann != nil {
		return knn.NewApproximateSearcher(i, q.field, q.vector, q.k, q.metric, *q.ann, q.boost.Value(), nil, options)
	}
	return knn.NewSearcher(i, q.field, q.vector, q.k, q.metric, q.boost.Value(), nil, options)
}

func (q *KNNQuery) Validate() error {
	if q.ann != nil {
		if err := q.ann.Validate(); err != nil {
			return err
		}
	}
	return knn.Validate(q.field, q.vector, q.k, q.metric)
}
