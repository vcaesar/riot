//  Copyright (c) 2026 The Bluge Authors.
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

package gse

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/vcaesar/riot/analysis"
)

func terms(a *analysis.Analyzer, text string) []string {
	return tokenTerms(a.Analyze([]byte(text)))
}

func newAnalyzer(t *testing.T, opt Option) *analysis.Analyzer {
	t.Helper()
	seg, err := NewSegmenter(opt)
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewAnalyzer(seg, opt)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestNewAnalyzerDicts(t *testing.T) {
	tests := []struct {
		name  string
		opt   Option
		input string
		want  []string
	}{
		{
			name:  "default embedded zh",
			opt:   Option{Opt: "hmm"},
			input: "今天天气很好",
			want:  []string{"今天天气", "很", "好"},
		},
		{
			name:  "embedded zh with stop words",
			opt:   Option{Dicts: "embed, zh", Stop: "embed, zh", Opt: "hmm"},
			input: "尽管下雨，假如天晴",
			want:  []string{"下雨", "天晴"},
		},
		{
			name:  "embedded jp",
			opt:   Option{Dicts: "embed, jp", Opt: "hmm"},
			input: "運命の犠牲者",
			want:  []string{"運命", "の", "犠", "牲", "者"},
		},
		{
			name:  "file zh",
			opt:   Option{Dicts: "zh"},
			input: "搜索引擎",
			want:  []string{"搜索引擎"},
		},
		{
			name:  "search-hmm expands sub-words",
			opt:   Option{Opt: "search-hmm"},
			input: "搜索引擎",
			want:  []string{"搜索", "索引", "引擎", "搜索引擎"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := terms(newAnalyzer(t, tt.opt), tt.input); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("terms = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewErrors(t *testing.T) {
	// gse logs missing dictionary files on its own; keep CI output clean.
	log.SetOutput(io.Discard)
	defer log.SetOutput(os.Stderr)

	if _, err := New(Option{Opt: "bogus"}); err == nil {
		t.Fatal("expected error for unknown cut mode")
	}
	missing := filepath.Join(t.TempDir(), "does-not-exist.txt")
	if _, err := New(Option{Dicts: missing}); err == nil {
		t.Fatal("expected error for missing dictionary file")
	}
	if _, err := New(Option{Stop: missing}); err == nil {
		t.Fatal("expected error for missing stop word file")
	}
}

func TestCutMode(t *testing.T) {
	tests := []struct {
		opt    string
		search bool
		hmm    []bool
	}{
		{"", false, nil},
		{"hmm", false, []bool{true}},
		{"dag", false, []bool{false}},
		{"search", true, nil},
		{"search-hmm", true, []bool{true}},
		{"search-dag", true, []bool{false}},
	}
	for _, tt := range tests {
		search, hmm, err := cutMode(tt.opt)
		if err != nil || search != tt.search || !reflect.DeepEqual(hmm, tt.hmm) {
			t.Fatalf("cutMode(%q) = %v, %v, %v", tt.opt, search, hmm, err)
		}
	}
}

func TestQueryMode(t *testing.T) {
	for opt, want := range map[string]string{
		"": "", "hmm": "hmm", "dag": "dag",
		"search": "", "search-hmm": "hmm", "search-dag": "dag",
	} {
		if got := queryMode(opt); got != want {
			t.Fatalf("queryMode(%q) = %q, want %q", opt, got, want)
		}
	}
}
