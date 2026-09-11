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

package collector

import (
	"github.com/vcaesar/riot/search"
)

type stubSearcher struct {
	index   int
	matches []*search.DocumentMatch
}

func (ss *stubSearcher) Next(ctx *search.Context) (*search.DocumentMatch, error) {
	if ss.index < len(ss.matches) {
		rv := ctx.DocumentMatchPool.Get()
		rv.Number = ss.matches[ss.index].Number
		rv.Score = ss.matches[ss.index].Score
		ss.index++
		return rv, nil
	}
	return nil, nil
}

func (ss *stubSearcher) DocumentMatchPoolSize() int {
	return 0
}

func (ss *stubSearcher) Close() error {
	return nil
}
