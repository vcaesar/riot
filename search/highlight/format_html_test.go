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

package highlight

import (
	"testing"

	"github.com/vcaesar/riot/search"
)

func TestHTMLFragmentFormatter(t *testing.T) {
	tests := []struct {
		name      string
		fragment  *Fragment
		tlm       search.TermLocationMap
		beforeTag string
		afterTag  string
		output    string
	}{
		{
			name: "fragment bold",
			fragment: &Fragment{
				Orig:  []byte("the quick brown fox"),
				Start: 0,
				End:   19,
			},
			tlm: search.TermLocationMap{
				"quick": []*search.Location{
					{
						Pos:   2,
						Start: 4,
						End:   9,
					},
				},
			},
			beforeTag: "<b>",
			afterTag:  "</b>",
			output:    "the <b>quick</b> brown fox",
		},
		{
			name: "fragment emphasis",
			fragment: &Fragment{
				Orig:  []byte("the quick brown fox"),
				Start: 0,
				End:   19,
			},
			tlm: search.TermLocationMap{
				"quick": []*search.Location{
					{
						Pos:   2,
						Start: 4,
						End:   9,
					},
				},
			},
			beforeTag: "<em>",
			afterTag:  "</em>",
			output:    "the <em>quick</em> brown fox",
		},
		// test html escaping
		{
			fragment: &Fragment{
				Orig:  []byte("<the> quick brown & fox"),
				Start: 0,
				End:   23,
			},
			tlm: search.TermLocationMap{
				"quick": []*search.Location{
					{
						Pos:   2,
						Start: 6,
						End:   11,
					},
				},
			},
			output:    "&lt;the&gt; <em>quick</em> brown &amp; fox",
			beforeTag: "<em>",
			afterTag:  "</em>",
		},
		// test html escaping inside search term
		{
			fragment: &Fragment{
				Orig:  []byte("<the> qu&ick brown & fox"),
				Start: 0,
				End:   24,
			},
			tlm: search.TermLocationMap{
				"qu&ick": []*search.Location{
					{
						Pos:   2,
						Start: 6,
						End:   12,
					},
				},
			},
			output:    "&lt;the&gt; <em>qu&amp;ick</em> brown &amp; fox",
			beforeTag: "<em>",
			afterTag:  "</em>",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			emHTMLFormatter := NewHTMLFragmentFormatterTags(test.beforeTag, test.afterTag)
			otl := OrderTermLocations(test.tlm)
			result := emHTMLFormatter.Format(test.fragment, otl)
			if result != test.output {
				t.Errorf("expected `%s`, got `%s`", test.output, result)
			}
		})
	}
}
