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
	"slices"
	"testing"

	"github.com/RoaringBitmap/roaring/v2"
	segment "github.com/vcaesar/bluge_segment_api"
)

// fakeSegPostings is a segment postings iterator with an actual bitmap or
// a 1-hit doc, recording ReplaceActual.
type fakeSegPostings struct {
	bm       *roaring.Bitmap
	oneHit   bool
	doc      uint64
	replaced *roaring.Bitmap
}

func (f *fakeSegPostings) Next() (segment.Posting, error)          { return nil, nil }
func (f *fakeSegPostings) Advance(uint64) (segment.Posting, error) { return nil, nil }
func (f *fakeSegPostings) Size() int                               { return 0 }
func (f *fakeSegPostings) Empty() bool                             { return false }
func (f *fakeSegPostings) Count() uint64                           { return 0 }
func (f *fakeSegPostings) Close() error                            { return nil }
func (f *fakeSegPostings) ActualBitmap() *roaring.Bitmap           { return f.bm }
func (f *fakeSegPostings) DocNum1Hit() (uint64, bool)              { return f.doc, f.oneHit }
func (f *fakeSegPostings) ReplaceActual(bm *roaring.Bitmap)        { f.replaced = bm }

// plainIterator hides the optimizable methods of its postings.
type plainIterator struct {
	segment.PostingsIterator
}

func bmOf(docs ...uint32) *fakeSegPostings { return &fakeSegPostings{bm: roaring.BitmapOf(docs...)} }
func hitOf(doc uint64) *fakeSegPostings    { return &fakeSegPostings{oneHit: true, doc: doc} }

func TestOptimizeExclusionFinish(t *testing.T) {
	for _, tc := range []struct {
		name     string
		includes [][]segment.PostingsIterator // [term][segment]
		excludes [][]segment.PostingsIterator
		excluded bool
		want     []*roaring.Bitmap // per segment, nil: untouched
	}{
		{
			name:     "and then andnot, shared per segment",
			includes: [][]segment.PostingsIterator{{bmOf(1, 2, 3, 4), bmOf(7)}, {bmOf(2, 3, 4, 9), bmOf(7, 8)}},
			excludes: [][]segment.PostingsIterator{{bmOf(3), bmOf(1)}, {hitOf(4), anEmptyPostingsIterator}},
			excluded: true,
			want:     []*roaring.Bitmap{roaring.BitmapOf(2), nil},
		},
		{
			name:     "1-hit include not excluded",
			includes: [][]segment.PostingsIterator{{hitOf(5)}, {bmOf(5, 6)}},
			excludes: [][]segment.PostingsIterator{{bmOf(6)}},
			excluded: true,
			want:     []*roaring.Bitmap{nil},
		},
		{
			name:     "1-hit include outside the other include",
			includes: [][]segment.PostingsIterator{{bmOf(1, 2), hitOf(5)}, {bmOf(1, 2), bmOf(5)}},
			excludes: [][]segment.PostingsIterator{{bmOf(1), bmOf(6)}},
			excluded: true,
			want:     []*roaring.Bitmap{roaring.BitmapOf(2), nil},
		},
		{
			name:     "1-hit include excluded empties the bitmap includes",
			includes: [][]segment.PostingsIterator{{hitOf(5)}, {bmOf(4, 5)}},
			excludes: [][]segment.PostingsIterator{{hitOf(5)}},
			excluded: true,
			want:     []*roaring.Bitmap{roaring.New()},
		},
		{
			name:     "only a 1-hit include, excluded, falls back",
			includes: [][]segment.PostingsIterator{{bmOf(1, 2), hitOf(5)}},
			excludes: [][]segment.PostingsIterator{{bmOf(1), bmOf(5)}},
			want:     []*roaring.Bitmap{nil, nil},
		},
		{
			name:     "1-hit excludes and several bitmaps",
			includes: [][]segment.PostingsIterator{{bmOf(1, 2, 3, 4, 5)}, {bmOf(2, 3, 4, 5)}, {bmOf(1, 3, 4, 5)}},
			excludes: [][]segment.PostingsIterator{{hitOf(4)}, {bmOf(9)}, {bmOf(5, 9)}},
			excluded: true,
			want:     []*roaring.Bitmap{roaring.BitmapOf(3)},
		},
		{
			name:     "unreadable exclude falls back",
			includes: [][]segment.PostingsIterator{{bmOf(1, 2)}},
			excludes: [][]segment.PostingsIterator{{&plainIterator{bmOf(1)}}},
			want:     []*roaring.Bitmap{nil},
		},
		{
			name:     "empty include segment",
			includes: [][]segment.PostingsIterator{{anEmptyPostingsIterator}, {bmOf(1)}},
			excludes: [][]segment.PostingsIterator{{bmOf(1)}},
			excluded: true,
			want:     []*roaring.Bitmap{nil},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snap := &Snapshot{segment: make([]*segmentSnapshot, len(tc.want))}
			o := &optimizeExclusion{snapshot: snap}
			for _, its := range tc.includes {
				o.includes = append(o.includes, &postingsIterator{snapshot: snap, iterators: its})
			}
			for _, its := range tc.excludes {
				o.excludes = append(o.excludes, &postingsIterator{snapshot: snap, iterators: its})
			}
			// segment bitmaps belong to the index: Finish must copy, not modify
			var orig, copies []*roaring.Bitmap
			for _, its := range append(slices.Clone(tc.includes), tc.excludes...) {
				for _, it := range its {
					if f, ok := it.(*fakeSegPostings); ok && f.bm != nil {
						orig, copies = append(orig, f.bm), append(copies, f.bm.Clone())
					}
				}
			}
			if _, err := o.Finish(); err != nil {
				t.Fatal(err)
			}
			for k, bm := range orig {
				if !bm.Equals(copies[k]) {
					t.Fatalf("segment bitmap modified: %v, was %v", bm, copies[k])
				}
			}
			if o.Excluded() != tc.excluded {
				t.Fatalf("excluded %v, want %v", o.Excluded(), tc.excluded)
			}
			for seg, want := range tc.want {
				var shared *roaring.Bitmap
				for term, tfr := range o.includes {
					f, ok := tfr.iterators[seg].(*fakeSegPostings)
					if !ok || f.oneHit {
						if ok && f.replaced != nil {
							t.Fatalf("segment %d term %d: 1-hit iterator replaced", seg, term)
						}
						continue
					}
					switch {
					case want == nil && f.replaced != nil:
						t.Fatalf("segment %d term %d replaced with %v", seg, term, f.replaced)
					case want != nil && (f.replaced == nil || !f.replaced.Equals(want)):
						t.Fatalf("segment %d term %d replaced with %v, want %v", seg, term, f.replaced, want)
					case want != nil && shared != nil && f.replaced != shared:
						t.Fatalf("segment %d: terms do not share the replaced bitmap", seg)
					}
					shared = f.replaced
				}
			}
		})
	}
}
