//  Copyright (c) 2026 The Bluge Authors.
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
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/similarity"
)

// closeErrDictReader wraps the stub reader so the dictionary iterator's
// Close reports an error after a successful scan.
type closeErrDictReader struct {
	search.Reader
	closeErr error
}

type closeErrDictItr struct {
	segment.DictionaryIterator
	err error
}

func (i *closeErrDictItr) Close() error {
	_ = i.DictionaryIterator.Close()
	return i.err
}

func (r *closeErrDictReader) DictionaryIterator(field string, a segment.Automaton, start, end []byte) (
	segment.DictionaryIterator, error) {
	itr, err := r.Reader.DictionaryIterator(field, a, start, end)
	if err != nil {
		return nil, err
	}
	return &closeErrDictItr{DictionaryIterator: itr, err: r.closeErr}, nil
}

func TestTermPrefixSearcherReportsDictionaryCloseError(t *testing.T) {
	boom := errors.New("dictionary close failed")
	reader := &closeErrDictReader{Reader: baseTestIndexReader, closeErr: boom}
	rv, err := NewTermPrefixSearcher(reader, "be", "desc", 1.0, nil, similarity.NewCompositeSumScorer(), testSearchOptions)
	if !errors.Is(err, boom) {
		t.Fatalf("got err %v, want %v", err, boom)
	}
	if rv != nil {
		t.Fatal("expected nil searcher when the dictionary iterator fails to close")
	}
}

func TestTermPrefixSearcherHappyPath(t *testing.T) {
	rv, err := NewTermPrefixSearcher(baseTestIndexReader, "be", "desc", 1.0, nil, similarity.NewCompositeSumScorer(), testSearchOptions)
	if err != nil {
		t.Fatal(err)
	}
	defer rv.Close()
	// "beer" matches docs 1-4
	if got := rv.Count(); got != 4 {
		t.Fatalf("count = %d, want 4", got)
	}
}
