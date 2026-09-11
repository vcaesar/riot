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
	"math"
	"testing"
	"time"

	bluge "github.com/vcaesar/riot"
	"github.com/vcaesar/riot/search/aggregations"

	"github.com/vcaesar/riot/search"
)

func aggregationsLoad(writer *bluge.Writer) error {
	updated, err := time.Parse(time.RFC3339, "2014-11-25T00:00:00Z")
	if err != nil {
		return err
	}
	err = writer.Insert(bluge.NewDocument("a").
		AddField(bluge.NewTextField("category", "inventory").
			Aggregatable()).
		AddField(bluge.NewTextField("type", "book").
			Aggregatable()).
		AddField(bluge.NewDateTimeField("updated", updated).
			Aggregatable()).
		AddField(bluge.NewNumericField("rating", 2).
			Aggregatable()).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	updated, err = time.Parse(time.RFC3339, "2013-07-25T00:00:00Z")
	if err != nil {
		return err
	}
	err = writer.Insert(bluge.NewDocument("b").
		AddField(bluge.NewTextField("category", "inventory").
			Aggregatable()).
		AddField(bluge.NewTextField("type", "book").
			Aggregatable()).
		AddField(bluge.NewDateTimeField("updated", updated).
			Aggregatable()).
		AddField(bluge.NewNumericField("rating", 7).
			Aggregatable()).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	updated, err = time.Parse(time.RFC3339, "2014-03-03T00:00:00Z")
	if err != nil {
		return err
	}
	err = writer.Insert(bluge.NewDocument("c").
		AddField(bluge.NewTextField("category", "inventory").
			Aggregatable()).
		AddField(bluge.NewTextField("type", "book").
			Aggregatable()).
		AddField(bluge.NewDateTimeField("updated", updated).
			Aggregatable()).
		AddField(bluge.NewNumericField("rating", 1).
			Aggregatable()).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	updated, err = time.Parse(time.RFC3339, "2014-09-16T00:00:00Z")
	if err != nil {
		return err
	}
	err = writer.Insert(bluge.NewDocument("d").
		AddField(bluge.NewTextField("category", "inventory").
			Aggregatable()).
		AddField(bluge.NewTextField("type", "book").
			Aggregatable()).
		AddField(bluge.NewDateTimeField("updated", updated).
			Aggregatable()).
		AddField(bluge.NewNumericField("rating", 9).
			Aggregatable()).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	updated, err = time.Parse(time.RFC3339, "2014-11-15T00:00:00Z")
	if err != nil {
		return err
	}
	err = writer.Insert(bluge.NewDocument("e").
		AddField(bluge.NewTextField("category", "inventory").
			Aggregatable()).
		AddField(bluge.NewTextField("type", "book").
			Aggregatable()).
		AddField(bluge.NewDateTimeField("updated", updated).
			Aggregatable()).
		AddField(bluge.NewNumericField("rating", 5).
			Aggregatable()).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	updated, err = time.Parse(time.RFC3339, "2017-06-05T00:00:00Z")
	if err != nil {
		return err
	}
	err = writer.Insert(bluge.NewDocument("f").
		AddField(bluge.NewTextField("category", "inventory").
			Aggregatable()).
		AddField(bluge.NewTextField("type", "movie").
			Aggregatable()).
		AddField(bluge.NewDateTimeField("updated", updated).
			Aggregatable()).
		AddField(bluge.NewNumericField("rating", 3).
			Aggregatable()).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	updated, err = time.Parse(time.RFC3339, "2011-10-03T00:00:00Z")
	if err != nil {
		return err
	}
	err = writer.Insert(bluge.NewDocument("g").
		AddField(bluge.NewTextField("category", "inventory").
			Aggregatable()).
		AddField(bluge.NewTextField("type", "movie").
			Aggregatable()).
		AddField(bluge.NewDateTimeField("updated", updated).
			Aggregatable()).
		AddField(bluge.NewNumericField("rating", 9).
			Aggregatable()).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	updated, err = time.Parse(time.RFC3339, "2019-08-02T00:00:00Z")
	if err != nil {
		return err
	}
	err = writer.Insert(bluge.NewDocument("h").
		AddField(bluge.NewTextField("category", "inventory").
			Aggregatable()).
		AddField(bluge.NewTextField("type", "movie").
			Aggregatable()).
		AddField(bluge.NewDateTimeField("updated", updated).
			Aggregatable()).
		AddField(bluge.NewNumericField("rating", 9).
			Aggregatable()).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	updated, err = time.Parse(time.RFC3339, "2014-12-14T00:00:00Z")
	if err != nil {
		return err
	}
	err = writer.Insert(bluge.NewDocument("h").
		AddField(bluge.NewTextField("category", "inventory").
			Aggregatable()).
		AddField(bluge.NewTextField("type", "movie").
			Aggregatable()).
		AddField(bluge.NewDateTimeField("updated", updated).
			Aggregatable()).
		AddField(bluge.NewNumericField("rating", 1).
			Aggregatable()).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	updated, err = time.Parse(time.RFC3339, "2013-10-20T00:00:00Z")
	if err != nil {
		return err
	}
	err = writer.Insert(bluge.NewDocument("i").
		AddField(bluge.NewTextField("category", "inventory").
			Aggregatable()).
		AddField(bluge.NewTextField("type", "game").
			Aggregatable()).
		AddField(bluge.NewDateTimeField("updated", updated).
			Aggregatable()).
		AddField(bluge.NewNumericField("rating", 9).
			Aggregatable()).
		AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id"})))
	if err != nil {
		return err
	}

	return nil
}

func bucketCount(b *search.Bucket) int {
	return int(b.Aggregations()["count"].(search.MetricCalculator).Value())
}

func aggregationsTests() []*RequestVerify {
	oldNewDate, err := time.Parse(time.RFC3339, "2012-01-01T00:00:00Z")
	if err != nil {
		panic(err)
	}

	return []*RequestVerify{
		{
			Comment: "category inventory, by type",
			Request: bluge.NewTopNSearch(0,
				bluge.NewTermQuery("inventory").
					SetField("category")),
			Aggregations: search.Aggregations{
				"count":     aggregations.CountMatches(),
				"max_score": aggregations.Max(search.DocumentScore()),
				"types":     aggregations.NewTermsAggregation(search.Field("type"), 3),
			},
			ExpectTotal:   10,
			ExpectMatches: []*match{},
			VerifyAggregations: func(t *testing.T, bucket *search.Bucket) {
				typesAgg := bucket.Aggregations()["types"].(*aggregations.TermsCalculator)
				if typesAgg.Other() != 0 {
					t.Errorf("expected other types 0, got %d", typesAgg.Other())
				}
				typesBuckets := typesAgg.Buckets()
				if len(typesBuckets) != 3 {
					t.Errorf("expected 3 buckets in types, got %d", len(typesBuckets))
				} else {
					for _, b := range typesAgg.Buckets() {
						switch b.Name() {
						case "book":
							bookCount := bucketCount(b)
							if bookCount != 5 {
								t.Errorf("expected 5 books, got %d", bookCount)
							}
						case "movie":
							movieCount := bucketCount(b)
							if movieCount != 4 {
								t.Errorf("expected 4 movies, got %d", movieCount)
							}
						case "game":
							gameCount := bucketCount(b)
							if gameCount != 1 {
								t.Errorf("expected 1 game, got %d", gameCount)
							}
						default:
							t.Errorf("unexpected bucket %s", b.Name())
						}
					}
				}
			},
		},
		{
			Comment: "category inventory, by rating high-low",
			Request: bluge.NewTopNSearch(0,
				bluge.NewTermQuery("inventory").
					SetField("category")),
			Aggregations: search.Aggregations{
				"count":     aggregations.CountMatches(),
				"max_score": aggregations.Max(search.DocumentScore()),
				"ratings": aggregations.Ranges(search.Field("rating")).
					AddRange(aggregations.NamedRange("low", math.Inf(-1), 5)).
					AddRange(aggregations.NamedRange("high", 5, math.Inf(1))),
			},
			ExpectTotal:   10,
			ExpectMatches: []*match{},
			VerifyAggregations: func(t *testing.T, bucket *search.Bucket) {
				typesAgg := bucket.Aggregations()["ratings"].(search.BucketCalculator)
				typesBuckets := typesAgg.Buckets()
				if len(typesBuckets) != 2 {
					t.Errorf("expected 2 buckets in types, got %d", len(typesBuckets))
				} else {
					for _, b := range typesAgg.Buckets() {
						switch b.Name() {
						case "low":
							bookCount := bucketCount(b)
							if bookCount != 4 {
								t.Errorf("expected 4 low, got %d", bookCount)
							}
						case "high":
							movieCount := bucketCount(b)
							if movieCount != 6 {
								t.Errorf("expected 6 high, got %d", movieCount)
							}
						default:
							t.Errorf("unexpected bucket %s", b.Name())
						}
					}
				}
			},
		},
		{
			Comment: "category inventory, by updated old-new",
			Request: bluge.NewTopNSearch(0,
				bluge.NewTermQuery("inventory").
					SetField("category")),
			Aggregations: search.Aggregations{
				"count":     aggregations.CountMatches(),
				"max_score": aggregations.Max(search.DocumentScore()),
				"updated": aggregations.DateRanges(search.Field("updated")).
					AddRange(aggregations.NewNamedDateRange("old", time.Time{}, oldNewDate)).
					AddRange(aggregations.NewNamedDateRange("new", oldNewDate, time.Time{})),
			},
			ExpectTotal:   10,
			ExpectMatches: []*match{},
			VerifyAggregations: func(t *testing.T, bucket *search.Bucket) {
				typesAgg := bucket.Aggregations()["updated"].(search.BucketCalculator)
				typesBuckets := typesAgg.Buckets()
				if len(typesBuckets) != 2 {
					t.Errorf("expected 2 buckets in types, got %d", len(typesBuckets))
				} else {
					for _, b := range typesAgg.Buckets() {
						switch b.Name() {
						case "old":
							bookCount := bucketCount(b)
							if bookCount != 1 {
								t.Errorf("expected 1 old, got %d", bookCount)
							}
						case "new":
							movieCount := bucketCount(b)
							if movieCount != 9 {
								t.Errorf("expected 9 new, got %d", movieCount)
							}
						default:
							t.Errorf("unexpected bucket %s", b.Name())
						}
					}
				}
			},
		},
	}
}
