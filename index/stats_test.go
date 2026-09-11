// Copyright (c) 2026 The Bluge Authors.
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
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
)

type statsDirectory struct {
	Directory
}

func (d *statsDirectory) Stats() (uint64, uint64) { return 7, 1234 }

func statsFieldPointers(s *Stats) []*uint64 {
	fields := []*uint64{
		&s.persistEpoch, &s.persistSnapshotSize, &s.mergeEpoch, &s.mergeSnapshotSize,
		&s.newSegBufBytesAdded, &s.newSegBufBytesRemoved, &s.analysisBytesAdded, &s.analysisBytesRemoved,
	}
	v := reflect.ValueOf(s).Elem()
	for i := 0; i < v.NumField(); i++ {
		if v.Type().Field(i).PkgPath == "" {
			fields = append(fields, v.Field(i).Addr().Interface().(*uint64))
		}
	}
	return fields
}

func TestWriterStatsSnapshot(t *testing.T) {
	w := &Writer{directory: &statsDirectory{}}
	for i, field := range statsFieldPointers(&w.stats) {
		atomic.StoreUint64(field, uint64(i+1))
	}
	before := w.stats
	want := before
	want.CurOnDiskFiles, want.CurOnDiskBytes = 7, 1234
	got := w.Stats()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot = %+v, want %+v", got, want)
	}
	if !reflect.DeepEqual(w.stats, before) {
		t.Fatal("Stats mutated shared statistics")
	}
	got.TotUpdates = 999
	if w.Stats().TotUpdates != want.TotUpdates {
		t.Fatal("returned statistics are not independent")
	}
	if items, size := w.DirectoryStats(); items != 7 || size != 1234 {
		t.Fatalf("DirectoryStats = (%d, %d), want (7, 1234)", items, size)
	}
}

func TestWriterStatsConcurrent(t *testing.T) {
	w := &Writer{directory: &statsDirectory{}}
	fields := statsFieldPointers(&w.stats)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 1000; i++ {
			for _, field := range fields {
				atomic.AddUint64(field, 1)
			}
		}
	}()
	for reader := 0; reader < 2; reader++ {
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < 1000; i++ {
				got := w.Stats()
				if got.CurOnDiskFiles != 7 || got.CurOnDiskBytes != 1234 {
					t.Errorf("incorrect directory statistics: %+v", got)
					return
				}
				w.DirectoryStats()
			}
		}()
	}
	close(start)
	wg.Wait()
	got := w.Stats()
	values := reflect.ValueOf(got)
	for i := 0; i < values.NumField(); i++ {
		name := values.Type().Field(i).Name
		if name == "CurOnDiskFiles" || name == "CurOnDiskBytes" {
			continue
		}
		if n := values.Field(i).Uint(); n != 1000 {
			t.Errorf("%s = %d, want 1000", name, n)
		}
	}
}
