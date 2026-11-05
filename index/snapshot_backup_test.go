//  Copyright (c) 2026 The AtomAI.
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
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

func writeDocs(t *testing.T, cfg Config, ids ...string) {
	t.Helper()
	idx, err := OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		b := NewBatch()
		b.Update(testIdentifier(id), &FakeDocument{NewFakeField("_id", id, true, false, false)})
		if err = idx.Batch(b); err != nil {
			t.Fatal(err)
		}
	}
	if err = idx.Close(); err != nil {
		t.Fatal(err)
	}
}

func backupDir(t *testing.T) (d *FileSystemDirectory, path string) {
	t.Helper()
	path = t.TempDir()
	d = NewFileSystemDirectory(path)
	if err := d.Setup(false); err != nil {
		t.Fatal(err)
	}
	return d, path
}

func countBackup(t *testing.T, path string) uint64 {
	t.Helper()
	r, err := OpenReader(DefaultConfig(path))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	}()
	n, err := r.Count()
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func sameFile(t *testing.T, a, b string) bool {
	t.Helper()
	ai, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	bi, err := os.Stat(b)
	if err != nil {
		t.Fatal(err)
	}
	return os.SameFile(ai, bi)
}

// TestBackupLinksPersistedSegments checks that a backup hard links the
// persisted segments instead of copying them, also into a directory an
// earlier backup already linked, and still opens as a full index.
func TestBackupLinksPersistedSegments(t *testing.T) {
	cfg, cleanup := CreateConfig("TestBackupLinksPersistedSegments")
	t.Cleanup(func() {
		if err := cleanup(); err != nil {
			t.Log(err)
		}
	})
	writeDocs(t, cfg, "1", "2", "3")
	src := cfg.DirectoryFunc().(*FileSystemDirectory)

	r, err := OpenReader(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	}()
	if len(r.segment) == 0 {
		t.Fatal("no segments to back up")
	}

	dst, path := backupDir(t)
	for round := range 2 {
		if err = r.Backup(dst, nil); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		for _, s := range r.segment {
			name := src.fileName(ItemKindSegment, s.id)
			if !sameFile(t, filepath.Join(src.path, name), filepath.Join(path, name)) {
				t.Fatalf("round %d: segment %d copied, want hard link", round, s.id)
			}
		}
		name := src.fileName(ItemKindSnapshot, r.epoch)
		if sameFile(t, filepath.Join(src.path, name), filepath.Join(path, name)) {
			t.Fatal("snapshot linked, want a copy")
		}
	}
	if n := countBackup(t, path); n != 3 {
		t.Fatalf("backup has %d docs, want 3", n)
	}
}

// TestBackupCopiesUnlinkableSegments checks the copy fallback: segments of
// an in memory index have no file to link.
func TestBackupCopiesUnlinkableSegments(t *testing.T) {
	idx, err := OpenWriter(InMemoryOnlyConfig().WithNormCalc(func(string, int) float32 { return 1 }))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := idx.Close(); err != nil {
			t.Error(err)
		}
	}()
	b := NewBatch()
	b.Update(testIdentifier("1"), &FakeDocument{NewFakeField("_id", "1", true, false, false)})
	if err = idx.Batch(b); err != nil {
		t.Fatal(err)
	}
	r, err := idx.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	}()
	dst, path := backupDir(t)
	if err = r.Backup(dst, nil); err != nil {
		t.Fatal(err)
	}
	if n := countBackup(t, path); n != 1 {
		t.Fatalf("backup has %d docs, want 1", n)
	}
}

// TestLinkFromMissingItem checks that a gone item is reported for copying.
func TestLinkFromMissingItem(t *testing.T) {
	src, _ := backupDir(t)
	dst, _ := backupDir(t)
	if dst.linkFrom(src, ItemKindSegment, 1) {
		t.Fatal("linked a missing segment")
	}
}

// copyOnlyDirectory hides the file system type, so Backup copies segments.
type copyOnlyDirectory struct{ *FileSystemDirectory }

func BenchmarkBackup(b *testing.B) {
	cfg, cleanup := CreateConfig("BenchmarkBackup")
	b.Cleanup(func() {
		if err := cleanup(); err != nil {
			b.Log(err)
		}
	})
	idx, err := OpenWriter(cfg)
	if err != nil {
		b.Fatal(err)
	}
	for i := range 200 {
		batch := NewBatch()
		for j := range 500 {
			id := strconv.Itoa(i*500 + j)
			batch.Update(testIdentifier(id), &FakeDocument{
				NewFakeField("_id", id, true, false, false),
				NewFakeField("body", "document "+id+" about search engines and replication", true, false, false),
			})
		}
		if err = idx.Batch(batch); err != nil {
			b.Fatal(err)
		}
	}
	if err = idx.Close(); err != nil {
		b.Fatal(err)
	}
	r, err := OpenReader(cfg)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := r.Close(); err != nil {
			b.Log(err)
		}
	})

	for _, mode := range []string{"copy", "link"} {
		b.Run(mode, func(b *testing.B) {
			for b.Loop() {
				d := NewFileSystemDirectory(b.TempDir())
				if err := d.Setup(false); err != nil {
					b.Fatal(err)
				}
				var dir Directory = d
				if mode == "copy" {
					dir = copyOnlyDirectory{d}
				}
				if err := r.Backup(dir, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}

	// a copy remote that already holds the segments only gets the snapshot
	b.Run("incremental", func(b *testing.B) {
		d := NewFileSystemDirectory(b.TempDir())
		if err := d.Setup(false); err != nil {
			b.Fatal(err)
		}
		dir := copyOnlyDirectory{d}
		if err := r.Backup(dir, nil); err != nil {
			b.Fatal(err)
		}
		for b.Loop() {
			if err := r.Backup(dir, nil); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// TestBackupSegmentFileRemoved checks that a segment whose file the deletion
// policy removed after a merge is still backed up from the open snapshot.
func TestBackupSegmentFileRemoved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows cannot remove a segment file the snapshot maps")
	}
	cfg, cleanup := CreateConfig("TestBackupSegmentFileRemoved")
	t.Cleanup(func() {
		if err := cleanup(); err != nil {
			t.Log(err)
		}
	})
	writeDocs(t, cfg, "1", "2", "3")
	src := cfg.DirectoryFunc().(*FileSystemDirectory)
	r, err := OpenReader(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	}()
	for _, s := range r.segment {
		if err = os.Remove(filepath.Join(src.path, src.fileName(ItemKindSegment, s.id))); err != nil {
			t.Fatal(err)
		}
	}
	dst, path := backupDir(t)
	if err = r.Backup(dst, nil); err != nil {
		t.Fatal(err)
	}
	if n := countBackup(t, path); n != 3 {
		t.Fatalf("backup has %d docs, want 3", n)
	}
}

// TestBackupSkipsGraphBeingPersisted checks that a graph still locked by
// Persist is neither linked half written nor fails the backup: it is a
// cache the restored index rebuilds.
func TestBackupSkipsGraphBeingPersisted(t *testing.T) {
	cfg, cleanup := CreateConfig("TestBackupSkipsGraphBeingPersisted")
	t.Cleanup(func() {
		if err := cleanup(); err != nil {
			t.Log(err)
		}
	})
	writeDocs(t, cfg, "1")
	src := cfg.DirectoryFunc().(*FileSystemDirectory)
	r, err := OpenReader(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	}()
	graph := r.segment[0].id<<graphKeyBits | 1
	f, err := src.openExclusive(filepath.Join(src.path, src.fileName(ItemKindGraph, graph)), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err = f.File().WriteString("half"); err != nil {
		t.Fatal(err)
	}

	dst, path := backupDir(t)
	if err = r.Backup(dst, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(path, dst.fileName(ItemKindGraph, graph))); !os.IsNotExist(err) {
		t.Fatalf("graph being persisted was backed up: %v", err)
	}
	if n := countBackup(t, path); n != 1 {
		t.Fatalf("backup has %d docs, want 1", n)
	}
}
