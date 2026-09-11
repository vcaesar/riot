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
	"time"

	"github.com/vcaesar/riot/search/highlight"

	bluge "github.com/vcaesar/riot"
	"github.com/vcaesar/riot/analysis/lang/en"
)

var basicBirthday time.Time

func init() {
	var err error
	basicBirthday, err = time.Parse(time.RFC3339, "2010-01-01T00:00:00Z")
	if err != nil {
		panic(err)
	}
}

func basicLoad(writer *bluge.Writer) error {
	enAnalyzer := en.NewAnalyzer()

	err := writer.Insert(bluge.NewDocument("a").
		AddField(bluge.NewTextField("name", "marty").
			SearchTermPositions().
			StoreValue().
			WithAnalyzer(enAnalyzer)).
		AddField(bluge.NewNumericField("age", 19)).
		AddField(bluge.NewTextField("title", "mista").
			StoreValue()).
		AddField(bluge.NewKeywordField("tags", "gopher").
			StoreValue().
			SearchTermPositions()).
		AddField(bluge.NewKeywordField("tags", "belieber").
			StoreValue().
			SearchTermPositions()).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	birthday, err := time.Parse(time.RFC3339, "2001-09-09T01:46:40Z")
	if err != nil {
		return err
	}
	err = writer.Insert(bluge.NewDocument("b").
		AddField(bluge.NewTextField("name", "steve has <a> long & complicated name").
			SearchTermPositions().
			StoreValue().
			WithAnalyzer(enAnalyzer)).
		AddField(bluge.NewNumericField("age", 27)).
		AddField(bluge.NewTextField("title", "missess").
			StoreValue()).
		AddField(bluge.NewDateTimeField("birthday", birthday)).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	birthday, err = time.Parse(time.RFC3339, "2014-05-13T16:53:20Z")
	if err != nil {
		return err
	}
	err = writer.Insert(bluge.NewDocument("c").
		AddField(bluge.NewTextField("name", "bob walks home").
			SearchTermPositions().
			StoreValue().
			WithAnalyzer(enAnalyzer)).
		AddField(bluge.NewNumericField("age", 64)).
		AddField(bluge.NewTextField("title", "masta").
			StoreValue()).
		AddField(bluge.NewDateTimeField("birthday", birthday)).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	birthday, err = time.Parse(time.RFC3339, "2014-05-13T16:53:20Z")
	if err != nil {
		return err
	}
	err = writer.Insert(bluge.NewDocument("d").
		AddField(bluge.NewTextField("name", "bobbleheaded wings top the phone").
			SearchTermPositions().
			StoreValue().
			WithAnalyzer(enAnalyzer)).
		AddField(bluge.NewNumericField("age", 72)).
		AddField(bluge.NewTextField("title", "mizz").
			StoreValue()).
		AddField(bluge.NewDateTimeField("birthday", birthday)).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	return nil
}

func basicTests() []*RequestVerify {
	enAnalyzer := en.NewAnalyzer()
	return []*RequestVerify{
		{
			Comment: "test term search, exact match",
			Request: bluge.NewTopNSearch(10,
				bluge.NewTermQuery("marti").
					SetField("name")),
			Aggregations: standardAggs,
			ExpectTotal:  1,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("a")},
					},
				},
			},
		},
		{
			Comment: "test term search, no match",
			Request: bluge.NewTopNSearch(10,
				bluge.NewTermQuery("noone").
					SetField("name")),
			Aggregations: standardAggs,
			ExpectTotal:  0,
		},
		{
			Comment: "test match phrase search",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("complicated name").
					SetAnalyzer(enAnalyzer)),
			Aggregations: standardAggs,
			ExpectTotal:  1,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("b")},
					},
				},
			},
		},
		{
			Comment: "test term search, no match",
			Request: bluge.NewTopNSearch(10,
				bluge.NewTermQuery("walking").
					SetField("name")),
			Aggregations: standardAggs,
			ExpectTotal:  0,
		},
		{
			Comment: "test match search, matching due to analysis",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchQuery("walking").
					SetField("name").
					SetAnalyzer(enAnalyzer)),
			Aggregations: standardAggs,
			ExpectTotal:  1,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("c")},
					},
				},
			},
		},
		{
			Comment: "test term prefix search",
			Request: bluge.NewTopNSearch(10,
				bluge.NewPrefixQuery("bobble")),
			Aggregations: standardAggs,
			ExpectTotal:  1,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("d")},
					},
				},
			},
		},
		{
			Comment: "test numeric range, no lower bound",
			Request: bluge.NewTopNSearch(10,
				bluge.NewNumericRangeInclusiveQuery(bluge.MinNumeric, 30, false, false).
					SetField("age")),
			Aggregations: standardAggs,
			ExpectTotal:  2,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("a")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("b")},
					},
				},
			},
		},
		{
			Comment: "test numeric range, upper and lower bounds",
			Request: bluge.NewTopNSearch(10,
				bluge.NewNumericRangeInclusiveQuery(20, 30, false, false).
					SetField("age")),
			Aggregations: standardAggs,
			ExpectTotal:  1,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("b")},
					},
				},
			},
		},
		{
			Comment: "test conjunction of numeric range, upper and lower bounds",
			Request: bluge.NewTopNSearch(10,
				bluge.NewBooleanQuery().
					AddMust(
						bluge.NewNumericRangeInclusiveQuery(20, bluge.MaxNumeric, false, false).
							SetField("age"),
						bluge.NewNumericRangeInclusiveQuery(bluge.MinNumeric, 30, false, false).
							SetField("age"))).SortBy([]string{"-_score", "_id"}),
			Aggregations: standardAggs,
			ExpectTotal:  1,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("b")},
					},
				},
			},
		},
		{
			Comment: "test date range, no upper bound",
			Request: bluge.NewTopNSearch(10,
				bluge.NewDateRangeInclusiveQuery(basicBirthday, time.Time{}, false, false).
					SetField("birthday"),
			).SortBy([]string{"-_score", "_id"}),
			Aggregations: standardAggs,
			ExpectTotal:  2,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("c")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("d")},
					},
				},
			},
		},
		{
			Comment: "test date range, no lower bound",
			Request: bluge.NewTopNSearch(10,
				bluge.NewDateRangeInclusiveQuery(time.Time{}, basicBirthday, false, false).
					SetField("birthday"),
			).SortBy([]string{"-_score", "_id"}),
			Aggregations: standardAggs,
			ExpectTotal:  1,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("b")},
					},
				},
			},
		},
		{
			Comment: "test term search, matching inside an array",
			Request: bluge.NewTopNSearch(10,
				bluge.NewTermQuery("gopher").
					SetField("tags")),
			Aggregations: standardAggs,
			ExpectTotal:  1,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("a")},
					},
				},
			},
		},
		{
			Comment: "test term search, matching another element inside array",
			Request: bluge.NewTopNSearch(10,
				bluge.NewTermQuery("belieber").
					SetField("tags")),
			Aggregations: standardAggs,
			ExpectTotal:  1,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("a")},
					},
				},
			},
		},
		{
			Comment: "test term search, not present in array",
			Request: bluge.NewTopNSearch(10,
				bluge.NewTermQuery("notintagsarray").
					SetField("tags")),
			Aggregations:  standardAggs,
			ExpectTotal:   0,
			ExpectMatches: []*match{},
		},
		{
			Comment: "with size 0, total should be 1, but hits empty",
			Request: bluge.NewTopNSearch(0,
				bluge.NewTermQuery("marti").
					SetField("name")),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: []*match{},
		},
		{
			Comment: "a search for doc a that includes tags field, verifies both values come back",
			Request: bluge.NewTopNSearch(10,
				bluge.NewTermQuery("marti").
					SetField("name")),
			Aggregations: standardAggs,
			ExpectTotal:  1,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id":  {[]byte("a")},
						"tags": {[]byte("belieber"), []byte("gopher")},
					},
				},
			},
		},
		{
			Comment: "test fuzzy search, fuzziness 1 with match",
			Request: bluge.NewTopNSearch(10,
				bluge.NewFuzzyQuery("msrti").
					SetField("name")),
			Aggregations: standardAggs,
			ExpectTotal:  1,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("a")},
					},
				},
			},
		},
		{
			Comment: "highlight results",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchQuery("long").
					SetField("name")).
				IncludeLocations(),
			Aggregations: standardAggs,
			ExpectTotal:  1,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("b")},
					},
					ExpectHighlights: []*ExpectHighlight{
						{
							Highlighter: highlight.NewHTMLHighlighter(),
							Field:       "name",
							Result:      "steve has &lt;a&gt; <mark>long</mark> &amp; complicated name",
						},
					},
				},
			},
		},
		{
			Comment: "highlight results including non-matching field (which should be produced in its entirety, though unhighlighted)",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchQuery("long").
					SetField("name")).
				IncludeLocations(),
			Aggregations: standardAggs,
			ExpectTotal:  1,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("b")},
					},
					ExpectHighlights: []*ExpectHighlight{
						{
							Highlighter: highlight.NewHTMLHighlighter(),
							Field:       "name",
							Result:      "steve has &lt;a&gt; <mark>long</mark> &amp; complicated name",
						},
						{
							Highlighter: highlight.NewHTMLHighlighter(),
							Field:       "title",
							Result:      "missess",
						},
					},
				},
			},
		},
		{
			Comment: "search and highlight an array field",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchQuery("gopher").
					SetField("tags")).
				IncludeLocations(),
			Aggregations: standardAggs,
			ExpectTotal:  1,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("a")},
					},
					ExpectHighlights: []*ExpectHighlight{
						{
							Highlighter: highlight.NewHTMLHighlighter(),
							Field:       "tags",
							Result:      "<mark>gopher</mark>",
						},
					},
				},
			},
		},
		{
			Comment: "reproduce bug in prefix search",
			Request: bluge.NewTopNSearch(10,
				bluge.NewPrefixQuery("miss").
					SetField("title")),
			Aggregations: standardAggs,
			ExpectTotal:  1,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("b")},
					},
				},
			},
		},
		{
			Comment: "test match none",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchNoneQuery()),
			Aggregations:  standardAggs,
			ExpectTotal:   0,
			ExpectMatches: []*match{},
		},
		{
			Comment: "test match all",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchAllQuery()).
				SortBy([]string{"-_score", "_id"}),
			Aggregations: standardAggs,
			ExpectTotal:  4,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("a")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("b")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("c")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("d")},
					},
				},
			},
		},
		{
			Comment: "test search on _id",
			Request: bluge.NewTopNSearch(10,
				bluge.NewBooleanQuery().
					AddShould(
						bluge.NewTermQuery("b").SetField("_id"),
						bluge.NewTermQuery("c").SetField("_id"))).
				SortBy([]string{"-_score", "_id"}),
			Aggregations: standardAggs,
			ExpectTotal:  2,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("b")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("c")},
					},
				},
			},
		},
		{
			Comment: "test regexp matching term",
			Request: bluge.NewTopNSearch(10,
				bluge.NewRegexpQuery("mar.*").
					SetField("name")),
			Aggregations: standardAggs,
			ExpectTotal:  1,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("a")},
					},
				},
			},
		},
		{
			Comment: "test regexp that should not match when properly anchored",
			Request: bluge.NewTopNSearch(10,
				bluge.NewRegexpQuery("mar.").
					SetField("name")),
			Aggregations:  standardAggs,
			ExpectTotal:   0,
			ExpectMatches: []*match{},
		},
		{
			Comment: "test wildcard matching term",
			Request: bluge.NewTopNSearch(10,
				bluge.NewWildcardQuery("mar*").
					SetField("name")),
			Aggregations: standardAggs,
			ExpectTotal:  1,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("a")},
					},
				},
			},
		},
		{
			Comment: "test boost - term query",
			Request: bluge.NewTopNSearch(10,
				bluge.NewBooleanQuery().
					AddShould(
						bluge.NewTermQuery("marti").
							SetField("name"),
						bluge.NewTermQuery("steve").
							SetField("name").
							SetBoost(5))),
			Aggregations: standardAggs,
			ExpectTotal:  2,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("b")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("a")},
					},
				},
			},
		},
		{
			Comment: "test boost - fuzzy query",
			Request: bluge.NewTopNSearch(10,
				bluge.NewBooleanQuery().
					AddShould(
						bluge.NewTermQuery("marti").
							SetField("name"),
						bluge.NewFuzzyQuery("steve").
							SetField("name").
							SetBoost(5))),
			Aggregations: standardAggs,
			ExpectTotal:  2,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("b")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("a")},
					},
				},
			},
		},
		{
			Comment: "test boost - numeric range",
			Request: bluge.NewTopNSearch(10,
				bluge.NewBooleanQuery().
					AddShould(
						bluge.NewTermQuery("marti").
							SetField("name"),
						bluge.NewNumericRangeQuery(25, 29).
							SetField("age").
							SetBoost(5))),
			Aggregations: standardAggs,
			ExpectTotal:  2,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("b")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("a")},
					},
				},
			},
		},
		{
			Comment: "test boost - regexp range",
			Request: bluge.NewTopNSearch(10,
				bluge.NewBooleanQuery().
					AddShould(
						bluge.NewTermQuery("marti").
							SetField("name"),
						bluge.NewRegexpQuery("stev.*").
							SetField("name").
							SetBoost(5))),
			Aggregations: standardAggs,
			ExpectTotal:  2,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("b")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("a")},
					},
				},
			},
		},
		{
			Comment: "test term range",
			Request: bluge.NewTopNSearch(10,
				bluge.NewTermRangeQuery("mis", "miz").
					SetField("title")),
			Aggregations: standardAggs,
			ExpectTotal:  2,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("a")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("b")},
					},
				},
			},
		},
	}
}
