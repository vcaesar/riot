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
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestLangs(t *testing.T) {
	codes := Langs()
	if !sort.StringsAreSorted(codes) || len(codes) != len(langAnalyzers) {
		t.Fatalf("Langs() = %v", codes)
	}
	for _, code := range codes {
		a, err := NewLangAnalyzer(code)
		if err != nil || a == nil || a.Tokenizer == nil {
			t.Fatalf("NewLangAnalyzer(%q) = %v, %v", code, a, err)
		}
	}
	if _, err := NewLangAnalyzer("xx"); err == nil || !strings.Contains(err.Error(), `"xx"`) {
		t.Fatalf("expected unknown language error, got %v", err)
	}
	if _, err := NewLangAnalyzer(""); err == nil {
		t.Fatal("expected error for empty language")
	}
}

func TestNewAnalyzerNilSegmenter(t *testing.T) {
	tests := []struct {
		lang  string
		input string
		want  []string
	}{
		{"en", "The Running foxes", []string{"run", "fox"}},
		{"cjk", "こんにちは", []string{"こん", "んに", "にち", "ちは"}},
		{"de", "Häuser und Bäume", []string{"haus", "baum"}},
		{"keyword", "Keep As Is", []string{"Keep As Is"}},
	}
	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			a, err := NewAnalyzer(nil, Option{Lang: tt.lang, Opt: "search-hmm", Dicts: "ignored"})
			if err != nil {
				t.Fatal(err)
			}
			if got := terms(a, tt.input); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("terms = %v, want %v", got, tt.want)
			}
		})
	}
	if _, err := NewAnalyzer(nil, Option{}); err == nil {
		t.Fatal("expected error for nil segmenter without Lang")
	}
}

func TestIndexLang(t *testing.T) {
	idx := openIndex(t, Option{Lang: "en", Dicts: "./test/does-not-exist.txt", Opt: "bogus"})
	if idx.Segmenter() != nil {
		t.Fatal("expected nil segmenter for Lang index")
	}
	docs := map[string]string{
		"1": "The quick brown foxes jumped over the lazy dogs",
		"2": "A humble vaudevillian veteran",
	}
	for id, text := range docs {
		if err := idx.Index(id, text); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		query string
		want  []string
	}{
		{"fox", []string{"1"}},
		{"Jumping Dogs", []string{"1"}},
		{"veterans", []string{"2"}},
		{"cat", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			res, err := idx.Search(NewQueryString(tt.query, true))
			if err != nil {
				t.Fatal(err)
			}
			if got := hitIDs(res); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("hits = %v, want %v", got, tt.want)
			}
			for _, h := range res.Hits {
				if h.Fields["text"] != docs[h.ID] || len(h.Fragments["text"]) != 1 ||
					!strings.Contains(h.Fragments["text"][0], "<mark>") {
					t.Fatalf("unexpected hit %+v", h)
				}
			}
		})
	}
}

func TestIndexLangUnknown(t *testing.T) {
	if _, err := New(Option{Lang: "xx"}); err == nil {
		t.Fatal("expected error for unknown Lang")
	}
}
