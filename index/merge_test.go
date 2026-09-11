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
	"sync"
	"sync/atomic"
	"testing"
)

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
	defer func() {
		err = idx.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()

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
}
