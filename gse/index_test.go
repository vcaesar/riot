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
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	riot "github.com/vcaesar/riot"
)

func openIndex(t *testing.T, opt Option) *Index {
	t.Helper()
	idx, err := New(opt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cerr := idx.Close(); cerr != nil {
			t.Error(cerr)
		}
	})
	return idx
}

func hitIDs(res *Result) []string {
	ids := make([]string, 0, len(res.Hits))
	for _, h := range res.Hits {
		ids = append(ids, h.ID)
	}
	return ids
}

func TestIndexSearch(t *testing.T) {
	idx := openIndex(t, Option{Dicts: "embed, zh", Opt: "search-hmm"})
	docs := map[string]string{
		"zh1": "今天是星期几",
		"zh2": "Riot 是用 Go 语言编写的全文搜索引擎",
		"zh3": "今天天气很好",
	}
	for id, text := range docs {
		if err := idx.Index(id, text); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		query string
		want  []string
	}{
		{"搜索引擎", []string{"zh2"}},
		{"引擎", []string{"zh2"}},
		{"go", []string{"zh2"}},
		{"今天", []string{"zh1", "zh3"}},
		{"人民", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			res, err := idx.Search(NewQueryString(tt.query))
			if err != nil {
				t.Fatal(err)
			}
			got := hitIDs(res)
			if len(got) == 2 && got[0] > got[1] {
				got[0], got[1] = got[1], got[0]
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("hits = %v, want %v", got, tt.want)
			}
			if res.Total != uint64(len(tt.want)) {
				t.Fatalf("total = %d, want %d", res.Total, len(tt.want))
			}
			for _, h := range res.Hits {
				if h.Fields["text"] != docs[h.ID] || h.Score <= 0 || h.Fragments != nil {
					t.Fatalf("unexpected hit %+v", h)
				}
			}
		})
	}
}

func TestIndexSearchModeWholeWord(t *testing.T) {
	idx := openIndex(t, Option{Dicts: "embed, zh", Opt: "search"})
	if err := idx.Index("1", "全文搜索引擎很好"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"搜索引擎", "搜索", "好"} {
		res, err := idx.Search(NewQueryString(q))
		if err != nil {
			t.Fatal(err)
		}
		if got := hitIDs(res); !reflect.DeepEqual(got, []string{"1"}) {
			t.Fatalf("query %q hits = %v, want [1]", q, got)
		}
	}
}

func TestIndexSearchHighlight(t *testing.T) {
	idx := openIndex(t, Option{Dicts: "embed, jp", Opt: "search-hmm"})
	text := "見解では、謙虚なヴォードヴィリアンのベテランは、運命の犠牲者と悪役の両方の変遷として代償を払っています"
	if err := idx.Index("1", text); err != nil {
		t.Fatal(err)
	}
	if err := idx.Index("2", "It's difficult to understand the sum of a person's life."); err != nil {
		t.Fatal(err)
	}

	res, err := idx.Search(NewQueryString("運命の犠牲者", true))
	if err != nil {
		t.Fatal(err)
	}
	if got := hitIDs(res); !reflect.DeepEqual(got, []string{"1"}) {
		t.Fatalf("hits = %v", got)
	}
	frags := res.Hits[0].Fragments["text"]
	if len(frags) != 1 || !strings.Contains(frags[0], "<mark>運命</mark>") {
		t.Fatalf("fragments = %q", frags)
	}
	// Windows' clock can report a 0 duration for a sub-microsecond search.
	if res.MaxScore != res.Hits[0].Score || res.Took < 0 {
		t.Fatalf("max score %v vs %v, took %v", res.MaxScore, res.Hits[0].Score, res.Took)
	}
}

func TestIndexUpdateDeletePaging(t *testing.T) {
	idx := openIndex(t, Option{Field: "body", Opt: "search-hmm"})
	for _, id := range []string{"a", "b", "c"} {
		if err := idx.Index(id, "今天天气很好 "+id); err != nil {
			t.Fatal(err)
		}
	}
	if err := idx.Index("a", "完全不同的内容"); err != nil {
		t.Fatal(err)
	}
	if err := idx.Delete("b"); err != nil {
		t.Fatal(err)
	}

	res, err := idx.Search(NewQueryString("天气"))
	if err != nil {
		t.Fatal(err)
	}
	if got := hitIDs(res); !reflect.DeepEqual(got, []string{"c"}) {
		t.Fatalf("hits = %v, want [c]", got)
	}
	if res.Hits[0].Fields["body"] != "今天天气很好 c" {
		t.Fatalf("fields = %v", res.Hits[0].Fields)
	}

	req := NewQueryString("内容")
	req.Field, req.Size, req.From = "body", 1, 1
	if res, err = idx.Search(req); err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 || res.Total != 1 {
		t.Fatalf("paged hits = %v, total %d", hitIDs(res), res.Total)
	}
}

func TestIndexStructMapping(t *testing.T) {
	type Mapping struct {
		Text   string `json:"text"`
		Title  string `json:"title"`
		Secret string `json:"-"`
	}
	idx := openIndex(t, Option{Lang: "en"})
	mapping := Mapping{Text: "search engines", Title: "Running outdoors", Secret: "hidden"}
	for id, data := range map[string]any{"value": mapping, "pointer": &mapping} {
		if err := idx.Index(id, data); err != nil {
			t.Fatal(err)
		}
	}
	for _, field := range []string{"text", "title"} {
		query := "engine"
		if field == "title" {
			query = "running"
		}
		req := NewQueryString(query, true)
		req.Field = field
		res, err := idx.Search(req)
		if err != nil {
			t.Fatal(err)
		}
		if res.Total != 2 {
			t.Fatalf("field %q: total = %d, want 2", field, res.Total)
		}
		for _, hit := range res.Hits {
			want := map[string]string{"text": mapping.Text, "title": mapping.Title}
			if !reflect.DeepEqual(hit.Fields, want) || len(hit.Fragments[field]) != 1 {
				t.Fatalf("unexpected mapped hit: %+v", hit)
			}
		}
	}
	if err := idx.Index("value", nil); err == nil {
		t.Fatal("expected invalid mapping error")
	}
	res, err := idx.Search(NewQueryString("engines"))
	if err != nil || res.Total != 2 {
		t.Fatalf("invalid mapping changed documents: result=%+v, err=%v", res, err)
	}
	if err := idx.Index("value", "replacement"); err != nil {
		t.Fatal(err)
	}
	if err := idx.Index("pointer", &Mapping{Text: "replacement"}); err != nil {
		t.Fatal(err)
	}
	req := NewQueryString("running")
	req.Field = "title"
	res, err = idx.Search(req)
	if err != nil || res.Total != 0 {
		t.Fatalf("update retained mapped fields: result=%+v, err=%v", res, err)
	}
}

func TestIndexOnDiskAndCustomDoc(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gse-index-test")

	idx := openIndex(t, Option{Index: dir})
	doc := riot.NewDocument("x").
		AddField(idx.Field("title", "多伦多悬崖公园")).
		AddField(idx.Field("text", "Seattle space needle"))
	if err := idx.Writer().Update(doc.ID(), doc); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("index dir not created: %v", err)
	}

	req := NewQueryString("公园", true)
	req.Field = "title"
	res, err := idx.Search(req)
	if err != nil {
		t.Fatal(err)
	}
	if got := hitIDs(res); !reflect.DeepEqual(got, []string{"x"}) {
		t.Fatalf("hits = %v", got)
	}
	hit := res.Hits[0]
	if hit.Fields["text"] != "Seattle space needle" || len(hit.Fragments["title"]) != 1 || hit.Fragments["text"] != nil {
		t.Fatalf("unexpected hit %+v", hit)
	}
}
