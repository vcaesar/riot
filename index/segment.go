//  Copyright (c) 2020 Couchbase, Inc.
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
	"math"

	"github.com/RoaringBitmap/roaring/v2"
	segment "github.com/vcaesar/bluge_segment_api"
)

type SegmentSnapshot interface {
	ID() uint64
	Deleted() *roaring.Bitmap
	DocNum() uint64
	SegmentSize() uint64
	Timestamp() (int64, int64)
}

type segmentSnapshot struct {
	id             uint64
	segment        *segmentWrapper
	deleted        *roaring.Bitmap
	creator        string
	segmentType    string
	segmentVersion uint32
	segmentSize    uint64
	docNum         uint64
	docTimeMin     int64
	docTimeMax     int64
}

func (s *segmentSnapshot) DocNum() uint64 {
	if s.segment != nil {
		return s.segment.Count()
	}
	return s.docNum
}

func (s *segmentSnapshot) SegmentSize() uint64 {
	if s.segmentSize != 0 {
		return s.segmentSize
	}
	if s.segment != nil {
		if size := s.segment.Size(); size > 0 {
			return uint64(size)
		}
	}
	return 0
}

func (s *segmentSnapshot) Timestamp() (timeMin, timeMax int64) {
	if s.docTimeMin != 0 || s.docTimeMax != 0 {
		return s.docTimeMin, s.docTimeMax
	}
	if s.segment != nil {
		return s.segment.Timestamp()
	}
	return 0, 0
}

func (s *segmentSnapshot) Bytes() int64 {
	size := s.SegmentSize()
	if size > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(size)
}

func (s *segmentSnapshot) Segment() segment.Segment {
	return s.segment
}

func (s *segmentSnapshot) Deleted() *roaring.Bitmap {
	return s.deleted
}

func (s *segmentSnapshot) ID() uint64 {
	return s.id
}

func (s *segmentSnapshot) FullSize() int64 {
	count := s.segment.Count()
	if count > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(count)
}

func (s *segmentSnapshot) LiveSize() int64 {
	count := s.Count()
	if count > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(count)
}

func (s *segmentSnapshot) Close() error {
	return s.segment.Close()
}

func (s *segmentSnapshot) VisitDocument(num uint64, visitor segment.StoredFieldVisitor) error {
	return s.segment.VisitStoredFields(num, visitor)
}

func (s *segmentSnapshot) Count() uint64 {
	rv := s.segment.Count()
	if s.deleted != nil {
		rv -= s.deleted.GetCardinality()
	}
	return rv
}

// DocNumbersLive returns a bitmap containing doc numbers for all live docs
func (s *segmentSnapshot) DocNumbersLive() *roaring.Bitmap {
	rv := roaring.NewBitmap()
	rv.AddRange(0, s.segment.Count())
	if s.deleted != nil {
		rv.AndNot(s.deleted)
	}
	return rv
}

func (s *segmentSnapshot) Fields() []string {
	return s.segment.Fields()
}

func (s *segmentSnapshot) Size() (rv int) {
	rv = s.segment.Size()
	if s.deleted != nil {
		deletedSize := s.deleted.GetSizeInBytes()
		if deletedSize > uint64(math.MaxInt) {
			return math.MaxInt
		}
		if rv > math.MaxInt-int(deletedSize) {
			return math.MaxInt
		}
		rv += int(deletedSize)
	}
	return
}
