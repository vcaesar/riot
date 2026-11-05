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
	"testing"

	"github.com/vcaesar/riot/index"
)

// TestReaderBackupTo backs up twice into one directory and opens the result.
func TestReaderBackupTo(t *testing.T) {
	path := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, path)
	w, err := OpenWriter(DefaultConfig(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if err = w.Update(Identifier(id), NewDocument(id).AddField(NewTextField("name", "marty"))); err != nil {
			t.Fatal(err)
		}
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}

	backup := t.TempDir()
	dir := index.NewFileSystemDirectory(backup)
	if err = dir.Setup(false); err != nil {
		t.Fatal(err)
	}
	r, err := OpenReader(DefaultConfig(path))
	if err != nil {
		t.Fatal(err)
	}
	for round := range 2 {
		if err = r.BackupTo(dir, nil); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}

	r, err = OpenReader(DefaultConfig(backup))
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
	if n != 2 {
		t.Fatalf("backup has %d docs, want 2", n)
	}
}
