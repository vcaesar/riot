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
	"errors"
	"strconv"
	"testing"

	"github.com/RoaringBitmap/roaring/v2"
	segment "github.com/vcaesar/bluge_segment_api"
)

// covNumErrSegment fails every numeric column read.
type covNumErrSegment struct{ segment.Segment }

func (s *covNumErrSegment) NumericRange(string, int64, int64) (*roaring.Bitmap, bool, error) {
	return nil, false, errCov
}

func (s *covNumErrSegment) VisitNumericColumn(string, func(uint64, int64) error) (bool, error) {
	return false, errCov
}

func (s *covNumErrSegment) VisitNumericBlocks(string, func(uint64, []uint64, []int64) error) (bool, error) {
	return false, errCov
}

// covSparseSegment holds n only in docs 0, 2 and 3.
func covSparseSegment(t *testing.T) *segmentWrapper {
	var docs []segment.Document
	for i, v := range []int64{5, 0, 7, 9} {
		doc := covDocument{NewFakeField("_id", strconv.Itoa(i), true, false, false)}
		if v != 0 {
			doc = append(doc, &covNumField{FakeField: NewFakeField("n", strconv.FormatInt(v, 10), false, false, true), v: v})
		}
		docs = append(docs, doc)
	}
	return covSegment(t, docs...)
}

type covNumHit struct {
	doc uint64
	v   int64
}

func TestVisitNumericColumn(t *testing.T) {
	cfg := covConfig()
	seg := covSegment(t)
	visit := func(snap *Snapshot, field string) ([]covNumHit, error) {
		var hits []covNumHit
		err := snap.VisitNumericColumn(field, func(doc uint64, v int64) error {
			hits = append(hits, covNumHit{doc, v})
			return nil
		})
		return hits, err
	}
	t.Run("live docs", func(t *testing.T) {
		snap := covSnapshot(cfg, &segmentSnapshot{segment: seg, deleted: roaring.BitmapOf(1)}, &segmentSnapshot{segment: seg})
		hits, err := visit(snap, "n")
		if err != nil {
			t.Fatal(err)
		}
		want := []covNumHit{{0, 10}, {2, 30}, {3, 40}, {4, 10}, {5, 20}, {6, 30}, {7, 40}}
		if len(hits) != len(want) {
			t.Fatalf("got %v, want %v", hits, want)
		}
		for i := range want {
			if hits[i] != want[i] {
				t.Fatalf("got %v, want %v", hits, want)
			}
		}
		if hits, err := visit(snap, "missing"); err != nil || len(hits) != 0 {
			t.Fatalf("missing field: %v %v", hits, err)
		}
	})
	stop := errors.New("stop")
	tests := []struct {
		name  string
		seg   segment.Segment
		field string
		fn    func(uint64, int64) error
		want  error
	}{
		{"no numeric columns", &covDictSegment{Segment: seg.Segment}, "n", nil, nil},
		{"column error", &covNumErrSegment{Segment: seg.Segment}, "n", nil, errCov},
		{"visitor error", seg.Segment, "n", func(uint64, int64) error { return stop }, stop},
		{"field without column", seg.Segment, "f", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := tt.fn
			if fn == nil {
				fn = func(uint64, int64) error { return nil }
			}
			err := covSnapshot(cfg, &segmentSnapshot{segment: covWrap(tt.seg)}).VisitNumericColumn(tt.field, fn)
			if err == nil || (tt.want != nil && !errors.Is(err, tt.want)) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestVisitNumericBlocks(t *testing.T) {
	cfg := covConfig()
	dense, sparse := covSegment(t), covSparseSegment(t)
	visit := func(snap *Snapshot) ([]covNumHit, error) {
		var hits []covNumHit
		err := snap.VisitNumericBlocks("n", func(base uint64, docs []uint64, vals []int64) error {
			for k, v := range vals {
				doc := base + uint64(k)
				if docs != nil {
					doc = docs[k]
				}
				hits = append(hits, covNumHit{doc, v})
			}
			return nil
		})
		return hits, err
	}
	tests := []struct {
		name string
		segs []*segmentSnapshot
		want []covNumHit
	}{
		{"dense", []*segmentSnapshot{{segment: dense}}, []covNumHit{{0, 10}, {1, 20}, {2, 30}, {3, 40}}},
		{"dense deleted", []*segmentSnapshot{{segment: dense, deleted: roaring.BitmapOf(2)}, {segment: dense}},
			[]covNumHit{{0, 10}, {1, 20}, {3, 40}, {4, 10}, {5, 20}, {6, 30}, {7, 40}}},
		{"sparse", []*segmentSnapshot{{segment: dense}, {segment: sparse}},
			[]covNumHit{{0, 10}, {1, 20}, {2, 30}, {3, 40}, {4, 5}, {6, 7}, {7, 9}}},
		{"sparse deleted", []*segmentSnapshot{{segment: sparse, deleted: roaring.BitmapOf(2)}},
			[]covNumHit{{0, 5}, {3, 9}}},
		{"all deleted", []*segmentSnapshot{{segment: sparse, deleted: roaring.BitmapOf(0, 1, 2, 3)}}, nil},
		{"disjoint deletions", []*segmentSnapshot{{segment: dense, deleted: roaring.BitmapOf(100)}},
			[]covNumHit{{0, 10}, {1, 20}, {2, 30}, {3, 40}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hits, err := visit(covSnapshot(cfg, tt.segs...))
			if err != nil {
				t.Fatal(err)
			}
			if len(hits) != len(tt.want) {
				t.Fatalf("got %v, want %v", hits, tt.want)
			}
			for i := range hits {
				if hits[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", hits, tt.want)
				}
			}
		})
	}
	errTests := []struct {
		name  string
		seg   segment.Segment
		field string
	}{
		{"no numeric columns", &covDictSegment{Segment: dense.Segment}, "n"},
		{"column error", &covNumErrSegment{Segment: dense.Segment}, "n"},
		{"field without column", dense.Segment, "f"},
	}
	for _, tt := range errTests {
		t.Run(tt.name, func(t *testing.T) {
			err := covSnapshot(cfg, &segmentSnapshot{segment: covWrap(tt.seg)}).VisitNumericBlocks(tt.field,
				func(uint64, []uint64, []int64) error { return nil })
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
	if err := covSnapshot(cfg, &segmentSnapshot{segment: dense}).VisitNumericBlocks("missing",
		func(uint64, []uint64, []int64) error { return errCov }); err != nil {
		t.Fatalf("missing field: %v", err)
	}
}

func TestNumericRange(t *testing.T) {
	cfg := covConfig()
	seg := covSegment(t)
	t.Run("column and dictionary", func(t *testing.T) {
		// f has no numeric column, so its terms [b, d) answer: {0,2,3}
		for _, field := range []string{"n", "f"} {
			lo, hi := []byte("15"), []byte("35")
			if field == "f" {
				lo, hi = []byte("b"), []byte("d")
			}
			snap := covSnapshot(cfg, &segmentSnapshot{segment: seg}, &segmentSnapshot{segment: seg, deleted: roaring.BitmapOf(2)})
			rv, err := snap.NumericRange(field, 15, 35, lo, hi)
			if err != nil {
				t.Fatal(err)
			}
			want := []uint64{1, 2, 5}
			if field == "f" {
				want = []uint64{0, 2, 3, 4, 7}
			}
			if got := covDrain(t, rv); !covEqual(got, want) {
				t.Fatalf("%s: got %v, want %v", field, got, want)
			}
		}
	})
	tests := []struct {
		name    string
		seg     segment.Segment
		wantErr bool
	}{
		{"column error", &covNumErrSegment{Segment: seg.Segment}, true},
		{"dictionary error", &covDictSegment{Segment: seg.Segment, err: errCov}, true},
		{"no dense or", &covDictSegment{Segment: seg.Segment, dict: &emptyDict{}}, false},
		{"dense or error", &covDictSegment{Segment: seg.Segment, dict: &covDict{orErr: errCov}}, true},
		{"dense or declined", &covDictSegment{Segment: seg.Segment, dict: &covDict{}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rv, err := covSnapshot(cfg, &segmentSnapshot{segment: covWrap(tt.seg)}).NumericRange("n", 0, 1, nil, nil)
			if rv != nil || (err != nil) != tt.wantErr {
				t.Fatalf("got %v, %v", rv, err)
			}
		})
	}
}
