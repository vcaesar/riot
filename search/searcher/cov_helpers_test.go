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

package searcher

import (
	"errors"
	"sort"

	"github.com/RoaringBitmap/roaring/v2"
	segment "github.com/vcaesar/bluge_segment_api"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/similarity"
)

var errCov = errors.New("injected")

// covDoc is one posting of a covPostings list.
type covDoc struct {
	num  uint64
	freq int
	norm float64
	locs []segment.Location
}

// covDocs returns postings with freq 1 and norm 1 for nums.
func covDocs(nums ...uint64) []covDoc {
	rv := make([]covDoc, len(nums))
	for i, n := range nums {
		rv[i] = covDoc{num: n, freq: 1, norm: 1}
	}
	return rv
}

// covPostings is a postings iterator over docs that fails on demand:
// Next returns nextErr once errAt postings were read, Advance advErr.
type covPostings struct {
	docs     []covDoc
	i        int
	nextErr  error
	errAt    int
	advErr   error
	closeErr error
	count    func() uint64
}

func (p *covPostings) Next() (segment.Posting, error) {
	if p.nextErr != nil && p.i >= p.errAt {
		return nil, p.nextErr
	}
	if p.i >= len(p.docs) {
		return nil, nil
	}
	d := p.docs[p.i]
	p.i++
	return &stubTermFieldDoc{number: d.num, freq: d.freq, norm: d.norm, vectors: d.locs}, nil
}

func (p *covPostings) Advance(number uint64) (segment.Posting, error) {
	if p.advErr != nil {
		return nil, p.advErr
	}
	for p.i < len(p.docs) && p.docs[p.i].num < number {
		p.i++
	}
	return p.Next()
}

func (p *covPostings) Size() int    { return 1 }
func (p *covPostings) Empty() bool  { return p.Count() == 0 }
func (p *covPostings) Close() error { return p.closeErr }

func (p *covPostings) Count() uint64 {
	if p.count != nil {
		return p.count()
	}
	return uint64(len(p.docs))
}

// covBatch adds batched reads, block skipping and bounds, segment bitmaps
// and optimization to covPostings. Blocks hold block postings; 0 disables
// bounds and skipping.
type covBatch struct {
	*covPostings
	batchErr  error
	skipErr   error
	block     int
	noBitmap  bool
	bm        *roaring.Bitmap
	optErr    error
	noOpt     bool
	finishErr error
}

func newCovBatch(docs []covDoc, block int) *covBatch {
	return &covBatch{covPostings: &covPostings{docs: docs}, block: block}
}

func (p *covBatch) NextBatch(docs []uint64, freqs []int, norms []float64) (int, error) {
	if p.batchErr != nil {
		return 0, p.batchErr
	}
	n := 0
	for n < len(docs) && p.i < len(p.docs) {
		d := p.docs[p.i]
		docs[n], freqs[n], norms[n] = d.num, d.freq, d.norm
		n++
		p.i++
	}
	return n, nil
}

func (p *covBatch) blockBounds(lo, hi int) (maxFreq int, minNorm float64) {
	minNorm = p.docs[lo].norm
	for _, d := range p.docs[lo:hi] {
		maxFreq, minNorm = max(maxFreq, d.freq), min(minNorm, d.norm)
	}
	return maxFreq, minNorm
}

func (p *covBatch) SkipBlocks(competitive func(maxFreq int, minNorm float64) bool) (uint64, error) {
	if p.skipErr != nil {
		return 0, p.skipErr
	}
	var skipped uint64
	for p.block > 0 && p.i < len(p.docs) {
		end := min((p.i/p.block+1)*p.block, len(p.docs))
		if competitive(p.blockBounds(p.i, end)) {
			break
		}
		skipped += uint64(end - p.i) //nolint:gosec // end > p.i
		p.i = end
	}
	return skipped, nil
}

func (p *covBatch) BlockBound(number uint64) (maxFreq int, minNorm float64, end uint64, ok bool) {
	k := sort.Search(len(p.docs), func(i int) bool { return p.docs[i].num >= number })
	if p.block == 0 || k == len(p.docs) {
		return 0, 0, 0, false
	}
	lo := k / p.block * p.block
	hi := min(lo+p.block, len(p.docs))
	maxFreq, minNorm = p.blockBounds(lo, hi)
	return maxFreq, minNorm, p.docs[hi-1].num + 1, true
}

func (p *covBatch) SegmentBitmap(_ uint64) (bm *roaring.Bitmap, base uint64) {
	if p.noBitmap {
		return nil, 0
	}
	if p.bm == nil {
		p.bm = roaring.New()
		for _, d := range p.docs {
			p.bm.Add(uint32(d.num)) //nolint:gosec // test doc numbers are small
		}
	}
	return p.bm, 0
}

func (p *covBatch) Optimize(kind string, octx segment.OptimizableContext) (segment.OptimizableContext, error) {
	if p.optErr != nil {
		return nil, p.optErr
	}
	if p.noOpt {
		return nil, nil
	}
	c, _ := octx.(*covOctx)
	if c == nil {
		c = &covOctx{kind: kind}
	}
	if kind == "exclusion:exclude" {
		c.exclude = append(c.exclude, p)
	} else {
		c.include = append(c.include, p)
	}
	if p.finishErr != nil {
		c.err = p.finishErr
	}
	return c, nil
}

// covOctx folds covBatch lists: a union for disjunctions, an intersection
// otherwise; exclusions only record that they ran.
type covOctx struct {
	kind     string
	include  []*covBatch
	exclude  []*covBatch
	err      error
	excluded bool
}

func (c *covOctx) Excluded() bool { return c.excluded }

func (c *covOctx) Finish() (segment.PostingsIterator, error) {
	if c.err != nil {
		return nil, c.err
	}
	if len(c.exclude) > 0 {
		c.excluded = true
		return nil, nil
	}
	seen := map[uint64]int{}
	for _, p := range c.include {
		for _, d := range p.docs {
			seen[d.num]++
		}
	}
	var nums []uint64
	for n, k := range seen {
		if c.kind == "disjunction:unadorned" || k == len(c.include) {
			nums = append(nums, n)
		}
	}
	sort.Slice(nums, func(i, j int) bool { return nums[i] < nums[j] })
	return newCovBatch(covDocs(nums...), 0), nil
}

// covReader is the stub index reader with injectable errors and postings
// iterators served by term.
type covReader struct {
	*stubIndexReader
	its            map[string]segment.PostingsIterator
	postErr        map[string]error
	posts          int // PostingsIterator calls
	postFailAt     int // the call, 1-based, from which PostingsIterator fails
	statsErr       error
	dvs, dvFailAt  int // DocumentValueReader calls, and the one from which it fails
	dictErr        error
	dictNextErr    error
	dictNextAfter  int // entries read before dictNextErr
	dictCloseErr   error
	lookups        int // DictionaryLookup calls
	lookupFailAt   int // the call from which DictionaryLookup fails
	lookupCloseErr error
}

func newCovReader() *covReader {
	sir := newStubIndexReader()
	for _, doc := range baseTestIndexDocs {
		sir.add(doc)
	}
	return covReaderOf(sir)
}

func covReaderOf(sir *stubIndexReader) *covReader {
	return &covReader{stubIndexReader: sir,
		its: map[string]segment.PostingsIterator{}, postErr: map[string]error{}}
}

func (r *covReader) PostingsIterator(term []byte, field string, includeFreq, includeNorm,
	includeTermVectors bool) (segment.PostingsIterator, error) {
	r.posts++
	if r.postFailAt > 0 && r.posts >= r.postFailAt {
		return nil, errCov
	}
	if err := r.postErr[string(term)]; err != nil {
		return nil, err
	}
	if it, ok := r.its[string(term)]; ok {
		return it, nil
	}
	return r.stubIndexReader.PostingsIterator(term, field, includeFreq, includeNorm, includeTermVectors)
}

func (r *covReader) CollectionStats(field string) (segment.CollectionStats, error) {
	if r.statsErr != nil {
		return nil, r.statsErr
	}
	return r.stubIndexReader.CollectionStats(field)
}

func (r *covReader) DocumentValueReader(fields []string) (segment.DocumentValueReader, error) {
	r.dvs++
	if r.dvFailAt > 0 && r.dvs >= r.dvFailAt {
		return nil, errCov
	}
	return r.stubIndexReader.DocumentValueReader(fields)
}

func (r *covReader) DictionaryIterator(field string, a segment.Automaton, start, end []byte) (
	segment.DictionaryIterator, error) {
	if r.dictErr != nil {
		return nil, r.dictErr
	}
	it, err := r.stubIndexReader.DictionaryIterator(field, a, start, end)
	if err != nil {
		return nil, err
	}
	return &covDictItr{DictionaryIterator: it, nextErr: r.dictNextErr, nextAfter: r.dictNextAfter,
		closeErr: r.dictCloseErr}, nil
}

func (r *covReader) DictionaryLookup(field string) (segment.DictionaryLookup, error) {
	r.lookups++
	if r.lookupFailAt > 0 && r.lookups >= r.lookupFailAt {
		return nil, errCov
	}
	l, err := r.stubIndexReader.DictionaryLookup(field)
	if err != nil {
		return nil, err
	}
	return &covLookup{DictionaryLookup: l, closeErr: r.lookupCloseErr}, nil
}

type covDictItr struct {
	segment.DictionaryIterator
	nextErr   error
	nextAfter int
	nexts     int
	closeErr  error
}

func (d *covDictItr) Next() (segment.DictionaryEntry, error) {
	d.nexts++
	if d.nextErr != nil && d.nexts > d.nextAfter {
		return nil, d.nextErr
	}
	return d.DictionaryIterator.Next()
}

func (d *covDictItr) Close() error { return d.closeErr }

type covLookup struct {
	segment.DictionaryLookup
	closeErr error
}

func (l *covLookup) Close() error { return l.closeErr }

// covFastReader adds the index snapshot fast paths to covReader; a nil
// iterator without an error means the path is unavailable.
type covFastReader struct {
	*covReader
	it  segment.PostingsIterator
	err error
}

func (r *covFastReader) DocsMatchingTerms(_ string, _ [][]byte) (segment.PostingsIterator, error) {
	return r.it, r.err
}

func (r *covFastReader) DocsMatchingRange(_ string, _ segment.Automaton, _, _ []byte) (segment.PostingsIterator, error) {
	return r.it, r.err
}

func (r *covFastReader) NumericRange(_ string, _, _ int64, _, _ []byte) (segment.PostingsIterator, error) {
	return r.it, r.err
}

func (r *covFastReader) TermsIterator(field string, a segment.Automaton, start, end []byte) (
	segment.DictionaryIterator, error) {
	return r.DictionaryIterator(field, a, start, end)
}

// covSearcher is a searcher over docs that fails on demand: the nextErrAt-th
// Next call (1-based) and later ones return nextErr, together with a match
// when withMatch is set.
type covSearcher struct {
	docs      []uint64
	pos       int
	min       int
	nexts     int
	nextErrAt int
	nextErr   error
	withMatch bool
	advErr    error
	closeErr  error
}

func (s *covSearcher) emit(ctx *search.Context) *search.DocumentMatch {
	if s.pos >= len(s.docs) {
		return nil
	}
	rv := ctx.DocumentMatchPool.Get()
	rv.Number, rv.Score = s.docs[s.pos], 1
	s.pos++
	return rv
}

func (s *covSearcher) Next(ctx *search.Context) (*search.DocumentMatch, error) {
	s.nexts++
	if s.nextErr != nil && s.nexts >= s.nextErrAt {
		if s.withMatch {
			return ctx.DocumentMatchPool.Get(), s.nextErr
		}
		return nil, s.nextErr
	}
	return s.emit(ctx), nil
}

func (s *covSearcher) Advance(ctx *search.Context, number uint64) (*search.DocumentMatch, error) {
	if s.advErr != nil {
		return nil, s.advErr
	}
	for s.pos < len(s.docs) && s.docs[s.pos] < number {
		s.pos++
	}
	return s.emit(ctx), nil
}

func (s *covSearcher) Close() error               { return s.closeErr }
func (s *covSearcher) Count() uint64              { return uint64(len(s.docs)) }
func (s *covSearcher) Min() int                   { return s.min }
func (s *covSearcher) Size() int                  { return 1 }
func (s *covSearcher) DocumentMatchPoolSize() int { return 1 }

func covCtx() *search.Context {
	return search.NewSearchContext(64, 0)
}

// covDrain reads s with Next until it ends, returning the numbers read.
func covDrain(ctx *search.Context, s search.Searcher) ([]uint64, error) {
	var rv []uint64
	for {
		dm, err := s.Next(ctx)
		if err != nil || dm == nil {
			return rv, err
		}
		rv = append(rv, dm.Number)
		ctx.DocumentMatchPool.Put(dm)
	}
}

// covTerm returns a term searcher over it scoring every hit 1.
func covTerm(it segment.PostingsIterator, options search.SearcherOptions) *TermSearcher {
	ts, err := newTermSearcherFromReader(baseTestIndexReader, it, "t", "desc", 1, similarity.ConstantScorer(1), options)
	if err != nil {
		panic(err)
	}
	return ts
}
