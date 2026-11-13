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

package analysis

import (
	"bytes"
	"math"
	"reflect"
	"slices"
	"sync"
	"unsafe"

	segment "github.com/vcaesar/bluge_segment_api"
)

var reflectStaticSizeTokenLocation int
var reflectStaticSizeTokenFreq int

func init() {
	var tl TokenLocation
	reflectStaticSizeTokenLocation = int(reflect.TypeOf(tl).Size())
	var tf TokenFreq
	reflectStaticSizeTokenFreq = int(reflect.TypeOf(tf).Size())
}

// TokenLocation represents one occurrence of a term at a particular location in
// a field. Start, End and Position have the same meaning as in analysis.Token.
// Field and ArrayPositions identify the field value in the source document.
// See document.Field for details. Values beyond int32 are stored as -1,
// which segments reject as a negative location.
type TokenLocation struct {
	FieldVal    string
	StartVal    int32
	EndVal      int32
	PositionVal int32
}

// Loc32 converts a location value for TokenLocation: -1 when it does not
// fit an int32.
func Loc32(v int) int32 {
	if v > math.MaxInt32 || v < math.MinInt32 {
		return -1
	}
	return int32(v)
}

func (tl *TokenLocation) Field() string {
	return tl.FieldVal
}

func (tl *TokenLocation) Pos() int {
	return int(tl.PositionVal)
}

func (tl *TokenLocation) Start() int {
	return int(tl.StartVal)
}

func (tl *TokenLocation) End() int {
	return int(tl.EndVal)
}

func (tl *TokenLocation) Size() int {
	return reflectStaticSizeTokenLocation
}

// TokenFreq represents all the occurrences of a term in all fields of a
// document. Locations holds the values, not pointers, so a term's
// locations are one allocation (or a window of one shared by the field).
type TokenFreq struct {
	TermVal   []byte
	Locations []TokenLocation
	frequency int
}

func (tf *TokenFreq) Size() int {
	return reflectStaticSizeTokenFreq + len(tf.TermVal) +
		len(tf.Locations)*reflectStaticSizeTokenLocation
}

func (tf *TokenFreq) Term() []byte {
	return tf.TermVal
}

func (tf *TokenFreq) Frequency() int {
	return tf.frequency
}

func (tf *TokenFreq) EachLocation(location segment.VisitLocation) {
	for i := range tf.Locations {
		location(&tf.Locations[i])
	}
}

// TokenFrequencies maps document terms to their combined frequencies from all
// fields.
type TokenFrequencies map[string]*TokenFreq

func (tfs TokenFrequencies) Size() int {
	rv := sizeOfMap
	rv += len(tfs) * (sizeOfString + sizeOfPtr)
	for k, v := range tfs {
		rv += len(k)
		rv += v.Size()
	}
	return rv
}

func (tfs TokenFrequencies) MergeAll(remoteField string, other TokenFrequencies) {
	// one block for every term new to tfs, allocated on first need and
	// bounded by len(other) so it is never grown and the pointers stay valid
	var block []TokenFreq
	// walk the new token frequencies
	for tfk, tf := range other {
		block = tfs.mergeOne(remoteField, tfk, tf, block, len(other))
	}
}

func (tfs TokenFrequencies) mergeOne(remoteField, tfk string, tf *TokenFreq, block []TokenFreq, blockCap int) []TokenFreq {
	// set the remoteField value in incoming token freqs
	for i := range tf.Locations {
		tf.Locations[i].FieldVal = remoteField
	}
	existingTf, exists := tfs[tfk]
	if exists {
		existingTf.Locations = append(existingTf.Locations, tf.Locations...)
		existingTf.frequency += tf.frequency
		return block
	}
	if block == nil {
		block = make([]TokenFreq, 0, blockCap)
	}
	block = append(block, TokenFreq{
		TermVal:   tf.TermVal,
		frequency: tf.frequency,
		// share the source locations; the capped slice forces any later
		// append to reallocate instead of writing into the source field
		Locations: tf.Locations[:len(tf.Locations):len(tf.Locations)],
	})
	tfs[tfk] = &block[len(block)-1]
	return block
}

func (tfs TokenFrequencies) MergeOneBytes(remoteField string, tfk []byte, tf *TokenFreq) {
	// set the remoteField value in incoming token freqs
	for i := range tf.Locations {
		tf.Locations[i].FieldVal = remoteField
	}
	existingTf, exists := tfs[string(tfk)]
	if exists {
		existingTf.Locations = append(existingTf.Locations, tf.Locations...)
		existingTf.frequency += tf.frequency
	} else {
		tfs[string(tfk)] = &TokenFreq{
			TermVal:   tf.TermVal,
			frequency: tf.frequency,
			Locations: slices.Clone(tf.Locations),
		}
	}
}

func TokenFrequency(tokens TokenStream, includeTermVectors bool, startOffset int) (
	tokenFreqs TokenFrequencies, position int) {
	tfs, position := TokenFrequencySlice(tokens, includeTermVectors, startOffset)
	return NewTokenFrequencies(tfs), position
}

// NewTokenFrequencies indexes tfs by term; the map points into tfs.
func NewTokenFrequencies(tfs []TokenFreq) TokenFrequencies {
	tokenFreqs := make(TokenFrequencies, len(tfs))
	for i := range tfs {
		tokenFreqs[string(tfs[i].TermVal)] = &tfs[i]
	}
	return tokenFreqs
}

// linearDedupMax is the stream length up to which terms are deduplicated
// by scanning instead of hashing.
const linearDedupMax = 16

// maxPooledDedup bounds the scratch maps kept for reuse.
const maxPooledDedup = 1 << 14

// dedupMaps recycles the term -> index scratch maps of TokenFrequencySlice.
// Keys alias token bytes and are cleared before the map is put back.
var dedupMaps = sync.Pool{New: func() any { return make(map[string]int32) }}

// maxPooledTermOf bounds the per-token term index scratch kept for reuse.
const maxPooledTermOf = 1 << 16

// termOfs recycles TokenFrequencySlice's token -> term index scratch.
var termOfs = sync.Pool{New: func() any { return new([]int32) }}

// AppendTerm adds one occurrence of term, without a location, to tfs: as
// TokenFrequencySlice does for a short stream without term vectors.
func AppendTerm(tfs []TokenFreq, term []byte) []TokenFreq {
	for i := range tfs {
		if bytes.Equal(tfs[i].TermVal, term) {
			tfs[i].frequency++
			return tfs
		}
	}
	return append(tfs, TokenFreq{TermVal: term, frequency: 1})
}

// SingleTokenFrequency is TokenFrequencySlice for a stream of one token
// spanning all of term (position increment 1), without building the stream.
func SingleTokenFrequency(term []byte, includeTermVectors bool, startOffset int) (
	tfs []TokenFreq, position int) {
	if !includeTermVectors {
		return []TokenFreq{{TermVal: term, frequency: 1}}, 0
	}
	// the term and its location in one allocation
	one := &struct {
		tf  [1]TokenFreq
		loc [1]TokenLocation
	}{}
	position = startOffset + 1
	one.loc[0] = TokenLocation{EndVal: Loc32(len(term)), PositionVal: Loc32(position)}
	one.tf[0] = TokenFreq{TermVal: term, frequency: 1, Locations: one.loc[:]}
	return one.tf[:], position
}

// TokenFrequencySlice is TokenFrequency without the map: the distinct terms
// in first occurrence order. As with TokenFrequency, position is only
// tracked (from startOffset) when includeTermVectors is set.
func TokenFrequencySlice(tokens TokenStream, includeTermVectors bool, startOffset int) (
	tfs []TokenFreq, position int) {
	var tls []TokenLocation
	if includeTermVectors {
		tls = make([]TokenLocation, len(tokens))
	}
	return tokenFrequency(tokens, includeTermVectors, startOffset, make([]TokenFreq, 0, len(tokens)), tls)
}

// FreqBlock is the storage behind a TokenFrequencyPooled result.
type FreqBlock struct {
	tfs  []TokenFreq
	tls  []TokenLocation
	text []byte
}

// maxPooledFreqBlock bounds the terms of a block kept for reuse.
const maxPooledFreqBlock = 1 << 12

// maxPooledFreqText bounds the copied text of a block kept for reuse.
const maxPooledFreqText = 1 << 16

var freqBlocks = sync.Pool{New: func() any { return new(FreqBlock) }}

// TokenFrequencyPooled is TokenFrequencySlice into recycled storage. The
// result is valid until blk is passed to ReleaseFreqBlock.
func TokenFrequencyPooled(tokens TokenStream, includeTermVectors bool, startOffset int) (
	tfs []TokenFreq, position int, blk *FreqBlock) {
	blk = NewFreqBlock()
	tfs, position = blk.TokenFrequency(tokens, includeTermVectors, startOffset)
	return tfs, position, blk
}

// NewFreqBlock returns recycled storage for TokenFrequency and CopyText.
func NewFreqBlock() *FreqBlock {
	return freqBlocks.Get().(*FreqBlock)
}

// CopyText copies text into blk, so tokens may alias and modify the copy;
// it is valid until blk is passed to ReleaseFreqBlock.
func (blk *FreqBlock) CopyText(text []byte) []byte {
	blk.text = append(blk.text[:0], text...)
	return blk.text
}

// TokenFrequency is TokenFrequencySlice into blk. The result is valid until
// blk is passed to ReleaseFreqBlock.
func (blk *FreqBlock) TokenFrequency(tokens TokenStream, includeTermVectors bool, startOffset int) (
	tfs []TokenFreq, position int) {
	if cap(blk.tfs) < len(tokens) {
		blk.tfs = make([]TokenFreq, 0, len(tokens))
	}
	var tls []TokenLocation
	if includeTermVectors {
		if cap(blk.tls) < len(tokens) {
			blk.tls = make([]TokenLocation, len(tokens))
		}
		tls = blk.tls[:len(tokens)]
	}
	return tokenFrequency(tokens, includeTermVectors, startOffset, blk.tfs[:0], tls)
}

// ReleaseFreqBlock recycles blk; nothing may use its results afterwards.
func ReleaseFreqBlock(blk *FreqBlock) {
	if cap(blk.tfs) > maxPooledFreqBlock || cap(blk.tls) > maxPooledFreqBlock || cap(blk.text) > maxPooledFreqText {
		return
	}
	// drop the term and value references so they can be collected
	clear(blk.tfs[:cap(blk.tfs)])
	clear(blk.tls[:cap(blk.tls)])
	freqBlocks.Put(blk)
}

// tokenFrequency fills tfs (capacity >= len(tokens), so it is never grown
// and pointers into it stay valid) and, with term vectors, tls (length
// len(tokens)).
func tokenFrequency(tokens TokenStream, includeTermVectors bool, startOffset int,
	tfs []TokenFreq, tls []TokenLocation) (_ []TokenFreq, position int) {
	// termOf[i] is the term of token i, for laying out locations by term
	var termOf []int32
	if includeTermVectors {
		sp := termOfs.Get().(*[]int32)
		termOf = slices.Grow((*sp)[:0], len(tokens))[:len(tokens)]
		defer func() {
			if cap(termOf) <= maxPooledTermOf {
				*sp = termOf[:0]
				termOfs.Put(sp)
			}
		}()
	}
	var seen map[string]int32
	if len(tokens) > linearDedupMax {
		seen = dedupMaps.Get().(map[string]int32)
		defer func() {
			if len(seen) <= maxPooledDedup {
				clear(seen)
				dedupMaps.Put(seen)
			}
		}()
	}

	for i, token := range tokens {
		idx := -1
		if seen != nil {
			if j, ok := seen[unsafe.String(unsafe.SliceData(token.Term), len(token.Term))]; ok {
				idx = int(j)
			}
		} else {
			for j := range tfs {
				if bytes.Equal(tfs[j].TermVal, token.Term) {
					idx = j
					break
				}
			}
		}
		if idx < 0 {
			idx = len(tfs)
			tfs = append(tfs, TokenFreq{TermVal: token.Term})
			if seen != nil {
				//nolint:gosec // G115: idx < len(tokens), far below 2^31 terms per field.
				seen[unsafe.String(unsafe.SliceData(token.Term), len(token.Term))] = int32(idx)
			}
		}
		tfs[idx].frequency++
		if termOf != nil {
			termOf[i] = int32(idx)
		}
	}
	if !includeTermVectors {
		return tfs, 0
	}

	// one block for the field, a window per term capped at its frequency,
	// so appending to a term's locations later reallocates instead of
	// writing into the next term's window
	off := 0
	for j := range tfs {
		n := tfs[j].frequency
		tfs[j].Locations = tls[off : off : off+n]
		off += n
	}
	position = startOffset
	for i, token := range tokens {
		position += token.PositionIncr
		tf := &tfs[termOf[i]]
		tf.Locations = append(tf.Locations, TokenLocation{
			StartVal:    Loc32(token.Start),
			EndVal:      Loc32(token.End),
			PositionVal: Loc32(position),
		})
	}
	return tfs, position
}
