//  Copyright (c) 2026 The Bluge Authors.
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

import "testing"

// TestPostingsIteratorAdvanceBackwardDoesNotRecycleSelf guards against the
// backward-seek path handing the live iterator to the snapshot's free list,
// where the next PostingsIterator call on the same field would reuse it.
func TestPostingsIteratorAdvanceBackwardDoesNotRecycleSelf(t *testing.T) {
	cfg, cleanup := CreateConfig("TestPostingsIteratorAdvanceBackward")
	defer func() {
		if err := cleanup(); err != nil {
			t.Log(err)
		}
	}()

	idx, err := OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := idx.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	b := NewBatch()
	for _, id := range []string{"1", "2", "3"} {
		b.Update(testIdentifier(id), &FakeDocument{
			NewFakeField("_id", id, true, false, false),
			NewFakeField("name", "test", false, false, true),
		})
	}
	if err = idx.Batch(b); err != nil {
		t.Fatal(err)
	}

	reader, err := idx.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	first, err := reader.PostingsIterator([]byte("test"), "name", true, true, false)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	var numbers []uint64
	p, err := first.Next()
	for err == nil && p != nil {
		numbers = append(numbers, p.Number())
		p, err = first.Next()
	}
	if err != nil || len(numbers) != 3 {
		t.Fatalf("expected 3 postings, got %v (err %v)", numbers, err)
	}

	// seek back to the start
	p, err = first.Advance(numbers[0])
	if err != nil || p == nil || p.Number() != numbers[0] {
		t.Fatalf("backward advance returned %v, %v", p, err)
	}

	reader.m2.Lock()
	for _, pooled := range reader.fieldTFRs["name"] {
		if pooled == first {
			reader.m2.Unlock()
			t.Fatal("iterator still in use was placed in the recycle pool")
		}
	}
	reader.m2.Unlock()

	// a second iterator must be a distinct object and not disturb the first
	second, err := reader.PostingsIterator([]byte("test"), "name", true, true, false)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if second == first {
		t.Fatal("second iterator aliases the first")
	}
	for i := 1; i < len(numbers); i++ {
		p, err = first.Next()
		if err != nil || p == nil || p.Number() != numbers[i] {
			t.Fatalf("after backward advance, posting %d = %v (err %v), want %d", i, p, err, numbers[i])
		}
	}
}

func pooledPostingsIterators(t *testing.T, snapshot *Snapshot, field string) int {
	t.Helper()
	snapshot.m2.Lock()
	defer snapshot.m2.Unlock()
	return len(snapshot.fieldTFRs[field])
}

// TestPostingsIteratorRecycledOnStaleAndReadOnlySnapshots: a Reader keeps
// its snapshot for its lifetime, so iterators must be pooled even after the
// writer moved on, and on a snapshot opened by OpenReader (which has no root).
func TestPostingsIteratorRecycledOnStaleAndReadOnlySnapshots(t *testing.T) {
	cfg, cleanup := CreateConfig("TestPostingsIteratorRecycled")
	defer func() {
		if err := cleanup(); err != nil {
			t.Log(err)
		}
	}()

	idx, err := OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	batch := func(id string) {
		b := NewBatch()
		b.Update(testIdentifier(id), &FakeDocument{
			NewFakeField("_id", id, true, false, false),
			NewFakeField("name", "test", false, false, true),
		})
		if err := idx.Batch(b); err != nil {
			t.Fatal(err)
		}
	}
	batch("1")

	reader, err := idx.Reader()
	if err != nil {
		t.Fatal(err)
	}
	batch("2") // reader now holds a stale snapshot

	iterateAndClose := func(snapshot *Snapshot) {
		itr, err := snapshot.PostingsIterator([]byte("test"), "name", true, true, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = itr.Next(); err != nil {
			t.Fatal(err)
		}
		if err = itr.Close(); err != nil {
			t.Fatal(err)
		}
	}
	iterateAndClose(reader)
	if n := pooledPostingsIterators(t, reader, "name"); n != 1 {
		t.Fatalf("stale snapshot pooled %d iterators, want 1", n)
	}
	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err = idx.Close(); err != nil {
		t.Fatal(err)
	}

	ro, err := OpenReader(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := ro.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	iterateAndClose(ro)
	if n := pooledPostingsIterators(t, ro, "name"); n != 1 {
		t.Fatalf("read-only snapshot pooled %d iterators, want 1", n)
	}
	// and the pooled iterator is handed back out rather than growing the pool
	iterateAndClose(ro)
	if n := pooledPostingsIterators(t, ro, "name"); n != 1 {
		t.Fatalf("read-only snapshot pooled %d iterators after reuse, want 1", n)
	}
}

// TestSnapshotCollectionStatsDoesNotMutateSegmentStats: segments may return
// shared stats objects, so merging across segments must not write into them.
func TestSnapshotCollectionStatsDoesNotMutateSegmentStats(t *testing.T) {
	cfg, cleanup := CreateConfig("TestSnapshotCollectionStats")
	// keep both batches as separate segments: no in-memory merge, and the
	// default merge plan budget (10 per tier) never merges two
	cfg.MinSegmentsForInMemoryMerge = 3
	defer func() {
		if err := cleanup(); err != nil {
			t.Log(err)
		}
	}()

	idx, err := OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := idx.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	for _, id := range []string{"1", "2"} { // two batches: two segments
		b := NewBatch()
		b.Update(testIdentifier(id), &FakeDocument{
			NewFakeField("_id", id, true, false, false),
			NewFakeField("name", "test", false, false, true),
		})
		if err := idx.Batch(b); err != nil {
			t.Fatal(err)
		}
	}

	reader, err := idx.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	if len(reader.segment) < 2 {
		t.Fatalf("expected at least 2 segments, got %d", len(reader.segment))
	}
	for i := 0; i < 3; i++ {
		stats, err := reader.CollectionStats("name")
		if err != nil {
			t.Fatal(err)
		}
		if stats.TotalDocumentCount() != 2 || stats.DocumentCount() != 2 || stats.SumTotalTermFrequency() != 2 {
			t.Fatalf("call %d: stats = (%d, %d, %d), want (2, 2, 2)", i,
				stats.TotalDocumentCount(), stats.DocumentCount(), stats.SumTotalTermFrequency())
		}
	}
}

// TestSnapshotCollectionStatsEmpty: with no segments there are no stats,
// and the result must be a nil interface (similarity treats nil as "no
// field"), not a typed nil pointer.
func TestSnapshotCollectionStatsEmpty(t *testing.T) {
	cfg, cleanup := CreateConfig("TestSnapshotCollectionStatsEmpty")
	defer func() {
		if err := cleanup(); err != nil {
			t.Log(err)
		}
	}()
	idx, err := OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := idx.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	reader, err := idx.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	stats, err := reader.CollectionStats("name")
	if err != nil {
		t.Fatal(err)
	}
	if stats != nil {
		t.Fatalf("empty snapshot stats = %#v, want nil", stats)
	}
}
