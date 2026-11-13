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

package index

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestWriterLogfUsesConfigLogger(t *testing.T) {
	var buf bytes.Buffer
	w := &Writer{config: Config{Logger: log.New(&buf, "", 0)}}
	w.logf("error loading snapshot epoch: %d: %v", 7, "boom")
	if got := buf.String(); !strings.Contains(got, "error loading snapshot epoch: 7: boom") {
		t.Fatalf("logger got %q", got)
	}
}

func TestWriterLogfDefaultsToStdLogger(t *testing.T) {
	var buf bytes.Buffer
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prev); log.SetFlags(flags) })

	(&Writer{}).logf("orphan %d", 3)
	if got := buf.String(); got != "orphan 3\n" {
		t.Fatalf("std logger got %q", got)
	}
}
