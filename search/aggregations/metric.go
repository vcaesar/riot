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

	"github.com/vcaesar/riot/search"
)

type singleValueOp uint8

const (
	opSum singleValueOp = iota
	opMin
	opMax
)

type SingleValueMetric struct {
	src  search.NumericValuesSource
	init float64
	op   singleValueOp
}

func Sum(src search.NumericValuesSource) *SingleValueMetric {
	return &SingleValueMetric{src: src, op: opSum}
}

func Min(src search.NumericValuesSource) *SingleValueMetric {
	return &SingleValueMetric{src: src, op: opMin, init: math.Inf(1)}
}

func Max(src search.NumericValuesSource) *SingleValueMetric {
	return MaxStartingAt(src, math.Inf(-1))
}

func MaxStartingAt(src search.NumericValuesSource, initial float64) *SingleValueMetric {
	return &SingleValueMetric{src: src, op: opMax, init: initial}
}

func (s *SingleValueMetric) Fields() []string {
	return s.src.Fields()
}

func (s *SingleValueMetric) Calculator() search.Calculator {
	rv := &SingleValueCalculator{
		val: s.init,
		src: s.src,
		op:  s.op,
	}
	// count and score are single valued and hot: decided once here so the
	// per-hit path is a branch rather than a type switch and a Numbers slice
	switch s.src.(type) {
	case *countingSource:
		rv.count = true
	case *search.ScoreSource:
		rv.score = true
	}
	return rv
}

type SingleValueCalculator struct {
	src          search.NumericValuesSource
	val          float64
	op           singleValueOp
	count, score bool
}

func (s *SingleValueCalculator) apply(val float64) {
	switch s.op {
	case opSum:
		s.val += val
	case opMin:
		if val < s.val {
			s.val = val
		}
	case opMax:
		if val > s.val {
			s.val = val
		}
	}
}

func (s *SingleValueCalculator) Consume(d *search.DocumentMatch) {
	switch {
	case s.count:
		s.apply(1)
	case s.score:
		s.apply(d.Score)
	default:
		for _, val := range s.src.Numbers(d) {
			s.apply(val)
		}
	}
}

func (s *SingleValueCalculator) Merge(other search.Calculator) {
	if other, ok := other.(*SingleValueCalculator); ok {
		s.apply(other.val)
	}
}

func (s *SingleValueCalculator) Finish() {}

func (s *SingleValueCalculator) Value() float64 {
	return s.val
}

type WeightedAvgMetric struct {
	src    search.NumericValuesSource
	weight search.NumericValuesSource
}

func Avg(src search.NumericValuesSource) *WeightedAvgMetric {
	return &WeightedAvgMetric{
		src: src,
	}
}

func WeightedAvg(src, weight search.NumericValuesSource) *WeightedAvgMetric {
	return &WeightedAvgMetric{
		src:    src,
		weight: weight,
	}
}

func (a *WeightedAvgMetric) Fields() []string {
	rv := a.src.Fields()
	if a.weight != nil {
		rv = append(rv, a.weight.Fields()...)
	}
	return rv
}

func (a *WeightedAvgMetric) Calculator() search.Calculator {
	rv := &WeightedAvgCalculator{
		src:    a.src,
		weight: a.weight,
	}
	return rv
}

type WeightedAvgCalculator struct {
	src     search.NumericValuesSource
	weight  search.NumericValuesSource
	val     float64
	weights float64
}

func (a *WeightedAvgCalculator) Value() float64 {
	return a.val / a.weights
}

func (a *WeightedAvgCalculator) Consume(d *search.DocumentMatch) {
	weight := 1.0
	if a.weight != nil {
		weightValues := a.weight.Numbers(d)
		if len(weightValues) > 0 {
			weight = weightValues[0]
		}
	}
	for _, val := range a.src.Numbers(d) {
		a.val += val * weight
		a.weights += weight
	}
}

func (a *WeightedAvgCalculator) Merge(other search.Calculator) {
	if other, ok := other.(*WeightedAvgCalculator); ok {
		a.val += other.val
		a.weights += other.weights
	}
}

func (a *WeightedAvgCalculator) Finish() {

}
