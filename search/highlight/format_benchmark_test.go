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

package highlight

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vcaesar/riot/search"
)

var benchmarkFormattedFragment string

func BenchmarkFragmentFormatter(b *testing.B) {
	for _, text := range []struct{ name, chunk string }{
		{"Plain", "word text "},
		{"EscapedUnicode", "<&é> text "},
	} {
		for _, count := range []int{0, 1, 32, 256} {
			orig := []byte(strings.Repeat(text.chunk, max(count, 1)))
			fragment := &Fragment{Orig: orig, End: len(orig)}
			locations := make(TermLocations, count)
			for i := range locations {
				locations[i] = &TermLocation{Start: i * len(text.chunk), End: i*len(text.chunk) + len(text.chunk) - 6}
			}
			for _, formatter := range []struct {
				name      string
				formatter FragmentFormatter
			}{
				{"HTML", NewHTMLFragmentFormatter()},
				{"ANSI", NewANSIFragmentFormatter()},
			} {
				b.Run(fmt.Sprintf("%s/%s/%d", formatter.name, text.name, count), func(b *testing.B) {
					b.ReportAllocs()
					b.SetBytes(int64(len(orig)))
					for i := 0; i < b.N; i++ {
						benchmarkFormattedFragment = formatter.formatter.Format(fragment, locations)
					}
				})
			}
		}
	}
}

func BenchmarkBestFragmentDense(b *testing.B) {
	orig := []byte(strings.Repeat("word text ", 32))
	locations := make([]*search.Location, 32)
	for i := range locations {
		locations[i] = &search.Location{Pos: i + 1, Start: i * 10, End: i*10 + 4}
	}
	tlm := search.TermLocationMap{"word": locations}
	highlighter := NewSimpleHighlighter(NewSimpleFragmenterSized(len(orig)), NewHTMLFragmentFormatter(), DefaultSeparator)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkFormattedFragment = highlighter.BestFragment(tlm, orig)
	}
}
