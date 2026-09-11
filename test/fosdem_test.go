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
	"github.com/vcaesar/riot/analysis/analyzer"

	"github.com/vcaesar/riot/analysis/lang/en"
)

func fosdemLoad(writer *bluge.Writer) error {
	enAnalyzer := en.NewAnalyzer()

	err := writer.Insert(bluge.NewDocument("3311@FOSDEM15@fosdem.org").
		AddField(bluge.NewTextField("description",
			"From Prolog to Erlang to Haskell to Lisp to TLC and then back to Prolog I have journeyed, and I'd like to share some of the beautiful").
			StoreValue().
			SearchTermPositions().
			WithAnalyzer(enAnalyzer)).
		AddField(bluge.NewKeywordField("category", "Word").
			StoreValue().
			SearchTermPositions()).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	err = writer.Insert(bluge.NewDocument("3492@FOSDEM15@fosdem.org").
		AddField(bluge.NewTextField("description",
			"different cats").
			StoreValue().
			SearchTermPositions().
			WithAnalyzer(enAnalyzer)).
		AddField(bluge.NewKeywordField("category", "Perl").
			StoreValue().
			SearchTermPositions()).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	err = writer.Insert(bluge.NewDocument("3496@FOSDEM15@fosdem.org").
		AddField(bluge.NewTextField("description",
			"many cats").
			StoreValue().
			SearchTermPositions().
			WithAnalyzer(enAnalyzer)).
		AddField(bluge.NewKeywordField("category", "Perl").
			StoreValue().
			SearchTermPositions()).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	err = writer.Insert(bluge.NewDocument("3505@FOSDEM15@fosdem.org").
		AddField(bluge.NewTextField("description",
			"From Prolog to Erlang to Haskell to Lisp to TLC and then back to Prolog I have journeyed, and I'd like to share some of the beautiful").
			StoreValue().
			SearchTermPositions().
			WithAnalyzer(enAnalyzer)).
		AddField(bluge.NewKeywordField("category", "Perl").
			StoreValue().
			SearchTermPositions()).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	err = writer.Insert(bluge.NewDocument("3507@FOSDEM15@fosdem.org").
		AddField(bluge.NewTextField("description",
			"From Prolog to Erlang to Haskell to Gel to TLC and then back to Prolog I have journeyed, and I'd like to share some of the beautifull").
			StoreValue().
			SearchTermPositions().
			WithAnalyzer(enAnalyzer)).
		AddField(bluge.NewKeywordField("category", "Perl").
			StoreValue().
			SearchTermPositions()).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	return nil
}

func fosdemTests() []*RequestVerify {
	enAnalyzer := en.NewAnalyzer()
	keywordAnalyzer := analyzer.NewKeywordAnalyzer()
	return []*RequestVerify{
		{
			Comment: "fosdem 1",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("Perl").
					SetField("category").
					SetAnalyzer(keywordAnalyzer)).
				SortBy([]string{"-_score", "_id"}),
			Aggregations: standardAggs,
			ExpectTotal:  4,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("3492@FOSDEM15@fosdem.org")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("3496@FOSDEM15@fosdem.org")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("3505@FOSDEM15@fosdem.org")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("3507@FOSDEM15@fosdem.org")},
					},
				},
			},
		},
		{
			Comment: "fosdem 2",
			Request: bluge.NewTopNSearch(10,
				bluge.NewMatchPhraseQuery("lisp")).
				SortBy([]string{"-_score", "_id"}),
			Aggregations: standardAggs,
			ExpectTotal:  2,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("3311@FOSDEM15@fosdem.org")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("3505@FOSDEM15@fosdem.org")},
					},
				},
			},
		},
		{
			Comment: "fosdem 3",
			Request: bluge.NewTopNSearch(10,
				bluge.NewBooleanQuery().
					AddMust(
						bluge.NewMatchQuery("lisp"),
						bluge.NewMatchQuery("Perl").
							SetField("category").
							SetAnalyzer(keywordAnalyzer))).
				SortBy([]string{"-_score", "_id"}),
			Aggregations: standardAggs,
			ExpectTotal:  1,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("3505@FOSDEM15@fosdem.org")},
					},
				},
			},
		},
		{
			Comment: "fosdem 4",
			Request: bluge.NewTopNSearch(10,
				bluge.NewBooleanQuery().
					AddMust(
						bluge.NewMatchQuery("lisp"),
						bluge.NewMatchPhraseQuery("Perl").
							SetField("category").
							SetAnalyzer(keywordAnalyzer))).
				SortBy([]string{"-_score", "_id"}),
			Aggregations: standardAggs,
			ExpectTotal:  1,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("3505@FOSDEM15@fosdem.org")},
					},
				},
			},
		},
		{
			Comment: "fosdem 5",
			Request: bluge.NewTopNSearch(10,
				bluge.NewBooleanQuery().
					AddMust(
						bluge.NewMatchQuery("Perl").
							SetField("category").
							SetAnalyzer(keywordAnalyzer)).
					AddMust(
						bluge.NewBooleanQuery().
							AddMust(
								bluge.NewMatchQuery("cats").
									SetAnalyzer(enAnalyzer)))).
				SortBy([]string{"-_score", "_id"}),
			Aggregations: standardAggs,
			ExpectTotal:  2,
			ExpectMatches: []*match{
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("3492@FOSDEM15@fosdem.org")},
					},
				},
				{
					Fields: map[string][][]byte{
						"_id": {[]byte("3496@FOSDEM15@fosdem.org")},
					},
				},
			},
		},
	}
}
