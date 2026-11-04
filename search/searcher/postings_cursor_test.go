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

package searcher

import (
	"slices"
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"
)

// batchList is a postings iterator over docs that records each NextBatch size.
type batchList struct {
	segment.PostingsIterator // only Count and NextBatch are used
	docs                     []uint64
	pos                      int
	asked                    []int
}

func (l *batchList) Count() uint64 { return uint64(len(l.docs)) }

func (l *batchList) NextBatch(docs []uint64, freqs []int, norms []float64) (int, error) {
	l.asked = append(l.asked, len(docs))
	n := copy(docs, l.docs[l.pos:])
	for k := range n {
		freqs[k], norms[k] = int(docs[k]%3)+1, float64(docs[k]%5)
	}
	l.pos += n
	return n, nil
}

// TestPostingsCursorBatchGrowth: batches start small and double up to
// postingsBatchSize (the last read finds the end), every posting is read
// once, and a released batch comes back from the pool empty.
func TestPostingsCursorBatchGrowth(t *testing.T) {
	list := &batchList{}
	for d := range uint64(500) {
		list.docs = append(list.docs, 3*d)
	}
	var c postingsCursor
	c.startBatch(list, false)
	var got []uint64
	for {
		p, err := c.next(list)
		if err != nil {
			t.Fatal(err)
		}
		if p == nil {
			break
		}
		if p.Frequency() != int(p.Number()%3)+1 || p.Norm() != float64(p.Number()%5) {
			t.Fatalf("posting %d: freq %d norm %v", p.Number(), p.Frequency(), p.Norm())
		}
		got = append(got, p.Number())
	}
	if !slices.Equal(got, list.docs) || c.seen != uint64(len(list.docs)) {
		t.Fatalf("read %d postings (seen %d), want %d", len(got), c.seen, len(list.docs))
	}
	if want := []int{16, 32, 64, 128, 128, 128, 128, 128}; !slices.Equal(list.asked, want) {
		t.Fatalf("batch sizes %v, want %v", list.asked, want)
	}

	b := c.batch
	c.release()
	if c.batch != nil || !c.noBatch {
		t.Fatal("release kept the batch")
	}
	c.release() // a second release is a no-op
	if b.n != 0 || b.pos != 0 || b.want != 0 || len(b.docs) != postingsBatchSize {
		t.Fatalf("released batch not reset: n=%d pos=%d want=%d len=%d", b.n, b.pos, b.want, len(b.docs))
	}

	empty := &batchList{}
	var e postingsCursor
	e.startBatch(empty, false)
	if e.batch != nil || !e.noBatch {
		t.Fatal("an empty list should not take a batch")
	}
}
