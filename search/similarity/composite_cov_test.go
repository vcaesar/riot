// Copyright (c) 2026 The AtomAI.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package similarity

import (
	"testing"

	"github.com/vcaesar/riot/search"
)

func constituents() []*search.DocumentMatch {
	return []*search.DocumentMatch{
		{Score: 1.5, Explanation: search.NewExplanation(1.5, "a")},
		{Score: 2.5, Explanation: search.NewExplanation(2.5, "b")},
	}
}

func TestCompositeSumScorer(t *testing.T) {
	for _, test := range []struct {
		name      string
		scorer    *CompositeSumScorer
		boost     float64
		score     float64
		msg       string
		nChildren int
	}{
		{name: "default", scorer: NewCompositeSumScorer(), boost: 1, score: 4, msg: "sum of:", nChildren: 2},
		{name: "boost one", scorer: NewCompositeSumScorerWithBoost(1), boost: 1, score: 4, msg: "sum of:", nChildren: 2},
		{name: "boost two", scorer: NewCompositeSumScorerWithBoost(2), boost: 2, score: 8, msg: "computed as boost * sum", nChildren: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.scorer.Boost(); got != test.boost {
				t.Errorf("Boost() = %v, want %v", got, test.boost)
			}
			if got := test.scorer.ScoreComposite(constituents()); got != test.score {
				t.Errorf("ScoreComposite() = %v, want %v", got, test.score)
			}
			exp := test.scorer.ExplainComposite(constituents())
			if exp.Value != test.score || exp.Message != test.msg || len(exp.Children) != test.nChildren {
				t.Errorf("ExplainComposite() = %v %q %d children, want %v %q %d",
					exp.Value, exp.Message, len(exp.Children), test.score, test.msg, test.nChildren)
			}
			if test.boost != 1 {
				if exp.Children[0].Value != test.boost || exp.Children[1].Value != 4 || len(exp.Children[1].Children) != 2 {
					t.Errorf("boosted explanation children = %+v", exp.Children)
				}
			}
		})
	}
	if got := NewCompositeSumScorer().ScoreComposite(nil); got != 0 {
		t.Errorf("ScoreComposite(nil) = %v, want 0", got)
	}
}

func TestConstantScorer(t *testing.T) {
	c := ConstantScorer(3.5)
	if got := c.Score(10, 2); got != 3.5 {
		t.Errorf("Score() = %v", got)
	}
	if got := c.ScoreComposite(constituents()); got != 3.5 {
		t.Errorf("ScoreComposite() = %v", got)
	}
	for name, exp := range map[string]*search.Explanation{
		"Explain":          c.Explain(1, 1),
		"ExplainComposite": c.ExplainComposite(nil),
	} {
		if exp.Value != 3.5 || exp.Message != "constant" {
			t.Errorf("%s() = %v %q", name, exp.Value, exp.Message)
		}
	}
}

func TestBM25AverageFieldLength(t *testing.T) {
	sim := NewBM25Similarity()
	if got := sim.AverageFieldLength(nil); got != 0 {
		t.Errorf("AverageFieldLength(nil) = %v, want 0", got)
	}
	if got := sim.AverageFieldLength(bm25Stats{docCount: 4, sumTF: 10}); got != 2.5 {
		t.Errorf("AverageFieldLength() = %v, want 2.5", got)
	}
}
