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

package index

import (
	"bytes"
	"errors"
	"math"
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"
)

func covStubDoc(store bool) stubDoc {
	return stubDoc{fields: []segment.Field{stubField{
		name: "f", value: []byte("value"), length: 1, index: true, store: store,
		terms: []stubTerm{{term: "term", freq: 1, locs: []stubLoc{{field: "f", pos: 1, end: 4}}}},
	}}}
}

func TestPackedBufferFullRuns(t *testing.T) {
	tests := []struct {
		name string
		fill func(b *packedBuffer)
	}{
		{"docs", func(b *packedBuffer) { b.docs.n = math.MaxUint32 }},
		{"fields", func(b *packedBuffer) { b.fields.n = math.MaxUint32 }},
		{"terms", func(b *packedBuffer) { b.terms.n = math.MaxUint32 }},
		{"locations", func(b *packedBuffer) { b.locs.n = math.MaxUint32 }},
		{"stored bytes", func(b *packedBuffer) { b.nBytes = maxBytes - 1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newPackedBuffer()
			tt.fill(b)
			if err := b.add(covStubDoc(true)); !errors.Is(err, errPackedRange) {
				t.Fatalf("got %v", err)
			}
		})
	}
	t.Run("term bytes", func(t *testing.T) {
		b := newPackedBuffer()
		b.nBytes = maxBytes - 1
		if err := b.add(covStubDoc(false)); !errors.Is(err, errPackedRange) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestPackedBufferValueStartsNextChunk(t *testing.T) {
	b := newPackedBuffer()
	if _, err := b.appendBytes([]byte("x")); err != nil {
		t.Fatal(err)
	}
	b.nBytes = byteChunk - 2
	v := []byte("0123456789")
	s, err := b.appendBytes(v)
	if err != nil {
		t.Fatal(err)
	}
	if s.off != byteChunk || len(b.bytes) != 2 || !bytes.Equal(b.slice(s), v) {
		t.Fatalf("span %+v chunks %d", s, len(b.bytes))
	}
}

func TestPackedDocViewAccessors(t *testing.T) {
	b := newPackedBuffer()
	if err := b.add(covStubDoc(false)); err != nil {
		t.Fatal(err)
	}
	doc := b.documents()[0]
	doc.Analyze()
	doc.EachField(func(f segment.Field) {
		if f.Value() != nil {
			t.Errorf("unstored value %q", f.Value())
		}
		f.EachTerm(func(term segment.FieldTerm) {
			term.EachLocation(func(l segment.Location) {
				if l.Size() <= 0 {
					t.Errorf("location size %d", l.Size())
				}
			})
		})
	})
}
