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

package searcher

import (
	"math"

	"github.com/vcaesar/riot/search/similarity"

	"github.com/vcaesar/riot/search"

	segment "github.com/vcaesar/bluge_segment_api"
)

var baseTestIndexReaderDirect *stubIndexReader
var baseTestIndexReader search.Reader

func init() {
	baseTestIndexReaderDirect = newStubIndexReader()
	for _, doc := range baseTestIndexDocs {
		baseTestIndexReaderDirect.add(doc)
	}
	baseTestIndexReader = baseTestIndexReaderDirect
}

var testSearchOptions = search.SearcherOptions{
	SimilarityForField: func(_ string) search.Similarity {
		return similarity.NewBM25Similarity()
	},
	Explain: true,
}

var baseTestIndexDocs = []segment.Document{
	// must have 4/4 beer
	&FakeDocument{
		NewFakeField("_id", "1", true, false, false, nil),
		NewFakeField("name", "marty", false, false, true, nil),
		NewFakeField("desc", "beer beer beer beer", false, true, true, nil),
		NewFakeField("street", "couchbase way", false, false, true, nil),
	},
	// must have 1/4 beer
	&FakeDocument{
		NewFakeField("_id", "2", true, false, false, nil),
		NewFakeField("name", "steve", false, false, true, nil),
		NewFakeField("desc", "angst beer couch database", false, true, true, nil),
		NewFakeField("street", "couchbase way", false, false, true, nil),
		NewFakeField("title", "mister", false, false, true, nil),
	},
	// must have 1/4 beer
	&FakeDocument{
		NewFakeField("_id", "3", true, false, false, nil),
		NewFakeField("name", "dustin", false, false, true, nil),
		NewFakeField("desc", "apple beer column dank", false, true, true, nil),
		NewFakeField("title", "mister", false, false, true, nil),
	},
	// must have 65/65 beer
	&FakeDocument{
		NewFakeField("_id", "4", true, false, false, nil),
		NewFakeField("name", "ravi", false, false, true, nil),
		NewFakeField("desc", "beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer beer", false, true, true, nil),
	},
	// must have 0/x beer
	&FakeDocument{
		NewFakeField("_id", "5", true, false, false, nil),
		NewFakeField("name", "bobert", false, false, true, nil),
		NewFakeField("desc", "water", false, true, true, nil),
		NewFakeField("title", "mister", false, false, true, nil),
	},
}

func scoresCloseEnough(a, b float64) bool {
	return math.Abs(a-b) < 0.001
}
