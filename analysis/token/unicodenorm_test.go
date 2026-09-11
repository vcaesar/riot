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

	"golang.org/x/text/unicode/norm"

	"github.com/vcaesar/riot/analysis"
)

// the following tests come from the lucene
// test cases for CJK width filter
// which is our basis for using this
// as a substitute for that
func TestUnicodeNormalization(t *testing.T) {
	tests := []struct {
		norm   norm.Form
		input  analysis.TokenStream
		output analysis.TokenStream
	}{
		{
			norm: norm.NFKD,
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("Ｔｅｓｔ"),
					PositionIncr: 1,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("Test"),
					PositionIncr: 1,
				},
			},
		},
		{
			norm: norm.NFKD,
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("１２３４"),
					PositionIncr: 1,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("1234"),
					PositionIncr: 1,
				},
			},
		},
		{
			norm: norm.NFKD,
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("ｶﾀｶﾅ"),
					PositionIncr: 1,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("カタカナ"),
					PositionIncr: 1,
				},
			},
		},
		{
			norm: norm.NFKC,
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("ｳﾞｨｯﾂ"),
					PositionIncr: 1,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("ヴィッツ"),
					PositionIncr: 1,
				},
			},
		},
		{
			norm: norm.NFKC,
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("ﾊﾟﾅｿﾆｯｸ"),
					PositionIncr: 1,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("パナソニック"),
					PositionIncr: 1,
				},
			},
		},
		{
			norm: norm.NFD,
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("\u212B"),
					PositionIncr: 1,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("\u0041\u030A"),
					PositionIncr: 1,
				},
			},
		},
		{
			norm: norm.NFC,
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("\u212B"),
					PositionIncr: 1,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("\u00C5"),
					PositionIncr: 1,
				},
			},
		},
		{
			norm: norm.NFKD,
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("\uFB01"),
					PositionIncr: 1,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("\u0066\u0069"),
					PositionIncr: 1,
				},
			},
		},
		{
			norm: norm.NFKC,
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("\uFB01"),
					PositionIncr: 1,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("\u0066\u0069"),
					PositionIncr: 1,
				},
			},
		},
	}

	for _, test := range tests {
		filter := NewUnicodeNormalizeFilter(test.norm)
		actual := filter.Filter(test.input)
		if !reflect.DeepEqual(actual, test.output) {
			t.Errorf("expected %s, got %s", test.output[0].Term, actual[0].Term)
			t.Errorf("expected %#v, got %#v", test.output[0].Term, actual[0].Term)
		}
	}
}
