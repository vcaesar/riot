//go:build !race

// Copyright (c) 2026 The AtomAI.
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

package riot

import "testing"

// Race instrumentation deliberately drops sync.Pool entries, so allocation
// budgets for pooled storage are checked only without -race.
func TestTextFieldReleaseAllocations(t *testing.T) {
	f := NewTextField("body", "the quick brown fox jumps over the lazy dog the end").SearchTermPositions()
	if allocs := testing.AllocsPerRun(50, func() { f.Analyze(0); f.ReleaseAnalysis() }); allocs > 2 {
		t.Fatalf("pooled analyze+release allocates %v per run", allocs)
	}
}
