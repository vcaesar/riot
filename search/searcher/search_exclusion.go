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

// ExcludeFromConjunction removes the documents mustNot matches from the
// postings of must before either is read, like Lucene's ReqExclBulkScorer
// masks excluded documents for the required scorer. must then yields only
// allowed documents and keeps its block-max skipping with exact counts,
// which a per-hit must-not check cannot offer, and mustNot is no longer
// needed. It returns false, changing nothing, unless must is an unread
// conjunction of term searchers and mustNot is a term searcher or an
// unread disjunction of them.
func ExcludeFromConjunction(must, mustNot search.Searcher) (bool, error) {
	conj, ok := must.(*ConjunctionSearcher)
	if !ok || conj.initialized || conj.raw != nil {
		return false, nil
	}
	excludes := mustNotTerms(mustNot)
	if excludes == nil {
		return false, nil
	}
	var octx segment.OptimizableContext
	var err error
	register := func(kind string, searchers []search.Searcher) bool {
		for _, s := range searchers {
			ts, ok := s.(*TermSearcher)
			if !ok {
				return false
			}
			if octx, err = ts.Optimize(kind, octx); err != nil || octx == nil {
				return false
			}
		}
		return true
	}
	if !register("exclusion:include", conj.searchers) || !register("exclusion:exclude", excludes) {
		return false, err
	}
	if _, err = octx.Finish(); err != nil {
		return false, err
	}
	ex, ok := octx.(interface{ Excluded() bool })
	return ok && ex.Excluded(), nil
}

// mustNotTerms returns the term searchers whose union mustNot is, or nil.
func mustNotTerms(mustNot search.Searcher) []search.Searcher {
	switch s := mustNot.(type) {
	case *TermSearcher:
		return []search.Searcher{s}
	case *DisjunctionSliceSearcher:
		if !s.initialized && s.raw == nil && s.min <= 1 {
			return s.searchers
		}
	case *DisjunctionHeapSearcher:
		if !s.initialized && s.min <= 1 {
			return s.searchers
		}
	}
	return nil
}
