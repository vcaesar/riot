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
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"

	segment "github.com/vcaesar/bluge_segment_api"
)

// backupTail is the number of trailing bytes compared to match items:
// segments and graphs both end with a CRC-32 of their content.
const backupTail = 64

func listItems(d Directory, kind string) (map[uint64]struct{}, error) {
	ids, err := d.List(kind)
	if err != nil {
		return nil, err
	}
	rv := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		rv[id] = struct{}{}
	}
	return rv, nil
}

// backupItem makes remote hold item kind/id, copying the bytes of src, or
// writing w when src does not hold it. A copy is byte-identical, so it
// matches on the next backup. An item remote already has is kept if it
// matches the one in src, otherwise removed first, so a hard link it shares
// with another index is never truncated. A segment whose file a merge
// removed is written from w; a graph (nil w) that src cannot load, removed
// or still being persisted, is skipped and rebuilt by the restored index.
func backupItem(remote Directory, have map[uint64]struct{}, src Directory, kind string, id uint64,
	w WriterTo, cancel chan struct{}) error {
	if _, ok := have[id]; ok {
		if src != nil && sameItem(src, remote, kind, id) {
			return nil
		}
		if err := remote.Remove(kind, id); err != nil {
			return fmt.Errorf("error removing stale item: %w", err)
		}
	}
	if src == nil {
		return remote.Persist(kind, id, w, cancel)
	}
	if dst, ok := remote.(*FileSystemDirectory); ok {
		if srcFS, ok := src.(*FileSystemDirectory); ok && dst.linkFrom(srcFS, kind, id) {
			return nil
		}
	}
	data, closer, err := src.Load(kind, id)
	if err != nil {
		if w == nil {
			return nil
		}
		if errors.Is(err, fs.ErrNotExist) {
			return remote.Persist(kind, id, w, cancel)
		}
		return err
	}
	if data != nil {
		w = dataWriter{data}
	}
	if w != nil {
		err = remote.Persist(kind, id, w, cancel)
	}
	return errors.Join(err, closeItem(closer))
}

// backupGraphs backs up the persisted graphs of the snapshot's segments.
// Graphs only cached in memory are not, a restored index rebuilds them.
func (i *Snapshot) backupGraphs(remote Directory, cancel chan struct{}) error {
	if i.parent == nil || len(i.segment) == 0 {
		return nil
	}
	src := i.parent.directory
	ids, err := src.List(ItemKindGraph)
	if err != nil || len(ids) == 0 {
		return err
	}
	have, err := listItems(remote, ItemKindGraph)
	if err != nil {
		return fmt.Errorf("error listing backup vector graphs: %w", err)
	}
	segs := make(map[uint64]struct{}, len(i.segment))
	for _, s := range i.segment {
		segs[s.id] = struct{}{}
	}
	for _, id := range ids {
		if _, ok := segs[id>>graphKeyBits]; !ok {
			continue
		}
		if err = backupItem(remote, have, src, ItemKindGraph, id, nil, cancel); err != nil {
			return fmt.Errorf("error backing up vector graph %x: %w", id, err)
		}
	}
	return nil
}

// sameItem reports whether a and b hold kind/id with the same length and
// trailing bytes.
func sameItem(a, b Directory, kind string, id uint64) bool {
	ta, ok := itemTail(a, kind, id)
	if !ok {
		return false
	}
	tb, ok := itemTail(b, kind, id)
	return ok && ta.n == tb.n && bytes.Equal(ta.tail, tb.tail)
}

type tail struct {
	n    int
	tail []byte
}

func itemTail(d Directory, kind string, id uint64) (tail, bool) {
	data, closer, err := d.Load(kind, id)
	if err != nil {
		return tail{}, false
	}
	var t tail
	ok := data != nil
	if ok {
		t.n = data.Len()
		var b []byte
		b, err = data.Read(max(0, t.n-backupTail), t.n)
		t.tail, ok = bytes.Clone(b), err == nil
	}
	// an item that cannot be read or released is treated as different
	return t, closeItem(closer) == nil && ok
}

func closeItem(closer io.Closer) error {
	if closer == nil {
		return nil
	}
	return closer.Close()
}

// dataWriter persists loaded item data.
type dataWriter struct{ data *segment.Data }

func (d dataWriter) WriteTo(w io.Writer, closeCh chan struct{}) (int64, error) {
	select {
	case <-closeCh:
		return 0, fmt.Errorf("backup canceled")
	default:
	}
	return d.data.WriteTo(w)
}
