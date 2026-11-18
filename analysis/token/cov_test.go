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

package token

import (
	"testing"

	"github.com/vcaesar/riot/analysis"
)

func TestNonAlphaNumericStartSym(t *testing.T) {
	s := &NonAlphaNumericCaseState{}
	if !s.StartSym('-') || s.StartSym('a') {
		t.Error("unexpected StartSym result")
	}
}

func TestLengthFilterSkips(t *testing.T) {
	in := analysis.TokenStream{
		{Term: []byte("a"), PositionIncr: 1},
		{Term: []byte("toolong"), PositionIncr: 1},
		{Term: []byte("ok"), PositionIncr: 1},
	}
	out := NewLengthFilter(2, 3).Filter(in)
	if len(out) != 1 || string(out[0].Term) != "ok" || out[0].PositionIncr != 3 {
		t.Fatalf("unexpected output %v", out)
	}
}
