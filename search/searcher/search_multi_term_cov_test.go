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
	"slices"
	"testing"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/similarity"
)

// setInt sets a package setting for the test.
func setInt(t *testing.T, p *int, v int) {
	t.Helper()
	prev := *p
	*p = v
	t.Cleanup(func() { *p = prev })
}

// mtReader serves each term an optimizable postings list over its docs.
func mtReader(lists map[string][]uint64) *covReader {
	r := newCovReader()
	for term, nums := range lists {
		r.its[term] = newCovBatch(covDocs(nums...), 0)
	}
	return r
}

type multiTermCtor func(r search.Reader, terms []string, scorer search.Scorer, comp search.CompositeScorer,
	options search.SearcherOptions, limit bool) (search.Searcher, error)

var multiTermCtors = map[string]multiTermCtor{
	"strings": func(r search.Reader, terms []string, scorer search.Scorer, comp search.CompositeScorer,
		options search.SearcherOptions, limit bool) (search.Searcher, error) {
		return NewMultiTermSearcher(r, terms, "desc", 2, scorer, comp, options, limit)
	},
	"boosts": func(r search.Reader, terms []string, scorer search.Scorer, comp search.CompositeScorer,
		options search.SearcherOptions, limit bool) (search.Searcher, error) {
		boosts := make([]float64, len(terms))
		for i := range boosts {
			boosts[i] = float64(i + 1)
		}
		return NewMultiTermSearcherIndividualBoost(r, terms, boosts, "desc", 2, scorer, comp, options, limit)
	},
	"bytes": func(r search.Reader, terms []string, scorer search.Scorer, comp search.CompositeScorer,
		options search.SearcherOptions, limit bool) (search.Searcher, error) {
		bterms := make([][]byte, len(terms))
		for i, term := range terms {
			bterms[i] = []byte(term)
		}
		return NewMultiTermSearcherBytes(r, bterms, "desc", 2, scorer, comp, options, limit)
	},
}

func TestMultiTermCovTooManyClauses(t *testing.T) {
	none := noExplain()
	none.Score = optionScoringNone
	tests := []struct {
		name    string
		options search.SearcherOptions
		limit   bool
		setup   func(r *covReader)
		want    []uint64
		wantErr bool
	}{
		{name: "optimized", options: none, want: docs(1, 2, 3)},
		{name: "first batch error", options: none, setup: func(r *covReader) { r.postErr["a"] = errCov }, wantErr: true},
		{name: "second batch error", options: none, setup: func(r *covReader) { r.postErr["c"] = errCov }, wantErr: true},
		{name: "optimize error", options: none, setup: func(r *covReader) { r.its["a"].(*covBatch).optErr = errCov },
			wantErr: true},
		{name: "declined", options: none, setup: func(r *covReader) { r.its["c"].(*covBatch).noOpt = true },
			wantErr: true},
		{name: "limit", options: noExplain(), limit: true, wantErr: true},
		{name: "no limit", options: noExplain(), want: docs(1, 2, 3)},
	}
	for name, ctor := range multiTermCtors {
		for _, tc := range tests {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				setInt(t, &DisjunctionMaxClauseCount, 2)
				r := mtReader(map[string][]uint64{"a": {1}, "b": {2}, "c": {3}})
				if tc.setup != nil {
					tc.setup(r)
				}
				s, err := ctor(r, []string{"a", "b", "c"}, nil, similarity.NewCompositeSumScorer(), tc.options, tc.limit)
				if (err != nil) != tc.wantErr {
					t.Fatalf("got %v", err)
				}
				if err != nil {
					return
				}
				if got, err := covDrain(covCtx(), s); err != nil || !slices.Equal(got, tc.want) {
					t.Fatalf("got %v %v", got, err)
				}
			})
		}
	}
}

func TestMultiTermCovErrors(t *testing.T) {
	none := noExplain()
	none.Score = optionScoringNone
	for name, ctor := range multiTermCtors {
		for _, tc := range []struct {
			name    string
			options search.SearcherOptions
			setup   func(r *covReader)
		}{
			{"term", noExplain(), func(r *covReader) { r.postErr["b"] = errCov }},
			{"disjunction", none, func(r *covReader) { r.its["a"].(*covBatch).optErr = errCov }},
		} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				r := mtReader(map[string][]uint64{"a": {1}, "b": {2}})
				tc.setup(r)
				if _, err := ctor(r, []string{"a", "b"}, nil, similarity.NewCompositeSumScorer(), tc.options, true); !errors.Is(err, errCov) {
					t.Fatalf("got %v", err)
				}
			})
		}
	}
}

func TestMultiTermCovConstantScore(t *testing.T) {
	tests := []struct {
		name     string
		reader   func() search.Reader
		maxCount int
		want     []uint64
		wantTerm bool // a constant score term searcher, not a disjunction
		wantErr  bool
	}{
		{name: "bitmap reader", wantTerm: true, want: docs(7), reader: func() search.Reader {
			return &covFastReader{covReader: newCovReader(), it: newCovBatch(covDocs(7), 0)}
		}},
		{name: "bitmap reader error", wantErr: true, reader: func() search.Reader {
			return &covFastReader{covReader: newCovReader(), err: errCov}
		}},
		{name: "bitmap reader declines", wantTerm: true, want: docs(1, 2, 3), reader: func() search.Reader {
			return &covFastReader{covReader: mtReader(map[string][]uint64{"a": {1}, "b": {2}, "c": {3}})}
		}},
		{name: "batches", maxCount: 1, wantTerm: true, want: docs(1, 2, 3), reader: func() search.Reader {
			return mtReader(map[string][]uint64{"a": {1}, "b": {2}, "c": {3}})
		}},
		{name: "batch error", maxCount: 1, wantErr: true, reader: func() search.Reader {
			r := mtReader(map[string][]uint64{"a": {1}, "b": {2}, "c": {3}})
			r.postErr["b"] = errCov
			return r
		}},
		{name: "optimize error", wantErr: true, reader: func() search.Reader {
			r := mtReader(map[string][]uint64{"a": {1}, "b": {2}, "c": {3}})
			r.its["b"].(*covBatch).optErr = errCov
			return r
		}},
		{name: "not optimizable", want: docs(1, 2, 3), reader: func() search.Reader {
			r := mtReader(map[string][]uint64{"a": {1}, "b": {2}, "c": {3}})
			r.its["b"].(*covBatch).noOpt = true
			return r
		}},
	}
	for name, ctor := range multiTermCtors {
		if name == "boosts" {
			continue // individually boosted terms are always scored
		}
		for _, tc := range tests {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				setInt(t, &MultiTermConstantScoreThreshold, 2)
				setInt(t, &DisjunctionMaxClauseCount, tc.maxCount)
				s, err := ctor(tc.reader(), []string{"a", "b", "c"}, nil, similarity.NewCompositeSumScorer(), noExplain(), false)
				if (err != nil) != tc.wantErr {
					t.Fatalf("got %v", err)
				}
				if err != nil {
					return
				}
				ts, isTerm := s.(*TermSearcher)
				if isTerm != tc.wantTerm || (isTerm && ts.scorer != similarity.ConstantScorer(2)) {
					t.Fatalf("got %T", s)
				}
				if got, err := covDrain(covCtx(), s); err != nil || !slices.Equal(got, tc.want) {
					t.Fatalf("got %v %v", got, err)
				}
			})
		}
	}
}

func TestMultiTermCovConstantScoreEligible(t *testing.T) {
	vectors := noExplain()
	vectors.IncludeTermVectors = true
	tests := []struct {
		name      string
		threshold int
		scorer    search.Scorer
		comp      search.CompositeScorer
		options   search.SearcherOptions
		want      float64
		wantOK    bool
	}{
		{"default", 16, nil, nil, noExplain(), 3, true},
		{"sum", 16, nil, similarity.NewCompositeSumScorer(), noExplain(), 3, true},
		{"constant", 16, similarity.ConstantScorer(5), nil, noExplain(), 5, true},
		{"scored", 16, freqScorer{}, nil, noExplain(), 0, false},
		{"other composite", 16, nil, similarity.ConstantScorer(1), noExplain(), 0, false},
		{"vectors", 16, nil, nil, vectors, 0, false},
		{"disabled", 0, nil, nil, noExplain(), 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setInt(t, &MultiTermConstantScoreThreshold, tc.threshold)
			got, ok := constantScoreEligible(3, tc.scorer, tc.comp, tc.options)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("got %v %v", got, ok)
			}
		})
	}
	setInt(t, &MultiTermConstantScoreThreshold, 1)
	if got, ok := constantScoreMultiTerm(2, 3, nil, nil, noExplain()); !ok || got != 3 {
		t.Fatalf("got %v %v", got, ok)
	}
}
