//  Copyright (c) 2020 The Bluge Authors.
//  Copyright (c) 2026 The AtomAI.
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

package index

import (
	"fmt"
	"log"
	"math"
	"slices"

	segment "github.com/vcaesar/bluge_segment_api"

	iceV1 "github.com/vcaesar/ice"
	"github.com/vcaesar/ice/vec"

	"github.com/vcaesar/riot/hnsw"
	"github.com/vcaesar/riot/index/mergeplan"
)

type Config struct {
	SegmentType    string
	SegmentVersion uint32

	supportedSegmentPlugins map[string]map[uint32]*SegmentPlugin

	UnsafeBatch   bool
	EventCallback func(Event)
	AsyncError    func(error)
	// Logger receives recoverable open-time problems; nil uses log.Default().
	Logger             *log.Logger
	MergePlanOptions   mergeplan.Options
	NumAnalysisWorkers int
	AnalysisChan       chan func()
	GoFunc             func(func())
	DeletionPolicyFunc func() DeletionPolicy
	DirectoryFunc      func() Directory
	NormCalc           func(string, int) float32

	MergeBufferSize int

	// StoredChunkCacheSize bounds decompressed stored-field chunks each ice
	// segment keeps in its LRU; zero disables caching. Applies to segments
	// opened after the index is configured.
	StoredChunkCacheSize int

	// VectorStorage selects where persisted HNSW graphs keep their vectors;
	// the zero value is VectorStorageDisk.
	VectorStorage VectorStorage

	// VectorIndexes declares the approximate searches to prepare: a Writer
	// builds their HNSW graphs in the background as segments are introduced,
	// persisted and merged, rather than on the first search.
	VectorIndexes []VectorIndex

	// Inclusive segment time bounds; zero leaves that bound unrestricted.
	// Unknown timestamps are retained. Only read-only OpenReader accepts a range.
	FilterTimeMin int64
	FilterTimeMax int64

	// Optimizations
	OptimizeConjunction          bool
	OptimizeConjunctionUnadorned bool
	OptimizeDisjunctionUnadorned bool

	// MinSegmentsForInMemoryMerge represents the number of
	// in-memory zap segments that persistSnapshotMaybeMerge() needs to
	// see in an Snapshot before it decides to merge and persist
	// those segments
	MinSegmentsForInMemoryMerge int

	// PersisterNapTimeMSec controls the wait/delay injected into
	// persistence workloop to improve the chances for
	// a healthier and heavier in-memory merging
	PersisterNapTimeMSec int

	// PersisterNapTimeMSec > 0, and the number of files is less than
	// PersisterNapUnderNumFiles, then the persister will sleep
	// PersisterNapTimeMSec amount of time to improve the chances for
	// a healthier and heavier in-memory merging
	PersisterNapUnderNumFiles int

	// MemoryPressurePauseThreshold let persister to have a better leeway
	// for prudently performing the memory merge of segments on a memory
	// pressure situation. Here the config value is an upper threshold
	// for the number of paused application threads. The default value would
	// be a very high number to always favor the merging of memory segments.
	MemoryPressurePauseThreshold int

	ValidateSnapshotCRC bool

	// IndexMode selects how Writer.Batch builds segments, see Mode.
	IndexMode Mode
	// BatchSegmentDocs caps the documents per segment in IndexModeBatch;
	// zero or less builds one segment per batch.
	BatchSegmentDocs int
	// StreamMemoryBudget is the packed bytes per segment in IndexModeStream
	// and of Writer.NewStream(0); zero or less uses
	// DefaultBatchStreamMemoryBudget for Writer.Batch and
	// DefaultStreamMemoryBudget for Writer.NewStream.
	StreamMemoryBudget int

	virtualFields map[string][]segment.Field
}

// Mode selects how Writer.Batch turns documents into segments. Either
// way a batch is published atomically.
type Mode uint8

const (
	// IndexModeBatch analyzes the documents and builds them
	// into segments of up to BatchSegmentDocs documents. A Batch already
	// holds every document, so this is its cheapest mode; for bounded
	// memory, add documents through Writer.NewStream instead.
	IndexModeBatch Mode = iota
	// IndexModeStream, the default of DefaultConfig and its variants,
	// analyzes documents a block at a time and packs them into a compact
	// buffer, built into a segment every StreamMemoryBudget bytes, as
	// Stream does.
	IndexModeStream
)

// WithBatchIndexing selects IndexModeBatch with up to segmentDocs documents
// per segment; zero or less builds one segment per batch. With a cap, each
// document's analysis is released once its segment is built.
func (config Config) WithBatchIndexing(segmentDocs int) Config {
	config.IndexMode, config.BatchSegmentDocs = IndexModeBatch, segmentDocs
	return config
}

// WithStreamIndexing selects IndexModeStream with a segment every
// memoryBudget packed bytes; zero or less uses DefaultBatchStreamMemoryBudget
// for Writer.Batch and DefaultStreamMemoryBudget for Writer.NewStream.
// Each document's analysis is released once it is packed.
func (config Config) WithStreamIndexing(memoryBudget int) Config {
	config.IndexMode, config.StreamMemoryBudget = IndexModeStream, memoryBudget
	return config
}

// WithTimeRange prunes segments outside the inclusive range, not individual documents.
// Zero bounds are unrestricted. Documents must use the same timestamp units.
func (config Config) WithTimeRange(timeMin, timeMax int64) Config {
	config.FilterTimeMin, config.FilterTimeMax = timeMin, timeMax
	return config
}

func (config Config) WithSegmentType(typ string) Config {
	config.SegmentType = typ
	return config
}

func (config Config) WithSegmentVersion(ver uint32) Config {
	config.SegmentVersion = ver
	return config
}

// VectorStorage selects where the HNSW graphs of persisted segments keep
// their vectors. Either way, graphs of in-memory segments live on the heap
// and search returns exact scores.
type VectorStorage uint8

const (
	// VectorStorageDisk, the default, persists each graph with its float32
	// vectors and int8 codes and memory-maps it, as Lucene does: opening
	// reads no vectors, and the OS page cache, not the Go heap, holds them.
	// Graph files take about 5 bytes more per dimension and vector.
	VectorStorageDisk VectorStorage = iota
	// VectorStorageMemory persists links only and reads the vectors from
	// the segment's stored fields onto the heap when a graph is opened.
	VectorStorageMemory
)

// WithVectorStorage selects where persisted HNSW graphs keep their vectors.
func (config Config) WithVectorStorage(storage VectorStorage) Config {
	config.VectorStorage = storage
	return config
}

// VectorIndex declares an approximate search on Field. A search uses the
// prebuilt graph only with the same Metric, M, EfConstruction and
// Quantization; EfSearch and ConcurrentAccept do not matter.
type VectorIndex struct {
	Field  string
	Metric vec.Metric
	Params hnsw.Params
}

func (v VectorIndex) validate() error {
	if v.Field == "" {
		return fmt.Errorf("vector index field must be non-empty")
	}
	switch v.Metric {
	case vec.Cosine, vec.DotProduct, vec.L2:
	default:
		return fmt.Errorf("vector index %q: unsupported metric %q", v.Field, v.Metric)
	}
	if err := v.Params.Validate(); err != nil {
		return fmt.Errorf("vector index %q: %w", v.Field, err)
	}
	return nil
}

func (v VectorIndex) key() graphKey {
	return newGraphKey(v.Field, v.Metric, v.Params.WithDefaults())
}

// WithVectorIndex declares an approximate search whose graphs a Writer
// builds at index time; see VectorIndex.
func (config Config) WithVectorIndex(field string, metric vec.Metric, params hnsw.Params) Config {
	config.VectorIndexes = append(slices.Clip(config.VectorIndexes),
		VectorIndex{Field: field, Metric: metric, Params: params})
	return config
}

// WithStoredChunkCacheSize re-registers the ice plugins so their segments
// cache at most n decompressed stored-field chunks; zero disables caching.
func (config Config) WithStoredChunkCacheSize(n int) Config {
	config.StoredChunkCacheSize = n
	return config.withIcePlugins(iceV1.Options{StoredChunkCacheSize: n})
}

func (config Config) withIcePlugins(opts iceV1.Options) Config {
	for ver := iceV1.MinReadableVersion; ver <= iceV1.Version; ver++ {
		config = config.WithSegmentPlugin(&SegmentPlugin{
			Type:    iceV1.Type,
			Version: ver,
			New:     opts.New,
			Load:    opts.Load,
			Merge:   iceV1.Merge,
			NewBuilder: func(normCalc func(string, int) float32) SegmentBuilder {
				return opts.NewBuilder(normCalc)
			},
		})
	}
	return config
}

func (config Config) WithPersisterNapTimeMSec(napTime int) Config {
	config.PersisterNapTimeMSec = napTime
	return config
}

func (config Config) WithVirtualField(field segment.Field) Config {
	config.virtualFields[field.Name()] = append(config.virtualFields[field.Name()], field)
	return config
}

// WithNormCalc sets the field length norm; segments of large batches are
// built by several goroutines at once, so calc must be safe to call
// concurrently.
func (config Config) WithNormCalc(calc func(field string, numTerms int) float32) Config {
	config.NormCalc = calc
	return config
}

func (config Config) WithSegmentPlugin(plugin *SegmentPlugin) Config {
	if _, ok := config.supportedSegmentPlugins[plugin.Type]; !ok {
		config.supportedSegmentPlugins[plugin.Type] = map[uint32]*SegmentPlugin{}
	}
	config.supportedSegmentPlugins[plugin.Type][plugin.Version] = plugin
	return config
}

func (config Config) DisableOptimizeConjunction() Config {
	config.OptimizeConjunction = false
	return config
}

func (config Config) DisableOptimizeConjunctionUnadorned() Config {
	config.OptimizeConjunctionUnadorned = false
	return config
}

func (config Config) DisableOptimizeDisjunctionUnadorned() Config {
	config.OptimizeDisjunctionUnadorned = false
	return config
}

func (config Config) WithUnsafeBatches() Config {
	config.UnsafeBatch = true
	return config
}

func DefaultConfig(path string) Config {
	rv := defaultConfig()
	rv.DirectoryFunc = func() Directory {
		return NewFileSystemDirectory(path)
	}
	return rv
}

func InMemoryOnlyConfig() Config {
	rv := defaultConfig()
	rv.DirectoryFunc = func() Directory {
		return NewInMemoryDirectory()
	}
	return rv
}

func DefaultConfigWithDirectory(df func() Directory) Config {
	rv := defaultConfig()
	rv.DirectoryFunc = df
	return rv
}

func defaultConfig() Config {
	rv := Config{
		SegmentType:      iceV1.Type,
		SegmentVersion:   iceV1.Version,
		MergePlanOptions: mergeplan.DefaultMergePlanOptions,
		DeletionPolicyFunc: func() DeletionPolicy {
			return NewKeepNLatestDeletionPolicy(1)
		},

		IndexMode: IndexModeStream,

		MergeBufferSize:      1024 * 1024,
		StoredChunkCacheSize: iceV1.StoredChunkCacheSize,

		// Optimizations enabled
		OptimizeConjunction:          true,
		OptimizeConjunctionUnadorned: true,
		OptimizeDisjunctionUnadorned: true,

		MinSegmentsForInMemoryMerge: 2,

		// DefaultPersisterNapTimeMSec is kept to zero as this helps in direct
		// persistence of segments with the default safe batch option.
		// If the default safe batch option results in high number of
		// files on disk, then users may initialize this configuration parameter
		// with higher values so that the persister will nap a bit within it's
		// work loop to favor better in-memory merging of segments to result
		// in fewer segment files on disk. But that may come with an indexing
		// performance overhead.
		// Unsafe batch users are advised to override this to higher value
		// for better performance especially with high data density.
		PersisterNapTimeMSec: 0,

		// DefaultPersisterNapUnderNumFiles helps in controlling the pace of
		// persister. At times of a slow merger progress with heavy file merging
		// operations, its better to pace down the persister for letting the merger
		// to catch up within a range defined by this parameter.
		// Fewer files on disk (as per the merge plan) would result in keeping the
		// file handle usage under limit, faster disk merger and a healthier index.
		// Its been observed that such a loosely sync'ed introducer-persister-merger
		// trio results in better overall performance.
		PersisterNapUnderNumFiles: 1000,

		MemoryPressurePauseThreshold: math.MaxInt32,

		// VirtualFields allow you to describe a set of fields
		// The index will behave as if all documents in this index were
		// indexed with these fields, even though nothing is
		// physically persisted about them in the index.
		virtualFields: map[string][]segment.Field{},

		NumAnalysisWorkers: 4,
		AnalysisChan:       make(chan func()),
		GoFunc: func(f func()) {
			go f()
		},

		ValidateSnapshotCRC: true,

		supportedSegmentPlugins: map[string]map[uint32]*SegmentPlugin{},
	}

	// every readable ice version: older segments lack numeric columns (3)
	// or postings block bounds (4); merges rewrite them as Version.
	return rv.withIcePlugins(iceV1.DefaultOptions())
}
