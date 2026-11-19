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
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/vcaesar/riot/index/lock"
)

// covLockedFile hands out files in turn, the last one from then on, and
// fails Close with closeErr.
type covLockedFile struct {
	files    []*os.File
	n        int
	closeErr error
}

func (f *covLockedFile) File() *os.File {
	i := min(f.n, len(f.files)-1)
	f.n++
	return f.files[i]
}

func (f *covLockedFile) Exclusive() bool { return true }

func (f *covLockedFile) Close() error {
	for _, file := range f.files {
		_ = file.Close()
	}
	return f.closeErr
}

// covFiles opens, in dir, a writable file, a read-only one and a closed one.
func covFiles(t *testing.T, dir string) (rw, ro, closed *os.File) {
	t.Helper()
	path := filepath.Join(dir, "scratch")
	var err error
	if rw, err = os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600); err != nil {
		t.Fatal(err)
	}
	if ro, err = os.Open(path); err != nil {
		t.Fatal(err)
	}
	if closed, err = os.Open(path); err != nil {
		t.Fatal(err)
	}
	if err = closed.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = rw.Close()
		_ = ro.Close()
	})
	return rw, ro, closed
}

type covWriterTo []byte

func (b covWriterTo) WriteTo(w io.Writer, _ chan struct{}) (int64, error) {
	n, err := w.Write(b)
	return int64(n), err
}

func TestFileSystemDirectoryPersistLockErrors(t *testing.T) {
	type pick func(rw, ro, closed *os.File) *covLockedFile
	tests := []struct {
		name string
		file pick
	}{
		{"open", nil},
		{"truncate", func(_, ro, _ *os.File) *covLockedFile { return &covLockedFile{files: []*os.File{ro}} }},
		{"write", func(rw, ro, _ *os.File) *covLockedFile { return &covLockedFile{files: []*os.File{rw, ro}} }},
		{"sync", func(rw, _, closed *os.File) *covLockedFile { return &covLockedFile{files: []*os.File{rw, rw, closed}} }},
		{"close", func(rw, _, _ *os.File) *covLockedFile { return &covLockedFile{files: []*os.File{rw}, closeErr: errCov} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, op := range []string{"persist", "lock"} {
				path := t.TempDir()
				d := NewFileSystemDirectory(path)
				rw, ro, closed := covFiles(t, t.TempDir())
				d.openExclusive = func(string, int, os.FileMode) (lock.LockedFile, error) {
					if tt.file == nil {
						return nil, errCov
					}
					return tt.file(rw, ro, closed), nil
				}
				var err error
				if op == "persist" {
					err = d.Persist(ItemKindSegment, 1, covWriterTo("data"), nil)
				} else {
					err = d.Lock()
				}
				if op == "lock" && tt.name == "close" {
					// Lock keeps the pid file open
					if err != nil {
						t.Fatalf("lock: %v", err)
					}
					if err := d.Unlock(); err == nil {
						t.Fatal("expected unlock error")
					}
					continue
				}
				if err == nil {
					t.Fatalf("%s: expected error", op)
				}
			}
		})
	}
}

func TestFileSystemDirectorySetupErrors(t *testing.T) {
	if err := NewFileSystemDirectory(filepath.Join(t.TempDir(), "missing")).Setup(true); err == nil {
		t.Fatal("read-only setup of a missing directory succeeded")
	}
	if err := NewFileSystemDirectory("bad\x00path").Setup(false); err == nil {
		t.Fatal("setup of an invalid path succeeded")
	}
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permissions are not enforced")
	}
	d := NewFileSystemDirectory(filepath.Join(t.TempDir(), "a", "b"))
	d.newDirPerm = 0500
	if err := d.Setup(false); err == nil {
		t.Fatal("setup under an unwritable directory succeeded")
	}
}

func TestFileSystemDirectoryLoadMMapNever(t *testing.T) {
	path := t.TempDir()
	d := NewFileSystemDirectory(path)
	d.SetLoadMMapFunc(LoadMMapNever)
	if err := d.Persist(ItemKindSegment, 1, covWriterTo("data"), nil); err != nil {
		t.Fatal(err)
	}
	data, closer, err := d.Load(ItemKindSegment, 1)
	if err != nil {
		t.Fatal(err)
	}
	if data.Len() != 4 {
		t.Fatalf("len %d", data.Len())
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	_, _, closed := covFiles(t, t.TempDir())
	if _, _, err := LoadMMapNever(&covLockedFile{files: []*os.File{closed}}); err == nil {
		t.Fatal("expected error from a closed file")
	}
}

func TestFileSystemDirectoryLinkFromExisting(t *testing.T) {
	src, dst := NewFileSystemDirectory(t.TempDir()), NewFileSystemDirectory(t.TempDir())
	for _, d := range []*FileSystemDirectory{src, dst} {
		if err := d.Persist(ItemKindSegment, 2, covWriterTo("data"), nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := src.Persist(ItemKindSegment, 1, covWriterTo("data"), nil); err != nil {
		t.Fatal(err)
	}
	if !dst.linkFrom(src, ItemKindSegment, 1) {
		t.Skip("hard links unsupported")
	}
	// already linked by an earlier backup
	if !dst.linkFrom(src, ItemKindSegment, 1) {
		t.Fatal("relink of the same file failed")
	}
	// another file of the same name
	if dst.linkFrom(src, ItemKindSegment, 2) {
		t.Fatal("linked over a different file")
	}
}

func TestFileSystemDirectoryUnlockRemoveError(t *testing.T) {
	d := NewFileSystemDirectory("bad\x00path")
	d.pid = &covLockedFile{files: []*os.File{nil}}
	if err := d.Unlock(); err == nil {
		t.Fatal("expected error removing the pid file")
	}
}
