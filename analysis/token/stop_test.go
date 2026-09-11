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

func TestStopWordsFilter(t *testing.T) {
	inputTokenStream := analysis.TokenStream{
		&analysis.Token{
			Term:         []byte("a"),
			PositionIncr: 1,
		},
		&analysis.Token{
			Term:         []byte("walk"),
			PositionIncr: 1,
		},
		&analysis.Token{
			Term:         []byte("in"),
			PositionIncr: 1,
		},
		&analysis.Token{
			Term:         []byte("the"),
			PositionIncr: 1,
		},
		&analysis.Token{
			Term:         []byte("park"),
			PositionIncr: 1,
		},
	}

	expectedTokenStream := analysis.TokenStream{
		&analysis.Token{
			Term:         []byte("walk"),
			PositionIncr: 2,
		},
		&analysis.Token{
			Term:         []byte("park"),
			PositionIncr: 3,
		},
	}

	tokenMap := analysis.NewTokenMap()
	words := []string{"a", "in", "the"}
	for _, word := range words {
		tokenMap.AddToken(word)
	}
	stopFilter := NewStopTokensFilter(tokenMap)

	outputTokenStream := stopFilter.Filter(inputTokenStream)
	if !reflect.DeepEqual(outputTokenStream, expectedTokenStream) {
		t.Errorf("expected %#v got %#v", expectedTokenStream, outputTokenStream)
	}
}

func BenchmarkStopWordsFilter(b *testing.B) {
	inputTokenStream := analysis.TokenStream{
		&analysis.Token{
			Term: []byte("a"),
		},
		&analysis.Token{
			Term: []byte("walk"),
		},
		&analysis.Token{
			Term: []byte("in"),
		},
		&analysis.Token{
			Term: []byte("the"),
		},
		&analysis.Token{
			Term: []byte("park"),
		},
	}

	tokenMap := analysis.NewTokenMap()
	words := []string{"a", "in", "the"}
	for _, word := range words {
		tokenMap.AddToken(word)
	}
	stopFilter := NewStopTokensFilter(tokenMap)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		stopFilter.Filter(inputTokenStream)
	}
}
