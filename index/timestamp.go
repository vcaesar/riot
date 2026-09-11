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

import "encoding/binary"

// TimestampField is reserved for document time metadata, stored as a big-endian int64.
const TimestampField = "_bluge_timestamp"

// Timestamp returns inclusive segment bounds. (0, 0) means unknown.
func (s *segmentWrapper) Timestamp() (int64, int64) {
	s.timeOnce.Do(s.loadTimestamp)
	return s.timeMin, s.timeMax
}

func (s *segmentWrapper) loadTimestamp() {
	if timed, ok := s.Segment.(interface{ Timestamp() (int64, int64) }); ok {
		s.timeMin, s.timeMax = timed.Timestamp()
		return
	}
	var min, max int64
	for doc := uint64(0); doc < s.Count(); doc++ {
		var timestamp int64
		err := s.VisitStoredFields(doc, func(name string, value []byte) bool {
			if name == TimestampField && len(value) == 8 {
				timestamp = int64(binary.BigEndian.Uint64(value))
				return false
			}
			return true
		})
		if err != nil {
			s.timeErr = err
			return
		}
		// A single unknown document makes pruning this segment unsafe.
		if timestamp == 0 {
			return
		}
		if doc == 0 || timestamp < min {
			min = timestamp
		}
		if doc == 0 || timestamp > max {
			max = timestamp
		}
	}
	s.timeMin, s.timeMax = min, max
}

func (c Config) hasTimeRange() bool { return c.FilterTimeMin != 0 || c.FilterTimeMax != 0 }

func (c Config) excludes(s *segmentSnapshot) bool {
	min, max := s.Timestamp()
	if min == 0 && max == 0 {
		return false
	}
	return (c.FilterTimeMin != 0 && max < c.FilterTimeMin) ||
		(c.FilterTimeMax != 0 && min > c.FilterTimeMax)
}
