//  Copyright (c) 2020 The Bluge Authors.
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

package bluge

import (
	"io"
	"log"

	"github.com/vcaesar/riot/index"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/similarity"

	"github.com/vcaesar/riot/analysis"
	"github.com/vcaesar/riot/analysis/analyzer"
)

type Config struct {
	indexConfig index.Config
	Logger      *log.Logger

	DefaultSearchField    string
	DefaultSearchAnalyzer *analysis.Analyzer
	DefaultSimilarity     search.Similarity
	PerFieldSimilarity    map[string]search.Similarity

	SearchStartFunc func(size uint64) error
	SearchEndFunc   func(size uint64)
}

// WithVirtualField allows you to describe a field that
// the index will behave as if all documents in this index were
// indexed with these field/terms, even though nothing is
// physically persisted about them in the index.
func (config Config) WithVirtualField(field Field) Config {
	_ = field.Analyze(0)
	config.indexConfig = config.indexConfig.WithVirtualField(field)
	return config
}

// WithTimeRange prunes segments outside inclusive bounds when opening a reader.
// Zero is unrestricted; unknown timestamps are retained. Writers reject ranges.
func (config Config) WithTimeRange(min, max int64) Config {
	config.indexConfig = config.indexConfig.WithTimeRange(min, max)
	return config
}

func (config Config) WithSegmentType(typ string) Config {
	config.indexConfig = config.indexConfig.WithSegmentType(typ)
	return config
}

func (config Config) WithSegmentVersion(ver uint32) Config {
	config.indexConfig = config.indexConfig.WithSegmentVersion(ver)
	return config
}

func (config Config) DisableOptimizeConjunction() Config {
	config.indexConfig = config.indexConfig.DisableOptimizeConjunction()
	return config
}

func (config Config) DisableOptimizeConjunctionUnadorned() Config {
	config.indexConfig = config.indexConfig.DisableOptimizeConjunctionUnadorned()
	return config
}

func (config Config) DisableOptimizeDisjunctionUnadorned() Config {
	config.indexConfig = config.indexConfig.DisableOptimizeDisjunctionUnadorned()
	return config
}

func (config Config) WithSearchStartFunc(f func(size uint64) error) Config {
	config.SearchStartFunc = f
	return config
}

func DefaultConfig(path string) Config {
	indexConfig := index.DefaultConfig(path)
	return defaultConfig(indexConfig)
}

func InMemoryOnlyConfig() Config {
	indexConfig := index.InMemoryOnlyConfig()
	return defaultConfig(indexConfig)
}

func DefaultConfigWithDirectory(df func() index.Directory) Config {
	indexConfig := index.DefaultConfigWithDirectory(df)
	return defaultConfig(indexConfig)
}

// DefaultConfigWithIndexConfig builds a search configuration using the supplied index settings.
func DefaultConfigWithIndexConfig(indexConfig index.Config) Config {
	return defaultConfig(indexConfig)
}

func defaultConfig(indexConfig index.Config) Config {
	rv := Config{
		Logger:                log.New(io.Discard, "bluge", log.LstdFlags),
		DefaultSearchField:    "_all",
		DefaultSearchAnalyzer: analyzer.NewStandardAnalyzer(),
		DefaultSimilarity:     similarity.NewBM25Similarity(),
		PerFieldSimilarity:    map[string]search.Similarity{},
	}

	allDocsFields := NewKeywordField("", "")
	_ = allDocsFields.Analyze(0)
	indexConfig = indexConfig.WithVirtualField(allDocsFields)
	indexConfig = indexConfig.WithNormCalc(func(field string, length int) float32 {
		if pfs, ok := rv.PerFieldSimilarity[field]; ok {
			return pfs.ComputeNorm(length)
		}
		return rv.DefaultSimilarity.ComputeNorm(length)
	})
	rv.indexConfig = indexConfig

	return rv
}
