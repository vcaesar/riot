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
	"bytes"
	"sort"
	"time"

	bluge "github.com/vcaesar/riot"
	"github.com/vcaesar/riot/search"
)

func sortLoad(writer *bluge.Writer) error {
	martyBorn, err := time.Parse(time.RFC3339, "2014-11-25T00:00:00Z")
	if err != nil {
		return err
	}
	err = writer.Insert(bluge.NewDocument("a").
		AddField(bluge.NewTextField("name", "marty").
			SearchTermPositions().
			StoreValue().
			Sortable()).
		AddField(bluge.NewNumericField("age", 19).
			Sortable()).
		AddField(bluge.NewDateTimeField("born", martyBorn).
			Sortable()).
		AddField(bluge.NewTextField("title", "mista")).
		AddField(bluge.NewKeywordField("tags", "gopher").StoreValue().
			Sortable()).
		AddField(bluge.NewKeywordField("tags", "belieber").StoreValue().
			Sortable()).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	steveBorn, err := time.Parse(time.RFC3339, "2000-09-11T00:00:00Z")
	if err != nil {
		return err
	}
	err = writer.Insert(bluge.NewDocument("b").
		AddField(bluge.NewTextField("name", "steve").
			SearchTermPositions().
			StoreValue().
			Sortable()).
		AddField(bluge.NewNumericField("age", 21).
			Sortable()).
		AddField(bluge.NewDateTimeField("born", steveBorn).
			Sortable()).
		AddField(bluge.NewTextField("title", "zebra")).
		AddField(bluge.NewKeywordField("tags", "thought-leader").StoreValue().
			Sortable()).
		AddField(bluge.NewKeywordField("tags", "futurist").StoreValue().
			Sortable()).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	asterBorn, err := time.Parse(time.RFC3339, "1954-02-02T00:00:00Z")
	if err != nil {
		return err
	}
	err = writer.Insert(bluge.NewDocument("c").
		AddField(bluge.NewTextField("name", "aster").
			SearchTermPositions().
			StoreValue().
			Sortable()).
		AddField(bluge.NewNumericField("age", 21).
			Sortable()).
		AddField(bluge.NewDateTimeField("born", asterBorn).
			Sortable()).
		AddField(bluge.NewTextField("title", "blogger")).
		AddField(bluge.NewKeywordField("tags", "red").StoreValue().
			Sortable()).
		AddField(bluge.NewKeywordField("tags", "blue").StoreValue().
			Sortable()).
		AddField(bluge.NewKeywordField("tags", "green").StoreValue().
			Sortable()).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	namelessBorn, err := time.Parse(time.RFC3339, "1978-12-02T00:00:00Z")
	if err != nil {
		return err
	}
	err = writer.Insert(bluge.NewDocument("d").
		AddField(bluge.NewNumericField("age", 65).
			Sortable()).
		AddField(bluge.NewDateTimeField("born", namelessBorn).
			Sortable()).
		AddField(bluge.NewTextField("title", "agent d is desperately trying out to be successful rapster!")).
		AddField(bluge.NewKeywordField("tags", "cats").StoreValue().
			Sortable()).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	nancyBorn, err := time.Parse(time.RFC3339, "1954-10-22T00:00:00Z")
	if err != nil {
		return err
	}
	err = writer.Insert(bluge.NewDocument("e").
		AddField(bluge.NewTextField("name", "nancy").
			SearchTermPositions().
			StoreValue().
			Sortable()).
		AddField(bluge.NewDateTimeField("born", nancyBorn).
			Sortable()).
		AddField(bluge.NewTextField("title", "rapstar nancy rapster")).
		AddField(bluge.NewKeywordField("tags", "pain").StoreValue().
			Sortable()).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	err = writer.Insert(bluge.NewDocument("f").
		AddField(bluge.NewTextField("name", "frank").
			SearchTermPositions().
			StoreValue().
			Sortable()).
		AddField(bluge.NewNumericField("age", 1).
			Sortable()).
		AddField(bluge.NewTextField("title", "frank the taxman of cb, Rapster!")).
		AddField(bluge.NewKeywordField("tags", "vitamin").StoreValue()).
		AddField(bluge.NewKeywordField("tags", "purple").StoreValue().
			Sortable()).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	return nil
}

type sortUsingMinOfMultipleValues struct {
	source search.TextValuesSource
}

func (c *sortUsingMinOfMultipleValues) Fields() []string {
	return c.source.Fields()
}

func (c *sortUsingMinOfMultipleValues) Value(match *search.DocumentMatch) []byte {
	vals := c.source.Values(match)
	if len(vals) > 0 {
		sort.Slice(vals, func(i, j int) bool { return bytes.Compare(vals[i], vals[j]) < 0 })
		return vals[0]
	}
	return nil
}

func sortTests() []*RequestVerify {
	return []*RequestVerify{
		{
			Comment: "sort by name, ascending",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchAllQuery()).
				SortBy([]string{"name"}),
			Aggregations: standardAggs,
			ExpectTotal:  6,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("c")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("f")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("a")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("e")},
					},
				}, {
					Fields: map[string][][]byte{
						"_id": {[]byte("b")},
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
			Comment: "sort by name, descending",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchAllQuery()).
				SortBy([]string{"-name"}),
			Aggregations: standardAggs,
			ExpectTotal:  6,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("b")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("e")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("a")},
					},
				}, {
					Fields: map[string][][]byte{
						"_id": {[]byte("f")},
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
			Comment: "sort by name, descending, missing first",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchAllQuery()).
				SortByCustom(search.SortOrder{
					search.SortBy(search.Field("name")).Desc().MissingFirst(),
				}),
			Aggregations: standardAggs,
			ExpectTotal:  6,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("d")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("b")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("e")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("a")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("f")},
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
			Comment: "sort by age, ascending",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchAllQuery()).
				SortBy([]string{"age", "_id"}),
			Aggregations: standardAggs,
			ExpectTotal:  6,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("f")},
					},
				},
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
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("e")},
					},
				},
			},
		},
		{
			Comment: "sort by age, descending",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchAllQuery()).
				SortBy([]string{"-age", "_id"}),
			Aggregations: standardAggs,
			ExpectTotal:  6,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("d")},
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
						"_id": {[]byte("a")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("f")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("e")},
					},
				},
			},
		},
		{
			Comment: "sort by age, descending, missing first",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchAllQuery()).
				SortByCustom(search.SortOrder{
					search.SortBy(search.Field("age")).Desc().MissingFirst(),
					search.SortBy(search.Field("_id")),
				}),
			Aggregations: standardAggs,
			ExpectTotal:  6,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("e")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("d")},
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
						"_id": {[]byte("a")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("f")},
					},
				},
			},
		},
		{
			Comment: "sort by born, ascending",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchAllQuery()).
				SortBy([]string{"born"}),
			Aggregations: standardAggs,
			ExpectTotal:  6,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("c")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("e")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("d")},
					},
				},
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
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("f")},
					},
				},
			},
		},
		{
			Comment: "sort by born, descending",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchAllQuery()).
				SortBy([]string{"-born"}),
			Aggregations: standardAggs,
			ExpectTotal:  6,
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
						"_id": {[]byte("d")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("e")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("c")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("f")},
					},
				},
			},
		},
		{
			Comment: "sort by born, descending, missing first",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchAllQuery()).
				SortByCustom(search.SortOrder{
					search.SortBy(search.Field("born")).Desc().MissingFirst(),
				}),
			Aggregations: standardAggs,
			ExpectTotal:  6,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("f")},
					},
				},
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
						"_id": {[]byte("d")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("e")},
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
			Comment: "sort on multi-valued field",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchAllQuery()).
				SortByCustom(search.SortOrder{
					search.SortBy(&sortUsingMinOfMultipleValues{source: search.Field("tags")}),
				}),
			Aggregations: standardAggs,
			ExpectTotal:  6,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("a")},
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
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("b")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("e")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("f")},
					},
				},
			},
		},
		{
			Comment: "multi-column sort by age, ascending, name, ascending (flips b and c which have same age)",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchAllQuery()).
				SortBy([]string{"age", "name"}),
			Aggregations: standardAggs,
			ExpectTotal:  6,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("f")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("a")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("c")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("b")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("d")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("e")},
					},
				},
			},
		},
		{
			Comment: "sort by id descending",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchAllQuery()).
				SortBy([]string{"-_id"}),
			Aggregations: standardAggs,
			ExpectTotal:  6,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("f")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("e")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("d")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("c")},
					},
				},
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
			Comment: "sort by name, ascending, after marty",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchAllQuery()).
				SortBy([]string{"name"}).
				After([][]byte{[]byte("marty")}),
			Aggregations: standardAggs,
			ExpectTotal:  6,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("e")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("b")},
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
			Comment: "sort by name, ascending, before nancy",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchAllQuery()).
				SortBy([]string{"name"}).
				Before([][]byte{[]byte("nancy")}),
			Aggregations: standardAggs,
			ExpectTotal:  6,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("c")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("f")},
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
			Comment: "sort by id, after doc d",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchAllQuery()).
				SortBy([]string{"_id"}).
				After([][]byte{[]byte("d")}),
			Aggregations: standardAggs,
			ExpectTotal:  6,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("e")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("f")},
					},
				},
			},
		},
		{
			Comment: "sort by id, before doc d",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchAllQuery()).
				SortBy([]string{"_id"}).
				Before([][]byte{[]byte("d")}),
			Aggregations: standardAggs,
			ExpectTotal:  6,
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
			},
		},
	}
}
