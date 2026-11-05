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
	"github.com/vcaesar/riot/search/similarity"
)

// rangeBitmapReader is implemented by readers that can OR the postings of
// a term range in one dictionary pass (index.Snapshot); nil means cannot.
type rangeBitmapReader interface {
	DocsMatchingRange(field string, automaton segment.Automaton, start, end []byte) (segment.PostingsIterator, error)
}

// minRangeTerms is the fewest terms worth a range pass: it allocates a
// segment-sized bitset per segment, while DocsMatchingTerms clones or ORs
// one or two postings bitmaps directly.
const minRangeTerms = 3

// termsIteratorReader is implemented by readers that can list terms
// without reading their postings (index.Snapshot).
type termsIteratorReader interface {
	TermsIterator(field string, automaton segment.Automaton, start, end []byte) (segment.DictionaryIterator, error)
}

func NewTermPrefixSearcher(indexReader search.Reader, prefix, field string,
	boost float64, scorer search.Scorer, compScorer search.CompositeScorer,
	options search.SearcherOptions) (rv search.Searcher, err error) {
	// find the terms with this prefix
	kBeg := []byte(prefix)
	kEnd := incrementBytes(kBeg)
	var fieldDict segment.DictionaryIterator
	if tr, ok := indexReader.(termsIteratorReader); ok {
		// only the terms are needed, not their counts
		fieldDict, err = tr.TermsIterator(field, nil, kBeg, kEnd)
	} else {
		fieldDict, err = indexReader.DictionaryIterator(field, nil, kBeg, kEnd)
	}
	if err != nil {
		return nil, err
	}
	// named returns so a Close error is actually reported; a searcher already
	// built must be released rather than dropped when that happens
	defer func() {
		if cerr := fieldDict.Close(); cerr != nil && err == nil {
			if rv != nil {
				_ = rv.Close()
			}
			rv, err = nil, cerr
		}
	}()

	// a prefix scores every match the same, as in Lucene and tantivy,
	// whatever the number of terms it expands to
	score, constant := constantScoreEligible(boost, scorer, compScorer, options)
	rr, ranged := indexReader.(rangeBitmapReader)

	// the range pass needs only to know there are enough terms, so listing
	// them all, a merge of every segment's dictionary, is left for fallbacks
	pt := &prefixTerms{dict: fieldDict, field: field}
	stop := 0
	if constant && ranged && DisjunctionMaxClauseCount == 0 {
		stop = minRangeTerms
	}
	if err := pt.list(stop); err != nil {
		return nil, err
	}

	if constant && len(pt.terms) > 0 {
		if ranged && len(pt.terms) >= minRangeTerms {
			it, err := rr.DocsMatchingRange(field, nil, kBeg, kEnd)
			if err != nil {
				return nil, err
			}
			if it != nil {
				return newTermSearcherFromReader(indexReader, it,
					multiTermConstantScoreTerm, field, score, similarity.ConstantScorer(score), options)
			}
		}
		if err := pt.list(0); err != nil {
			return nil, err
		}
		bterms := make([][]byte, len(pt.terms))
		for i, t := range pt.terms {
			bterms[i] = []byte(t)
		}
		rv, err := newConstantScoreMultiTermSearcher(indexReader, bterms, field, score, options)
		if err != nil || rv != nil {
			return rv, err
		}
	}

	return NewMultiTermSearcher(indexReader, pt.terms, field, boost, scorer, compScorer, options, true)
}

// prefixTerms lists the terms of a dictionary iterator on demand.
type prefixTerms struct {
	dict   segment.DictionaryIterator
	field  string
	terms  []string
	listed bool
}

// list lists terms until there are stop of them, or all if stop is 0.
func (p *prefixTerms) list(stop int) error {
	for !p.listed && (stop == 0 || len(p.terms) < stop) {
		tfd, err := p.dict.Next()
		if err != nil {
			return err
		}
		if tfd == nil {
			p.listed = true
			return nil
		}
		p.terms = append(p.terms, tfd.Term())
		if tooManyClauses(len(p.terms)) {
			return tooManyClausesErr(p.field, len(p.terms))
		}
	}
	return nil
}
