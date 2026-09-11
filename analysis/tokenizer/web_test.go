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

	"github.com/vcaesar/riot/analysis"
)

func TestWeb(t *testing.T) {
	tests := []struct {
		input  []byte
		output analysis.TokenStream
	}{
		{
			[]byte("Hello info@blugelabs.com"),
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
					End:          24,
					Term:         []byte("info@blugelabs.com"),
					PositionIncr: 1,
					Type:         analysis.AlphaNumeric,
				},
			},
		},
		{
			[]byte("That http://blugelabs.com"),
			analysis.TokenStream{
				{
					Start:        0,
					End:          4,
					Term:         []byte("That"),
					PositionIncr: 1,
					Type:         analysis.AlphaNumeric,
				},
				{
					Start:        5,
					End:          25,
					Term:         []byte("http://blugelabs.com"),
					PositionIncr: 1,
					Type:         analysis.AlphaNumeric,
				},
			},
		},
		{
			[]byte("Hey @blugelabs"),
			analysis.TokenStream{
				{
					Start:        0,
					End:          3,
					Term:         []byte("Hey"),
					PositionIncr: 1,
					Type:         analysis.AlphaNumeric,
				},
				{
					Start:        4,
					End:          14,
					Term:         []byte("@blugelabs"),
					PositionIncr: 1,
					Type:         analysis.AlphaNumeric,
				},
			},
		},
		{
			[]byte("This #bluge"),
			analysis.TokenStream{
				{
					Start:        0,
					End:          4,
					Term:         []byte("This"),
					PositionIncr: 1,
					Type:         analysis.AlphaNumeric,
				},
				{
					Start:        5,
					End:          11,
					Term:         []byte("#bluge"),
					PositionIncr: 1,
					Type:         analysis.AlphaNumeric,
				},
			},
		},
		{
			[]byte("What about @blugelabs?"),
			analysis.TokenStream{
				{
					Start:        0,
					End:          4,
					Term:         []byte("What"),
					PositionIncr: 1,
					Type:         analysis.AlphaNumeric,
				},
				{
					Start:        5,
					End:          10,
					Term:         []byte("about"),
					PositionIncr: 1,
					Type:         analysis.AlphaNumeric,
				},
				{
					Start:        11,
					End:          21,
					Term:         []byte("@blugelabs"),
					PositionIncr: 1,
					Type:         analysis.AlphaNumeric,
				},
			},
		},
	}

	tokenizer := NewWebTokenizer()
	for _, test := range tests {
		actual := tokenizer.Tokenize(test.input)
		if !reflect.DeepEqual(actual, test.output) {
			t.Errorf("Expected %v, got %v for %s", test.output, actual, string(test.input))
		}
	}
}
