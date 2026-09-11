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

package tokenizer

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/vcaesar/riot/analysis"
)

func TestExceptionsTokenizer(t *testing.T) {
	tests := []struct {
		input    []byte
		patterns []string
		result   analysis.TokenStream
	}{
		{
			input: []byte("test http://blugelabs.com/ words"),
			patterns: []string{
				`[hH][tT][tT][pP][sS]?://(\S)*`,
				`[fF][iI][lL][eE]://(\S)*`,
				`[fF][tT][pP]://(\S)*`,
			},
			result: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("test"),
					PositionIncr: 1,
					Start:        0,
					End:          4,
				},
				&analysis.Token{
					Term:         []byte("http://blugelabs.com/"),
					PositionIncr: 1,
					Start:        5,
					End:          26,
				},
				&analysis.Token{
					Term:         []byte("words"),
					PositionIncr: 1,
					Start:        27,
					End:          32,
				},
			},
		},
		{
			input: []byte("what ftp://blugelabs.com/ songs"),
			patterns: []string{
				`[hH][tT][tT][pP][sS]?://(\S)*`,
				`[fF][iI][lL][eE]://(\S)*`,
				`[fF][tT][pP]://(\S)*`,
			},
			result: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("what"),
					PositionIncr: 1,
					Start:        0,
					End:          4,
				},
				&analysis.Token{
					Term:         []byte("ftp://blugelabs.com/"),
					PositionIncr: 1,
					Start:        5,
					End:          25,
				},
				&analysis.Token{
					Term:         []byte("songs"),
					PositionIncr: 1,
					Start:        26,
					End:          31,
				},
			},
		},
		{
			input: []byte("please email marty@couchbase.com the URL https://blugelabs.com/"),
			patterns: []string{
				`[hH][tT][tT][pP][sS]?://(\S)*`,
				`[fF][iI][lL][eE]://(\S)*`,
				`[fF][tT][pP]://(\S)*`,
				`\S+@\S+`,
			},
			result: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("please"),
					PositionIncr: 1,
					Start:        0,
					End:          6,
				},
				&analysis.Token{
					Term:         []byte("email"),
					PositionIncr: 1,
					Start:        7,
					End:          12,
				},
				&analysis.Token{
					Term:         []byte("marty@couchbase.com"),
					PositionIncr: 1,
					Start:        13,
					End:          32,
				},
				&analysis.Token{
					Term:         []byte("the"),
					PositionIncr: 1,
					Start:        33,
					End:          36,
				},
				&analysis.Token{
					Term:         []byte("URL"),
					PositionIncr: 1,
					Start:        37,
					End:          40,
				},
				&analysis.Token{
					Term:         []byte("https://blugelabs.com/"),
					PositionIncr: 1,
					Start:        41,
					End:          63,
				},
			},
		},
	}

	remaining := NewUnicodeTokenizer()
	for _, test := range tests {
		pattern := strings.Join(test.patterns, "|")
		r, err := regexp.Compile(pattern)
		if err != nil {
			t.Fatal(err)
		}

		// build the requested exception tokenizer
		tokenizer := NewExceptionsTokenizer(r, remaining)
		actual := tokenizer.Tokenize(test.input)
		if !reflect.DeepEqual(actual, test.result) {
			t.Errorf("expected %v, got %v", test.result, actual)
		}
	}
}
