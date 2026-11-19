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
	"strconv"
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"
	iceV1 "github.com/vcaesar/ice"
)

// covIcePlugin is the current ice plugin without a segment builder.
func covIcePlugin() *SegmentPlugin {
	opts := iceV1.DefaultOptions()
	return &SegmentPlugin{Type: iceV1.Type, Version: iceV1.Version, New: opts.New, Load: opts.Load, Merge: iceV1.Merge}
}

// covWithPlugin registers p in a copy of the plugins of cfg, which copies
// of a Config otherwise share.
func covWithPlugin(cfg Config, p *SegmentPlugin) Config {
	plugins := make(map[string]map[uint32]*SegmentPlugin, len(cfg.supportedSegmentPlugins))
	for typ, versions := range cfg.supportedSegmentPlugins {
		plugins[typ] = make(map[uint32]*SegmentPlugin, len(versions))
		for v, plugin := range versions {
			plugins[typ][v] = plugin
		}
	}
	cfg.supportedSegmentPlugins = plugins
	return cfg.WithSegmentPlugin(p)
}

func covFailingNew(cfg Config) Config {
	p := covIcePlugin()
	p.New = func([]segment.Document, func(string, int) float32) (segment.Segment, uint64, error) {
		return nil, 0, errCov
	}
	return covWithPlugin(cfg, p)
}

func covFailingLoad(cfg Config) Config {
	p := covIcePlugin()
	p.Load = func(*segment.Data) (segment.Segment, error) { return nil, errCov }
	return covWithPlugin(cfg, p)
}

// covDirConfig is a file system config at a fresh path whose directory
// is wrapped by fault.
func covDirConfig(t *testing.T, fault func(*covFaultDir)) Config {
	path := t.TempDir()
	return DefaultConfigWithDirectory(func() Directory {
		d := &covFaultDir{Directory: NewFileSystemDirectory(path)}
		if fault != nil {
			fault(d)
		}
		return d
	}).WithNormCalc(covNorm)
}

func covMemDirConfig(fault func(*covFaultDir)) Config {
	return DefaultConfigWithDirectory(func() Directory {
		d := &covFaultDir{Directory: NewInMemoryDirectory()}
		fault(d)
		return d
	}).WithNormCalc(covNorm)
}

func covBatch(start, n uint64) *Batch {
	b := NewBatch()
	for i := start; i < start+n; i++ {
		b.Insert(covDoc(strconv.FormatUint(i, 10), "a b", 1))
	}
	return b
}

func TestOfflineWriterMerges(t *testing.T) {
	for _, batches := range []uint64{0, 1, 5} {
		t.Run(strconv.FormatUint(batches, 10), func(t *testing.T) {
			cfg := DefaultConfig(t.TempDir()).WithNormCalc(covNorm)
			w, err := OpenOfflineWriter(cfg)
			if err != nil {
				t.Fatal(err)
			}
			w.mergeMax = 2
			for i := range batches {
				if err := w.Batch(covBatch(3*i, 3)); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.Batch(NewBatch()); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			if batches == 0 {
				return
			}
			r, err := OpenReader(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = r.Close() }()
			if n, _ := r.Count(); n != 3*batches {
				t.Fatalf("count %d, want %d", n, 3*batches)
			}
		})
	}
}

func TestOfflineWriterInMemory(t *testing.T) {
	w, err := OpenOfflineWriter(covConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Batch(covBatch(0, 2)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOfflineWriterErrors(t *testing.T) {
	failID := func(kind string, id uint64) func(string, uint64) error {
		return func(k string, i uint64) error {
			if k == kind && i == id {
				return errCov
			}
			return nil
		}
	}
	tests := []struct {
		name    string
		cfg     func(t *testing.T) Config
		batches uint64
		failAt  string
	}{
		{"time range", func(*testing.T) Config { return covConfig().WithTimeRange(1, 2) }, 0, "open"},
		{"setup", func(*testing.T) Config {
			return covMemDirConfig(func(d *covFaultDir) { d.setup = errCov })
		}, 0, "open"},
		{"segment type", func(*testing.T) Config { return covConfig().WithSegmentType("nope") }, 0, "open"},
		{"new segment", func(*testing.T) Config { return covFailingNew(covConfig()) }, 1, "batch"},
		{"persist segment", func(*testing.T) Config {
			return covMemDirConfig(func(d *covFaultDir) { d.persist = covFailKind(ItemKindSegment) })
		}, 1, "batch"},
		{"merge load", func(t *testing.T) Config {
			return covDirConfig(t, func(d *covFaultDir) { d.load = failID(ItemKindSegment, 1) })
		}, 2, "close"},
		{"merge plugin load", func(t *testing.T) Config { return covFailingLoad(covDirConfig(t, nil)) }, 2, "close"},
		{"merge persist", func(t *testing.T) Config {
			return covDirConfig(t, func(d *covFaultDir) { d.persist = failID(ItemKindSegment, 2) })
		}, 2, "close"},
		{"merge close", func(t *testing.T) Config {
			return covDirConfig(t, func(d *covFaultDir) { d.closeErr = covFailKind(ItemKindSegment) })
		}, 2, "close"},
		{"merge remove", func(t *testing.T) Config {
			return covDirConfig(t, func(d *covFaultDir) { d.remove = covFailKind(ItemKindSegment) })
		}, 2, "close"},
		{"final load", func(t *testing.T) Config {
			return covDirConfig(t, func(d *covFaultDir) { d.load = covFailKind(ItemKindSegment) })
		}, 1, "close"},
		{"final plugin load", func(t *testing.T) Config { return covFailingLoad(covDirConfig(t, nil)) }, 1, "close"},
		{"final plugin load in memory", func(*testing.T) Config { return covFailingLoad(covConfig()) }, 1, "close"},
		{"snapshot persist", func(t *testing.T) Config {
			return covDirConfig(t, func(d *covFaultDir) { d.persist = covFailKind(ItemKindSnapshot) })
		}, 1, "close"},
		{"snapshot persist in memory", func(*testing.T) Config {
			return covMemDirConfig(func(d *covFaultDir) { d.persist = covFailKind(ItemKindSnapshot) })
		}, 1, "close"},
		{"final close", func(t *testing.T) Config {
			return covDirConfig(t, func(d *covFaultDir) { d.closeErr = covFailKind(ItemKindSegment) })
		}, 1, "close"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, err := OpenOfflineWriter(tt.cfg(t))
			if (err != nil) != (tt.failAt == "open") {
				t.Fatalf("open: %v", err)
			}
			if err != nil {
				return
			}
			for i := range tt.batches {
				err = w.Batch(covBatch(2*i, 2))
				if (err != nil) != (tt.failAt == "batch") {
					t.Fatalf("batch: %v", err)
				}
			}
			if err = w.Close(); (err != nil) != (tt.failAt == "close") {
				t.Fatalf("close: %v", err)
			}
		})
	}
}
