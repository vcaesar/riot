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

//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package index

import (
	"path/filepath"
	"testing"
)

func TestFileSystemDirectorySyncMissing(t *testing.T) {
	d := NewFileSystemDirectory(filepath.Join(t.TempDir(), "missing"))
	if err := d.Sync(); err == nil {
		t.Fatal("expected error syncing a missing directory")
	}
}
