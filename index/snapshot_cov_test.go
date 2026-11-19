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
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/RoaringBitmap/roaring/v2"
	segment "github.com/vcaesar/bluge_segment_api"
	iceV1 "github.com/vcaesar/ice"
)

// covDictItr yields terms, each counted once, then err.
type covDictItr struct {
	terms  []string
	err    error
	pos    int
	closed int
}

func (c *covDictItr) Next() (segment.DictionaryEntry, error) {
	if c.pos < len(c.terms) {
		c.pos++
		return &dictionaryEntry{term: c.terms[c.pos-1], count: 1}, nil
	}
	return nil, c.err
}

func (c *covDictItr) Close() error { c.closed++; return nil }

// covItrDict iterates itr and contains the terms of has.
type covItrDict struct {
	segment.Dictionary
	itr segment.DictionaryIterator
	has string
}

func (d *covItrDict) Iterator(segment.Automaton, []byte, []byte) segment.DictionaryIterator {
	return d.itr
}
func (d *covItrDict) Contains(key []byte) (bool, error) { return string(key) == d.has, nil }

func covItrSnapshot(itrs ...*covDictItr) *Snapshot {
	var segs []segment.Segment
	for _, itr := range itrs {
		segs = append(segs, &covDictSegment{dict: &covItrDict{itr: itr}})
	}
	return snapshotWith(segs...)
}

func covTerms(t *testing.T, itr segment.DictionaryIterator) (terms []string, counts []uint64, err error) {
	t.Helper()
	for {
		e, err := itr.Next()
		if err != nil || e == nil {
			return terms, counts, err
		}
		terms, counts = append(terms, e.Term()), append(counts, e.Count())
	}
}

func TestDictionaryMergesSegments(t *testing.T) {
	snap := covItrSnapshot(&covDictItr{terms: []string{"a", "b", "c"}}, &covDictItr{terms: []string{"a", "b", "d"}})
	itr, err := snap.DictionaryIterator("f", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	terms, counts, err := covTerms(t, itr)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(terms, []string{"a", "b", "c", "d"}) || !reflect.DeepEqual(counts, []uint64{2, 2, 1, 1}) {
		t.Fatalf("terms %v counts %v", terms, counts)
	}
	d := itr.(*dictionary)
	d.Push(&segmentDictCursor{})
	if d.Len() != 1 {
		t.Fatalf("len %d after push", d.Len())
	}
}

func TestDictionaryIteratorErrors(t *testing.T) {
	tests := []struct {
		name string
		itrs []*covDictItr
	}{
		{"advance", []*covDictItr{{terms: []string{"a"}, err: errCov}}},
		{"advance duplicate", []*covDictItr{{terms: []string{"a", "b"}}, {terms: []string{"a"}, err: errCov}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			itr, err := covItrSnapshot(tt.itrs...).DictionaryIterator("f", nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := covTerms(t, itr); !errors.Is(err, errCov) {
				t.Fatalf("got %v", err)
			}
		})
	}
	t.Run("first entry", func(t *testing.T) {
		ok := &covDictItr{terms: []string{"a"}}
		snap := covItrSnapshot(ok, &covDictItr{err: errCov})
		if _, err := snap.DictionaryIterator("f", nil, nil, nil); !errors.Is(err, errCov) {
			t.Fatalf("got %v", err)
		}
		if ok.closed != 1 {
			t.Fatalf("open cursor closed %d times", ok.closed)
		}
	})
	t.Run("later dictionary", func(t *testing.T) {
		ok := &covDictItr{terms: []string{"a"}}
		snap := snapshotWith(&covDictSegment{dict: &covItrDict{itr: ok}}, &dictErrSegment{err: errCov})
		if _, err := snap.DictionaryIterator("f", nil, nil, nil); !errors.Is(err, errCov) {
			t.Fatalf("got %v", err)
		}
		if ok.closed != 1 {
			t.Fatalf("open cursor closed %d times", ok.closed)
		}
	})
}

func TestDictionaryContains(t *testing.T) {
	snap := snapshotWith(&covDictSegment{dict: &covItrDict{has: "x"}}, &covDictSegment{dict: &covItrDict{has: "y"}})
	lookup, err := snap.DictionaryLookup("f")
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]bool{"x": true, "y": true, "z": false} {
		if got, err := lookup.Contains([]byte(key)); got != want || err != nil {
			t.Errorf("Contains(%s) = %v %v", key, got, err)
		}
	}
	if err := lookup.Close(); err != nil {
		t.Fatal(err)
	}
	empty, err := snapshotWith().DictionaryLookup("f")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := empty.Contains([]byte("x")); got || err != nil {
		t.Fatalf("empty Contains = %v %v", got, err)
	}
}

// covStatsSegment hands out scripted collection stats, stored fields and
// doc value readers.
type covStatsSegment struct {
	segment.Segment
	stats    segment.CollectionStats
	err      error
	fields   []string
	visitErr error
}

func (s *covStatsSegment) CollectionStats(string) (segment.CollectionStats, error) {
	return s.stats, s.err
}
func (s *covStatsSegment) Fields() []string { return s.fields }
func (s *covStatsSegment) Count() uint64    { return 2 }
func (s *covStatsSegment) VisitStoredFields(uint64, segment.StoredFieldVisitor) error {
	return s.visitErr
}
func (s *covStatsSegment) DocumentValueReader([]string) (segment.DocumentValueReader, error) {
	return nil, s.err
}

func TestSnapshotFieldsAndStats(t *testing.T) {
	cfg := covConfig().WithVirtualField(NewFakeField("v", "x", true, false, false))
	seg := covSegment(t)
	snap := covSnapshot(cfg, &segmentSnapshot{segment: seg}, &segmentSnapshot{segment: covWrap(&covStatsSegment{fields: []string{"g"}})})
	fields, err := snap.Fields()
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(fields)
	if !reflect.DeepEqual(fields, []string{"_id", "f", "g", "n"}) {
		t.Fatalf("fields %v", fields)
	}

	stats, err := snap.CollectionStats("v")
	if err != nil || stats.TotalDocumentCount() != 6 || stats.DocumentCount() != 6 || stats.SumTotalTermFrequency() != 6 {
		t.Fatalf("virtual stats %v %v", stats, err)
	}
	single := covSnapshot(cfg, &segmentSnapshot{segment: seg})
	if stats, err := single.CollectionStats("f"); err != nil || stats.DocumentCount() != 4 {
		t.Fatalf("single segment stats %v %v", stats, err)
	}
	// a segment without stats for the field is skipped
	if stats, err := snap.CollectionStats("f"); err != nil || stats.DocumentCount() != 4 {
		t.Fatalf("stats %v %v", stats, err)
	}
	failing := covSnapshot(cfg, &segmentSnapshot{segment: seg}, &segmentSnapshot{segment: covWrap(&covStatsSegment{err: errCov})})
	if _, err := failing.CollectionStats("f"); !errors.Is(err, errCov) {
		t.Fatalf("got %v", err)
	}
	none := covSnapshot(cfg, &segmentSnapshot{segment: covWrap(&covStatsSegment{})}, &segmentSnapshot{segment: covWrap(&covStatsSegment{})})
	if stats, err := none.CollectionStats("f"); stats != nil || err != nil {
		t.Fatalf("no stats: %v %v", stats, err)
	}
}

func TestSnapshotVisitStoredFields(t *testing.T) {
	cfg := covConfig().WithVirtualField(NewFakeField("v", "x", true, false, false))
	snap := covSnapshot(cfg, &segmentSnapshot{segment: covSegment(t)})
	var seen []string
	err := snap.VisitStoredFields(1, func(field string, _ []byte) bool {
		seen = append(seen, field)
		return false
	})
	if err != nil || !reflect.DeepEqual(seen, []string{"v"}) {
		t.Fatalf("seen %v, %v", seen, err)
	}
	failing := covSnapshot(cfg, &segmentSnapshot{segment: covWrap(&covStatsSegment{visitErr: errCov})})
	if err := failing.VisitStoredFields(0, func(string, []byte) bool { return true }); !errors.Is(err, errCov) {
		t.Fatalf("got %v", err)
	}
}

func TestSnapshotDocumentValueReader(t *testing.T) {
	cfg := covConfig().WithVirtualField(NewFakeField("v", "x", false, false, true))
	snap := covSnapshot(cfg, &segmentSnapshot{segment: covSegment(t)}, &segmentSnapshot{segment: covSegment(t)})
	dvr, err := snap.DocumentValueReader([]string{"n", "f", "v"})
	if err != nil {
		t.Fatal(err)
	}
	nr := dvr.(*documentValueReader)
	terms := map[string]int{}
	var nums []int64
	err = nr.VisitDocumentNumbers(5, func(field string, _ []byte) { terms[field]++ },
		func(_ string, v int64) { nums = append(nums, v) })
	if err != nil {
		t.Fatal(err)
	}
	if terms["v"] != 1 || terms["f"] == 0 || !reflect.DeepEqual(nums, []int64{20}) {
		t.Fatalf("terms %v nums %v", terms, nums)
	}
	snap.offsets = append(snap.offsets, 100)
	if err := nr.VisitDocumentValues(150, func(string, []byte) {}); err != nil {
		t.Fatalf("past the segments: %v", err)
	}
	failing := covSnapshot(covConfig(), &segmentSnapshot{segment: covWrap(&covStatsSegment{err: errCov})})
	fdvr, err := failing.DocumentValueReader([]string{"f"})
	if err != nil {
		t.Fatal(err)
	}
	if err := fdvr.VisitDocumentValues(0, func(string, []byte) {}); !errors.Is(err, errCov) {
		t.Fatalf("got %v", err)
	}
}

// covFailWriter fails every write; covShortWriter writes 8-byte values
// short; covNthWriter fails the nth write.
type covFailWriter struct{}

func (covFailWriter) Write([]byte) (int, error) { return 0, errCov }

type covShortWriter struct{}

func (covShortWriter) Write(p []byte) (int, error) {
	if len(p) == 8 {
		return 7, nil
	}
	return len(p), nil
}

type covNthWriter struct{ n, failAt int }

func (w *covNthWriter) Write(p []byte) (int, error) {
	w.n++
	if w.n == w.failAt {
		return 0, errCov
	}
	return len(p), nil
}

func TestSnapshotWriteToErrors(t *testing.T) {
	// the bluge header, one record and the CRC; 4054 type bytes make the
	// record end the 4096 byte buffer, so only the CRC write flushes it
	for _, typLen := range []int{4054, 5000} {
		snap := &Snapshot{segment: []*segmentSnapshot{{segmentType: strings.Repeat("t", typLen), segmentVersion: 1}}}
		if _, err := snap.WriteTo(covFailWriter{}, nil); !errors.Is(err, errCov) {
			t.Fatalf("type length %d: got %v", typLen, err)
		}
	}
	ss := &segmentSnapshot{segmentType: "t"}
	if _, err := recordSegment(covShortWriter{}, ss, 1, "t", 1); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write: got %v", err)
	}
	// type length, type, version, id, four values, then the deletions
	if _, err := recordSegment(&covNthWriter{failAt: 9}, ss, 1, "t", 1); !errors.Is(err, errCov) {
		t.Fatalf("deletions write: got %v", err)
	}
}

func TestSnapshotReadTrailingDeleted(t *testing.T) {
	deleted := roaring.BitmapOf(1, 2)
	snap := &Snapshot{segment: []*segmentSnapshot{{segmentType: "t", segmentVersion: 1, deleted: deleted}}}
	var buf bytes.Buffer
	if _, err := snap.WriteTo(&buf, nil); err != nil {
		t.Fatal(err)
	}
	d := deleted.Clone()
	d.RunOptimize()
	bm, err := d.ToBytes()
	if err != nil {
		t.Fatal(err)
	}
	payload := buf.Bytes()[:buf.Len()-crcWidth]
	head := payload[:len(payload)-len(bm)-1]
	// the bitmap is short, so its length is a one byte uvarint
	patched := append(append(append(bytes.Clone(head), payload[len(head)]+1), bm...), 0)
	if _, err := (&Snapshot{}).ReadFrom(bytes.NewReader(patched)); err == nil {
		t.Fatal("expected trailing data error")
	}
	// the unpatched payload reads back
	var back Snapshot
	if _, err := back.ReadFrom(bytes.NewReader(payload)); err != nil || !back.segment[0].deleted.Equals(deleted) {
		t.Fatalf("read back %v %v", back.segment, err)
	}
}

func TestSnapshotBackupErrors(t *testing.T) {
	seg := covSegment(t)
	newSnap := func(ss *segmentSnapshot) *Snapshot {
		s := covSnapshot(covConfig(), ss)
		s.parent.directory = NewInMemoryDirectory()
		return s
	}
	persisted := func(load error) *segmentSnapshot {
		w := &Writer{directory: &covFaultDir{Directory: NewInMemoryDirectory(), load: func(string, uint64) error { return load }}}
		return &segmentSnapshot{segment: &segmentWrapper{Segment: seg.Segment, refCounter: noOpRefCounter{}, persisted: true, writer: w}}
	}
	tests := []struct {
		name   string
		snap   *Snapshot
		remote func() Directory
	}{
		{"list", newSnap(&segmentSnapshot{segment: seg}), func() Directory {
			return &covFaultDir{Directory: NewInMemoryDirectory(), list: func(string) error { return errCov }}
		}},
		{"persist segment", newSnap(&segmentSnapshot{segment: seg}), func() Directory {
			return &covFaultDir{Directory: NewInMemoryDirectory(), persist: covFailKind(ItemKindSegment)}
		}},
		{"remove stale", newSnap(&segmentSnapshot{segment: seg}), func() Directory {
			d := &covFaultDir{Directory: NewInMemoryDirectory(), remove: covFailKind(ItemKindSegment)}
			if err := d.Persist(ItemKindSegment, 1, covSegment(t, covDocs()[:1]...), nil); err != nil {
				t.Fatal(err)
			}
			return d
		}},
		{"load source", newSnap(persisted(errCov)), func() Directory { return NewInMemoryDirectory() }},
		{"persist snapshot", newSnap(&segmentSnapshot{segment: seg}), func() Directory {
			return &covFaultDir{Directory: NewInMemoryDirectory(), persist: covFailKind(ItemKindSnapshot)}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.snap.Backup(tt.remote(), nil); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	t.Run("source graph listing", func(t *testing.T) {
		snap := newSnap(&segmentSnapshot{segment: seg})
		snap.parent.directory = &covFaultDir{Directory: NewInMemoryDirectory(), list: func(k string) error {
			return covFailKind(k)(ItemKindGraph, 0)
		}}
		if err := snap.Backup(NewInMemoryDirectory(), nil); !errors.Is(err, errCov) {
			t.Fatalf("got %v", err)
		}
	})
	if err := (&Snapshot{}).backupGraphs(nil, nil); err != nil {
		t.Fatalf("empty snapshot graphs: %v", err)
	}
	cancel := make(chan struct{})
	close(cancel)
	if _, err := (dataWriter{}).WriteTo(io.Discard, cancel); err == nil {
		t.Fatal("expected canceled backup")
	}
}

func TestSnapshotBackupGraphErrors(t *testing.T) {
	src := t.TempDir()
	dir := NewFileSystemDirectory(src)
	// graphs of segment 1 (backed up) and segment 9 (not in the snapshot)
	for _, id := range []uint64{1<<graphKeyBits | 1, 9 << graphKeyBits} {
		if err := os.WriteFile(filepath.Join(src, dir.fileName(ItemKindGraph, id)), []byte("graph"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	snap := covSnapshot(covConfig(), &segmentSnapshot{segment: covSegment(t)})
	snap.parent.directory = dir
	tests := []struct {
		name  string
		fault func(*covFaultDir)
	}{
		{"list remote", func(d *covFaultDir) { d.list = func(k string) error { return covFailKind(k)(ItemKindGraph, 0) } }},
		{"persist graph", func(d *covFaultDir) { d.persist = covFailKind(ItemKindGraph) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remote := &covFaultDir{Directory: NewFileSystemDirectory(t.TempDir())}
			tt.fault(remote)
			if err := snap.Backup(remote, nil); !errors.Is(err, errCov) {
				t.Fatalf("got %v", err)
			}
		})
	}
	remote := NewFileSystemDirectory(t.TempDir())
	if err := snap.Backup(remote, nil); err != nil {
		t.Fatal(err)
	}
	graphs, err := remote.List(ItemKindGraph)
	if err != nil || !reflect.DeepEqual(graphs, []uint64{1<<graphKeyBits | 1}) {
		t.Fatalf("backed up graphs %v %v", graphs, err)
	}
}

func TestSegmentSnapshotAccessors(t *testing.T) {
	seg := covSegment(t)
	ss := &segmentSnapshot{segment: seg, deleted: roaring.BitmapOf(1)}
	if live := ss.DocNumbersLive(); !live.Equals(roaring.BitmapOf(0, 2, 3)) {
		t.Fatalf("live %v", live)
	}
	if fields := ss.Fields(); len(fields) != 3 {
		t.Fatalf("fields %v", fields)
	}
	if ss.Segment() != seg || ss.Close() != nil {
		t.Fatal("unexpected segment")
	}
	huge := &segmentSnapshot{segment: covWrap(&sizeTestSegment{count: math.MaxUint64})}
	if huge.FullSize() != math.MaxInt64 {
		t.Fatalf("full size %d", huge.FullSize())
	}
}

func TestLoadSegmentPluginVersion(t *testing.T) {
	plugins := covConfig().supportedSegmentPlugins
	if _, err := loadSegmentPlugin(plugins, iceV1.Type, 9999); err == nil || !strings.Contains(err.Error(), "unsupported version") {
		t.Fatalf("got %v", err)
	}
	if _, err := OpenWriter(covConfig().WithSegmentVersion(9999)); err == nil {
		t.Fatal("expected error")
	}
}

func TestConfigBuilders(t *testing.T) {
	cfg := covConfig().
		WithStreamIndexing(7).
		WithVectorStorage(VectorStorageMemory).
		DisableOptimizeConjunction().
		DisableOptimizeConjunctionUnadorned().
		WithUnsafeBatches()
	if cfg.IndexMode != IndexModeStream || cfg.StreamMemoryBudget != 7 || cfg.VectorStorage != VectorStorageMemory ||
		cfg.OptimizeConjunction || cfg.OptimizeConjunctionUnadorned || !cfg.UnsafeBatch {
		t.Fatalf("unexpected config %+v", cfg)
	}
}
