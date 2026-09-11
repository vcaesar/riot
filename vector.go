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

package bluge

import (
	"context"
	"fmt"

	"github.com/vcaesar/ice/vec"
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
