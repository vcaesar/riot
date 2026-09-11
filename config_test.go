// Copyright (c) 2026 The Bluge Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bluge

import (
	"context"
	"testing"

	"github.com/vcaesar/riot/index"
)

func TestDefaultConfigWithIndexConfig(t *testing.T) {
	for _, version := range []uint32{index.DefaultConfig("").SegmentVersion} {
		t.Run(string(rune('0'+version)), func(t *testing.T) {
			directory := index.NewInMemoryDirectory()
			indexConfig := index.DefaultConfigWithDirectory(func() index.Directory { return directory }).
				WithSegmentVersion(version).WithPersisterNapTimeMSec(10).DisableOptimizeConjunction()
			config := DefaultConfigWithIndexConfig(indexConfig)
			if config.indexConfig.DirectoryFunc() != directory || config.indexConfig.SegmentVersion != version ||
				config.indexConfig.PersisterNapTimeMSec != 10 || config.indexConfig.OptimizeConjunction {
				t.Fatal("custom index settings were not preserved")
			}
			if config.Logger == nil || config.DefaultSearchAnalyzer == nil || config.DefaultSimilarity == nil ||
				config.PerFieldSimilarity == nil || config.DefaultSearchField != "_all" {
				t.Fatal("missing default search settings")
			}
			if got := config.indexConfig.NormCalc("text", 3); got != config.DefaultSimilarity.ComputeNorm(3) {
				t.Fatalf("incorrect norm: %v", got)
			}
			writer, err := OpenWriter(config)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if closeErr := writer.Close(); closeErr != nil {
					t.Error(closeErr)
				}
			}()
			if err = writer.Insert(NewDocument("doc").AddField(NewTextField("text", "hello"))); err != nil {
				t.Fatal(err)
			}
			reader, err := writer.Reader()
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if closeErr := reader.Close(); closeErr != nil {
					t.Error(closeErr)
				}
			}()
			iter, err := reader.Search(context.Background(), NewAllMatches(NewMatchAllQuery()))
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for {
				match, nextErr := iter.Next()
				if nextErr != nil {
					t.Fatal(nextErr)
				}
				if match == nil {
					break
				}
				count++
			}
			if count != 1 {
				t.Fatalf("expected one match with virtual field, got %d", count)
			}
		})
	}
}
