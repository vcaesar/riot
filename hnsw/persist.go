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
	"hash"
	"hash/crc32"
	"io"
	"math"
	"math/rand/v2"

	"github.com/vcaesar/ice/vec"
)

// Persisted layout, little-endian, followed by a CRC-32 (IEEE) of all
// preceding bytes:
//
//	magic "HNSW", version u32, metric (u32 length + bytes),
//	M, EfConstruction, EfSearch u64, dim u32, nodes u64,
//	maxLevel u32, entry u32, level generator state (u32 length + bytes),
//	per node: id u64, dim float32 bits, levels u32,
//	          per level: count u32, count neighbor u32.
const (
	formatMagic   = "HNSW"
	formatVersion = 1

	// maxLevels bounds node levels: Float64 has 53 random bits and M >= 2,
	// so generated levels stay below 54.
	maxLevels = 64
	// maxShortBytes bounds the metric name and generator state.
	maxShortBytes = 256
	// readChunk bounds each allocation while decoding, so a corrupt length
	// fails on a short read instead of reserving memory up front. It also
	// sizes the write buffer.
	readChunk = 64 << 10
)

// WriteTo serializes the graph to w; Read restores it. Graphs built by the
// same Adds before and after a WriteTo/Read round trip are identical. WriteTo
// may run concurrently with Search but not with Add.
func (g *Graph) WriteTo(w io.Writer) (int64, error) {
	state, err := g.pcg.MarshalBinary()
	if err != nil {
		return 0, fmt.Errorf("error encoding hnsw level generator: %w", err)
	}
	cw := &countWriter{w: w}
	crc := crc32.NewIEEE()
	bw := bufio.NewWriterSize(io.MultiWriter(cw, crc), readChunk)

	le := binary.LittleEndian
	buf := append([]byte(nil), formatMagic...)
	buf = le.AppendUint32(buf, formatVersion)
	buf = appendBytes(buf, []byte(g.metric))
	buf = le.AppendUint64(buf, uint64(g.m))        // #nosec G115 -- validated positive.
	buf = le.AppendUint64(buf, uint64(g.efCons))   // #nosec G115 -- validated positive.
	buf = le.AppendUint64(buf, uint64(g.efSearch)) // #nosec G115 -- validated positive.
	buf = le.AppendUint32(buf, uint32(g.dim))      // #nosec G115 -- vector lengths fit in memory.
	buf = le.AppendUint64(buf, uint64(len(g.ids)))
	buf = le.AppendUint32(buf, uint32(g.maxLevel)) // #nosec G115 -- below maxLevels.
	buf = le.AppendUint32(buf, g.entry)
	buf = appendBytes(buf, state)
	if _, err := bw.Write(buf); err != nil {
		return cw.n, err
	}
	for node := range g.ids {
		buf = le.AppendUint64(buf[:0], g.ids[node])
		for _, v := range g.vector(uint32(node)) { // #nosec G115 -- Add bounds nodes by MaxUint32.
			buf = le.AppendUint32(buf, math.Float32bits(v))
		}
		buf = le.AppendUint32(buf, uint32(len(g.links[node]))) // #nosec G115 -- below maxLevels.
		for _, links := range g.links[node] {
			buf = le.AppendUint32(buf, uint32(len(links))) // #nosec G115 -- bounded by 2*M nodes.
			for _, n := range links {
				buf = le.AppendUint32(buf, n)
			}
		}
		if _, err := bw.Write(buf); err != nil {
			return cw.n, err
		}
	}
	if err := bw.Flush(); err != nil {
		return cw.n, err
	}
	_, err = cw.Write(le.AppendUint32(nil, crc.Sum32()))
	return cw.n, err
}

// Read restores a graph written by WriteTo, consuming exactly its bytes.
// Input is checksummed and structurally validated, so a corrupt or foreign
// stream returns an error rather than a graph that could panic in Search.
func Read(r io.Reader) (*Graph, error) {
	d := &decoder{r: r, crc: crc32.NewIEEE()}
	g, err := d.graph()
	if err != nil {
		return nil, fmt.Errorf("error reading hnsw graph: %w", err)
	}
	sum := d.crc.Sum32()
	stored, err := d.read(4)
	if err != nil {
		return nil, fmt.Errorf("error reading hnsw graph: %w", err)
	}
	if binary.LittleEndian.Uint32(stored) != sum {
		return nil, fmt.Errorf("error reading hnsw graph: checksum mismatch")
	}
	return g, nil
}

func appendBytes(buf, b []byte) []byte {
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(b))) // #nosec G115 -- short fields.
	return append(buf, b...)
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

type decoder struct {
	r   io.Reader
	crc hash.Hash32
	buf []byte
}

// read returns the next n bytes, valid until the following read.
func (d *decoder) read(n int) ([]byte, error) {
	if cap(d.buf) < n {
		d.buf = make([]byte, n)
	}
	b := d.buf[:n]
	if _, err := io.ReadFull(d.r, b); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return b, nil
}

// next is read with the bytes added to the checksum.
func (d *decoder) next(n int) ([]byte, error) {
	b, err := d.read(n)
	if err != nil {
		return nil, err
	}
	_, err = d.crc.Write(b)
	return b, err
}

func (d *decoder) u32() (uint32, error) {
	b, err := d.next(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b), nil
}

func (d *decoder) u64() (uint64, error) {
	b, err := d.next(8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(b), nil
}

// param reads a u64 that must be a positive int.
func (d *decoder) param() (int, error) {
	v, err := d.u64()
	if err != nil {
		return 0, err
	}
	if v == 0 || v > math.MaxInt {
		return 0, fmt.Errorf("invalid parameter %d", v)
	}
	return int(v), nil
}

func (d *decoder) short() ([]byte, error) {
	n, err := d.u32()
	if err != nil {
		return nil, err
	}
	if n > maxShortBytes {
		return nil, fmt.Errorf("field length %d exceeds %d", n, maxShortBytes)
	}
	b, err := d.next(int(n))
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), b...), nil
}

// u32s passes count u32 values to each, reading at most readChunk bytes at a time.
func (d *decoder) u32s(count uint64, each func(uint32)) error {
	for count > 0 {
		c := min(count, readChunk/4)
		b, err := d.next(int(c) * 4)
		if err != nil {
			return err
		}
		for i := 0; i < len(b); i += 4 {
			each(binary.LittleEndian.Uint32(b[i:]))
		}
		count -= c
	}
	return nil
}

func (d *decoder) graph() (*Graph, error) {
	magic, err := d.next(len(formatMagic))
	if err != nil {
		return nil, err
	}
	if string(magic) != formatMagic {
		return nil, fmt.Errorf("not an hnsw graph")
	}
	version, err := d.u32()
	if err != nil {
		return nil, err
	}
	if version != formatVersion {
		return nil, fmt.Errorf("unsupported hnsw format version %d", version)
	}
	metric, err := d.short()
	if err != nil {
		return nil, err
	}
	var params Params
	for _, p := range []*int{&params.M, &params.EfConstruction, &params.EfSearch} {
		if *p, err = d.param(); err != nil {
			return nil, err
		}
	}
	g, err := New(vec.Metric(metric), params)
	if err != nil {
		return nil, err
	}
	if err := d.header(g); err != nil {
		return nil, err
	}
	return g, nil
}

func (d *decoder) header(g *Graph) error {
	dim, err := d.u32()
	if err != nil {
		return err
	}
	nodes, err := d.u64()
	if err != nil {
		return err
	}
	maxLevel, err := d.u32()
	if err != nil {
		return err
	}
	entry, err := d.u32()
	if err != nil {
		return err
	}
	state, err := d.short()
	if err != nil {
		return err
	}
	switch {
	case nodes > math.MaxUint32:
		return fmt.Errorf("node count %d exceeds %d", nodes, uint64(math.MaxUint32))
	case nodes > 0 && (dim == 0 || uint64(entry) >= nodes || maxLevel >= maxLevels):
		return fmt.Errorf("invalid header: dim %d, entry %d, max level %d", dim, entry, maxLevel)
	case nodes == 0 && (dim != 0 || entry != 0 || maxLevel != 0):
		return fmt.Errorf("invalid empty graph header")
	}
	pcg := &rand.PCG{}
	if err := pcg.UnmarshalBinary(state); err != nil {
		return fmt.Errorf("invalid level generator state: %w", err)
	}
	g.pcg, g.rng = pcg, rand.New(pcg) // #nosec G404 -- levels need reproducibility, not secrecy.
	g.dim, g.entry, g.maxLevel = int(dim), entry, int(maxLevel)
	return d.nodes(g, nodes)
}

func (d *decoder) nodes(g *Graph, nodes uint64) error {
	vector := make([]float32, 0, g.dim)
	for node := range nodes {
		id, err := d.u64()
		if err != nil {
			return err
		}
		vector = vector[:0]
		err = d.u32s(uint64(g.dim), func(bits uint32) { vector = append(vector, math.Float32frombits(bits)) }) // #nosec G115 -- dim is a u32.
		if err != nil {
			return err
		}
		if err := vec.Validate(vector, g.metric); err != nil {
			return fmt.Errorf("node %d: %w", node, err)
		}
		levels, err := d.u32()
		if err != nil {
			return err
		}
		if levels == 0 || int(levels) > g.maxLevel+1 || (node == uint64(g.entry) && int(levels) != g.maxLevel+1) {
			return fmt.Errorf("node %d: invalid level count %d", node, levels)
		}
		links := make([][]uint32, levels)
		for l := range links {
			count, err := d.u32()
			if err != nil {
				return err
			}
			if int64(count) > int64(g.mmax(l)) {
				return fmt.Errorf("node %d level %d: %d neighbors exceed %d", node, l, count, g.mmax(l))
			}
			links[l] = make([]uint32, 0, count)
			err = d.u32s(uint64(count), func(n uint32) { links[l] = append(links[l], n) })
			if err != nil {
				return err
			}
		}
		if node > 0 && id <= g.ids[node-1] {
			g.repeated = true
		}
		g.ids = append(g.ids, id)
		g.data = append(g.data, vector...)
		if g.metric == vec.Cosine {
			g.norms = append(g.norms, norm(vector))
		}
		g.links = append(g.links, links)
	}
	return g.checkLinks()
}

// checkLinks requires every neighbor to be another node present on that level.
func (g *Graph) checkLinks() error {
	for node, levels := range g.links {
		for l, links := range levels {
			for _, n := range links {
				if int(n) >= len(g.links) || int(n) == node || len(g.links[n]) <= l {
					return fmt.Errorf("node %d level %d: invalid neighbor %d", node, l, n)
				}
			}
		}
	}
	return nil
}

var _ io.WriterTo = (*Graph)(nil)
