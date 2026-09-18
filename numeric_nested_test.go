//  Copyright (c) 2026 The Bluge Authors.
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

package riot

import (
	"context"
	"testing"

	"github.com/vcaesar/riot/numeric"
	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/aggregations"
)

func TestNumericColumnNestedAggregations(t *testing.T) {
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	}()
	doc := NewDocument("nested").
		AddField(NewNumericField("outer", 1)).
		AddField(NewNumericField("outer", 2)).
		AddField(NewNumericField("inner", 100)).
		AddField(NewNumericField("inner", 200))
	if err := w.Insert(doc); err != nil {
		t.Fatal(err)
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	}()
	terms := aggregations.NewTermsAggregation(search.Field("outer"), 10)
	terms.AddAggregation("inner", aggregations.NewTermsAggregation(search.Field("inner"), 10))
	ranges := aggregations.Ranges(search.Field("outer")).
		AddRange(aggregations.NamedRange("one", 1, 2)).
		AddRange(aggregations.NamedRange("two", 2, 3))
	ranges.AddAggregation("sum", aggregations.Sum(search.Field("inner")))
	req := NewTopNSearch(0, NewMatchAllQuery())
	req.AddAggregation("terms", terms)
	req.AddAggregation("ranges", ranges)
	dmi, err := r.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	for {
		match, err := dmi.Next()
		if err != nil {
			t.Fatal(err)
		}
		if match == nil {
			break
		}
	}
	buckets := dmi.Aggregations().Buckets("terms")
	if len(buckets) != 2 {
		t.Fatalf("got %d outer terms", len(buckets))
	}
	seen := map[float64]bool{}
	for _, bucket := range buckets {
		n, err := numeric.PrefixCoded(bucket.Name()).Int64()
		if err != nil {
			t.Fatal(err)
		}
		seen[numeric.Int64ToFloat64(n)] = true
		if bucket.Count() != 1 || len(bucket.Buckets("inner")) != 2 {
			t.Fatalf("invalid nested bucket: %v", bucket)
		}
	}
	if !seen[1] || !seen[2] {
		t.Fatalf("outer keys overwritten: %v", seen)
	}
	for _, bucket := range dmi.Aggregations().Buckets("ranges") {
		if bucket.Count() != 1 || bucket.Metric("sum") != 300 {
			t.Fatalf("range %s: count %d, sum %v", bucket.Name(), bucket.Count(), bucket.Metric("sum"))
		}
	}
}
