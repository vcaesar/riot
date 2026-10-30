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
	"reflect"
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
// See document.Field for details.
type TokenLocation struct {
	FieldVal    string
	StartVal    int
	EndVal      int
	PositionVal int
}

func (tl *TokenLocation) Field() string {
	return tl.FieldVal
}

func (tl *TokenLocation) Pos() int {
	return tl.PositionVal
}

func (tl *TokenLocation) Start() int {
	return tl.StartVal
}

func (tl *TokenLocation) End() int {
	return tl.EndVal
}

func (tl *TokenLocation) Size() int {
	return reflectStaticSizeTokenLocation
}

// TokenFreq represents all the occurrences of a term in all fields of a
// document.
type TokenFreq struct {
	TermVal   []byte
	Locations []*TokenLocation
	frequency int
}

func (tf *TokenFreq) Size() int {
	rv := reflectStaticSizeTokenFreq
	rv += len(tf.TermVal)
	for _, loc := range tf.Locations {
		rv += loc.Size()
	}
	return rv
}

func (tf *TokenFreq) Term() []byte {
	return tf.TermVal
}

func (tf *TokenFreq) Frequency() int {
	return tf.frequency
}

func (tf *TokenFreq) EachLocation(location segment.VisitLocation) {
	for _, tl := range tf.Locations {
		location(tl)
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
	for _, l := range tf.Locations {
		l.FieldVal = remoteField
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
	for _, l := range tf.Locations {
		l.FieldVal = remoteField
	}
	existingTf, exists := tfs[string(tfk)]
	if exists {
		existingTf.Locations = append(existingTf.Locations, tf.Locations...)
		existingTf.frequency += tf.frequency
	} else {
		tfs[string(tfk)] = &TokenFreq{
			TermVal:   tf.TermVal,
			frequency: tf.frequency,
			Locations: make([]*TokenLocation, len(tf.Locations)),
		}
		copy(tfs[string(tfk)].Locations, tf.Locations)
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

// TokenFrequencySlice is TokenFrequency without the map: the distinct terms
// in first occurrence order. As with TokenFrequency, position is only
// tracked (from startOffset) when includeTermVectors is set.
func TokenFrequencySlice(tokens TokenStream, includeTermVectors bool, startOffset int) (
	tfs []TokenFreq, position int) {
	// bounded by len(tokens) so it is never grown and pointers into it stay valid
	tfs = make([]TokenFreq, 0, len(tokens))
	var tls []TokenLocation
	var tlPtrs []*TokenLocation
	if includeTermVectors {
		tls = make([]TokenLocation, len(tokens))
		tlPtrs = make([]*TokenLocation, len(tokens))
		position = startOffset
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
		tf := &tfs[idx]
		tf.frequency++
		if !includeTermVectors {
			continue
		}
		position += token.PositionIncr
		tls[i] = TokenLocation{
			StartVal:    token.Start,
			EndVal:      token.End,
			PositionVal: position,
		}
		tlPtrs[i] = &tls[i]
		if tf.Locations == nil {
			// cap 1 so a second location reallocates rather than
			// overwriting the next term's pointer in tlPtrs
			tf.Locations = tlPtrs[i : i+1 : i+1]
		} else {
			tf.Locations = append(tf.Locations, tlPtrs[i])
		}
	}
	return tfs, position
}
