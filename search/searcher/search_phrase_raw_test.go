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
	"encoding/binary"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// rawPhraseOf returns a rawPhrase over one posting per term, whose
// locations are the given (field, pos) pairs.
func rawPhraseOf(fields [][]string, pos [][]int) *rawPhrase {
	n := len(pos)
	r := &rawPhrase{term: make([]*phraseCursor, n), terms: make([]string, n),
		lo: make([]int, n), hi: make([]int, n), ptr: make([]int, n), cur: make([]int, n),
		mask: make([]uint64, n*ownMaskWords), used: make([]int, n)}
	for t := range pos {
		c := &phraseCursor{offs: []uint64{noOffsets}, ends: []int{0, len(pos[t])}, n: 1, fields: fields[t]}
		for _, p := range pos[t] {
			c.locs = append(c.locs, p, p*10, p*10+1)
		}
		r.term[t], r.terms[t] = c, string(rune('a'+t))
	}
	return r
}

// match, with its single-pass sorted merge, finds what the quadratic
// matchAny finds, for sorted, unsorted and multi-field locations.
func TestRawPhraseMergeMatchesAny(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	// field names built at run time, so equal names do not share bytes
	body := []string{strings.Repeat("body", 1)[:4], string([]byte("body")), string([]byte("body"))}
	for trial := range 3000 {
		nt := 2 + rng.Intn(2)
		fields, pos := make([][]string, nt), make([][]int, nt)
		for t := range nt {
			n := 1 + rng.Intn(8)
			p := rng.Intn(4)
			for range n {
				switch trial % 4 {
				case 0, 1: // sorted
					p += 1 + rng.Intn(3)
				case 2: // possibly unsorted or repeated
					p = rng.Intn(20)
				}
				f := body[rng.Intn(len(body))]
				if trial%4 == 3 && rng.Intn(4) == 0 {
					f = "title"
				}
				if trial%4 == 3 {
					p += 1 + rng.Intn(3)
				}
				fields[t], pos[t] = append(fields[t], f), append(pos[t], p)
			}
		}
		for _, all := range []bool{true, false} {
			r := rawPhraseOf(fields, pos)
			got, gotFound := r.match(nil, all)
			r = rawPhraseOf(fields, pos)
			for t, c := range r.term {
				r.lo[t], r.hi[t] = c.span()
			}
			want, wantFound := r.matchAny(nil, all)
			if gotFound != wantFound || !reflect.DeepEqual(got, want) {
				t.Fatalf("trial %d all=%v fields %v pos %v: match %v %v, matchAny %v %v",
					trial, all, fields, pos, gotFound, got, wantFound, want)
			}
		}
	}
}

// encodeOwn makes the posting of term t of r, whose positions pos do not
// decrease and are all in field body, a batch of encoded positions.
func encodeOwn(r *rawPhrase, t int, pos []int) {
	c := r.term[t]
	var recs []byte
	prev := 0
	for _, p := range pos {
		recs = binary.AppendUvarint(recs, uint64(p-prev)) //nolint:gosec // G115: positions do not decrease.
		prev = p
	}
	c.field, c.recs, c.starts, c.ends = "body", recs, []int{0}, []int{0, len(recs)}
	c.fields, c.locs, c.isOwn = nil, nil, true
}

// match over batches of encoded positions, through the bit masks or the
// decoding merge, and over a mix of encoded and decoded batches, finds
// what the quadratic matchAny finds in the decoded locations.
func TestRawPhraseOwnMatchesAny(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	var masked, merged int
	for trial := range 4000 {
		nt := 2 + rng.Intn(3)
		fields, pos := make([][]string, nt), make([][]int, nt)
		for t := range nt {
			p := rng.Intn(4)
			if trial%8 == 7 {
				p += 64*ownMaskWords - 8 + rng.Intn(16) // around the last mask bit
			}
			for range rng.Intn(9) { // a posting may have no location
				switch rng.Intn(12) {
				case 0: // a repeated position
				case 1:
					p += 60 + rng.Intn(200) // also deltas of two bytes
				default:
					p += 1 + rng.Intn(3)
				}
				fields[t], pos[t] = append(fields[t], "body"), append(pos[t], p)
			}
		}
		decoded := func() *rawPhrase {
			r := rawPhraseOf(fields, pos)
			for t, c := range r.term {
				r.lo[t], r.hi[t] = c.span()
			}
			return r
		}
		for _, all := range []bool{true, false} {
			want, wantFound := decoded().matchAny(nil, all)
			for i := range want {
				// encoded positions carry no start and end; LoadOffsets,
				// which the fixture has no handle for, fills them in
				want[i].Location.Start, want[i].Location.End = 0, 0
			}
			for _, mixed := range []bool{false, true} {
				r := rawPhraseOf(fields, pos)
				for t := range nt {
					if !mixed || t > 0 {
						encodeOwn(r, t, pos[t])
					}
				}
				got, gotFound := r.match(nil, all)
				for i := range got {
					got[i].Location.Start, got[i].Location.End = 0, 0 // of term 0 when mixed
				}
				if r.err != nil || gotFound != wantFound || !reflect.DeepEqual(got, want) {
					t.Fatalf("trial %d all=%v mixed=%v pos %v: match %v %v (%v), matchAny %v %v",
						trial, all, mixed, pos, gotFound, got, r.err, wantFound, want)
				}
			}
		}
		// both own matchers, on masks left by the trial before
		r := rawPhraseOf(fields, pos)
		for t := range nt {
			encodeOwn(r, t, pos[t])
		}
		for range 2 {
			_, wantFound := decoded().matchAny(nil, false)
			if found := r.matchOwn(); found != wantFound {
				t.Fatalf("trial %d pos %v: matchOwn %v, want %v", trial, pos, found, wantFound)
			}
			found, ok := r.matchOwnMask()
			if ok && found != wantFound {
				t.Fatalf("trial %d pos %v: matchOwnMask %v, want %v", trial, pos, found, wantFound)
			}
			if ok {
				masked++
			} else {
				merged++
			}
			// the next round reuses the masks for other positions
			for t := range nt {
				for j := range pos[t] {
					pos[t][j] = pos[t][j]/2 + j
				}
				encodeOwn(r, t, pos[t])
			}
		}
	}
	if masked == 0 || merged == 0 {
		t.Fatalf("%d documents matched by masks, %d by the merge", masked, merged)
	}
}

// A record that does not decode is an error, not a wrong match.
func TestRawPhraseOwnBadRecord(t *testing.T) {
	r := rawPhraseOf([][]string{{"body"}, {"body"}}, [][]int{{1}, {2}})
	encodeOwn(r, 0, []int{1})
	encodeOwn(r, 1, []int{2})
	c := r.term[1]
	c.recs, c.ends = []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f}, []int{0, 10}
	if _, _, err := c.decode(0); err == nil {
		t.Fatal("decode read a position past the int range")
	}
	if err := c.expand(); err == nil {
		t.Fatal("expand read a position past the int range")
	}
}
