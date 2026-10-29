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
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"math"

	"github.com/vcaesar/ice/vec"
)

// Links-only layout, for callers that keep the vectors elsewhere. It shares
// the WriteTo header with version linksVersion, then:
//
//	content CRC-32 (IEEE) of ids (u64) and vector bits (u32), node-major,
//	per node: block length uvarint, block of uvarints:
//	          levels, per level: count, count neighbors.
//
// followed by a CRC-32 of all preceding bytes. Ids and vectors are left out,
// which drops most of the size: 4*dim+8 bytes per node. Version 2, still
// read, predates quantization.
const (
	linksVersion       = 4
	legacyLinksVersion = 2
)

// WriteLinksTo serializes the graph without its ids and vectors; ReadLinks
// restores it given the same ids and vectors. It may run concurrently with
// Search but not with Add.
func (g *Graph) WriteLinksTo(w io.Writer) (int64, error) {
	buf, err := g.appendHeader(nil, linksVersion)
	if err != nil {
		return 0, err
	}
	buf = binary.LittleEndian.AppendUint32(buf, contentSum(g.ids, g.data))
	cw := &countWriter{w: w}
	crc := crc32.NewIEEE()
	bw := bufio.NewWriterSize(io.MultiWriter(cw, crc), readChunk)
	if _, err := bw.Write(buf); err != nil {
		return cw.n, err
	}
	if err := g.writeLinkBlocks(bw); err != nil {
		return cw.n, err
	}
	if err := bw.Flush(); err != nil {
		return cw.n, err
	}
	_, err = cw.Write(binary.LittleEndian.AppendUint32(nil, crc.Sum32()))
	return cw.n, err
}

// writeLinkBlocks writes each node's levels and neighbors as a block of
// uvarints prefixed by its length.
func (g *Graph) writeLinkBlocks(w io.Writer) error {
	var buf, block []byte
	for _, levels := range g.links {
		block = binary.AppendUvarint(block[:0], uint64(len(levels)))
		for _, links := range levels {
			block = binary.AppendUvarint(block, uint64(len(links)))
			for _, n := range links {
				block = binary.AppendUvarint(block, uint64(n))
			}
		}
		buf = binary.AppendUvarint(buf[:0], uint64(len(block)))
		if _, err := w.Write(append(buf, block...)); err != nil {
			return err
		}
	}
	return nil
}

// ReadLinks restores a graph written by WriteLinksTo, consuming exactly its
// bytes. ids and data, node-major with the graph's dimension, must be those
// of the written graph; a mismatch is an error. The graph keeps data.
func ReadLinks(r io.Reader, ids []uint64, data []float32) (*Graph, error) {
	g, err := readLinks(r, ids, data)
	if err != nil {
		return nil, fmt.Errorf("error reading hnsw links: %w", err)
	}
	return g, nil
}

func readLinks(r io.Reader, ids []uint64, data []float32) (*Graph, error) {
	d := &decoder{r: r, crc: crc32.NewIEEE()}
	g, nodes, err := d.graph(linksVersion, legacyLinksVersion)
	if err != nil {
		return nil, err
	}
	if uint64(len(ids)) != nodes || uint64(len(data)) != nodes*uint64(g.dim) { // #nosec G115 -- dim is a u32.
		return nil, fmt.Errorf("%d ids and %d values do not match %d nodes of dimension %d",
			len(ids), len(data), nodes, g.dim)
	}
	sum, err := d.u32()
	if err != nil {
		return nil, err
	}
	if contentSum(ids, data) != sum {
		return nil, fmt.Errorf("ids and vectors differ from the written graph")
	}
	g.ids, g.data = ids, data
	g.links = make([][][]uint32, 0, nodes)
	g.reserveCodes(len(ids))
	for node := range nodes {
		vector := g.vector(uint32(node)) // #nosec G115 -- nodes bounded by MaxUint32.
		if err := vec.Validate(vector, g.metric); err != nil {
			return nil, fmt.Errorf("node %d: %w", node, err)
		}
		if g.metric == vec.Cosine {
			g.norms = append(g.norms, norm(vector))
		}
		g.addCodes(vector)
		if node > 0 && ids[node] <= ids[node-1] {
			g.repeated = true
		}
		links, err := d.nodeLinks(g, node)
		if err != nil {
			return nil, err
		}
		g.links = append(g.links, links)
	}
	if err := g.checkLinks(); err != nil {
		return nil, err
	}
	return g, d.checksum()
}

// nodeLinks reads one node's block.
func (d *decoder) nodeLinks(g *Graph, node uint64) ([][]uint32, error) {
	size, err := d.uvarint()
	if err != nil {
		return nil, err
	}
	block, err := d.block(size)
	if err != nil {
		return nil, err
	}
	br := &blockReader{b: block}
	levels := br.next()
	if err := g.checkLevels(node, levels); err != nil {
		return nil, err
	}
	links := d.levels(int(levels)) // #nosec G115 -- checkLevels bounds levels by maxLevels.
	for l := range links {
		count := br.next()
		if err := g.checkCount(node, l, count); err != nil {
			return nil, err
		}
		links[l] = d.neighbors(int(count)) // #nosec G115 -- checkCount bounds count by mmax.
		for i := range links[l] {
			n := br.next()
			if n > math.MaxUint32 {
				return nil, fmt.Errorf("node %d level %d: invalid neighbor %d", node, l, n)
			}
			links[l][i] = uint32(n)
		}
	}
	if br.err != nil || len(br.b) != 0 {
		return nil, fmt.Errorf("node %d: malformed link block", node)
	}
	return links, nil
}

// arenaSize is the number of slots allocated at once for decoded links.
const arenaSize = 16 << 10

// levels and neighbors carve node links out of shared arenas, saving an
// allocation per node and level. Capacities are capped, so a later append
// copies instead of overwriting a neighbor's slots.
func (d *decoder) levels(n int) [][]uint32 {
	if len(d.levelArena) < n {
		d.levelArena = make([][]uint32, max(n, arenaSize))
	}
	rv := d.levelArena[:n:n]
	d.levelArena = d.levelArena[n:]
	return rv
}

func (d *decoder) neighbors(n int) []uint32 {
	if len(d.linkArena) < n {
		d.linkArena = make([]uint32, max(n, arenaSize))
	}
	rv := d.linkArena[:n:n]
	d.linkArena = d.linkArena[n:]
	return rv
}

// uvarint reads a checksummed uvarint one byte at a time, so it never reads
// past the value.
func (d *decoder) uvarint() (uint64, error) {
	var v uint64
	for shift := uint(0); shift < 64; shift += 7 {
		b, err := d.next(1)
		if err != nil {
			return 0, err
		}
		v |= uint64(b[0]&0x7f) << shift
		if b[0] < 0x80 {
			return v, nil
		}
	}
	return 0, fmt.Errorf("uvarint overflows 64 bits")
}

// block returns the next n checksummed bytes, growing by at most readChunk
// per read so a corrupt length fails on a short read.
func (d *decoder) block(n uint64) ([]byte, error) {
	b := d.buf[:0]
	for uint64(len(b)) < n {
		c := int(min(n-uint64(len(b)), readChunk))
		start := len(b)
		b = append(b, make([]byte, c)...)
		if _, err := io.ReadFull(d.r, b[start:]); err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return nil, err
		}
		if _, err := d.crc.Write(b[start:]); err != nil {
			return nil, err
		}
	}
	d.buf = b
	return b, nil
}

// blockReader decodes uvarints, recording the first error; values read after
// an error are zero.
type blockReader struct {
	b   []byte
	err error
}

func (r *blockReader) next() uint64 {
	if r.err != nil {
		return 0
	}
	v, n := binary.Uvarint(r.b)
	if n <= 0 {
		r.err = fmt.Errorf("malformed uvarint")
		return 0
	}
	r.b = r.b[n:]
	return v
}

// contentSum is the CRC-32 of ids then vector bits, little-endian.
func contentSum(ids []uint64, data []float32) uint32 {
	le := binary.LittleEndian
	var sum uint32
	buf := make([]byte, 0, readChunk)
	flush := func() {
		sum = crc32.Update(sum, crc32.IEEETable, buf)
		buf = buf[:0]
	}
	for _, id := range ids {
		if len(buf)+8 > cap(buf) {
			flush()
		}
		buf = le.AppendUint64(buf, id)
	}
	for _, v := range data {
		if len(buf)+4 > cap(buf) {
			flush()
		}
		buf = le.AppendUint32(buf, math.Float32bits(v))
	}
	flush()
	return sum
}
