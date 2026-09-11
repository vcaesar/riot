// Copyright (c) 2026 The Bluge Authors.
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
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"testing"
	"testing/iotest"

	"github.com/RoaringBitmap/roaring/v2"
	segment "github.com/vcaesar/bluge_segment_api"
)

func snapshotFixture(t *testing.T, version byte) []byte {
	t.Helper()
	b := bytes.NewBuffer([]byte{version, 1, 3, 'i', 'c', 'e', 0, 0, 0, 1, 7})
	values := []uint64{}
	if version == 3 {
		values = append(values, 1234, 10)
	}
	if version >= 2 {
		values = append(values, uint64(20), uint64(40))
	}
	for _, value := range values {
		if err := binary.Write(b, binary.BigEndian, value); err != nil {
			t.Fatal(err)
		}
	}
	deleted := roaring.BitmapOf(2, 5)
	data, err := deleted.ToBytes()
	if err != nil {
		t.Fatal(err)
	}
	var v [10]byte
	n := binary.PutUvarint(v[:], uint64(len(data)))
	b.Write(v[:n])
	b.Write(data)
	return b.Bytes()
}

func TestSnapshotLegacyHelpers(t *testing.T) {
	data := snapshotFixture(t, 1)
	for end := 1; end <= len(data); end++ {
		var s Snapshot
		n, err := s.readFromVersion1(bufio.NewReader(iotest.OneByteReader(bytes.NewReader(data[1:end]))))
		if n != int64(end-1) || (err == nil) != (end == len(data)) {
			t.Fatalf("snapshot end=%d n=%d err=%v", end, n, err)
		}
		if err != nil && len(s.segment) != 0 {
			t.Fatal("partial snapshot mutated receiver")
		}
		if err == nil && (len(s.segment) != 1 || s.segment[0].id != 7) {
			t.Fatal("legacy snapshot identity lost")
		}
	}
	for end := 2; end <= len(data); end++ {
		var s Snapshot
		n, ss, err := s.readSegmentSnapshot(bufio.NewReader(iotest.OneByteReader(bytes.NewReader(data[2:end]))))
		if n != int64(end-2) || (err == nil) != (end == len(data)) {
			t.Fatalf("segment end=%d n=%d err=%v", end, n, err)
		}
		if err != nil && ss != nil {
			t.Fatal("partial segment returned")
		}
		if err == nil && (ss.id != 7 || ss.segmentType != "ice" || ss.segmentVersion != 1 ||
			!ss.deleted.Equals(roaring.BitmapOf(2, 5)) || ss.docTimeMin != 0 || ss.docTimeMax != 0) {
			t.Fatal("legacy segment metadata lost")
		}
	}
	// A complete first segment must not be published if the second is malformed.
	payload := append([]byte{2}, data[2:]...)
	payload = append(payload, 0x80)
	for _, populated := range []bool{false, true} {
		original := &segmentSnapshot{id: 99}
		var s Snapshot
		if populated {
			s.segment = []*segmentSnapshot{original}
		}
		n, err := s.readFromVersion1(bufio.NewReader(bytes.NewReader(payload)))
		if err == nil || n != int64(len(payload)) {
			t.Fatalf("malformed snapshot n=%d err=%v", n, err)
		}
		if populated {
			if len(s.segment) != 1 || s.segment[0] != original {
				t.Fatal("populated snapshot mutated")
			}
			if _, err := s.readFromVersion1(bufio.NewReader(bytes.NewReader(data[1:]))); err == nil {
				t.Fatal("populated snapshot accepted")
			}
		} else if len(s.segment) != 0 {
			t.Fatal("malformed snapshot mutated receiver")
		}
	}
}

func TestSnapshotReadVarLenString(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		want string
		n    int
		fail bool
	}{
		{"empty", []byte{0}, "", 1, false},
		{"short", []byte{3, 'i', 'c', 'e'}, "ice", 4, false},
		{"trailing", []byte{1, 'a', 'z'}, "a", 2, false},
		{"missing", nil, "", 0, true},
		{"truncated length", []byte{0x80}, "", 1, true},
		{"overflow", bytes.Repeat([]byte{0xff}, 10), "", 10, true},
		{"oversized", append(bytes.Repeat([]byte{0xff}, 9), 1), "", 10, true},
		{"truncated string", []byte{3, 'i'}, "", 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			br := bufio.NewReader(iotest.OneByteReader(bytes.NewReader(tc.data)))
			n, str, err := readVarLenString(br)
			if n != tc.n || str != tc.want || (err != nil) != tc.fail {
				t.Fatalf("n=%d str=%q err=%v", n, str, err)
			}
			if tc.name == "trailing" {
				if b, err := br.ReadByte(); err != nil || b != 'z' {
					t.Fatalf("trailing byte lost: %q %v", b, err)
				}
			}
		})
	}
}

func TestSnapshotFormats(t *testing.T) {
	for version := byte(1); version <= 3; version++ {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			data := snapshotFixture(t, version)
			var s Snapshot
			n, err := s.ReadFrom(iotest.OneByteReader(bytes.NewReader(data)))
			if err != nil || n != int64(len(data)) {
				t.Fatalf("read %d/%d: %v", n, len(data), err)
			}
			ss := s.Segments()[0]
			if ss.ID() != 7 || !ss.Deleted().Equals(roaring.BitmapOf(2, 5)) {
				t.Fatal("identity or deletions lost")
			}
			min, max := ss.Timestamp()
			if version == 1 && (min != 0 || max != 0) {
				t.Fatal("legacy time is not unknown")
			}
			if version >= 2 && (min != 20 || max != 40) {
				t.Fatal("time lost")
			}
			if version == 3 && (ss.DocNum() != 10 || ss.SegmentSize() != 1234) {
				t.Fatal("metadata lost")
			}
			var out bytes.Buffer
			written, err := s.WriteTo(&out, nil)
			if err != nil || written != int64(out.Len()) {
				t.Fatalf("write %d: %v", written, err)
			}
			encoded := out.Bytes()
			if encoded[0] != 3 {
				t.Fatal("not v3")
			}
			if binary.BigEndian.Uint32(encoded[len(encoded)-4:]) != crc32.ChecksumIEEE(encoded[:len(encoded)-4]) {
				t.Fatal("bad CRC")
			}
			var again Snapshot
			read, err := again.ReadFrom(bytes.NewReader(encoded[:len(encoded)-4]))
			if err != nil || read != written-4 {
				t.Fatalf("roundtrip read %d: %v", read, err)
			}
			if lo, hi := again.Segments()[0].Timestamp(); lo != min || hi != max {
				t.Fatal("roundtrip time lost")
			}
			for end := 0; end < len(data); end++ {
				var truncated Snapshot
				n, err := truncated.ReadFrom(iotest.OneByteReader(bytes.NewReader(data[:end])))
				if err == nil || n != int64(end) || len(truncated.segment) != 0 {
					t.Fatalf("truncation %d: n=%d err=%v segments=%d", end, n, err, len(truncated.segment))
				}
			}
		})
	}
}

func TestSnapshotMalformed(t *testing.T) {
	cases := [][]byte{{0}, {4}, {0x80}, bytes.Repeat([]byte{0xff}, 11), {3, 0x80}, {3, 1, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 1}}
	reversed := snapshotFixture(t, 3)
	binary.BigEndian.PutUint64(reversed[27:35], 50)
	cases = append(cases, reversed)
	bitmap := snapshotFixture(t, 1)
	bitmap[12] = 0xff
	cases = append(cases, bitmap)
	for j, data := range cases {
		var s Snapshot
		if _, err := s.ReadFrom(bytes.NewReader(data)); err == nil {
			t.Errorf("case %d accepted", j)
		}
	}
	for _, version := range []byte{1, 2, 3} {
		var empty Snapshot
		if n, err := empty.ReadFrom(bytes.NewReader([]byte{version, 0})); err != nil || n != 2 {
			t.Fatalf("empty: %d %v", n, err)
		}
	}
}

type limitedSnapshotWriter struct {
	remaining, n int
	err          error
}

func (w *limitedSnapshotWriter) Write(p []byte) (int, error) {
	n := len(p)
	if n > w.remaining {
		n = w.remaining
	}
	w.remaining -= n
	w.n += n
	if n != len(p) {
		return n, w.err
	}
	return n, nil
}

func TestSnapshotWriteFailures(t *testing.T) {
	var s Snapshot
	if _, err := s.ReadFrom(bytes.NewReader(snapshotFixture(t, 3))); err != nil {
		t.Fatal(err)
	}
	var full bytes.Buffer
	if _, err := s.WriteTo(&full, nil); err != nil {
		t.Fatal(err)
	}
	for _, writeErr := range []error{nil, io.ErrClosedPipe} {
		for limit := 0; limit < full.Len(); limit++ {
			w := &limitedSnapshotWriter{remaining: limit, err: writeErr}
			n, err := s.WriteTo(w, nil)
			if err == nil || n != int64(w.n) {
				t.Fatalf("write %d: %d/%d %v", limit, n, w.n, err)
			}
			w = &limitedSnapshotWriter{remaining: limit, err: writeErr}
			n2, err := recordSegment(w, s.segment[0], 7, "ice", 1)
			if n2 != w.n {
				t.Fatalf("record count %d/%d", n2, w.n)
			}
			if limit < full.Len()-6 && err == nil {
				t.Fatalf("record error missing at %d", limit)
			}
		}
	}
}

type snapshotTestCloser struct{ count int }

func (c *snapshotTestCloser) Close() error { c.count++; return nil }

type snapshotTestDirectory struct {
	Directory
	payload                       []byte
	snapshotCloser, segmentCloser snapshotTestCloser
}

func (d *snapshotTestDirectory) Load(kind string, id uint64) (*segment.Data, io.Closer, error) {
	if kind == ItemKindSnapshot {
		return segment.NewDataBytes(d.payload), &d.snapshotCloser, nil
	}
	data, _, err := d.Directory.Load(kind, id)
	return data, &d.segmentCloser, err
}

func TestSnapshotLoadValidationAndRefs(t *testing.T) {
	cfg := InMemoryOnlyConfig().WithNormCalc(func(string, int) float32 { return 1 })
	plugin, err := loadSegmentPlugin(cfg.supportedSegmentPlugins, cfg.SegmentType, cfg.SegmentVersion)
	if err != nil {
		t.Fatal(err)
	}
	doc := &FakeDocument{NewFakeField("_id", "a", true, false, false)}
	seg, _, err := (&Writer{segPlugin: plugin, config: cfg}).newSegment([]segment.Document{doc})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := seg.Close(); err != nil {
			t.Error(err)
		}
	}()
	dir := &snapshotTestDirectory{Directory: NewInMemoryDirectory()}
	if err := dir.Persist(ItemKindSegment, 1, seg, nil); err != nil {
		t.Fatal(err)
	}
	s := &Snapshot{segment: []*segmentSnapshot{{id: 1, segment: seg}, {id: 2, segment: seg}}}
	var b bytes.Buffer
	if _, err := s.WriteTo(&b, nil); err != nil {
		t.Fatal(err)
	}
	cfg.ValidateSnapshotCRC = true
	w := &Writer{config: cfg, directory: dir}
	dir.payload = b.Bytes()
	if _, err := w.loadSnapshot(1); err == nil {
		t.Fatal("missing segment accepted")
	}
	if dir.segmentCloser.count != 1 || dir.snapshotCloser.count != 1 {
		t.Fatalf("leaked refs: %+v", dir)
	}
	for _, data := range [][]byte{{}, {1, 0, 0}, append([]byte(nil), b.Bytes()...), append(append([]byte(nil), b.Bytes()[:b.Len()-4]...), 1, 0, 0, 0, 0)} {
		if len(data) == b.Len() {
			data[len(data)-1] ^= 1
		}
		dir.payload = data
		if _, err := w.loadSnapshot(1); err == nil {
			t.Fatal("corruption accepted")
		}
	}
	if dir.segmentCloser.count != 1 {
		t.Fatal("opened segments before validating snapshot")
	}
}

func TestSnapshotFilteredLoad(t *testing.T) {
	for version := byte(1); version <= 3; version++ {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			cfg := InMemoryOnlyConfig().WithNormCalc(func(string, int) float32 { return 1 }).WithTimeRange(20, 20)
			cfg.ValidateSnapshotCRC = true
			plugin, err := loadSegmentPlugin(cfg.supportedSegmentPlugins, cfg.SegmentType, cfg.SegmentVersion)
			if err != nil {
				t.Fatal(err)
			}
			dir := &snapshotTestDirectory{Directory: NewInMemoryDirectory()}
			w := &Writer{config: cfg, directory: dir, segPlugin: plugin}
			b := bytes.NewBuffer([]byte{version, 3})
			for j, timestamp := range []uint64{10, 20, 0} {
				doc := &FakeDocument{NewFakeField("_id", fmt.Sprint(j), true, false, false)}
				seg, _, err := w.newSegment([]segment.Document{doc})
				if err != nil {
					t.Fatal(err)
				}
				if err := dir.Persist(ItemKindSegment, uint64(j+1), seg, nil); err != nil {
					t.Fatal(err)
				}
				b.Write([]byte{3, 'i', 'c', 'e'})
				if err := binary.Write(b, binary.BigEndian, cfg.SegmentVersion); err != nil {
					t.Fatal(err)
				}
				b.WriteByte(byte(j + 1))
				var values []uint64
				if version == 3 {
					values = append(values, uint64(seg.Size()), 1)
				}
				if version >= 2 {
					values = append(values, timestamp, timestamp)
				}
				for _, v := range values {
					if err := binary.Write(b, binary.BigEndian, v); err != nil {
						t.Fatal(err)
					}
				}
				b.WriteByte(0)
				if err := seg.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if err := binary.Write(b, binary.BigEndian, crc32.ChecksumIEEE(b.Bytes())); err != nil {
				t.Fatal(err)
			}
			dir.payload = b.Bytes()
			s, err := w.loadSnapshot(1)
			if err != nil {
				t.Fatal(err)
			}
			want := uint64(2)
			firstID := "1"
			if version == 1 {
				want, firstID = 3, "0"
			}
			if count, err := s.Count(); err != nil || count != want {
				t.Fatalf("count=%d err=%v", count, err)
			}
			var got string
			if err := s.VisitStoredFields(0, func(name string, value []byte) bool {
				if name == "_id" {
					got = string(value)
				}
				return true
			}); err != nil {
				t.Fatal(err)
			}
			if got != firstID {
				t.Fatalf("offsets: first ID=%q want=%q", got, firstID)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			if dir.segmentCloser.count != int(want) {
				t.Fatalf("close count=%d want=%d", dir.segmentCloser.count, want)
			}
		})
	}
}

func TestSnapshotRangeSafety(t *testing.T) {
	for _, bounds := range [][2]int64{{10, 20}, {0, 20}, {10, 0}, {-20, -10}} {
		cfg := InMemoryOnlyConfig().WithTimeRange(bounds[0], bounds[1])
		cfg.DirectoryFunc = func() Directory { t.Fatal("filtered writer touched directory"); return nil }
		if _, err := OpenWriter(cfg); err == nil {
			t.Fatal("filtered writer accepted")
		}
		if _, err := OpenOfflineWriter(cfg); err == nil {
			t.Fatal("filtered offline writer accepted")
		}
		if cfg.excludes(&segmentSnapshot{}) {
			t.Fatal("unknown time excluded")
		}
	}
	if _, err := OpenReader(InMemoryOnlyConfig().WithTimeRange(20, 10)); err == nil {
		t.Fatal("reversed range accepted")
	}
	cfg := InMemoryOnlyConfig().WithTimeRange(20, 40)
	for _, tc := range []struct {
		min, max int64
		excluded bool
	}{{1, 19, true}, {41, 50, true}, {1, 20, false}, {40, 50, false}, {25, 35, false}, {0, 0, false}} {
		if cfg.excludes(&segmentSnapshot{docTimeMin: tc.min, docTimeMax: tc.max}) != tc.excluded {
			t.Errorf("range %+v", tc)
		}
	}
}

type failingTimestampSegment struct {
	segment.Segment
	min, max int64
}

func (s failingTimestampSegment) Size() int                 { return 0 }
func (s failingTimestampSegment) Count() uint64             { return 1 }
func (s failingTimestampSegment) Timestamp() (int64, int64) { return s.min, s.max }
func (s failingTimestampSegment) VisitStoredFields(uint64, segment.StoredFieldVisitor) error {
	return io.ErrClosedPipe
}
func TestSnapshotTimestampReadError(t *testing.T) {
	ss := &segmentSnapshot{segment: &segmentWrapper{
		Segment: failingTimestampSegment{}, timeErr: io.ErrClosedPipe,
	}}
	var b bytes.Buffer
	if _, err := recordSegment(&b, ss, 1, "ice", 1); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("missing cached read error: %v", err)
	}
}

func TestSnapshotNativeTimestamp(t *testing.T) {
	for _, bounds := range [][2]int64{{0, 0}, {10, 20}, {-20, -10}} {
		ss := &segmentSnapshot{segment: &segmentWrapper{Segment: failingTimestampSegment{
			min: bounds[0], max: bounds[1],
		}}}
		if min, max := ss.Timestamp(); min != bounds[0] || max != bounds[1] {
			t.Fatalf("timestamp = (%d, %d), want %v", min, max, bounds)
		}
		var b bytes.Buffer
		if _, err := recordSegment(&b, ss, 1, "ice", 1); err != nil {
			t.Fatalf("native timestamp must not read stored fields: %v", err)
		}
	}
}
