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

package es

import "testing"

func TestStemCoverage(t *testing.T) {
	tests := []struct{ in, want string }{
		{"casas", "cas"},
		{"àòèùìx", "aoeuix"},
		{"gatos", "gat"}, {"mesa", "mesa"},
		{"eseses", "eses"},
		{"luces", "luz"},
		{"perros", "perr"},
		{"casas_", "casas_"},
	}
	for _, tt := range tests {
		if got := string(stem([]rune(tt.in))); got != tt.want {
			t.Errorf("stem(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
