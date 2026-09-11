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

func TestLengthFilter(t *testing.T) {
	tests := []struct {
		name   string
		min    int
		max    int
		input  analysis.TokenStream
		output analysis.TokenStream
	}{
		{
			name: "min 3 max 4",
			min:  3,
			max:  4,
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("1"),
					PositionIncr: 1,
				},
				&analysis.Token{
					Term:         []byte("two"),
					PositionIncr: 1,
				},
				&analysis.Token{
					Term:         []byte("three"),
					PositionIncr: 1,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("two"),
					PositionIncr: 2,
				},
			},
		},
		{
			name: "min 3, no max",
			min:  3,
			max:  -1,
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("1"),
					PositionIncr: 1,
				},
				&analysis.Token{
					Term:         []byte("two"),
					PositionIncr: 1,
				},
				&analysis.Token{
					Term:         []byte("three"),
					PositionIncr: 1,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("two"),
					PositionIncr: 2,
				},
				&analysis.Token{
					Term:         []byte("three"),
					PositionIncr: 1,
				},
			},
		},
		{
			name: "no min, max 4",
			min:  -1,
			max:  4,
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("1"),
					PositionIncr: 1,
				},
				&analysis.Token{
					Term:         []byte("two"),
					PositionIncr: 1,
				},
				&analysis.Token{
					Term:         []byte("three"),
					PositionIncr: 1,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("1"),
					PositionIncr: 1,
				},
				&analysis.Token{
					Term:         []byte("two"),
					PositionIncr: 1,
				},
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			lengthFilter := NewLengthFilter(test.min, test.max)
			actual := lengthFilter.Filter(test.output)
			if !reflect.DeepEqual(actual, test.output) {
				t.Errorf("expected %s, got %s", test.output[0].Term, actual[0].Term)
			}
		})
	}
}
