//  Copyright (c) 2026 The AtomAI.
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
	"regexp"
	"testing"

	"github.com/vcaesar/riot/analysis"
)

func TestLetterTokenizer(t *testing.T) {
	out := NewLetterTokenizer().Tokenize([]byte("ab1cd"))
	if len(out) != 2 || string(out[0].Term) != "ab" || string(out[1].Term) != "cd" {
		t.Fatalf("unexpected output %v", out)
	}
}

func TestRegexpNumericType(t *testing.T) {
	out := NewRegexpTokenizer(regexp.MustCompile(`\S+`)).Tokenize([]byte("12.5 x"))
	if len(out) != 2 || out[0].Type != analysis.Numeric || out[1].Type != analysis.AlphaNumeric {
		t.Fatalf("unexpected output %v", out)
	}
}
