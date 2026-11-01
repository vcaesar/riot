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
	"strconv"
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

// rangeCountingReader records DocsMatchingRange calls and declines them,
// so the searcher falls back to its terms path.
type rangeCountingReader struct {
	search.Reader
	calls int
}

func (r *rangeCountingReader) DocsMatchingRange(string, segment.Automaton, []byte, []byte) (
	segment.PostingsIterator, error) {
	r.calls++
	return nil, nil
}

func TestTermPrefixSearcherRangeNeedsThreeTerms(t *testing.T) {
	stub := newStubIndexReader()
	for i, body := range []string{"pa qa", "pb qb", "pc", "pa"} {
		stub.add(&FakeDocument{
			NewFakeField("_id", strconv.Itoa(i), true, false, false, nil),
			NewFakeField("desc", body, false, false, false, nil),
		})
	}
	for _, tt := range []struct {
		prefix    string
		wantCalls int
		wantCount uint64
	}{
		{"pa", 0, 2},
		{"q", 0, 2},
		{"p", 1, 4},
	} {
		t.Run(tt.prefix, func(t *testing.T) {
			reader := &rangeCountingReader{Reader: stub}
			rv, err := NewTermPrefixSearcher(reader, tt.prefix, "desc", 1.0, nil,
				similarity.NewCompositeSumScorer(), testSearchOptions)
			if err != nil {
				t.Fatal(err)
			}
			defer rv.Close()
			if reader.calls != tt.wantCalls {
				t.Errorf("DocsMatchingRange calls = %d, want %d", reader.calls, tt.wantCalls)
			}
			if got := rv.Count(); got != tt.wantCount {
				t.Errorf("count = %d, want %d", got, tt.wantCount)
			}
		})
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
