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

package token

import (
	"reflect"
	"testing"

	"github.com/vcaesar/riot/analysis"
)

func TestUniqueTermFilter(t *testing.T) {
	var tests = []struct {
		input  analysis.TokenStream
		output analysis.TokenStream
	}{
		{
			input:  tokenStream(),
			output: tokenStream(),
		},
		{
			input:  tokenStream("a"),
			output: tokenStream("a"),
		},
		{
			input:  tokenStream("each", "term", "in", "this", "sentence", "is", "unique"),
			output: tokenStream("each", "term", "in", "this", "sentence", "is", "unique"),
		},
		{
			input: tokenStream("Lui", "è", "alto", "e", "lei", "è", "bassa"),
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("Lui"),
					PositionIncr: 1,
					Start:        0,
					End:          3,
				},
				&analysis.Token{
					Term:         []byte("è"),
					PositionIncr: 1,
					Start:        3,
					End:          5,
				},
				&analysis.Token{
					Term:         []byte("alto"),
					PositionIncr: 1,
					Start:        5,
					End:          9,
				},
				&analysis.Token{
					Term:         []byte("e"),
					PositionIncr: 1,
					Start:        9,
					End:          10,
				},
				&analysis.Token{
					Term:         []byte("lei"),
					PositionIncr: 1,
					Start:        10,
					End:          13,
				},
				&analysis.Token{
					Term:         []byte("bassa"),
					PositionIncr: 2,
					Start:        15,
					End:          20,
				},
			},
		},
		{
			input: tokenStream("a", "a", "A", "a", "a", "A"),
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("a"),
					PositionIncr: 1,
					Start:        0,
					End:          1,
				},
				&analysis.Token{
					Term:         []byte("A"),
					PositionIncr: 2,
					Start:        2,
					End:          3,
				},
			},
		},
	}
	uniqueTermFilter := NewUniqueTermFilter()
	for _, test := range tests {
		actual := uniqueTermFilter.Filter(test.input)
		if !reflect.DeepEqual(actual, test.output) {
			t.Errorf("expected %s \n\n got %s", actual, test.output)
		}
	}
}

func tokenStream(termStrs ...string) analysis.TokenStream {
	tokenStream := make([]*analysis.Token, len(termStrs))
	index := 0
	for i, termStr := range termStrs {
		tokenStream[i] = &analysis.Token{
			Term:         []byte(termStr),
			PositionIncr: 1,
			Start:        index,
			End:          index + len(termStr),
		}
		index += len(termStr)
	}
	return tokenStream
}
