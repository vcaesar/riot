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

package in

import "testing"

func TestComposeBranches(t *testing.T) {
	tests := []struct{ in, want string }{
		{"अx", "अx"},
		{"अक", "अक"},
		{"अाx", "आx"},
		{"अा\u200D", "आ\u200D"},
	}
	for _, tt := range tests {
		if got := string(normalize([]rune(tt.in))); got != tt.want {
			t.Errorf("normalize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
