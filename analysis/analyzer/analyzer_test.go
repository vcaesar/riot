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

package analyzer

import (
	"reflect"
	"testing"

	"github.com/vcaesar/riot/analysis"
)

func terms(ts analysis.TokenStream) []string {
	rv := make([]string, 0, len(ts))
	for _, t := range ts {
		rv = append(rv, string(t.Term))
	}
	return rv
}

func TestAnalyzers(t *testing.T) {
	tests := []struct {
		name     string
		analyzer *analysis.Analyzer
		input    string
		want     []string
	}{
		{"keyword", NewKeywordAnalyzer(), "Hello World", []string{"Hello World"}},
		{"simple", NewSimpleAnalyzer(), "Hello World42", []string{"hello", "world"}},
		{"standard", NewStandardAnalyzer(), "The Quick fox", []string{"the", "quick", "fox"}},
		{"web", NewWebAnalyzer(), "Mail a@b.com The Fox", []string{"mail", "a@b.com", "fox"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := terms(tt.analyzer.Analyze([]byte(tt.input)))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
