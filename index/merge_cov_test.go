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
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring/v2"
	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/riot/index/mergeplan"
)

// covManual is a Writer without its background goroutines, over a fault
// injecting in-memory directory.
func covManual(t *testing.T) (*Writer, *covFaultDir) {
	t.Helper()
	cfg := covConfig()
	dir := &covFaultDir{Directory: NewInMemoryDirectory()}
	w := &Writer{config: cfg, directory: dir, closeCh: make(chan struct{}), deletionPolicy: cfg.DeletionPolicyFunc()}
	var err error
	if w.segPlugin, err = loadSegmentPlugin(cfg.supportedSegmentPlugins, cfg.SegmentType, cfg.SegmentVersion); err != nil {
		t.Fatal(err)
	}
	w.root = &Snapshot{parent: w, refs: 1, epoch: 1}
	t.Cleanup(func() { w.replaceRoot(nil, nil, nil) })
	return w, dir
}

// covPersistedSeg persists a segment of docs as id and loads it back.
func covPersistedSeg(t *testing.T, w *Writer, id uint64, deleted *roaring.Bitmap) *segmentSnapshot {
	t.Helper()
	if err := w.directory.Persist(ItemKindSegment, id, covSegment(t).Segment, nil); err != nil {
		t.Fatal(err)
	}
	seg, err := w.loadSegment(id, w.segPlugin)
	if err != nil {
		t.Fatal(err)
	}
	return &segmentSnapshot{id: id, segment: seg, deleted: deleted}
}

// covSetRoot makes segs the root of w.
func covSetRoot(w *Writer, segs ...*segmentSnapshot) {
	root := &Snapshot{parent: w, refs: 1, epoch: 1, segment: segs}
	var running uint64
	for _, s := range segs {
		root.offsets = append(root.offsets, running)
		running += s.segment.Count()
	}
	w.replaceRoot(root, nil, nil)
}

// covServeMerges answers every merge on merges, until the test ends.
func covServeMerges(t *testing.T, merges chan *segmentMerge, skipped bool) {
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		for {
			select {
			case <-done:
				return
			case sm := <-merges:
				// a skipped merge leaves its segment to the merger
				snap := &Snapshot{refs: 1}
				if sm.new != nil && !skipped {
					snap.segment = []*segmentSnapshot{{id: sm.id, segment: sm.new}}
				}
				sm.notifyCh <- &mergeTaskIntroStatus{snapshot: snap, skipped: skipped}
			}
		}
	}()
}

func covMergeTask(segs ...*segmentSnapshot) *mergeplan.MergeTask {
	task := &mergeplan.MergeTask{}
	for _, s := range segs {
		task.Segments = append(task.Segments, s)
	}
	return task
}

func TestExecuteMergeTask(t *testing.T) {
	closeAfterMerge := func(w *Writer, d *covFaultDir) {
		d.afterPersist = func(string, uint64) { close(w.closeCh) }
	}
	tests := []struct {
		name    string
		fault   func(w *Writer, d *covFaultDir)
		empty   bool
		serve   bool
		skipped bool
		want    error // nil: any error
		ok      bool
	}{
		{name: "merged", serve: true, ok: true},
		{name: "skipped", serve: true, skipped: true, ok: true},
		{name: "skipped, remove fails", serve: true, skipped: true, fault: func(_ *Writer, d *covFaultDir) {
			d.remove = covFailKind(ItemKindSegment)
		}},
		{name: "persist fails", fault: func(_ *Writer, d *covFaultDir) { d.persist = covFailKind(ItemKindSegment) }},
		{name: "load fails", fault: func(w *Writer, _ *covFaultDir) {
			w.segPlugin = covFailingLoad(covConfig()).supportedSegmentPlugins[w.segPlugin.Type][w.segPlugin.Version]
		}},
		{name: "closed", fault: closeAfterMerge, want: segment.ErrClosed},
		{name: "closed, remove fails", fault: func(w *Writer, d *covFaultDir) {
			closeAfterMerge(w, d)
			d.remove = covFailKind(ItemKindSegment)
		}},
		{name: "all deleted", empty: true, serve: true, ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, dir := covManual(t)
			var deleted *roaring.Bitmap
			if tt.empty {
				deleted = roaring.BitmapOf(0, 1, 2, 3)
			}
			a, b := covPersistedSeg(t, w, 1, deleted), covPersistedSeg(t, w, 2, deleted)
			covSetRoot(w, a, b)
			w.nextSegmentID = 2
			if tt.fault != nil {
				tt.fault(w, dir)
			}
			merges := make(chan *segmentMerge)
			if tt.serve {
				covServeMerges(t, merges, tt.skipped)
			}
			err := w.executeMergeTask(merges, covMergeTask(a, b))
			switch {
			case tt.ok && err != nil:
				t.Fatalf("got %v", err)
			case !tt.ok && err == nil:
				t.Fatal("expected error")
			case tt.want != nil && !errors.Is(err, tt.want):
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
	t.Run("empty task", func(t *testing.T) {
		w, _ := covManual(t)
		if err := w.executeMergeTask(nil, &mergeplan.MergeTask{}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestMergeSegmentBases(t *testing.T) {
	closeAfterMerge := func(w *Writer, d *covFaultDir) {
		d.afterPersist = func(string, uint64) { close(w.closeCh) }
	}
	tests := []struct {
		name    string
		fault   func(w *Writer, d *covFaultDir)
		serve   bool
		skipped bool
		ok      bool
	}{
		{name: "merged", serve: true, ok: true},
		{name: "skipped", serve: true, skipped: true, ok: true},
		{name: "skipped, remove fails", serve: true, skipped: true, fault: func(_ *Writer, d *covFaultDir) {
			d.remove = covFailKind(ItemKindSegment)
		}},
		{name: "persist fails", fault: func(_ *Writer, d *covFaultDir) { d.persist = covFailKind(ItemKindSegment) }},
		{name: "load fails", fault: func(w *Writer, _ *covFaultDir) {
			w.segPlugin = covFailingLoad(covConfig()).supportedSegmentPlugins[w.segPlugin.Type][w.segPlugin.Version]
		}},
		{name: "closed", fault: closeAfterMerge},
		{name: "closed, remove fails", fault: func(w *Writer, d *covFaultDir) {
			closeAfterMerge(w, d)
			d.remove = covFailKind(ItemKindSegment)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, dir := covManual(t)
			a := &segmentSnapshot{id: 1, segment: covSegment(t)}
			b := &segmentSnapshot{id: 2, segment: covSegment(t), deleted: roaring.BitmapOf(0)}
			covSetRoot(w, a, b)
			w.nextSegmentID = 2
			if tt.fault != nil {
				tt.fault(w, dir)
			}
			merges := make(chan *segmentMerge)
			if tt.serve {
				covServeMerges(t, merges, tt.skipped)
			}
			snap, id, err := w.mergeSegmentBases(merges, w.root,
				[]segment.Segment{a.segment.Segment, b.segment.Segment}, []*roaring.Bitmap{nil, b.deleted}, []int{0, 1})
			if tt.ok != (err == nil) {
				t.Fatalf("got %v", err)
			}
			if snap != nil {
				if id != 3 || len(snap.segment) != 1 || snap.segment[0].segment.Count() != 7 {
					t.Fatalf("merged %d into %v", id, snap.segment)
				}
				_ = snap.Close()
			}
		})
	}
}

func TestProcessSegmentNow(t *testing.T) {
	sm := &segmentMerge{
		old: map[uint64]*segmentSnapshot{
			1: {deleted: roaring.BitmapOf(0)},
			2: {},
		},
		oldNewDocNums: map[uint64][]uint64{1: {10, 11, 12}, 2: {20, 21}},
	}
	deleted := roaring.New()
	if !sm.ProcessSegmentNow(1, &segmentSnapshot{deleted: roaring.BitmapOf(0, 2)}, deleted) {
		t.Fatal("segment 1 is going away")
	}
	if !sm.ProcessSegmentNow(2, &segmentSnapshot{deleted: roaring.BitmapOf(1)}, deleted) {
		t.Fatal("segment 2 is going away")
	}
	if sm.ProcessSegmentNow(3, &segmentSnapshot{}, deleted) {
		t.Fatal("segment 3 is staying")
	}
	if !deleted.Equals(roaring.BitmapOf(12, 21)) || len(sm.old) != 0 {
		t.Fatalf("deleted %v, old %v", deleted, sm.old)
	}
}

func TestMergerLoopRetriesErrors(t *testing.T) {
	w, dir := covManual(t)
	covSetRoot(w, covPersistedSeg(t, w, 1, nil), covPersistedSeg(t, w, 2, nil))
	w.nextSegmentID = 2
	dir.persist = covFailKind(ItemKindSegment)
	failed := make(chan struct{})
	var once atomic.Bool
	w.config.AsyncError = func(error) {
		if once.CompareAndSwap(false, true) {
			close(failed)
		}
	}
	notifier := make(watcherChan, 1)
	w.asyncTasks.Add(1)
	go w.mergerLoop(make(chan *segmentMerge), notifier)
	ew := <-notifier
	close(ew.notifyCh)
	<-failed
	close(w.closeCh)
	w.asyncTasks.Wait()
	if w.stats.TotFileMergeLoopErr == 0 {
		t.Fatal("merge error not counted")
	}
}

func TestPersistSnapshotPaths(t *testing.T) {
	tests := []struct {
		name    string
		fault   func(d *covFaultDir)
		serve   bool
		skipped bool
		ok      bool
	}{
		{name: "merge fails", fault: func(d *covFaultDir) { d.persist = covFailKind(ItemKindSegment) }},
		{name: "merge skipped", serve: true, skipped: true, ok: true},
		{name: "merged, snapshot fails", serve: true, fault: func(d *covFaultDir) {
			d.persist = covFailKind(ItemKindSnapshot)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, dir := covManual(t)
			kept := covPersistedSeg(t, w, 1, nil)
			covSetRoot(w, kept, &segmentSnapshot{id: 2, segment: covSegment(t)}, &segmentSnapshot{id: 3, segment: covSegment(t)})
			w.nextSegmentID = 3
			if tt.fault != nil {
				tt.fault(dir)
			}
			merges, persists := make(chan *segmentMerge), make(chan *persistIntroduction)
			if tt.serve {
				covServeMerges(t, merges, tt.skipped)
			}
			go func() {
				for p := range persists {
					close(p.applied)
				}
			}()
			defer close(persists)
			if err := w.persistSnapshot(merges, persists, w.root); tt.ok != (err == nil) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestPersistSnapshotDirectErrors(t *testing.T) {
	tests := []struct {
		name  string
		fault func(w *Writer, d *covFaultDir)
	}{
		{name: "segment", fault: func(_ *Writer, d *covFaultDir) { d.persist = covFailKind(ItemKindSegment) }},
		{name: "load", fault: func(_ *Writer, d *covFaultDir) {
			d.load = func(_ string, id uint64) error {
				if id == 2 {
					return errCov
				}
				return nil
			}
		}},
		{name: "closed", fault: func(w *Writer, d *covFaultDir) {
			d.afterPersist = func(_ string, id uint64) {
				if id == 2 {
					close(w.closeCh)
				}
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, dir := covManual(t)
			covSetRoot(w, &segmentSnapshot{id: 1, segment: covSegment(t)}, &segmentSnapshot{id: 2, segment: covSegment(t)})
			tt.fault(w, dir)
			if err := w.persistSnapshotDirect(nil, w.root); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestPausePersisterForMergerCatchUp(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	var asyncErrs atomic.Int64
	newWriter := func(t *testing.T) *Writer {
		w, dir := covManual(t)
		dir.stats = func() (uint64, uint64) { return 10, 0 }
		w.config.PersisterNapUnderNumFiles = 5
		w.config.AsyncError = func(error) { asyncErrs.Add(1) }
		w.deletionPolicy = &covPolicy{KeepNLatestDeletionPolicy: NewKeepNLatestDeletionPolicy(1), fail: &fail}
		return w
	}
	t.Run("merger catches up", func(t *testing.T) {
		w := newWriter(t)
		notifier := make(watcherChan, 1)
		ew := &epochWatcher{epoch: 5, notifyCh: make(notificationChan, 1)}
		notifier <- ew
		merged, watchers := w.pausePersisterForMergerCatchUp(notifier, 4, 1, nil)
		if merged != 5 || len(watchers) != 1 || w.stats.TotPersisterSlowMergerResume != 1 {
			t.Fatalf("merged %d watchers %d", merged, len(watchers))
		}
	})
	t.Run("closed", func(t *testing.T) {
		w := newWriter(t)
		close(w.closeCh)
		if merged, _ := w.pausePersisterForMergerCatchUp(make(watcherChan), 4, 1, nil); merged != 1 {
			t.Fatalf("merged %d", merged)
		}
	})
	if asyncErrs.Load() != 2 {
		t.Fatalf("cleanup errors %d", asyncErrs.Load())
	}
}

// covRunPersister runs the persister loop of w until it exits.
func covRunPersister(w *Writer, introducer watcherChan, persists chan *persistIntroduction) {
	w.asyncTasks.Add(1)
	go w.persisterLoop(make(chan *segmentMerge), persists, introducer, make(watcherChan), 0)
	ew := <-introducer
	close(ew.notifyCh)
	w.asyncTasks.Wait()
}

func TestPersisterLoopStopsWhenClosedWhilePersisting(t *testing.T) {
	w, dir := covManual(t)
	covSetRoot(w, &segmentSnapshot{id: 1, segment: covSegment(t)})
	dir.afterPersist = func(string, uint64) { close(w.closeCh) }
	covRunPersister(w, make(watcherChan, 1), make(chan *persistIntroduction))
	if w.stats.LastPersistedEpoch != 0 {
		t.Fatalf("persisted epoch %d", w.stats.LastPersistedEpoch)
	}
}

func TestPersisterLoopStopsWhenClosedAfterPersisting(t *testing.T) {
	w, _ := covManual(t)
	covSetRoot(w, &segmentSnapshot{id: 1, segment: covSegment(t)})
	introducer := make(watcherChan, 1)
	persists := make(chan *persistIntroduction)
	go func() {
		p := <-persists
		// the persister will find its next watch request blocked
		introducer <- &epochWatcher{notifyCh: make(notificationChan, 1)}
		close(w.closeCh)
		close(p.applied)
	}()
	covRunPersister(w, introducer, persists)
	if w.stats.LastPersistedEpoch != 1 {
		t.Fatalf("persisted epoch %d", w.stats.LastPersistedEpoch)
	}
}

func TestPersisterRetriesWithCallbacks(t *testing.T) {
	var fails atomic.Int64
	cfg := covMemDirConfig(func(d *covFaultDir) {
		d.persist = func(kind string, _ uint64) error {
			if kind == ItemKindSnapshot && fails.Add(1) == 1 {
				return errCov
			}
			return nil
		}
	})
	var asyncErrs atomic.Int64
	cfg.AsyncError = func(error) { asyncErrs.Add(1) }
	w := covWriter(t, cfg)
	persisted := make(chan error, 1)
	b := covBatchOf(covDocsN(1))
	b.SetPersistedCallback(func(err error) { persisted <- err })
	if err := w.Batch(b); !errors.Is(err, errCov) {
		t.Fatalf("got %v", err)
	}
	select {
	case err := <-persisted:
		if err != nil {
			t.Fatalf("callback got %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("persister never retried")
	}
	if asyncErrs.Load() == 0 {
		t.Fatal("persist error not reported")
	}
}

func TestIntroduceSegmentDeletions(t *testing.T) {
	tests := []struct {
		name  string
		delta *roaring.Bitmap
		want  *roaring.Bitmap
	}{
		{"no new deletions", roaring.New(), roaring.BitmapOf(0)},
		{"new deletions", roaring.BitmapOf(2), roaring.BitmapOf(0, 2)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, _ := covManual(t)
			covSetRoot(w, &segmentSnapshot{id: 1, segment: covSegment(t), deleted: roaring.BitmapOf(0)})
			intro := &segmentIntroduction{obsoletes: map[uint64]*roaring.Bitmap{1: tt.delta}, applied: make(chan error)}
			if err := w.introduceSegment(intro, 2); err != nil {
				t.Fatal(err)
			}
			if got := w.root.segment[0].deleted; !got.Equals(tt.want) {
				t.Fatalf("deleted %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIntroducerLoopSkipsFailedIntroductions(t *testing.T) {
	w, _ := covManual(t)
	covSetRoot(w, &segmentSnapshot{id: 1, segment: covWrap(&covDocsErrSegment{covSegment(t).Segment})})
	introductions := make(chan *segmentIntroduction)
	w.asyncTasks.Add(1)
	go w.introducerLoop(introductions, nil, nil, make(watcherChan), 2)
	intro := &segmentIntroduction{applied: make(chan error, 1), idTerms: []segment.Term{testIdentifier("1")}}
	introductions <- intro
	if err := <-intro.applied; err == nil {
		t.Fatal("expected error")
	}
	close(w.closeCh)
	w.asyncTasks.Wait()
	if w.root.epoch != 1 {
		t.Fatalf("root epoch %d", w.root.epoch)
	}
}

func TestIntroducePersistAndMergeKeepMemorySegments(t *testing.T) {
	w, _ := covManual(t)
	gone := covPersistedSeg(t, w, 1, nil)
	mem := &segmentSnapshot{id: 2, segment: covSegment(t)}
	covSetRoot(w, gone, mem)

	persist := &persistIntroduction{persisted: map[uint64]*segmentWrapper{}, applied: make(notificationChan)}
	w.introducePersist(persist, 2)
	if w.stats.TotMemorySegmentsAtRoot != 1 || w.stats.TotFileSegmentsAtRoot != 1 {
		t.Fatalf("memory %d file %d", w.stats.TotMemorySegmentsAtRoot, w.stats.TotFileSegmentsAtRoot)
	}

	sm := &segmentMerge{
		id:            3,
		old:           map[uint64]*segmentSnapshot{1: gone},
		oldNewDocNums: map[uint64][]uint64{1: {0, 1, 2, 3}},
		notifyCh:      make(chan *mergeTaskIntroStatus, 1),
	}
	w.introduceMerge(sm, 3)
	status := <-sm.notifyCh
	if !status.skipped || len(status.snapshot.segment) != 1 || status.snapshot.segment[0].id != 2 {
		t.Fatalf("status %+v", status)
	}
	if err := status.snapshot.Close(); err != nil {
		t.Fatal(err)
	}
	if w.stats.TotMemorySegmentsAtRoot != 1 {
		t.Fatalf("memory segments %d", w.stats.TotMemorySegmentsAtRoot)
	}
}

func TestKeepNLatestCleanupRemoveError(t *testing.T) {
	p := NewKeepNLatestDeletionPolicy(1)
	for epoch := uint64(1); epoch <= 2; epoch++ {
		p.Commit(&Snapshot{epoch: epoch})
	}
	dir := &covFaultDir{Directory: NewInMemoryDirectory(), remove: covFailKind(ItemKindSnapshot)}
	if err := p.Cleanup(dir); err != nil {
		t.Fatal(err)
	}
	if len(p.deletableEpochs) != 1 {
		t.Fatalf("deletable %v", p.deletableEpochs)
	}
	dir.remove = nil
	if err := p.Cleanup(dir); err != nil || len(p.deletableEpochs) != 0 {
		t.Fatalf("deletable %v, %v", p.deletableEpochs, err)
	}
}

func TestCountHashReader(t *testing.T) {
	r := newCountHashReader(strings.NewReader("abc"))
	if _, err := io.ReadAll(r); err != nil {
		t.Fatal(err)
	}
	if r.Count() != 3 || r.Sum32() == 0 {
		t.Fatalf("count %d sum %x", r.Count(), r.Sum32())
	}
}
