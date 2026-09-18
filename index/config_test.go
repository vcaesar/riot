// Copyright 2022 Zinc Labs Inc. and Contributors
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
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/ice"
)

var _ segment.Segment = (*ice.Segment)(nil)

// Embedding the upstream interface hides FakeDocument's optional Timestamp method.
type documentWithoutTimestamp struct{ segment.Document }

func TestICEDocumentWithoutTimestamp(t *testing.T) {
	doc := documentWithoutTimestamp{&FakeDocument{NewFakeField("_id", "a", true, false, false)}}
	seg, _, err := ice.New([]segment.Document{doc}, func(string, int) float32 { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	timeMin, timeMax := seg.(*ice.Segment).Timestamp()
	if timeMin != 0 || timeMax != 0 {
		t.Fatalf("document without timestamp: bounds=(%d,%d), want=(0,0)", timeMin, timeMax)
	}
}

func TestDefaultConfigOnlyRegistersLocalICE(t *testing.T) {
	config := defaultConfig()
	if config.SegmentType != ice.Type || config.SegmentVersion != ice.Version {
		t.Fatalf("unexpected default segment: %s/%d", config.SegmentType, config.SegmentVersion)
	}
	plugins := config.supportedSegmentPlugins[ice.Type]
	// current version plus the previous term-doc-value format, still loadable
	if len(config.supportedSegmentPlugins) != 1 || len(plugins) != 2 {
		t.Fatalf("expected only local ICE plugins, got %v", config.supportedSegmentPlugins)
	}
	for _, ver := range []uint32{ice.Version, ice.Version - 1} {
		plugin := plugins[ver]
		if plugin == nil || plugin.New == nil || plugin.Load == nil || plugin.Merge == nil {
			t.Fatalf("local ICE plugin %d is missing or incomplete", ver)
		}
	}
}

func TestWithStoredChunkCacheSizeReachesSegments(t *testing.T) {
	for _, tc := range []struct {
		name        string
		size        int
		wantEntries int
	}{{"disabled", 0, 0}, {"enabled", 4, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := InMemoryOnlyConfig().WithNormCalc(func(string, int) float32 { return 1 }).WithStoredChunkCacheSize(tc.size)
			if cfg.StoredChunkCacheSize != tc.size {
				t.Fatalf("StoredChunkCacheSize=%d, want %d", cfg.StoredChunkCacheSize, tc.size)
			}
			idx, err := OpenWriter(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := idx.Close(); err != nil {
					t.Error(err)
				}
			}()
			b := NewBatch()
			b.Update(testIdentifier("1"), &FakeDocument{NewFakeField("_id", "1", true, false, false)})
			if err := idx.Batch(b); err != nil {
				t.Fatal(err)
			}
			reader, err := idx.Reader()
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := reader.Close(); err != nil {
					t.Error(err)
				}
			}()
			if err := reader.VisitStoredFields(0, func(string, []byte) bool { return true }); err != nil {
				t.Fatal(err)
			}
			seg, ok := reader.segment[0].segment.Segment.(*ice.Segment)
			if !ok {
				t.Fatalf("segment is %T, want *ice.Segment", reader.segment[0].segment.Segment)
			}
			if _, _, entries := seg.StoredChunkCacheStats(); entries != tc.wantEntries {
				t.Fatalf("cached chunks=%d, want %d", entries, tc.wantEntries)
			}
		})
	}
}
