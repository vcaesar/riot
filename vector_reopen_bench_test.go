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

package riot

import (
	"context"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vcaesar/riot/hnsw"
)

// TestANNReopenTiming reports, per vector storage and quantization, the
// first ANN query after a reopen, steady query latency, recall@10, the heap
// held by the reopened index and the graph bytes on disk. It runs only when
// RIOT_ANN_REOPEN_DOCS is set, e.g. 100000.
func TestANNReopenTiming(t *testing.T) {
	n, err := strconv.Atoi(os.Getenv("RIOT_ANN_REOPEN_DOCS"))
	if err != nil || n <= 0 {
		t.Skip("set RIOT_ANN_REOPEN_DOCS to run")
	}
	const dim, queries = 64, 50
	rng := rand.New(rand.NewSource(1))
	random := func() []float32 {
		v := make([]float32, dim)
		for i := range v {
			v[i] = float32(rng.NormFloat64())
		}
		return v
	}
	docs := make([][]float32, n)
	for i := range docs {
		docs[i] = random()
	}
	qs := make([][]float32, queries)
	for i := range qs {
		qs[i] = random()
	}
	for _, storage := range []VectorStorage{VectorStorageDisk, VectorStorageMemory} {
		for _, quant := range []hnsw.Quantization{hnsw.QuantizationInt8, hnsw.QuantizationNone} {
			path := createTmpIndexPath(t)
			annReopen(t, path, DefaultConfig(path).WithVectorStorage(storage), docs, qs,
				ANNParams{Quantization: quant}, fmt.Sprintf("%s/%v", map[VectorStorage]string{VectorStorageDisk: "disk", VectorStorageMemory: "memory"}[storage], quant))
		}
	}
}

func annReopen(t *testing.T, path string, config Config, docs, qs [][]float32, params ANNParams, name string) {
	t.Helper()
	defer cleanupTmpIndexPath(t, path)
	build := annBuild(t, config, docs, qs[0], params)

	heap := func() uint64 {
		runtime.GC()
		runtime.GC() // also empties sync.Pool victims
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m.HeapAlloc
	}
	before := heap()
	start := time.Now()
	// OpenReader loads graphs on first use, without background warm-up.
	r, err := OpenReader(config)
	if err != nil {
		t.Fatal(err)
	}
	opened := time.Since(start)
	start = time.Now()
	if _, err := r.SearchVectorsANN(context.Background(), "v", qs[0], 10, Cosine, params, nil); err != nil {
		t.Fatal(err)
	}
	first := time.Since(start)
	held := int64(heap()) - int64(before) // #nosec G115 -- heap sizes fit in int64.
	var hits int
	var annTime, exactTime time.Duration
	for _, q := range qs {
		start = time.Now()
		got, err := r.SearchVectorsANN(context.Background(), "v", q, 10, Cosine, params, nil)
		annTime += time.Since(start)
		if err != nil {
			t.Fatal(err)
		}
		start = time.Now()
		want, err := r.SearchVectors(context.Background(), "v", q, 10, Cosine, nil)
		exactTime += time.Since(start)
		if err != nil {
			t.Fatal(err)
		}
		wanted := make(map[uint64]bool, len(want))
		for _, m := range want {
			wanted[m.Number] = true
		}
		for _, m := range got {
			if wanted[m.Number] {
				hits++
			}
		}
	}
	t.Logf("%-12s %d docs: build+first query %v | reopen %v, first ANN %v, ANN %v/query, exact %v/query, recall@10 %.3f, heap %.1f MB, graph %.1f MB, %d segments %.1f MB",
		name, len(docs), build.Round(time.Millisecond), opened.Round(time.Millisecond), first.Round(time.Microsecond),
		(annTime / time.Duration(len(qs))).Round(time.Microsecond), (exactTime / time.Duration(len(qs))).Round(time.Microsecond),
		float64(hits)/float64(10*len(qs)), float64(held)/1e6,
		float64(diskBytes(t, path, ".hnsw"))/1e6, countItems(t, path, ".seg"), float64(diskBytes(t, path, ".seg"))/1e6)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

// annBuild indexes docs and builds and persists their graphs with one ANN
// query, returning its duration.
func annBuild(t *testing.T, config Config, docs [][]float32, query []float32, params ANNParams) time.Duration {
	t.Helper()
	w, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}
	for start := 0; start < len(docs); start += 10000 {
		b := NewBatch()
		for i := start; i < min(start+10000, len(docs)); i++ {
			d := vectorDocument(t, fmt.Sprint(i), docs[i])
			b.Update(d.ID(), d)
		}
		if err := w.Batch(b); err != nil {
			t.Fatal(err)
		}
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := r.SearchVectorsANN(context.Background(), "v", query, 10, Cosine, params, nil); err != nil {
		t.Fatal(err)
	}
	build := time.Since(start)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return build
}

func diskBytes(t *testing.T, root, suffix string) (total int64) {
	t.Helper()
	walkItems(t, root, suffix, func(info fs.FileInfo) { total += info.Size() })
	return total
}

func countItems(t *testing.T, root, suffix string) (n int) {
	t.Helper()
	walkItems(t, root, suffix, func(fs.FileInfo) { n++ })
	return n
}

func walkItems(t *testing.T, root, suffix string, fn func(fs.FileInfo)) {
	t.Helper()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, suffix) {
			return err
		}
		info, err := d.Info()
		if err == nil {
			fn(info)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
