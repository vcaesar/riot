// Copyright (c) 2026 The AtomAI.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package index

import (
	"errors"
	"strconv"
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"
)

// Only directories that keep nothing past the process skip the persist wait.
func TestVolatileDirectory(t *testing.T) {
	if !isVolatile(NewInMemoryDirectory()) {
		t.Fatal("in-memory directory should be volatile")
	}
	if isVolatile(NewFileSystemDirectory(t.TempDir())) {
		t.Fatal("file system directory must not be volatile")
	}
}

// In memory, each batch is searchable on return, and the persister still
// runs its callbacks and merges in the background; a batch Close overtakes
// gets segment.ErrClosed, as nothing durable was lost.
func TestInMemoryBatchVisibleWithoutPersistWait(t *testing.T) {
	cfg := InMemoryOnlyConfig().
		WithNormCalc(func(_ string, numTerms int) float32 { return float32(numTerms) }).
		WithVirtualField(NewFakeField("", "", false, false, false))
	w, err := OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	persisted := make(chan error, 64)
	const n = 50
	for i := range n {
		b := NewBatch()
		id := strconv.Itoa(i)
		b.Update(testIdentifier(id), &FakeDocument{
			NewFakeField("_id", id, true, false, false),
			NewFakeField("name", "test", false, false, true),
		})
		b.SetPersistedCallback(func(err error) { persisted <- err })
		if err := w.Batch(b); err != nil {
			t.Fatal(err)
		}
		r, err := w.Reader()
		if err != nil {
			t.Fatal(err)
		}
		count, err := r.Count()
		if err != nil || count != uint64(i+1) {
			t.Fatalf("after batch %d: count %d err %v", i, count, err)
		}
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	close(persisted)
	got := 0
	for err := range persisted {
		if err != nil && !errors.Is(err, segment.ErrClosed) {
			t.Fatal(err)
		}
		if err == nil {
			got++
		}
	}
	if got == 0 {
		t.Fatal("no persisted callbacks ran")
	}
}
