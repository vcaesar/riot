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
	"sync"
	"unicode/utf8"

	segment "github.com/vcaesar/bluge_segment_api"

	"github.com/blevesearch/vellum"
	"github.com/blevesearch/vellum/levenshtein"
	"github.com/vcaesar/riot/search"
)

// levAutomatonBuilders holds reusable, thread-safe levenshtein builders indexed
// by fuzziness. They are built on first use: building both costs ~2 ms and
// ~1 MB at startup, which every process paid even if it never ran a fuzzy query.
var levAutomatonBuilders = [...]func() (*levenshtein.LevenshteinAutomatonBuilder, error){
	1: sync.OnceValues(func() (*levenshtein.LevenshteinAutomatonBuilder, error) {
		return levenshtein.NewLevenshteinAutomatonBuilder(1, true)
	}),
	2: sync.OnceValues(func() (*levenshtein.LevenshteinAutomatonBuilder, error) {
		return levenshtein.NewLevenshteinAutomatonBuilder(2, true)
	}),
}

var MaxFuzziness = 2

func NewFuzzySearcher(indexReader search.Reader, term string,
	prefix, fuzziness int, field string, boost float64, scorer search.Scorer,
	compScorer search.CompositeScorer, options search.SearcherOptions) (search.Searcher, error) {
	if fuzziness > MaxFuzziness {
		return nil, fmt.Errorf("fuzziness exceeds max (%d)", MaxFuzziness)
	}

	if fuzziness < 0 {
		return nil, fmt.Errorf("invalid fuzziness, negative")
	}

	// distance 0 is the exact term, which already has the prefix
	if fuzziness == 0 {
		return NewTermSearcher(indexReader, term, field, boost, scorer, options)
	}

	// Note: we don't byte slice the term for a prefix because of runes.
	prefixTerm := ""
	for i, r := range term {
		if i < prefix {
			prefixTerm += string(r)
		} else {
			break
		}
	}
	candidateTerms, termBoosts, err := findFuzzyCandidateTerms(indexReader, term, fuzziness,
		field, prefixTerm)
	if err != nil {
		return nil, err
	}

	return NewMultiTermSearcherIndividualBoost(indexReader, candidateTerms, termBoosts, field,
		boost, scorer, compScorer, options, true)
}

func findFuzzyCandidateTerms(indexReader search.Reader, term string,
	fuzziness int, field, prefixTerm string) (terms []string, boosts []float64, err error) {
	automatons, err := getLevAutomatons(term, fuzziness)
	if err != nil {
		return nil, nil, err
	}

	var prefixBeg, prefixEnd []byte
	if prefixTerm != "" {
		prefixBeg = []byte(prefixTerm)
		prefixEnd = incrementBytes(prefixBeg)
	}

	fieldDict, err := indexReader.DictionaryIterator(field, automatons[0], prefixBeg, prefixEnd)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if cerr := fieldDict.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	termLen := utf8.RuneCountInString(term)

	tfd, err := fieldDict.Next()
	for err == nil && tfd != nil {
		terms = append(terms, tfd.Term())
		if tooManyClauses(len(terms)) {
			return nil, nil, tooManyClausesErr(field, len(terms))
		}
		// compute actual edit distance for this term
		boost := 1.0
		if tfd.Term() != term {
			boost = boostFromDistance(fuzziness, automatons, tfd.Term(), termLen)
		}
		boosts = append(boosts, boost)
		tfd, err = fieldDict.Next()
	}
	return terms, boosts, err
}

func boostFromDistance(fuzziness int, automatons []segment.Automaton, dictTerm string, searchTermLen int) float64 {
	termEditDistance := fuzziness // start assuming it is fuzziness of automaton that found it
	for i := 1; i < len(automatons); i++ {
		if vellum.AutomatonContains(automatons[i], []byte(dictTerm)) {
			termEditDistance--
		}
	}
	minTermLen := searchTermLen
	thisTermLen := utf8.RuneCountInString(dictTerm)
	if thisTermLen < minTermLen {
		minTermLen = thisTermLen
	}
	return 1.0 - (float64(termEditDistance) / float64(minTermLen))
}

func getLevAutomaton(term string, fuzziness int) (segment.Automaton, error) {
	if fuzziness < 1 || fuzziness >= len(levAutomatonBuilders) {
		return nil, fmt.Errorf("unsupported fuzziness: %d", fuzziness)
	}
	levAutomatonBuilder, err := levAutomatonBuilders[fuzziness]()
	if err != nil {
		return nil, fmt.Errorf("error building levenshtein automaton builder: %v", err)
	}
	return levAutomatonBuilder.BuildDfa(term, uint8(fuzziness))
}

func getLevAutomatons(term string, maxFuzziness int) (rv []segment.Automaton, err error) {
	for fuzziness := maxFuzziness; fuzziness > 0; fuzziness-- {
		var levAutomaton segment.Automaton
		levAutomaton, err = getLevAutomaton(term, fuzziness)
		if err != nil {
			return nil, err
		}
		rv = append(rv, levAutomaton)
	}
	return rv, nil
}
