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

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	riot "github.com/vcaesar/riot"
)

func main() {
	dir, err := os.MkdirTemp("", "riot_vectors")
	if err != nil {
		log.Fatal(err)
	}
	err = errors.Join(run(os.Stdout, dir), os.RemoveAll(dir))
	if err != nil {
		log.Fatal(err)
	}
}

// run indexes vectors on disk, then reopens the index with a reader that
// searches the persisted HNSW graphs.
func run(out io.Writer, dir string) error {
	// VectorStorageDisk (the default) memory-maps graphs with their vectors;
	// VectorStorageMemory persists links only and loads vectors onto the heap.
	config := riot.DefaultConfig(dir).WithVectorStorage(riot.VectorStorageDisk)
	if err := index(config); err != nil {
		return err
	}
	return search(out, config)
}

func index(config riot.Config) (err error) {
	writer, err := riot.OpenWriter(config)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, writer.Close()) }()

	batch := riot.NewBatch()
	for _, item := range []struct {
		id     string
		vector []float32
	}{
		{"east", []float32{1, 0}},
		{"northeast", []float32{1, 1}},
		{"north", []float32{0, 1}},
	} {
		field, err := riot.NewVectorField("embedding", item.vector)
		if err != nil {
			return err
		}
		doc := riot.NewDocument(item.id).AddField(field)
		batch.Update(doc.ID(), doc)
	}
	return writer.Batch(batch)
}

func search(out io.Writer, config riot.Config) (err error) {
	reader, err := riot.OpenReader(config)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, reader.Close()) }()

	query := riot.NewKNNQuery("embedding", []float32{0, 1}, 2).
		SetMetric(riot.Cosine).
		SetANN(riot.ANNParams{EfSearch: 100})
	matches, err := reader.Search(context.Background(), riot.NewTopNSearch(2, query))
	if err != nil {
		return err
	}
	for {
		match, err := matches.Next()
		if err != nil {
			return err
		}
		if match == nil {
			return nil
		}
		var id string
		if err := match.VisitStoredFields(func(name string, value []byte) bool {
			if name == "_id" {
				id = string(value)
			}
			return true
		}); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "ANN: %s (%.3f)\n", id, match.Score); err != nil {
			return err
		}
	}
}
