//  Copyright (c) 2020 Couchbase, Inc.
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

package tokenizer

import (
	"github.com/zedsearch/segment"

	"github.com/vcaesar/riot/analysis"
)

const maxEstimatedRemainingSegments = 1000
const maxRvCapacity = 256

type UnicodeTokenizer struct{}

func NewUnicodeTokenizer() *UnicodeTokenizer {
	return &UnicodeTokenizer{}
}

func (rt *UnicodeTokenizer) Tokenize(input []byte) analysis.TokenStream {
	// size the first stream and token block for words of about 5 bytes
	// plus a separator, so short inputs of such text never regrow them
	est := len(input)/6 + 1
	return tokenize(input, make(analysis.TokenStream, 0, min(est, maxRvCapacity)),
		make([]analysis.Token, min(est, maxEstimatedRemainingSegments)))
}

// Scratch is the reusable token storage of TokenizeInto.
type Scratch struct {
	stream analysis.TokenStream
	tokens []analysis.Token
}

// TokenizeInto is Tokenize reusing s's stream and first token block; the
// tokens returned are valid only until s is used again.
func (rt *UnicodeTokenizer) TokenizeInto(input []byte, s *Scratch) analysis.TokenStream {
	if est := min(len(input)/6+1, maxEstimatedRemainingSegments); cap(s.tokens) < est {
		s.tokens = make([]analysis.Token, est)
	}
	if est := min(len(input)/6+1, maxRvCapacity); cap(s.stream) < est {
		s.stream = make(analysis.TokenStream, 0, est)
	}
	rv := tokenize(input, s.stream[:0], s.tokens[:cap(s.tokens)])
	if cap(rv) > cap(s.stream) {
		s.stream = rv[:0]
	}
	return rv
}

// tokens collects a token stream, taking tokens from ta, then from blocks
// sized by the words seen so far.
type tokens struct {
	input  []byte
	rv     analysis.TokenStream
	rvx    []analysis.TokenStream // filled streams, before rv
	ta     []analysis.Token
	taNext int
}

// tokenize appends the tokens of input to rv, taking them from ta first.
func tokenize(input []byte, rv analysis.TokenStream, ta []analysis.Token) analysis.TokenStream {
	t := tokens{input: input, rv: rv, ta: ta}
	t.addWords()
	return t.stream()
}

// addWords adds the words of the input. No Unicode word rule joins over
// an ASCII separator (asciiBreak), so each span from one up to the next
// that is not plain ASCII is left to the segmenter, and the rest to
// addPlainASCII.
func (t *tokens) addWords() {
	in := t.input
	done := 0
	for i := 0; i < len(in); i++ {
		if in[i] < 0x80 && asciiClass[in[i]] != 0 {
			continue
		}
		lo, hi := i, i+1
		for lo > done && !isBreak(in[lo]) {
			lo--
		}
		for hi < len(in) && !isBreak(in[hi]) {
			hi++
		}
		t.addPlainASCII(done, lo)
		t.addSegments(lo, hi)
		done, i = hi, hi
	}
	t.addPlainASCII(done, len(in))
}

func isBreak(b byte) bool { return b < 0x80 && asciiClass[b] == asciiBreak }

// addSegments adds the words the Unicode segmenter finds in input[lo:hi].
func (t *tokens) addSegments(lo, hi int) {
	segmenter := segment.NewWordSegmenterDirect(t.input[lo:hi])
	start := lo
	for segmenter.Segment() {
		end := start + len(segmenter.Bytes())
		if typ := segmenter.Type(); typ != segment.None {
			t.add(start, end, convertType(typ))
		}
		start = end
	}
}

func (t *tokens) add(start, end int, typ analysis.TokenType) {
	if t.taNext >= len(t.ta) {
		// guess the remaining words from the average length so far
		remaining := (len(t.input) - end) / max(end/(len(t.rv)+1), 1)
		t.ta = make([]analysis.Token, min(max(remaining, 1), maxEstimatedRemainingSegments))
		t.taNext = 0
	}
	token := &t.ta[t.taNext]
	t.taNext++
	*token = analysis.Token{ // reused blocks may hold an old token
		Term:         t.input[start:end],
		Start:        start,
		End:          end,
		PositionIncr: 1,
		Type:         typ,
	}
	if len(t.rv) >= cap(t.rv) { // When rv is full, save it into rvx.
		t.rvx = append(t.rvx, t.rv)
		t.rv = make(analysis.TokenStream, 0, min(cap(t.rv)*2, maxRvCapacity)) // Next rv cap is bigger.
	}
	t.rv = append(t.rv, token)
}

func (t *tokens) stream() analysis.TokenStream {
	if len(t.rvx) == 0 {
		return t.rv
	}
	n := len(t.rv)
	for _, r := range t.rvx {
		n += len(r)
	}
	rall := make(analysis.TokenStream, 0, n)
	for _, r := range t.rvx {
		rall = append(rall, r...)
	}
	return append(rall, t.rv...)
}

// asciiClass is, per ASCII byte, asciiWord for letters and digits,
// asciiMid for the bytes the Unicode word rules join words over between
// letters or digits (. ' : , ;), 0 for the underscore, which joins words
// in more ways, and for the double quote, which joins Hebrew letters
// (WB7b, WB7c), and asciiBreak for bytes that always separate words
// (Unicode word break property Other, CR, LF, Newline or WSegSpace).
var asciiClass = func() (c [128]byte) {
	for b := range c {
		switch {
		case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
			c[b] = asciiWord
		case b == '.', b == '\'', b == ':', b == ',', b == ';':
			c[b] = asciiMid
		case b == '_', b == '"':
		default:
			c[b] = asciiBreak
		}
	}
	return c
}()

const (
	asciiWord = 1 + iota
	asciiMid
	asciiBreak
)

func isDigit(b byte) bool { return b <= '9' } // of a letter or digit

// joins reports whether the Unicode word rules keep the letters or digits
// prev and next together over mid (WB6, WB7, WB11, WB12).
func joins(prev, mid, next byte) bool {
	switch mid {
	case '.', '\'':
		return isDigit(prev) == isDigit(next)
	case ':':
		return !isDigit(prev) && !isDigit(next)
	default: // , ;
		return isDigit(prev) && isDigit(next)
	}
}

// addPlainASCII adds the words of input[lo:hi], ASCII without
// underscores, as the Unicode segmenter finds and types them: letters and
// digits (WB5, WB8-WB10) joined over . ' : , ; as the rules allow,
// Numeric without letters, else AlphaNumeric.
func (t *tokens) addPlainASCII(lo, hi int) {
	in := t.input[:hi]
	for i := lo; i < len(in); {
		if asciiClass[in[i]] != asciiWord {
			i++
			continue
		}
		start, typ := i, analysis.Numeric
		for {
			if !isDigit(in[i]) {
				typ = analysis.AlphaNumeric
			}
			i++
			if i == len(in) {
				break
			}
			switch asciiClass[in[i]] {
			case asciiWord:
				continue
			case asciiMid:
				if i+1 < len(in) && asciiClass[in[i+1]] == asciiWord && joins(in[i-1], in[i], in[i+1]) {
					i++
					continue
				}
			}
			break
		}
		t.add(start, i, typ)
	}
}

func convertType(segmentWordType int) analysis.TokenType {
	switch segmentWordType {
	case segment.Ideo:
		return analysis.Ideographic
	case segment.Kana:
		return analysis.Ideographic
	case segment.Number:
		return analysis.Numeric
	}
	return analysis.AlphaNumeric
}
