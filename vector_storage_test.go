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

package riot

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"github.com/vcaesar/riot/hnsw"
)

// Both storages return the same results before and after reopening, also
// through OpenReader, for int8 and float32 graphs.
func TestSearchVectorsANNStorageReopen(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	vectors := make([][]float32, 300)
	for i := range vectors {
		vectors[i] = []float32{float32(rng.NormFloat64()), float32(rng.NormFloat64()), float32(rng.NormFloat64())}
	}
	query := []float32{0.3, -0.2, 0.9}
	for _, storage := range []VectorStorage{VectorStorageDisk, VectorStorageMemory} {
		for _, quant := range []hnsw.Quantization{hnsw.QuantizationInt8, hnsw.QuantizationNone} {
			t.Run(fmt.Sprintf("%d/%v", storage, quant), func(t *testing.T) {
				path := createTmpIndexPath(t)
				defer cleanupTmpIndexPath(t, path)
				config := DefaultConfig(path).WithVectorStorage(storage)
				params := ANNParams{Quantization: quant}
				w, err := OpenWriter(config)
				if err != nil {
					t.Fatal(err)
				}
				b := NewBatch()
				for i, v := range vectors {
					d := vectorDocument(t, fmt.Sprint(i), v)
					b.Update(d.ID(), d)
				}
				if err := w.Batch(b); err != nil {
					t.Fatal(err)
				}
				search := func(r *Reader) []VectorMatch {
					t.Helper()
					got, err := r.SearchVectorsANN(context.Background(), "v", query, 10, Cosine, params, nil)
					if err != nil {
						t.Fatal(err)
					}
					exact, err := r.SearchVectors(context.Background(), "v", query, 10, Cosine, nil)
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got, exact) {
						t.Fatalf("ANN %v, exact %v", got, exact)
					}
					return got
				}
				r, err := w.Reader()
				if err != nil {
					t.Fatal(err)
				}
				want := search(r)
				if err := r.Close(); err != nil {
					t.Fatal(err)
				}
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}

				for _, open := range []string{"writer", "reader"} {
					var r *Reader
					var w *Writer
					if open == "writer" {
						if w, err = OpenWriter(config); err == nil {
							r, err = w.Reader()
						}
					} else {
						r, err = OpenReader(config)
					}
					if err != nil {
						t.Fatal(err)
					}
					if got := search(r); !reflect.DeepEqual(got, want) {
						t.Fatalf("%s after reopen: %v, want %v", open, got, want)
					}
					if err := r.Close(); err != nil {
						t.Fatal(err)
					}
					if w != nil {
						if err := w.Close(); err != nil {
							t.Fatal(err)
						}
					}
				}
			})
		}
	}
}
