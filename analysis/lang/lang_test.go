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

package lang

import (
	"reflect"
	"strings"
	"testing"

	"github.com/vcaesar/riot/analysis"
)

func TestBuiltinCodes(t *testing.T) {
	want := []string{"en", "keyword", "simple", "standard", "web"}
	if got := Codes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Codes() = %v, want %v (no language package imported)", got, want)
	}
	for _, code := range want {
		if a, err := New(code); err != nil || a == nil || a.Tokenizer == nil {
			t.Fatalf("New(%q) = %v, %v", code, a, err)
		}
	}
}

func TestNewUnknown(t *testing.T) {
	for _, code := range []string{"", "xx", "de"} {
		if _, err := New(code); err == nil || !strings.Contains(err.Error(), `"`+code+`"`) {
			t.Fatalf("New(%q) error = %v", code, err)
		}
	}
}

func TestRegister(t *testing.T) {
	calls := 0
	Register("test-xx", func() *analysis.Analyzer {
		calls++
		return &analysis.Analyzer{}
	})
	t.Cleanup(func() {
		mu.Lock()
		delete(analyzers, "test-xx")
		mu.Unlock()
	})
	a, b := mustNew(t, "test-xx"), mustNew(t, "test-xx")
	if a == b || calls != 2 {
		t.Fatalf("want a new analyzer per call, got same=%v calls=%d", a == b, calls)
	}
}

func mustNew(t *testing.T, code string) *analysis.Analyzer {
	t.Helper()
	a, err := New(code)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
