// Copyright (c) 2026 The Bluge Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package in

import "testing"

func TestScriptDecompositionOffsets(t *testing.T) {
	for i, decomposition := range decompositions {
		if decomposition[0] < 0 {
			t.Errorf("decomposition %d has negative initial offset", i)
		}
	}
	for script, data := range scripts {
		for _, r := range script.R16 {
			if int64(r.Lo) < int64(data.base) {
				t.Errorf("script rune %U is below base %U", r.Lo, data.base)
			}
		}
		for _, r := range script.R32 {
			if int64(r.Lo) < int64(data.base) {
				t.Errorf("script rune %U is below base %U", r.Lo, data.base)
			}
		}
	}
}
