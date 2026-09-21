// Copyright (c) 2026 The Riot Authors.
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

package query

import (
	"reflect"
	"testing"

	"github.com/vcaesar/riot"
	"github.com/vcaesar/riot/analysis"
	"github.com/vcaesar/riot/search"
)

func build(t *testing.T, b *Builder) *riot.TopNSearch {
	t.Helper()
	r, err := b.Build("body", nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestDefaults(t *testing.T) {
	for _, b := range []*Builder{Query(), {}, Query().MatchAll()} {
		r := build(t, b)
		if _, ok := r.Query().(*riot.MatchAllQuery); !ok {
			t.Fatalf("expected match all, got %T", r.Query())
		}
		if r.Size() != 10 || r.From() != 0 || r.Options().IncludeLocations || b.HighlightEnabled() {
			t.Fatalf("incorrect defaults: %+v", r)
		}
		if r.SortOrder().Compare(&search.DocumentMatch{Score: 2}, &search.DocumentMatch{Score: 1}) >= 0 {
			t.Fatal("default score sort must descend")
		}
	}
}

func TestMatchClauses(t *testing.T) {
	a := &analysis.Analyzer{}
	b := Query().Match("one two").Field("title").Or().MatchAll("three four").Match("five")
	r, err := b.Build("body", a)
	if err != nil {
		t.Fatal(err)
	}
	outer := r.Query().(*riot.BooleanQuery)
	if len(outer.Musts()) != 2 {
		t.Fatal("OR must reset to AND for next clause")
	}
	inner := outer.Musts()[0].(*riot.BooleanQuery)
	if len(inner.Shoulds()) != 2 || inner.MinShould() != 1 {
		t.Fatal("expected left-associated OR requiring one match")
	}
	clauses := []riot.Query{inner.Shoulds()[0], inner.Shoulds()[1], outer.Musts()[1]}
	for i, want := range []struct {
		text, field string
		op          riot.MatchQueryOperator
	}{{"one two", "title", riot.MatchQueryOperatorOr}, {"three four", "body", riot.MatchQueryOperatorAnd}, {"five", "body", riot.MatchQueryOperatorOr}} {
		q := clauses[i].(*riot.MatchQuery)
		if q.Match() != want.text || q.Field() != want.field || q.Operator() != want.op || q.Analyzer() != a {
			t.Fatalf("clause %d: %+v", i, q)
		}
	}
	q := build(t, Query().Match("a").Or().And().Match("b").Or()).Query().(*riot.BooleanQuery)
	if len(q.Musts()) != 2 {
		t.Fatal("last connector must win; trailing connector must be ignored")
	}
	q = build(t, Query().Match("a").Match("b").Or().MatchAll()).Query().(*riot.BooleanQuery)
	if len(q.Shoulds()[0].(*riot.BooleanQuery).Musts()) != 2 {
		t.Fatal("expected (a AND b) OR all")
	}
	if _, ok := q.Shoulds()[1].(*riot.MatchAllQuery); !ok {
		t.Fatal("zero-argument MatchAll must remain a clause")
	}
}

func TestPaginationAndHighlight(t *testing.T) {
	for _, b := range []*Builder{Query().From(3), Query().Form(3)} {
		r := build(t, b.Size(0).Highlight())
		if r.From() != 3 || r.Size() != 0 || !r.Options().IncludeLocations {
			t.Fatalf("incorrect options: %+v", r)
		}
	}
	for _, tc := range []struct {
		b    *Builder
		want bool
	}{
		{Query(), false}, {Query().Highlight(), true}, {Query().Highlight(true), true},
		{Query().Highlight().Highlight(false), false}, {Query().Highlight(false, true), false},
	} {
		if tc.b.HighlightEnabled() != tc.want || build(t, tc.b).Options().IncludeLocations != tc.want {
			t.Fatalf("incorrect highlight state: %+v", tc.b)
		}
	}
	var nilBuilder *Builder
	if nilBuilder.HighlightEnabled() {
		t.Fatal("nil builder highlight enabled")
	}
}

func TestSort(t *testing.T) {
	r := build(t, Query().Sort("title", true).Sort("date", false))
	if !reflect.DeepEqual(r.SortOrder().Fields(), []string{"title", "date"}) {
		t.Fatal("sort fields lost or reordered")
	}
	for _, tc := range []struct {
		left, right [][]byte
		want        int
	}{
		{[][]byte{[]byte("a"), []byte("a")}, [][]byte{[]byte("b"), []byte("a")}, -1},
		{[][]byte{[]byte("a"), []byte("a")}, [][]byte{[]byte("a"), []byte("b")}, 1},
	} {
		if got := r.SortOrder().Compare(&search.DocumentMatch{SortValue: tc.left}, &search.DocumentMatch{SortValue: tc.right}); got != tc.want {
			t.Fatalf("sort comparison = %d, want %d", got, tc.want)
		}
	}
	for _, ascending := range []bool{false, true} {
		order := build(t, Query().Sort("_score", ascending)).SortOrder()
		got := order.Compare(&search.DocumentMatch{Score: 1}, &search.DocumentMatch{Score: 2})
		if (got < 0) != ascending {
			t.Fatalf("score ascending=%v: comparison=%d", ascending, got)
		}
	}
}

func TestBuildErrors(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, tc := range []struct {
		name string
		b    *Builder
	}{
		{"nil", nil}, {"negative from", Query().From(-1)}, {"negative size", Query().Size(-1)},
		{"overflow default", Query().From(maxInt)}, {"overflow explicit", Query().From(1).Size(maxInt)},
		{"empty sort", Query().Sort("", true)}, {"later empty sort", Query().Sort("title", true).Sort("", false)},
		{"field without clause", Query().Field("title")},
		{"field error retained", Query().Field("title").Match("a")},
		{"multiple texts", Query().MatchAll("a", "b")},
		{"arity error retained", Query().MatchAll("a", "b").Match("c")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if r, err := tc.b.Build("body", nil); err == nil || r != nil {
				t.Fatalf("Build = %v, %v; want nil request and error", r, err)
			}
		})
	}
	for _, b := range []*Builder{Query().From(maxInt).Size(0), Query().Size(maxInt), Query().From(maxInt - 10)} {
		build(t, b)
	}
}

func TestImmutableReuse(t *testing.T) {
	base := Query().Match("a").Match("b").Match("c").Sort("title", true)
	left := base.Field("left").Match("left").Sort("left", true).Highlight().From(2).Size(3)
	right := base.Field("right").Match("right").Sort("right", false)
	for _, tc := range []struct {
		b     *Builder
		field string
		count int
	}{
		{base, "body", 1}, {left, "left", 2}, {right, "right", 2},
	} {
		r := build(t, tc.b)
		q := r.Query().(*riot.BooleanQuery)
		if tc.b != base {
			q = q.Musts()[0].(*riot.BooleanQuery)
		}
		if q.Musts()[1].(*riot.MatchQuery).Field() != tc.field || len(r.SortOrder()) != tc.count {
			t.Fatal("builder branch mutated another branch")
		}
	}
	if base.HighlightEnabled() || build(t, base).From() != 0 || build(t, base).Size() != 10 {
		t.Fatal("scalar options mutated base")
	}
	r := build(t, base)
	r.Query().(*riot.BooleanQuery).Musts()[1].(*riot.MatchQuery).SetField("mutated")
	r.SortOrder()[0].Desc()
	again := build(t, base)
	if again.Query().(*riot.BooleanQuery).Musts()[1].(*riot.MatchQuery).Field() != "body" {
		t.Fatal("built query mutation leaked")
	}
	if again.SortOrder().Compare(&search.DocumentMatch{SortValue: [][]byte{[]byte("a")}}, &search.DocumentMatch{SortValue: [][]byte{[]byte("b")}}) >= 0 {
		t.Fatal("built sort mutation leaked")
	}
}
