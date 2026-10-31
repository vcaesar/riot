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
	"fmt"

	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/similarity"
)

// MultiTermConstantScoreThreshold is a compile time setting that
// applications can adjust to control when a multi-term searcher (prefix,
// regexp, wildcard, term range, numeric range, ...) stops scoring each
// expanded term and instead returns every matching document with the
// constant score boost, computed as a single bitmap union per segment.
// This is what Lucene (CONSTANT_SCORE_BLENDED_REWRITE, 16 terms) and
// tantivy (always) do; summing BM25 over hundreds of expansions is
// expensive and not a useful relevance signal. Set to 0 to disable.
var MultiTermConstantScoreThreshold = 16

func NewMultiTermSearcher(indexReader search.Reader, terms []string,
	field string, boost float64, scorer search.Scorer, compScorer search.CompositeScorer,
	options search.SearcherOptions, limit bool) (
	search.Searcher, error) {
	if tooManyClauses(len(terms)) {
		if optionsDisjunctionOptimizable(options) {
			return optimizeMultiTermSearcher(indexReader, terms, nil, field, boost, scorer, options)
		}
		if limit {
			return nil, tooManyClausesErr(field, len(terms))
		}
	}

	if score, ok := constantScoreMultiTerm(len(terms), boost, scorer, compScorer, options); ok {
		bterms := make([][]byte, len(terms))
		for i, term := range terms {
			bterms[i] = []byte(term)
		}
		rv, err := newConstantScoreMultiTermSearcher(indexReader, bterms, field, score, options)
		if err != nil || rv != nil {
			return rv, err
		}
	}

	qsearchers, err := makeBatchSearchers(indexReader, terms, nil, field, boost, scorer, options)
	if err != nil {
		return nil, err
	}

	// build disjunction searcher of these ranges
	return newMultiTermSearcherInternal(indexReader, qsearchers, compScorer, options, limit)
}

func NewMultiTermSearcherIndividualBoost(indexReader search.Reader, terms []string, termBoosts []float64,
	field string, boost float64, scorer search.Scorer, compScorer search.CompositeScorer,
	options search.SearcherOptions, limit bool) (search.Searcher, error) {
	if tooManyClauses(len(terms)) {
		if optionsDisjunctionOptimizable(options) {
			return optimizeMultiTermSearcher(indexReader, terms, termBoosts, field, boost, scorer, options)
		}
		if limit {
			return nil, tooManyClausesErr(field, len(terms))
		}
	}

	qsearchers, err := makeBatchSearchers(indexReader, terms, termBoosts, field, boost, scorer, options)
	if err != nil {
		return nil, err
	}

	// build disjunction searcher of these ranges
	return newMultiTermSearcherInternal(indexReader, qsearchers, compScorer, options, limit)
}

func NewMultiTermSearcherBytes(indexReader search.Reader, terms [][]byte,
	field string, boost float64, scorer search.Scorer, compScorer search.CompositeScorer,
	options search.SearcherOptions, limit bool) (search.Searcher, error) {
	if tooManyClauses(len(terms)) {
		if optionsDisjunctionOptimizable(options) {
			return optimizeMultiTermSearcherBytes(indexReader, terms, field, boost, scorer, options)
		}

		if limit {
			return nil, tooManyClausesErr(field, len(terms))
		}
	}

	if score, ok := constantScoreMultiTerm(len(terms), boost, scorer, compScorer, options); ok {
		rv, err := newConstantScoreMultiTermSearcher(indexReader, terms, field, score, options)
		if err != nil || rv != nil {
			return rv, err
		}
	}

	qsearchers, err := makeBatchSearchersBytes(indexReader, terms, field, boost, scorer, options)
	if err != nil {
		return nil, err
	}

	// build disjunction searcher of these ranges
	return newMultiTermSearcherInternal(indexReader, qsearchers, compScorer, options, limit)
}

func newMultiTermSearcherInternal(indexReader search.Reader,
	searchers []search.Searcher, compScorer search.CompositeScorer,
	options search.SearcherOptions, limit bool) (
	search.Searcher, error) {
	// build disjunction searcher of these ranges
	searcher, err := newDisjunctionSearcher(indexReader, searchers, 0, compScorer, options,
		limit)
	if err != nil {
		for _, s := range searchers {
			_ = s.Close()
		}
		return nil, err
	}

	return searcher, nil
}

func optimizeMultiTermSearcher(indexReader search.Reader, terms []string, termBoosts []float64,
	field string, boost float64, scorer search.Scorer, options search.SearcherOptions) (
	search.Searcher, error) {
	var finalSearcher search.Searcher
	for len(terms) > 0 {
		var batchTerms []string
		var batchBoosts []float64
		if len(terms) > DisjunctionMaxClauseCount {
			batchTerms = terms[:DisjunctionMaxClauseCount]
			terms = terms[DisjunctionMaxClauseCount:]
			if termBoosts != nil {
				batchBoosts = termBoosts[:DisjunctionMaxClauseCount]
				termBoosts = termBoosts[DisjunctionMaxClauseCount:]
			}
		} else {
			batchTerms = terms
			terms = nil
			batchBoosts = termBoosts
			termBoosts = nil
		}
		batch, err := makeBatchSearchers(indexReader, batchTerms, batchBoosts, field, boost, scorer, options)
		if err != nil {
			if finalSearcher != nil {
				_ = finalSearcher.Close()
			}
			return nil, err
		}
		if finalSearcher != nil {
			batch = append(batch, finalSearcher)
		}
		cleanup := func() {
			for _, searcher := range batch {
				if searcher != nil {
					_ = searcher.Close()
				}
			}
		}
		finalSearcher, err = optimizeCompositeSearcher("disjunction:unadorned",
			indexReader, batch, options)
		// all searchers in batch should be closed, regardless of error or optimization failure
		// either we're returning, or continuing and only finalSearcher is needed for next loop
		cleanup()
		if err != nil {
			return nil, err
		}
		if finalSearcher == nil {
			return nil, fmt.Errorf("unable to optimize")
		}
	}
	return finalSearcher, nil
}

func makeBatchSearchers(indexReader search.Reader, terms []string, termBoosts []float64, field string,
	boost float64, scorer search.Scorer, options search.SearcherOptions) ([]search.Searcher, error) {
	qsearchers := make([]search.Searcher, len(terms))
	qsearchersClose := func() {
		for _, searcher := range qsearchers {
			if searcher != nil {
				_ = searcher.Close()
			}
		}
	}
	for i, term := range terms {
		termBoost := boost
		if termBoosts != nil {
			termBoost *= termBoosts[i]
		}
		// assign only on success: a nil *TermSearcher in the interface
		// would pass the nil check in qsearchersClose and panic in Close
		ts, err := NewTermSearcher(indexReader, term, field, termBoost, scorer, options)
		if err != nil {
			qsearchersClose()
			return nil, err
		}
		qsearchers[i] = ts
	}
	return qsearchers, nil
}

func optimizeMultiTermSearcherBytes(indexReader search.Reader, terms [][]byte,
	field string, boost float64, scorer search.Scorer, options search.SearcherOptions) (
	search.Searcher, error) {
	var finalSearcher search.Searcher
	for len(terms) > 0 {
		var batchTerms [][]byte
		if len(terms) > DisjunctionMaxClauseCount {
			batchTerms = terms[:DisjunctionMaxClauseCount]
			terms = terms[DisjunctionMaxClauseCount:]
		} else {
			batchTerms = terms
			terms = nil
		}
		batch, err := makeBatchSearchersBytes(indexReader, batchTerms, field, boost, scorer, options)
		if err != nil {
			if finalSearcher != nil {
				_ = finalSearcher.Close()
			}
			return nil, err
		}
		if finalSearcher != nil {
			batch = append(batch, finalSearcher)
		}
		cleanup := func() {
			for _, searcher := range batch {
				if searcher != nil {
					_ = searcher.Close()
				}
			}
		}
		finalSearcher, err = optimizeCompositeSearcher("disjunction:unadorned",
			indexReader, batch, options)
		// all searchers in batch should be closed, regardless of error or optimization failure
		// either we're returning, or continuing and only finalSearcher is needed for next loop
		cleanup()
		if err != nil {
			return nil, err
		}
		if finalSearcher == nil {
			return nil, fmt.Errorf("unable to optimize")
		}
	}
	return finalSearcher, nil
}

// constantScoreMultiTerm reports whether a multi-term searcher over
// numTerms expansions should be built as a constant-score bitmap union,
// and the score to use. Term vectors, custom (non-constant) scorers and
// custom composite scorers need the per-term searchers.
func constantScoreMultiTerm(numTerms int, boost float64, scorer search.Scorer,
	compScorer search.CompositeScorer, options search.SearcherOptions) (float64, bool) {
	if numTerms <= MultiTermConstantScoreThreshold {
		return 0, false
	}
	return constantScoreEligible(boost, scorer, compScorer, options)
}

// constantScoreEligible reports whether a multi-term query may be served
// as a constant-score bitmap union, regardless of its term count, and the
// score to use.
func constantScoreEligible(boost float64, scorer search.Scorer,
	compScorer search.CompositeScorer, options search.SearcherOptions) (float64, bool) {
	if MultiTermConstantScoreThreshold <= 0 || options.IncludeTermVectors {
		return 0, false
	}
	if _, ok := compScorer.(*similarity.CompositeSumScorer); !ok && compScorer != nil {
		return 0, false
	}
	switch s := scorer.(type) {
	case nil:
		return boost, true
	case similarity.ConstantScorer:
		return float64(s), true
	}
	return 0, false
}

var multiTermConstantScoreTerm = []byte("<multi-term:constant-score>")

// termsBitmapReader is implemented by readers that can OR the postings of
// many terms straight from their dictionaries (index.Snapshot). A nil
// iterator means the reader cannot, and the caller ORs term searchers.
type termsBitmapReader interface {
	DocsMatchingTerms(field string, terms [][]byte) (segment.PostingsIterator, error)
}

// newConstantScoreMultiTermSearcher ORs the postings of every term into
// one bitmap per segment and returns a searcher scoring each document
// with score. It returns nil when the index cannot take part in the
// optimization, in which case the caller falls back to a scored
// disjunction.
func newConstantScoreMultiTermSearcher(indexReader search.Reader, terms [][]byte, field string,
	score float64, options search.SearcherOptions) (search.Searcher, error) {
	if r, ok := indexReader.(termsBitmapReader); ok {
		it, err := r.DocsMatchingTerms(field, terms)
		if err != nil {
			return nil, err
		}
		if it != nil {
			return newTermSearcherFromReader(indexReader, it,
				multiTermConstantScoreTerm, field, score, similarity.ConstantScorer(score), options)
		}
	}
	// only the document numbers are needed from the constituent terms
	termOptions := options
	termOptions.Score = optionScoringNone
	termOptions.IncludeTermVectors = false

	var finalSearcher search.Searcher
	for len(terms) > 0 {
		batchTerms := terms
		if DisjunctionMaxClauseCount > 0 && len(terms) > DisjunctionMaxClauseCount {
			batchTerms = terms[:DisjunctionMaxClauseCount]
		}
		terms = terms[len(batchTerms):]
		batch, err := makeBatchSearchersBytes(indexReader, batchTerms, field, 1.0,
			similarity.ConstantScorer(1), termOptions)
		if err != nil {
			if finalSearcher != nil {
				_ = finalSearcher.Close()
			}
			return nil, err
		}
		if finalSearcher != nil {
			batch = append(batch, finalSearcher)
		}
		optimized, err := optimizeComposite("disjunction:unadorned", batch)
		// the batch is folded into optimized (or unusable), close it either way
		for _, searcher := range batch {
			_ = searcher.Close()
		}
		if err != nil || optimized == nil {
			return nil, err
		}
		finalSearcher, err = newTermSearcherFromReader(indexReader, optimized,
			multiTermConstantScoreTerm, field, score, similarity.ConstantScorer(score), options)
		if err != nil {
			return nil, err
		}
	}
	return finalSearcher, nil
}

func makeBatchSearchersBytes(indexReader search.Reader, terms [][]byte, field string,
	boost float64, scorer search.Scorer, options search.SearcherOptions) ([]search.Searcher, error) {
	qsearchers := make([]search.Searcher, len(terms))
	qsearchersClose := func() {
		for _, searcher := range qsearchers {
			if searcher != nil {
				_ = searcher.Close()
			}
		}
	}
	for i, term := range terms {
		ts, err := NewTermSearcherBytes(indexReader, term, field, boost, scorer, options)
		if err != nil {
			qsearchersClose()
			return nil, err
		}
		qsearchers[i] = ts
	}
	return qsearchers, nil
}
