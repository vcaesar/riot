// Copyright (c) 2026 The Riot Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package riot

import (
	"math"
	"strconv"
	"testing"

	"github.com/vcaesar/riot/search"
)

type memoryTestSearcher struct {
	search.Searcher
	size, pool int
}

func (s memoryTestSearcher) Size() int                  { return s.size }
func (s memoryTestSearcher) DocumentMatchPoolSize() int { return s.pool }

type memoryTestCollector struct {
	search.Collector
	size, backing int
}

func (c memoryTestCollector) Size() int        { return c.size }
func (c memoryTestCollector) BackingSize() int { return c.backing }

func TestMemNeededForSearch(t *testing.T) {
	for _, test := range []struct {
		name      string
		searcher  memoryTestSearcher
		collector memoryTestCollector
		want      string
	}{
		{
			name:      "ordinary",
			searcher:  memoryTestSearcher{size: 10, pool: 2},
			collector: memoryTestCollector{size: 20, backing: 3},
			want:      strconv.Itoa(30 + searchContextEmptySize + 11*documentMatchEmptySize),
		},
		{name: "negative searcher", searcher: memoryTestSearcher{size: -1}, want: "18446744073709551615"},
		{name: "negative collector", collector: memoryTestCollector{size: -1}, want: "18446744073709551615"},
		{name: "negative pool", searcher: memoryTestSearcher{pool: -1}, want: "18446744073709551615"},
		{name: "negative backing", collector: memoryTestCollector{backing: -1}, want: "18446744073709551615"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := memNeededForSearch(test.searcher, test.collector)
			if strconv.FormatUint(got, 10) != test.want {
				t.Errorf("estimate = %d, want %s", got, test.want)
			}
		})
	}
}

func TestMemNeededForSearchOverflow(t *testing.T) {
	if strconv.IntSize != 64 {
		t.Skip("uint64 overflow requires 64-bit int sizes")
	}
	maxInt, err := strconv.Atoi("9223372036854775807")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		searcher  memoryTestSearcher
		collector memoryTestCollector
	}{
		{name: "multiplication", searcher: memoryTestSearcher{pool: maxInt}},
		{name: "addition", searcher: memoryTestSearcher{size: maxInt}, collector: memoryTestCollector{size: maxInt}},
		{name: "match count sum", searcher: memoryTestSearcher{pool: maxInt}, collector: memoryTestCollector{backing: maxInt}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := memNeededForSearch(test.searcher, test.collector); got != math.MaxUint64 {
				t.Errorf("estimate = %d, want saturation", got)
			}
		})
	}
}
