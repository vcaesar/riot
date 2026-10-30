//  Copyright (c) 2020 Couchbase, Inc.
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
	"context"
)

type Collector interface {
	Collect(context.Context, Aggregations, Collectible) (DocumentMatchIterator, error)

	Size() int
	BackingSize() int
}

// ScoreSkipper is implemented by searchers that can score a hit without
// building a DocumentMatch, so a top-N collector only materializes hits
// that can still enter its results.
type ScoreSkipper interface {
	// NextAbove returns the next hit whose score sorts above floor (as
	// numeric.Float64ToInt64 orders scores). Hits at or below floor are
	// passed to skipped instead, in index order, as n consecutive hits
	// sharing score; returning false from skipped stops the search and
	// NextAbove returns nil. When ctx.SkipBlocks is set, skipped hits may
	// be reported with floor, an upper bound, instead of their own score.
	NextAbove(ctx *Context, floor float64, skipped func(score float64, n int) bool) (*DocumentMatch, error)
}

type Collectible interface {
	Next(ctx *Context) (*DocumentMatch, error)
	DocumentMatchPoolSize() int
	Close() error
}

type DocumentMatchIterator interface {
	Next() (*DocumentMatch, error)
	Aggregations() *Bucket
}
