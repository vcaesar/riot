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
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"
)

func TestBatchAfterCloseReturnsErrClosed(t *testing.T) {
	idx, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err = idx.Close(); err != nil {
		t.Fatal(err)
	}
	update := NewBatch()
	update.Update(testIdentifier("1"), &FakeDocument{NewFakeField("_id", "1", true, false, false)})
	remove := NewBatch()
	remove.Delete(testIdentifier("1"))
	for name, b := range map[string]*Batch{"update": update, "delete": remove} {
		if err := idx.Batch(b); !errors.Is(err, segment.ErrClosed) {
			t.Errorf("%s batch after close: got %v, want ErrClosed", name, err)
		}
	}
}

func TestFileSystemPersistTruncates(t *testing.T) {
	d := NewFileSystemDirectory(t.TempDir())
	if err := d.Setup(false); err != nil {
		t.Fatal(err)
	}
	if err := d.Persist(ItemKindSnapshot, 7, bytesWriterTo("long stale content"), nil); err != nil {
		t.Fatal(err)
	}
	if err := d.Persist(ItemKindSnapshot, 7, bytesWriterTo("new"), nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(d.path, d.fileName(ItemKindSnapshot, 7)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("new")) {
		t.Fatalf("got %q, want %q", got, "new")
	}
}

// TestReopenSkipsUnreadableSnapshotEpoch checks that new snapshots never
// reuse the epoch of a newer snapshot file that failed to load.
func TestReopenSkipsUnreadableSnapshotEpoch(t *testing.T) {
	cfg, cleanup := CreateConfig("TestReopenSkipsUnreadableSnapshotEpoch")
	defer func() {
		if err := cleanup(); err != nil {
			t.Log(err)
		}
	}()
	batch := func(idx *Writer, id string) {
		b := NewBatch()
		b.Update(testIdentifier(id), &FakeDocument{NewFakeField("_id", id, true, false, false)})
		if err := idx.Batch(b); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	batch(idx, "1")
	epochs, err := idx.directory.List(ItemKindSnapshot)
	if err != nil || len(epochs) == 0 {
		t.Fatalf("expected a persisted snapshot: %v %v", epochs, err)
	}
	if err = idx.Close(); err != nil {
		t.Fatal(err)
	}
	torn := epochs[0] + 5
	dir := cfg.DirectoryFunc()
	if err = dir.Persist(ItemKindSnapshot, torn, bytesWriterTo("torn"), nil); err != nil {
		t.Fatal(err)
	}

	idx, err = OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := idx.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	batch(idx, "2")
	if got := idx.currentEpoch(); got <= torn {
		t.Fatalf("epoch %d reuses or precedes unreadable snapshot %d", got, torn)
	}
}
