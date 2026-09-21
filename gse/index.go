//  Copyright (c) 2026 The Riot Authors.
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
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	gogse "github.com/go-ego/gse"

	riot "github.com/vcaesar/riot"
	"github.com/vcaesar/riot/analysis"
	"github.com/vcaesar/riot/gse/query"
	"github.com/vcaesar/riot/search/highlight"
)

const (
	defaultField = "text"
	defaultSize  = 10
)

// Index is a riot writer with gse (or analysis/lang) analyzers attached,
// in the spirit of gse-bleve: New, Index, Search, Close.
type Index struct {
	writer *riot.Writer
	// seg is nil when Option.Lang selected a non-gse analyzer.
	seg           *gogse.Segmenter
	indexAnalyzer *analysis.Analyzer
	queryAnalyzer *analysis.Analyzer
	field         string
}

// New opens the index at opt.Index (in-memory when empty). With opt.Lang set
// the matching analysis/lang analyzer is used for both indexing and queries;
// otherwise the gse dictionaries are loaded, documents are cut with opt.Opt
// and queries with its non-search counterpart ("search-hmm" -> "hmm").
func New(opt Option) (*Index, error) {
	var seg *gogse.Segmenter
	queryOpt := opt
	if opt.Lang == "" {
		var err error
		if seg, err = NewSegmenter(opt); err != nil {
			return nil, err
		}
		queryOpt.Opt = queryMode(opt.Opt)
	}
	indexAnalyzer, err := NewAnalyzer(seg, opt)
	if err != nil {
		return nil, err
	}
	queryAnalyzer, err := NewAnalyzer(seg, queryOpt)
	if err != nil {
		return nil, err
	}

	config := riot.InMemoryOnlyConfig()
	if opt.Index != "" {
		config = riot.DefaultConfig(opt.Index)
	}
	writer, err := riot.OpenWriter(config)
	if err != nil {
		return nil, fmt.Errorf("error opening index %q: %v", opt.Index, err)
	}
	field := opt.Field
	if field == "" {
		field = defaultField
	}
	return &Index{
		writer:        writer,
		seg:           seg,
		indexAnalyzer: indexAnalyzer,
		queryAnalyzer: queryAnalyzer,
		field:         field,
	}, nil
}

// queryMode maps an index cut mode to the query cut mode without sub-word
// expansion: "search" -> "", "search-hmm" -> "hmm", "search-dag" -> "dag".
func queryMode(opt string) string {
	if opt == modeSearch {
		return ""
	}
	return strings.TrimPrefix(opt, modeSearch+"-")
}

// Segmenter returns the loaded gse segmenter, e.g. to add user words.
// It is nil when the index was opened with Option.Lang.
func (x *Index) Segmenter() *gogse.Segmenter { return x.seg }

// Writer returns the underlying riot writer for batch or custom documents.
func (x *Index) Writer() *riot.Writer { return x.writer }

// Field builds a stored, highlightable text field cut by the index analyzer;
// use it to add extra gse fields to documents written through Writer.
func (x *Index) Field(name, text string) *riot.TermField {
	return riot.NewTextField(name, text).
		WithAnalyzer(x.indexAnalyzer).
		StoreValue().
		Sortable().
		HighlightMatches()
}

func (x *Index) document(id string, data any) (*riot.Document, error) {
	fields, err := documentFields(x.field, data)
	if err != nil {
		return nil, fmt.Errorf("error indexing %q: %v", id, err)
	}
	doc := riot.NewDocument(id)
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		doc.AddField(x.Field(name, strings.Join(fields[name], "\n")))
	}
	return doc, nil
}

// Index writes a string or a struct (or non-nil pointer to a struct) under id,
// replacing any existing document with that id. Strings use Option.Field.
// Structs follow encoding/json field names and tags, including "-" and omitempty.
// Nested fields use dotted names; arrays are joined with newlines; nulls are skipped.
// Mapped names must be nonempty, without dots or a leading underscore.
// Scalar values
// are stored and analyzed as text, not as numeric or date range fields.
// Use Request.Field to search a mapped field; the default search field is unchanged.
func (x *Index) Index(id string, data any) error {
	doc, err := x.document(id, data)
	if err != nil {
		return err
	}
	if err := x.writer.Update(doc.ID(), doc); err != nil {
		return fmt.Errorf("error indexing %q: %v", id, err)
	}
	return nil
}

// Delete removes the document with id.
func (x *Index) Delete(id string) error {
	if err := x.writer.Delete(riot.Identifier(id)); err != nil {
		return fmt.Errorf("error deleting %q: %v", id, err)
	}
	return nil
}

// Close closes the underlying writer.
func (x *Index) Close() error { return x.writer.Close() }

// Query creates a fluent search builder. See package query for clause semantics.
func Query() *query.Builder { return query.Query() }

// Request is an analyzed text search; build it with QueryString.
type Request struct {
	Query string
	// Field to match; empty uses the field configured in Option.
	Field string
	// Size is the number of hits to return (default 10) and From the number
	// of hits to skip.
	Size, From int
	// Highlight adds HTML <mark> fragments of the stored field to every hit.
	Highlight bool
}

// QueryString creates an analyzed text request against the index field.
// It does not parse Elasticsearch query-string syntax.
// Pass true to also return highlighted fragments.
func QueryString(text string, enableHighlight ...bool) *Request {
	return &Request{Query: text, Size: defaultSize, Highlight: len(enableHighlight) > 0 && enableHighlight[0]}
}

// NewQueryString creates an analyzed text request.
//
// Deprecated: use QueryString instead.
func NewQueryString(text string, enableHighlight ...bool) *Request {
	return QueryString(text, enableHighlight...)
}

// SearchRequest is implemented by Request and query.Builder.
type SearchRequest interface {
	Build(string, *analysis.Analyzer) (*riot.TopNSearch, error)
	HighlightEnabled() bool
}

// Build compiles a legacy request using the index's field and query analyzer.
func (r *Request) Build(field string, analyzer *analysis.Analyzer) (*riot.TopNSearch, error) {
	if r == nil {
		return nil, fmt.Errorf("error building search: nil request")
	}
	return Query().Match(r.Query).Field(r.Field).From(r.From).Size(r.Size).
		Highlight(r.Highlight).Build(field, analyzer)
}

// HighlightEnabled reports whether the request includes highlighted fragments.
func (r *Request) HighlightEnabled() bool { return r != nil && r.Highlight }

// Hit is one matched document.
type Hit struct {
	ID    string
	Score float64
	// Fields holds the stored field values of the document.
	Fields map[string]string
	// Fragments holds highlighted snippets per field when requested.
	Fragments map[string][]string
}

// Result is the outcome of Index.Search.
type Result struct {
	Total    uint64
	MaxScore float64
	Took     time.Duration
	Hits     []*Hit
}

// Search runs req against the current index snapshot.
func (x *Index) Search(req SearchRequest) (res *Result, err error) {
	if req == nil {
		return nil, fmt.Errorf("error executing search: nil request")
	}
	search, err := req.Build(x.field, x.queryAnalyzer)
	if err != nil {
		return nil, err
	}
	search.WithStandardAggregations()

	reader, err := x.writer.Reader()
	if err != nil {
		return nil, fmt.Errorf("error getting index reader: %v", err)
	}
	defer func() {
		if closeErr := reader.Close(); closeErr != nil && err == nil {
			res, err = nil, fmt.Errorf("error closing index reader: %v", closeErr)
		}
	}()

	it, err := reader.Search(context.Background(), search)
	if err != nil {
		return nil, fmt.Errorf("error executing search: %v", err)
	}
	res = &Result{}
	var highlighter *highlight.SimpleHighlighter
	if req.HighlightEnabled() {
		highlighter = highlight.NewHTMLHighlighter()
	}
	for {
		match, nextErr := it.Next()
		if nextErr != nil {
			return nil, fmt.Errorf("error iterating search results: %v", nextErr)
		}
		if match == nil {
			break
		}
		hit := &Hit{Score: match.Score, Fields: map[string]string{}}
		err = match.VisitStoredFields(func(name string, value []byte) bool {
			if name == "_id" {
				hit.ID = string(value)
				return true
			}
			hit.Fields[name] = string(value)
			if locs := match.Locations[name]; highlighter != nil && len(locs) > 0 {
				if hit.Fragments == nil {
					hit.Fragments = map[string][]string{}
				}
				hit.Fragments[name] = highlighter.BestFragments(locs, value, 1)
			}
			return true
		})
		if err != nil {
			return nil, fmt.Errorf("error loading stored fields: %v", err)
		}
		res.Hits = append(res.Hits, hit)
	}
	aggs := it.Aggregations()
	res.Total = aggs.Count()
	res.MaxScore = aggs.Metric("max_score")
	res.Took = aggs.Duration()
	return res, nil
}
