//  Copyright (c) 2026 The Bluge Authors.
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

package riot

import (
	"context"
	"testing"

	"github.com/vcaesar/riot/search/searcher"
)

// TestBooleanShouldOnlyShortcut: a should-only boolean with default scoring
// is served by its disjunction (or the single should) directly, with the
// same hits and scores as the wrapped form; boost, musts and explain keep
// the BooleanSearcher.
func TestBooleanShouldOnlyShortcut(t *testing.T) {
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	}()
	b := NewBatch()
	for id, text := range map[string]string{
		"a": "red apple", "b": "green apple pie", "c": "red red wine", "d": "blue sky",
	} {
		b.Update(Identifier(id), NewDocument(id).AddField(NewTextField("text", text)))
	}
	if err = w.Batch(b); err != nil {
		t.Fatal(err)
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	}()

	type hit struct {
		id    string
		score float64
	}
	run := func(q Query, explain bool) []hit {
		t.Helper()
		req := NewTopNSearch(10, q)
		if explain { // explain keeps the BooleanSearcher wrapper: the reference form
			req.ExplainScores()
		}
		dmi, err := r.Search(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		var hits []hit
		m, err := dmi.Next()
		for err == nil && m != nil {
			h := hit{score: m.Score}
			if err = m.VisitStoredFields(func(field string, value []byte) bool {
				if field == "_id" {
					h.id = string(value)
				}
				return true
			}); err != nil {
				t.Fatal(err)
			}
			hits = append(hits, h)
			m, err = dmi.Next()
		}
		if err != nil {
			t.Fatal(err)
		}
		return hits
	}
	same := func(name string, got, want []hit) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: got %v, want %v", name, got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("%s: hit %d = %v, want %v", name, i, got[i], want[i])
			}
		}
	}

	red := NewTermQuery("red").SetField("text")
	apple := NewTermQuery("apple").SetField("text")
	opts := searchOptionsFromConfig(r.config, SearchOptions{})

	// two shoulds → the disjunction itself, same results as the boosted (wrapped) form
	should2 := NewBooleanQuery().AddShould(red, apple)
	if s, err := should2.Searcher(r.reader, opts); err != nil {
		t.Fatal(err)
	} else if _, ok := s.(*searcher.BooleanSearcher); ok {
		t.Fatal("should-only boolean was not unwrapped")
	}
	same("two shoulds", run(should2, false), run(should2, true))
	if got := run(should2, false); len(got) != 3 {
		t.Fatalf("expected 3 hits, got %v", got)
	}

	// one should → the term query itself
	should1 := NewBooleanQuery().AddShould(red).SetMinShould(1)
	if s, err := should1.Searcher(r.reader, opts); err != nil {
		t.Fatal(err)
	} else if _, ok := s.(*searcher.TermSearcher); !ok {
		t.Fatalf("single should returned %T, want *searcher.TermSearcher", s)
	}
	same("one should", run(should1, false), run(should1, true))
	same("one should vs term", run(should1, false), run(red, false))

	// must-only: the conjunction itself, or the single must
	must2 := NewBooleanQuery().AddMust(red, apple)
	if s, err := must2.Searcher(r.reader, opts); err != nil {
		t.Fatal(err)
	} else if _, ok := s.(*searcher.ConjunctionSearcher); !ok {
		t.Fatalf("two musts returned %T, want *searcher.ConjunctionSearcher", s)
	}
	same("two musts", run(must2, false), run(must2, true))
	if got := run(must2, false); len(got) != 1 || got[0].id != "a" {
		t.Fatalf("expected only doc a, got %v", got)
	}
	must1 := NewBooleanQuery().AddMust(red)
	if s, err := must1.Searcher(r.reader, opts); err != nil {
		t.Fatal(err)
	} else if _, ok := s.(*searcher.TermSearcher); !ok {
		t.Fatalf("single must returned %T, want *searcher.TermSearcher", s)
	}
	same("one must", run(must1, false), run(must1, true))

	// a match query is a should-only boolean underneath
	match := NewMatchQuery("red apple").SetField("text")
	same("match", run(match, false), run(match, true))

	// anything that changes scoring or semantics keeps the boolean wrapper
	for name, q := range map[string]*BooleanQuery{
		"boost":       NewBooleanQuery().AddShould(red).SetBoost(2),
		"must+should": NewBooleanQuery().AddMust(apple).AddShould(red),
		"must+boost":  NewBooleanQuery().AddMust(red, apple).SetBoost(2),
		"mustnot":     NewBooleanQuery().AddShould(red).AddMustNot(apple),
	} {
		s, err := q.Searcher(r.reader, opts)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := s.(*searcher.BooleanSearcher); !ok {
			t.Fatalf("%s: got %T, want *searcher.BooleanSearcher", name, s)
		}
		if err = s.Close(); err != nil {
			t.Fatal(err)
		}
	}
	explain := opts
	explain.Explain = true
	s, err := should2.Searcher(r.reader, explain)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.(*searcher.BooleanSearcher); !ok {
		t.Fatalf("explain: got %T, want *searcher.BooleanSearcher", s)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
}
