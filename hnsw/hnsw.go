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

// Package hnsw implements an in-memory Hierarchical Navigable Small World
// graph (Malkov & Yashunin) for approximate nearest neighbor search over
// float32 vectors. Scores use the same arithmetic as vec.Score, so a document
// found approximately carries exactly the score exact search would give it.
package hnsw

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
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

// Validate rejects negative values, M below 2, and overflow of the 2*M level-0 limit.
func (p Params) Validate() error {
	if p.M < 0 || (p.M > 0 && p.M < 2) {
		return fmt.Errorf("hnsw M must be 0 (default) or at least 2, got %d", p.M)
	}
	if p.M > math.MaxInt/2 {
		return fmt.Errorf("hnsw M must not exceed %d, got %d", math.MaxInt/2, p.M)
	}
	if p.EfConstruction < 0 || p.EfSearch < 0 {
		return fmt.Errorf("hnsw ef parameters must not be negative")
	}
	return nil
}

// cand fields are ordered to pack into 16 bytes.
type cand struct {
	score    float64
	node     uint32
	rejected bool
}

// candHeap keeps the best candidate on top when best is set, else the worst.
// It is typed to avoid container/heap boxing in the search loop.
type candHeap struct {
	g     *Graph
	items []cand
	best  bool
	ids   map[uint64]int // document id → heap index; nil without deduplication
}

func (h *candHeap) Len() int  { return len(h.items) }
func (h *candHeap) top() cand { return h.items[0] }

// before reports whether a belongs above b. It stays small enough to
// inline; ties are rare and go out of line.
func (h *candHeap) before(a, b cand) bool {
	if a.score != b.score {
		return (a.score > b.score) == h.best
	}
	return h.tie(a, b)
}

//go:noinline
func (h *candHeap) tie(a, b cand) bool {
	if h.best {
		return h.g.better(a, b)
	}
	return h.g.better(b, a)
}

func (h *candHeap) push(c cand) {
	h.items = append(h.items, c)
	h.up(len(h.items) - 1)
}

func (h *candHeap) pop() cand {
	n := len(h.items) - 1
	top := h.items[0]
	if h.ids != nil {
		delete(h.ids, h.g.ids[top.node])
	}
	h.items[0] = h.items[n]
	h.items = h.items[:n]
	if n > 0 {
		h.down(0)
	}
	return top
}

// fix restores heap order after items[i] changed.
func (h *candHeap) fix(i int) {
	if !h.down(i) {
		h.up(i)
	}
}

func (h *candHeap) set(i int, c cand) {
	h.items[i] = c
	if h.ids != nil {
		h.ids[h.g.ids[c.node]] = i
	}
}

// up and down move a hole instead of swapping, writing each slot once.
func (h *candHeap) up(i int) {
	items, x := h.items, h.items[i]
	for i > 0 {
		p := (i - 1) / 2
		if !h.before(x, items[p]) {
			break
		}
		h.set(i, items[p])
		i = p
	}
	h.set(i, x)
}

func (h *candHeap) down(i int) bool {
	items, x := h.items, h.items[i]
	start, n := i, len(items)
	for {
		j := 2*i + 1
		if j >= n {
			break
		}
		if r := j + 1; r < n && h.before(items[r], items[j]) {
			j = r
		}
		if !h.before(items[j], x) {
			break
		}
		h.set(i, items[j])
		i = j
	}
	h.set(i, x)
	return i > start
}

// keep adds c to the result beam bounded by ef. With ids set, the beam holds
// one entry per document id, its best, so repeated vectors cannot starve it.
func (h *candHeap) keep(c cand, ef int) {
	if h.ids != nil {
		if i, dup := h.ids[h.g.ids[c.node]]; dup {
			if h.g.better(c, h.items[i]) {
				h.items[i] = c
				h.fix(i)
			}
			return
		}
	}
	if len(h.items) < ef {
		h.push(c)
		return
	}
	if !h.g.better(c, h.top()) {
		return
	}
	if h.ids != nil {
		delete(h.ids, h.g.ids[h.top().node])
	}
	h.items[0] = c
	h.down(0)
}

// visitedSet marks nodes with an epoch so reuse costs nothing. It also
// carries the reusable per-search buffers.
type visitedSet struct {
	marks   []uint32
	epoch   uint32
	cands   []cand
	results []cand
	ids     map[uint64]int
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

// resultIDs returns an empty id-to-heap-index map for deduplicating results, or nil.
func (v *visitedSet) resultIDs(unique bool) map[uint64]int {
	if !unique {
		return nil
	}
	if v.ids == nil {
		v.ids = make(map[uint64]int)
	}
	clear(v.ids)
	return v.ids
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
	pcg      *rand.PCG // rng's source, persisted so Add after Read stays reproducible
	rng      *rand.Rand
	visited  sync.Pool
	scratch  []cand // shrink buffer; Add is not concurrent
	// repeated is set once an id is not above the previous one, so search
	// must deduplicate ids in its beam.
	repeated bool
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
	pcg := rand.NewPCG(seed, seed)
	return &Graph{
		metric:   metric,
		m:        params.M,
		m0:       2 * params.M,
		efCons:   params.EfConstruction,
		efSearch: params.EfSearch,
		levelMul: 1 / math.Log(float64(params.M)),
		pcg:      pcg,
		rng:      rand.New(pcg), // #nosec G404 -- levels need reproducibility, not secrecy.
		visited:  sync.Pool{New: func() any { return &visitedSet{} }},
	}, nil
}

// Len returns the number of vectors.
func (g *Graph) Len() int { return len(g.ids) }

// Metric returns the graph's similarity metric.
func (g *Graph) Metric() vec.Metric { return g.metric }

// Params returns the graph's parameters with defaults applied.
func (g *Graph) Params() Params {
	return Params{M: g.m, EfConstruction: g.efCons, EfSearch: g.efSearch}
}

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
	if uint64(len(g.ids)) >= math.MaxUint32 {
		return fmt.Errorf("hnsw graph is full")
	}
	node := uint32(len(g.ids)) // #nosec G115 -- bounded above.
	level := int(math.Floor(-math.Log(1-g.rng.Float64()) * g.levelMul))
	if node > 0 && id <= g.ids[node-1] {
		g.repeated = true
	}
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
		var err error
		ep, err = g.greedy(context.Background(), q, qn, ep, l)
		if err != nil {
			return err
		}
	}
	visited := g.visited.Get().(*visitedSet)
	defer g.visited.Put(visited)
	for l := min(level, g.maxLevel); l >= 0; l-- {
		cands, err := g.searchLayer(context.Background(), q, qn, ep, g.efCons, l, nil, false, visited)
		if err != nil {
			return err
		}
		mmax := g.mmax(l)
		// Bound the reservation by the candidates, leaving room for one backlink.
		neighbors := g.selectNeighbors(make([]uint32, 0, min(mmax, len(cands))+1), cands, g.m)
		links[l] = neighbors
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
// with the traversal semantics documented by SearchContext.
func (g *Graph) Search(query []float32, k, ef int, accept func(uint64) bool) ([]vec.Match, error) {
	return g.SearchContext(context.Background(), query, k, ef, accept)
}

// SearchContext is Search with cancellation during descent and layer expansion.
// Discovered rejected nodes are always expanded as bridges, even with a full
// accepted beam. Accepted nodes remain score-pruned, so search is approximate;
// selective filters may traverse an entire connected rejected region.
func (g *Graph) SearchContext(ctx context.Context, query []float32, k, ef int,
	accept func(uint64) bool) ([]vec.Match, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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
		var err error
		ep, err = g.greedy(ctx, query, qn, ep, l)
		if err != nil {
			return nil, err
		}
	}
	visited := g.visited.Get().(*visitedSet)
	defer g.visited.Put(visited)
	cands, err := g.searchLayer(ctx, query, qn, ep, ef, 0, accept, g.repeated, visited)
	if err != nil {
		return nil, err
	}
	// cands holds unique ids ordered by score, then id.
	cands = cands[:min(k, len(cands))]
	matches := make([]vec.Match, len(cands))
	for i, c := range cands {
		matches[i] = vec.Match{Number: g.ids[c.node], Score: c.score}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return matches, nil
}

// better orders by descending score, then ascending document id like exact
// search, then ascending node for determinism.
func (g *Graph) better(a, b cand) bool {
	if a.score != b.score {
		return a.score > b.score
	}
	if ia, ib := g.ids[a.node], g.ids[b.node]; ia != ib {
		return ia < ib
	}
	return a.node < b.node
}

func (g *Graph) compare(a, b cand) int {
	if g.better(a, b) {
		return -1
	}
	if g.better(b, a) {
		return 1
	}
	return 0
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
	switch g.metric {
	case vec.L2:
		return -squaredDistance(q, v)
	case vec.Cosine:
		// Same result as math.Max(-1, math.Min(1, x)) for every x, including NaN.
		x := dot(q, v) / (qn * g.norms[node])
		if x > 1 {
			return 1
		}
		if x < -1 {
			return -1
		}
		return x
	default:
		return dot(q, v)
	}
}

// dot and squaredDistance unroll without reordering the float64 additions,
// so results stay bit-identical to vec.Score. len(v) must be at least len(q).
func dot(q, v []float32) float64 {
	v = v[:len(q)]
	var acc float64
	i := 0
	for ; i+4 <= len(q); i += 4 {
		acc += float64(q[i]) * float64(v[i])
		acc += float64(q[i+1]) * float64(v[i+1])
		acc += float64(q[i+2]) * float64(v[i+2])
		acc += float64(q[i+3]) * float64(v[i+3])
	}
	for ; i < len(q); i++ {
		acc += float64(q[i]) * float64(v[i])
	}
	return acc
}

func squaredDistance(q, v []float32) float64 {
	v = v[:len(q)]
	var acc float64
	i := 0
	for ; i+4 <= len(q); i += 4 {
		d0 := float64(q[i]) - float64(v[i])
		acc += d0 * d0
		d1 := float64(q[i+1]) - float64(v[i+1])
		acc += d1 * d1
		d2 := float64(q[i+2]) - float64(v[i+2])
		acc += d2 * d2
		d3 := float64(q[i+3]) - float64(v[i+3])
		acc += d3 * d3
	}
	for ; i < len(q); i++ {
		d := float64(q[i]) - float64(v[i])
		acc += d * d
	}
	return acc
}

// greedy descends to the local best node on one layer.
func (g *Graph) greedy(ctx context.Context, q []float32, qn float64, ep uint32, level int) (uint32, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	cur, best := ep, g.score(q, qn, ep)
	for changed := true; changed; {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		changed = false
		for _, n := range g.links[cur][level] {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			if s := g.score(q, qn, n); s > best {
				cur, best, changed = n, s, true
			}
		}
	}
	return cur, ctx.Err()
}

// searchLayer is a beam search on one layer returning up to ef results, best
// first. Rejected candidates bypass beam pruning; accepted candidates below
// the beam are skipped rather than stopping traversal of pending bridges.
// unique keeps one result per id. The result aliases visited's buffers and
// is valid until visited is reused.
func (g *Graph) searchLayer(ctx context.Context, q []float32, qn float64, ep uint32, ef, level int,
	accept func(uint64) bool, unique bool, visited *visitedSet) ([]cand, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	visited.reset(len(g.ids))
	visited.visit(ep)
	cands := &candHeap{g: g, items: visited.cands[:0], best: true}
	results := &candHeap{g: g, items: visited.results[:0], ids: visited.resultIDs(unique)}
	defer func() { visited.cands, visited.results = cands.items[:0], results.items }()
	start := cand{node: ep, score: g.score(q, qn, ep)}
	start.rejected = accept != nil && !accept(g.ids[ep])
	cands.push(start)
	if !start.rejected {
		results.keep(start, ef)
	}
	for cands.Len() > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c := cands.pop()
		if !c.rejected && results.Len() >= ef && c.score < results.top().score {
			if accept == nil {
				break
			}
			continue
		}
		for _, n := range g.links[c.node][level] {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !visited.visit(n) {
				continue
			}
			next := cand{node: n, score: g.score(q, qn, n)}
			next.rejected = accept != nil && !accept(g.ids[n])
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !next.rejected && results.Len() >= ef && !g.better(next, results.top()) {
				continue
			}
			cands.push(next)
			if !next.rejected {
				results.keep(next, ef)
			}
		}
	}
	slices.SortFunc(results.items, g.compare)
	return results.items, ctx.Err()
}

// selectNeighbors applies the diversity heuristic: a candidate is kept only
// when no already kept neighbor is closer to it than the query is. Kept
// nodes are appended to out, which must not alias cands.
func (g *Graph) selectNeighbors(out []uint32, cands []cand, m int) []uint32 {
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
	links := g.links[node][level]
	cands := g.scratch[:0]
	for _, n := range links {
		cands = append(cands, cand{node: n, score: g.score(nv, nn, n)})
	}
	slices.SortFunc(cands, g.compare)
	g.scratch = cands
	// cands holds copies, so the kept nodes can overwrite links in place.
	g.links[node][level] = g.selectNeighbors(links[:0], cands, mmax)
}
