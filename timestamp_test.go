// Copyright (c) 2026 The Riot Authors.
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

package riot

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/vcaesar/riot/index"
)

func TestDocumentTimestamp(t *testing.T) {
	doc := NewDocument("a")
	if doc.Timestamp() != 0 {
		t.Fatal("default timestamp")
	}
	for _, value := range []int64{123, -123, 0, 456} {
		if doc.SetTimestamp(value) != doc || doc.Timestamp() != value {
			t.Fatal("timestamp setter")
		}
		if len(*doc) != 2 || string(doc.ID().Term()) != "a" {
			t.Fatal("document representation changed")
		}
	}
}

func TestTimestampPersistenceAndMerge(t *testing.T) {
	for _, version := range []uint32{index.DefaultConfig("").SegmentVersion} {
		for _, unknown := range []bool{false, true} {
			t.Run(fmt.Sprintf("ice%d/unknown=%v", version, unknown), func(t *testing.T) {
				path, err := os.MkdirTemp("./test", "timestamp-")
				if err != nil {
					t.Fatal(err)
				}
				defer cleanupTmpIndexPath(t, path)
				cfg := DefaultConfig(path).WithSegmentVersion(version)
				// Offline close merges these batches, proving timestamps survive the current ice merger.
				w, err := OpenOfflineWriter(cfg, 0, 10)
				if err != nil {
					t.Fatal(err)
				}
				for j := 0; j < 3; j++ {
					doc := NewDocument(fmt.Sprint(j)).SetTimestamp(int64(10 + j*10))
					if unknown && j == 1 {
						doc.SetTimestamp(0)
					}
					if err := w.Insert(doc); err != nil {
						t.Fatal(err)
					}
				}
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
				for _, tc := range []struct {
					min, max int64
					want     uint64
				}{
					{0, 0, 3}, {10, 10, 3}, {15, 25, 3}, {30, 30, 3}, {31, 0, 0}, {0, 9, 0}, {-30, -10, 0},
				} {
					want := tc.want
					if unknown {
						want = 3
					}
					r, err := OpenReader(cfg.WithTimeRange(tc.min, tc.max))
					if err != nil {
						t.Fatal(err)
					}
					count, err := r.Count()
					if err != nil || count != want {
						t.Fatalf("[%d,%d] count=%d want=%d err=%v", tc.min, tc.max, count, want, err)
					}
					matches, err := r.Search(context.Background(), NewTopNSearch(10, NewMatchAllQuery()))
					if err != nil {
						t.Fatal(err)
					}
					var found uint64
					for match, err := matches.Next(); ; match, err = matches.Next() {
						if err != nil {
							t.Fatal(err)
						}
						if match == nil {
							break
						}
						found++
						if err := match.VisitStoredFields(func(string, []byte) bool { return true }); err != nil {
							t.Fatal(err)
						}
					}
					if found != want {
						t.Fatalf("search=%d want=%d", found, want)
					}
					if err := r.Close(); err != nil {
						t.Fatal(err)
					}
				}
				// A rejected filtered writer must not alter the index; normal reopen keeps all docs.
				if _, err := OpenWriter(cfg.WithTimeRange(31, 0)); err == nil {
					t.Fatal("filtered writer accepted")
				}
				normal, err := OpenWriter(cfg)
				if err != nil {
					t.Fatal(err)
				}
				if err := normal.Close(); err != nil {
					t.Fatal(err)
				}
				r, err := index.OpenReader(cfg.indexConfig)
				if err != nil {
					t.Fatal(err)
				}
				count, err := r.Count()
				if err != nil || count != 3 {
					t.Fatalf("reopen count=%d err=%v", count, err)
				}
				for _, ss := range r.Segments() {
					minTime, maxTime := ss.Timestamp()
					if !unknown && (minTime != 10 || maxTime != 30) {
						t.Fatalf("merged range=[%d,%d]", minTime, maxTime)
					}
					if unknown && (minTime != 0 || maxTime != 0) {
						t.Fatal("mixed unknown must retain segment")
					}
					if ss.DocNum() != 3 || ss.SegmentSize() == 0 {
						t.Fatal("persisted metadata missing")
					}
				}
				if err := r.Close(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
