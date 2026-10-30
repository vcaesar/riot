//  Copyright (c) 2020 The Bluge Authors.
//  Copyright (c) 2026 The AtomAI.
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

// ScoreCalculator is a Calculator that can be fed only a hit's score, for
// hits that were never built into a DocumentMatch.
type ScoreCalculator interface {
	Calculator
	// ScoreOnly reports whether ConsumeScore is equivalent to Consume.
	ScoreOnly() bool
	// ConsumeScore consumes n hits that all scored score.
	ConsumeScore(score float64, n int)
	// BoundOnly reports whether ConsumeScore(bound, n) is exact for n hits
	// whose scores are only known to be at most bound, given a hit scoring
	// at least bound was consumed before.
	BoundOnly() bool
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
	// scoreCalcs is set when every calculator only reads the score
	scoreCalcs []ScoreCalculator
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
	rv.scoreCalcs = scoreCalculators(rv.calculators)
	return rv
}

func scoreCalculators(calcs []Calculator) []ScoreCalculator {
	rv := make([]ScoreCalculator, 0, len(calcs))
	for _, calc := range calcs {
		sc, ok := calc.(ScoreCalculator)
		if !ok || !sc.ScoreOnly() {
			return nil
		}
		rv = append(rv, sc)
	}
	return rv
}

// ScoreOnly reports whether every aggregation only reads the hit score, so
// ConsumeScore may stand in for Consume.
func (b *Bucket) ScoreOnly() bool {
	return b.scoreCalcs != nil
}

// BoundOnly reports whether every aggregation accepts hits known only by an
// upper bound on their score; see ScoreCalculator.BoundOnly.
func (b *Bucket) BoundOnly() bool {
	for _, calc := range b.scoreCalcs {
		if !calc.BoundOnly() {
			return false
		}
	}
	return b.scoreCalcs != nil
}

// ConsumeScore aggregates n hits known only by their shared score; see
// ScoreOnly.
func (b *Bucket) ConsumeScore(score float64, n int) {
	for _, calc := range b.scoreCalcs {
		calc.ConsumeScore(score, n)
	}
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
	b.scoreCalcs = scoreCalculators(b.calculators)
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
