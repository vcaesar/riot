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

package hnsw

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/vcaesar/ice/vec"
)

// builder guards a graph while Build inserts concurrently. Each lock is held
// alone and briefly, so locks never nest.
type builder struct {
	top   sync.RWMutex // entry and maxLevel
	nodes []sync.Mutex // links of each node
}

// Build returns a graph of the vectors in data, node-major, for document ids
// in order, inserted by up to workers goroutines. Node levels are drawn in
// order, as by Add. With one worker the graph equals Adding the vectors in
// order; with more, nodes are inserted in a fixed shuffled order and links
// depend on scheduling, so graphs differ between runs with the same quality.
// The graph keeps ids and data. Cancellation of ctx stops the build with its
// error.
func Build(ctx context.Context, metric vec.Metric, params Params, ids []uint64, data []float32,
	workers int) (*Graph, error) {
	g, err := New(metric, params)
	if err != nil {
		return nil, err
	}
	if err := g.prepare(ids, data); err != nil {
		return nil, err
	}
	pending := make([]uint32, 0, max(len(ids)-1, 0))
	for node := 1; node < len(ids); node++ {
		pending = append(pending, uint32(node)) // #nosec G115 -- bounded by prepare.
	}
	if err := g.insertAll(ctx, pending, workers); err != nil {
		return nil, err
	}
	return g, nil
}

// BuildFrom is Build reusing base, a graph of a subset of the vectors, as
// Lucene does when merging segments: mapping[i] is the node in ids and data
// of base's node i, whose vector must be equal. Base's links are copied and
// only the other nodes are inserted, so the cost is that of the inserts. The
// graph takes base's metric and parameters; base is not modified.
func BuildFrom(ctx context.Context, base *Graph, mapping []uint32, ids []uint64, data []float32,
	workers int) (*Graph, error) {
	g, err := New(base.metric, base.Params())
	if err != nil {
		return nil, err
	}
	if err := g.prepare(ids, data); err != nil {
		return nil, err
	}
	if err := g.seed(base, mapping); err != nil {
		return nil, err
	}
	pending := make([]uint32, 0, len(ids)-len(mapping))
	for node := range g.links {
		if g.links[node] != nil {
			continue
		}
		g.links[node] = make([][]uint32, g.randomLevel()+1)
		pending = append(pending, uint32(node)) // #nosec G115 -- bounded by prepare.
	}
	if err := g.insertAll(ctx, pending, workers); err != nil {
		return nil, err
	}
	return g, nil
}

// seed copies base's links into g through mapping and leaves the other
// nodes without levels.
func (g *Graph) seed(base *Graph, mapping []uint32) error {
	if len(mapping) != base.Len() || base.Len() == 0 {
		return fmt.Errorf("%d mapped nodes for a base graph of %d", len(mapping), base.Len())
	}
	if base.dim != g.dim {
		return fmt.Errorf("base graph dimension %d, vectors %d", base.dim, g.dim)
	}
	g.links = make([][][]uint32, len(g.ids))
	for old, node := range mapping {
		if int(node) >= len(g.ids) || g.links[node] != nil {
			return fmt.Errorf("base node %d maps to invalid or repeated node %d", old, node)
		}
		if !slices.Equal(base.vector(uint32(old)), g.vector(node)) { // #nosec G115 -- bounded by base.Len.
			return fmt.Errorf("base node %d and node %d hold different vectors", old, node)
		}
		levels := make([][]uint32, len(base.links[old]))
		for l, links := range base.links[old] {
			levels[l] = make([]uint32, len(links), min(len(links)+1, g.mmax(l)+1))
			for i, n := range links {
				levels[l][i] = mapping[n]
			}
		}
		g.links[node] = levels
	}
	g.entry, g.maxLevel = mapping[base.entry], base.maxLevel
	return nil
}

// insertAll inserts pending nodes, serially in order for one worker, else
// concurrently in a fixed shuffled order.
func (g *Graph) insertAll(ctx context.Context, pending []uint32, workers int) error {
	if workers <= 1 || len(pending) < 2 {
		for _, node := range pending {
			if err := g.insert(ctx, node, &g.scratch); err != nil {
				return err
			}
		}
		return nil
	}
	g.build = &builder{nodes: make([]sync.Mutex, len(g.ids))}
	defer func() { g.build = nil }()
	// A shuffled order keeps similar vectors, often adjacent in document
	// order, out of concurrent inserts that cannot see each other.
	order := slices.Clone(pending)
	rand.New(rand.NewPCG(seed, seed)).Shuffle(len(order), func(i, j int) { // #nosec G404 -- order needs no secrecy.
		order[i], order[j] = order[j], order[i]
	})
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var next atomic.Int64
	var wg sync.WaitGroup
	for range min(workers, len(order)) {
		wg.Go(func() {
			var scratch []cand
			for ctx.Err() == nil {
				i := next.Add(1) - 1
				if i >= int64(len(order)) {
					return
				}
				if err := g.insert(ctx, order[i], &scratch); err != nil {
					cancel(err)
					return
				}
			}
		})
	}
	wg.Wait()
	return context.Cause(ctx)
}

// prepare sets every node's id, vector and levels, linking none but the
// first, which becomes the entry.
func (g *Graph) prepare(ids []uint64, data []float32) error {
	if len(ids) == 0 {
		if len(data) != 0 {
			return fmt.Errorf("%d values without ids", len(data))
		}
		return nil
	}
	if uint64(len(ids)) > math.MaxUint32 {
		return fmt.Errorf("hnsw graph is full")
	}
	if len(data) == 0 || len(data)%len(ids) != 0 {
		return fmt.Errorf("%d values do not split into %d vectors", len(data), len(ids))
	}
	g.dim, g.ids, g.data = len(data)/len(ids), ids, data
	g.links = make([][][]uint32, len(ids))
	g.reserveCodes(len(ids))
	for node := range uint32(len(ids)) { // #nosec G115 -- bounded above.
		vector := g.vector(node)
		if err := vec.Validate(vector, g.metric); err != nil {
			return fmt.Errorf("node %d: %w", node, err)
		}
		if g.metric == vec.Cosine {
			g.norms = append(g.norms, norm(vector))
		}
		g.addCodes(vector)
		if node > 0 && ids[node] <= ids[node-1] {
			g.repeated = true
		}
		g.links[node] = make([][]uint32, g.randomLevel()+1)
	}
	g.entry, g.maxLevel = 0, len(g.links[0])-1
	return nil
}

func (g *Graph) top() (entry uint32, maxLevel int) {
	if g.build != nil {
		g.build.top.RLock()
		defer g.build.top.RUnlock()
	}
	return g.entry, g.maxLevel
}

// raise makes node the entry when its level is above the graph's.
func (g *Graph) raise(node uint32, level int) {
	if g.build != nil {
		g.build.top.Lock()
		defer g.build.top.Unlock()
	}
	if level > g.maxLevel {
		g.entry, g.maxLevel = node, level
	}
}

func (g *Graph) lock(node uint32) {
	if g.build != nil {
		g.build.nodes[node].Lock()
	}
}

func (g *Graph) unlock(node uint32) {
	if g.build != nil {
		g.build.nodes[node].Unlock()
	}
}

// neighbors returns node's links on level. During a concurrent build they
// are copied into visited's buffer, valid until the next call.
func (g *Graph) neighbors(node uint32, level int, visited *visitedSet) []uint32 {
	if g.build == nil {
		return g.links[node][level]
	}
	g.lock(node)
	visited.links = append(visited.links[:0], g.links[node][level]...)
	g.unlock(node)
	return visited.links
}
