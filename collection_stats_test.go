// Copyright (c) 2026 The AtomAI.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package riot

import "testing"

// A one-segment snapshot returns its segment's stats directly: they must
// equal the merged stats of the same documents in several segments, and
// merging into a returned value must not change the next one.
func TestCollectionStatsOneSegment(t *testing.T) {
	const n = 3000
	stats := func(config Config) (total, docs, freq uint64, segments int) {
		w := openConfigWriter(t, config)
		b := NewBatch()
		for i := range n {
			b.Insert(streamTestDoc(i))
		}
		if err := w.Batch(b); err != nil {
			t.Fatal(err)
		}
		r, err := w.Reader()
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := r.Close(); err != nil {
				t.Error(err)
			}
		}()
		cs, err := r.reader.CollectionStats("body")
		if err != nil {
			t.Fatal(err)
		}
		total, docs, freq = cs.TotalDocumentCount(), cs.DocumentCount(), cs.SumTotalTermFrequency()
		cs.Merge(cs)
		again, err := r.reader.CollectionStats("body")
		if err != nil {
			t.Fatal(err)
		}
		if again.DocumentCount() != docs || again.SumTotalTermFrequency() != freq {
			t.Fatalf("Merge into returned stats changed the next: %d/%d, want %d/%d",
				again.DocumentCount(), again.SumTotalTermFrequency(), docs, freq)
		}
		return total, docs, freq, len(r.reader.Segments())
	}
	t1, d1, f1, s1 := stats(InMemoryOnlyConfig().WithBatchIndexing(0))
	t2, d2, f2, s2 := stats(InMemoryOnlyConfig().WithBatchIndexing(n / 3))
	if s1 != 1 {
		t.Fatalf("%d segments, want 1", s1)
	}
	if s2 < 2 {
		t.Logf("background merges left %d segment", s2) // the sums still compare
	}
	if t1 != n || d1 != n || t1 != t2 || d1 != d2 || f1 != f2 || f1 == 0 {
		t.Fatalf("one segment %d/%d/%d, several %d/%d/%d", t1, d1, f1, t2, d2, f2)
	}
}
