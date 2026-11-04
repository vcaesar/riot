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

import "fmt"

// numericColumnVisitor is implemented by segments with typed numeric
// columns (ice); ok is false when field has no column in the segment.
type numericColumnVisitor interface {
	VisitNumericColumn(field string, visitor func(docNum uint64, value int64) error) (ok bool, err error)
}

// VisitNumericColumn calls visitor with the global doc number and value of
// every live document holding field, scanning each segment's numeric column
// directly. It fails when a segment holding field has no numeric column.
func (i *Snapshot) VisitNumericColumn(field string, visitor func(docNum uint64, value int64) error) error {
	for j, seg := range i.segment {
		nv, ok := seg.segment.Segment.(numericColumnVisitor)
		if !ok {
			return fmt.Errorf("segment %d cannot scan numeric columns", j)
		}
		base, deleted := i.offsets[j], seg.deleted
		found, err := nv.VisitNumericColumn(field, func(doc uint64, v int64) error {
			// #nosec G115 -- local doc numbers fit the segment's uint32 doc space.
			if deleted != nil && deleted.Contains(uint32(doc)) {
				return nil
			}
			return visitor(base+doc, v)
		})
		if err != nil {
			return err
		}
		if !found && hasField(seg.segment.Fields(), field) {
			return fmt.Errorf("field %s has no numeric column in segment %d", field, j)
		}
	}
	return nil
}

// numericBlockVisitor is implemented by segments that hand out their
// numeric column a block at a time (ice); docs is nil for dense blocks.
type numericBlockVisitor interface {
	VisitNumericBlocks(field string, visitor func(base uint64, docs []uint64, vals []int64) error) (ok bool, err error)
}

// VisitNumericBlocks is VisitNumericColumn a block at a time: docs holds
// the global doc numbers of vals, or is nil when they are base, base+1, ...
// Both slices are reused between calls.
func (i *Snapshot) VisitNumericBlocks(field string, visitor func(base uint64, docs []uint64, vals []int64) error) error {
	var docs []uint64
	var live []int64
	for j, seg := range i.segment {
		nv, ok := seg.segment.Segment.(numericBlockVisitor)
		if !ok {
			return fmt.Errorf("segment %d cannot scan numeric columns", j)
		}
		base, deleted := i.offsets[j], seg.deleted
		found, err := nv.VisitNumericBlocks(field, func(start uint64, local []uint64, vals []int64) error {
			lo, hi := start, start+uint64(len(vals))
			if local != nil {
				lo, hi = local[0], local[len(local)-1]+1
			}
			if deleted == nil || !deleted.IntersectsWithInterval(lo, hi) {
				if local == nil {
					return visitor(base+start, nil, vals)
				}
				docs = docs[:0]
				for _, doc := range local {
					docs = append(docs, base+doc)
				}
				return visitor(base, docs, vals)
			}
			docs, live = docs[:0], live[:0]
			for k, v := range vals {
				doc := start + uint64(k)
				if local != nil {
					doc = local[k]
				}
				// #nosec G115 -- local doc numbers fit the segment's uint32 doc space.
				if deleted.Contains(uint32(doc)) {
					continue
				}
				docs = append(docs, base+doc)
				live = append(live, v)
			}
			if len(live) == 0 {
				return nil
			}
			return visitor(base, docs, live)
		})
		if err != nil {
			return err
		}
		if !found && hasField(seg.segment.Fields(), field) {
			return fmt.Errorf("field %s has no numeric column in segment %d", field, j)
		}
	}
	return nil
}

func hasField(fields []string, field string) bool {
	for _, f := range fields {
		if f == field {
			return true
		}
	}
	return false
}
