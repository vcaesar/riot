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
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"reflect"
	"sort"
	"testing"

	"github.com/blugelabs/bluge/search"
	"github.com/blugelabs/bluge/search/aggregations"

	"github.com/blugelabs/bluge"
)

var segType = flag.String("segType", "", "force scorch segment type")
var segVer = flag.Int("segVer", 0, "force scorch segment version")

func collectHits(dmi search.DocumentMatchIterator) (rv []*match, err error) {
	var next *search.DocumentMatch
	next, err = dmi.Next()
	for next != nil && err == nil {
		nextMatch := &match{
			Number:    next.Number,
			Score:     next.Score,
			Fields:    map[string][][]byte{},
			Locations: next.Locations,
		}
		err = next.VisitStoredFields(func(field string, value []byte) bool {
			cp := make([]byte, len(value))
			copy(cp, value)
			nextMatch.Fields[field] = append(nextMatch.Fields[field], cp)
			return true
		})
		if err != nil {
			return nil, fmt.Errorf("error visiting stored fields: %v", err)
		}
		rv = append(rv, nextMatch)
		next, err = dmi.Next()
	}
	if err != nil {
		return nil, fmt.Errorf("error iterating results:  %v", err)
	}
	return rv, nil
}

func getTotalHitsMaxScore(bucket *search.Bucket) (total int, topScore float64) {
	total = int(bucket.Aggregations()["count"].(search.MetricCalculator).Value())
	topScore = bucket.Aggregations()["max_score"].(search.MetricCalculator).Value()
	if math.IsInf(topScore, -1) {
		topScore = 0
	}
	return total, topScore
}

var standardAggs = search.Aggregations{
	"count":     aggregations.CountMatches(),
	"max_score": aggregations.Max(search.DocumentScore()),
}

func TestIntegration(t *testing.T) {
	integrationTests := []IntegrationTest{
		{
			Name:     "basic",
			DataLoad: basicLoad,
			Tests:    basicTests,
		},
		{
			Name:     "sort",
			DataLoad: sortLoad,
			Tests:    sortTests,
		},
		{
			Name:     "fosdem",
			DataLoad: fosdemLoad,
			Tests:    fosdemTests,
		},
		{
			Name:     "geo",
			DataLoad: geoLoad,
			Tests:    geoTests,
		},
		{
			Name:     "phrase",
			DataLoad: phraseLoad,
			Tests:    phraseTests,
		},
		{
			Name:     "aggregations",
			DataLoad: aggregationsLoad,
			Tests:    aggregationsTests,
		},
	}

	for _, intTest := range integrationTests {
		path, err := os.MkdirTemp("", "bluge-integration-test-"+intTest.Name)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("testdir: %s", path)
		cfg := bluge.DefaultConfig(path)
		if *segType != "" {
			cfg = cfg.WithSegmentType(*segType)
			t.Logf("forcing segment type: %s", *segType)
		}
		if *segVer != 0 {
			cfg = cfg.WithSegmentVersion(uint32(*segVer))
			t.Logf("forcing segment version: %d", *segVer)
		}
		idx, err := bluge.OpenWriter(cfg)
		if err != nil {
			t.Fatal(err)
		}
		err = intTest.DataLoad(idx)
		if err != nil {
			t.Fatalf("error loading data for %s: %v", intTest.Name, err)
		}
		reader, err := idx.Reader()
		if err != nil {
			t.Fatal(err)
		}
		for _, test := range intTest.Tests() {
			test := test
			t.Run(fmt.Sprintf("%s-%s", intTest.Name, test.Comment), func(t *testing.T) {
				for aggName, agg := range test.Aggregations {
					test.Request.AddAggregation(aggName, agg)
				}
				dmi, err := reader.Search(context.Background(), test.Request)
				if err != nil {
					t.Fatalf("error executing search: %v", err)
				}
				if test.ExpectMatches != nil {
					matches, err := collectHits(dmi)
					if err != nil {
						t.Errorf("error collecting hits: %v", err)
					}
					if len(test.ExpectMatches) != len(matches) {
						t.Errorf("expected %d matches, got %d", len(test.ExpectMatches), len(matches))
					}
					for i, match := range matches {
						if i < len(test.ExpectMatches) {
							for field, vals := range test.ExpectMatches[i].Fields {
								compareFieldVals(t, i, field, vals, match.Fields[field], match.Number, match.Score, match.SortValue)
							}

							if len(test.ExpectMatches[i].ExpectHighlights) > 0 {
								for _, expectHighlight := range test.ExpectMatches[i].ExpectHighlights {
									got := expectHighlight.Highlighter.BestFragment(
										match.Locations[expectHighlight.Field], match.Fields[expectHighlight.Field][0])
									if got != expectHighlight.Result {
										t.Errorf("expected '%s', got '%s'", expectHighlight.Result, got)
									}
								}
							}
						}
					}
				}

				total, _ := getTotalHitsMaxScore(dmi.Aggregations())
				if total != test.ExpectTotal {
					t.Errorf("expected %d total hits, got %d", test.ExpectTotal, total)
				}
				if test.VerifyAggregations != nil {
					test.VerifyAggregations(t, dmi.Aggregations())
				}
			})
		}
	}
}

func compareFieldVals(t *testing.T, index int, field string, a, b [][]byte, number uint64, score float64, sortV [][]byte) {
	var aStrs, bStrs []string
	for _, aVal := range a {
		aStrs = append(aStrs, string(aVal))
	}
	for _, bVal := range b {
		bStrs = append(bStrs, string(bVal))
	}
	sort.Strings(aStrs)
	sort.Strings(bStrs)
	if !reflect.DeepEqual(aStrs, bStrs) {
		t.Errorf("expected hit %d - %s to contain %v, got %s number: %d score: %f sort: %v", index, field, aStrs, bStrs, number, score, sortV)
	}
}
