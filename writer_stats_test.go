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

package bluge

import (
	"sync"
	"testing"

	"github.com/vcaesar/riot/index"
)

type writerStatsDirectory struct {
	index.Directory
}

func (d *writerStatsDirectory) Stats() (items, bytes uint64) { return 3, 456 }

func TestWriterStatus(t *testing.T) {
	for _, custom := range []bool{false, true} {
		name := "in-memory"
		config := InMemoryOnlyConfig()
		var wantItems, wantBytes uint64
		if custom {
			name = "custom-directory"
			config = DefaultConfigWithDirectory(func() index.Directory {
				return &writerStatsDirectory{Directory: index.NewInMemoryDirectory()}
			})
			wantItems, wantBytes = 3, 456
		}
		t.Run(name, func(t *testing.T) {
			w, err := OpenWriter(config)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := w.Close(); err != nil {
					t.Error(err)
				}
			}()
			before := w.Status()
			if before.TotUpdates != 0 || before.TotBatches != 0 {
				t.Fatalf("unexpected initial counters: %+v", before)
			}
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 200; i++ {
					status := w.Status()
					if status.CurOnDiskFiles != wantItems || status.CurOnDiskBytes != wantBytes {
						t.Errorf("incorrect directory totals: %+v", status)
						return
					}
					if items, size := w.DirectoryStats(); items != wantItems || size != wantBytes {
						t.Errorf("DirectoryStats = (%d, %d), want (%d, %d)", items, size, wantItems, wantBytes)
						return
					}
				}
			}()
			for i := 0; i < 20; i++ {
				doc := NewDocument("a").AddField(NewTextField("text", "hello world"))
				if err := w.Update(doc.ID(), doc); err != nil {
					t.Error(err)
					break
				}
			}
			wg.Wait()
			if err := w.Delete(Identifier("a")); err != nil {
				t.Fatal(err)
			}
			got := w.Status()
			if got.TotUpdates != 20 || got.TotDeletes != 21 || got.TotBatches != 21 {
				t.Fatalf("unexpected counters: %+v", got)
			}
			if got.CurOnDiskFiles != wantItems || got.CurOnDiskBytes != wantBytes {
				t.Fatalf("incorrect directory totals: %+v", got)
			}
			if before.TotUpdates != 0 || before.TotBatches != 0 {
				t.Fatal("previous snapshot changed")
			}
			got.TotUpdates = 999
			if w.Status().TotUpdates != 20 {
				t.Fatal("mutating returned status changed writer counters")
			}
		})
	}
}
