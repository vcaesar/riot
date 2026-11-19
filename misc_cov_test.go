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

package riot

import (
	"context"
	"errors"
	"reflect"
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"

	"github.com/vcaesar/riot/index"
	"github.com/vcaesar/riot/numeric"
	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/similarity"
)

func TestConfigBuildersCov(t *testing.T) {
	config := InMemoryOnlyConfig().
		WithSegmentType("ice").
		WithStoredChunkCacheSize(4).
		DisableOptimizeConjunctionUnadorned()
	config.PerFieldSimilarity["name"] = similarity.NewBM25Similarity()
	w, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := w.Insert(NewDocument("a").AddField(NewTextField("name", "hello world"))); err != nil {
		t.Fatal(err)
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	}()
	// the per-field similarity also scores the field
	n, err := covSearch(context.Background(), r, NewTermQuery("hello").SetField("name"))
	if err != nil || n != 1 {
		t.Fatalf("got %d matches, err %v", n, err)
	}
}

func TestDocumentCov(t *testing.T) {
	doc := NewDocumentWithIdentifier(Identifier("x"))
	if got := string(doc.ID().Term()); got != "x" {
		t.Fatalf("ID %q, want x", got)
	}
	empty := Document{}
	if doc.Size() <= empty.Size() {
		t.Fatalf("Size %d not above empty %d", doc.Size(), empty.Size())
	}
}

func TestFieldsCov(t *testing.T) {
	f := NewKeywordFieldBytes("k", []byte("v")).SetPositionIncrementGap(7)
	if f.PositionIncrementGap() != 7 || f.Size() <= len("k") {
		t.Fatalf("gap %d size %d", f.PositionIncrementGap(), f.Size())
	}
	text := NewTextFieldBytes("t", []byte("a b"))
	text.Analyze(0)
	if got := text.AnalyzedLength(); got != 2 {
		t.Fatalf("text bytes field length %d, want 2", got)
	}

	composite := NewCompositeFieldIncluding("c", []string{"a"})
	excluding := NewCompositeField("e", true, []string{"a"}, []string{"b"})
	doc := NewDocument("x").
		AddField(NewTextField("a", "hi there")).
		AddField(NewTextField("b", "skipped")).
		AddField(composite).
		AddField(excluding)
	doc.Analyze()
	terms := map[string]bool{}
	composite.EachTerm(func(term segment.FieldTerm) { terms[string(term.Term())] = true })
	if !reflect.DeepEqual(terms, map[string]bool{"hi": true, "there": true}) {
		t.Fatalf("composite terms %v", terms)
	}
	embedded := 0
	composite.TermField.EachTerm(func(segment.FieldTerm) { embedded++ })
	if embedded != len(terms) {
		t.Fatalf("embedded TermField visited %d terms, want %d", embedded, len(terms))
	}
	if excluding.includesField("b") || !excluding.includesField("a") {
		t.Fatal("composite field inclusion")
	}
	if composite.PositionIncrementGap() != 0 || excluding.Size() <= composite.TermField.Size() {
		t.Fatalf("composite gap %d size %d", composite.PositionIncrementGap(), excluding.Size())
	}
}

func TestDecodersCov(t *testing.T) {
	dt := NewDateTimeField("d", covDate)
	got, err := DecodeDateTime(dt.Value())
	if err != nil || !got.Equal(covDate) {
		t.Fatalf("DecodeDateTime %v, %v", got, err)
	}
	lon, lat, err := DecodeGeoLonLat(NewGeoPointField("g", 10, 20).Value())
	if err != nil || lon < 9.99 || lon > 10.01 || lat < 19.99 || lat > 20.01 {
		t.Fatalf("DecodeGeoLonLat %v, %v, %v", lon, lat, err)
	}
	if _, err := DecodeDateTime(nil); err == nil {
		t.Fatal("DecodeDateTime: expected error")
	}
	if _, _, err := DecodeGeoLonLat(nil); err == nil {
		t.Fatal("DecodeGeoLonLat: expected error")
	}
	// a value that is not prefix coded keeps only its own term
	tfs, count := numberAnalyzer.terms([]byte("x"))
	if count != 1 || len(tfs) != 1 {
		t.Fatalf("terms %v, count %d", tfs, count)
	}
	if _, err := numeric.PrefixCoded(nil).Int64(); err == nil {
		t.Fatal("expected prefix coded error")
	}
}

func TestTopNSearchCov(t *testing.T) {
	r := covIndex(t)
	q := NewMatchAllQuery()
	req := NewTopNSearch(5, q).SetFrom(0)
	if req.Query() != q || req.Options() != (SearchOptions{}) || req.Size() != 5 || req.From() != 0 ||
		len(req.SortOrder()) != 1 {
		t.Fatalf("accessors of %+v", req)
	}
	ids := func(req SearchRequest) []string {
		t.Helper()
		it, err := r.Search(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		var rv []string
		for {
			dm, err := it.Next()
			if err != nil {
				t.Fatal(err)
			}
			if dm == nil {
				return rv
			}
			err = dm.VisitStoredFields(func(field string, value []byte) bool {
				if field == _idField {
					rv = append(rv, string(value))
				}
				return true
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	after := NewTopNSearch(5, q).SortBy([]string{_idField}).After([][]byte{[]byte("a")})
	if got := ids(after); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("after a: %v", got)
	}
	before := NewTopNSearch(5, q).SortBy([]string{_idField}).Before([][]byte{[]byte("b")})
	if got := ids(before); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("before b: %v", got)
	}
	all := NewAllMatches(q).WithStandardAggregations()
	if got := ids(all); len(got) != 2 || len(all.Aggregations()) != len(standardAggs) {
		t.Fatalf("all matches %v, aggregations %v", got, all.Aggregations())
	}
	s, err := req.AllMatches(r.reader, r.config)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	config := InMemoryOnlyConfig()
	sim := similarity.NewBM25Similarity()
	config.PerFieldSimilarity["name"] = sim
	opts := searchOptionsFromConfig(config, SearchOptions{})
	if opts.SimilarityForField("name") != sim || opts.SimilarityForField("other") != config.DefaultSimilarity {
		t.Fatal("per-field similarity lookup")
	}
}

func TestReaderCov(t *testing.T) {
	r := covIndex(t)
	var ended uint64
	r.config = r.config.WithSearchStartFunc(func(uint64) error { return nil })
	r.config.SearchEndFunc = func(size uint64) { ended = size }
	if n, err := covSearch(context.Background(), r, NewMatchAllQuery()); err != nil || n != 2 {
		t.Fatalf("got %d matches, err %v", n, err)
	}
	if ended == 0 {
		t.Fatal("SearchEndFunc not called")
	}
	values := map[uint64]int64{}
	err := r.VisitInt64("n", func(doc uint64, v int64) error {
		values[doc] = v
		return nil
	})
	if err != nil || len(values) != 2 {
		t.Fatalf("VisitInt64 %v, %v", values, err)
	}
}

func TestMultiSearchCov(t *testing.T) {
	r := covIndex(t)
	if _, err := MultiSearch(context.Background(), NewTopNSearch(1, NewRegexpQuery("[")), r); err == nil {
		t.Fatal("bad query: expected error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := MultiSearch(ctx, NewTopNSearch(1, NewMatchAllQuery()), r); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: err %v", err)
	}
	nextErr := errors.New("next")
	msl := NewMultiSearcherList([]search.Searcher{&errSearcher{nextErr: nextErr}})
	sctx := search.NewSearchContext(1, 0)
	for i := range 2 {
		if _, err := msl.Next(sctx); !errors.Is(err, nextErr) {
			t.Fatalf("call %d: err %v", i, err)
		}
	}
}

// failingDirectory fails Setup or Persist when set.
type failingDirectory struct {
	index.Directory
	setup, persist bool
}

var errDirectoryCov = errors.New("directory failure")

func (d *failingDirectory) Setup(readOnly bool) error {
	if d.setup {
		return errDirectoryCov
	}
	return d.Directory.Setup(readOnly)
}

func (d *failingDirectory) Persist(kind string, id uint64, w index.WriterTo, closeCh chan struct{}) error {
	if d.persist {
		return errDirectoryCov
	}
	return d.Directory.Persist(kind, id, w, closeCh)
}

func failingConfig(t *testing.T, setup, persist bool) Config {
	path := t.TempDir()
	return DefaultConfigWithDirectory(func() index.Directory {
		return &failingDirectory{Directory: index.NewFileSystemDirectory(path), setup: setup, persist: persist}
	})
}

func TestOpenErrorsCov(t *testing.T) {
	config := failingConfig(t, true, false)
	if _, err := OpenReader(config); !errors.Is(err, errDirectoryCov) {
		t.Fatalf("OpenReader: err %v", err)
	}
	if _, err := OpenOfflineWriter(config, 1, 1); !errors.Is(err, errDirectoryCov) {
		t.Fatalf("OpenOfflineWriter: err %v", err)
	}
}

func TestOfflineWriterBatchErrorsCov(t *testing.T) {
	tests := []struct {
		name      string
		batchSize int
		insertErr bool
	}{
		{"insert", 0, true},
		{"close", 10, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, err := OpenOfflineWriter(failingConfig(t, false, true), tt.batchSize, 1)
			if err != nil {
				t.Fatal(err)
			}
			// a failed Batch leaves the index writer open
			defer func() {
				if err := w.writer.Close(); err != nil {
					t.Error(err)
				}
			}()
			err = w.Insert(NewDocument("a").AddField(NewKeywordField("k", "v")))
			if gotErr := err != nil; gotErr != tt.insertErr {
				t.Fatalf("Insert: err %v", err)
			}
			if !tt.insertErr {
				if err := w.Close(); err == nil {
					t.Fatalf("Close: err %v", err)
				}
			}
		})
	}
}
