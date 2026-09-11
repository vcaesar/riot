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

package index

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFileSystemDirectoryListAndStats(t *testing.T) {
	path := t.TempDir()
	dir := NewFileSystemDirectory(path)
	if ids, err := dir.List(".seg"); err != nil || len(ids) != 0 {
		t.Fatalf("empty list: %v, %v", ids, err)
	}
	if files, size := dir.Stats(); files != 0 || size != 0 {
		t.Fatalf("empty stats: %d, %d", files, size)
	}
	for name, data := range map[string]string{"2.seg": "abc", "10.seg": "12345", "ignored.txt": "x"} {
		if err := os.WriteFile(filepath.Join(path, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(path, "f.seg"), 0700); err != nil {
		t.Fatal(err)
	}
	ids, err := dir.List(".seg")
	if err != nil || !reflect.DeepEqual(ids, []uint64{16, 15, 2}) {
		t.Fatalf("list: got %v, %v; want [16 15 2]", ids, err)
	}
	if files, size := dir.Stats(); files != 3 || size != 9 {
		t.Fatalf("stats: got %d, %d; want 3, 9", files, size)
	}
	if err := os.WriteFile(filepath.Join(path, "invalid.seg"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if ids, err := dir.List(".seg"); err == nil || ids != nil {
		t.Fatalf("invalid identifier: got %v, %v", ids, err)
	}
}

func TestFileSystemDirectoryReadErrors(t *testing.T) {
	path := t.TempDir()
	file := filepath.Join(path, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"missing", "file"} {
		t.Run(name, func(t *testing.T) {
			dir := NewFileSystemDirectory(filepath.Join(path, name))
			if ids, err := dir.List(".seg"); err == nil || ids != nil {
				t.Fatalf("list: got %v, %v; want nil and error", ids, err)
			}
			if files, size := dir.Stats(); files != 0 || size != 0 {
				t.Fatalf("stats: got %d, %d; want zeros", files, size)
			}
		})
	}
}

func TestFileSystemDirectorySymlinkStats(t *testing.T) {
	path := t.TempDir()
	link := filepath.Join(path, "1.seg")
	if err := os.Symlink("missing-target", link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	dir := NewFileSystemDirectory(path)
	if files, size := dir.Stats(); files != 1 || size != uint64(info.Size()) {
		t.Fatalf("symlink stats: got %d, %d; want 1, %d", files, size, info.Size())
	}
	if ids, err := dir.List(".seg"); err != nil || !reflect.DeepEqual(ids, []uint64{1}) {
		t.Fatalf("symlink list: got %v, %v; want [1]", ids, err)
	}
}
