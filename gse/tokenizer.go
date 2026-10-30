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

package gse

import (
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
	"unsafe"

	gogse "github.com/go-ego/gse"

	"github.com/vcaesar/riot/analysis"
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
	words := t.seg.Cut(text, t.hmm...)
	// one token block and one term arena for the whole stream; terms get
	// their own bytes since search mode sub-words overlap
	block := make([]analysis.Token, 0, len(words))
	arenaCap := len(text)
	if t.search {
		arenaCap *= 2 // sub-words roughly double the term bytes
	}
	arena := make([]byte, 0, arenaCap)
	cursor := 0
	for _, word := range words {
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
		// a single rune has no sub-words, CutSearch would return just it;
		// nor does a plain ASCII letter/digit word without hmm, which gse
		// keeps whole unless AlphaNum
		if t.search && utf8.RuneCountInString(word) > 1 &&
			(t.seg.AlphaNum || len(t.hmm) > 0 || !isASCIIAlnum(word)) {
			subs = t.seg.CutSearch(word, t.hmm...)
			// gse's plain search mode returns only the sub-words; the
			// segment itself must stay searchable.
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
			arena = append(arena, sub...)
			block = append(block, analysis.Token{
				Start:        subStart,
				End:          subStart + len(sub),
				Term:         arena[len(arena)-len(sub) : len(arena) : len(arena)],
				PositionIncr: posIncr,
				Type:         tokenType(sub),
			})
			posIncr = 0
		}
	}
	tokens := make(analysis.TokenStream, len(block))
	for i := range block {
		tokens[i] = &block[i]
	}
	return tokens
}

// tokenType classifies a term like analysis/tokenizer does so that filters
// such as the cjk bigram filter treat Latin and numeric terms correctly.
func tokenType(term string) analysis.TokenType {
	for _, r := range term {
		if r < utf8.RuneSelf {
			continue
		}
		if unicode.In(r, unicode.Han, unicode.Hangul, unicode.Hiragana, unicode.Katakana) {
			return analysis.Ideographic
		}
	}
	// ParseFloat only accepts digits, a sign, a dot, "inf" or "nan" first;
	// checking that skips its error allocation for ordinary words.
	if term != "" && strings.IndexByte("0123456789+-.iInN", term[0]) >= 0 {
		if _, err := strconv.ParseFloat(term, 64); err == nil {
			return analysis.Numeric
		}
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
	return unsafe.String(unsafe.SliceData(out), len(out)) // out is never written again
}

func isASCIIAlnum(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}
