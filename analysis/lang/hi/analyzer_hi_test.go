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

package hi

import (
	"reflect"
	"testing"

	"github.com/vcaesar/riot/analysis"
)

func TestHindiAnalyzer(t *testing.T) {
	tests := []struct {
		input  []byte
		output analysis.TokenStream
	}{
		// two ways to write 'hindi' itself
		{
			input: []byte("हिन्दी"),
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("हिंद"),
					PositionIncr: 1,
					Start:        0,
					End:          18,
				},
			},
		},
		{
			input: []byte("हिंदी"),
			output: analysis.TokenStream{
				&analysis.Token{
					Term:         []byte("हिंद"),
					PositionIncr: 1,
					Start:        0,
					End:          15,
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
