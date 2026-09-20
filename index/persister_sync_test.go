//  Copyright (c) 2020 The Bluge Authors.
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
	"sync"
	"testing"
)

// syncTrackingDirectory records the order of snapshot persists and directory syncs.
type syncTrackingDirectory struct {
	Directory
	mu      sync.Mutex
	events  []string
	syncErr error
}

func (d *syncTrackingDirectory) Persist(kind string, id uint64, w WriterTo, closeCh chan struct{}) error {
	err := d.Directory.Persist(kind, id, w, closeCh)
	if err == nil && kind == ItemKindSnapshot {
		d.record("snapshot")
	}
	return err
}

func (d *syncTrackingDirectory) Sync() error {
	if d.syncErr != nil {
		return d.syncErr
	}
	d.record("sync")
	return d.Directory.Sync()
}

func (d *syncTrackingDirectory) record(event string) {
	d.mu.Lock()
	d.events = append(d.events, event)
	d.mu.Unlock()
}

func (d *syncTrackingDirectory) snapshotEvents() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.events...)
}

func TestPersistSnapshotSyncsDirectory(t *testing.T) {
	path, err := os.MkdirTemp("", "bluge-index-test-sync")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(path) }()

	for _, name := range []string{"ordered", "sync error"} {
		t.Run(name, func(t *testing.T) {
			dir := &syncTrackingDirectory{Directory: NewFileSystemDirectory(path)}
			if name == "sync error" {
				dir.syncErr = errors.New("sync failed")
			}
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
			b := NewBatch()
			b.Update(testIdentifier("1"), &FakeDocument{
				NewFakeField("_id", "1", true, false, false),
				NewFakeField("name", "test", false, false, true),
			})
			err = idx.Batch(b)
			if name == "sync error" {
				if !errors.Is(err, dir.syncErr) {
					t.Fatalf("batch did not surface sync failure: %v", err)
				}
				_ = idx.Close()
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = idx.Close(); err != nil {
				t.Fatal(err)
			}

			events := dir.snapshotEvents()
			if len(events) < 2 {
				t.Fatalf("expected snapshot persist followed by sync, got %v", events)
			}
			for i, event := range events {
				if event == "snapshot" && (i+1 >= len(events) || events[i+1] != "sync") {
					t.Fatalf("snapshot persist at %d not followed by directory sync: %v", i, events)
				}
			}
		})
	}
}
