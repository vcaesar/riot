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

package gse

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"

	riot "github.com/vcaesar/riot"
	"github.com/vcaesar/riot/analysis"
	"github.com/vcaesar/riot/search"
)

func TestEmbeddedStopWordsCov(t *testing.T) {
	// a tiny file dictionary keeps the test light
	dict := filepath.Join(t.TempDir(), "dict.txt")
	if err := os.WriteFile(dict, []byte("你好 100 n\n世界 100 n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newAnalyzer(t, Option{Dicts: dict, Stop: embedPrefix + "世界"})
	if got, want := terms(a, "你好世界"), []string{"你好"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("terms %v, want %v", got, want)
	}
}

func TestNewOpenWriterErrorCov(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if x, err := New(Option{Lang: "en", Index: file}); err == nil {
		_ = x.Close()
		t.Fatal("expected error opening an index over a file")
	}
}

func TestClosedIndexErrorsCov(t *testing.T) {
	x, err := New(Option{Lang: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if err := x.Close(); err != nil {
		t.Fatal(err)
	}
	if err := x.Index("a", "hello"); err == nil {
		t.Error("Index: expected error")
	}
	if err := x.Delete("a"); err == nil {
		t.Error("Delete: expected error")
	}
	b := x.Batch()
	b.Delete("a")
	if err := b.Commit(); err == nil {
		t.Error("Commit: expected error")
	}
}

// customRequest searches q directly.
type customRequest struct{ q riot.Query }

func (r customRequest) Build(string, *analysis.Analyzer) (*riot.TopNSearch, error) {
	return riot.NewTopNSearch(defaultSize, r.q), nil
}

func (customRequest) HighlightEnabled() bool { return false }

var errCov = errors.New("cov failure")

// failingQuery matches every document; with storedErr its matches fail to
// load stored fields, otherwise building the searcher fails.
type failingQuery struct{ storedErr bool }

func (q failingQuery) Searcher(i search.Reader, options search.SearcherOptions) (search.Searcher, error) {
	if !q.storedErr {
		return nil, errCov
	}
	return riot.NewMatchAllQuery().Searcher(storedFieldsErrReader{i}, options)
}

type storedFieldsErrReader struct{ search.Reader }

func (storedFieldsErrReader) VisitStoredFields(uint64, segment.StoredFieldVisitor) error {
	return errCov
}

func TestSearchErrorsCov(t *testing.T) {
	x := openIndex(t, Option{Lang: "en"})
	if err := x.Index("a", "hello"); err != nil {
		t.Fatal(err)
	}
	for _, storedErr := range []bool{false, true} {
		if _, err := x.Search(customRequest{failingQuery{storedErr: storedErr}}); err == nil {
			t.Errorf("stored fields error %v: expected search error", storedErr)
		}
	}
}
