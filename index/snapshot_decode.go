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
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"github.com/RoaringBitmap/roaring/v2"
)

type snapshotDecoder struct {
	r io.Reader
	n int64
}

func (d *snapshotDecoder) Read(p []byte) (int, error) {
	n, err := d.r.Read(p)
	d.n += int64(n)
	return n, err
}

func (d *snapshotDecoder) ReadByte() (byte, error) {
	var b [1]byte
	_, err := io.ReadFull(d, b[:])
	return b[0], err
}

func (d *snapshotDecoder) fixed(width int) (uint64, error) {
	var b [8]byte
	_, err := io.ReadFull(d, b[:width])
	if err != nil {
		return 0, err
	}
	if width == 4 {
		return uint64(binary.BigEndian.Uint32(b[:4])), nil
	}
	return binary.BigEndian.Uint64(b[:]), nil
}

func (d *snapshotDecoder) blob() ([]byte, error) {
	length, err := binary.ReadUvarint(d)
	if err != nil {
		return nil, err
	}
	if length > math.MaxInt64 {
		return nil, fmt.Errorf("invalid snapshot length: %d", length)
	}
	// Grow only as bytes arrive, never allocate an untrusted declared length.
	var b bytes.Buffer
	_, err = io.CopyN(&b, d, int64(length))
	if err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (d *snapshotDecoder) segment(version uint64, typ string) (*segmentSnapshot, error) {
	ver, err := d.fixed(4)
	if err != nil {
		return nil, err
	}
	id, err := binary.ReadUvarint(d)
	if err != nil {
		return nil, err
	}
	ss := &segmentSnapshot{id: id, segmentType: typ, segmentVersion: uint32(ver)}
	if version >= blugeSnapshotFormatVersion3 {
		ss.segmentSize, err = d.fixed(8)
		if err != nil {
			return nil, err
		}
		ss.docNum, err = d.fixed(8)
		if err != nil {
			return nil, err
		}
	}
	if version >= blugeSnapshotFormatVersion2 {
		min, err := d.fixed(8)
		if err != nil {
			return nil, err
		}
		max, err := d.fixed(8)
		if err != nil {
			return nil, err
		}
		ss.docTimeMin, ss.docTimeMax = int64(min), int64(max)
		if ss.docTimeMin > ss.docTimeMax {
			return nil, fmt.Errorf("invalid segment timestamp range")
		}
	}
	deleted, err := d.blob()
	if err != nil {
		return nil, err
	}
	if len(deleted) != 0 {
		bitmap := roaring.NewBitmap()
		n, err := bitmap.ReadFrom(bytes.NewReader(deleted))
		if err != nil {
			return nil, err
		}
		if n != int64(len(deleted)) {
			return nil, fmt.Errorf("trailing deleted bitmap data")
		}
		if !bitmap.IsEmpty() {
			ss.deleted = bitmap
		}
	}
	return ss, nil
}
