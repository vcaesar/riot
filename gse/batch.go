//  Copyright (c) 2026 The Riot Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 		http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gse

import (
	"fmt"
	"maps"
	"slices"

	riot "github.com/vcaesar/riot"
)

// Batch queues mapped updates and deletes for a single atomic writer batch.
// It is not safe for concurrent use. The last operation for an ID wins.
type Batch struct {
	index *Index
	docs  map[string]*riot.Document
}

// Batch creates an empty batch. Changes are visible only after Commit succeeds.
func (x *Index) Batch() *Batch {
	return &Batch{index: x, docs: make(map[string]*riot.Document)}
}

// Index queues a replacement using the same mapping and analyzer as Index.Index.
// Invalid data returns an error without changing the queued operations.
func (b *Batch) Index(id string, data any) error {
	doc, err := b.index.document(id, data)
	if err != nil {
		return err
	}
	b.docs[id] = doc
	return nil
}

// Delete queues removal of id, replacing any queued update for that ID.
func (b *Batch) Delete(id string) { b.docs[id] = nil }

// Reset discards all queued operations.
func (b *Batch) Reset() { clear(b.docs) }

// Commit applies all queued operations together and resets the batch on success.
// On failure the operations remain queued for retry.
func (b *Batch) Commit() error {
	if len(b.docs) == 0 {
		return nil
	}
	batch := riot.NewBatch()
	for _, id := range slices.Sorted(maps.Keys(b.docs)) {
		if doc := b.docs[id]; doc != nil {
			batch.Update(doc.ID(), doc)
		} else {
			batch.Delete(riot.Identifier(id))
		}
	}
	if err := b.index.writer.Batch(batch); err != nil {
		return fmt.Errorf("error committing batch: %v", err)
	}
	b.Reset()
	return nil
}
