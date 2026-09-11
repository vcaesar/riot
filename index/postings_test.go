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
