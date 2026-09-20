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
	"reflect"
	"runtime"
	"testing"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/similarity"
)

func TestPhraseScratchParityAndLifetime(t *testing.T) {
	for _, tc := range []struct {
		name  string
		terms [][]string
		slop  int
		want  []int
	}{
		{"exact", [][]string{{"a"}, {"b"}}, 0, []int{1, 2, 3, 4}},
		{"alternatives", [][]string{{"a", "c"}, {"b"}}, 0, []int{1, 2, 3, 4, 1, 2}},
		{"wildcard", [][]string{{"a"}, {""}, {"a"}}, 0, []int{1, 3}},
		{"sloppy", [][]string{{"a"}, {"b"}}, 2, []int{1, 2, 1, 4, 3, 2, 3, 4}},
		{"repeated", [][]string{{"a"}, {"a"}}, 1, []int{1, 3}},
		{"missing", [][]string{{"a"}, {"missing"}}, 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tlm := search.TermLocationMap{
				"a": {{Pos: 1, Start: 0, End: 1}, {Pos: 3, Start: 4, End: 5}},
				"b": {{Pos: 2, Start: 2, End: 3}, {Pos: 4, Start: 6, End: 7}},
				"c": {{Pos: 1, Start: 0, End: 1}},
			}
			s := &PhraseSearcher{terms: tc.terms, slop: tc.slop}
			got := s.checkCurrMustMatchField("body", tlm, nil)
			var positions []int
			for _, loc := range got {
				positions = append(positions, loc.Location.Pos)
			}
			if !reflect.DeepEqual(positions, tc.want) {
				t.Fatalf("positions: got %v, want %v", positions, tc.want)
			}
			want := append([]search.FieldTermLocation(nil), got...)
			// The scoped arena has already been freed. Reuse the searcher and
			// mutate the input locations, neither of which may change the result.
			for i := 0; i < 32; i++ {
				s.checkCurrMustMatchField("other", tlm, nil)
			}
			for _, locs := range tlm {
				for _, loc := range locs {
					loc.Pos = 99
				}
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			runtime.GC()
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("result changed after scratch release/reuse: got %v, want %v", got, want)
			}
		})
	}
}

func TestPhraseScratchMatchAfterClose(t *testing.T) {
	s, err := NewSloppyMultiPhraseSearcher(baseTestIndexReader, [][]string{{"angst"}, {"beer"}}, "desc", 1, nil,
		search.SearcherOptions{SimilarityForField: func(string) search.Similarity {
			return similarity.NewBM25Similarity()
		}})
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			if err := s.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	ctx := &search.Context{DocumentMatchPool: search.NewDocumentMatchPool(s.DocumentMatchPoolSize(), 0)}
	match, err := s.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if match == nil || len(match.FieldTermLocations) == 0 {
		t.Fatal("expected phrase locations")
	}
	want := append([]search.FieldTermLocation(nil), match.FieldTermLocations...)
	for {
		next, err := s.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if next == nil {
			break
		}
		ctx.DocumentMatchPool.Put(next)
	}
	closed = true
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	if !reflect.DeepEqual(match.FieldTermLocations, want) {
		t.Fatalf("retained match changed: got %v, want %v", match.FieldTermLocations, want)
	}
	match.Complete(nil)
	if len(match.Locations["desc"]["angst"]) == 0 {
		t.Fatal("retained match cannot be completed after Close")
	}
}

func TestPhraseScratchIndependentSearchers(t *testing.T) {
	for i := 0; i < 8; i++ {
		t.Run("parallel", func(t *testing.T) {
			t.Parallel()
			s := &PhraseSearcher{terms: [][]string{{"a"}, {"b"}}, slop: 1}
			tlm := search.TermLocationMap{"a": {{Pos: 1}}, "b": {{Pos: 2}}}
			for j := 0; j < 4; j++ {
				got := s.checkCurrMustMatchField("body", tlm, nil)
				if len(got) != 2 || got[0].Term != "a" || got[1].Location.Pos != 2 {
					t.Fatalf("unexpected locations: %v", got)
				}
			}
		})
	}
}

// Materialized is the previous production strategy: save every completed
// path, then copy its locations into the result. Both variants share traversal.
func BenchmarkPhraseScratch(b *testing.B) {
	tlm := search.TermLocationMap{}
	for i := 0; i < 64; i++ {
		for j, term := range []string{"a", "b", "c"} {
			tlm[term] = append(tlm[term], &search.Location{Pos: i*3 + j + 1})
		}
	}
	terms := [][]string{{"a"}, {"b"}, {"c"}}
	for _, warm := range []bool{false, true} {
		name := "cold"
		if warm {
			name = "warm"
		}
		b.Run(name, func(b *testing.B) {
			b.Run("materialized", func(b *testing.B) {
				var path phrasePath
				var paths []phrasePath
				var out []search.FieldTermLocation
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if path == nil {
						path = make(phrasePath, 0, len(terms))
					}
					paths = findPhrasePaths(terms, tlm, path[:0], 1, paths[:0])
					out = out[:0]
					for _, p := range paths {
						for _, pp := range p {
							out = append(out, search.FieldTermLocation{
								Field: "body", Term: pp.term,
								Location: search.Location{Pos: pp.loc.Pos, Start: pp.loc.Start, End: pp.loc.End},
							})
						}
					}
					if len(out) != 192 {
						b.Fatal(len(out))
					}
					if !warm {
						path, paths, out = nil, nil, nil
					}
				}
			})
			b.Run("streamed", func(b *testing.B) {
				s := PhraseSearcher{terms: terms, slop: 1}
				var out []search.FieldTermLocation
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					out = s.checkCurrMustMatchField("body", tlm, out[:0])
					if len(out) != 192 {
						b.Fatal(len(out))
					}
					if !warm {
						s.path, out = nil, nil
					}
				}
			})
		})
	}
}
