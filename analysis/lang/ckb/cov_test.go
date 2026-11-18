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

package ckb

import "testing"

func TestStemNormalizeCoverage(t *testing.T) {
	stems := []struct{ in, want string }{
		{"ببببیەکی", "بببب"},
		{"بببببانی", "ببببب"},
		{"ببببببیانی", "بببببب"},
	}
	for _, tt := range stems {
		if got := string(stem([]byte(tt.in))); got != tt.want {
			t.Errorf("stem(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if got := string(normalize([]byte("ب\u200Eب"))); got != "بب" {
		t.Errorf("normalize did not drop format rune: %q", got)
	}
}
