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

func TestApostropheFilter(t *testing.T) {
	tests := []struct {
		input  analysis.TokenStream
		output analysis.TokenStream
	}{
		{
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("Türkiye'de"),
					PositionIncr: 1,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("Türkiye"),
					PositionIncr: 1,
				},
			},
		},
		{
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("2003'te"),
					PositionIncr: 1,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("2003"),
					PositionIncr: 1,
				},
			},
		},
		{
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("Van"),
					PositionIncr: 1,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("Van"),
					PositionIncr: 1,
				},
			},
		},
		{
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("Gölü'nü"),
					PositionIncr: 1,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("Gölü"),
					PositionIncr: 1,
				},
			},
		},
		{
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("gördüm"),
					PositionIncr: 1,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("gördüm"),
					PositionIncr: 1,
				},
			},
		},
	}

	for _, test := range tests {
		apostropheFilter := NewApostropheFilter()
		actual := apostropheFilter.Filter(test.input)
		if !reflect.DeepEqual(actual, test.output) {
			t.Errorf("expected %s, got %s", test.output[0].Term, actual[0].Term)
		}
	}
}
