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
	min, max := seg.(*ice.Segment).Timestamp()
	if min != 0 || max != 0 {
		t.Fatalf("document without timestamp: bounds=(%d,%d), want=(0,0)", min, max)
	}
}

func TestDefaultConfigOnlyRegistersLocalICE(t *testing.T) {
	config := defaultConfig()
	if config.SegmentType != ice.Type || config.SegmentVersion != ice.Version {
		t.Fatalf("unexpected default segment: %s/%d", config.SegmentType, config.SegmentVersion)
	}
	plugins := config.supportedSegmentPlugins[ice.Type]
	if len(config.supportedSegmentPlugins) != 1 || len(plugins) != 1 {
		t.Fatalf("expected only local ICE plugin, got %v", config.supportedSegmentPlugins)
	}
	plugin := plugins[ice.Version]
	if plugin == nil || plugin.New == nil || plugin.Load == nil || plugin.Merge == nil {
		t.Fatal("local ICE plugin is missing or incomplete")
	}
}
