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

package de

import (
	"reflect"
	"testing"

	"github.com/vcaesar/riot/analysis"
)

func TestGermanAnalyzer(t *testing.T) {
	tests := []struct {
		input  []byte
		output analysis.TokenStream
	}{
		{
			input: []byte("Tisch"),
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("tisch"),
					PositionIncr: 1,
					Start:        0,
					End:          5,
				},
			},
		},
		{
			input: []byte("Tische"),
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("tisch"),
					PositionIncr: 1,
					Start:        0,
					End:          6,
				},
			},
		},
		{
			input: []byte("Tischen"),
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("tisch"),
					PositionIncr: 1,
					Start:        0,
					End:          7,
				},
			},
		},
		// german specials
		{
			input: []byte("Schaltflächen"),
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("schaltflach"),
					PositionIncr: 1,
					Start:        0,
					End:          14,
				},
			},
		},
		{
			input: []byte("Schaltflaechen"),
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("schaltflach"),
					PositionIncr: 1,
					Start:        0,
					End:          14,
				},
			},
		},
		// tests added by marty to increase coverage
		{
			input: []byte("Blechern"),
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("blech"),
					PositionIncr: 1,
					Start:        0,
					End:          8,
				},
			},
		},
		{
			input: []byte("Klecks"),
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("kleck"),
					PositionIncr: 1,
					Start:        0,
					End:          6,
				},
			},
		},
		{
			input: []byte("Mindestens"),
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("mindest"),
					PositionIncr: 1,
					Start:        0,
					End:          10,
				},
			},
		},
		{
			input: []byte("Kugelfest"),
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("kugelf"),
					PositionIncr: 1,
					Start:        0,
					End:          9,
				},
			},
		},
		{
			input: []byte("Baldigst"),
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("baldig"),
					PositionIncr: 1,
					Start:        0,
					End:          8,
				},
			},
		},
	}

	analyzer := Analyzer()
	for _, test := range tests {
		actual := analyzer.Analyze(test.input)
		if !reflect.DeepEqual(actual, test.output) {
			t.Errorf("expected %v, got %v", test.output, actual)
		}
	}
}
