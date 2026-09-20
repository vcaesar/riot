//  Copyright (c) 2020 The Bluge Authors.
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

package index

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// persistOneDoc indexes a single document and returns its path,
// the directory and the segment ids referenced by the last snapshot.
func persistOneDoc(t *testing.T, cfg *Config) (dir *FileSystemDirectory, live []uint64) {
	t.Helper()
	idx, err := OpenWriter(*cfg)
	if err != nil {
		t.Fatal(err)
	}
	b := NewBatch()
	b.Update(testIdentifier("1"), &FakeDocument{
		NewFakeField("_id", "1", true, false, false),
		NewFakeField("name", "test", false, false, true),
	})
	if err = idx.Batch(b); err != nil {
		t.Fatal(err)
	}
	if err = idx.Close(); err != nil {
		t.Fatal(err)
	}
	dir = cfg.DirectoryFunc().(*FileSystemDirectory)
	snapshots, err := dir.List(ItemKindSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) == 0 {
		t.Fatal("expected a persisted snapshot")
	}
	live, err = dir.List(ItemKindSegment)
	if err != nil {
		t.Fatal(err)
	}
	if len(live) == 0 {
		t.Fatal("expected a persisted segment")
	}
	return dir, live
}

func writeOrphanSegment(t *testing.T, dir *FileSystemDirectory, id uint64) string {
	t.Helper()
	path := filepath.Join(dir.path, dir.fileName(ItemKindSegment, id))
	if err := os.WriteFile(path, []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOpenWriterRemovesOrphanSegments(t *testing.T) {
	cfg, cleanup := CreateConfig("TestOpenWriterRemovesOrphanSegments")
	defer func() {
		if err := cleanup(); err != nil {
			t.Log(err)
		}
	}()
	dir, live := persistOneDoc(t, &cfg)

	// simulate a crash after persisting a segment but before the snapshot
	orphan := writeOrphanSegment(t, dir, live[0]+1)

	idx, err := OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = idx.Close() }()

	if _, err = os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan segment still on disk: %v", err)
	}
	for _, id := range live {
		if _, err = os.Stat(filepath.Join(dir.path, dir.fileName(ItemKindSegment, id))); err != nil {
			t.Fatalf("live segment %d removed: %v", id, err)
		}
	}
	if idx.nextSegmentID <= live[0]+1 {
		t.Fatalf("nextSegmentID %d must stay above orphan id %d", idx.nextSegmentID, live[0]+1)
	}
	reader, err := idx.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	count, err := reader.Count()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 doc after reopen, got %d", count)
	}
}

func TestOpenWriterKeepsSegmentsWhenSnapshotUnreadable(t *testing.T) {
	cfg, cleanup := CreateConfig("TestOpenWriterKeepsSegmentsWhenSnapshotUnreadable")
	defer func() {
		if err := cleanup(); err != nil {
			t.Log(err)
		}
	}()
	dir, live := persistOneDoc(t, &cfg)
	orphan := writeOrphanSegment(t, dir, live[0]+1)

	// corrupt an older snapshot so its segments cannot be proven orphaned
	snapshots, err := dir.List(ItemKindSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(dir.path, dir.fileName(ItemKindSnapshot, snapshots[0]-1))
	if err = os.WriteFile(broken, bytes.Repeat([]byte{0xff}, 16), 0o600); err != nil {
		t.Fatal(err)
	}

	idx, err := OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = idx.Close() }()

	if _, err = os.Stat(orphan); err != nil {
		t.Fatalf("segment removed although a snapshot was unreadable: %v", err)
	}
}
