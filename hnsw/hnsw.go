// Copyright (c) 2026 The Bluge Authors.
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

// Package hnsw implements an in-memory Hierarchical Navigable Small World
// graph (Malkov & Yashunin) for approximate nearest neighbor search over
// float32 vectors. Scores use the same arithmetic as vec.Score, so a document
// found approximately carries exactly the score exact search would give it.
package hnsw

import (
	"container/heap"
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"sync"

	"github.com/vcaesar/ice/vec"
)

const (
	DefaultM              = 16
	DefaultEfConstruction = 200
	DefaultEfSearch       = 100

	// A fixed seed keeps graphs, and therefore results, reproducible for the
	// same insertion order.
	seed = 0x5eed
)

// Params tunes graph construction and search. Zero fields take the defaults.
type Params struct {
	// M is the maximum number of neighbors per node above level 0; level 0
	// keeps 2*M. Larger values raise recall and memory. Minimum 2.
	M int
	// EfConstruction is the candidate list size while inserting.
	EfConstruction int
	// EfSearch is the candidate list size while searching; k raises it when larger.
	EfSearch int
}

// WithDefaults fills zero fields.
func (p Params) WithDefaults() Params {
	if p.M == 0 {
		p.M = DefaultM
	}
	if p.EfConstruction == 0 {
		p.EfConstruction = DefaultEfConstruction
	}
	if p.EfSearch == 0 {
		p.EfSearch = DefaultEfSearch
	}
	return p
}

// Validate rejects negative values and M below 2.
func (p Params) Validate() error {
	if p.M < 0 || (p.M > 0 && p.M < 2) {
		return fmt.Errorf("hnsw M must be 0 (default) or at least 2, got %d", p.M)
	}
	if p.EfConstruction < 0 || p.EfSearch < 0 {
		return fmt.Errorf("hnsw ef parameters must not be negative")
	}
	return nil
}

type cand struct {
	node  uint32
	score float64
}

// better orders by descending score, then ascending node for determinism.
func better(a, b cand) bool {
	return a.score > b.score || (a.score == b.score && a.node < b.node)
}

// candHeap is a max-heap of candidates when best is set, else a min-heap.
type candHeap struct {
	items []cand
	best  bool
}

func (h *candHeap) Len() int { return len(h.items) }
func (h *candHeap) Less(i, j int) bool {
	if h.best {
		return better(h.items[i], h.items[j])
	}
	return better(h.items[j], h.items[i])
}
func (h *candHeap) Swap(i, j int)      { h.items[i], h.items[j] = h.items[j], h.items[i] }
func (h *candHeap) Push(x interface{}) { h.items = append(h.items, x.(cand)) }
func (h *candHeap) Pop() interface{} {
	n := len(h.items)
	x := h.items[n-1]
	h.items = h.items[:n-1]
	return x
}
func (h *candHeap) top() cand { return h.items[0] }

// visitedSet marks nodes with an epoch so reuse costs nothing.
type visitedSet struct {
	marks []uint32
	epoch uint32
}

func (v *visitedSet) reset(n int) {
	if len(v.marks) < n {
		v.marks = append(v.marks, make([]uint32, n-len(v.marks))...)
	}
	v.epoch++
	if v.epoch == 0 {
		clear(v.marks)
		v.epoch = 1
	}
}

// visit reports whether node was unvisited and marks it.
func (v *visitedSet) visit(node uint32) bool {
	if v.marks[node] == v.epoch {
		return false
	}
	v.marks[node] = v.epoch
	return true
}

// Graph holds vectors and their navigable layers. Add is not safe for
// concurrent use; once building is done, Search is safe for concurrent use.
type Graph struct {
	metric   vec.Metric
	m, m0    int
	efCons   int
	efSearch int
	levelMul float64
	dim      int

	data  []float32 // node-major, dim values per node
	norms []float64 // cosine only: L2 norm per node
	ids   []uint64  // node → document number
	links [][][]uint32

	entry    uint32
	maxLevel int
	rng      *rand.Rand
	visited  sync.Pool
}

// New creates an empty graph for metric.
func New(metric vec.Metric, params Params) (*Graph, error) {
	switch metric {
	case vec.Cosine, vec.DotProduct, vec.L2:
	default:
		return nil, fmt.Errorf("unsupported vector metric %q", metric)
	}
	if err := params.Validate(); err != nil {
		return nil, err
	}
	params = params.WithDefaults()
	return &Graph{
		metric:   metric,
		m:        params.M,
		m0:       2 * params.M,
		efCons:   params.EfConstruction,
		efSearch: params.EfSearch,
		levelMul: 1 / math.Log(float64(params.M)),
		rng:      rand.New(rand.NewPCG(seed, seed)), // #nosec G404 -- levels need reproducibility, not secrecy.
		visited:  sync.Pool{New: func() interface{} { return &visitedSet{} }},
	}, nil
}

// Len returns the number of vectors.
func (g *Graph) Len() int { return len(g.ids) }

// Add inserts vector for document id. Every vector must share one dimension;
// the same id may be added more than once.
func (g *Graph) Add(id uint64, vector []float32) error {
	if err := vec.Validate(vector, g.metric); err != nil {
		return err
	}
	if len(g.ids) == 0 {
		g.dim = len(vector)
	} else if len(vector) != g.dim {
		return fmt.Errorf("vector dimension mismatch: graph %d, vector %d", g.dim, len(vector))
	}
	if len(g.ids) == math.MaxUint32 {
		return fmt.Errorf("hnsw graph is full")
	}
	node := uint32(len(g.ids)) // #nosec G115 -- bounded above.
	level := int(math.Floor(-math.Log(1-g.rng.Float64()) * g.levelMul))
	g.ids = append(g.ids, id)
	g.data = append(g.data, vector...)
	if g.metric == vec.Cosine {
		g.norms = append(g.norms, norm(vector))
	}
	links := make([][]uint32, level+1)
	g.links = append(g.links, links)
	if node == 0 {
		g.maxLevel = level
		return nil
	}
	q, qn := g.vector(node), g.norm(node)
	ep := g.entry
	for l := g.maxLevel; l > level; l-- {
		ep = g.greedy(q, qn, ep, l)
	}
	visited := g.visited.Get().(*visitedSet)
	defer g.visited.Put(visited)
	for l := min(level, g.maxLevel); l >= 0; l-- {
		cands := g.searchLayer(q, qn, ep, g.efCons, l, nil, visited)
		neighbors := g.selectNeighbors(cands, g.m)
		links[l] = neighbors
		mmax := g.mmax(l)
		for _, n := range neighbors {
			g.links[n][l] = append(g.links[n][l], node)
			if len(g.links[n][l]) > mmax {
				g.shrink(n, l, mmax)
			}
		}
		if len(cands) > 0 {
			ep = cands[0].node
		}
	}
	if level > g.maxLevel {
		g.entry, g.maxLevel = node, level
	}
	return nil
}

// Search returns up to k documents, best first with ties by ascending id;
// repeated ids keep their best score. ef bounds the candidate list (0 uses
// the graph's EfSearch; k raises it). accept optionally filters results
// without limiting traversal, so selective filters still explore the graph.
func (g *Graph) Search(query []float32, k, ef int, accept func(uint64) bool) ([]vec.Match, error) {
	if k <= 0 {
		return nil, fmt.Errorf("vector search k must be positive")
	}
	if err := vec.Validate(query, g.metric); err != nil {
		return nil, err
	}
	if len(g.ids) == 0 {
		return nil, nil
	}
	if len(query) != g.dim {
		return nil, fmt.Errorf("vector dimension mismatch: query %d, vector %d", len(query), g.dim)
	}
	if ef <= 0 {
		ef = g.efSearch
	}
	ef = max(ef, k)
	var qn float64
	if g.metric == vec.Cosine {
		qn = norm(query)
	}
	ep := g.entry
	for l := g.maxLevel; l > 0; l-- {
		ep = g.greedy(query, qn, ep, l)
	}
	visited := g.visited.Get().(*visitedSet)
	defer g.visited.Put(visited)
	cands := g.searchLayer(query, qn, ep, ef, 0, accept, visited)
	matches := make([]vec.Match, 0, min(k, len(cands)))
	seen := make(map[uint64]struct{}, len(cands))
	for _, c := range cands { // best first, so the first hit per id is its best
		id := g.ids[c.node]
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		matches = append(matches, vec.Match{Number: id, Score: c.score})
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score ||
			(matches[i].Score == matches[j].Score && matches[i].Number < matches[j].Number)
	})
	if len(matches) > k {
		matches = matches[:k]
	}
	return matches, nil
}

func (g *Graph) mmax(level int) int {
	if level == 0 {
		return g.m0
	}
	return g.m
}

func (g *Graph) vector(node uint32) []float32 {
	return g.data[int(node)*g.dim : int(node+1)*g.dim]
}

func (g *Graph) norm(node uint32) float64 {
	if g.metric == vec.Cosine {
		return g.norms[node]
	}
	return 0
}

func norm(vector []float32) float64 {
	var sum float64
	for _, v := range vector {
		sum += float64(v) * float64(v)
	}
	return math.Sqrt(sum)
}

// score mirrors vec.Score, with the query norm precomputed.
func (g *Graph) score(q []float32, qn float64, node uint32) float64 {
	v := g.vector(node)
	var acc float64
	switch g.metric {
	case vec.L2:
		for i, a := range q {
			d := float64(a) - float64(v[i])
			acc += d * d
		}
		return -acc
	case vec.Cosine:
		for i, a := range q {
			acc += float64(a) * float64(v[i])
		}
		return math.Max(-1, math.Min(1, acc/(qn*g.norms[node])))
	default:
		for i, a := range q {
			acc += float64(a) * float64(v[i])
		}
		return acc
	}
}

// greedy descends to the local best node on one layer.
func (g *Graph) greedy(q []float32, qn float64, ep uint32, level int) uint32 {
	cur, best := ep, g.score(q, qn, ep)
	for changed := true; changed; {
		changed = false
		for _, n := range g.links[cur][level] {
			if s := g.score(q, qn, n); s > best {
				cur, best, changed = n, s, true
			}
		}
	}
	return cur
}

// searchLayer is a beam search on one layer returning up to ef results, best
// first. Filtered nodes are traversed but not returned; the beam only stops
// once ef accepted results are better than every remaining candidate.
func (g *Graph) searchLayer(q []float32, qn float64, ep uint32, ef, level int,
	accept func(uint64) bool, visited *visitedSet) []cand {
	visited.reset(len(g.ids))
	visited.visit(ep)
	cands := &candHeap{best: true}
	results := &candHeap{}
	start := cand{node: ep, score: g.score(q, qn, ep)}
	heap.Push(cands, start)
	if accept == nil || accept(g.ids[ep]) {
		heap.Push(results, start)
	}
	for cands.Len() > 0 {
		c := heap.Pop(cands).(cand)
		if results.Len() >= ef && c.score < results.top().score {
			break
		}
		for _, n := range g.links[c.node][level] {
			if !visited.visit(n) {
				continue
			}
			next := cand{node: n, score: g.score(q, qn, n)}
			if results.Len() >= ef && next.score <= results.top().score {
				continue
			}
			heap.Push(cands, next)
			if accept == nil || accept(g.ids[n]) {
				heap.Push(results, next)
				if results.Len() > ef {
					heap.Pop(results)
				}
			}
		}
	}
	out := results.items
	sort.Slice(out, func(i, j int) bool { return better(out[i], out[j]) })
	return out
}

// selectNeighbors applies the diversity heuristic: a candidate is kept only
// when no already kept neighbor is closer to it than the query is.
func (g *Graph) selectNeighbors(cands []cand, m int) []uint32 {
	out := make([]uint32, 0, min(m, len(cands)))
	for _, c := range cands {
		if len(out) == m {
			break
		}
		cv, cn := g.vector(c.node), g.norm(c.node)
		keep := true
		for _, o := range out {
			if g.score(cv, cn, o) > c.score {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, c.node)
		}
	}
	return out
}

// shrink re-selects node's neighbors on level to at most mmax.
func (g *Graph) shrink(node uint32, level, mmax int) {
	nv, nn := g.vector(node), g.norm(node)
	cands := make([]cand, 0, len(g.links[node][level]))
	for _, n := range g.links[node][level] {
		cands = append(cands, cand{node: n, score: g.score(nv, nn, n)})
	}
	sort.Slice(cands, func(i, j int) bool { return better(cands[i], cands[j]) })
	g.links[node][level] = g.selectNeighbors(cands, mmax)
}
