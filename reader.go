//  Copyright (c) 2020 The Bluge Authors.
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
	"context"
	"fmt"
	"slices"

	"github.com/vcaesar/riot/index"
	"github.com/vcaesar/riot/numeric"

	segment "github.com/vcaesar/bluge_segment_api"

	"github.com/vcaesar/riot/search"
)

type Reader struct {
	config Config
	reader *index.Snapshot
}

func OpenReader(config Config) (*Reader, error) {
	rv := &Reader{
		config: config,
	}
	var err error
	rv.reader, err = index.OpenReader(config.indexConfig)
	if err != nil {
		return nil, fmt.Errorf("error opening index: %w", err)
	}

	return rv, nil
}

func (r *Reader) Count() (count uint64, err error) {
	return r.reader.Count()
}

func (r *Reader) Fields() (fields []string, err error) {
	return r.reader.Fields()
}

type StoredFieldVisitor func(field string, value []byte) bool

func (r *Reader) VisitStoredFields(number uint64, visitor StoredFieldVisitor) error {
	return r.reader.VisitStoredFields(number, segment.StoredFieldVisitor(visitor))
}

func (r *Reader) Search(ctx context.Context, req SearchRequest) (search.DocumentMatchIterator, error) {
	collector := req.Collector()
	searcher, err := req.Searcher(r.reader, r.config)
	if err != nil {
		return nil, err
	}

	// sizing walks every searcher, so only do it for the hooks that read it
	if r.config.SearchStartFunc != nil || r.config.SearchEndFunc != nil {
		memNeeded := memNeededForSearch(searcher, collector)
		if r.config.SearchStartFunc != nil {
			err = r.config.SearchStartFunc(memNeeded)
		}
		if err != nil {
			_ = searcher.Close()
			return nil, err
		}
		if r.config.SearchEndFunc != nil {
			defer r.config.SearchEndFunc(memNeeded)
		}
	}

	var dmItr search.DocumentMatchIterator
	dmItr, err = collector.Collect(ctx, req.Aggregations(), searcher)
	if err != nil {
		return nil, err
	}

	// FIXME search stats on reader?

	return dmItr, nil
}

func (r *Reader) DictionaryIterator(field string, automaton segment.Automaton, start, end []byte) (segment.DictionaryIterator, error) {
	return r.reader.DictionaryIterator(field, automaton, start, end)
}

func (r *Reader) Backup(path string, cancel chan struct{}) error {
	dir := index.NewFileSystemDirectory(path)
	return r.reader.Backup(dir, cancel)
}

func (r *Reader) Close() error {
	return r.reader.Close()
}

// VisitNumbers calls visitor with the doc number and value of every live
// document holding the numeric field, scanning the typed numeric columns
// without building per-hit matches. Values are float64 for numeric fields;
// date fields pass UnixNano as float64 bits (use VisitInt64 for exact dates).
func (r *Reader) VisitNumbers(field string, visitor func(docNum uint64, value float64) error) error {
	return r.reader.VisitNumericColumn(field, func(doc uint64, v int64) error {
		return visitor(doc, numeric.Int64ToFloat64(v))
	})
}

// VisitInt64 is VisitNumbers with the raw column value, e.g. UnixNano for
// date fields.
func (r *Reader) VisitInt64(field string, visitor func(docNum uint64, value int64) error) error {
	return r.reader.VisitNumericColumn(field, visitor)
}

// VisitNumberBlocks is VisitNumbers a block (up to 1024 values) at a time,
// for column scans such as stats. docs holds the doc numbers of values, or
// is nil when they are base, base+1, ...; both slices are reused between
// calls, so the visitor must not retain them.
func (r *Reader) VisitNumberBlocks(field string, visitor func(base uint64, docs []uint64, values []float64) error) error {
	var floats []float64
	return r.reader.VisitNumericBlocks(field, func(base uint64, docs []uint64, vals []int64) error {
		floats = slices.Grow(floats[:0], len(vals))[:len(vals)]
		for i, v := range vals {
			floats[i] = numeric.Int64ToFloat64(v)
		}
		return visitor(base, docs, floats)
	})
}

// VisitInt64Blocks is VisitNumberBlocks with the raw column values.
func (r *Reader) VisitInt64Blocks(field string, visitor func(base uint64, docs []uint64, values []int64) error) error {
	return r.reader.VisitNumericBlocks(field, visitor)
}
