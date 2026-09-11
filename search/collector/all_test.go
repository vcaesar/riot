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

package collector

import (
	"context"
	"reflect"
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/riot/numeric"
	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/aggregations"
)

func TestUniqueFields(t *testing.T) {
	for _, test := range []struct {
		name   string
		fields []string
		want   []string
	}{
		{name: "nil"},
		{name: "empty", fields: []string{}, want: []string{}},
		{name: "single", fields: []string{"a"}, want: []string{"a"}},
		{name: "distinct", fields: []string{"b", "a"}, want: []string{"b", "a"}},
		{name: "repeated", fields: []string{"b", "a", "b", "c", "a"}, want: []string{"b", "a", "c"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := uniqueFields(test.fields); !reflect.DeepEqual(got, test.want) {
				t.Errorf("expected %v, got %v", test.want, got)
			}
		})
	}
}

type fieldTrackingReader struct {
	fields []string
}

func (r *fieldTrackingReader) DocumentValueReader(fields []string) (segment.DocumentValueReader, error) {
	r.fields = fields
	return r, nil
}

func (r *fieldTrackingReader) VisitDocumentValues(_ uint64, visitor segment.DocumentValueVisitor) error {
	for _, field := range r.fields {
		for _, value := range []float64{2, 3} {
			encoded, err := numeric.NewPrefixCodedInt64(numeric.Float64ToInt64(value), 0)
			if err != nil {
				return err
			}
			visitor(field, encoded)
		}
	}
	return nil
}

func (r *fieldTrackingReader) VisitStoredFields(_ uint64, _ segment.StoredFieldVisitor) error {
	return nil
}

type fieldTrackingSearcher struct {
	stubSearcher
	reader *fieldTrackingReader
}

func (s *fieldTrackingSearcher) Next(ctx *search.Context) (*search.DocumentMatch, error) {
	match, err := s.stubSearcher.Next(ctx)
	if match != nil {
		match.SetReader(s.reader)
	}
	return match, err
}

func TestCollectorsSharedFields(t *testing.T) {
	for _, name := range []string{"all", "topn", "sort_and_aggregation", "duplicate_sort"} {
		t.Run(name, func(t *testing.T) {
			aggs := make(search.Aggregations)
			aggs.Add("sum", aggregations.Sum(search.Field("value")))
			if name == "all" || name == "topn" {
				aggs.Add("other_sum", aggregations.Sum(search.Field("value")))
			}
			var c search.Collector = NewAllCollector()
			if name != "all" {
				var order search.SortOrder
				if name != "topn" {
					order = search.ParseSortOrderStrings([]string{"value"})
				}
				if name == "duplicate_sort" {
					order = search.ParseSortOrderStrings([]string{"value", "-value"})
				}
				c = NewTopNCollector(1, 0, order)
			}
			reader := &fieldTrackingReader{}
			s := &fieldTrackingSearcher{
				stubSearcher: stubSearcher{matches: []*search.DocumentMatch{{Number: 0}}},
				reader:       reader,
			}
			iter, err := c.Collect(context.Background(), aggs, s)
			if err != nil {
				t.Fatal(err)
			}
			for {
				match, nextErr := iter.Next()
				if nextErr != nil {
					t.Fatal(nextErr)
				}
				if match == nil {
					break
				}
			}
			if len(reader.fields) != 1 || reader.fields[0] != "value" {
				t.Errorf("expected value loaded once, got %v", reader.fields)
			}
			for agg := range aggs {
				if got := iter.Aggregations().Metric(agg); got != 5 {
					t.Errorf("%s: expected sum 5, got %v", agg, got)
				}
			}
		})
	}
}

func TestAllCollector(t *testing.T) {
	matches := makeMatches(99, 11)
	searcher := &stubSearcher{
		matches: matches,
	}

	aggs := make(search.Aggregations)
	aggs.Add("count", aggregations.CountMatches())

	collector := NewAllCollector()
	dmi, err := collector.Collect(context.Background(), aggs, searcher)
	if err != nil {
		t.Fatal(err)
	}

	var count uint64
	next, err := dmi.Next()
	for err == nil && next != nil {
		count++

		// test that we can see aggregations while iterating with this collector
		if dmi.Aggregations().Count() != count {
			t.Errorf("expected aggregations count to match running count, %d != %d",
				count, dmi.Aggregations().Count())
		}

		next, err = dmi.Next()
	}
	if err != nil {
		t.Fatalf("error iterator matches: %v", err)
	}

	if count != 99 {
		t.Errorf("expected to see 99 hits, saw: %d", count)
	}
}
