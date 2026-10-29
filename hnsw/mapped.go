// Copyright (c) 2026 The AtomAI.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hnsw

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"unsafe"

	"github.com/vcaesar/ice/vec"
)

// Mapped layout, version mappedVersion, for graphs used in place, e.g. from
// a memory-mapped file, as Lucene does. The WriteTo header is zero-padded to
// a multiple of 8 bytes, then arrays follow, little-endian, ordered so each
// stays aligned to its element size:
//
//	ids u64 × nodes; cosine: norms f64 × nodes;
//	int8 L2: squared norms f64 × nodes; int8: scales f32 × nodes;
//	vectors f32 × nodes×dim; int8: codes i8 × nodes×dim;
//	links as in the links-only layout; CRC-32 (IEEE) of all preceding bytes.
const mappedVersion = 5

// littleEndian reports whether arrays can be used in place.
var littleEndian = binary.NativeEndian.Uint16([]byte{1, 0}) == 1

// WriteMappedTo serializes the graph with its vectors for Map. It may run
// concurrently with Search but not with Add.
func (g *Graph) WriteMappedTo(w io.Writer) (int64, error) {
	buf, err := g.appendHeader(nil, mappedVersion)
	if err != nil {
		return 0, err
	}
	buf = append(buf, make([]byte, pad8(len(buf)))...)
	cw := &countWriter{w: w}
	crc := crc32.NewIEEE()
	bw := bufio.NewWriterSize(io.MultiWriter(cw, crc), readChunk)
	if _, err := bw.Write(buf); err != nil {
		return cw.n, err
	}
	err = writeArray(bw, g.ids)
	if err == nil && g.metric == vec.Cosine {
		err = writeArray(bw, g.norms)
	}
	if err == nil && g.quant == QuantizationInt8 {
		if g.metric == vec.L2 {
			err = writeArray(bw, g.sqNorms)
		}
		if err == nil {
			err = writeArray(bw, g.scales)
		}
	}
	if err == nil {
		err = writeArray(bw, g.data)
	}
	if err == nil && g.quant == QuantizationInt8 {
		err = writeArray(bw, g.codes)
	}
	if err == nil {
		err = g.writeLinkBlocks(bw)
	}
	if err == nil {
		err = bw.Flush()
	}
	if err != nil {
		return cw.n, err
	}
	_, err = cw.Write(binary.LittleEndian.AppendUint32(nil, crc.Sum32()))
	return cw.n, err
}

// Map returns the graph written by WriteMappedTo in b, which must hold
// exactly its bytes. Ids, vectors and codes are used in place when b is
// suitably aligned on a little-endian machine, so b must stay valid and
// unmodified while the graph is used; only links are decoded. The checksum
// is verified first, then the structure, so corrupt input returns an error.
// Add copies the arrays before growing them.
func Map(b []byte) (*Graph, error) {
	g, err := mapGraph(b)
	if err != nil {
		return nil, fmt.Errorf("error mapping hnsw graph: %w", err)
	}
	return g, nil
}

func mapGraph(b []byte) (*Graph, error) {
	if len(b) < 4 {
		return nil, io.ErrUnexpectedEOF
	}
	body := b[:len(b)-4]
	if crc32.ChecksumIEEE(body) != binary.LittleEndian.Uint32(b[len(body):]) {
		return nil, fmt.Errorf("checksum mismatch")
	}
	r := bytes.NewReader(body)
	d := &decoder{r: r, crc: crc32.NewIEEE()}
	g, nodes, err := d.graph(mappedVersion)
	if err != nil {
		return nil, err
	}
	off := len(body) - r.Len()
	off += pad8(off)
	if off > len(body) {
		return nil, io.ErrUnexpectedEOF
	}
	m := &mapper{rest: body[off:]}
	n, values := nodes, nodes*uint64(g.dim) // #nosec G115 -- dim is a u32 and nodes below 2^32.
	g.ids = mapArray[uint64](m, n)
	if g.metric == vec.Cosine {
		g.norms = mapArray[float64](m, n)
	}
	if g.quant == QuantizationInt8 {
		if g.metric == vec.L2 {
			g.sqNorms = mapArray[float64](m, n)
		}
		g.scales = mapArray[float32](m, n)
	}
	g.data = mapArray[float32](m, values)
	if g.quant == QuantizationInt8 {
		g.codes = mapArray[int8](m, values)
	}
	if m.err != nil {
		return nil, m.err
	}
	for i := 1; i < len(g.ids); i++ {
		if g.ids[i] <= g.ids[i-1] {
			g.repeated = true
			break
		}
	}
	lr := bytes.NewReader(m.rest)
	ld := &decoder{r: lr, crc: crc32.NewIEEE()}
	g.links = make([][][]uint32, 0, nodes)
	for node := range nodes {
		links, err := ld.nodeLinks(g, node)
		if err != nil {
			return nil, err
		}
		g.links = append(g.links, links)
	}
	if lr.Len() != 0 {
		return nil, fmt.Errorf("%d trailing bytes after links", lr.Len())
	}
	if err := g.checkLinks(); err != nil {
		return nil, err
	}
	return g, nil
}

func pad8(n int) int { return (8 - n%8) % 8 }

// mapper carves arrays off rest, recording the first error.
type mapper struct {
	rest []byte
	err  error
}

// mapArray returns the next count elements, in place when aligned on a
// little-endian machine, else decoded. Capacity is capped so appends copy.
func mapArray[T uint64 | float64 | float32 | int8](m *mapper, count uint64) []T {
	if m.err != nil || count == 0 {
		return nil
	}
	var zero T
	size := uint64(unsafe.Sizeof(zero))
	if count > math.MaxInt/size || count*size > uint64(len(m.rest)) {
		m.err = fmt.Errorf("%d array elements exceed the %d remaining bytes", count, len(m.rest))
		return nil
	}
	b := m.rest[:count*size]
	m.rest = m.rest[count*size:]
	p := unsafe.Pointer(unsafe.SliceData(b)) // #nosec G103 -- arrays are read in place, bounds checked above.
	if littleEndian && uintptr(p)%unsafe.Alignof(zero) == 0 {
		return unsafe.Slice((*T)(p), count) // #nosec G103 -- p holds count elements.
	}
	out := make([]T, count)
	if err := binary.Read(bytes.NewReader(b), binary.LittleEndian, out); err != nil {
		m.err = err
		return nil
	}
	return out
}

// writeArray writes s little-endian, as raw memory when the machine is.
func writeArray[T uint64 | float64 | float32 | int8](w io.Writer, s []T) error {
	if len(s) == 0 {
		return nil
	}
	if !littleEndian {
		return binary.Write(w, binary.LittleEndian, s)
	}
	var zero T
	b := unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(s))), len(s)*int(unsafe.Sizeof(zero))) // #nosec G103 -- views s as bytes.
	_, err := w.Write(b)
	return err
}
