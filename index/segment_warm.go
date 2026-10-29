// Copyright (c) 2026 The AtomAI.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package index

import (
	"context"
	"fmt"
	"maps"

	"github.com/vcaesar/riot/hnsw"
)

// noteGraphKey records a key searched approximately, so graphs for it are
// built in the background for segments introduced later.
func (s *Writer) noteGraphKey(key graphKey) {
	s.graphKeysMu.Lock()
	defer s.graphKeysMu.Unlock()
	if s.graphKeys == nil {
		s.graphKeys = make(map[graphKey]struct{})
	}
	s.graphKeys[key] = struct{}{}
}

func (s *Writer) searchedGraphKeys() []graphKey {
	s.graphKeysMu.Lock()
	defer s.graphKeysMu.Unlock()
	keys := make([]graphKey, 0, len(s.graphKeys))
	for key := range s.graphKeys {
		keys = append(keys, key)
	}
	return keys
}

// adoptGraphs shares from's cached graphs, built or in progress, with s,
// which must hold the same documents in the same order, as a persisted copy
// of an in-memory segment does. It returns the adopted entries.
func (s *segmentWrapper) adoptGraphs(from *segmentWrapper) map[graphKey]*graphEntry {
	from.graphMu.Lock()
	adopted := maps.Clone(from.graphs)
	from.graphMu.Unlock()
	if len(adopted) == 0 {
		return nil
	}
	s.graphMu.Lock()
	defer s.graphMu.Unlock()
	if s.graphs == nil {
		s.graphs = make(map[graphKey]*graphEntry, len(adopted))
	}
	for key, e := range adopted {
		if _, ok := s.graphs[key]; ok {
			delete(adopted, key)
			continue
		}
		s.graphs[key] = e
	}
	return adopted
}

// warmGraphs makes seg's graphs ready in the background, so the first
// approximate search after a persist or merge does not pay for a build.
// Adopted graphs are persisted for seg; every key searched so far that is
// missing is loaded or built, one segment at a time. Work stops when the
// writer closes, and segments no longer in the root are skipped.
func (s *Writer) warmGraphs(seg *segmentWrapper, adopted map[graphKey]*graphEntry) {
	keys := s.searchedGraphKeys()
	if len(keys) == 0 && len(adopted) == 0 {
		s.releaseMergeSources(seg)
		return
	}
	s.graphPersistMu.Lock()
	defer s.graphPersistMu.Unlock()
	select {
	case <-s.closeCh:
		s.releaseMergeSources(seg)
		return
	default:
	}
	seg.AddRef()
	ctx, cancel := context.WithCancel(context.Background())
	w := s.trackWarmup(seg, cancel)
	s.graphPersists.Go(func() {
		defer func() {
			s.releaseMergeSources(seg)
			s.untrackWarmup(w)
			cancel()
			if err := seg.DecRef(); err != nil {
				s.fireAsyncError(fmt.Errorf("error releasing segment %d after graph warm-up: %w", seg.id, err))
			}
		}()
		go func() {
			select {
			case <-s.closeCh:
				cancel()
			case <-ctx.Done():
			}
		}()
		s.persistAdopted(ctx, seg, adopted)

		s.graphWarmMu.Lock()
		defer s.graphWarmMu.Unlock()
		if !s.inRoot(seg) {
			return
		}
		for _, key := range keys {
			if s.warmupMoved(w, seg) {
				// The persisted copy's own warm-up builds the rest.
				return
			}
			_, err := seg.vectorGraph(ctx, key)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				s.fireAsyncError(fmt.Errorf("error building vector graph of segment %d: %w", seg.id, err))
			}
		}
	})
}

// persistAdopted waits for adopted graphs and persists those that built.
func (s *Writer) persistAdopted(ctx context.Context, seg *segmentWrapper, adopted map[graphKey]*graphEntry) {
	if seg.writer == nil || s.readOnly {
		return
	}
	for key, e := range adopted {
		select {
		case <-e.ready:
		case <-ctx.Done():
			return
		}
		if !e.abandoned && e.err == nil {
			s.persistGraph(seg.id, seg.Count(), key, e.graph)
		}
	}
}

func (s *Writer) releaseMergeSources(seg *segmentWrapper) {
	if err := seg.clearMergeSources(); err != nil {
		s.fireAsyncError(err)
	}
}

// warmup is a running warm-up; seg is the segment whose presence in the
// root keeps it alive, guarded by Writer.warmupsMu.
type warmup struct {
	seg    *segmentWrapper
	cancel context.CancelFunc
}

func (s *Writer) trackWarmup(seg *segmentWrapper, cancel context.CancelFunc) *warmup {
	w := &warmup{seg: seg, cancel: cancel}
	s.warmupsMu.Lock()
	defer s.warmupsMu.Unlock()
	if s.warmups == nil {
		s.warmups = make(map[*warmup]struct{})
	}
	s.warmups[w] = struct{}{}
	return w
}

func (s *Writer) untrackWarmup(w *warmup) {
	s.warmupsMu.Lock()
	delete(s.warmups, w)
	s.warmupsMu.Unlock()
}

func (s *Writer) warmupMoved(w *warmup, seg *segmentWrapper) bool {
	s.warmupsMu.Lock()
	defer s.warmupsMu.Unlock()
	return w.seg != seg
}

// moveWarmups ties warm-ups of persisted in-memory segments to their
// replacements, which adopted their graphs, so the builds are kept.
func (s *Writer) moveWarmups(replaced map[*segmentWrapper]*segmentWrapper) {
	s.warmupsMu.Lock()
	defer s.warmupsMu.Unlock()
	for w := range s.warmups {
		if next, ok := replaced[w.seg]; ok {
			w.seg = next
		}
	}
}

// pruneWarmups cancels warm-ups whose segment is not in root, so a merged
// away segment releases its files instead of finishing a useless build.
func (s *Writer) pruneWarmups(root *Snapshot) {
	s.warmupsMu.Lock()
	defer s.warmupsMu.Unlock()
	if len(s.warmups) == 0 {
		return
	}
	live := make(map[*segmentWrapper]struct{})
	if root != nil {
		for _, ss := range root.segment {
			live[ss.segment] = struct{}{}
		}
	}
	for w := range s.warmups {
		if _, ok := live[w.seg]; !ok {
			w.cancel()
		}
	}
}

func (s *Writer) inRoot(seg *segmentWrapper) bool {
	root := s.currentSnapshot()
	if root == nil {
		return false
	}
	defer func() {
		if err := root.Close(); err != nil {
			s.fireAsyncError(fmt.Errorf("error releasing snapshot after graph warm-up: %w", err))
		}
	}()
	for _, ss := range root.segment {
		if ss.segment == seg {
			return true
		}
	}
	return false
}

// warmPersistedGraphs resumes warm-ups after a reopen: keys of persisted
// graphs count as searched, so root segments missing a graph, e.g. one
// merged just before close, are built in the background rather than by the
// first approximate search.
func (s *Writer) warmPersistedGraphs() {
	for _, key := range s.persistedGraphKeys() {
		s.noteGraphKey(key)
	}
	root := s.currentSnapshot()
	if root == nil {
		return
	}
	defer func() {
		if err := root.Close(); err != nil {
			s.fireAsyncError(fmt.Errorf("error releasing snapshot after graph warm-up: %w", err))
		}
	}()
	for _, ss := range root.segment {
		s.warmGraphs(ss.segment, nil)
	}
}

// persistedGraphKeys returns the keys of the graph items in the directory,
// reading one item header per key hash.
func (s *Writer) persistedGraphKeys() []graphKey {
	ids, err := s.directory.List(ItemKindGraph)
	if err != nil {
		s.fireAsyncError(fmt.Errorf("error listing vector graphs: %w", err))
		return nil
	}
	var keys []graphKey
	seen := make(map[uint64]struct{})
	for _, id := range ids {
		hash := id & (1<<graphKeyBits - 1)
		if _, ok := seen[hash]; ok {
			continue
		}
		if key, ok := s.readGraphKey(id); ok {
			seen[hash] = struct{}{}
			keys = append(keys, key)
		}
	}
	return keys
}

// readGraphKey reads the key of graph item id. Unreadable items are skipped:
// loading one later rebuilds it.
func (s *Writer) readGraphKey(id uint64) (graphKey, bool) {
	data, closer, err := s.directory.Load(ItemKindGraph, id)
	if err != nil || data == nil {
		return graphKey{}, false
	}
	defer func() {
		if closer != nil {
			if err := closer.Close(); err != nil {
				s.fireAsyncError(fmt.Errorf("error closing vector graph %x: %w", id, err))
			}
		}
	}()
	r := data.Reader()
	item, _, err := readGraphHeader(r)
	if err != nil {
		return graphKey{}, false
	}
	metric, params, err := hnsw.ReadParams(r)
	if err != nil {
		return graphKey{}, false
	}
	key := newGraphKey(item.field, metric, params)
	if want, ok := graphItemID(item.segmentID, key); !ok || want != id {
		return graphKey{}, false
	}
	return key, true
}
