//  Copyright (c) 2020 Couchbase, Inc.
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

package highlight

import (
	"container/heap"

	"github.com/vcaesar/riot/search"
)

const Name = "simple"
const DefaultSeparator = "…"

type SimpleHighlighter struct {
	fragmenter Fragmenter
	formatter  FragmentFormatter
	sep        string
}

func NewSimpleHighlighter(fragmenter Fragmenter, formatter FragmentFormatter, separator string) *SimpleHighlighter {
	return &SimpleHighlighter{
		fragmenter: fragmenter,
		formatter:  formatter,
		sep:        separator,
	}
}

func (s *SimpleHighlighter) BestFragment(tlm search.TermLocationMap, orig []byte) string {
	fragments := s.BestFragments(tlm, orig, 1)
	if len(fragments) > 0 {
		return fragments[0]
	}
	return ""
}

func (s *SimpleHighlighter) BestFragments(tlm search.TermLocationMap, orig []byte, num int) []string {
	orderedTermLocations := OrderTermLocations(tlm)
	scorer := NewFragmentScorer(tlm)

	// score the fragments and put them into a priority queue ordered by score
	fq := make(FragmentQueue, 0)
	heap.Init(&fq)

	termLocationsSameArrayPosition := make(TermLocations, 0)
	termLocationsSameArrayPosition = append(termLocationsSameArrayPosition, orderedTermLocations...)

	fragments := s.fragmenter.Fragment(orig, termLocationsSameArrayPosition)
	for _, fragment := range fragments {
		scorer.Score(fragment)
		heap.Push(&fq, fragment)
	}

	// now find the N best non-overlapping fragments
	var bestFragments []*Fragment
	if len(fq) > 0 {
		candidate := heap.Pop(&fq)
	OUTER:
		for candidate != nil && len(bestFragments) < num {
			// see if this overlaps with any of the best already identified
			if len(bestFragments) > 0 {
				for _, frag := range bestFragments {
					if candidate.(*Fragment).Overlaps(frag) {
						if len(fq) < 1 {
							break OUTER
						}
						candidate = heap.Pop(&fq)
						continue OUTER
					}
				}
				bestFragments = append(bestFragments, candidate.(*Fragment))
			} else {
				bestFragments = append(bestFragments, candidate.(*Fragment))
			}

			if len(fq) < 1 {
				break
			}
			candidate = heap.Pop(&fq)
		}
	}

	// now that we have the best fragments, we can format them
	orderedTermLocations.MergeOverlapping()
	formattedFragments := make([]string, len(bestFragments))
	for i, fragment := range bestFragments {
		formattedFragments[i] = ""
		if fragment.Start != 0 {
			formattedFragments[i] += s.sep
		}
		formattedFragments[i] += s.formatter.Format(fragment, orderedTermLocations)
		if fragment.End != len(fragment.Orig) {
			formattedFragments[i] += s.sep
		}
	}

	return formattedFragments
}

// FragmentQueue implements heap.Interface and holds Items.
type FragmentQueue []*Fragment

func (fq FragmentQueue) Len() int { return len(fq) }

func (fq FragmentQueue) Less(i, j int) bool {
	// We want Pop to give us the highest, not lowest, priority so we use greater-than here.
	return fq[i].Score > fq[j].Score
}

func (fq FragmentQueue) Swap(i, j int) {
	fq[i], fq[j] = fq[j], fq[i]
	fq[i].Index = i
	fq[j].Index = j
}

func (fq *FragmentQueue) Push(x interface{}) {
	n := len(*fq)
	item := x.(*Fragment)
	item.Index = n
	*fq = append(*fq, item)
}

func (fq *FragmentQueue) Pop() interface{} {
	old := *fq
	n := len(old)
	item := old[n-1]
	item.Index = -1 // for safety
	*fq = old[0 : n-1]
	return item
}
