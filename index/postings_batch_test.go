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
	"encoding/binary"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type batchLoc struct {
	field           string
	pos, start, end int
}

type batchPosting struct {
	number uint64
	freq   int
	norm   float64
	locs   []batchLoc
}

// NextPosBatch with offsets loaded on demand, and NextLocBatch, must read
// what Next does, across segments and deletions.
func TestNextPosBatchMatchesNext(t *testing.T) {
	cfg, cleanup := CreateConfig("TestNextPosBatchMatchesNext")
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
	id := 0
	for _, size := range []int{900, 40, 300} {
		b := NewBatch()
		for range size {
			body := strings.Repeat("pad ", id%50) + "quick brown fox"
			if id%7 == 0 {
				body += " quick"
			}
			if id%301 == 0 {
				body = strings.Repeat("padding ", 2100) + body
			}
			sid := strconv.Itoa(id)
			b.Update(testIdentifier(sid), &FakeDocument{
				NewFakeField("_id", sid, true, false, false),
				NewFakeField("body", body, false, true, false),
			})
			id++
		}
		if err = idx.Batch(b); err != nil {
			t.Fatal(err)
		}
	}
	b := NewBatch()
	b.Delete(testIdentifier("5"))
	b.Delete(testIdentifier("950"))
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
	iterator := func() *postingsIterator {
		pi, err := reader.PostingsIterator([]byte("quick"), "body", true, true, true)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := pi.Close(); err != nil {
				t.Error(err)
			}
		})
		return pi.(*postingsIterator)
	}

	var want []batchPosting
	it := iterator()
	for {
		p, err := it.Next()
		if err != nil {
			t.Fatal(err)
		}
		if p == nil {
			break
		}
		bp := batchPosting{number: p.Number(), freq: p.Frequency(), norm: p.Norm()}
		for _, l := range p.Locations() {
			bp.locs = append(bp.locs, batchLoc{l.Field(), l.Pos(), l.Start(), l.End()})
		}
		want = append(want, bp)
	}
	if len(want) != id-2 {
		t.Fatalf("Next read %d postings, want %d", len(want), id-2)
	}

	const size = 16
	docs, freqs, norms, ends, offs := make([]uint64, size), make([]int, size), make([]float64, size),
		make([]int, size), make([]uint64, size)
	for _, lazyRead := range []bool{true, false} {
		it = iterator()
		var got []batchPosting
		lazy := 0
		for {
			var n int
			var fields []string
			var locs []int
			if lazyRead {
				n, fields, locs, err = it.NextPosBatch(docs, freqs, norms, ends, offs, nil, nil)
			} else {
				n, fields, locs, err = it.NextLocBatch(docs, freqs, norms, ends, nil, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				break
			}
			for k := n - 1; k >= 0 && lazyRead; k-- {
				lo := 0
				if k > 0 {
					lo = ends[k-1]
				}
				if offs[k] != noOffsets {
					lazy++
				}
				if err := it.LoadOffsets(offs[k], locs[3*lo:3*ends[k]]); err != nil {
					t.Fatal(err)
				}
			}
			lo := 0
			for k := range n {
				bp := batchPosting{number: docs[k], freq: freqs[k], norm: norms[k]}
				for j := lo; j < ends[k]; j++ {
					bp.locs = append(bp.locs, batchLoc{fields[j], locs[3*j], locs[3*j+1], locs[3*j+2]})
				}
				lo = ends[k]
				got = append(got, bp)
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("lazy=%v: batches read %d postings, Next %d, or they differ", lazyRead, len(got), len(want))
		}
		// segments that keep offsets apart load them on demand
		if _, ok := it.iterators[0].(posBatcher); ok && lazyRead && lazy == 0 {
			t.Fatal("no offsets loaded on demand")
		}
	}

	// postings of the iterator's own field are read as encoded positions,
	// with the same documents, positions and, once loaded, offsets
	it = iterator()
	if _, ok := it.iterators[0].(ownPosBatcher); !ok {
		t.Skip("the segment plugin does not read positions alone")
	}
	starts := make([]int, size)
	var got []batchPosting
	for {
		n, recs, own, err := it.NextOwnPosBatch(docs, freqs, norms, starts, ends, offs)
		if err != nil {
			t.Fatal(err)
		}
		if !own {
			t.Fatal("postings of one field are not read as positions")
		}
		if n == 0 {
			break
		}
		for k := range n {
			bp := batchPosting{number: docs[k], freq: freqs[k], norm: norms[k]}
			var locs []int
			pos := 0
			for rec := recs[starts[k]:ends[k]]; len(rec) > 0; {
				delta, w := binary.Uvarint(rec)
				if w <= 0 {
					t.Fatalf("doc %d: bad position record % x", docs[k], rec)
				}
				pos, rec = pos+int(delta), rec[w:] //nolint:gosec // G115: small test positions.
				locs = append(locs, pos, 0, 0)
			}
			if err := it.LoadOffsets(offs[k], locs); err != nil {
				t.Fatal(err)
			}
			for j := 0; j < len(locs); j += 3 {
				bp.locs = append(bp.locs, batchLoc{"body", locs[j], locs[j+1], locs[j+2]})
			}
			got = append(got, bp)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("own batches read %d postings, Next %d, or they differ", len(got), len(want))
	}
}
