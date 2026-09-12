//  Copyright (c) 2020 The Bluge Authors.
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

package search

import (
	"bytes"
	"strings"

	"github.com/vcaesar/riot/numeric"
)

type SortOrder []*Sort

func (o SortOrder) Fields() (fields []string) {
	for _, sort := range o {
		fields = append(fields, sort.Fields()...)
	}
	return fields
}

func (o SortOrder) Copy() SortOrder {
	rv := make(SortOrder, len(o))
	copy(rv, o)
	return rv
}

func (o SortOrder) Reverse() {
	for _, oi := range o {
		oi.desc = !oi.desc
		oi.missingFirst = !oi.missingFirst
	}
}

// Compute fills match.SortValue, reusing the byte buffers a pooled
// DocumentMatch already owns from a previous use so steady-state is
// allocation free. Score sorts are compared on DocumentMatch.Score, so
// their key is left empty until Complete encodes it for the final hits.
func (o SortOrder) Compute(match *DocumentMatch) {
	if o.scoreOnly() {
		return
	}
	for i, sort := range o {
		buf := sortSlot(match, i)
		if !sort.score {
			buf = sort.appendValue(match, buf)
		}
		match.SortValue = append(match.SortValue[:i], buf)
	}
}

// Complete encodes the score sort keys Compute deferred, so a returned
// hit's SortValue is a valid search-after key.
func (o SortOrder) Complete(match *DocumentMatch) {
	if len(match.SortValue) < len(o) { // Compute skipped a score-only order
		for i := range o {
			match.SortValue = append(match.SortValue[:i], sortSlot(match, i))
		}
	}
	for i, sort := range o {
		if sort.score {
			match.SortValue[i] = sort.appendValue(match, match.SortValue[i][:0])
		}
	}
}

func (o SortOrder) scoreOnly() bool {
	for _, sort := range o {
		if !sort.score {
			return false
		}
	}
	return true
}

// sortSlot returns the byte buffer a pooled match still holds for sort i
// beyond its current length, or nil.
func sortSlot(match *DocumentMatch, i int) []byte {
	if i < cap(match.SortValue) {
		return match.SortValue[:i+1][i][:0]
	}
	return nil
}

// DecodeScore restores match.Score from the prefix-coded key of the
// first score sort, so a DocumentMatch built from a search-after key
// compares like a live hit. Keys that fail to decode leave Score at 0.
func (o SortOrder) DecodeScore(match *DocumentMatch) {
	for i, sort := range o {
		if !sort.score || i >= len(match.SortValue) {
			continue
		}
		if v, err := numeric.PrefixCoded(match.SortValue[i]).Int64(); err == nil {
			match.Score = numeric.Int64ToFloat64(v)
		}
		return
	}
}

func (o SortOrder) Compare(i, j *DocumentMatch) int {
	// compare the documents on all search sorts until a differences is found
	for x, sort := range o {
		var c int
		if sort.score {
			// NaN scores compare equal and fall through to the hit number
			if i.Score > j.Score {
				c = 1
			} else if i.Score < j.Score {
				c = -1
			}
		} else {
			c = bytes.Compare(i.SortValue[x], j.SortValue[x])
		}
		if c == 0 {
			continue
		}
		if sort.desc {
			c = -c
		}
		return c
	}
	// if they are the same at this point, impose order based on index natural sort order
	if i.HitNumber == j.HitNumber {
		return 0
	} else if i.HitNumber > j.HitNumber {
		return 1
	}
	return -1
}

type SortValue [][]byte

type Sort struct {
	source       TextValueSource
	desc         bool
	missingFirst bool
	// score: source is the document score, compared as a float64 instead
	// of through its prefix-coded key
	score bool
}

func SortBy(source TextValueSource) *Sort {
	rv := &Sort{}
	_, rv.score = source.(*ScoreSource)

	rv.source = MissingTextValue(source, &sortFirstLast{
		desc:  &rv.desc,
		first: &rv.missingFirst,
	})

	return rv
}

func (s *Sort) Desc() *Sort {
	s.desc = true
	return s
}

func (s *Sort) MissingFirst() *Sort {
	s.missingFirst = true
	return s
}

func (s *Sort) Fields() []string {
	return s.source.Fields()
}

func (s *Sort) Value(match *DocumentMatch) []byte {
	return s.source.Value(match)
}

func (s *Sort) appendValue(match *DocumentMatch, buf []byte) []byte {
	if a, ok := s.source.(TextValueAppender); ok {
		return a.AppendValue(match, buf)
	}
	return append(buf, s.source.Value(match)...)
}

func ParseSearchSortString(input string) *Sort {
	descending := false
	if strings.HasPrefix(input, "-") {
		descending = true
		input = input[1:]
	}
	input = strings.TrimPrefix(input, "+")
	if input == "_score" {
		return SortBy(&ScoreSource{}).Desc()
	}
	rv := SortBy(Field(input))
	if descending {
		rv.Desc()
	}
	return rv
}

func ParseSortOrderStrings(in []string) SortOrder {
	rv := make(SortOrder, 0, len(in))
	for _, i := range in {
		ss := ParseSearchSortString(i)
		rv = append(rv, ss)
	}
	return rv
}

var highTerm = bytes.Repeat([]byte{0xff}, 10)
var lowTerm = []byte{0x00}

type sortFirstLast struct {
	desc  *bool
	first *bool
}

func (c *sortFirstLast) Fields() []string {
	return nil
}

func (c *sortFirstLast) Value(_ *DocumentMatch) []byte {
	if c.desc != nil && *c.desc && c.first != nil && *c.first {
		return highTerm
	} else if c.desc != nil && *c.desc {
		return lowTerm
	} else if c.first != nil && *c.first {
		return lowTerm
	}
	return highTerm
}
