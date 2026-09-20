// Copyright (c) 2026 The Bluge Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package searcher

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"github.com/vcaesar/riot/search"
)

// Exhaustive traversal is intentionally independent of the sorted-window optimization.
func phrasePathsExhaustive(prev int, terms [][]string, tlm search.TermLocationMap,
	path phrasePath, slop int, out []phrasePath) []phrasePath {
	if len(terms) == 0 {
		return append(out, append(phrasePath(nil), path...))
	}
	if len(terms[0]) == 0 || (len(terms[0]) == 1 && terms[0][0] == "") {
		next := 0
		if prev != 0 {
			next = prev + 1
		}
		return phrasePathsExhaustive(next, terms[1:], tlm, path, slop, out)
	}
	for _, term := range terms[0] {
	locations:
		for _, loc := range tlm[term] {
			dist := 0
			if prev != 0 {
				dist = editDistance(prev+1, loc.Pos)
			}
			if prev != 0 && dist > slop {
				continue
			}
			for _, part := range path {
				if part.term == term && part.loc == loc {
					continue locations
				}
			}
			out = phrasePathsExhaustive(loc.Pos, terms[1:], tlm,
				append(path, phrasePart{term: term, loc: loc}), slop-dist, out)
		}
	}
	return out
}

func TestPhrasePositionWindowParity(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for trial := 0; trial < 100; trial++ {
		var input []search.FieldTermLocation
		for _, term := range []string{"a", "b", "c"} {
			for i := 0; i < 24; i++ {
				input = append(input, search.FieldTermLocation{Field: "body", Term: term,
					Location: search.Location{Pos: rng.Intn(80) + 1, Start: i * 2, End: i*2 + 1}})
			}
		}
		// Include identical locations and equal positions with different offsets;
		// Complete must sort and deduplicate before the bounded traversal.
		input = append(input, input[0])
		dm := search.DocumentMatch{FieldTermLocations: append([]search.FieldTermLocation(nil), input...)}
		dm.Complete(nil)
		for _, terms := range [][][]string{
			{{"a"}, {"b"}, {"c"}}, {{"a"}, {"a"}}, {{"a", "b"}, {"c"}},
			{{""}, {"a"}, {}, {"b"}}, {{"a"}, {"missing"}},
		} {
			for _, slop := range []int{-1, 0, 1, 4} {
				want := phrasePathsExhaustive(0, terms, dm.Locations["body"], nil, slop, nil)
				got := findPhrasePaths(terms, dm.Locations["body"], nil, slop, nil)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("trial %d terms %v slop %d: got %v, want %v", trial, terms, slop, got, want)
				}
				// Also exercise exact rejection of unsorted input, Complete, and reuse.
				s := PhraseSearcher{terms: terms, slop: slop, exact: slop == 0 && singleDistinctTerms(terms),
					exactLocs: make([][]int, len(terms)), exactPtr: make([]int, len(terms))}
				for reuse := 0; reuse < 2; reuse++ {
					candidate := search.DocumentMatch{FieldTermLocations: append([]search.FieldTermLocation(nil), input...)}
					s.currMust = &candidate
					match := s.checkCurrMustMatch()
					var expected []search.FieldTermLocation
					for _, p := range want {
						for _, part := range p {
							expected = append(expected, search.FieldTermLocation{Field: "body", Term: part.term, Location: *part.loc})
						}
					}
					if len(expected) == 0 {
						if match != nil {
							t.Fatal("unexpected match")
						}
					} else if match == nil || !reflect.DeepEqual(match.FieldTermLocations, expected) {
						t.Fatalf("trial %d terms %v slop %d: candidate locations differ", trial, terms, slop)
					}
				}
			}
		}
	}
}

func BenchmarkPhrasePositions(b *testing.B) {
	for _, n := range []int{8, 128, 1024} {
		for _, kind := range []string{"sloppy", "repeated", "alternatives", "exact-dense", "exact-sparse"} {
			b.Run(fmt.Sprintf("%s/%d", kind, n), func(b *testing.B) {
				terms := [][]string{{"a"}, {"b"}, {"c"}}
				slop := 0
				switch kind {
				case "sloppy":
					slop = 1
				case "repeated":
					terms = [][]string{{"a"}, {""}, {"a"}}
				case "alternatives":
					terms[1] = []string{"b", "missing"}
				}
				var input []search.FieldTermLocation
				for i := 0; i < n; i++ {
					for j, term := range []string{"a", "b", "c"} {
						if kind == "exact-sparse" && j == 1 && i != n-1 {
							continue
						}
						input = append(input, search.FieldTermLocation{Field: "body", Term: term, Location: search.Location{Pos: i*3 + j + 1}})
					}
				}
				s := PhraseSearcher{terms: terms, slop: slop, exact: slop == 0 && singleDistinctTerms(terms), exactLocs: make([][]int, len(terms)), exactPtr: make([]int, len(terms))}
				dm := search.DocumentMatch{}
				want := n * 3
				if kind == "exact-sparse" {
					want = 3
				}
				if kind == "repeated" {
					want = 0
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					dm.FieldTermLocations = append(dm.FieldTermLocations[:0], input...)
					s.currMust = &dm
					got := s.checkCurrMustMatch()
					if want == 0 {
						if got != nil {
							b.Fatal("unexpected match")
						}
					} else if got == nil || len(got.FieldTermLocations) != want {
						b.Fatal("incorrect locations")
					}
				}
			})
		}
	}
}
