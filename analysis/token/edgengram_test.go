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

func TestEdgeNgramFilter(t *testing.T) {
	tests := []struct {
		side   Side
		min    int
		max    int
		input  analysis.TokenStream
		output analysis.TokenStream
	}{
		{
			side: FRONT,
			min:  1,
			max:  1,
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("abcde"),
					PositionIncr: 1,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("a"),
					PositionIncr: 1,
				},
			},
		},
		{
			side: BACK,
			min:  1,
			max:  1,
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("abcde"),
					PositionIncr: 1,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("e"),
					PositionIncr: 1,
				},
			},
		},
		{
			side: FRONT,
			min:  1,
			max:  3,
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("abcde"),
					PositionIncr: 1,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("a"),
					PositionIncr: 1,
				},
				&analysis.Token{
					Term:         []byte("ab"),
					PositionIncr: 0,
				},
				&analysis.Token{
					Term:         []byte("abc"),
					PositionIncr: 0,
				},
			},
		},
		{
			side: BACK,
			min:  1,
			max:  3,
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("abcde"),
					PositionIncr: 1,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("e"),
					PositionIncr: 1,
				},
				&analysis.Token{
					Term:         []byte("de"),
					PositionIncr: 0,
				},
				&analysis.Token{
					Term:         []byte("cde"),
					PositionIncr: 0,
				},
			},
		},
		{
			side: FRONT,
			min:  1,
			max:  3,
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("abcde"),
					PositionIncr: 1,
				},
				&analysis.Token{
					Term:         []byte("vwxyz"),
					PositionIncr: 1,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("a"),
					PositionIncr: 1,
				},
				&analysis.Token{
					Term:         []byte("ab"),
					PositionIncr: 0,
				},
				&analysis.Token{
					Term:         []byte("abc"),
					PositionIncr: 0,
				},
				&analysis.Token{
					Term:         []byte("v"),
					PositionIncr: 1,
				},
				&analysis.Token{
					Term:         []byte("vw"),
					PositionIncr: 0,
				},
				&analysis.Token{
					Term:         []byte("vwx"),
					PositionIncr: 0,
				},
			},
		},
		{
			side: BACK,
			min:  3,
			max:  5,
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("Beryl"),
					PositionIncr: 1,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("ryl"),
					PositionIncr: 1,
				},
				&analysis.Token{
					Term:         []byte("eryl"),
					PositionIncr: 0,
				},
				&analysis.Token{
					Term:         []byte("Beryl"),
					PositionIncr: 0,
				},
			},
		},
		{
			side: FRONT,
			min:  3,
			max:  5,
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("Beryl"),
					PositionIncr: 1,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("Ber"),
					PositionIncr: 1,
				},
				&analysis.Token{
					Term:         []byte("Bery"),
					PositionIncr: 0,
				},
				&analysis.Token{
					Term:         []byte("Beryl"),
					PositionIncr: 0,
				},
			},
		},
	}

	for _, test := range tests {
		edgeNgramFilter := NewEdgeNgramFilter(test.side, test.min, test.max)
		actual := edgeNgramFilter.Filter(test.input)
		if !reflect.DeepEqual(actual, test.output) {
			t.Errorf("expected %s, got %s", test.output, actual)
		}
	}
}
