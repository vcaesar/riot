// Copyright (c) 2026 The AtomAI.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package riot

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/aggregations"
)

// The raw phrase path snapshots reusable postings. Compare scores and every
// location with the general (explain) path across seeks, segments and deletes.
func TestPhrasePostingSnapshots(t *testing.T) {
	for _, optimize := range []bool{false, true} {
		cfg := InMemoryOnlyConfig()
		if !optimize {
			cfg = cfg.DisableOptimizeConjunction()
		}
		w, err := OpenWriter(cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := w.Close(); err != nil {
				t.Error(err)
			}
		})
		bodies := []string{
			"search engine fast", "engine search fast", "search fast",
			"engine fast", "search engine fast search engine fast",
			strings.Repeat("filler ", 300) + "search engine fast", "unrelated",
		}
		for batch := range 3 {
			b := NewBatch()
			for i := range 43 {
				id := batch*43 + i
				b.Insert(NewDocument(strconv.Itoa(id)).AddField(NewTextField("body", bodies[id%len(bodies)]).SearchTermPositions()))
			}
			if err := w.Batch(b); err != nil {
				t.Fatal(err)
			}
		}
		b := NewBatch()
		b.Delete(Identifier("0"))
		b.Delete(Identifier("48"))
		if err := w.Batch(b); err != nil {
			t.Fatal(err)
		}
		r, err := w.Reader()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := r.Close(); err != nil {
				t.Error(err)
			}
		})
		type hit struct {
			number    uint64
			score     float64
			locations []search.FieldTermLocation
		}
		for _, phrase := range []string{"search engine", "search engine fast", "engine search", "search absent"} {
			read := func(explain bool) []hit {
				req := NewAllMatches(NewMatchPhraseQuery(phrase).SetField("body")).IncludeLocations()
				if explain {
					req.ExplainScores()
				}
				it, err := r.Search(context.Background(), req)
				if err != nil {
					t.Fatal(err)
				}
				var hits []hit
				for {
					m, err := it.Next()
					if err != nil {
						t.Fatal(err)
					}
					if m == nil {
						return hits
					}
					m.Complete(nil)
					h := hit{number: m.Number, score: m.Score}
					for _, term := range strings.Fields(phrase) {
						for _, loc := range m.Locations["body"][term] {
							h.locations = append(h.locations, search.FieldTermLocation{Field: "body", Term: term, Location: *loc})
						}
					}
					if len(h.locations) == 0 {
						t.Fatal("phrase hit without locations")
					}
					hits = append(hits, h)
				}
			}
			got, want := read(false), read(true)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("optimize=%v phrase=%q: raw and general phrase results differ\ngot=%v\nwant=%v", optimize, phrase, got, want)
			}
			if phrase != "search absent" && len(got) == 0 {
				t.Fatal("expected phrase hits")
			}
		}
	}
}

// Raw phrases read postings in batches and seek a sparse term's partner
// with Advance across gaps; under a TopN floor they report skipped hits in
// runs. Compare all hits and TopN + count with the general (explain) path.
func TestPhraseBatchesSeeksAndSkippedRuns(t *testing.T) {
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	body := func(i int) string {
		switch i % 1000 {
		case 7, 8:
			return "rare common filler"
		case 500:
			return "common rare"
		}
		return "common filler" + strings.Repeat(" pad", i%3) + " common filler"
	}
	id := 0
	for _, size := range []int{5000, 300} {
		b := NewBatch()
		for range size {
			b.Insert(NewDocument(strconv.Itoa(id)).AddField(NewTextField("body", body(id)).SearchTermPositions()))
			id++
		}
		if err := w.Batch(b); err != nil {
			t.Fatal(err)
		}
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	type hit struct {
		number uint64
		score  float64
		locs   int
	}
	read := func(req SearchRequest) ([]hit, uint64) {
		it, err := r.Search(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		var hits []hit
		for {
			m, err := it.Next()
			if err != nil {
				t.Fatal(err)
			}
			if m == nil {
				return hits, it.Aggregations().Count()
			}
			m.Complete(nil)
			n := 0
			for _, tl := range m.Locations["body"] {
				n += len(tl)
			}
			hits = append(hits, hit{m.Number, m.Score, n})
		}
	}
	for _, tc := range []struct {
		phrase string
		count  uint64
	}{{"rare common", 12}, {"common filler", 5295}, {"filler common", 1761}, {"filler rare", 0}} {
		q := func() *MatchPhraseQuery { return NewMatchPhraseQuery(tc.phrase).SetField("body") }
		all, _ := read(NewAllMatches(q()).IncludeLocations())
		allExplain, _ := read(NewAllMatches(q()).IncludeLocations().ExplainScores())
		if !reflect.DeepEqual(all, allExplain) || uint64(len(all)) != tc.count {
			t.Fatalf("%q: %d hits differ from explain or want %d", tc.phrase, len(all), tc.count)
		}
		top := func(explain bool) ([]hit, uint64) {
			req := NewTopNSearch(3, q())
			req.AddAggregation("count", aggregations.CountMatches())
			if explain {
				req.ExplainScores()
			}
			return read(req)
		}
		got, gotCount := top(false)
		want, wantCount := top(true)
		if !reflect.DeepEqual(got, want) || gotCount != tc.count || wantCount != tc.count {
			t.Fatalf("%q: top %v count %d, want %v count %d (all %d)", tc.phrase, got, gotCount, want, wantCount, tc.count)
		}
	}
}

// A phrase in (nearly) every document, each term once with one norm, is
// counted under a TopN floor by the skipped-run loop over dense postings
// runs. Documents with the terms reversed, repeated or one missing break
// the runs; TopN + count must match the general (explain) path.
func TestPhraseDenseSkippedRuns(t *testing.T) {
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	var want uint64
	body := func(i int) string {
		switch {
		case i%911 == 5:
			return "engine search zig doc" // reversed
		case i%1301 == 9:
			want++
			return "search engine search engine" // twice
		case i%1733 == 1:
			return "search zig doc pad" // no engine
		}
		want++
		return "search engine zig doc"
	}
	id := 0
	for _, size := range []int{6000, 2500} {
		b := NewBatch()
		for range size {
			b.Insert(NewDocument(strconv.Itoa(id)).AddField(NewTextField("body", body(id)).SearchTermPositions()))
			id++
		}
		if err := w.Batch(b); err != nil {
			t.Fatal(err)
		}
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	top := func(explain bool) ([]uint64, []float64, uint64) {
		req := NewTopNSearch(10, NewMatchPhraseQuery("search engine").SetField("body"))
		req.AddAggregation("count", aggregations.CountMatches())
		if explain {
			req.ExplainScores()
		}
		it, err := r.Search(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		var numbers []uint64
		var scores []float64
		for {
			m, err := it.Next()
			if err != nil {
				t.Fatal(err)
			}
			if m == nil {
				return numbers, scores, it.Aggregations().Count()
			}
			numbers, scores = append(numbers, m.Number), append(scores, m.Score)
		}
	}
	gotN, gotS, gotCount := top(false)
	wantN, wantS, wantCount := top(true)
	if !reflect.DeepEqual(gotN, wantN) || !reflect.DeepEqual(gotS, wantS) || gotCount != want || wantCount != want {
		t.Fatalf("top %v %v count %d, want %v %v count %d (%d)", gotN, gotS, gotCount, wantN, wantS, wantCount, want)
	}
}

// Phrase hits carry the byte offsets of their terms, which the raw path
// loads only for the hits it returns: body[Start:End] is the term and
// positions follow the phrase, for all matches and TopN, as the general
// (explain) path reports them.
func TestPhraseHitOffsets(t *testing.T) {
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	body := func(i int) string {
		pad := strings.Repeat("pad ", i%40)
		if i%250 == 0 {
			pad = strings.Repeat("padding ", 2100) // starts past 16K
		}
		s := pad + "quick brown fox"
		if i%3 == 0 {
			s += " jumps quick brown"
		}
		return s
	}
	id := 0
	for _, size := range []int{3000, 700, 50} {
		b := NewBatch()
		for range size {
			b.Insert(NewDocument(strconv.Itoa(id)).AddField(NewTextField("body", body(id)).SearchTermPositions()))
			id++
		}
		if err := w.Batch(b); err != nil {
			t.Fatal(err)
		}
	}
	b := NewBatch()
	b.Delete(Identifier("3"))
	b.Delete(Identifier("3001"))
	if err := w.Batch(b); err != nil {
		t.Fatal(err)
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	type hit struct {
		number uint64
		score  float64
		locs   []search.FieldTermLocation
	}
	read := func(req SearchRequest, phrase string) (hits []hit, count uint64) {
		it, err := r.Search(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		for {
			m, err := it.Next()
			if err != nil {
				t.Fatal(err)
			}
			if m == nil {
				return hits, it.Aggregations().Count()
			}
			var text string
			if err := m.VisitStoredFields(func(field string, value []byte) bool {
				if field == "_id" {
					n, err := strconv.Atoi(string(value))
					if err != nil {
						t.Fatal(err)
					}
					text = body(n)
				}
				return true
			}); err != nil {
				t.Fatal(err)
			}
			m.Complete(nil)
			h := hit{number: m.Number, score: m.Score}
			for t0, term := range strings.Fields(phrase) {
				for _, loc := range m.Locations["body"][term] {
					if loc.Start < 0 || loc.End > len(text) || text[loc.Start:loc.End] != term ||
						strings.Count(text[:loc.Start], " ")+1 != loc.Pos {
						t.Fatalf("%q doc %d term %q: location %+v does not fit %q", phrase, m.Number, term, *loc,
							text[max(0, len(text)-60):])
					}
					h.locs = append(h.locs, search.FieldTermLocation{Field: "body", Term: term, Location: *loc})
				}
				if len(m.Locations["body"][term]) == 0 {
					t.Fatalf("%q doc %d: no location for term %d", phrase, m.Number, t0)
				}
			}
			hits = append(hits, h)
		}
	}
	for _, phrase := range []string{"quick brown", "quick brown fox", "brown fox"} {
		q := func() *MatchPhraseQuery { return NewMatchPhraseQuery(phrase).SetField("body") }
		all, _ := read(NewAllMatches(q()).IncludeLocations(), phrase)
		allExplain, _ := read(NewAllMatches(q()).IncludeLocations().ExplainScores(), phrase)
		if !reflect.DeepEqual(all, allExplain) || len(all) != id-2 {
			t.Fatalf("%q: %d hits differ from explain, want %d", phrase, len(all), id-2)
		}
		topN := func() *TopNSearch {
			req := NewTopNSearch(10, q()).IncludeLocations()
			req.AddAggregation("count", aggregations.CountMatches())
			return req
		}
		top, topCount := read(topN(), phrase)
		topExplain, _ := read(topN().ExplainScores(), phrase)
		if !reflect.DeepEqual(top, topExplain) || len(top) != 10 || topCount != uint64(len(all)) {
			t.Fatalf("%q: top %d hits (count %d) differ from explain", phrase, len(top), topCount)
		}
	}
}
