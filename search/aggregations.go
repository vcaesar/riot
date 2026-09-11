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

package search

import (
	"time"
)

type Aggregation interface {
	Fields() []string
	Calculator() Calculator
}

type Aggregations map[string]Aggregation

func (a Aggregations) Add(name string, aggregation Aggregation) {
	a[name] = aggregation
}

func (a Aggregations) Fields() []string {
	var rv []string
	for _, aggregation := range a {
		rv = append(rv, aggregation.Fields()...)
	}
	return rv
}

type Calculator interface {
	Consume(*DocumentMatch)
	Finish()
	Merge(Calculator)
}

type MetricCalculator interface {
	Calculator
	Value() float64
}

type DurationCalculator interface {
	Calculator
	Duration() time.Duration
}

type BucketCalculator interface {
	Calculator
	Buckets() []*Bucket
}

type Bucket struct {
	name         string
	aggregations map[string]Calculator
	// calculators mirrors aggregations; iterated per hit because ranging
	// over a map is far slower than a slice
	calculators []Calculator
}

func NewBucket(name string, aggregations map[string]Aggregation) *Bucket {
	rv := &Bucket{
		name:         name,
		aggregations: make(map[string]Calculator, len(aggregations)),
		calculators:  make([]Calculator, 0, len(aggregations)),
	}
	for name, agg := range aggregations {
		calc := agg.Calculator()
		rv.aggregations[name] = calc
		rv.calculators = append(rv.calculators, calc)
	}
	return rv
}

func (b *Bucket) Merge(other *Bucket) {
	for otherAggName, otherCalculator := range other.aggregations {
		if thisCalculator, ok := b.aggregations[otherAggName]; ok {
			thisCalculator.Merge(otherCalculator)
		} else {
			b.aggregations[otherAggName] = otherCalculator
			b.calculators = append(b.calculators, otherCalculator)
		}
	}
}

func (b *Bucket) Name() string {
	return b.name
}

func (b *Bucket) Consume(d *DocumentMatch) {
	for _, aggCalc := range b.calculators {
		aggCalc.Consume(d)
	}
}

func (b *Bucket) Finish() {
	for _, aggCalc := range b.calculators {
		aggCalc.Finish()
	}
}

func (b *Bucket) Aggregations() map[string]Calculator {
	return b.aggregations
}

func (b *Bucket) Count() uint64 {
	if countAgg, ok := b.aggregations["count"]; ok {
		if countCalc, ok := countAgg.(MetricCalculator); ok {
			return uint64(countCalc.Value())
		}
	}
	return 0
}

func (b *Bucket) Duration() time.Duration {
	if durationAgg, ok := b.aggregations["duration"]; ok {
		if durationCalc, ok := durationAgg.(DurationCalculator); ok {
			return durationCalc.Duration()
		}
	}
	return 0
}

func (b *Bucket) Metric(name string) float64 {
	if agg, ok := b.aggregations[name]; ok {
		if calc, ok := agg.(MetricCalculator); ok {
			return calc.Value()
		}
	}
	return 0
}

func (b *Bucket) Buckets(name string) []*Bucket {
	if agg, ok := b.aggregations[name]; ok {
		if calc, ok := agg.(BucketCalculator); ok {
			return calc.Buckets()
		}
	}
	return nil
}

func (b *Bucket) Aggregation(name string) Calculator {
	return b.aggregations[name]
}
