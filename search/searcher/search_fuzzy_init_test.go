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

package searcher

import (
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
	"testing"

	"github.com/blevesearch/vellum"
)

// TestPackageInitIsCheap guards process startup (and so index reopen wall
// time): building the levenshtein builders eagerly cost ~25,700 allocs and
// ~2 ms in this package's init.
func TestPackageInitIsCheap(t *testing.T) {
	if os.Getenv("RIOT_INITTRACE_CHILD") == "1" {
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestPackageInitIsCheap$") //nolint:gosec // G204: re-runs this test binary
	cmd.Env = append(os.Environ(), "GODEBUG=inittrace=1", "RIOT_INITTRACE_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("error running child: %v\n%s", err, out)
	}
	m := regexp.MustCompile(`init github\.com/vcaesar/riot/search/searcher @.* (\d+) allocs`).FindSubmatch(out)
	if m == nil {
		t.Fatalf("no inittrace line for package searcher:\n%s", out)
	}
	allocs, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatal(err)
	}
	// test-only package vars account for a few hundred allocs
	if allocs > 5000 {
		t.Fatalf("package init made %d allocs, want <= 5000", allocs)
	}
}

func TestGetLevAutomatonFuzziness(t *testing.T) {
	for _, f := range []int{-1, 0, 3} {
		if _, err := getLevAutomaton("beet", f); err == nil {
			t.Errorf("fuzziness %d: want error", f)
		}
	}

	var wg sync.WaitGroup
	for range 8 {
		for _, f := range []int{1, 2} {
			wg.Go(func() {
				a, err := getLevAutomaton("beet", f)
				if err != nil {
					t.Errorf("fuzziness %d: %v", f, err)
					return
				}
				if !vellum.AutomatonContains(a, []byte("bet")) {
					t.Errorf("fuzziness %d: want bet to match beet", f)
				}
				if got := vellum.AutomatonContains(a, []byte("bt")); got != (f == 2) {
					t.Errorf("fuzziness %d: bt match = %v", f, got)
				}
			})
		}
	}
	wg.Wait()
}
