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
	"context"
	"io"
	"math/rand"
	"os"
	"strconv"
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"

	"github.com/vcaesar/riot/hnsw"
)

// benchGraphDocs is the segment size, overridable with RIOT_GRAPH_BENCH_DOCS.
func benchGraphDocs(b *testing.B) int {
	n := 10000
	if s := os.Getenv("RIOT_GRAPH_BENCH_DOCS"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v <= 0 {
			b.Fatalf("invalid RIOT_GRAPH_BENCH_DOCS %q", s)
		}
		n = v
	}
	return n
}

func benchVectorSegment(b *testing.B, seed int64, n, dim int) (seg *storedVectorSegment, queries [][]float32) {
	b.Helper()
	rng := rand.New(rand.NewSource(seed))
	vectors := make([][]float32, n)
	for i := range vectors {
		vectors[i] = make([]float32, dim)
		for j := range vectors[i] {
			vectors[i][j] = float32(rng.NormFloat64())
		}
	}
	return &storedVectorSegment{vectors: encodedVectors(b, vectors...)}, vectors[:64]
}

// BenchmarkSegmentGraphFirstQuery is the cost a search pays for a segment
// whose graph is not cached: a build, or a load of a persisted item.
func BenchmarkSegmentGraphFirstQuery(b *testing.B) {
	const dim = 64
	n := benchGraphDocs(b)
	seg, _ := benchVectorSegment(b, 1, n, dim)
	ctx := context.Background()
	graph, err := buildVectorGraph(ctx, seg, testGraphKey)
	if err != nil {
		b.Fatal(err)
	}
	var links, full bytes.Buffer
	if _, err := (graphItem{segmentID: 1, count: seg.Count(), field: "v", graph: graph}).WriteTo(&links, nil); err != nil {
		b.Fatal(err)
	}
	if _, err := graph.WriteTo(&full); err != nil {
		b.Fatal(err)
	}
	b.Logf("%d docs x %d dims: links-only item %d bytes, full graph %d bytes", n, dim, links.Len(), full.Len())

	b.Run("build", func(b *testing.B) {
		for b.Loop() {
			if _, err := buildVectorGraph(ctx, seg, testGraphKey); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("load_links", func(b *testing.B) {
		for b.Loop() {
			lr := &io.LimitedReader{R: bytes.NewReader(links.Bytes()), N: int64(links.Len())}
			_, version, err := readGraphHeader(lr)
			if err != nil {
				b.Fatal(err)
			}
			if _, err := readGraph(ctx, segment.NewDataBytes(links.Bytes()), lr, version, seg, testGraphKey, false); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("read_vectors", func(b *testing.B) {
		for b.Loop() {
			if _, _, err := segmentVectors(ctx, seg, "v", testGraphKey.metric); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("load_full", func(b *testing.B) {
		for b.Loop() {
			if _, err := hnsw.Read(bytes.NewReader(full.Bytes())); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkSegmentGraphSearchDuringWarmup measures a cached-graph query while
// another segment's graph builds in the background, as a warm-up does.
func BenchmarkSegmentGraphSearchDuringWarmup(b *testing.B) {
	const dim = 64
	n := benchGraphDocs(b)
	seg, queries := benchVectorSegment(b, 2, n, dim)
	ctx := context.Background()
	graph, err := buildVectorGraph(ctx, seg, testGraphKey)
	if err != nil {
		b.Fatal(err)
	}
	search := func(b *testing.B) {
		i := 0
		for b.Loop() {
			if _, err := graph.SearchContext(ctx, queries[i%len(queries)], 10, hnsw.DefaultEfSearch, nil); err != nil {
				b.Fatal(err)
			}
			i++
		}
	}
	b.Run("idle", search)
	b.Run("warming", func(b *testing.B) {
		other, _ := benchVectorSegment(b, 3, n, dim)
		warmCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for warmCtx.Err() == nil {
				if _, err := buildVectorGraph(warmCtx, other, testGraphKey); err != nil && warmCtx.Err() == nil {
					b.Error(err)
					return
				}
			}
		}()
		search(b)
		cancel()
		<-done
	})
}
