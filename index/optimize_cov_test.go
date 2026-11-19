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
	"reflect"
	"testing"

	"github.com/RoaringBitmap/roaring/v2"
	segment "github.com/vcaesar/bluge_segment_api"
)

// covFakeSnapshot is a snapshot of n fake segments numbered from 0, 10, 20...
func covFakeSnapshot(n int, cfg Config) *Snapshot {
	s := &Snapshot{parent: &Writer{config: cfg}, segment: make([]*segmentSnapshot, n)}
	for i := range n {
		s.offsets = append(s.offsets, uint64(10*i))
	}
	return s
}

// covTFRs turns [term][segment] postings into postings iterators of snap.
func covTFRs(snap *Snapshot, its [][]segment.PostingsIterator) []*postingsIterator {
	rv := make([]*postingsIterator, 0, len(its))
	for _, it := range its {
		rv = append(rv, &postingsIterator{snapshot: snap, iterators: it})
	}
	return rv
}

func TestOptimizeDispatch(t *testing.T) {
	on := Config{OptimizeConjunction: true, OptimizeConjunctionUnadorned: true, OptimizeDisjunctionUnadorned: true}
	tests := []struct {
		kind string
		cfg  Config
		want any
	}{
		{"conjunction", on, &optimizeConjunction{}},
		{optimizeExclusionInclude, on, &optimizeExclusion{}},
		{optimizeExclusionExclude, on, &optimizeExclusion{}},
		{"conjunction:unadorned", on, &optimizeConjunctionUnadorned{}},
		{"disjunction:unadorned", on, &optimizeDisjunctionUnadorned{}},
		{"conjunction", Config{}, nil},
		{"conjunction:unadorned", Config{}, nil},
		{"disjunction:unadorned", Config{}, nil},
		{"unknown", on, nil},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			snap := covFakeSnapshot(1, tt.cfg)
			it := &postingsIterator{snapshot: snap}
			octx, err := it.Optimize(tt.kind, nil)
			if err != nil {
				t.Fatalf("optimize: %v", err)
			}
			if tt.want == nil {
				if octx != nil {
					t.Fatalf("expected no context, got %T", octx)
				}
				return
			}
			if reflect.TypeOf(octx) != reflect.TypeOf(tt.want) {
				t.Fatalf("got %T, want %T", octx, tt.want)
			}
			// a second iterator joins the same context
			again, err := (&postingsIterator{snapshot: snap}).Optimize(tt.kind, octx)
			if err != nil || again != octx {
				t.Fatalf("second optimize: %v, %v", again, err)
			}
		})
	}
}

type covOtherCtx struct{}

func (covOtherCtx) Finish() (segment.PostingsIterator, error) { return nil, nil }

func TestOptimizeContextMismatch(t *testing.T) {
	snap := covFakeSnapshot(1, Config{})
	other := covFakeSnapshot(1, Config{})
	it := &postingsIterator{snapshot: snap}
	tests := []struct {
		name     string
		optimize func(segment.OptimizableContext) (segment.OptimizableContext, error)
		foreign  segment.OptimizableContext
		keepType bool
	}{
		{"conjunction", it.optimizeConjunction, &optimizeConjunction{snapshot: other}, true},
		{"exclusion", func(o segment.OptimizableContext) (segment.OptimizableContext, error) {
			return it.optimizeExclusion(false, o)
		}, &optimizeExclusion{snapshot: other}, false},
		{"conjunction unadorned", it.optimizeConjunctionUnadorned, &optimizeConjunctionUnadorned{snapshot: other}, false},
		{"disjunction unadorned", it.optimizeDisjunctionUnadorned, &optimizeDisjunctionUnadorned{snapshot: other}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.optimize(tt.foreign); err == nil {
				t.Fatalf("expected error across snapshots")
			}
			got, err := tt.optimize(covOtherCtx{})
			if err != nil {
				t.Fatalf("foreign context: %v", err)
			}
			if tt.keepType != (got != nil) {
				t.Fatalf("foreign context returned %v", got)
			}
		})
	}
}

func TestOptimizeConjunctionFinish(t *testing.T) {
	snap := covFakeSnapshot(5, Config{})
	plain := func(docs ...uint32) segment.PostingsIterator { return &plainIterator{bmOf(docs...)} }
	nilBM := func() segment.PostingsIterator { return &fakeSegPostings{} }
	its := [][]segment.PostingsIterator{
		{bmOf(1, 2, 3), plain(1), nilBM(), bmOf(1), bmOf(1)},
		{bmOf(2, 3), bmOf(1), bmOf(1), plain(1), nilBM()},
		{bmOf(3, 4), bmOf(1), bmOf(1), bmOf(1), bmOf(1)},
		{plain(3), bmOf(1), bmOf(1), bmOf(1), bmOf(1)},
		{nilBM(), bmOf(1), bmOf(1), bmOf(1), bmOf(1)},
	}
	o := &optimizeConjunction{snapshot: snap}
	if rv, err := o.Finish(); rv != nil || err != nil {
		t.Fatalf("empty finish: %v, %v", rv, err)
	}
	o.tfrs = covTFRs(snap, its)
	if rv, err := o.Finish(); rv != nil || err != nil {
		t.Fatalf("finish: %v, %v", rv, err)
	}
	want := roaring.BitmapOf(3)
	for term := range 3 {
		f := its[term][0].(*fakeSegPostings)
		if f.replaced == nil || !f.replaced.Equals(want) {
			t.Fatalf("term %d: replaced %v, want %v", term, f.replaced, want)
		}
	}
	for seg := 1; seg < 5; seg++ {
		for term := range its {
			if f, ok := its[term][seg].(*fakeSegPostings); ok && f.replaced != nil {
				t.Errorf("segment %d term %d replaced", seg, term)
			}
		}
	}
}

func TestOptimizeExclusionFinishBranches(t *testing.T) {
	shared := roaring.BitmapOf(1, 2)
	tests := []struct {
		name     string
		includes [][]segment.PostingsIterator
		excludes [][]segment.PostingsIterator
		excluded bool
		want     *roaring.Bitmap // replaced bitmap of the first include
	}{
		{name: "no excludes", includes: [][]segment.PostingsIterator{{bmOf(1)}}},
		{
			name:     "unreadable include",
			includes: [][]segment.PostingsIterator{{&plainIterator{bmOf(1)}}},
			excludes: [][]segment.PostingsIterator{{bmOf(1)}},
		},
		{
			name:     "1-hit includes disagree",
			includes: [][]segment.PostingsIterator{{hitOf(1)}, {hitOf(2)}, {bmOf(1, 2)}},
			excludes: [][]segment.PostingsIterator{{bmOf(1)}},
			excluded: true,
		},
		{
			name:     "1-hit outside the bitmap",
			includes: [][]segment.PostingsIterator{{bmOf(4)}, {hitOf(5)}},
			excludes: [][]segment.PostingsIterator{{bmOf(4)}},
			excluded: true,
		},
		{
			name:     "shared bitmap untouched",
			includes: [][]segment.PostingsIterator{{&fakeSegPostings{bm: shared}}, {&fakeSegPostings{bm: shared}}},
			excludes: [][]segment.PostingsIterator{{bmOf(9)}},
			excluded: true,
		},
		{
			name:     "1-hit excludes copy once",
			includes: [][]segment.PostingsIterator{{bmOf(1, 2)}},
			excludes: [][]segment.PostingsIterator{{hitOf(7)}, {hitOf(2)}},
			excluded: true,
			want:     roaring.BitmapOf(1),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := covFakeSnapshot(1, Config{})
			o := &optimizeExclusion{snapshot: snap, includes: covTFRs(snap, tt.includes), excludes: covTFRs(snap, tt.excludes)}
			if _, err := o.Finish(); err != nil {
				t.Fatal(err)
			}
			if o.Excluded() != tt.excluded {
				t.Fatalf("excluded %v, want %v", o.Excluded(), tt.excluded)
			}
			f, _ := tt.includes[0][0].(*fakeSegPostings)
			if f == nil {
				return
			}
			switch {
			case tt.want == nil && f.replaced != nil:
				t.Fatalf("replaced with %v", f.replaced)
			case tt.want != nil && (f.replaced == nil || !f.replaced.Equals(tt.want)):
				t.Fatalf("replaced with %v, want %v", f.replaced, tt.want)
			}
			if !shared.Equals(roaring.BitmapOf(1, 2)) {
				t.Fatalf("shared bitmap modified: %v", shared)
			}
		})
	}
}

func TestOptimizeConjunctionUnadornedFinish(t *testing.T) {
	nilBM := &fakeSegPostings{}
	t.Run("segments", func(t *testing.T) {
		its := [][]segment.PostingsIterator{
			{anEmptyPostingsIterator, hitOf(1), hitOf(1), hitOf(1), hitOf(1), bmOf(1, 2, 3)},
			{bmOf(1), hitOf(2), nilBM, bmOf(2), bmOf(1, 2), bmOf(2, 3)},
			{bmOf(1), bmOf(1), bmOf(1), bmOf(1), bmOf(1, 3), bmOf(3, 4)},
		}
		snap := covFakeSnapshot(len(its[0]), Config{})
		o := &optimizeConjunctionUnadorned{snapshot: snap, tfrs: covTFRs(snap, its)}
		rv, err := o.Finish()
		if err != nil {
			t.Fatal(err)
		}
		if got, want := covDrain(t, rv), []uint64{41, 53}; !covEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		if snap.parent.stats.TotTermSearchersStarted != 1 {
			t.Fatalf("searchers started %d", snap.parent.stats.TotTermSearchersStarted)
		}
	})
	t.Run("not optimizable", func(t *testing.T) {
		snap := covFakeSnapshot(1, Config{})
		o := &optimizeConjunctionUnadorned{snapshot: snap, tfrs: covTFRs(snap, [][]segment.PostingsIterator{
			{&plainIterator{bmOf(1)}}, {bmOf(1)},
		})}
		if rv, err := o.Finish(); rv != nil || err != nil {
			t.Fatalf("got %v, %v", rv, err)
		}
	})
	t.Run("single", func(t *testing.T) {
		snap := covFakeSnapshot(1, Config{})
		o := &optimizeConjunctionUnadorned{snapshot: snap, tfrs: covTFRs(snap, [][]segment.PostingsIterator{{bmOf(1)}})}
		if rv, err := o.Finish(); rv != nil || err != nil {
			t.Fatalf("got %v, %v", rv, err)
		}
	})
}

func TestOptimizeDisjunctionUnadornedFinish(t *testing.T) {
	nilBM := &fakeSegPostings{}
	t.Run("segments", func(t *testing.T) {
		its := [][]segment.PostingsIterator{
			{bmOf(1), bmOf(1), bmOf(1), hitOf(1)},
			{bmOf(2), bmOf(2), hitOf(4), hitOf(2)},
			{bmOf(3), hitOf(5), nilBM, nilBM},
		}
		snap := covFakeSnapshot(4, Config{})
		o := &optimizeDisjunctionUnadorned{snapshot: snap, tfrs: covTFRs(snap, its)}
		rv, err := o.Finish()
		if err != nil {
			t.Fatal(err)
		}
		if got, want := covDrain(t, rv), []uint64{1, 2, 3, 11, 12, 15, 21, 24, 31, 32}; !covEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
	t.Run("not optimizable", func(t *testing.T) {
		snap := covFakeSnapshot(1, Config{})
		o := &optimizeDisjunctionUnadorned{snapshot: snap, tfrs: covTFRs(snap, [][]segment.PostingsIterator{
			{bmOf(1)}, {&plainIterator{bmOf(1)}},
		})}
		if rv, err := o.Finish(); rv != nil || err != nil {
			t.Fatalf("got %v, %v", rv, err)
		}
	})
	t.Run("single", func(t *testing.T) {
		snap := covFakeSnapshot(1, Config{})
		o := &optimizeDisjunctionUnadorned{snapshot: snap, tfrs: covTFRs(snap, [][]segment.PostingsIterator{{bmOf(1)}})}
		if rv, err := o.Finish(); rv != nil || err != nil {
			t.Fatalf("got %v, %v", rv, err)
		}
	})
}

func TestDocsMatchingTerms(t *testing.T) {
	cfg := covConfig()
	seg := covSegment(t)
	t.Run("live docs", func(t *testing.T) {
		snap := covSnapshot(cfg,
			&segmentSnapshot{segment: seg},
			&segmentSnapshot{segment: seg, deleted: roaring.BitmapOf(0)})
		rv, err := snap.DocsMatchingTerms("f", [][]byte{[]byte("b"), []byte("d"), []byte("zz")})
		if err != nil {
			t.Fatal(err)
		}
		// b {0,2} d {1}; segment 2 deleted its doc 0
		if got, want := covDrain(t, rv), []uint64{0, 1, 2, 5, 6}; !covEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
	t.Run("disabled", func(t *testing.T) {
		snap := covSnapshot(cfg.DisableOptimizeDisjunctionUnadorned(), &segmentSnapshot{segment: seg})
		if rv, err := snap.DocsMatchingTerms("f", [][]byte{[]byte("a")}); rv != nil || err != nil {
			t.Fatalf("got %v, %v", rv, err)
		}
	})
	t.Run("1-hit", func(t *testing.T) {
		snap := covSnapshot(cfg, &segmentSnapshot{segment: covWrap(&covDictSegment{Segment: seg.Segment, dict: &covDict{itr: hitOf(3)}})})
		rv, err := snap.DocsMatchingTerms("f", [][]byte{[]byte("x")})
		if err != nil {
			t.Fatal(err)
		}
		if got := covDrain(t, rv); !covEqual(got, []uint64{3}) {
			t.Fatalf("got %v", got)
		}
	})
	hugeHit := &fakeSegPostings{oneHit: true, doc: 1 << 40}
	tests := []struct {
		name    string
		seg     segment.Segment
		wantErr bool
	}{
		{"dictionary error", &covDictSegment{Segment: seg.Segment, err: errCov}, true},
		{"postings list error", &covDictSegment{Segment: seg.Segment, dict: &covDict{plErr: errCov}}, true},
		{"iterator error", &covDictSegment{Segment: seg.Segment, dict: &covDict{itrErr: errCov}}, true},
		{"not optimizable", &covDictSegment{Segment: seg.Segment, dict: &covDict{itr: &plainIterator{bmOf(1)}}}, false},
		{"1-hit out of range", &covDictSegment{Segment: seg.Segment, dict: &covDict{itr: hugeHit}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := covSnapshot(cfg, &segmentSnapshot{segment: &segmentWrapper{Segment: tt.seg, refCounter: noOpRefCounter{}}})
			rv, err := snap.DocsMatchingTerms("f", [][]byte{[]byte("a")})
			if rv != nil || (err != nil) != tt.wantErr {
				t.Fatalf("got %v, %v", rv, err)
			}
		})
	}
}

func TestDocsMatchingRange(t *testing.T) {
	cfg := covConfig()
	seg := covSegment(t)
	t.Run("live docs", func(t *testing.T) {
		snap := covSnapshot(cfg,
			&segmentSnapshot{segment: seg},
			&segmentSnapshot{segment: seg, deleted: roaring.BitmapOf(2)})
		rv, err := snap.DocsMatchingRange("f", nil, []byte("b"), []byte("d"))
		if err != nil {
			t.Fatal(err)
		}
		// b {0,2} c {2,3}
		if got, want := covDrain(t, rv), []uint64{0, 2, 3, 4, 7}; !covEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
	t.Run("disabled", func(t *testing.T) {
		snap := covSnapshot(cfg.DisableOptimizeDisjunctionUnadorned(), &segmentSnapshot{segment: seg})
		if rv, err := snap.DocsMatchingRange("f", nil, nil, nil); rv != nil || err != nil {
			t.Fatalf("got %v, %v", rv, err)
		}
	})
	tests := []struct {
		name    string
		seg     segment.Segment
		wantErr bool
	}{
		{"dictionary error", &covDictSegment{Segment: seg.Segment, err: errCov}, true},
		{"no dense or", &covDictSegment{Segment: seg.Segment, dict: &emptyDict{}}, false},
		{"dense or error", &covDictSegment{Segment: seg.Segment, dict: &covDict{orErr: errCov}}, true},
		{"dense or declined", &covDictSegment{Segment: seg.Segment, dict: &covDict{}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := covSnapshot(cfg, &segmentSnapshot{segment: &segmentWrapper{Segment: tt.seg, refCounter: noOpRefCounter{}}})
			rv, err := snap.DocsMatchingRange("f", nil, nil, nil)
			if rv != nil || (err != nil) != tt.wantErr {
				t.Fatalf("got %v, %v", rv, err)
			}
		})
	}
}

func TestTermsIterator(t *testing.T) {
	cfg := covConfig()
	seg := covSegment(t)
	snap := covSnapshot(cfg, &segmentSnapshot{segment: seg})
	itr, err := snap.TermsIterator("f", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var terms []string
	for {
		e, err := itr.Next()
		if err != nil {
			t.Fatal(err)
		}
		if e == nil {
			break
		}
		terms = append(terms, e.Term())
	}
	if !reflect.DeepEqual(terms, []string{"a", "b", "c", "d"}) {
		t.Fatalf("terms %v", terms)
	}
	if err := itr.Close(); err != nil {
		t.Fatal(err)
	}

	// a dictionary without TermsIterator falls back to Iterator
	empty := &closeTrackingItr{}
	if _, err := snapshotWith(&dictEmptySegment{itr: empty}).TermsIterator("f", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if empty.closed != 1 {
		t.Fatalf("fallback iterator closed %d times", empty.closed)
	}
	if _, err := snapshotWith(&dictErrSegment{err: errCov}).TermsIterator("f", nil, nil, nil); !errors.Is(err, errCov) {
		t.Fatalf("got %v", err)
	}
}
