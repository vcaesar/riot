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

package searcher

import (
	"errors"
	"slices"
	"testing"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/similarity"
)

// asSearcher keeps a nil *covSearcher a nil search.Searcher.
func asSearcher(c *covSearcher) search.Searcher {
	if c == nil {
		return nil
	}
	return c
}

func newCovBoolean(t *testing.T, must, should, mustNot *covSearcher) *BooleanSearcher {
	t.Helper()
	b, err := NewBooleanSearcher(asSearcher(must), asSearcher(should), asSearcher(mustNot),
		similarity.NewCompositeSumScorer(), noExplain())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func docs(n ...uint64) []uint64 { return n }

func TestBooleanCovErrors(t *testing.T) {
	tests := []struct {
		name                  string
		must, should, mustNot *covSearcher
		advance               uint64 // Advance to it after a first Next, if not 0
		pre                   bool   // Advance before any Next
	}{
		{name: "init must", must: &covSearcher{nextErr: errCov, nextErrAt: 1}},
		{name: "init should", must: &covSearcher{docs: docs(1)}, should: &covSearcher{nextErr: errCov, nextErrAt: 1}},
		{name: "must next", must: &covSearcher{docs: docs(1, 2), nextErr: errCov, nextErrAt: 2}},
		{name: "should next", should: &covSearcher{docs: docs(1, 2), nextErr: errCov, nextErrAt: 2}},
		{name: "must not advance", must: &covSearcher{docs: docs(5)},
			mustNot: &covSearcher{docs: docs(1), advErr: errCov}},
		{name: "excluded by advanced must not", must: &covSearcher{docs: docs(5, 6), nextErr: errCov, nextErrAt: 2},
			mustNot: &covSearcher{docs: docs(1, 5)}},
		{name: "excluded by must not", must: &covSearcher{docs: docs(1, 2), nextErr: errCov, nextErrAt: 2},
			mustNot: &covSearcher{docs: docs(1)}},
		{name: "should advance", must: &covSearcher{docs: docs(5)},
			should: &covSearcher{docs: docs(1), advErr: errCov}},
		{name: "should advanced on", must: &covSearcher{docs: docs(5, 6), nextErr: errCov, nextErrAt: 2},
			should: &covSearcher{docs: docs(1, 5)}},
		{name: "should advanced past", must: &covSearcher{docs: docs(5, 6), nextErr: errCov, nextErrAt: 2},
			should: &covSearcher{docs: docs(1, 7)}},
		{name: "should required", must: &covSearcher{docs: docs(1, 2), nextErr: errCov, nextErrAt: 2},
			should: &covSearcher{docs: docs(3), min: 1}},
		{name: "advance init", must: &covSearcher{nextErr: errCov, nextErrAt: 1}, pre: true},
		{name: "advance must", must: &covSearcher{docs: docs(1, 5), advErr: errCov}, advance: 6},
		{name: "advance should", must: &covSearcher{docs: docs(1, 5)},
			should: &covSearcher{docs: docs(1, 5), advErr: errCov}, advance: 6},
		{name: "advance must not", must: &covSearcher{docs: docs(1, 5)},
			mustNot: &covSearcher{docs: docs(2, 6), advErr: errCov}, advance: 6},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := newCovBoolean(t, tc.must, tc.should, tc.mustNot)
			ctx := covCtx()
			var err error
			switch {
			case tc.pre:
				_, err = b.Advance(ctx, 1)
			case tc.advance > 0:
				if _, err = b.Next(ctx); err != nil {
					t.Fatal(err)
				}
				_, err = b.Advance(ctx, tc.advance)
			default:
				_, err = b.Next(ctx)
			}
			if !errors.Is(err, errCov) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

// A failed init is retried by the next call, which recycles the matches
// the first one read.
func TestBooleanCovReinit(t *testing.T) {
	b := newCovBoolean(t, &covSearcher{docs: docs(1)}, &covSearcher{docs: docs(1)},
		&covSearcher{nextErr: errCov, nextErrAt: 1, withMatch: true})
	ctx := covCtx()
	for range 2 {
		if _, err := b.Next(ctx); !errors.Is(err, errCov) {
			t.Fatalf("got %v", err)
		}
	}
}

func TestBooleanCovResults(t *testing.T) {
	tests := []struct {
		name                  string
		must, should, mustNot *covSearcher
		advance               uint64
		want                  []uint64
	}{
		{name: "should only, excluded", should: &covSearcher{docs: docs(1, 2)}, mustNot: &covSearcher{docs: docs(1)},
			want: docs(2)},
		{name: "advance should only", should: &covSearcher{docs: docs(1, 5, 6)}, advance: 6, want: docs(6)},
		{name: "advance must past end", must: &covSearcher{docs: docs(1)}, advance: 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := newCovBoolean(t, tc.must, tc.should, tc.mustNot)
			ctx := covCtx()
			var got []uint64
			if tc.advance > 0 {
				dm, err := b.Next(ctx)
				if err != nil || dm == nil {
					t.Fatalf("first: %v %v", dm, err)
				}
				if dm, err = b.Advance(ctx, tc.advance); err != nil {
					t.Fatal(err)
				}
				if dm != nil {
					got = append(got, dm.Number)
				}
			}
			rest, err := covDrain(ctx, b)
			if err != nil {
				t.Fatal(err)
			}
			if got = append(got, rest...); !slices.Equal(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
			if dm, err := b.Next(ctx); dm != nil || err != nil {
				t.Fatalf("next after done: %v %v", dm, err)
			}
			if dm, err := b.Advance(ctx, 100); dm != nil || err != nil {
				t.Fatalf("advance after done: %v %v", dm, err)
			}
		})
	}
}

func TestBooleanCovMisc(t *testing.T) {
	b := newCovBoolean(t, &covSearcher{docs: docs(1, 2)}, &covSearcher{docs: docs(1)}, &covSearcher{docs: docs(5)})
	if b.Min() != 0 {
		t.Fatal("min")
	}
	if _, err := b.Next(covCtx()); err != nil {
		t.Fatal(err)
	}
	if b.Size() <= reflectStaticSizeBooleanSearcher+3 {
		t.Fatalf("size %d", b.Size())
	}
	for i, tc := range [][3]error{{errCov, nil, nil}, {nil, errCov, nil}, {nil, nil, errCov}} {
		b := newCovBoolean(t, &covSearcher{closeErr: tc[0]}, &covSearcher{closeErr: tc[1]}, &covSearcher{closeErr: tc[2]})
		if err := b.Close(); !errors.Is(err, errCov) {
			t.Fatalf("close %d: %v", i, err)
		}
	}
}
