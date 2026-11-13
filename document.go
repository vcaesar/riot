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

package riot

import (
	"encoding/binary"

	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/riot/index"
)

type Document []Field

// documentInline is how many fields a new Document holds before AddField
// allocates
const documentInline = 4

// documentBlock allocates a Document with room for its first fields
type documentBlock struct {
	doc    Document
	fields [documentInline]Field
}

func NewDocument(id string) *Document {
	blk := &documentBlock{}
	blk.fields[0] = NewKeywordField(_idField, id).StoreValue().Sortable()
	blk.doc = blk.fields[:1]
	return &blk.doc
}

// NewDocumentWithoutID returns a document with no _id field, which saves
// the identifier's term, stored value and doc values in the index. Such a
// document can only be added with Insert: it cannot be updated or deleted
// by identifier, and its matches have no _id stored field.
func NewDocumentWithoutID() *Document {
	return &Document{}
}

func NewDocumentWithIdentifier(id Identifier) *Document {
	return &Document{
		NewKeywordFieldBytes(id.Field(), id.Term()).StoreValue().Sortable(),
	}
}

func (d Document) Size() int {
	sizeInBytes := sizeOfSlice

	for _, entry := range d {
		sizeInBytes += entry.Size()
	}

	return sizeInBytes
}

// ID is an experimental helper method
// to simplify common use cases
// It returns nil for a document without an _id field.
func (d Document) ID() segment.Term {
	if len(d) == 0 || d[0].Name() != _idField {
		return nil
	}
	return Identifier(d[0].Value())
}

func (d *Document) AddField(f Field) *Document {
	*d = append(*d, f)
	return d
}

// SetTimestamp sets optional segment-pruning metadata. Zero means unknown.
// The units are caller-defined and must match Config.WithTimeRange.
func (d *Document) SetTimestamp(v int64) *Document {
	value := make([]byte, 8)
	//nolint:gosec // G115: encode the signed timestamp's complete two's-complement bit pattern.
	binary.BigEndian.PutUint64(value, uint64(v))
	field := NewStoredOnlyField(index.TimestampField, value)
	for j, f := range *d {
		if f.Name() == index.TimestampField {
			(*d)[j] = field
			return d
		}
	}
	return d.AddField(field)
}

func (d Document) Timestamp() int64 {
	for _, f := range d {
		if f.Name() == index.TimestampField && len(f.Value()) == 8 {
			//nolint:gosec // G115: recover the signed timestamp from its stored two's-complement bits.
			return int64(binary.BigEndian.Uint64(f.Value()))
		}
	}
	return 0
}

// FieldConsumer is anything which can consume a field
// Fields can implement this interface to consume the
// content of another field.
type FieldConsumer interface {
	Consume(Field)
}

func (d Document) Analyze() {
	// composite fields are few; find them once instead of per analyzed field
	var consumers []Field
	for _, field := range d {
		if _, ok := field.(FieldConsumer); ok {
			consumers = append(consumers, field)
		}
	}

	fieldOffsets := map[string]int{}
	for _, field := range d {
		if !field.Index() {
			continue
		}
		fieldOffset := fieldOffsets[field.Name()]
		if fieldOffset > 0 {
			fieldOffset += field.PositionIncrementGap()
		}
		lastPos := field.Analyze(fieldOffset)
		fieldOffsets[field.Name()] = lastPos

		// see if any of the composite fields need this
		for _, consumer := range consumers {
			if consumer == field {
				// never include yourself
				continue
			}
			consumer.(FieldConsumer).Consume(field)
		}
	}
}

// ReleaseAnalysis drops the fields' analysis once it has been indexed, so
// a document still referenced by a Batch does not keep it alive. Analyze
// rebuilds it.
func (d Document) ReleaseAnalysis() {
	for _, field := range d {
		if r, ok := field.(interface{ ReleaseAnalysis() }); ok {
			r.ReleaseAnalysis()
		}
	}
}

func (d Document) EachField(vf segment.VisitField) {
	for _, field := range d {
		vf(field)
	}
}
