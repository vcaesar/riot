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

// bucketInline is how many aggregations a Bucket holds without allocating
// beyond itself; most searches have one or two
const bucketInline = 2

type Bucket struct {
	name string
	// names and calculators are parallel; lookups scan names, which beats
	// a map for the few aggregations a bucket has
	names       []string
	calculators []Calculator
	// scoreCalcs is set when every calculator only reads the score
	scoreCalcs []ScoreCalculator
	// aggregations is built by Aggregations on first use
	aggregations map[string]Calculator

	nameBuf  [bucketInline]string
	calcBuf  [bucketInline]Calculator
	scoreBuf [bucketInline]ScoreCalculator
}

func NewBucket(name string, aggregations map[string]Aggregation) *Bucket {
	rv := &Bucket{name: name}
	rv.names, rv.calculators = rv.nameBuf[:0], rv.calcBuf[:0]
	for name, agg := range aggregations {
		rv.names = append(rv.names, name)
		rv.calculators = append(rv.calculators, agg.Calculator())
	}
	rv.setScoreCalcs()
	return rv
}

func (b *Bucket) setScoreCalcs() {
	b.scoreCalcs = nil
	rv := b.scoreBuf[:0]
	for _, calc := range b.calculators {
		sc, ok := calc.(ScoreCalculator)
		if !ok || !sc.ScoreOnly() {
			return
		}
		rv = append(rv, sc)
	}
	b.scoreCalcs = rv
}

func (b *Bucket) lookup(name string) Calculator {
	for i, n := range b.names {
		if n == name {
			return b.calculators[i]
		}
	}
	return nil
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
	for i, otherAggName := range other.names {
		otherCalculator := other.calculators[i]
		if thisCalculator := b.lookup(otherAggName); thisCalculator != nil {
			thisCalculator.Merge(otherCalculator)
		} else {
			b.names = append(b.names, otherAggName)
			b.calculators = append(b.calculators, otherCalculator)
			if b.aggregations != nil {
				b.aggregations[otherAggName] = otherCalculator
			}
		}
	}
	b.setScoreCalcs()
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
	if b.aggregations == nil {
		b.aggregations = make(map[string]Calculator, len(b.names))
		for i, name := range b.names {
			b.aggregations[name] = b.calculators[i]
		}
	}
	return b.aggregations
}

func (b *Bucket) Count() uint64 {
	if countAgg := b.lookup("count"); countAgg != nil {
		if countCalc, ok := countAgg.(MetricCalculator); ok {
			return uint64(countCalc.Value())
		}
	}
	return 0
}

func (b *Bucket) Duration() time.Duration {
	if durationAgg := b.lookup("duration"); durationAgg != nil {
		if durationCalc, ok := durationAgg.(DurationCalculator); ok {
			return durationCalc.Duration()
		}
	}
	return 0
}

func (b *Bucket) Metric(name string) float64 {
	if agg := b.lookup(name); agg != nil {
		if calc, ok := agg.(MetricCalculator); ok {
			return calc.Value()
		}
	}
	return 0
}

func (b *Bucket) Buckets(name string) []*Bucket {
	if agg := b.lookup(name); agg != nil {
		if calc, ok := agg.(BucketCalculator); ok {
			return calc.Buckets()
		}
	}
	return nil
}

func (b *Bucket) Aggregation(name string) Calculator {
	return b.lookup(name)
}
