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

package search

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/vcaesar/riot/numeric"
)

func TestParseSortOrderStrings(t *testing.T) {
	order := ParseSortOrderStrings([]string{"a", "-b", "+c", "_score", "-_score"})
	type flags struct {
		desc, score bool
	}
	want := []flags{{}, {desc: true}, {}, {desc: true, score: true}, {desc: true, score: true}}
	if len(order) != len(want) {
		t.Fatalf("len = %d, want %d", len(order), len(want))
	}
	for i, s := range order {
		if got := (flags{desc: s.desc, score: s.score}); got != want[i] {
			t.Errorf("sort %d = %+v, want %+v", i, got, want[i])
		}
	}
	if got := order.Fields(); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("Fields() = %v, want [a b c]", got)
	}
	if got := order[1].Fields(); !reflect.DeepEqual(got, []string{"b"}) {
		t.Errorf("Sort.Fields() = %v, want [b]", got)
	}
}

func TestSortOrderCopyAndReverse(t *testing.T) {
	order := SortOrder{SortBy(Field("a")), SortBy(Field("b")).Desc().MissingFirst()}
	cp := order.Copy()
	cp[0] = SortBy(Field("z"))
	if order[0].Fields()[0] != "a" {
		t.Errorf("Copy() shares the backing array")
	}
	order.Reverse()
	if !order[0].desc || !order[0].missingFirst || order[1].desc || order[1].missingFirst {
		t.Errorf("Reverse() = %+v %+v", *order[0], *order[1])
	}
}

func TestSortOrderScoreDescOnlyAndNumericFirst(t *testing.T) {
	score := SortBy(DocumentScore()).Desc()
	for _, test := range []struct {
		name      string
		order     SortOrder
		scoreDesc bool
		field     string
		desc, ok  bool
	}{
		{name: "empty"},
		{name: "score desc", order: SortOrder{score}, scoreDesc: true},
		{name: "score asc", order: SortOrder{SortBy(DocumentScore())}},
		{name: "score then field", order: SortOrder{score, SortBy(Field("a"))}},
		{name: "field", order: SortOrder{SortBy(Field("a"))}, field: "a", ok: true},
		{name: "field desc", order: SortOrder{SortBy(Field("a")).Desc()}, field: "a", desc: true, ok: true},
		{name: "constant", order: SortOrder{SortBy(ConstantTextValueSource("x"))}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.order.ScoreDescOnly(); got != test.scoreDesc {
				t.Errorf("ScoreDescOnly() = %v, want %v", got, test.scoreDesc)
			}
			field, desc, ok := test.order.NumericFirst()
			if field != test.field || desc != test.desc || ok != test.ok {
				t.Errorf("NumericFirst() = %q, %v, %v, want %q, %v, %v", field, desc, ok, test.field, test.desc, test.ok)
			}
		})
	}
}

func TestSortOrderDecodeScoreSkipsOtherKeys(t *testing.T) {
	key := numeric.MustNewPrefixCodedInt64(numeric.Float64ToInt64(2.5), 0)
	order := SortOrder{SortBy(Field("a")), SortBy(DocumentScore())}
	m := &DocumentMatch{SortValue: [][]byte{[]byte("x"), key}}
	order.DecodeScore(m)
	if m.Score != 2.5 {
		t.Errorf("Score = %v, want 2.5", m.Score)
	}
	short := &DocumentMatch{Score: 1}
	SortOrder{SortBy(DocumentScore())}.DecodeScore(short)
	if short.Score != 1 {
		t.Errorf("Score without key = %v, want 1", short.Score)
	}
}

func TestSortOrderCompareHitNumber(t *testing.T) {
	order := SortOrder{SortBy(Field("a"))}
	for _, test := range []struct {
		i, j int
		want int
	}{
		{i: 1, j: 1, want: 0},
		{i: 2, j: 1, want: 1},
		{i: 1, j: 2, want: -1},
	} {
		a := &DocumentMatch{HitNumber: test.i, SortValue: [][]byte{[]byte("k")}}
		b := &DocumentMatch{HitNumber: test.j, SortValue: [][]byte{[]byte("k")}}
		if got := order.Compare(a, b); got != test.want {
			t.Errorf("Compare(hit %d, hit %d) = %d, want %d", test.i, test.j, got, test.want)
		}
	}
}

func TestSortAppendValueWithoutAppender(t *testing.T) {
	s := &Sort{source: ConstantTextValueSource("abc")}
	if got := s.appendValue(&DocumentMatch{}, []byte("zz")); string(got) != "zzabc" {
		t.Errorf("appendValue() = %q, want zzabc", got)
	}
}

func TestSortFirstLast(t *testing.T) {
	yes, no := true, false
	for _, test := range []struct {
		name        string
		desc, first *bool
		want        []byte
	}{
		{name: "unset", want: highTerm},
		{name: "asc last", desc: &no, first: &no, want: highTerm},
		{name: "asc first", desc: &no, first: &yes, want: lowTerm},
		{name: "desc last", desc: &yes, first: &no, want: lowTerm},
		{name: "desc first", desc: &yes, first: &yes, want: highTerm},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := &sortFirstLast{desc: test.desc, first: test.first}
			if s.Fields() != nil {
				t.Errorf("Fields() = %v, want nil", s.Fields())
			}
			if got := s.Value(nil); !bytes.Equal(got, test.want) {
				t.Errorf("Value() = %v, want %v", got, test.want)
			}
		})
	}
}
