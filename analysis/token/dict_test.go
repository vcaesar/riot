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

func TestDictionaryCompoundFilter(t *testing.T) {
	inputTokenStream := analysis.TokenStream{
		&analysis.Token{
			Term:         []byte("i"),
			Start:        0,
			End:          1,
			PositionIncr: 1,
		},
		&analysis.Token{
			Term:         []byte("like"),
			Start:        2,
			End:          6,
			PositionIncr: 1,
		},
		&analysis.Token{
			Term:         []byte("to"),
			Start:        7,
			End:          9,
			PositionIncr: 1,
		},
		&analysis.Token{
			Term:         []byte("play"),
			Start:        10,
			End:          14,
			PositionIncr: 1,
		},
		&analysis.Token{
			Term:         []byte("softball"),
			Start:        15,
			End:          23,
			PositionIncr: 1,
		},
	}

	expectedTokenStream := analysis.TokenStream{
		&analysis.Token{
			Term:         []byte("i"),
			Start:        0,
			End:          1,
			PositionIncr: 1,
		},
		&analysis.Token{
			Term:         []byte("like"),
			Start:        2,
			End:          6,
			PositionIncr: 1,
		},
		&analysis.Token{
			Term:         []byte("to"),
			Start:        7,
			End:          9,
			PositionIncr: 1,
		},
		&analysis.Token{
			Term:         []byte("play"),
			Start:        10,
			End:          14,
			PositionIncr: 1,
		},
		&analysis.Token{
			Term:         []byte("softball"),
			Start:        15,
			End:          23,
			PositionIncr: 1,
		},
		&analysis.Token{
			Term:         []byte("soft"),
			Start:        15,
			End:          19,
			PositionIncr: 0,
		},
		&analysis.Token{
			Term:         []byte("ball"),
			Start:        19,
			End:          23,
			PositionIncr: 0,
		},
	}

	tokenMap := analysis.NewTokenMap()
	words := []string{"factor", "soft", "ball", "team"}
	for _, word := range words {
		tokenMap.AddToken(word)
	}
	dictFilter := NewDictionaryCompoundFilter(tokenMap, 5, 2, 15, false)

	outputTokenStream := dictFilter.Filter(inputTokenStream)
	if !reflect.DeepEqual(outputTokenStream, expectedTokenStream) {
		t.Errorf("expected %#v got %#v", expectedTokenStream, outputTokenStream)
	}
}

func TestStopWordsFilterLongestMatch(t *testing.T) {
	inputTokenStream := analysis.TokenStream{
		&analysis.Token{
			Term:         []byte("softestball"),
			Start:        0,
			End:          11,
			PositionIncr: 1,
		},
	}

	expectedTokenStream := analysis.TokenStream{
		&analysis.Token{
			Term:         []byte("softestball"),
			Start:        0,
			End:          11,
			PositionIncr: 1,
		},
		&analysis.Token{
			Term:         []byte("softest"),
			Start:        0,
			End:          7,
			PositionIncr: 0,
		},
		&analysis.Token{
			Term:         []byte("ball"),
			Start:        7,
			End:          11,
			PositionIncr: 0,
		},
	}

	tokenMap := analysis.NewTokenMap()
	words := []string{"soft", "softest", "ball"}
	for _, word := range words {
		tokenMap.AddToken(word)
	}
	dictFilter := NewDictionaryCompoundFilter(tokenMap, 5, 2, 15, true)

	outputTokenStream := dictFilter.Filter(inputTokenStream)
	if !reflect.DeepEqual(outputTokenStream, expectedTokenStream) {
		t.Errorf("expected %#v got %#v", expectedTokenStream, outputTokenStream)
	}
}
