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

package mergeplan

import (
	"strings"
	"testing"
)

func TestPlanNoRosterFits(t *testing.T) {
	o := &Options{
		MaxSegmentSize:       100,
		SegmentsPerMergeTask: 0,
		CalcBudget: func(int64, int64, *Options) int {
			return 0
		},
	}
	plan, err := Plan(makeLinearSegments(5), o)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan == nil || len(plan.Tasks) != 1 {
		t.Fatalf("expected only the empty-segment task, got %+v", plan)
	}
}

func TestScoreSegmentsZeroSize(t *testing.T) {
	tests := []struct {
		name string
		segs []Segment
	}{
		{"zero full", []Segment{&segment{MyID: 1}}},
		{"zero live", []Segment{&segment{MyID: 1, MyFullSize: 10}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ScoreSegments(tt.segs, &Options{}); got != 0 {
				t.Fatalf("expected score 0, got %v", got)
			}
		})
	}
}

func TestToBarChart(t *testing.T) {
	segs := []Segment{
		&segment{MyID: 1, MyFullSize: 40, MyLiveSize: 20},
		&segment{MyID: 2, MyFullSize: 10, MyLiveSize: 10},
		&segment{MyID: 3, MyFullSize: 5, MyLiveSize: 5},
	}
	plan := &MergePlan{Tasks: []*MergeTask{
		{Segments: []Segment{segs[2]}},
		{Segments: []Segment{segs[1]}},
	}}
	tests := []struct {
		name   string
		barMax int
		plan   *MergePlan
		want   []string
	}{
		{"scaled with plan", 20, plan, []string{"p     1:    20 /   40 -   ..........xxxxxxxxxx", "* 11111", "* 00"}},
		{"unscaled no plan", 100, nil, []string{"p     2:    10 /   10 -   ..........", "p     3:     5 /    5 -   ....."}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToBarChart("p", tt.barMax, segs, tt.plan)
			if lines := strings.Split(got, "\n"); len(lines) != len(segs) {
				t.Fatalf("expected %d lines, got %q", len(segs), got)
			}
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("chart %q missing %q", got, w)
				}
			}
		})
	}
}

func TestValidateMergePlannerOptionsOK(t *testing.T) {
	if err := ValidateMergePlannerOptions(&DefaultMergePlanOptions); err != nil {
		t.Fatalf("expected default options valid, got %v", err)
	}
}
