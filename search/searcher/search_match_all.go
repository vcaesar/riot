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
	"github.com/vcaesar/riot/search"
)

type MatchAllSearcher struct {
	reader      segment.PostingsIterator
	scorer      search.Scorer
	indexReader search.Reader
	options     search.SearcherOptions
	cursor      postingsCursor
}

func NewMatchAllSearcher(indexReader search.Reader, _ float64, scorer search.Scorer,
	options search.SearcherOptions) (*MatchAllSearcher, error) {
	reader, err := indexReader.PostingsIterator(nil, "",
		false, false, false)
	if err != nil {
		return nil, err
	}
	return &MatchAllSearcher{
		indexReader: indexReader,
		reader:      reader,
		scorer:      scorer,
		options:     options,
	}, nil
}

func (s *MatchAllSearcher) Size() int {
	return reflectStaticSizeMatchAllSearcher + sizeOfPtr +
		s.reader.Size()
}

func (s *MatchAllSearcher) Count() uint64 {
	return s.reader.Count()
}

func (s *MatchAllSearcher) Next(ctx *search.Context) (*search.DocumentMatch, error) {
	tfd, err := s.cursor.next(s.reader)
	if err != nil {
		return nil, err
	}

	if tfd == nil {
		return nil, nil
	}

	// score match
	docMatch := s.buildDocumentMatch(ctx, tfd)

	// return doc match
	return docMatch, nil
}

func (s *MatchAllSearcher) Advance(ctx *search.Context, number uint64) (*search.DocumentMatch, error) {
	tfd, err := s.cursor.advance(s.reader, number)
	if err != nil {
		return nil, err
	}

	if tfd == nil {
		return nil, nil
	}

	// score match
	docMatch := s.buildDocumentMatch(ctx, tfd)

	// return doc match
	return docMatch, nil
}

func (s *MatchAllSearcher) Close() error {
	return s.reader.Close()
}

func (s *MatchAllSearcher) Min() int {
	return 0
}

func (s *MatchAllSearcher) DocumentMatchPoolSize() int {
	return 1
}

// NextAbove implements search.ScoreSkipper.
func (s *MatchAllSearcher) NextAbove(ctx *search.Context, floor float64,
	skipped func(score float64, n int) bool) (*search.DocumentMatch, error) {
	if s.options.Explain {
		return s.Next(ctx)
	}
	posting, score, err := s.cursor.nextAbove(s.reader, s.scorer, floor, false, skipped)
	if err != nil || posting == nil {
		return nil, err
	}
	return s.buildScoredMatch(ctx, posting, score), nil
}

func (s *MatchAllSearcher) buildDocumentMatch(ctx *search.Context, termMatch segment.Posting) *search.DocumentMatch {
	if s.options.Explain {
		explanation := s.scorer.Explain(termMatch.Frequency(), termMatch.Norm())
		rv := s.buildScoredMatch(ctx, termMatch, explanation.Value)
		rv.Explanation = explanation
		return rv
	}
	return s.buildScoredMatch(ctx, termMatch, s.scorer.Score(termMatch.Frequency(), termMatch.Norm()))
}

func (s *MatchAllSearcher) buildScoredMatch(ctx *search.Context, termMatch segment.Posting, score float64) *search.DocumentMatch {
	rv := ctx.DocumentMatchPool.Get()
	rv.SetReader(s.indexReader)
	rv.Number = termMatch.Number()
	rv.Score = score
	return rv
}
