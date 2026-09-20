//  Copyright (c) 2020 Couchbase, Inc.
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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type discardTestDirectory struct {
	Directory
	remove func(string, uint64) error
}

func (d discardTestDirectory) Remove(kind string, id uint64) error {
	return d.remove(kind, id)
}

type discardTestCloser struct {
	closed bool
	err    error
}

func (c *discardTestCloser) Close() error {
	c.closed = true
	return c.err
}

func TestDiscardSegment(t *testing.T) {
	for _, name := range []string{"nil", "success", "close error", "remove error"} {
		t.Run(name, func(t *testing.T) {
			closer := &discardTestCloser{}
			if name == "close error" {
				closer.err = errors.New("close failed")
			}
			removed := false
			writer := &Writer{directory: discardTestDirectory{remove: func(kind string, id uint64) error {
				if !closer.closed {
					t.Fatal("segment removed before closing")
				}
				if kind != ItemKindSegment || id != 42 {
					t.Fatalf("unexpected removal: %s %d", kind, id)
				}
				removed = true
				if name == "remove error" {
					return errors.New("remove failed")
				}
				return nil
			}}}
			var seg *segmentWrapper
			if name != "nil" {
				seg = &segmentWrapper{refCounter: &closeOnLastRefCounter{closer: closer, refs: 1}}
			}
			err := writer.discardSegment(seg, 42)
			switch name {
			case "nil", "success":
				if err != nil {
					t.Fatal(err)
				}
			case "close error":
				if err == nil || !strings.Contains(err.Error(), "close failed") {
					t.Fatalf("expected close error, got %v", err)
				}
			case "remove error":
				if err == nil || !strings.Contains(err.Error(), "remove failed") {
					t.Fatalf("expected remove error, got %v", err)
				}
			}
			if removed != (name == "success" || name == "remove error") {
				t.Fatalf("unexpected removal: %v", removed)
			}
		})
	}
}

func TestObsoleteSegmentMergeIntroduction(t *testing.T) {
	cfg, cleanup := CreateConfig("TestObsoleteSegmentMergeIntroduction")
	var introComplete, mergeIntroStart, mergeIntroComplete sync.WaitGroup
	introComplete.Add(1)
	mergeIntroStart.Add(1)
	mergeIntroComplete.Add(1)
	var segIntroCompleted int
	cfg.EventCallback = func(e Event) {
		switch e.Kind {
		case EventKindBatchIntroduction:
			segIntroCompleted++
			if segIntroCompleted == 3 {
				// all 3 segments introduced
				introComplete.Done()
			}
		case EventKindMergeTaskIntroductionStart:
			// signal the start of merge task introduction so that
			// we can introduce a new batch which obsoletes the
			// merged segment's contents.
			mergeIntroStart.Done()
			// hold the merge task introduction until the merged segment contents
			// are obsoleted with the next batch/segment introduction.
			introComplete.Wait()
		case EventKindMergeTaskIntroduction:
			// signal the completion of the merge task introduction.
			mergeIntroComplete.Done()
		}
	}

	defer func() {
		err := cleanup()
		if err != nil {
			t.Log(err)
		}
	}()

	idx, err := OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// first introduce two documents over two batches.
	batch := NewBatch()
	doc := &FakeDocument{
		NewFakeField("_id", "1", true, false, false),
		NewFakeField("name", "test3", true, false, true),
	}
	doc.FakeComposite("_all", nil)
	batch.Update(testIdentifier("1"), doc)
	err = idx.Batch(batch)
	if err != nil {
		t.Error(err)
	}

	batch.Reset()
	doc = &FakeDocument{
		NewFakeField("_id", "2", true, false, false),
		NewFakeField("name", "test2updated", true, false, true),
	}
	doc.FakeComposite("_all", nil)
	batch.Update(testIdentifier("2"), doc)
	err = idx.Batch(batch)
	if err != nil {
		t.Error(err)
	}

	// wait until the merger trying to introduce the new merged segment.
	mergeIntroStart.Wait()

	// execute another batch which obsoletes the contents of the new merged
	// segment awaiting introduction.
	batch.Reset()
	batch.Delete(testIdentifier("1"))
	batch.Delete(testIdentifier("2"))
	doc = &FakeDocument{
		NewFakeField("_id", "3", true, false, false),
		NewFakeField("name", "test3updated", true, false, true),
	}
	doc.FakeComposite("_all", nil)
	batch.Update(testIdentifier("3"), doc)
	err = idx.Batch(batch)
	if err != nil {
		t.Error(err)
	}

	// wait until the merge task introduction complete.
	mergeIntroComplete.Wait()

	idxr, err := idx.Reader()
	if err != nil {
		t.Error(err)
	}

	numSegments := len(idxr.segment)
	if numSegments != 1 {
		t.Errorf("expected one segment at the root, got: %d", numSegments)
	}

	skipIntroCount := atomic.LoadUint64(&idxr.parent.stats.TotFileMergeIntroductionsObsoleted)
	if skipIntroCount != 1 {
		t.Errorf("expected one obsolete merge segment skipping the introduction, got: %d", skipIntroCount)
	}

	docCount, err := idxr.Count()
	if err != nil {
		t.Fatal(err)
	}
	if docCount != 1 {
		t.Errorf("Expected document count to be %d got %d", 1, docCount)
	}

	err = idxr.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = idx.Close()
	if err != nil {
		t.Fatal(err)
	}

	// reopening runs the deletion policy cleanup, so afterwards every segment
	// file on disk must be referenced by the root snapshot; the skipped merged
	// segment was never in a snapshot and would otherwise leak forever.
	cfg.EventCallback = nil
	idx, err = OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err = idx.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()
	onDisk, err := idx.directory.List(ItemKindSegment)
	if err != nil {
		t.Fatal(err)
	}
	root := idx.currentSnapshot()
	defer func() { _ = root.Close() }()
	live := map[uint64]struct{}{}
	for _, seg := range root.segment {
		live[seg.id] = struct{}{}
	}
	for _, id := range onDisk {
		if _, ok := live[id]; !ok {
			t.Errorf("stale segment file %d not referenced by any snapshot", id)
		}
	}
}

// TestMergeReclaimsOverDeletedSegment verifies a lone persisted segment whose
// deletions exceed DeletesRatioBeforeMerge is rewritten without its tombstones.
func TestMergeReclaimsOverDeletedSegment(t *testing.T) {
	cfg, cleanup := CreateConfig("TestMergeReclaimsOverDeletedSegment")
	defer func() {
		if err := cleanup(); err != nil {
			t.Log(err)
		}
	}()
	cfg.MergePlanOptions.DeletesRatioBeforeMerge = 0.5
	merged := make(chan struct{}, 16)
	cfg.EventCallback = func(e Event) {
		if e.Kind == EventKindMergeTaskIntroduction {
			merged <- struct{}{}
		}
	}

	idx, err := OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = idx.Close() }()

	const numDocs = 10
	batch := NewBatch()
	for i := 0; i < numDocs; i++ {
		id := strconv.Itoa(i)
		doc := &FakeDocument{
			NewFakeField("_id", id, true, false, false),
			NewFakeField("name", "test", true, false, true),
		}
		doc.FakeComposite("_all", nil)
		batch.Update(testIdentifier(id), doc)
	}
	if err = idx.Batch(batch); err != nil {
		t.Fatal(err)
	}
	// wait for the segment to be persisted before deleting, so the
	// deletions land on a persisted segment the merger can see
	waitForPersisted := func() {
		for i := 0; i < 500; i++ {
			snap := idx.currentSnapshot()
			persisted := len(snap.segment) > 0
			for _, seg := range snap.segment {
				persisted = persisted && seg.segment.Persisted()
			}
			_ = snap.Close()
			if persisted {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("segments never persisted")
	}
	waitForPersisted()

	batch = NewBatch()
	for i := 0; i < numDocs-2; i++ {
		batch.Delete(testIdentifier(strconv.Itoa(i)))
	}
	if err = idx.Batch(batch); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(10 * time.Second)
	for {
		snap := idx.currentSnapshot()
		clean := len(snap.segment) == 1 && snap.segment[0].segment.Count() == 2 &&
			snap.segment[0].Count() == 2
		_ = snap.Close()
		if clean {
			return
		}
		select {
		case <-merged:
		case <-deadline:
			snap = idx.currentSnapshot()
			for _, seg := range snap.segment {
				t.Logf("segment %d count=%d live=%d persisted=%v", seg.id, seg.segment.Count(), seg.Count(), seg.segment.Persisted())
			}
			_ = snap.Close()
			t.Fatal("over-deleted segment was never rewritten")
		}
	}
}
