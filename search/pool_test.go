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

package search

import (
	"bytes"
	"testing"
)

func TestDocumentMatchPool(t *testing.T) {
	tooManyCalled := false

	// create a pool
	dmp := NewDocumentMatchPool(10, 0)
	dmp.TooSmall = func(_ *DocumentMatchPool) *DocumentMatch {
		tooManyCalled = true
		return &DocumentMatch{}
	}

	// get 10 instances without returning
	returned := make(DocumentMatchCollection, 10)

	for i := 0; i < 10; i++ {
		returned[i] = dmp.Get()
		if tooManyCalled {
			t.Fatal("too many function called before expected")
		}
	}

	// get one more and see if too many function is called
	extra := dmp.Get()
	if !tooManyCalled {
		t.Fatal("expected too many function to be called, but wasn't")
	}

	// return the first 10
	for i := 0; i < 10; i++ {
		dmp.Put(returned[i])
	}

	// check len and cap
	if len(dmp.avail) != 10 {
		t.Fatalf("expected 10 available, got %d", len(dmp.avail))
	}
	if cap(dmp.avail) != 10 {
		t.Fatalf("expected avail cap still 10, got %d", cap(dmp.avail))
	}

	// return the extra
	dmp.Put(extra)

	// check len and cap grown to 11
	if len(dmp.avail) != 11 {
		t.Fatalf("expected 11 available, got %d", len(dmp.avail))
	}
	// cap grows, but not by 1 (append behavior)
	if cap(dmp.avail) <= 10 {
		t.Fatalf("expected avail cap mpore than 10, got %d", cap(dmp.avail))
	}
}

// TestDocumentMatchPoolPreallocatesSortKeys: a fresh pooled match must be
// able to take a score sort key without allocating, and slots must not
// alias each other.
func TestDocumentMatchPoolPreallocatesSortKeys(t *testing.T) {
	order := SortOrder{SortBy(DocumentScore()).Desc(), SortBy(DocumentScore())}
	dmp := NewDocumentMatchPool(3, len(order))
	a, b := dmp.Get(), dmp.Get()
	a.Score, b.Score = 1, 2
	// the very first Complete must land in the preallocated slot: AllocsPerRun
	// warms up once, which would hide a too-small slot being grown
	slot0 := &sortSlot(a, 0)[:1][0]
	order.Complete(a)
	if &a.SortValue[0][0] != slot0 {
		t.Fatal("Complete on a fresh pooled match did not reuse its preallocated slot")
	}
	allocs := testing.AllocsPerRun(1, func() {
		order.Complete(a)
		order.Complete(b)
	})
	if allocs != 0 {
		t.Fatalf("Complete on fresh pooled matches allocated %v times", allocs)
	}
	if len(a.SortValue) != 2 || len(b.SortValue) != 2 {
		t.Fatalf("unexpected sort values %v %v", a.SortValue, b.SortValue)
	}
	if &a.SortValue[0][0] == &a.SortValue[1][0] || &a.SortValue[0][0] == &b.SortValue[0][0] {
		t.Fatal("sort key slots alias each other")
	}
	// a longer text key grows its own slot without touching the neighbors
	a.SortValue[0] = append(a.SortValue[0][:0], []byte("longer than ten bytes")...)
	if !bytes.Equal(b.SortValue[0], DocumentScore().Value(b)) {
		t.Fatalf("growing a's slot changed b's key: %v", b.SortValue[0])
	}
}
