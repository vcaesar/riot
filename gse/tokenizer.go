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
	"slices"
	"strconv"
	"strings"
	"unicode"

	gogse "github.com/go-ego/gse"

	"github.com/vcaesar/riot/analysis"
	"github.com/vcaesar/riot/analysis/tokenizer"
)

// Tokenizer adapts a loaded gse.Segmenter to the analysis.Tokenizer interface.
type Tokenizer struct {
	seg *gogse.Segmenter
	// hmm is passed through to gse: nil selects the shortest path cut,
	// []bool{true} the DAG + HMM cut, []bool{false} the DAG cut.
	hmm    []bool
	search bool
}

// NewTokenizer wraps seg. With search enabled every segment is additionally
// expanded into its sub-words (搜索引擎 -> 搜索 索引 引擎 搜索引擎) at the same
// position; use it for indexing so that shorter query terms still hit.
func NewTokenizer(seg *gogse.Segmenter, search bool, hmm ...bool) *Tokenizer {
	return &Tokenizer{seg: seg, hmm: hmm, search: search}
}

// Tokenize cuts input and reports exact byte offsets for every token.
func (t *Tokenizer) Tokenize(input []byte) analysis.TokenStream {
	// gse lowercases ASCII letters only, so byte offsets stay stable.
	text := lowerASCII(input)
	var tokens analysis.TokenStream
	cursor := 0
	for _, word := range t.seg.Cut(text, t.hmm...) {
		idx := strings.Index(text[cursor:], word)
		if idx < 0 {
			continue
		}
		start := cursor + idx
		cursor = start + len(word)
		if t.skip(word) {
			continue
		}
		subs := []string{word}
		if t.search {
			subs = t.seg.CutSearch(word, t.hmm...)
			// gse's plain search mode returns only the sub-words (nothing at
			// all for a single rune); the segment itself must stay searchable.
			if !slices.Contains(subs, word) {
				subs = append(subs, word)
			}
		}
		posIncr := 1
		for _, sub := range subs {
			if sub != word && t.skip(sub) {
				continue
			}
			subStart := start + strings.Index(word, sub)
			tokens = append(tokens, &analysis.Token{
				Start:        subStart,
				End:          subStart + len(sub),
				Term:         []byte(sub),
				PositionIncr: posIncr,
				Type:         tokenType(sub),
			})
			posIncr = 0
		}
	}
	return tokens
}

// tokenType classifies a term like analysis/tokenizer does so that filters
// such as the cjk bigram filter treat Latin and numeric terms correctly.
func tokenType(term string) analysis.TokenType {
	if tokenizer.IdeographRegexp.MatchString(term) {
		return analysis.Ideographic
	}
	if _, err := strconv.ParseFloat(term, 64); err == nil {
		return analysis.Numeric
	}
	return analysis.AlphaNumeric
}

// skip reports whether word is only whitespace/punctuation/symbols or a
// loaded stop word.
func (t *Tokenizer) skip(word string) bool {
	if t.seg.IsStop(word) {
		return true
	}
	return strings.IndexFunc(word, func(r rune) bool {
		return !unicode.IsSpace(r) && !unicode.IsPunct(r) && !unicode.IsSymbol(r)
	}) < 0
}

func lowerASCII(b []byte) string {
	out := make([]byte, len(b))
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}
