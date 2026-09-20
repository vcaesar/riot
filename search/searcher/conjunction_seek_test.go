// Copyright (c) 2026 The Bluge Authors.
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
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/similarity"
)

// A forward-only postings mock. Advance materializes only the target posting.
type conjunctionPostings struct {
	docs       []uint64
	pos        int
	seeks      int
	nexts      int
	advanceErr error
}

func (s *conjunctionPostings) Next(ctx *search.Context) (*search.DocumentMatch, error) {
	s.nexts++
	if s.pos == len(s.docs) {
		return nil, nil
	}
	rv := ctx.DocumentMatchPool.Get()
	rv.Number = s.docs[s.pos]
	rv.Score = 1
	s.pos++
	return rv, nil
}
func (s *conjunctionPostings) Advance(ctx *search.Context, target uint64) (*search.DocumentMatch, error) {
	s.seeks++
	if s.advanceErr != nil {
		return nil, s.advanceErr
	}
	s.pos += sort.Search(len(s.docs)-s.pos, func(i int) bool { return s.docs[s.pos+i] >= target })
	return s.Next(ctx)
}
func (s *conjunctionPostings) Count() uint64              { return uint64(len(s.docs)) }
func (s *conjunctionPostings) Close() error               { return nil }
func (s *conjunctionPostings) Min() int                   { return 0 }
func (s *conjunctionPostings) Size() int                  { return 0 }
func (s *conjunctionPostings) DocumentMatchPoolSize() int { return 1 }

func seekConjunction(lists [][]uint64) (*ConjunctionSearcher, *search.Context) {
	children := make(OrderedSearcherList, len(lists))
	for i, docs := range lists {
		children[i] = &conjunctionPostings{docs: docs}
	}
	sort.Sort(children)
	return &ConjunctionSearcher{
		searchers: children, currs: make([]*search.DocumentMatch, len(children)),
		scorer: similarity.NewCompositeSumScorer(),
	}, search.NewSearchContext(2*len(children)+1, 0)
}

func TestConjunctionSeekIntersection(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	cases := [][][]uint64{
		nil, {nil}, {{0, 1, 9}}, {{0, 2}, nil},
		{{0, 100, 200}, {0, 50, 200}, {1, 50, 200}},
		{{0, ^uint64(0)}, {1, ^uint64(0)}},
	}
	for n := 0; n < 200; n++ {
		lists := make([][]uint64, 2+rng.Intn(7))
		for i := range lists {
			for doc := uint64(0); doc < 150; doc++ {
				if rng.Intn(3) == 0 {
					lists[i] = append(lists[i], doc)
				}
			}
		}
		cases = append(cases, lists)
	}
	for n, lists := range cases {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			for _, advance := range []bool{false, true} {
				s, ctx := seekConjunction(lists)
				var want []uint64
				if len(lists) > 0 {
					for _, doc := range lists[0] {
						present := true
						for _, list := range lists[1:] {
							i := sort.Search(len(list), func(i int) bool { return list[i] >= doc })
							if i == len(list) || list[i] != doc {
								present = false
								break
							}
						}
						if present {
							want = append(want, doc)
						}
					}
				}
				var got []uint64
				target := uint64(0)
				for {
					var match *search.DocumentMatch
					var err error
					if advance {
						match, err = s.Advance(ctx, target)
					} else {
						match, err = s.Next(ctx)
					}
					if err != nil {
						t.Fatal(err)
					}
					if match == nil {
						break
					}
					got = append(got, match.Number)
					if match.Score != float64(len(lists)) {
						t.Fatalf("score: %v", match.Score)
					}
					target = match.Number
					ctx.DocumentMatchPool.Put(match)
					// Exercise seeks that skip results, including terminal uint64 IDs.
					if advance && target < ^uint64(0)-3 {
						target += 3
						for i := len(got); i < len(want) && want[i] < target; {
							want = append(want[:i], want[i+1:]...)
						}
					}
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("advance=%v: got %v want %v", advance, got, want)
				}
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestConjunctionSeekSkipsStaleCandidate(t *testing.T) {
	lists := [][]uint64{{0, 90, 91}, {0, 10, 90}, {0, 10, 90}, {0, 10, 90}, {0, 10, 90}, {10, 90, 91}}
	s, ctx := seekConjunction(lists)
	match, err := s.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if match == nil || match.Number != 90 || match.Score != 6 {
		t.Fatalf("unexpected match: %+v", match)
	}
	seeks := 0
	for _, child := range s.searchers {
		seeks += child.(*conjunctionPostings).seeks
	}
	if seeks != 6 {
		t.Fatalf("got %d seeks, want 6 (old loop needed 10)", seeks)
	}
	ctx.DocumentMatchPool.Put(match)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestConjunctionSeekError(t *testing.T) {
	for _, child := range []int{0, 1, 2} {
		s, ctx := seekConjunction([][]uint64{{0, 90, 91}, {0, 10, 90}, {10, 90, 91}})
		want := errors.New("seek failed")
		s.searchers[child].(*conjunctionPostings).advanceErr = want
		match, err := s.Next(ctx)
		if !errors.Is(err, want) || match != nil {
			t.Fatalf("child %d: match=%v err=%v", child, match, err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestConjunctionAdvanceDirect(t *testing.T) {
	s, ctx := seekConjunction([][]uint64{{0, 90, 100}, {0, 50, 90, 100}, {0, 20, 50, 90, 100}})
	match, err := s.Advance(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if match == nil || match.Number != 90 || match.Score != 3 {
		t.Fatalf("unexpected match: %+v", match)
	}
	for i, child := range s.searchers {
		postings := child.(*conjunctionPostings)
		if postings.seeks != 1 || postings.nexts != 2 {
			t.Errorf("child %d: seeks=%d nexts=%d, want 1 and 2", i, postings.seeks, postings.nexts)
		}
	}
	ctx.DocumentMatchPool.Put(match)
	match, err = s.Advance(ctx, 101)
	if err != nil || match != nil {
		t.Fatalf("exhaustion: match=%v err=%v", match, err)
	}
	for _, next := range []func(*search.Context) (*search.DocumentMatch, error){s.Next,
		func(ctx *search.Context) (*search.DocumentMatch, error) { return s.Advance(ctx, 102) }} {
		match, err = next(ctx)
		if err != nil || match != nil {
			t.Fatalf("after exhaustion: match=%v err=%v", match, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkConjunctionAdvance(b *testing.B) {
	lists := make([][]uint64, 6)
	for i := range lists {
		for doc := uint64(0); doc < 8192; doc++ {
			if i != 0 || doc%97 == 0 {
				lists[i] = append(lists[i], doc)
			}
		}
	}
	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		s, ctx := seekConjunction(lists)
		for target := uint64(1); ; {
			match, err := s.Advance(ctx, target)
			if err != nil {
				b.Fatal(err)
			}
			if match == nil {
				break
			}
			target = match.Number + 40
			ctx.DocumentMatchPool.Put(match)
		}
		if err := s.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConjunctionSeek(b *testing.B) {
	for _, workload := range []string{"dense", "sparse", "staggered", "overshoot"} {
		for _, width := range []int{2, 6} {
			lists := make([][]uint64, width)
			for i := range lists {
				for doc := uint64(0); doc < 8192; doc++ {
					present := true
					switch workload {
					case "overshoot":
						pos := doc % 100
						if i == 0 {
							present = pos == 0 || pos == 90 || pos == 91
						} else if i == width-1 {
							present = pos == 10 || pos == 90 || pos == 91
						} else {
							present = pos == 0 || pos == 10 || pos == 90
						}
					case "sparse":
						present = doc%uint64(17+i*12) == 0
					case "staggered":
						present = doc%uint64(width) == uint64(i) || doc%257 == 0
					}
					if present {
						lists[i] = append(lists[i], doc)
					}
				}
			}
			b.Run(fmt.Sprintf("%s/%d", workload, width), func(b *testing.B) {
				b.ReportAllocs()
				var seeks int
				for n := 0; n < b.N; n++ {
					s, ctx := seekConjunction(lists)
					for {
						match, err := s.Next(ctx)
						if err != nil {
							b.Fatal(err)
						}
						if match == nil {
							break
						}
						ctx.DocumentMatchPool.Put(match)
					}
					for _, child := range s.searchers {
						seeks += child.(*conjunctionPostings).seeks
					}
					if err := s.Close(); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(seeks)/float64(b.N), "seeks/op")
			})
		}
	}
}
