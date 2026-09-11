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

package aggregations

import (
	"math"
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"

	"github.com/vcaesar/riot/numeric"
	"github.com/vcaesar/riot/search"
)

func TestAggregations(t *testing.T) {
	global := buildTestAggregations()
	fieldsNeeded := global.Fields()
	assertFieldsSeen(t, []string{"age", "name", "type"}, fieldsNeeded)

	bucket := search.NewBucket("global", global)
	testDocs := buildTestDocs()
	for _, doc := range testDocs {
		err := doc.LoadDocumentValues(search.NewSearchContext(0, 0), global.Fields())
		if err != nil {
			t.Fatal(err)
		}
		bucket.Consume(doc)
	}
	bucket.Finish()

	expect := buildTestBucketExpectations()
	expect.Assert(t, bucket, "")
}

func TestAggregationMerge(t *testing.T) {
	global := buildTestAggregations()
	fieldsNeeded := global.Fields()
	assertFieldsSeen(t, []string{"age", "name", "type"}, fieldsNeeded)

	testDocs := buildTestDocs()

	// process the first 5 docs in shard1
	shard1 := search.NewBucket("shard1", global)
	for _, doc := range testDocs[0:5] {
		err := doc.LoadDocumentValues(search.NewSearchContext(0, 0), global.Fields())
		if err != nil {
			t.Fatal(err)
		}
		shard1.Consume(doc)
	}
	shard1.Finish()

	// process the next 5 docs in shard2
	shard2 := search.NewBucket("shard2", global)
	for _, doc := range testDocs[5:] {
		err := doc.LoadDocumentValues(search.NewSearchContext(0, 0), global.Fields())
		if err != nil {
			t.Fatal(err)
		}
		shard2.Consume(doc)
	}
	shard2.Finish()

	// merge shard2 into shard1
	shard1.Merge(shard2)

	expect := buildTestBucketExpectations()
	expect.Assert(t, shard1, "")
}

type matchReader struct {
	docVals map[string][]byte
}

func (mr *matchReader) DocumentValueReader(_ []string) (segment.DocumentValueReader, error) {
	return mr, nil
}

func (mr *matchReader) VisitDocumentValues(_ uint64, visitor segment.DocumentValueVisitor) error {
	for k, v := range mr.docVals {
		visitor(k, v)
	}
	return nil
}

func (mr *matchReader) VisitStoredFields(_ uint64, _ segment.StoredFieldVisitor) error {
	return nil
}

func newDocumentMatch(number uint64, score float64, docVals map[string][]byte) *search.DocumentMatch {
	rv := &search.DocumentMatch{
		Number: number,
		Score:  score,
	}
	rv.SetReader(&matchReader{docVals: docVals})
	return rv
}

func buildTestDocs() []*search.DocumentMatch {
	return []*search.DocumentMatch{
		newDocumentMatch(0, 1.0,
			map[string][]byte{
				"name": []byte("barbara"),
				"type": []byte("employee"),
				"age":  numeric.MustNewPrefixCodedInt64(numeric.Float64ToInt64(1), 0),
			}),
		newDocumentMatch(1, 1.2,
			map[string][]byte{
				"name": []byte("john"),
				"type": []byte("employee"),
				"age":  numeric.MustNewPrefixCodedInt64(numeric.Float64ToInt64(25), 0),
			}),

		newDocumentMatch(2, 1.01,
			map[string][]byte{
				"name": []byte("barbara"),
				"type": []byte("employee"),
				"age":  numeric.MustNewPrefixCodedInt64(numeric.Float64ToInt64(16), 0),
			}),

		newDocumentMatch(3, 1.5,
			map[string][]byte{
				"name": []byte("dale"),
				"type": []byte("employee"),
				"age":  numeric.MustNewPrefixCodedInt64(numeric.Float64ToInt64(32), 0),
			}),
		newDocumentMatch(4, 1.6,
			map[string][]byte{
				"name": []byte("judy"),
				"type": []byte("contractor"),
				"age":  numeric.MustNewPrefixCodedInt64(numeric.Float64ToInt64(48), 0),
			}),
		newDocumentMatch(5, 1.2,
			map[string][]byte{
				"name": []byte("donna"),
				"type": []byte("employee"),
				"age":  numeric.MustNewPrefixCodedInt64(numeric.Float64ToInt64(63), 0),
			}),

		newDocumentMatch(6, 1.2,
			map[string][]byte{
				"name": []byte("john"),
				"type": []byte("employee"),
				"age":  numeric.MustNewPrefixCodedInt64(numeric.Float64ToInt64(4), 0),
			}),

		newDocumentMatch(7, 1.14,
			map[string][]byte{
				"name": []byte("gary"),
				"type": []byte("employee"),
				"age":  numeric.MustNewPrefixCodedInt64(numeric.Float64ToInt64(95), 0),
			}),

		newDocumentMatch(8, 1.1,
			map[string][]byte{
				"name": []byte("john"),
				"type": []byte("contractor"),
				"age":  numeric.MustNewPrefixCodedInt64(numeric.Float64ToInt64(39), 0),
			}),

		newDocumentMatch(9, 1.22,
			map[string][]byte{
				"name": []byte("carol"),
				"type": []byte("employee"),
				"age":  numeric.MustNewPrefixCodedInt64(numeric.Float64ToInt64(11), 0),
			}),
	}
}

func assertFieldsSeen(t *testing.T, expectedFields, actualFields []string) {
	expectFields := map[string]bool{}
	for _, expectField := range expectedFields {
		expectFields[expectField] = false
	}
	for _, field := range actualFields {
		expectFields[field] = true
	}
	for field, seen := range expectFields {
		if !seen {
			t.Errorf("expected to see field '%s', did not", field)
		}
	}
}

type bucketExpectation struct {
	metrics  map[string]float64
	children map[string]map[string]*bucketExpectation
}

func (b bucketExpectation) Assert(t *testing.T, bucket *search.Bucket, path string) {
	for name, agg := range bucket.Aggregations() {
		switch c := agg.(type) {
		case search.MetricCalculator:
			if expectMetricValue, ok := b.metrics[name]; ok {
				if c.Value() != expectMetricValue {
					t.Errorf("expected value %f got %f for '%s'", expectMetricValue, c.Value(), path+"."+name)
				}
			} else {
				t.Errorf("unexpected metric %s in path '%s'", name, path)
			}
			delete(b.metrics, name)
		case search.BucketCalculator:
			if expectedBuckets, ok := b.children[name]; ok {
				buckets := c.Buckets()
				if len(expectedBuckets) != len(buckets) {
					t.Errorf("expected %d buckets, got %d, at '%s'", len(expectedBuckets), len(buckets), path+"."+name)
				}
				for _, bucket := range buckets {
					if expectedBucket, ok := expectedBuckets[bucket.Name()]; ok {
						expectedBucket.Assert(t, bucket, path+name+"."+bucket.Name())
					} else {
						t.Errorf("unexpected bucket %s in path '%s'", bucket.Name(), path+"."+name)
					}
				}
			} else {
				t.Errorf("unexpected bucket aggregation %s in path '%s'", name, path)
			}
			delete(b.children, name)
		}
	}
	for metricName := range b.metrics {
		t.Errorf("expected a metric named %s at path '%s', was missing", metricName, path)
	}
	for aggName := range b.children {
		t.Errorf("expected an aggregation named: %s at path '%s', was missing", aggName, path)
	}
}

func buildTestAggregations() search.Aggregations {
	global := make(search.Aggregations)

	child := NamedRange("children", 0, 18)
	adult := NamedRange("adults", 18, math.Inf(1))

	byAge := Ranges(search.Field("age")).
		AddRange(child).
		AddRange(adult).
		AddAggregation("min_age", Min(search.Field("age"))).
		AddAggregation("max_age", Max(search.Field("age")))

	global.Add("byAge", byAge)

	global.Add("max_score", Max(search.DocumentScore()))
	global.Add("doc_count", CountMatches())
	global.Add("min_age", Min(search.Field("age")))
	global.Add("max_age", Max(search.Field("age")))
	global.Add("avg_age", Avg(search.Field("age")))

	global.Add("quantiles", Quantiles(search.Field("age")))

	termsAgg := NewTermsAggregation(search.Field("name"), 2)
	global.Add("byName", termsAgg)

	typesAgg := NewTermsAggregation(search.Field("type"), 2)
	global.Add("byType", typesAgg)

	return global
}

func buildTestBucketExpectations() *bucketExpectation {
	return &bucketExpectation{
		metrics: map[string]float64{
			"doc_count": 10.0,
			"max_score": 1.6,
			"min_age":   1.0,
			"max_age":   95,
			"avg_age":   33.4,
		},
		children: map[string]map[string]*bucketExpectation{
			"byType": {
				"employee": &bucketExpectation{
					metrics: map[string]float64{
						"count": 8.0,
					},
				},
				"contractor": &bucketExpectation{
					metrics: map[string]float64{
						"count": 2.0,
					},
				},
			},
			"byName": {
				"john": &bucketExpectation{
					metrics: map[string]float64{
						"count": 3.0,
					},
				},
				"barbara": &bucketExpectation{
					metrics: map[string]float64{
						"count": 2.0,
					},
				},
			},
			"byAge": {
				"children": &bucketExpectation{
					metrics: map[string]float64{
						"count":   4.0,
						"min_age": 1.0,
						"max_age": 16.0,
					},
				},
				"adults": &bucketExpectation{
					metrics: map[string]float64{
						"count":   6.0,
						"min_age": 25.0,
						"max_age": 95.0,
					},
				},
			},
		},
	}
}
