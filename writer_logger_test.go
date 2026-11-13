// Copyright (c) 2026 AtomAI
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

package riot

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A corrupt snapshot is logged through Config.Logger, not the std logger.
func TestOpenWriterUsesConfigLogger(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig(dir)
	w, err := OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Insert(NewDocument("1").AddField(NewKeywordField("name", "riot"))); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	snapshots, err := filepath.Glob(filepath.Join(dir, "*.snp"))
	if err != nil || len(snapshots) == 0 {
		t.Fatalf("no snapshot written: %v", err)
	}
	for _, s := range snapshots {
		if err = os.WriteFile(s, []byte("corrupt"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	var buf strings.Builder
	prev := log.Writer()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(prev) })

	cfg.Logger = log.New(&buf, "", 0)
	w, err = OpenWriter(cfg)
	if err == nil {
		_ = w.Close()
	}
	if !strings.Contains(buf.String(), "error loading snapshot epoch") {
		t.Fatalf("config logger got %q (open err %v)", buf.String(), err)
	}
}
