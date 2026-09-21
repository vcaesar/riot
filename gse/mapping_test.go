//  Copyright (c) 2026 The Riot Authors.
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
	"encoding/json"
	"reflect"
	"testing"
)

type jsonMappingStub struct {
	JSON string
}

func (m jsonMappingStub) MarshalJSON() ([]byte, error) { return []byte(m.JSON), nil }

func TestDocumentFields(t *testing.T) {
	type embedded struct {
		Title string
	}
	type mapping struct {
		embedded
		Text    string `json:"body"`
		Skip    string `json:"-"`
		Empty   string `json:"empty,omitempty"`
		private string
		Count   uint64
		Active  bool
		Nested  *embedded `json:"nested"`
		Tags    []string
		Missing *string
	}
	value := mapping{
		embedded: embedded{Title: "heading"}, Text: "content", Skip: "secret", private: "hidden",
		Count: 18446744073709551615, Active: true, Nested: &embedded{Title: "child"}, Tags: []string{"one", "two"},
	}
	want := map[string][]string{
		"Title": {"heading"}, "body": {"content"}, "Count": {"18446744073709551615"},
		"Active": {"true"}, "nested.Title": {"child"}, "Tags": {"one", "two"},
	}
	for _, input := range []any{value, &value} {
		got, err := documentFields("text", input)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("fields = %v, err = %v, want %v", got, err, want)
		}
	}
	got, err := documentFields("custom", "original text")
	if err != nil || !reflect.DeepEqual(got, map[string][]string{"custom": {"original text"}}) {
		t.Fatalf("string fields = %v, err = %v", got, err)
	}
	got, err = documentFields("text", jsonMappingStub{JSON: `{"items":[{"name":"first"},{"name":"second"}],"empty":"","null":null}`})
	if err != nil || !reflect.DeepEqual(got, map[string][]string{"items.name": {"first", "second"}, "empty": {""}}) {
		t.Fatalf("custom mapping fields = %v, err = %v", got, err)
	}
}

func TestDocumentFieldsErrors(t *testing.T) {
	type node struct{ Next *node }
	cycle := &node{}
	cycle.Next = cycle
	tests := map[string]any{
		"nil":               nil,
		"nil pointer":       (*struct{})(nil),
		"number":            1,
		"slice":             []string{"text"},
		"map":               map[string]string{"text": "body"},
		"unsupported field": struct{ Function func() }{},
		"cycle":             cycle,
		"scalar JSON":       jsonMappingStub{JSON: `"text"`},
		"null JSON":         jsonMappingStub{JSON: `null`},
		"invalid JSON":      jsonMappingStub{JSON: `{`},
		"reserved name": struct {
			ID string `json:"_id"`
		}{ID: "wrong"},
		"dotted name": struct {
			Text string `json:"a.b"`
		}{},
		"empty name":           jsonMappingStub{JSON: `{"":"text"}`},
		"nested reserved name": jsonMappingStub{JSON: `{"items":[{"_all":"text"}]}`},
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := documentFields("text", input); err == nil {
				t.Fatal("expected mapping error")
			}
		})
	}
}

var _ json.Marshaler = jsonMappingStub{}
