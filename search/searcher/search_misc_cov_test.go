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
	"math"
	"slices"
	"testing"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/similarity"
)

// covExclusionConj is an unread conjunction of term searchers over its
// lists, which may be optimized once it is built.
func covExclusionConj(t *testing.T, lists ...*covBatch) *ConjunctionSearcher {
	t.Helper()
	children := make([]search.Searcher, len(lists))
	for i, l := range lists {
		l.noOpt = true
		children[i] = covTerm(l, noExplain())
	}
	s := newCovConjunction(t, children...)
	for _, l := range lists {
		l.noOpt = false
	}
	return s
}

func TestExcludeFromConjunctionCov(t *testing.T) {
	ctx := covCtx()
	term := func(b *covBatch) search.Searcher { return covTerm(b, noExplain()) }
	disj := func(t *testing.T, heap bool, children ...search.Searcher) search.Searcher {
		if heap {
			useHeap(t)
		}
		return newCovDisjunction(t, 0, noExplain(), children...)
	}
	tests := []struct {
		name     string
		must     func(t *testing.T) search.Searcher
		mustNot  func(t *testing.T) search.Searcher
		want     bool
		wantErr  bool
		wantDocs []uint64
	}{
		{name: "term", want: true, wantDocs: docs(2),
			must:    func(t *testing.T) search.Searcher { return covExclusionConj(t, newCovBatch(covDocs(1, 2), 0)) },
			mustNot: func(*testing.T) search.Searcher { return term(newCovBatch(covDocs(1), 0)) }},
		{name: "slice", want: true,
			must: func(t *testing.T) search.Searcher { return covExclusionConj(t, newCovBatch(covDocs(1), 0)) },
			mustNot: func(t *testing.T) search.Searcher {
				return disj(t, false, term(newCovBatch(covDocs(1), 0)), term(newCovBatch(covDocs(2), 0)))
			}},
		{name: "heap", want: true,
			must: func(t *testing.T) search.Searcher { return covExclusionConj(t, newCovBatch(covDocs(1), 0)) },
			mustNot: func(t *testing.T) search.Searcher {
				return disj(t, true, term(newCovBatch(covDocs(1), 0)), term(newCovBatch(covDocs(2), 0)))
			}},
		{name: "not a conjunction",
			must:    func(*testing.T) search.Searcher { return &covSearcher{} },
			mustNot: func(*testing.T) search.Searcher { return term(newCovBatch(covDocs(1), 0)) }},
		{name: "read conjunction",
			must: func(t *testing.T) search.Searcher {
				s := covExclusionConj(t, newCovBatch(covDocs(1), 0))
				if _, err := s.Next(ctx); err != nil {
					t.Fatal(err)
				}
				return s
			},
			mustNot: func(*testing.T) search.Searcher { return term(newCovBatch(covDocs(1), 0)) }},
		{name: "raw conjunction",
			must: func(t *testing.T) search.Searcher {
				s := covExclusionConj(t, newCovBatch(covDocs(1), 0), newCovBatch(covDocs(1), 0))
				if _, err := s.NextAbove(ctx, math.Inf(-1), nil); err != nil {
					t.Fatal(err)
				}
				s.initialized = false
				return s
			},
			mustNot: func(*testing.T) search.Searcher { return term(newCovBatch(covDocs(1), 0)) }},
		{name: "read slice",
			must: func(t *testing.T) search.Searcher { return covExclusionConj(t, newCovBatch(covDocs(1), 0)) },
			mustNot: func(t *testing.T) search.Searcher {
				s := disj(t, false, &covSearcher{docs: docs(1)}, &covSearcher{docs: docs(2)})
				if _, err := s.Next(ctx); err != nil {
					t.Fatal(err)
				}
				return s
			}},
		{name: "read heap",
			must: func(t *testing.T) search.Searcher { return covExclusionConj(t, newCovBatch(covDocs(1), 0)) },
			mustNot: func(t *testing.T) search.Searcher {
				s := disj(t, true, &covSearcher{docs: docs(1)}, &covSearcher{docs: docs(2)})
				if _, err := s.Next(ctx); err != nil {
					t.Fatal(err)
				}
				return s
			}},
		{name: "other must not",
			must:    func(t *testing.T) search.Searcher { return covExclusionConj(t, newCovBatch(covDocs(1), 0)) },
			mustNot: func(*testing.T) search.Searcher { return &covSearcher{} }},
		{name: "must not of searchers",
			must: func(t *testing.T) search.Searcher { return covExclusionConj(t, newCovBatch(covDocs(1), 0)) },
			mustNot: func(t *testing.T) search.Searcher {
				return disj(t, false, &covSearcher{docs: docs(1)}, &covSearcher{docs: docs(2)})
			}},
		{name: "conjunction of searchers",
			must: func(t *testing.T) search.Searcher {
				return newCovConjunction(t, &covSearcher{docs: docs(1)}, &covSearcher{docs: docs(1)})
			},
			mustNot: func(*testing.T) search.Searcher { return term(newCovBatch(covDocs(1), 0)) }},
		{name: "declined",
			must: func(t *testing.T) search.Searcher { return covExclusionConj(t, newCovBatch(covDocs(1), 0)) },
			mustNot: func(*testing.T) search.Searcher {
				b := newCovBatch(covDocs(1), 0)
				b.noOpt = true
				return term(b)
			}},
		{name: "optimize error", wantErr: true,
			must: func(t *testing.T) search.Searcher { return covExclusionConj(t, newCovBatch(covDocs(1), 0)) },
			mustNot: func(*testing.T) search.Searcher {
				b := newCovBatch(covDocs(1), 0)
				b.optErr = errCov
				return term(b)
			}},
		{name: "finish error", wantErr: true,
			must: func(t *testing.T) search.Searcher { return covExclusionConj(t, newCovBatch(covDocs(1), 0)) },
			mustNot: func(*testing.T) search.Searcher {
				b := newCovBatch(covDocs(1), 0)
				b.finishErr = errCov
				return term(b)
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			must := tc.must(t)
			got, err := ExcludeFromConjunction(must, tc.mustNot(t))
			if got != tc.want || (err != nil) != tc.wantErr {
				t.Fatalf("got %v %v", got, err)
			}
			if tc.wantErr && !errors.Is(err, errCov) {
				t.Fatalf("got %v", err)
			}
			if tc.wantDocs != nil {
				if hits, err := covDrain(ctx, must); err != nil || !slices.Equal(hits, tc.wantDocs) {
					t.Fatalf("docs: got %v %v, want %v", hits, err, tc.wantDocs)
				}
			}
		})
	}
}

func TestMatchAllSearcherCov(t *testing.T) {
	r := newCovReader()
	r.postErr[""] = errCov
	if _, err := NewMatchAllSearcher(r, 1, similarity.ConstantScorer(1), noExplain()); !errors.Is(err, errCov) {
		t.Fatalf("new: %v", err)
	}
	newAll := func(t *testing.T, p *covPostings, options search.SearcherOptions) *MatchAllSearcher {
		t.Helper()
		r := newCovReader()
		r.its[""] = p
		s, err := NewMatchAllSearcher(r, 1, freqScorer{}, options)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	ctx := covCtx()
	s := newAll(t, &covPostings{docs: covDocs(1, 2, 3)}, noExplain())
	if s.Size() <= reflectStaticSizeMatchAllSearcher || s.Count() != 3 || s.Min() != 0 {
		t.Fatalf("size %d count %d", s.Size(), s.Count())
	}
	if dm, err := s.Advance(ctx, 2); err != nil || dm.Number != 2 || dm.Score != 1 {
		t.Fatalf("advance: %v %v", dm, err)
	}
	if dm, err := s.Advance(ctx, 9); err != nil || dm != nil {
		t.Fatalf("advance past end: %v %v", dm, err)
	}
	for _, tc := range []struct {
		name string
		p    *covPostings
		call func(s *MatchAllSearcher) error
	}{
		{"next", &covPostings{nextErr: errCov}, func(s *MatchAllSearcher) error { _, err := s.Next(ctx); return err }},
		{"advance", &covPostings{advErr: errCov}, func(s *MatchAllSearcher) error { _, err := s.Advance(ctx, 1); return err }},
		{"next above", &covPostings{nextErr: errCov}, func(s *MatchAllSearcher) error {
			_, err := s.NextAbove(ctx, 0, func(float64, int) bool { return true })
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(newAll(t, tc.p, noExplain())); !errors.Is(err, errCov) {
				t.Fatalf("got %v", err)
			}
		})
	}
	explain := noExplain()
	explain.Explain = true
	for _, tc := range []struct {
		name        string
		options     search.SearcherOptions
		wantHits    []uint64
		wantSkipped int
	}{
		{"scored", noExplain(), docs(3), 2},
		{"explain", explain, docs(1, 2, 3), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &covPostings{docs: []covDoc{{num: 1, freq: 1}, {num: 2, freq: 1}, {num: 3, freq: 2}}}
			hits, skipped, err := nextAboveAll(t, newAll(t, p, tc.options), ctx, 1, always)
			if err != nil || !slices.Equal(hits, tc.wantHits) || skipped != tc.wantSkipped {
				t.Fatalf("hits %v skipped %d err %v", hits, skipped, err)
			}
		})
	}
}

func TestFilteringSearcherCov(t *testing.T) {
	odd := func(d *search.DocumentMatch) bool { return d.Number%2 == 1 }
	ctx := covCtx()
	f := NewFilteringSearcher(&covSearcher{docs: docs(1, 2, 4, 5, 6), min: 3, closeErr: errCov}, odd)
	if f.Size() <= reflectStaticSizeFilteringSearcher || f.Min() != 3 || f.Count() != 5 || f.DocumentMatchPoolSize() != 1 {
		t.Fatalf("size %d min %d", f.Size(), f.Min())
	}
	for _, tc := range []struct{ target, want uint64 }{{1, 1}, {2, 5}} {
		if dm, err := f.Advance(ctx, tc.target); err != nil || dm.Number != tc.want {
			t.Fatalf("advance %d: %v %v", tc.target, dm, err)
		}
	}
	if dm, err := f.Advance(ctx, 7); err != nil || dm != nil {
		t.Fatalf("advance past end: %v %v", dm, err)
	}
	if err := f.Close(); !errors.Is(err, errCov) {
		t.Fatalf("close: %v", err)
	}
	f = NewFilteringSearcher(&covSearcher{advErr: errCov}, odd)
	if _, err := f.Advance(ctx, 1); !errors.Is(err, errCov) {
		t.Fatalf("advance: %v", err)
	}
}

func TestMatchNoneSearcherCov(t *testing.T) {
	s, err := NewMatchNoneSearcher(baseTestIndexReader, noExplain())
	if err != nil {
		t.Fatal(err)
	}
	s.SetQueryNorm(2)
	dm, err := s.Advance(covCtx(), 1)
	if dm != nil || err != nil || s.Weight() != 0 || s.Count() != 0 || s.Min() != 0 || s.Size() <= 0 {
		t.Fatalf("got %v %v", dm, err)
	}
}
