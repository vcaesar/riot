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

// Package hnsw implements a Hierarchical Navigable Small World graph
// (Malkov & Yashunin) for approximate nearest neighbor search over float32
// vectors. By default graphs are built and traversed with int8 codes
// (QuantizationInt8), and search rescores its candidates with the float32
// vectors using the same arithmetic as vec.Score, so a document found
// approximately carries exactly the score exact search would give it. Graphs
// live on the heap, or are used in place from memory such as a mapped file
// with WriteMappedTo and Map.
package hnsw

import (
	"context"
	"errors"
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

	// scanMinVisits is how many nodes a filtered search visits before it may
	// switch to an exact scan, so the accepted share is estimated from a sample.
	scanMinVisits = 64
	// scanCheckEvery spaces cancellation checks during an exact scan.
	scanCheckEvery = 1024
)

// errScan stops a filtered graph search once an exact scan is cheaper.
var errScan = errors.New("hnsw: exact scan is cheaper")

// Params tunes graph construction and search. Zero fields take the defaults.
type Params struct {
	// M is the maximum number of neighbors per node above level 0; level 0
	// keeps 2*M. Larger values raise recall and memory. Minimum 2.
	M int
	// EfConstruction is the candidate list size while inserting.
	EfConstruction int
	// EfSearch is the candidate list size while searching; k raises it when larger.
	EfSearch int
	// Quantization selects the vectors compared while building and
	// traversing; the zero value is QuantizationInt8.
	Quantization Quantization
	// ConcurrentAccept declares a search filter safe for concurrent use, so
	// an index searching segments in parallel calls it without a shared
	// lock. Graph itself ignores it; it is not persisted.
	ConcurrentAccept bool
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
	if p.Quantization > QuantizationNone {
		return fmt.Errorf("unsupported hnsw quantization %v", p.Quantization)
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
	links   []uint32 // neighbor copies during a concurrent build
	codes   []int8   // the quantized query
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

	quant   Quantization
	codes   []int8    // int8: node-major codes
	scales  []float32 // int8: value of code 1 per node
	sqNorms []float64 // int8 L2: squared L2 norm per node

	entry    uint32
	maxLevel int
	pcg      *rand.PCG // rng's source, persisted so Add after Read stays reproducible
	rng      *rand.Rand
	visited  sync.Pool
	scratch  []cand // shrink buffer; Add is not concurrent
	build    *builder
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
		quant:    params.Quantization,
		levelMul: 1 / math.Log(float64(params.M)),
		pcg:      pcg,
		rng:      rand.New(pcg), // #nosec G404 -- levels need reproducibility, not secrecy.
		visited:  sync.Pool{New: func() any { return &visitedSet{} }},
	}, nil
}

// Len returns the number of vectors.
func (g *Graph) Len() int { return len(g.ids) }

// IDs returns the document id of each node, in node order. It must not be modified.
func (g *Graph) IDs() []uint64 { return g.ids }

// Metric returns the graph's similarity metric.
func (g *Graph) Metric() vec.Metric { return g.metric }

// Params returns the graph's parameters with defaults applied.
func (g *Graph) Params() Params {
	return Params{M: g.m, EfConstruction: g.efCons, EfSearch: g.efSearch, Quantization: g.quant}
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
	if node > 0 && id <= g.ids[node-1] {
		g.repeated = true
	}
	g.ids = append(g.ids, id)
	g.data = append(g.data, vector...)
	if g.metric == vec.Cosine {
		g.norms = append(g.norms, norm(vector))
	}
	g.addCodes(vector)
	g.links = append(g.links, make([][]uint32, g.randomLevel()+1))
	if node == 0 {
		g.maxLevel = len(g.links[0]) - 1
		return nil
	}
	return g.insert(context.Background(), node, &g.scratch)
}

func (g *Graph) randomLevel() int {
	return int(math.Floor(-math.Log(1-g.rng.Float64()) * g.levelMul))
}

// insert links node, whose vector and levels are set, into the graph. During
// a concurrent build each caller passes its own scratch.
func (g *Graph) insert(ctx context.Context, node uint32, scratch *[]cand) error {
	links := g.links[node]
	level := len(links) - 1
	q := g.nodeQuery(node)
	ep, maxLevel := g.top()
	visited := g.visited.Get().(*visitedSet)
	defer g.visited.Put(visited)
	for l := maxLevel; l > level; l-- {
		var err error
		ep, err = g.greedy(ctx, &q, ep, l, visited)
		if err != nil {
			return err
		}
	}
	for l := min(level, maxLevel); l >= 0; l-- {
		cands, err := g.searchLayer(ctx, &q, ep, g.efCons, l, nil, false, visited)
		if err != nil {
			return err
		}
		mmax := g.mmax(l)
		// Bound the reservation by the candidates, leaving room for one backlink.
		neighbors := g.selectNeighbors(make([]uint32, 0, min(mmax, len(cands))+1), cands, g.m)
		g.lock(node)
		// Concurrent inserts that reached node through a higher level may
		// have linked back already; keep those links.
		early := links[l]
		links[l] = neighbors
		if g.build != nil {
			// Once published, other inserts may shrink the links in place.
			neighbors = slices.Clone(neighbors)
			for _, n := range early {
				if !slices.Contains(neighbors, n) {
					links[l] = append(links[l], n)
				}
			}
			if len(links[l]) > mmax {
				g.shrink(node, l, mmax, scratch)
			}
		}
		g.unlock(node)
		for _, n := range neighbors {
			if slices.Contains(early, n) {
				continue // n already links to node
			}
			g.lock(n)
			g.links[n][l] = append(g.links[n][l], node)
			if len(g.links[n][l]) > mmax {
				g.shrink(n, l, mmax, scratch)
			}
			g.unlock(n)
		}
		if len(cands) > 0 {
			ep = cands[0].node
		}
	}
	g.raise(node, level)
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
// accepted beam. Accepted nodes remain score-pruned, so search is approximate.
// Once a filtered search has visited more nodes than an exact scan would
// cost, estimated from the accepted share of the visited nodes, it scores
// every accepted node exactly instead, as Lucene and Weaviate do for
// selective filters and heavily deleted segments; results are then exact.
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
	visited := g.visited.Get().(*visitedSet)
	defer g.visited.Put(visited)
	q := g.newQuery(query, visited.codes)
	visited.codes = q.codes
	ep := g.entry
	for l := g.maxLevel; l > 0; l-- {
		var err error
		ep, err = g.greedy(ctx, &q, ep, l, visited)
		if err != nil {
			return nil, err
		}
	}
	cands, err := g.searchLayer(ctx, &q, ep, ef, 0, accept, g.repeated, visited)
	switch {
	case errors.Is(err, errScan):
		cands, err = g.scan(ctx, &q, k, accept, visited)
	case err == nil:
		// cands holds unique ids; rescore them exactly before taking the top k.
		for i := range cands {
			cands[i].score = g.exactScore(&q, cands[i].node)
		}
		slices.SortFunc(cands, g.compare)
	}
	if err != nil {
		return nil, err
	}
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

// score approximates exactScore, from int8 codes or in float32 with
// independent accumulators; it only steers traversal and neighbor selection.
func (g *Graph) score(q *query, node uint32) float64 {
	if g.quant == QuantizationInt8 {
		return g.scoreInt8(q, node)
	}
	v := g.vector(node)
	switch g.metric {
	case vec.L2:
		return -float64(squaredDistance32(q.values, v))
	case vec.Cosine:
		return clamp(float64(dot32(q.values, v)) / (q.norm * g.norms[node]))
	default:
		return float64(dot32(q.values, v))
	}
}

// exactScore mirrors vec.Score, with the query norm precomputed.
func (g *Graph) exactScore(q *query, node uint32) float64 {
	v := g.vector(node)
	switch g.metric {
	case vec.L2:
		return -squaredDistance(q.values, v)
	case vec.Cosine:
		return clamp(dot(q.values, v) / (q.norm * g.norms[node]))
	default:
		return dot(q.values, v)
	}
}

// clamp gives the same result as math.Max(-1, math.Min(1, x)) for every x, including NaN.
func clamp(x float64) float64 {
	if x > 1 {
		return 1
	}
	if x < -1 {
		return -1
	}
	return x
}

// dot32 and squaredDistance32 keep eight float32 sums so the additions
// pipeline. len(v) must be at least len(q).
func dot32(q, v []float32) float32 {
	v = v[:len(q)]
	var a0, a1, a2, a3, a4, a5, a6, a7 float32
	i := 0
	for ; i+8 <= len(q); i += 8 {
		q8, v8 := q[i:i+8:i+8], v[i:i+8:i+8]
		a0 += q8[0] * v8[0]
		a1 += q8[1] * v8[1]
		a2 += q8[2] * v8[2]
		a3 += q8[3] * v8[3]
		a4 += q8[4] * v8[4]
		a5 += q8[5] * v8[5]
		a6 += q8[6] * v8[6]
		a7 += q8[7] * v8[7]
	}
	for ; i < len(q); i++ {
		a0 += q[i] * v[i]
	}
	return ((a0 + a1) + (a2 + a3)) + ((a4 + a5) + (a6 + a7))
}

func squaredDistance32(q, v []float32) float32 {
	v = v[:len(q)]
	var a0, a1, a2, a3, a4, a5, a6, a7 float32
	i := 0
	for ; i+8 <= len(q); i += 8 {
		q8, v8 := q[i:i+8:i+8], v[i:i+8:i+8]
		d0, d1, d2, d3 := q8[0]-v8[0], q8[1]-v8[1], q8[2]-v8[2], q8[3]-v8[3]
		d4, d5, d6, d7 := q8[4]-v8[4], q8[5]-v8[5], q8[6]-v8[6], q8[7]-v8[7]
		a0 += d0 * d0
		a1 += d1 * d1
		a2 += d2 * d2
		a3 += d3 * d3
		a4 += d4 * d4
		a5 += d5 * d5
		a6 += d6 * d6
		a7 += d7 * d7
	}
	for ; i < len(q); i++ {
		d := q[i] - v[i]
		a0 += d * d
	}
	return ((a0 + a1) + (a2 + a3)) + ((a4 + a5) + (a6 + a7))
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
func (g *Graph) greedy(ctx context.Context, q *query, ep uint32, level int,
	visited *visitedSet) (uint32, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	cur, best := ep, g.score(q, ep)
	for changed := true; changed; {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		changed = false
		for _, n := range g.neighbors(cur, level, visited) {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			if s := g.score(q, n); s > best {
				cur, best, changed = n, s, true
			}
		}
	}
	return cur, ctx.Err()
}

// scan exactly scores every accepted node and returns the best k, one per
// id, best first. The result aliases visited's buffers like searchLayer's.
func (g *Graph) scan(ctx context.Context, q *query, k int, accept func(uint64) bool,
	visited *visitedSet) ([]cand, error) {
	results := &candHeap{g: g, items: visited.results[:0], ids: visited.resultIDs(g.repeated)}
	defer func() { visited.results = results.items }()
	for node := range g.ids {
		if node%scanCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if !accept(g.ids[node]) {
			continue
		}
		n := uint32(node) // #nosec G115 -- Add bounds nodes by MaxUint32.
		results.keep(cand{node: n, score: g.exactScore(q, n)}, k)
	}
	slices.SortFunc(results.items, g.compare)
	return results.items, ctx.Err()
}

// scanCheaper reports whether a filtered search that visited seen of n
// nodes, accepted of them, costs more than an exact scan. The scan calls
// accept on every node, taken as a sixteenth of a visit, and scores the
// accepted share, estimated from the visited nodes: it stops once
// seen > n/16 + n*accepted/seen.
func scanCheaper(seen, accepted, n int) bool {
	s := float64(seen)
	return seen >= scanMinVisits && 16*s*s > float64(n)*(s+16*float64(accepted))
}

// scanBudget counts the visits of a filtered search over n nodes; with n
// zero, for an unfiltered search, it never expires.
type scanBudget struct{ seen, accepted, n int }

func newScanBudget(filtered bool, n int) scanBudget {
	if !filtered {
		return scanBudget{}
	}
	return scanBudget{n: n}
}

func (b *scanBudget) visit(rejected bool) {
	b.seen++
	if !rejected {
		b.accepted++
	}
}

func (b *scanBudget) expired() bool {
	return b.n > 0 && scanCheaper(b.seen, b.accepted, b.n)
}

// searchLayer is a beam search on one layer returning up to ef results, best
// first. Rejected candidates bypass beam pruning; accepted candidates below
// the beam are skipped rather than stopping traversal of pending bridges.
// unique keeps one result per id. With accept set it returns errScan once
// scanCheaper holds. The result aliases visited's buffers and is valid until
// visited is reused.
func (g *Graph) searchLayer(ctx context.Context, q *query, ep uint32, ef, level int,
	accept func(uint64) bool, unique bool, visited *visitedSet) ([]cand, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	visited.reset(len(g.ids))
	visited.visit(ep)
	cands := &candHeap{g: g, items: visited.cands[:0], best: true}
	results := &candHeap{g: g, items: visited.results[:0], ids: visited.resultIDs(unique)}
	defer func() { visited.cands, visited.results = cands.items[:0], results.items }()
	start := cand{node: ep, score: g.score(q, ep)}
	start.rejected = accept != nil && !accept(g.ids[ep])
	cands.push(start)
	budget := newScanBudget(accept != nil, len(g.ids))
	budget.visit(start.rejected)
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
		for _, n := range g.neighbors(c.node, level, visited) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !visited.visit(n) {
				continue
			}
			next := cand{node: n, score: g.score(q, n)}
			next.rejected = accept != nil && !accept(g.ids[n])
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			budget.visit(next.rejected)
			if budget.expired() {
				return nil, errScan
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
		cq := g.nodeQuery(c.node)
		keep := true
		for _, o := range out {
			if g.score(&cq, o) > c.score {
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
func (g *Graph) shrink(node uint32, level, mmax int, scratch *[]cand) {
	nq := g.nodeQuery(node)
	links := g.links[node][level]
	cands := (*scratch)[:0]
	for _, n := range links {
		cands = append(cands, cand{node: n, score: g.score(&nq, n)})
	}
	slices.SortFunc(cands, g.compare)
	*scratch = cands
	// cands holds copies, so the kept nodes can overwrite links in place.
	g.links[node][level] = g.selectNeighbors(links[:0], cands, mmax)
}
