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

package cjk

import (
	"testing"

	"github.com/vcaesar/riot/analysis"
)

func TestBigramUnigramNoIdeographs(t *testing.T) {
	in := analysis.TokenStream{&analysis.Token{Term: []byte("abc"), Type: analysis.AlphaNumeric}}
	out := NewBigramFilter(true).Filter(in)
	if len(out) != 1 || string(out[0].Term) != "abc" {
		t.Fatalf("unexpected output %v", out)
	}
}

func TestWidthNoCombine(t *testing.T) {
	in := analysis.TokenStream{&analysis.Token{Term: []byte("a\uFF9E")}}
	out := NewWidthFilter().Filter(in)
	if got := string(out[0].Term); got != "a\u3099" {
		t.Fatalf("got %q", got)
	}
}
