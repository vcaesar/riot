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
		return
	}
	s.graphPersistMu.Lock()
	defer s.graphPersistMu.Unlock()
	select {
	case <-s.closeCh:
		return
	default:
	}
	seg.AddRef()
	s.graphPersists.Go(func() {
		defer func() {
			if err := seg.DecRef(); err != nil {
				s.fireAsyncError(fmt.Errorf("error releasing segment %d after graph warm-up: %w", seg.id, err))
			}
		}()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
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
