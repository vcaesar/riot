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
	"sync"
	"testing"

	gogse "github.com/go-ego/gse"

	"github.com/vcaesar/riot/analysis"
)

var (
	segOnce sync.Once
	segZh   *gogse.Segmenter
	segErr  error
)

func loadSeg(t *testing.T) *gogse.Segmenter {
	t.Helper()
	segOnce.Do(func() {
		segZh, segErr = NewSegmenter(Option{Dicts: "embed, zh"})
	})
	if segErr != nil {
		t.Fatalf("error loading gse dictionary: %v", segErr)
	}
	return segZh
}

func tokenTerms(tokens analysis.TokenStream) []string {
	out := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		out = append(out, string(tok.Term))
	}
	return out
}

func TestTokenizer(t *testing.T) {
	seg := loadSeg(t)
	tests := []struct {
		name   string
		search bool
		hmm    []bool
		input  string
		want   []string
	}{
		{
			name:  "precise mixed",
			input: "Riot 是用 Go 语言编写的全文搜索引擎。",
			want:  []string{"riot", "是", "用", "go", "语言", "编写", "的", "全文", "搜索引擎"},
		},
		{
			name:   "search hmm",
			search: true,
			hmm:    []bool{true},
			input:  "今天是星期几",
			want:   []string{"今天", "是", "星期", "几"},
		},
		{
			name:   "search without hmm keeps whole word and single runes",
			search: true,
			input:  "搜索引擎好, 四条腿好, All animals is equal.",
			want:   []string{"搜索", "引擎", "搜索引擎", "好", "四", "条", "四条", "腿", "好", "all", "animals", "is", "equal"},
		},
		{
			name:  "punct and space only",
			input: " ，。！ ",
			want:  []string{},
		},
		{
			name:  "empty",
			input: "",
			want:  []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := NewTokenizer(seg, tt.search, tt.hmm...).Tokenize([]byte(tt.input))
			if got := tokenTerms(tokens); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("terms = %v, want %v", got, tt.want)
			}
			checkOffsets(t, tt.input, tokens)
		})
	}
}

func checkOffsets(t *testing.T, input string, tokens analysis.TokenStream) {
	t.Helper()
	segStart := 0
	for i, tok := range tokens {
		if tok.End <= tok.Start || tok.End > len(input) {
			t.Fatalf("bad offsets for %q: %d-%d", tok.Term, tok.Start, tok.End)
		}
		if got := lowerASCII([]byte(input[tok.Start:tok.End])); got != string(tok.Term) {
			t.Fatalf("offset mismatch: term %q, slice %q", tok.Term, got)
		}
		if want := tokenType(string(tok.Term)); tok.Type != want {
			t.Fatalf("token %q type = %d, want %d", tok.Term, tok.Type, want)
		}
		switch tok.PositionIncr {
		case 1:
			segStart = tok.Start
		case 0:
			if i == 0 {
				t.Fatalf("first token %q has PositionIncr 0", tok.Term)
			}
		default:
			t.Fatalf("bad PositionIncr %d for %q", tok.PositionIncr, tok.Term)
		}
		if tok.Start < segStart {
			t.Fatalf("token %q starts before its segment", tok.Term)
		}
	}
}

func TestTokenizerRepeatedWords(t *testing.T) {
	seg := loadSeg(t)
	input := "天气天气"
	tokens := NewTokenizer(seg, false).Tokenize([]byte(input))
	if got := tokenTerms(tokens); !reflect.DeepEqual(got, []string{"天气", "天气"}) {
		t.Fatalf("terms = %v", got)
	}
	if tokens[0].Start != 0 || tokens[1].Start != 6 {
		t.Fatalf("repeated word offsets = %d, %d; want 0, 6", tokens[0].Start, tokens[1].Start)
	}
}

func TestTokenizerStopWords(t *testing.T) {
	seg, err := NewSegmenter(Option{Dicts: "embed, zh"})
	if err != nil {
		t.Fatal(err)
	}
	seg.LoadStopArr([]string{"的", "是"})
	tokens := NewTokenizer(seg, false).Tokenize([]byte("今天是晴朗的日子"))
	want := []string{"今天", "晴朗", "日子"}
	if got := tokenTerms(tokens); !reflect.DeepEqual(got, want) {
		t.Fatalf("terms = %v, want %v", got, want)
	}
}

func TestTokenType(t *testing.T) {
	for term, want := range map[string]analysis.TokenType{
		"搜索": analysis.Ideographic, "こん": analysis.Ideographic, "한": analysis.Ideographic,
		"riot": analysis.AlphaNumeric, "go1": analysis.AlphaNumeric,
		"2026": analysis.Numeric, "3.5": analysis.Numeric,
	} {
		if got := tokenType(term); got != want {
			t.Fatalf("tokenType(%q) = %d, want %d", term, got, want)
		}
	}
}

func TestLowerASCII(t *testing.T) {
	in := "Go 语言 ÀB"
	if got := lowerASCII([]byte(in)); got != "go 语言 Àb" || len(got) != len(in) {
		t.Fatalf("lowerASCII = %q", got)
	}
}
