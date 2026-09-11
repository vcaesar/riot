//  Copyright (c) 2020 The Bluge Authors.
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

package test

import (
	"testing"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/highlight"

	bluge "github.com/vcaesar/riot"
)

type match struct {
	Number           uint64
	Score            float64
	SortValue        [][]byte
	Fields           map[string][][]byte
	ExpectHighlights []*ExpectHighlight
	Locations        search.FieldTermLocationMap
}

func newIDMatches(ids ...string) []*match {
	result := []*match{}

	for _, id := range ids {
		result = append(result, &match{
			Fields: map[string][][]byte{
				"_id": {[]byte(id)},
			}})
	}
	return result
}

type ExpectHighlight struct {
	Highlighter highlight.Highlighter
	Field       string
	Result      string
}

type RequestVerify struct {
	Comment            string
	Request            bluge.SearchRequest
	Aggregations       search.Aggregations
	ExpectTotal        int
	ExpectMatches      []*match
	VerifyAggregations func(t *testing.T, bucket *search.Bucket)
}

type IntegrationTest struct {
	Name     string
	DataLoad func(writer *bluge.Writer) error
	Tests    func() []*RequestVerify
}
