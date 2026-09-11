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
	"testing"
	"unicode"

	"github.com/vcaesar/riot/analysis"
)

func TestCharacterTokenizer(t *testing.T) {
	tests := []struct {
		input  []byte
		output analysis.TokenStream
	}{
		{
			[]byte("Hello World."),
			analysis.TokenStream{
				{
					Start:        0,
					End:          5,
					Term:         []byte("Hello"),
					PositionIncr: 1,
					Type:         analysis.AlphaNumeric,
				},
				{
					Start:        6,
					End:          11,
					Term:         []byte("World"),
					PositionIncr: 1,
					Type:         analysis.AlphaNumeric,
				},
			},
		},
		{
			[]byte("dominique@mcdiabetes.com"),
			analysis.TokenStream{
				{
					Start:        0,
					End:          9,
					Term:         []byte("dominique"),
					PositionIncr: 1,
					Type:         analysis.AlphaNumeric,
				},
				{
					Start:        10,
					End:          20,
					Term:         []byte("mcdiabetes"),
					PositionIncr: 1,
					Type:         analysis.AlphaNumeric,
				},
				{
					Start:        21,
					End:          24,
					Term:         []byte("com"),
					PositionIncr: 1,
					Type:         analysis.AlphaNumeric,
				},
			},
		},
	}

	tokenizer := NewCharacterTokenizer(unicode.IsLetter)
	for _, test := range tests {
		actual := tokenizer.Tokenize(test.input)
		if !reflect.DeepEqual(actual, test.output) {
			t.Errorf("Expected %v, got %v for %s", test.output, actual, string(test.input))
		}
	}
}
