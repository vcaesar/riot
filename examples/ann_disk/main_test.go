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

package main

import (
	"strings"
	"testing"

	riot "github.com/vcaesar/riot"
)

const want = "ANN: north (1.000)\nANN: northeast (0.707)\n"

func TestRun(t *testing.T) {
	var out strings.Builder
	if err := run(&out, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if out.String() != want {
		t.Fatalf("got %q, want %q", out.String(), want)
	}
}

// Both storages answer the same after the index is reopened.
func TestSearchStorages(t *testing.T) {
	for _, storage := range []riot.VectorStorage{riot.VectorStorageDisk, riot.VectorStorageMemory} {
		config := riot.DefaultConfig(t.TempDir()).WithVectorStorage(storage)
		if err := index(config); err != nil {
			t.Fatal(err)
		}
		var out strings.Builder
		if err := search(&out, config); err != nil {
			t.Fatal(err)
		}
		if out.String() != want {
			t.Fatalf("storage %d: got %q, want %q", storage, out.String(), want)
		}
	}
}
