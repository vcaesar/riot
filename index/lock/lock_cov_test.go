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

package lock

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLockedFileAccessors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	tests := []struct {
		name      string
		open      func(string, int, os.FileMode) (LockedFile, error)
		exclusive bool
	}{
		{"exclusive", OpenExclusive, true},
		{"shared", OpenShared, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := tt.open(path, os.O_CREATE|os.O_RDWR, 0600)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if f.File() == nil || f.File().Name() != path {
				t.Errorf("unexpected file %v", f.File())
			}
			if f.Exclusive() != tt.exclusive {
				t.Errorf("Exclusive() = %v, want %v", f.Exclusive(), tt.exclusive)
			}
			if err := f.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}
			if err := f.Close(); err == nil {
				t.Fatalf("expected error closing twice")
			}
		})
	}
}

func TestOpenMissingDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "file")
	if _, err := OpenExclusive(path, os.O_RDWR, 0600); err == nil {
		t.Fatalf("expected error opening %s", path)
	}
}
