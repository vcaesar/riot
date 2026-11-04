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
	"errors"
	"math"
	"slices"
	"unsafe"

	segment "github.com/vcaesar/bluge_segment_api"
)

// packedBuffer holds analyzed documents as flat runs of fixed-size
// records, the way riot-go's segment writer does, so a document's analysis
// objects can be collected as soon as it is packed. Its documents implement
// segment.Document for building a segment; a visited field, term or
// location is only valid during its callback.
//
// Records and bytes live in fixed-size chunks, so the buffer is never
// copied to grow and wastes at most one partial chunk per run.
type packedBuffer struct {
	bytes   [][]byte // byteChunk-sized chunks of term and stored value bytes
	nBytes  uint32   // bytes used, as an offset into the chunks
	big     [][]byte // values over bigValue bytes, one allocation each
	bigSize int
	names   []string
	nameIDs map[string]uint32
	docs    chunks[packedDoc]
	fields  chunks[packedField]
	terms   chunks[packedTerm]
	locs    chunks[packedLoc]

	// packing visitors, bound once so they do not allocate per call;
	// err is their first error
	visitField segment.VisitField
	visitTerm  segment.VisitTerm
	visitLoc   segment.VisitLocation
	err        error
}

// span locates bytes: off is an offset into the chunks, or with bigSpan
// set an index into big.
type span struct{ off, n uint32 }

const (
	byteChunkBits = 20 // 1 MiB byte chunks
	byteChunk     = 1 << byteChunkBits
	// bigValue is the largest value stored in the chunks; a value never
	// straddles two chunks, so this caps the space skipped per chunk at 1/8
	bigValue = byteChunk / 8
	bigSpan  = 1 << 31
	// maxBytes keeps chunk offsets clear of bigSpan
	maxBytes = bigSpan - 1
)

// recordChunkBits sets the records per chunk (16K): 256 KiB to 640 KiB.
const (
	recordChunkBits = 14
	recordChunk     = 1 << recordChunkBits
)

// chunks is an append-only run of records in recordChunk-sized chunks;
// a record never moves once pushed.
type chunks[T any] struct {
	c [][]T
	n uint32
}

func (s *chunks[T]) at(i uint32) *T {
	return &s.c[i>>recordChunkBits][i&(recordChunk-1)]
}

// push appends v unless the run already holds MaxUint32 records.
func (s *chunks[T]) push(v T) bool {
	if s.n == math.MaxUint32 {
		return false
	}
	if int(s.n>>recordChunkBits) == len(s.c) {
		s.c = append(s.c, make([]T, recordChunk))
	}
	s.c[s.n>>recordChunkBits][s.n&(recordChunk-1)] = v
	s.n++
	return true
}

// The records hold no pointers, so the collector does not scan them.
type packedDoc struct {
	first, end uint32 // fields
	timestamp  int64
}

const (
	packedIndex uint8 = 1 << iota
	packedStore
	packedDocValues
	packedNumeric
)

// A field's terms, and a term's locations, run from its first to the next
// record's first (or the run's end): records are pushed in order.
type packedField struct {
	value   span
	numeric int64
	first   uint32 // terms
	length  uint32
	name    uint32
	flags   uint8
}

type packedTerm struct {
	term  span
	freq  uint32
	first uint32 // locations
}

type packedLoc struct {
	pos, start, end uint32
	field           uint32 // names index; 0 is ""
}

var errPackedRange = errors.New("analyzed value out of range")

func newPackedBuffer() *packedBuffer {
	b := &packedBuffer{names: []string{""}, nameIDs: map[string]uint32{"": 0}}
	b.visitField = func(f segment.Field) {
		if b.err == nil {
			b.err = b.packField(f)
		}
	}
	b.visitTerm = func(t segment.FieldTerm) {
		if b.err == nil {
			b.err = b.packTerm(t)
		}
	}
	b.visitLoc = b.packLoc
	return b
}

// reset empties the buffer but keeps its chunks, so the next segment's
// documents pack without allocating them again; segments copy what they keep.
func (b *packedBuffer) reset() {
	clear(b.big)
	b.nBytes, b.big, b.bigSize = 0, b.big[:0], 0
	b.docs.n, b.fields.n, b.terms.n, b.locs.n = 0, 0, 0, 0
}

// size approximates the buffer's heap use.
func (b *packedBuffer) size() int {
	return int(b.nBytes) + b.bigSize +
		int(b.docs.n)*int(unsafe.Sizeof(packedDoc{})) +
		int(b.fields.n)*int(unsafe.Sizeof(packedField{})) +
		int(b.terms.n)*int(unsafe.Sizeof(packedTerm{})) +
		int(b.locs.n)*int(unsafe.Sizeof(packedLoc{}))
}

func (b *packedBuffer) nameID(name string) uint32 {
	id, ok := b.nameIDs[name]
	if !ok {
		id = uint32(len(b.names)) // #nosec G115 -- one entry per distinct field name.
		b.names = append(b.names, name)
		b.nameIDs[name] = id
	}
	return id
}

func (b *packedBuffer) appendBytes(v []byte) (span, error) {
	n, ok := u32(len(v))
	if !ok {
		return span{}, errPackedRange
	}
	if n > bigValue {
		if len(b.big) >= maxBytes {
			return span{}, errPackedRange
		}
		b.big = append(b.big, slices.Clone(v))
		b.bigSize += len(v)
		return span{bigSpan | uint32(len(b.big)-1), n}, nil // #nosec G115 -- bounded above.
	}
	off := b.nBytes
	if (off&(byteChunk-1))+n > byteChunk {
		off = (off | (byteChunk - 1)) + 1 // the value starts the next chunk
	}
	if uint64(off)+uint64(n) > maxBytes {
		return span{}, errPackedRange
	}
	if int(off>>byteChunkBits) == len(b.bytes) {
		b.bytes = append(b.bytes, make([]byte, byteChunk))
	}
	copy(b.bytes[off>>byteChunkBits][off&(byteChunk-1):], v)
	b.nBytes = off + n
	return span{off, n}, nil
}

func u32(v int) (uint32, bool) {
	return uint32(v), v >= 0 && uint64(v) <= math.MaxUint32 // #nosec G115 -- checked.
}

// add packs an analyzed document; on error the buffer is unchanged.
func (b *packedBuffer) add(doc segment.Document) error {
	nBytes, nBig, bigSize := b.nBytes, len(b.big), b.bigSize
	nFields, nTerms, nLocs := b.fields.n, b.terms.n, b.locs.n
	if err := b.pack(doc); err != nil {
		clear(b.big[nBig:])
		b.nBytes, b.big, b.bigSize = nBytes, b.big[:nBig], bigSize
		b.fields.n, b.terms.n, b.locs.n = nFields, nTerms, nLocs
		return err
	}
	return nil
}

func (b *packedBuffer) pack(doc segment.Document) error {
	d := packedDoc{first: b.fields.n, timestamp: doc.Timestamp()}
	b.err = nil
	doc.EachField(b.visitField)
	if b.err != nil {
		return b.err
	}
	d.end = b.fields.n
	if !b.docs.push(d) {
		return errPackedRange
	}
	return nil
}

func (b *packedBuffer) packField(f segment.Field) (err error) {
	length, ok := u32(f.Length())
	if !ok {
		return errPackedRange
	}
	pf := packedField{length: length, name: b.nameID(f.Name()), first: b.terms.n}
	if f.Index() {
		pf.flags |= packedIndex
	}
	if f.Store() {
		pf.flags |= packedStore
		if pf.value, err = b.appendBytes(f.Value()); err != nil {
			return err
		}
	}
	if f.IndexDocValues() {
		pf.flags |= packedDocValues
		if nf, ok := f.(segment.NumericField); ok {
			if v, ok := nf.NumericValue(); ok {
				pf.flags |= packedNumeric
				pf.numeric = v
			}
		}
	}
	f.EachTerm(b.visitTerm)
	if b.err != nil {
		return b.err
	}
	if !b.fields.push(pf) {
		return errPackedRange
	}
	return nil
}

func (b *packedBuffer) packTerm(t segment.FieldTerm) (err error) {
	freq, ok := u32(t.Frequency())
	if !ok {
		return errPackedRange
	}
	pt := packedTerm{freq: freq, first: b.locs.n}
	if pt.term, err = b.appendBytes(t.Term()); err != nil {
		return err
	}
	t.EachLocation(b.visitLoc)
	if b.err != nil {
		return b.err
	}
	if !b.terms.push(pt) {
		return errPackedRange
	}
	return nil
}

func (b *packedBuffer) packLoc(l segment.Location) {
	pos, ok1 := u32(l.Pos())
	start, ok2 := u32(l.Start())
	end, ok3 := u32(l.End())
	if !ok1 || !ok2 || !ok3 {
		b.err = errPackedRange
	}
	if b.err == nil && !b.locs.push(packedLoc{pos: pos, start: start, end: end, field: b.nameID(l.Field())}) {
		b.err = errPackedRange
	}
}

// documents returns the packed documents; the buffer must not change while
// they are in use.
func (b *packedBuffer) documents() []segment.Document {
	views := make([]docView, b.docs.n)
	rv := make([]segment.Document, b.docs.n)
	for i := range views {
		views[i] = docView{buf: b, d: b.docs.at(uint32(i))} // #nosec G115 -- i < docs.n.
		rv[i] = &views[i]
	}
	return rv
}

type docView struct {
	buf *packedBuffer
	d   *packedDoc
}

func (v *docView) Analyze()         {}
func (v *docView) Timestamp() int64 { return v.d.timestamp }

// EachField visits the fields through one cursor per call, which also
// serves their terms and locations, so concurrent visits do not share state.
func (v *docView) EachField(vf segment.VisitField) {
	c := &fieldCursor{buf: v.buf}
	c.term.buf, c.term.loc.buf = v.buf, v.buf
	for i := v.d.first; i < v.d.end; i++ {
		c.f = v.buf.fields.at(i)
		c.end = v.buf.terms.n
		if i+1 < v.buf.fields.n {
			c.end = v.buf.fields.at(i + 1).first
		}
		vf(c)
	}
}

type fieldCursor struct {
	buf  *packedBuffer
	f    *packedField
	end  uint32 // of f's terms
	term termCursor
}

func (c *fieldCursor) Name() string         { return c.buf.names[c.f.name] }
func (c *fieldCursor) Length() int          { return int(c.f.length) }
func (c *fieldCursor) Index() bool          { return c.f.flags&packedIndex != 0 }
func (c *fieldCursor) Store() bool          { return c.f.flags&packedStore != 0 }
func (c *fieldCursor) IndexDocValues() bool { return c.f.flags&packedDocValues != 0 }

func (c *fieldCursor) Value() []byte {
	if !c.Store() {
		return nil
	}
	return c.buf.slice(c.f.value)
}

func (c *fieldCursor) NumericValue() (int64, bool) {
	return c.f.numeric, c.f.flags&packedNumeric != 0
}

func (c *fieldCursor) EachTerm(vt segment.VisitTerm) {
	for i := c.f.first; i < c.end; i++ {
		c.term.t = c.buf.terms.at(i)
		c.term.end = c.buf.locs.n
		if i+1 < c.buf.terms.n {
			c.term.end = c.buf.terms.at(i + 1).first
		}
		vt(&c.term)
	}
}

func (b *packedBuffer) slice(s span) []byte {
	if s.off&bigSpan != 0 {
		v := b.big[s.off&^bigSpan]
		return v[:s.n:s.n]
	}
	o := s.off & (byteChunk - 1)
	return b.bytes[s.off>>byteChunkBits][o : o+s.n : o+s.n]
}

type termCursor struct {
	buf *packedBuffer
	t   *packedTerm
	end uint32 // of t's locations
	loc locCursor
}

func (c *termCursor) Term() []byte   { return c.buf.slice(c.t.term) }
func (c *termCursor) Frequency() int { return int(c.t.freq) }

func (c *termCursor) EachLocation(vl segment.VisitLocation) {
	for i := c.t.first; i < c.end; i++ {
		c.loc.l = c.buf.locs.at(i)
		vl(&c.loc)
	}
}

type locCursor struct {
	buf *packedBuffer
	l   *packedLoc
}

func (c *locCursor) Field() string { return c.buf.names[c.l.field] }
func (c *locCursor) Start() int    { return int(c.l.start) }
func (c *locCursor) End() int      { return int(c.l.end) }
func (c *locCursor) Pos() int      { return int(c.l.pos) }
func (c *locCursor) Size() int     { return int(unsafe.Sizeof(packedLoc{})) }
