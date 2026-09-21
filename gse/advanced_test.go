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

package gse

import (
	"reflect"
	"testing"

	"github.com/vcaesar/riot/gse/query"
)

func TestAdvancedSearch(t *testing.T) {
	idx := openIndex(t, Option{Lang: "en", Field: "body"})
	type document struct {
		Body  string `json:"body"`
		Title string `json:"title"`
		Rank  string `json:"rank"`
	}
	for id, doc := range map[string]document{
		"1": {"red fox", "running", "charlie"},
		"2": {"red bear", "walking", "alpha"},
		"3": {"blue fox", "running", "bravo"},
	} {
		if err := idx.Index(id, doc); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name    string
		request SearchRequest
		ids     []string
		total   uint64
	}{
		{"match OR terms", Query().Match("red blue").Sort("_id", true), []string{"1", "2", "3"}, 3},
		{"match all terms", Query().MatchAll("red fox"), []string{"1"}, 1},
		{"match all documents", Query().MatchAll().Sort("_id", true), []string{"1", "2", "3"}, 3},
		{"empty query", Query().Sort("_id", true), []string{"1", "2", "3"}, 3},
		{"and fields", Query().Match("red").And().Match("run").Field("title"), []string{"1"}, 1},
		{"or fields", Query().Match("bear").Or().Match("run").Field("title").Sort("_id", true), []string{"1", "2", "3"}, 3},
		{"left associative", Query().Match("red").Or().Match("blue").And().Match("fox").Sort("_id", true), []string{"1", "3"}, 2},
		{"ascending", Query().Sort("rank", true), []string{"2", "3", "1"}, 3},
		{"descending", Query().Sort("rank", false), []string{"1", "3", "2"}, 3},
		{"paging", Query().Sort("rank", true).Form(1).Size(1), []string{"3"}, 3},
		{"count only", Query().Size(0), []string{}, 3},
		{"past end", Query().From(10), []string{}, 3},
		{"package query", query.Query().MatchAll("blue fox"), []string{"3"}, 1},
		{"query string", QueryString("bear"), []string{"2"}, 1},
		{"legacy alias", NewQueryString("bear"), []string{"2"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := idx.Search(tt.request)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(hitIDs(res), tt.ids) || res.Total != tt.total {
				t.Fatalf("got ids %v total %d, want %v total %d", hitIDs(res), res.Total, tt.ids, tt.total)
			}
		})
	}
	res, err := idx.Search(Query().Match("run").Field("title").Highlight())
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 {
		t.Fatalf("highlight total = %d", res.Total)
	}
	for _, hit := range res.Hits {
		if len(hit.Fragments["title"]) == 0 {
			t.Fatalf("missing highlight: %+v", hit)
		}
	}
	for _, req := range []SearchRequest{nil, (*Request)(nil), (*query.Builder)(nil), Query().Size(-1), Query().From(-1), Query().Sort("", true), Query().Field("title")} {
		if _, err := idx.Search(req); err == nil {
			t.Fatalf("expected error for %#v", req)
		}
	}
}

func TestMappedBatch(t *testing.T) {
	idx := openIndex(t, Option{Lang: "en"})
	for _, id := range []string{"old", "remove"} {
		if err := idx.Index(id, "old content"); err != nil {
			t.Fatal(err)
		}
	}
	batch := idx.Batch()
	if err := batch.Index("old", "updated content"); err != nil {
		t.Fatal(err)
	}
	if err := batch.Index("new", struct {
		Text string `json:"text"`
	}{"running engines"}); err != nil {
		t.Fatal(err)
	}
	if err := batch.Index("new", nil); err == nil {
		t.Fatal("expected invalid mapping error")
	}
	batch.Delete("remove")
	batch.Delete("absent")
	res, err := idx.Search(QueryString("old"))
	if err != nil || res.Total != 2 {
		t.Fatalf("batch visible before commit: %+v, %v", res, err)
	}
	if err := batch.Commit(); err != nil {
		t.Fatal(err)
	}
	res, err = idx.Search(Query().Sort("_id", true))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(hitIDs(res), []string{"new", "old"}) {
		t.Fatalf("hits: %v", hitIDs(res))
	}
	res, err = idx.Search(Query().MatchAll("run engine").Highlight())
	if err != nil || res.Total != 1 || len(res.Hits[0].Fragments["text"]) == 0 {
		t.Fatalf("batch mapping/analyzer/highlight: %+v, %v", res, err)
	}
	if len(batch.docs) != 0 {
		t.Fatal("successful commit did not reset batch")
	}
	if err := batch.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := batch.Index("new", "discarded"); err != nil {
		t.Fatal(err)
	}
	batch.Reset()
	if err := batch.Commit(); err != nil {
		t.Fatal(err)
	}
	// Last operation wins, including update-after-delete and delete-after-update.
	if err := batch.Index("old", "discarded"); err != nil {
		t.Fatal(err)
	}
	batch.Delete("old")
	batch.Delete("new")
	if err := batch.Index("new", "first"); err != nil {
		t.Fatal(err)
	}
	if err := batch.Index("new", "last"); err != nil {
		t.Fatal(err)
	}
	if err := batch.Commit(); err != nil {
		t.Fatal(err)
	}
	res, err = idx.Search(Query())
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 || res.Hits[0].Fields["text"] != "last" {
		t.Fatalf("last operation: %+v", res)
	}
}
