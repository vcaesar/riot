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
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/ice/vec"

	"github.com/vcaesar/riot/hnsw"
)

// countingDirectory counts persisted items by kind; it hides the file
// system type, so Backup copies instead of linking.
type countingDirectory struct {
	Directory
	mu       sync.Mutex
	persists map[string]int
}

func newCountingDirectory(d Directory) *countingDirectory {
	return &countingDirectory{Directory: d, persists: make(map[string]int)}
}

func (d *countingDirectory) Persist(kind string, id uint64, w WriterTo, closeCh chan struct{}) error {
	d.mu.Lock()
	d.persists[kind]++
	d.mu.Unlock()
	return d.Directory.Persist(kind, id, w, closeCh)
}

func (d *countingDirectory) count(kind string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.persists[kind]
}

func openTestReader(t *testing.T, cfg Config) *Snapshot {
	t.Helper()
	r, err := OpenReader(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	return r
}

func segmentIDs(r *Snapshot) map[uint64]struct{} {
	ids := make(map[uint64]struct{}, len(r.segment))
	for _, s := range r.segment {
		ids[s.id] = struct{}{}
	}
	return ids
}

// TestBackupIncremental checks that backing up into the same remote again
// copies only the segments it does not hold yet.
func TestBackupIncremental(t *testing.T) {
	cfg, cleanup := CreateConfig("TestBackupIncremental")
	t.Cleanup(func() {
		if err := cleanup(); err != nil {
			t.Log(err)
		}
	})
	writeDocs(t, cfg, "1", "2", "3")
	dst, path := backupDir(t)

	first := openTestReader(t, cfg)
	remote := newCountingDirectory(dst)
	if err := first.Backup(remote, nil); err != nil {
		t.Fatal(err)
	}
	if got, want := remote.count(ItemKindSegment), len(first.segment); got != want {
		t.Fatalf("first backup persisted %d segments, want %d", got, want)
	}

	writeDocs(t, cfg, "4")
	second := openTestReader(t, cfg)
	old := segmentIDs(first)
	added := 0
	for _, s := range second.segment {
		if _, ok := old[s.id]; !ok {
			added++
		}
	}
	remote = newCountingDirectory(dst)
	if err := second.Backup(remote, nil); err != nil {
		t.Fatal(err)
	}
	if got := remote.count(ItemKindSegment); got != added {
		t.Fatalf("incremental backup persisted %d segments, want the %d new ones", got, added)
	}
	if remote.count(ItemKindSnapshot) != 1 {
		t.Fatal("snapshot not written")
	}
	if n := countBackup(t, path); n != 4 {
		t.Fatalf("backup has %d docs, want 4", n)
	}

	remote = newCountingDirectory(dst)
	if err := second.Backup(remote, nil); err != nil {
		t.Fatal(err)
	}
	if got := remote.count(ItemKindSegment); got != 0 {
		t.Fatalf("repeated backup persisted %d segments, want 0", got)
	}
}

// TestBackupReplacesMismatchedItem checks that a remote item with a reused
// id but other content is replaced without truncating a file it is linked to.
func TestBackupReplacesMismatchedItem(t *testing.T) {
	cfg, cleanup := CreateConfig("TestBackupReplacesMismatchedItem")
	t.Cleanup(func() {
		if err := cleanup(); err != nil {
			t.Log(err)
		}
	})
	writeDocs(t, cfg, "1", "2")
	r := openTestReader(t, cfg)
	dst, path := backupDir(t)

	outside := filepath.Join(t.TempDir(), "other-index.seg")
	junk := []byte("segment of another index")
	if err := os.WriteFile(outside, junk, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, s := range r.segment {
		if err := os.Link(outside, filepath.Join(path, dst.fileName(ItemKindSegment, s.id))); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Backup(dst, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(outside); err != nil || !bytes.Equal(got, junk) {
		t.Fatalf("linked file changed: %q %v", got, err)
	}
	if n := countBackup(t, path); n != 2 {
		t.Fatalf("backup has %d docs, want 2", n)
	}
}

func waitGraphItems(t *testing.T, d Directory) []uint64 {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		ids, err := d.List(ItemKindGraph)
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) > 0 {
			return ids
		}
		if time.Now().After(deadline) {
			t.Fatal("no vector graph persisted")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestBackupVectorGraphs checks that persisted graphs are backed up, once.
func TestBackupVectorGraphs(t *testing.T) {
	cfg, cleanup := CreateConfig("TestBackupVectorGraphs")
	t.Cleanup(func() {
		if err := cleanup(); err != nil {
			t.Log(err)
		}
	})
	cfg = cfg.WithVectorIndex("v", vec.L2, hnsw.Params{})
	idx, err := OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	b := NewBatch()
	for i := range 10 {
		encoded, err := vec.Encode([]float32{float32(i), 1})
		if err != nil {
			t.Fatal(err)
		}
		id := strconv.Itoa(i)
		b.Update(testIdentifier(id), &FakeDocument{
			NewFakeField("_id", id, true, false, false),
			NewFakeField("v", string(encoded), true, false, false),
		})
	}
	if err = idx.Batch(b); err != nil {
		t.Fatal(err)
	}
	waitGraphItems(t, idx.directory)
	if err = idx.Close(); err != nil {
		t.Fatal(err)
	}

	r := openTestReader(t, cfg)
	var want []uint64
	ids, err := cfg.DirectoryFunc().List(ItemKindGraph)
	if err != nil {
		t.Fatal(err)
	}
	live := segmentIDs(r)
	for _, id := range ids {
		if _, ok := live[id>>graphKeyBits]; ok {
			want = append(want, id)
		}
	}
	if len(want) == 0 {
		t.Fatal("no graph of a live segment")
	}

	dst, _ := backupDir(t)
	remote := newCountingDirectory(dst)
	if err = r.Backup(remote, nil); err != nil {
		t.Fatal(err)
	}
	got, err := dst.List(ItemKindGraph)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("backed up graphs %x, want %x", got, want)
	}

	remote = newCountingDirectory(dst)
	if err = r.Backup(remote, nil); err != nil {
		t.Fatal(err)
	}
	if remote.count(ItemKindGraph) != 0 || remote.count(ItemKindSegment) != 0 {
		t.Fatalf("unchanged backup copied %v", remote.persists)
	}
}

func TestSameItem(t *testing.T) {
	put := func(d Directory, b []byte) {
		t.Helper()
		if err := d.Persist(ItemKindSegment, 1, dataWriter{segment.NewDataBytes(b)}, nil); err != nil {
			t.Fatal(err)
		}
	}
	long := bytes.Repeat([]byte("x"), 2*backupTail)
	tests := []struct {
		name string
		a, b []byte
		want bool
	}{
		{"equal", long, long, true},
		{"length", long, long[1:], false},
		{"tail", long, append(bytes.Clone(long[:len(long)-1]), 'y'), false},
		{"short", []byte("ab"), []byte("ab"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, b := NewInMemoryDirectory(), NewInMemoryDirectory()
			put(a, tt.a)
			put(b, tt.b)
			if got := sameItem(a, b, ItemKindSegment, 1); got != tt.want {
				t.Fatalf("sameItem = %v, want %v", got, tt.want)
			}
		})
	}
	if sameItem(NewInMemoryDirectory(), NewInMemoryDirectory(), ItemKindSegment, 1) {
		t.Fatal("missing items match")
	}
}
