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
	bluge "github.com/vcaesar/riot"
	"github.com/vcaesar/riot/analysis/lang/en"
)

func phraseLoad(writer *bluge.Writer) error {
	enAnalyzer := en.NewAnalyzer()

	err := writer.Insert(bluge.NewDocument("a").
		AddField(bluge.NewTextField("body", "Twenty Thousand Leagues Under The Sea").
			SearchTermPositions().
			StoreValue().
			WithAnalyzer(enAnalyzer)).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	err = writer.Insert(bluge.NewDocument("b").
		AddField(bluge.NewTextField("body", "bad call").
			SearchTermPositions().
			StoreValue().
			WithAnalyzer(enAnalyzer)).
		AddField(bluge.NewTextField("body", "defenseless receiver").
			SearchTermPositions().
			StoreValue().
			WithAnalyzer(enAnalyzer)).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	return nil
}

func phraseTests() []*RequestVerify {
	enAnalyzer := en.NewAnalyzer()
	return []*RequestVerify{
		{
			Comment: "phrase 1",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("Twenty").
					SetAnalyzer(enAnalyzer)),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: newIDMatches("a"),
		},
		{
			Comment: "phrase 2",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("Twenty Thousand").
					SetAnalyzer(enAnalyzer)),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: newIDMatches("a"),
		},
		{
			Comment: "phrase 3",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("Twenty Thousand Leagues").
					SetAnalyzer(enAnalyzer)),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: newIDMatches("a"),
		},
		{
			Comment: "phrase 4",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("Twenty Thousand Leagues Under").
					SetAnalyzer(enAnalyzer)),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: newIDMatches("a"),
		},
		{
			Comment: "phrase 5",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("Twenty Thousand Leagues Under the").
					SetAnalyzer(enAnalyzer)),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: newIDMatches("a"),
		},
		{
			Comment: "phrase 6",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("Twenty Thousand Leagues Under the Sea").
					SetAnalyzer(enAnalyzer)),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: newIDMatches("a"),
		},
		{
			Comment: "phrase 7",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("Thousand").
					SetAnalyzer(enAnalyzer)),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: newIDMatches("a"),
		},
		{
			Comment: "phrase 8",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("Thousand Leagues").
					SetAnalyzer(enAnalyzer)),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: newIDMatches("a"),
		},
		{
			Comment: "phrase 9",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("Thousand Leagues Under").
					SetAnalyzer(enAnalyzer)),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: newIDMatches("a"),
		},
		{
			Comment: "phrase 10",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("Thousand Leagues Under the").
					SetAnalyzer(enAnalyzer)),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: newIDMatches("a"),
		},
		{
			Comment: "phrase 11",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("Thousand Leagues Under the Sea").
					SetAnalyzer(enAnalyzer)),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: newIDMatches("a"),
		},
		{
			Comment: "phrase 12",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("Leagues").
					SetAnalyzer(enAnalyzer)),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: newIDMatches("a"),
		},
		{
			Comment: "phrase 13",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("Leagues Under").
					SetAnalyzer(enAnalyzer)),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: newIDMatches("a"),
		},
		{
			Comment: "phrase 14",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("Leagues Under the").
					SetAnalyzer(enAnalyzer)),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: newIDMatches("a"),
		},
		{
			Comment: "phrase 15",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("Leagues Under the Sea").
					SetAnalyzer(enAnalyzer)),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: newIDMatches("a"),
		},
		{
			Comment: "phrase 16",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("Under the Sea").
					SetAnalyzer(enAnalyzer)),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: newIDMatches("a"),
		},
		{
			Comment: "phrase 17",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("the Sea").
					SetAnalyzer(enAnalyzer)),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: newIDMatches("a"),
		},
		{
			Comment: "phrase 18",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("Sea").
					SetAnalyzer(enAnalyzer)),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: newIDMatches("a"),
		},
		{
			Comment: "phrase 19",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("bad call").
					SetAnalyzer(enAnalyzer)),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: newIDMatches("b"),
		},
		{
			Comment: "phrase 20",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("defenseless receiver").
					SetAnalyzer(enAnalyzer)),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: newIDMatches("b"),
		},
		{
			Comment: "phrase 21",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("bad receiver").
					SetAnalyzer(enAnalyzer)),
			Aggregations:  standardAggs,
			ExpectTotal:   0,
			ExpectMatches: []*match{},
		},
		{
			Comment: "phrase 22",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMultiPhraseQuery([][]string{{"twenti", "thirti"}, {"thousand"}})),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: newIDMatches("a"),
		},
		{
			Comment: "phrase 23",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("call defenseless").
					SetAnalyzer(enAnalyzer)),
			Aggregations:  standardAggs,
			ExpectTotal:   0,
			ExpectMatches: []*match{},
		},
		{
			Comment: "phrase 24",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("Twenty Thousand").
					SetAnalyzer(enAnalyzer).
					SetSlop(1)),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: newIDMatches("a"),
		},
		{
			Comment: "phrase 25",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("Twenty Leagues").
					SetAnalyzer(enAnalyzer).
					SetSlop(1)),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: newIDMatches("a"),
		},
		{
			Comment: "phrase 26",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("Twenty under the sea").
					SetAnalyzer(enAnalyzer).
					SetSlop(2)),
			Aggregations:  standardAggs,
			ExpectTotal:   1,
			ExpectMatches: newIDMatches("a"),
		},
	}
}
