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

package cjk

import (
	"reflect"
	"testing"

	"github.com/vcaesar/riot/analysis"
)

func TestCJKBigramFilter(t *testing.T) {
	tests := []struct {
		outputUnigram bool
		input         analysis.TokenStream
		output        analysis.TokenStream
	}{
		// first test that non-adjacent terms are not combined
		{
			outputUnigram: false,
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("こ"),
					Type:         analysis.Ideographic,
					PositionIncr: 1,
					Start:        0,
					End:          3,
				},
				&analysis.Token{
					Term:         []byte("ん"),
					Type:         analysis.Ideographic,
					PositionIncr: 1,
					Start:        5,
					End:          8,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("こ"),
					Type:         analysis.Single,
					PositionIncr: 1,
					Start:        0,
					End:          3,
				},
				&analysis.Token{
					Term:         []byte("ん"),
					Type:         analysis.Single,
					PositionIncr: 1,
					Start:        5,
					End:          8,
				},
			},
		},
		{
			outputUnigram: false,
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("こ"),
					Type:         analysis.Ideographic,
					PositionIncr: 1,
					Start:        0,
					End:          3,
				},
				&analysis.Token{
					Term:         []byte("ん"),
					Type:         analysis.Ideographic,
					PositionIncr: 1,
					Start:        3,
					End:          6,
				},
				&analysis.Token{
					Term:         []byte("に"),
					Type:         analysis.Ideographic,
					PositionIncr: 1,
					Start:        6,
					End:          9,
				},
				&analysis.Token{
					Term:         []byte("ち"),
					Type:         analysis.Ideographic,
					PositionIncr: 1,
					Start:        9,
					End:          12,
				},
				&analysis.Token{
					Term:         []byte("は"),
					Type:         analysis.Ideographic,
					PositionIncr: 1,
					Start:        12,
					End:          15,
				},
				&analysis.Token{
					Term:         []byte("世"),
					Type:         analysis.Ideographic,
					PositionIncr: 1,
					Start:        15,
					End:          18,
				},
				&analysis.Token{
					Term:         []byte("界"),
					Type:         analysis.Ideographic,
					PositionIncr: 1,
					Start:        18,
					End:          21,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("こん"),
					Type:         analysis.Double,
					PositionIncr: 1,
					Start:        0,
					End:          6,
				},
				&analysis.Token{
					Term:         []byte("んに"),
					Type:         analysis.Double,
					PositionIncr: 1,
					Start:        3,
					End:          9,
				},
				&analysis.Token{
					Term:         []byte("にち"),
					Type:         analysis.Double,
					PositionIncr: 1,
					Start:        6,
					End:          12,
				},
				&analysis.Token{
					Term:         []byte("ちは"),
					Type:         analysis.Double,
					PositionIncr: 1,
					Start:        9,
					End:          15,
				},
				&analysis.Token{
					Term:         []byte("は世"),
					Type:         analysis.Double,
					PositionIncr: 1,
					Start:        12,
					End:          18,
				},
				&analysis.Token{
					Term:         []byte("世界"),
					Type:         analysis.Double,
					PositionIncr: 1,
					Start:        15,
					End:          21,
				},
			},
		},
		{
			outputUnigram: true,
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("こ"),
					Type:         analysis.Ideographic,
					PositionIncr: 1,
					Start:        0,
					End:          3,
				},
				&analysis.Token{
					Term:         []byte("ん"),
					Type:         analysis.Ideographic,
					PositionIncr: 1,
					Start:        3,
					End:          6,
				},
				&analysis.Token{
					Term:         []byte("に"),
					Type:         analysis.Ideographic,
					PositionIncr: 1,
					Start:        6,
					End:          9,
				},
				&analysis.Token{
					Term:         []byte("ち"),
					Type:         analysis.Ideographic,
					PositionIncr: 1,
					Start:        9,
					End:          12,
				},
				&analysis.Token{
					Term:         []byte("は"),
					Type:         analysis.Ideographic,
					PositionIncr: 1,
					Start:        12,
					End:          15,
				},
				&analysis.Token{
					Term:         []byte("世"),
					Type:         analysis.Ideographic,
					PositionIncr: 1,
					Start:        15,
					End:          18,
				},
				&analysis.Token{
					Term:         []byte("界"),
					Type:         analysis.Ideographic,
					PositionIncr: 1,
					Start:        18,
					End:          21,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("こ"),
					Type:         analysis.Single,
					PositionIncr: 1,
					Start:        0,
					End:          3,
				},
				&analysis.Token{
					Term:         []byte("こん"),
					Type:         analysis.Double,
					PositionIncr: 0,
					Start:        0,
					End:          6,
				},
				&analysis.Token{
					Term:         []byte("ん"),
					Type:         analysis.Single,
					PositionIncr: 1,
					Start:        3,
					End:          6,
				},
				&analysis.Token{
					Term:         []byte("んに"),
					Type:         analysis.Double,
					PositionIncr: 0,
					Start:        3,
					End:          9,
				},
				&analysis.Token{
					Term:         []byte("に"),
					Type:         analysis.Single,
					PositionIncr: 1,
					Start:        6,
					End:          9,
				},
				&analysis.Token{
					Term:         []byte("にち"),
					Type:         analysis.Double,
					PositionIncr: 0,
					Start:        6,
					End:          12,
				},
				&analysis.Token{
					Term:         []byte("ち"),
					Type:         analysis.Single,
					PositionIncr: 1,
					Start:        9,
					End:          12,
				},
				&analysis.Token{
					Term:         []byte("ちは"),
					Type:         analysis.Double,
					PositionIncr: 0,
					Start:        9,
					End:          15,
				},
				&analysis.Token{
					Term:         []byte("は"),
					Type:         analysis.Single,
					PositionIncr: 1,
					Start:        12,
					End:          15,
				},
				&analysis.Token{
					Term:         []byte("は世"),
					Type:         analysis.Double,
					PositionIncr: 0,
					Start:        12,
					End:          18,
				},
				&analysis.Token{
					Term:         []byte("世"),
					Type:         analysis.Single,
					PositionIncr: 1,
					Start:        15,
					End:          18,
				},
				&analysis.Token{
					Term:         []byte("世界"),
					Type:         analysis.Double,
					PositionIncr: 0,
					Start:        15,
					End:          21,
				},
				&analysis.Token{
					Term:         []byte("界"),
					Type:         analysis.Single,
					PositionIncr: 1,
					Start:        18,
					End:          21,
				},
			},
		},
		{
			outputUnigram: false,
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("こ"),
					Type:         analysis.Ideographic,
					PositionIncr: 1,
					Start:        0,
					End:          3,
				},
				&analysis.Token{
					Term:         []byte("ん"),
					Type:         analysis.Ideographic,
					PositionIncr: 1,
					Start:        3,
					End:          6,
				},
				&analysis.Token{
					Term:         []byte("に"),
					Type:         analysis.Ideographic,
					PositionIncr: 1,
					Start:        6,
					End:          9,
				},
				&analysis.Token{
					Term:         []byte("ち"),
					Type:         analysis.Ideographic,
					PositionIncr: 1,
					Start:        9,
					End:          12,
				},
				&analysis.Token{
					Term:         []byte("は"),
					Type:         analysis.Ideographic,
					PositionIncr: 1,
					Start:        12,
					End:          15,
				},
				&analysis.Token{
					Term:         []byte("cat"),
					Type:         analysis.AlphaNumeric,
					PositionIncr: 1,
					Start:        12,
					End:          15,
				},
				&analysis.Token{
					Term:         []byte("世"),
					Type:         analysis.Ideographic,
					PositionIncr: 1,
					Start:        18,
					End:          21,
				},
				&analysis.Token{
					Term:         []byte("界"),
					Type:         analysis.Ideographic,
					PositionIncr: 1,
					Start:        21,
					End:          24,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("こん"),
					Type:         analysis.Double,
					PositionIncr: 1,
					Start:        0,
					End:          6,
				},
				&analysis.Token{
					Term:         []byte("んに"),
					Type:         analysis.Double,
					PositionIncr: 1,
					Start:        3,
					End:          9,
				},
				&analysis.Token{
					Term:         []byte("にち"),
					Type:         analysis.Double,
					PositionIncr: 1,
					Start:        6,
					End:          12,
				},
				&analysis.Token{
					Term:         []byte("ちは"),
					Type:         analysis.Double,
					PositionIncr: 1,
					Start:        9,
					End:          15,
				},
				&analysis.Token{
					Term:         []byte("cat"),
					Type:         analysis.AlphaNumeric,
					PositionIncr: 1,
					Start:        12,
					End:          15,
				},
				&analysis.Token{
					Term:         []byte("世界"),
					Type:         analysis.Double,
					PositionIncr: 1,
					Start:        18,
					End:          24,
				},
			},
		},
		{
			outputUnigram: false,
			input: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("パイプライン"),
					Type:         analysis.Ideographic,
					PositionIncr: 1,
					Start:        0,
					End:          18,
				},
			},
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("パイ"),
					Type:         analysis.Double,
					PositionIncr: 1,
					Start:        0,
					End:          6,
				},
				&analysis.Token{
					Term:         []byte("イプ"),
					Type:         analysis.Double,
					PositionIncr: 1,
					Start:        3,
					End:          9,
				},
				&analysis.Token{
					Term:         []byte("プラ"),
					Type:         analysis.Double,
					PositionIncr: 1,
					Start:        6,
					End:          12,
				},
				&analysis.Token{
					Term:         []byte("ライ"),
					Type:         analysis.Double,
					PositionIncr: 1,
					Start:        9,
					End:          15,
				},
				&analysis.Token{
					Term:         []byte("イン"),
					Type:         analysis.Double,
					PositionIncr: 1,
					Start:        12,
					End:          18,
				},
			},
		},
	}

	for _, test := range tests {
		cjkBigramFilter := NewBigramFilter(test.outputUnigram)
		actual := cjkBigramFilter.Filter(test.input)
		if !reflect.DeepEqual(actual, test.output) {
			t.Errorf("expected %s, got %s", test.output, actual)
		}
	}
}
