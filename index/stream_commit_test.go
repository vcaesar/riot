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
	"errors"
	"math"
	"os"
	"testing"
)

// TestStreamCommitPersistErrorKeepsIntroducedSegments checks that when the
// snapshot persist fails after the stream's segments were introduced, the
// commit error does not close the segments the root snapshot now owns nor
// remove their files: they stay searchable and the persister retries.
func TestStreamCommitPersistErrorKeepsIntroducedSegments(t *testing.T) {
	path, err := os.MkdirTemp("", "bluge-index-test-stream-persist-err")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(path) }()

	dir := &syncTrackingDirectory{Directory: NewFileSystemDirectory(path), syncErr: errors.New("sync failed")}
	cfg := DefaultConfigWithDirectory(func() Directory { return dir }).
		WithPersisterNapTimeMSec(1).
		WithNormCalc(func(_ string, numTerms int) float32 {
			//nolint:gosec // G115: encode the low 32 norm bits, matching the Float32bits decoder.
			return math.Float32frombits(uint32(numTerms))
		}).
		WithVirtualField(NewFakeField("", "", false, false, false))
	idx, err := OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = idx.Close() }()

	// a tiny budget flushes (and persists) a segment per document, so the
	// introduced segments are file-backed and sensitive to a stray Close
	st := idx.NewStream(1)
	for _, id := range []string{"1", "2", "3"} {
		if err = st.Insert(&FakeDocument{NewFakeField("_id", id, true, false, false)}); err != nil {
			t.Fatal(err)
		}
	}
	if err = st.Commit(); !errors.Is(err, dir.syncErr) {
		t.Fatalf("commit did not surface the persist failure: %v", err)
	}

	snapshot := idx.currentSnapshot()
	defer func() { _ = snapshot.Close() }()
	count, err := snapshot.Count()
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("introduced documents %d, want 3", count)
	}
	for _, seg := range snapshot.segment {
		if !seg.segment.Persisted() {
			continue
		}
		if _, _, err := dir.Load(ItemKindSegment, seg.id); err != nil {
			t.Fatalf("segment %d of the root was removed: %v", seg.id, err)
		}
		dict, err := seg.segment.Dictionary("_id")
		if err != nil {
			t.Fatalf("segment %d of the root is unreadable: %v", seg.id, err)
		}
		_ = dict.Close()
	}
}
