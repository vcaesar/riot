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

package riot

import (
	"context"
	"slices"
	"testing"
)

func shouldTestReader(t *testing.T, docs map[string]string) *Reader {
	t.Helper()
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Errorf("error closing writer: %v", err)
		}
	})
	batch := NewBatch()
	for id, body := range docs {
		batch.Update(Identifier(id), NewDocument(id).AddField(NewTextField("body", body)))
	}
	if err = w.Batch(batch); err != nil {
		t.Fatal(err)
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Errorf("error closing reader: %v", err)
		}
	})
	return r
}

// hitScores returns the scores of all hits by document id.
func hitScores(t *testing.T, r *Reader, req *TopNSearch) map[string]float64 {
	t.Helper()
	dmi, err := r.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	rv := map[string]float64{}
	next, err := dmi.Next()
	for err == nil && next != nil {
		var id string
		if err = next.VisitStoredFields(func(field string, value []byte) bool {
			if field == _idField {
				id = string(value)
			}
			return true
		}); err != nil {
			break
		}
		rv[id] = next.Score
		next, err = dmi.Next()
	}
	if err != nil {
		t.Fatal(err)
	}
	return rv
}

func hitIDs(scores map[string]float64) []string {
	var ids []string
	for id := range scores {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// TestBooleanMinShouldWithoutScoring checks that minShould still requires a
// should match when Score is "none" and the shoulds merge into one postings
// list.
func TestBooleanMinShouldWithoutScoring(t *testing.T) {
	r := shouldTestReader(t, map[string]string{"0": "a b", "1": "a c", "2": "a", "3": "b c"})
	q := NewBooleanQuery().
		AddMust(NewTermQuery("a").SetField("body")).
		AddShould(NewTermQuery("b").SetField("body"), NewTermQuery("c").SetField("body")).
		SetMinShould(1)
	for _, mode := range []string{"", "none"} {
		got := hitIDs(hitScores(t, r, NewTopNSearch(10, q).SetScore(mode)))
		if want := []string{"0", "1"}; !slices.Equal(got, want) {
			t.Errorf("score mode %q: got %v, want %v", mode, got, want)
		}
	}
}

// TestNestedBooleanAdvanceKeepsShouldMatch checks that a boolean driven by
// Advance does not drop a should match its cursor already sits on.
func TestNestedBooleanAdvanceKeepsShouldMatch(t *testing.T) {
	r := shouldTestReader(t, map[string]string{
		"1": "a", "3": "a", "4": "a", "5": "x a b", "9": "b c",
	})
	inner := NewBooleanQuery().
		AddMust(NewTermQuery("a").SetField("body")).
		AddShould(NewTermQuery("b").SetField("body"), NewTermQuery("c").SetField("body"))
	outer := NewBooleanQuery().AddMust(NewTermQuery("x").SetField("body"), inner)
	withShould := hitScores(t, r, NewTopNSearch(10, outer))

	flat := NewBooleanQuery().
		AddMust(NewTermQuery("x").SetField("body"), NewTermQuery("a").SetField("body")).
		AddShould(NewTermQuery("b").SetField("body"))
	want := hitScores(t, r, NewTopNSearch(10, flat))
	if len(withShould) != 1 || withShould["5"] != want["5"] {
		t.Fatalf("doc 5 should score x + a + b = %v, got %v", want["5"], withShould)
	}
}
