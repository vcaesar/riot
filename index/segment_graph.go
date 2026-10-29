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

package index

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"hash/fnv"
	"io"
	"io/fs"

	segment "github.com/vcaesar/bluge_segment_api"

	"github.com/vcaesar/riot/hnsw"
)

// A graph item is a header followed by an hnsw graph, little-endian:
//
//	magic "RIOTHNSW", version u32, segment id u64, document count u64,
//	field (u32 length + bytes), CRC-32 (IEEE) of the preceding header bytes.
//
// Version 3 zero-pads the header to a multiple of 8 bytes, then follows with
// hnsw.Graph.WriteMappedTo, so a memory-mapped item is used in place.
// Version 2 follows with hnsw.Graph.WriteLinksTo, whose ids and vectors are
// read back from the segment's stored fields; version 1, still read, follows
// with hnsw.Graph.WriteTo. The graph carries its own metric, parameters and
// checksum. Graph items are a cache: a missing, corrupt or mismatched item is
// rebuilt, never fatal.
const (
	graphMagic         = "RIOTHNSW"
	graphVersion       = 2
	graphVersionFull   = 1
	graphVersionMapped = 3
	graphHeaderFixed   = len(graphMagic) + 4 + 8 + 8 + 4

	graphKeyBits  = 16
	maxGraphField = 1 << 16
)

// graphItemID packs a segment id with a hash of key, so cleanup can map an
// item to its segment without reading it. Hash collisions are detected by the
// header and only cost persistence for one of the colliding keys.
func graphItemID(segmentID uint64, key graphKey) (uint64, bool) {
	if segmentID >= 1<<(64-graphKeyBits) {
		return 0, false
	}
	h := fnv.New64a()
	_, err := fmt.Fprintf(h, "%q %q %d %d", key.field, key.metric, key.m, key.efConstruction)
	if err == nil && key.quantization != hnsw.QuantizationNone {
		// Float32 keys keep the ids of items written before quantization.
		_, err = fmt.Fprintf(h, " %v", key.quantization)
	}
	if err != nil {
		return 0, false
	}
	return segmentID<<graphKeyBits | h.Sum64()&(1<<graphKeyBits-1), true
}

type graphItem struct {
	segmentID, count uint64
	field            string
	graph            *hnsw.Graph
	mapped           bool // write version 3, with the vectors
}

// WriteTo ignores cancellation: Writer.close waits for started persists so a
// graph built before Close is kept.
func (g graphItem) WriteTo(w io.Writer, _ chan struct{}) (int64, error) {
	le := binary.LittleEndian
	version := uint32(graphVersion)
	if g.mapped {
		version = graphVersionMapped
	}
	header := append([]byte(nil), graphMagic...)
	header = le.AppendUint32(header, version)
	header = le.AppendUint64(header, g.segmentID)
	header = le.AppendUint64(header, g.count)
	header = le.AppendUint32(header, uint32(len(g.field))) // #nosec G115 -- bounded by maxGraphField.
	header = append(header, g.field...)
	header = le.AppendUint32(header, crc32.ChecksumIEEE(header))
	if g.mapped {
		header = append(header, make([]byte, graphHeaderPad(len(header)))...)
	}
	n, err := w.Write(header)
	if err != nil {
		return int64(n), err
	}
	var m int64
	if g.mapped {
		m, err = g.graph.WriteMappedTo(w)
	} else {
		m, err = g.graph.WriteLinksTo(w)
	}
	return int64(n) + m, err
}

// graphHeaderPad aligns a mapped graph to 8 bytes within its item.
func graphHeaderPad(n int) int { return (8 - n%8) % 8 }

// readGraphHeader reads an item's header and returns its version.
func readGraphHeader(r io.Reader) (graphItem, uint32, error) {
	var item graphItem
	fixed := make([]byte, graphHeaderFixed)
	if _, err := io.ReadFull(r, fixed); err != nil {
		return item, 0, err
	}
	le := binary.LittleEndian
	if string(fixed[:len(graphMagic)]) != graphMagic {
		return item, 0, fmt.Errorf("not a vector graph item")
	}
	rest := fixed[len(graphMagic):]
	version := le.Uint32(rest)
	if version < graphVersionFull || version > graphVersionMapped {
		return item, 0, fmt.Errorf("unsupported vector graph item version %d", version)
	}
	item.segmentID, item.count = le.Uint64(rest[4:]), le.Uint64(rest[12:])
	fieldLen := le.Uint32(rest[20:])
	if fieldLen > maxGraphField {
		return item, 0, fmt.Errorf("vector graph field length %d exceeds %d", fieldLen, maxGraphField)
	}
	tail := make([]byte, int(fieldLen)+4)
	if _, err := io.ReadFull(r, tail); err != nil {
		return item, 0, err
	}
	if crc32.Update(crc32.ChecksumIEEE(fixed), crc32.IEEETable, tail[:fieldLen]) != le.Uint32(tail[fieldLen:]) {
		return item, 0, fmt.Errorf("vector graph header checksum mismatch")
	}
	item.field = string(tail[:fieldLen])
	if version == graphVersionMapped {
		if _, err := io.ReadFull(r, make([]byte, graphHeaderPad(graphHeaderFixed+len(tail)))); err != nil {
			return item, 0, err
		}
	}
	return item, version, nil
}

func (g graphItem) matches(segmentID, count uint64, field string) bool {
	return g.segmentID == segmentID && g.count == count && g.field == field
}

func graphMatches(graph *hnsw.Graph, key graphKey) bool {
	params := graph.Params()
	return graph.Metric() == key.metric && params.M == key.m && params.EfConstruction == key.efConstruction &&
		params.Quantization == key.quantization
}

// errGraphVectors marks a failure reading the segment's vectors, which says
// nothing about the item itself.
var errGraphVectors = errors.New("error reading segment vectors")

// readGraph reads the rest of an item of the given version after its header,
// all of whose bytes lr must hold. A version 3 graph is mapped from data,
// in place when inPlace is set, else from a heap copy.
func readGraph(ctx context.Context, data *segment.Data, lr *io.LimitedReader, version uint32,
	seg segment.Segment, key graphKey, inPlace bool) (*hnsw.Graph, error) {
	var graph *hnsw.Graph
	var err error
	switch version {
	case graphVersionFull:
		graph, err = hnsw.Read(lr)
	case graphVersionMapped:
		var b []byte
		if b, err = data.Read(data.Len()-int(lr.N), data.Len()); err != nil {
			return nil, err
		}
		if !inPlace {
			b = bytes.Clone(b)
		}
		lr.N = 0
		if graph, err = hnsw.Map(b); err == nil {
			err = checkGraphIDs(graph, seg.Count())
		}
	default:
		ids, vectors, vecErr := segmentVectors(ctx, seg, key.field, key.metric)
		if vecErr != nil {
			return nil, fmt.Errorf("%w: %w", errGraphVectors, vecErr)
		}
		graph, err = hnsw.ReadLinks(lr, ids, vectors)
	}
	if err != nil {
		return nil, err
	}
	if lr.N != 0 {
		return nil, fmt.Errorf("%d trailing bytes after vector graph", lr.N)
	}
	return graph, nil
}

// checkGraphIDs requires a mapped graph's document numbers to be the
// segment's, as those of graphs read with the segment's vectors are.
func checkGraphIDs(graph *hnsw.Graph, count uint64) error {
	for _, id := range graph.IDs() {
		if id >= count {
			return fmt.Errorf("vector graph document %d out of %d", id, count)
		}
	}
	return nil
}

// loadGraph returns the persisted graph of seg, or nil and whether a freshly
// built graph should be persisted in its place. Version 2 items read seg's
// vectors, which is far cheaper than a build; version 3 items read nothing
// more. A non-nil mapping backs a graph used in place and must be closed
// after the graph's last use.
func (s *Writer) loadGraph(ctx context.Context, seg segment.Segment, segmentID uint64,
	key graphKey) (graph *hnsw.Graph, mapping io.Closer, persist bool) {
	id, ok := graphItemID(segmentID, key)
	if !ok {
		return nil, nil, false
	}
	data, closer, err := s.directory.Load(ItemKindGraph, id)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && data == nil) {
		return nil, nil, !s.readOnly
	}
	if err != nil {
		s.fireAsyncError(fmt.Errorf("error loading vector graph %x: %w", id, err))
		return nil, nil, false
	}
	inPlace := s.config.VectorStorage == VectorStorageDisk && closer != nil
	lr := &io.LimitedReader{R: data.Reader(), N: int64(data.Len())}
	item, version, err := readGraphHeader(lr)
	if err == nil && !item.matches(segmentID, seg.Count(), key.field) {
		err = errGraphMismatch
	}
	if err == nil {
		item.graph, err = readGraph(ctx, data, lr, version, seg, key, inPlace)
	}
	keep := err == nil && version == graphVersionMapped && inPlace && graphMatches(item.graph, key)
	if !keep {
		s.closeGraphItem(id, closer)
	}
	if errors.Is(err, errGraphMismatch) || errors.Is(err, errGraphVectors) {
		return nil, nil, false
	}
	if err != nil {
		return nil, nil, s.discardGraph(id, err)
	}
	if !graphMatches(item.graph, key) {
		return nil, nil, false
	}
	if keep {
		return item.graph, closer, false
	}
	return item.graph, nil, false
}

// discardGraph handles an unreadable item and reports whether a rebuilt
// graph should replace it.
func (s *Writer) discardGraph(id uint64, err error) bool {
	if s.readOnly {
		s.fireAsyncError(fmt.Errorf("error reading vector graph %x: %w", id, err))
		return false
	}
	// Torn or corrupt, e.g. after a crash mid-persist: replace it.
	if err := s.directory.Remove(ItemKindGraph, id); err != nil {
		s.fireAsyncError(fmt.Errorf("error removing corrupt vector graph %x: %w", id, err))
		return false
	}
	return true
}

func (s *Writer) closeGraphItem(id uint64, closer io.Closer) {
	if closer == nil {
		return
	}
	if err := closer.Close(); err != nil {
		s.fireAsyncError(fmt.Errorf("error closing vector graph %x: %w", id, err))
	}
}

// errGraphMismatch marks a valid item of another segment or field, e.g. a
// hash collision, which is kept.
var errGraphMismatch = errors.New("vector graph item belongs to another segment")

// persistGraph writes graph in the background, keeping disk writes out of
// the search that built it. It is a no-op once the writer is closing.
func (s *Writer) persistGraph(segmentID, count uint64, key graphKey, graph *hnsw.Graph) {
	id, ok := graphItemID(segmentID, key)
	if !ok || len(key.field) > maxGraphField {
		return
	}
	s.graphPersistMu.Lock()
	defer s.graphPersistMu.Unlock()
	select {
	case <-s.closeCh:
		return
	default:
	}
	s.graphPersists.Go(func() {
		item := graphItem{segmentID: segmentID, count: count, field: key.field, graph: graph,
			mapped: s.config.VectorStorage == VectorStorageDisk}
		if err := s.directory.Persist(ItemKindGraph, id, item, nil); err != nil {
			s.fireAsyncError(fmt.Errorf("error persisting vector graph %x: %w", id, err))
		}
	})
}

// removeOrphanGraphs removes graph items whose segment is gone. Graphs are
// listed before segments: a graph is only persisted for a segment already in
// the directory, so one written in between is never mistaken for an orphan.
func (s *Writer) removeOrphanGraphs() error {
	graphs, err := s.directory.List(ItemKindGraph)
	if err != nil || len(graphs) == 0 {
		return err
	}
	segments, err := s.directory.List(ItemKindSegment)
	if err != nil {
		return err
	}
	live := make(map[uint64]struct{}, len(segments))
	for _, id := range segments {
		live[id] = struct{}{}
	}
	var errs []error
	for _, id := range graphs {
		if _, ok := live[id>>graphKeyBits]; ok {
			continue
		}
		if err := s.directory.Remove(ItemKindGraph, id); err != nil {
			errs = append(errs, fmt.Errorf("error removing orphan vector graph %x: %w", id, err))
		}
	}
	return errors.Join(errs...)
}
