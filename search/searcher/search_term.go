//  Copyright (c) 2020 Couchbase, Inc.
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
	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/riot/numeric"
	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/similarity"
)

type TermSearcher struct {
	indexReader search.Reader
	reader      segment.PostingsIterator
	options     search.SearcherOptions
	scorer      search.Scorer
	queryTerm   string
	cursor      postingsCursor
}

func NewTermSearcher(indexReader search.Reader, term, field string, boost float64, scorer search.Scorer,
	options search.SearcherOptions) (*TermSearcher, error) {
	return NewTermSearcherBytes(indexReader, []byte(term), field, boost, scorer, options)
}

func NewTermSearcherBytes(indexReader search.Reader, term []byte, field string, boost float64, scorer search.Scorer,
	options search.SearcherOptions) (*TermSearcher, error) {
	needFreqNorm := options.Score != "none"
	reader, err := indexReader.PostingsIterator(term, field, needFreqNorm, needFreqNorm, options.IncludeTermVectors)
	if err != nil {
		return nil, err
	}
	return newTermSearcherFromReader(indexReader, reader, term, field, boost, scorer, options)
}

type termStatsWrapper struct {
	docFreq uint64
}

func (t *termStatsWrapper) DocumentFrequency() uint64 {
	return t.docFreq
}

func newTermSearcherFromReader(indexReader search.Reader, reader segment.PostingsIterator,
	term []byte, field string, boost float64, scorer search.Scorer, options search.SearcherOptions) (*TermSearcher, error) {
	if scorer == nil {
		collStats, err := indexReader.CollectionStats(field)
		if err != nil {
			return nil, err
		}
		scorer = options.SimilarityForField(field).Scorer(boost, collStats, &termStatsWrapper{docFreq: reader.Count()})
	}
	return &TermSearcher{
		indexReader: indexReader,
		reader:      reader,
		scorer:      scorer,
		options:     options,
		queryTerm:   string(term),
	}, nil
}

func (s *TermSearcher) Size() int {
	return reflectStaticSizeTermSearcher + sizeOfPtr + s.reader.Size()
}

func (s *TermSearcher) Count() uint64 {
	return s.reader.Count()
}

func (s *TermSearcher) Next(ctx *search.Context) (*search.DocumentMatch, error) {
	termMatch, err := s.cursor.next(s.reader)
	if err != nil {
		return nil, err
	}

	if termMatch == nil {
		return nil, nil
	}

	// score match
	docMatch := s.buildDocumentMatch(ctx, termMatch)

	// return doc match
	return docMatch, nil
}

func (s *TermSearcher) Advance(ctx *search.Context, number uint64) (*search.DocumentMatch, error) {
	termMatch, err := s.cursor.advance(s.reader, number)
	if err != nil {
		return nil, err
	}

	if termMatch == nil {
		return nil, nil
	}

	// score match
	docMatch := s.buildDocumentMatch(ctx, termMatch)

	// return doc match
	return docMatch, nil
}

func (s *TermSearcher) Close() error {
	return s.reader.Close()
}

func (s *TermSearcher) Min() int {
	return 0
}

func (s *TermSearcher) DocumentMatchPoolSize() int {
	return 1
}

func (s *TermSearcher) Optimize(kind string, octx segment.OptimizableContext) (
	segment.OptimizableContext, error) {
	o, ok := s.reader.(segment.Optimizable)
	if ok {
		return o.Optimize(kind, octx)
	}

	return nil, nil
}

// NextAbove implements search.ScoreSkipper.
func (s *TermSearcher) NextAbove(ctx *search.Context, floor float64,
	skipped func(score float64, n int) bool) (*search.DocumentMatch, error) {
	if s.options.Explain {
		return s.Next(ctx)
	}
	s.cursor.startBatch(s.reader, s.options.IncludeTermVectors)
	posting, score, err := s.cursor.nextAbove(s.reader, s.scorer, floor, ctx.SkipBlocks, skipped)
	if err != nil || posting == nil {
		return nil, err
	}
	return s.buildScoredMatch(ctx, posting, score), nil
}

func (s *TermSearcher) buildDocumentMatch(ctx *search.Context, termMatch segment.Posting) *search.DocumentMatch {
	if s.options.Explain {
		explanation := s.scorer.Explain(termMatch.Frequency(), termMatch.Norm())
		rv := s.buildScoredMatch(ctx, termMatch, explanation.Value)
		rv.Explanation = explanation
		return rv
	}
	return s.buildScoredMatch(ctx, termMatch, s.scorer.Score(termMatch.Frequency(), termMatch.Norm()))
}

func (s *TermSearcher) buildScoredMatch(ctx *search.Context, termMatch segment.Posting, score float64) *search.DocumentMatch {
	rv := ctx.DocumentMatchPool.Get()
	rv.SetReader(s.indexReader)
	rv.Number = termMatch.Number()
	rv.Score = score

	if len(termMatch.Locations()) > 0 {
		if cap(rv.FieldTermLocations) < len(termMatch.Locations()) {
			rv.FieldTermLocations = make([]search.FieldTermLocation, 0, len(termMatch.Locations()))
		}

		for _, v := range termMatch.Locations() {
			rv.FieldTermLocations =
				append(rv.FieldTermLocations, search.FieldTermLocation{
					Field: v.Field(),
					Term:  s.queryTerm,
					Location: search.Location{
						Pos:   v.Pos(),
						Start: v.Start(),
						End:   v.End(),
					},
				})
		}
	}

	return rv
}

// postingsBatcher is implemented by postings iterators that read many
// postings per call (index snapshot iterators).
type postingsBatcher interface {
	NextBatch(docs []uint64, freqs []int, norms []float64) (int, error)
}

// blockSkipper is implemented by postings iterators that can skip blocks
// of postings by their freq/norm bounds (index snapshot iterators).
type blockSkipper interface {
	SkipBlocks(competitive func(maxFreq int, minNorm float64) bool) (uint64, error)
}

// monotoneScorer reports whether scorer never scores a higher freq or a
// smaller norm lower, so a block's bound is its (max freq, min norm) score.
func monotoneScorer(scorer search.Scorer) bool {
	switch s := scorer.(type) {
	case *similarity.BM25Scorer:
		return s.Monotone()
	case similarity.ConstantScorer:
		return true
	}
	return false
}

const postingsBatchSize = 128

// postingsCursor counts the postings read through it so that, under a
// constant scorer, the hits left after the top-N fills can be counted from
// the reader's total instead of iterated. Once nextAbove runs on a reader
// without locations it reads postings in batches, which next and advance
// then drain first.
type postingsCursor struct {
	seen     uint64
	advanced bool // an Advance skipped postings, seen no longer counts them all
	done     bool

	batch       *postingsBatch
	noBatch     bool // locations are needed, or reader cannot batch
	batchPost   batchPosting
	batchReader postingsBatcher
}

type postingsBatch struct {
	docs   []uint64
	freqs  []int
	norms  []float64
	n, pos int
}

// batchPosting is a segment.Posting over the current batch entry.
type batchPosting struct {
	number uint64
	freq   int
	norm   float64
}

func (p *batchPosting) Number() uint64                { return p.number }
func (p *batchPosting) SetNumber(n uint64)            { p.number = n }
func (p *batchPosting) Frequency() int                { return p.freq }
func (p *batchPosting) Norm() float64                 { return p.norm }
func (p *batchPosting) Locations() []segment.Location { return nil }
func (p *batchPosting) Size() int                     { return 0 }

// fill refills an empty batch; false means the reader is exhausted.
func (c *postingsCursor) fill() (bool, error) {
	b := c.batch
	if b.pos < b.n {
		return true, nil
	}
	n, err := c.batchReader.NextBatch(b.docs, b.freqs, b.norms)
	if err != nil {
		return false, err
	}
	b.n, b.pos = n, 0
	return n > 0, nil
}

func (c *postingsCursor) pop() segment.Posting {
	b := c.batch
	c.batchPost = batchPosting{b.docs[b.pos], b.freqs[b.pos], b.norms[b.pos]}
	b.pos++
	c.seen++
	return &c.batchPost
}

func (c *postingsCursor) next(reader segment.PostingsIterator) (segment.Posting, error) {
	if c.done {
		return nil, nil
	}
	if c.batch != nil {
		ok, err := c.fill()
		if err != nil || !ok {
			return nil, err
		}
		return c.pop(), nil
	}
	p, err := reader.Next()
	if p != nil {
		c.seen++
	}
	return p, err
}

func (c *postingsCursor) advance(reader segment.PostingsIterator, number uint64) (segment.Posting, error) {
	c.advanced = true
	if c.batch != nil {
		b := c.batch
		for b.pos < b.n && b.docs[b.pos] < number {
			b.pos++
		}
		if b.pos < b.n {
			return c.pop(), nil
		}
	}
	return reader.Advance(number)
}

// startBatch switches to batched reads when the reader supports them and
// no locations are needed.
func (c *postingsCursor) startBatch(reader segment.PostingsIterator, needLocations bool) {
	if c.batch != nil || c.noBatch {
		return
	}
	br, ok := reader.(postingsBatcher)
	if !ok || needLocations {
		c.noBatch = true
		return
	}
	c.batchReader = br
	// rare terms need no more room than their postings
	size := int(min(reader.Count(), postingsBatchSize))
	if size == 0 {
		c.noBatch = true
		return
	}
	c.batch = &postingsBatch{
		docs:  make([]uint64, size),
		freqs: make([]int, size),
		norms: make([]float64, size),
	}
}

// nextAbove advances reader to the next posting scoring above floor, passing
// the scores of the postings it passes over to skipped.
func (c *postingsCursor) nextAbove(reader segment.PostingsIterator, scorer search.Scorer, floor float64,
	skipBlocks bool, skipped func(float64, int) bool) (segment.Posting, float64, error) {
	floorBits := numeric.Float64ToInt64(floor)
	if cs, ok := scorer.(similarity.ConstantScorer); ok && !c.advanced &&
		numeric.Float64ToInt64(float64(cs)) <= floorBits {
		// every remaining hit ties at or below floor and loses
		if total := reader.Count(); total >= c.seen {
			if n := total - c.seen; n > 0 {
				c.seen = total
				skipped(float64(cs), int(n)) //nolint:gosec // bounded by the snapshot's doc count
			}
			c.done = true
			return nil, 0, nil
		}
	}
	if c.batch != nil && !c.done {
		return c.nextAboveBatch(reader, scorer, floor, floorBits, skipBlocks, skipped)
	}
	for {
		p, err := c.next(reader)
		if err != nil || p == nil {
			return nil, 0, err
		}
		score := scorer.Score(p.Frequency(), p.Norm())
		if numeric.Float64ToInt64(score) > floorBits {
			return p, score, nil
		}
		if !skipped(score, 1) {
			return nil, 0, nil
		}
	}
}

// nextAboveBatch is nextAbove's loop over batched postings.
// With skipBlocks, whole postings blocks whose bound is at or below floor
// are reported to skipped as floor without being decoded.
func (c *postingsCursor) nextAboveBatch(reader segment.PostingsIterator, scorer search.Scorer, floor float64,
	floorBits int64, skipBlocks bool, skipped func(float64, int) bool) (segment.Posting, float64, error) {
	b := c.batch
	bs, canSkip := reader.(blockSkipper)
	canSkip = canSkip && skipBlocks && !c.advanced && monotoneScorer(scorer)
	competitive := func(maxFreq int, minNorm float64) bool {
		return numeric.Float64ToInt64(scorer.Score(maxFreq, minNorm)) > floorBits
	}
	for {
		if canSkip && b.pos >= b.n {
			n, err := bs.SkipBlocks(competitive)
			if err != nil {
				return nil, 0, err
			}
			if n > 0 {
				c.seen += n
				if !skipped(floor, int(n)) { //nolint:gosec // bounded by the segment's doc count
					return nil, 0, nil
				}
			}
		}
		ok, err := c.fill()
		if err != nil || !ok {
			return nil, 0, err
		}
		for b.pos < b.n {
			k := b.pos
			score := scorer.Score(b.freqs[k], b.norms[k])
			if numeric.Float64ToInt64(score) > floorBits {
				return c.pop(), score, nil
			}
			b.pos++
			c.seen++
			if !skipped(score, 1) {
				return nil, 0, nil
			}
		}
	}
}
