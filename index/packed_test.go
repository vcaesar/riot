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
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"unsafe"

	segment "github.com/vcaesar/bluge_segment_api"
)

type stubLoc struct {
	field           string
	pos, start, end int
}

func (l stubLoc) Field() string { return l.field }
func (l stubLoc) Start() int    { return l.start }
func (l stubLoc) End() int      { return l.end }
func (l stubLoc) Pos() int      { return l.pos }
func (l stubLoc) Size() int     { return 0 }

type stubTerm struct {
	term string
	freq int
	locs []stubLoc
}

func (t stubTerm) Term() []byte   { return []byte(t.term) }
func (t stubTerm) Frequency() int { return t.freq }
func (t stubTerm) EachLocation(vl segment.VisitLocation) {
	for _, l := range t.locs {
		vl(l)
	}
}

type stubField struct {
	name             string
	value            []byte
	length           int
	terms            []stubTerm
	index, store, dv bool
	numeric          int64
	isNumeric        bool
}

func (f stubField) Name() string         { return f.name }
func (f stubField) Length() int          { return f.length }
func (f stubField) Value() []byte        { return f.value }
func (f stubField) Index() bool          { return f.index }
func (f stubField) Store() bool          { return f.store }
func (f stubField) IndexDocValues() bool { return f.dv }
func (f stubField) EachTerm(vt segment.VisitTerm) {
	for _, t := range f.terms {
		vt(t)
	}
}

type stubNumericField struct{ stubField }

func (f stubNumericField) NumericValue() (int64, bool) { return f.numeric, f.isNumeric }

type stubDoc struct {
	fields []segment.Field
	ts     int64
}

func (d stubDoc) Analyze()         {}
func (d stubDoc) Timestamp() int64 { return d.ts }
func (d stubDoc) EachField(vf segment.VisitField) {
	for _, f := range d.fields {
		vf(f)
	}
}

// dump renders everything ice reads from a document.
func dump(d segment.Document) []any {
	var out []any
	out = append(out, d.Timestamp())
	d.EachField(func(f segment.Field) {
		out = append(out, f.Name(), f.Length(), f.Index(), f.Store(), f.IndexDocValues())
		if f.Store() { // ice reads values of stored fields only
			out = append(out, string(f.Value()))
		}
		// ice treats a NumericField reporting !ok like a non-numeric field
		if nf, ok := f.(segment.NumericField); ok {
			if v, ok := nf.NumericValue(); ok {
				out = append(out, v)
			}
		}
		f.EachTerm(func(t segment.FieldTerm) {
			out = append(out, string(t.Term()), t.Frequency())
			t.EachLocation(func(l segment.Location) {
				out = append(out, l.Field(), l.Pos(), l.Start(), l.End())
			})
		})
	})
	return out
}

func TestPackedBufferRoundTrip(t *testing.T) {
	docs := []segment.Document{
		stubDoc{ts: 7, fields: []segment.Field{
			stubField{name: "_id", value: []byte("a"), length: 1, index: true, store: true, dv: true,
				terms: []stubTerm{{term: "a", freq: 1}}},
			stubField{name: "body", value: []byte("x y x"), length: 3, index: true, store: true,
				terms: []stubTerm{{term: "x", freq: 2, locs: []stubLoc{{pos: 1, end: 1}, {pos: 3, start: 4, end: 5}}},
					{term: "y", freq: 1, locs: []stubLoc{{pos: 2, start: 2, end: 3}}}}},
			stubNumericField{stubField{name: "n", value: []byte{1}, length: 1, index: true, dv: true, numeric: -5, isNumeric: true,
				terms: []stubTerm{{term: "\x01", freq: 1}}}},
			stubField{name: "_all", length: 1, index: true,
				terms: []stubTerm{{term: "x", freq: 1, locs: []stubLoc{{field: "body", pos: 1, end: 1}}}}},
			stubField{name: "stored", value: []byte("only"), store: true},
		}},
		stubDoc{fields: []segment.Field{stubField{name: "body", length: 0, index: true}}},
	}
	b := newPackedBuffer()
	for _, d := range docs {
		if err := b.add(d); err != nil {
			t.Fatal(err)
		}
	}
	packed := b.documents()
	for i, d := range docs {
		want, got := dump(d), dump(packed[i])
		if len(want) != len(got) {
			t.Fatalf("doc %d:\n got %v\nwant %v", i, got, want)
		}
		for j := range want {
			if want[j] != got[j] {
				t.Fatalf("doc %d item %d: got %v want %v\n got %v\nwant %v", i, j, got[j], want[j], got, want)
			}
		}
	}
	if b.size() <= 0 {
		t.Fatal("size not tracked")
	}
}

func TestPackedBufferRejectsOutOfRange(t *testing.T) {
	invalidStart := -1
	if strconv.IntSize == 64 {
		wide := int64(1 << 33)
		invalidStart = int(wide)
	}
	ok := stubDoc{fields: []segment.Field{stubField{name: "f", length: 1, index: true,
		terms: []stubTerm{{term: "t", freq: 1, locs: []stubLoc{{pos: 1}}}}}}}
	for _, bad := range []stubField{
		{name: "f", length: -1},
		{name: "f", length: 1, terms: []stubTerm{{term: "t", freq: -1}}},
		{name: "f", length: 1, terms: []stubTerm{{term: "t", freq: 1, locs: []stubLoc{{pos: -1}}}}},
		{name: "f", length: 1, terms: []stubTerm{{term: "t", freq: 1, locs: []stubLoc{{start: invalidStart}}}}},
	} {
		b := newPackedBuffer()
		if err := b.add(ok); err != nil {
			t.Fatal(err)
		}
		before := b.size()
		doc := stubDoc{fields: []segment.Field{ok.fields[0], bad}}
		if err := b.add(doc); !errors.Is(err, errPackedRange) {
			t.Fatalf("%+v: err %v", bad, err)
		}
		if b.size() != before || b.docs.n != 1 {
			t.Fatalf("%+v: failed add left state behind", bad)
		}
	}
}

// Packing a document into a reset buffer allocates no more than visiting
// it does: visitors are bound once and the buffer keeps its capacity.
func TestPackedBufferAddDoesNotAllocate(t *testing.T) {
	doc := stubDoc{ts: 1, fields: []segment.Field{
		stubField{name: "body", value: []byte("x y x"), length: 3, index: true, store: true,
			terms: []stubTerm{{term: "x", freq: 2, locs: []stubLoc{{pos: 1, end: 1}, {pos: 3, start: 4, end: 5}}},
				{term: "y", freq: 1, locs: []stubLoc{{pos: 2, start: 2, end: 3}}}}},
		stubNumericField{stubField{name: "n", length: 1, index: true, dv: true, numeric: 3, isNumeric: true,
			terms: []stubTerm{{term: "\x01", freq: 1}}}},
	}}
	var sink int
	visit := func() { // reads what packing reads
		doc.EachField(func(f segment.Field) {
			sink += len(f.Name()) + len(f.Value())
			f.EachTerm(func(t segment.FieldTerm) {
				sink += len(t.Term())
				t.EachLocation(func(l segment.Location) { sink += len(l.Field()) })
			})
		})
	}
	b := newPackedBuffer()
	pack := func() {
		b.reset()
		if err := b.add(doc); err != nil {
			t.Fatal(err)
		}
	}
	pack()
	if got, base := testing.AllocsPerRun(100, pack), testing.AllocsPerRun(100, visit); got > base {
		t.Fatalf("packing allocates %v per document, visiting %v", got, base)
	}
}

// ice visits a document's fields from several goroutines at once (one
// field set each), so packed documents must not share cursor state.
func TestPackedDocConcurrentVisits(t *testing.T) {
	doc := stubDoc{fields: []segment.Field{
		stubField{name: "a", length: 2, index: true,
			terms: []stubTerm{{term: "x", freq: 1, locs: []stubLoc{{pos: 1, end: 1}}}, {term: "y", freq: 1, locs: []stubLoc{{pos: 2}}}}},
		stubField{name: "b", length: 1, index: true, terms: []stubTerm{{term: "z", freq: 1, locs: []stubLoc{{pos: 1}}}}},
	}}
	b := newPackedBuffer()
	if err := b.add(doc); err != nil {
		t.Fatal(err)
	}
	packed := b.documents()[0]
	want := fmt.Sprint(dump(doc))
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 200 {
				if got := fmt.Sprint(dump(packed)); got != want {
					t.Errorf("got %s want %s", got, want)
					return
				}
			}
		})
	}
	wg.Wait()
}

// Values and records span chunk boundaries and big values without moving:
// every span and record reads back what was packed.
func TestPackedBufferChunks(t *testing.T) {
	b := newPackedBuffer()
	var spans []span
	var want [][]byte
	sizes := []int{1, 100, bigValue, bigValue + 1, byteChunk - 3, 7, 3 * bigValue}
	for i := range 3 * len(sizes) {
		v := bytes.Repeat([]byte{byte(i)}, sizes[i%len(sizes)])
		sp, err := b.appendBytes(v)
		if err != nil {
			t.Fatal(err)
		}
		spans, want = append(spans, sp), append(want, v)
	}
	for i := range 3 * recordChunk {
		if !b.locs.push(packedLoc{pos: uint32(i)}) { // #nosec G115 -- small test index.
			t.Fatal("push failed")
		}
	}
	for i, sp := range spans {
		if got := b.slice(sp); !bytes.Equal(got, want[i]) || cap(got) != len(got) {
			t.Fatalf("value %d: len %d cap %d want len %d", i, len(got), cap(got), len(want[i]))
		}
	}
	for i := range 3 * recordChunk {
		if l := b.locs.at(uint32(i)); l.pos != uint32(i) { // #nosec G115 -- see above.
			t.Fatalf("record %d: pos %d", i, l.pos)
		}
	}
	if len(b.locs.c) != 3 {
		t.Fatalf("want 3 record chunks, got %d", len(b.locs.c))
	}
	chunksBefore := len(b.bytes)
	b.reset()
	if b.size() != 0 || len(b.big) != 0 || len(b.bytes) != chunksBefore {
		t.Fatalf("reset: size %d big %d chunks %d->%d", b.size(), len(b.big), chunksBefore, len(b.bytes))
	}
}

// A field's terms and a term's locations end where the next record begins,
// so a failed add between two documents must not shift either one.
func TestPackedBufferImpliedEndsAfterRollback(t *testing.T) {
	first := stubDoc{fields: []segment.Field{
		stubField{name: "a", length: 2, index: true,
			terms: []stubTerm{{term: "x", freq: 1, locs: []stubLoc{{pos: 1, end: 1}}}, {term: "y", freq: 1}}},
		stubField{name: "empty", index: true},
	}}
	bad := stubDoc{fields: []segment.Field{
		stubField{name: "a", length: 1, index: true, terms: []stubTerm{{term: "z", freq: 1, locs: []stubLoc{{pos: 1}}}}},
		stubField{name: "f", length: -1},
	}}
	last := stubDoc{fields: []segment.Field{
		stubField{name: "b", length: 1, index: true,
			terms: []stubTerm{{term: "w", freq: 2, locs: []stubLoc{{pos: 1, end: 1}, {pos: 2, start: 2, end: 3}}}}},
	}}
	b := newPackedBuffer()
	if err := b.add(first); err != nil {
		t.Fatal(err)
	}
	if err := b.add(bad); !errors.Is(err, errPackedRange) {
		t.Fatalf("err %v", err)
	}
	if err := b.add(last); err != nil {
		t.Fatal(err)
	}
	packed := b.documents()
	for i, d := range []segment.Document{first, last} {
		if want, got := fmt.Sprint(dump(d)), fmt.Sprint(dump(packed[i])); got != want {
			t.Fatalf("doc %d: got %s want %s", i, got, want)
		}
	}
	if s := unsafe.Sizeof(packedTerm{}); s != 16 {
		t.Errorf("packedTerm is %d bytes, want 16", s)
	}
	if s := unsafe.Sizeof(packedField{}); s != 32 {
		t.Errorf("packedField is %d bytes, want 32", s)
	}
}

// A failed add drops the big values it stored.
func TestPackedBufferAddRollsBackBig(t *testing.T) {
	b := newPackedBuffer()
	big := make([]byte, bigValue+1)
	doc := stubDoc{fields: []segment.Field{
		stubField{name: "v", value: big, store: true},
		stubField{name: "f", length: -1},
	}}
	if err := b.add(doc); !errors.Is(err, errPackedRange) {
		t.Fatalf("err %v", err)
	}
	if len(b.big) != 0 || b.size() != 0 || b.nBytes != 0 {
		t.Fatalf("state left behind: big %d size %d", len(b.big), b.size())
	}
}
