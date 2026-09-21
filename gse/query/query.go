// Copyright (c) 2026 The Bluge Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package query provides an immutable fluent builder for riot search requests.
// Expressions are left associative: Match("a").Or().Match("b").Match("c")
// means (a OR b) AND c, without operator precedence.
package query

import (
	"fmt"
	"slices"

	"github.com/vcaesar/riot"
	"github.com/vcaesar/riot/analysis"
	"github.com/vcaesar/riot/search"
)

type clause struct {
	text, field string
	allDocs     bool
	allTerms    bool
	or          bool
}

type sortField struct {
	name      string
	ascending bool
}

// Builder stores a query description. Its zero value is ready to use.
// Fluent methods return copies; callers must retain the returned pointer.
// Build creates independent requests, but shares the supplied analyzer.
// Invalid clause operations are retained as errors until Build.
type Builder struct {
	clauses   []clause
	sorts     []sortField
	from      int
	size      int
	sizeSet   bool
	nextOr    bool
	highlight bool
	err       error
}

// Query returns an empty builder (all documents, size 10, offset 0,
// score descending, highlighting disabled).
func Query() *Builder { return &Builder{} }

// Match adds a clause matching any analyzed term in text.
func (b *Builder) Match(text string) *Builder {
	return b.add(clause{text: text})
}

// MatchAll with no arguments adds a clause matching all documents.
// With one argument it requires all analyzed terms using riot's AND operator.
// More than one argument is an error reported by Build.
func (b *Builder) MatchAll(text ...string) *Builder {
	next := *b

	if len(text) > 1 {
		if next.err == nil {
			next.err = fmt.Errorf("MatchAll accepts at most one text argument")
		}
		return &next
	}
	if len(text) == 0 {
		return next.add(clause{allDocs: true})
	}
	return next.add(clause{text: text[0], allTerms: true})
}

func (b *Builder) add(c clause) *Builder {
	next := *b

	c.or = next.nextOr
	next.clauses = append(slices.Clone(next.clauses), c)
	next.nextOr = false
	return &next
}

// Field sets the last clause's field. An empty name uses Build's defaultField.
// It has no effect on an all-documents clause. Without a clause it records an error.
func (b *Builder) Field(name string) *Builder {
	next := *b

	if len(next.clauses) == 0 {
		if next.err == nil {
			next.err = fmt.Errorf("Field requires a preceding clause")
		}
		return &next
	}
	next.clauses = slices.Clone(next.clauses)
	next.clauses[len(next.clauses)-1].field = name
	return &next
}

// And selects AND for the next clause (the default after each added clause).
// The last connector wins; leading and trailing connectors have no effect.
func (b *Builder) And() *Builder {
	next := *b
	next.nextOr = false
	return &next
}

// Or selects OR for the next clause, after which the connector resets to AND.
func (b *Builder) Or() *Builder {
	next := *b
	next.nextOr = true
	return &next
}

// From sets the number of results to skip.
func (b *Builder) From(from int) *Builder {
	next := *b
	next.from = from
	return &next
}

// Form is an alias for From.
func (b *Builder) Form(from int) *Builder { return b.From(from) }

// Size sets the result limit, including zero. The default is 10.
func (b *Builder) Size(size int) *Builder {
	next := *b
	next.size = size
	next.sizeSet = true
	return &next
}

// Sort appends a sort key in priority order, replacing the default score sort.
// Names are literal field names, except _score selects document score.
// Direction is controlled only by ascending, not by prefixes in the field name.
func (b *Builder) Sort(field string, ascending bool) *Builder {
	next := *b

	next.sorts = append(slices.Clone(next.sorts), sortField{field, ascending})
	return &next
}

// Highlight enables highlighting by default when called without arguments.
// If supplied, the first bool controls it; further values are ignored.
// Build requests match locations, leaving rendering to the caller.
func (b *Builder) Highlight(enabled ...bool) *Builder {
	next := *b

	next.highlight = len(enabled) == 0 || enabled[0]
	return &next
}

// HighlightEnabled reports whether highlighting was enabled. Nil is disabled.
func (b *Builder) HighlightEnabled() bool { return b != nil && b.highlight }

// Build validates the description and creates a fresh search request.
// Empty queries match all documents. Every text clause receives analyzer;
// nil lets riot use its configured default analyzer. Empty defaultField lets
// riot use its configured default search field. Pagination must be nonnegative
// and from+size must fit in an int. Sort names must not be empty.
func (b *Builder) Build(defaultField string, analyzer *analysis.Analyzer) (*riot.TopNSearch, error) {
	if b == nil {
		return nil, fmt.Errorf("cannot build a nil query builder")
	}
	if b.err != nil {
		return nil, b.err
	}
	size := 10
	if b.sizeSet {
		size = b.size
	}
	if b.from < 0 || size < 0 || b.from > int(^uint(0)>>1)-size {
		return nil, fmt.Errorf("invalid pagination: from=%d size=%d", b.from, size)
	}
	order := make(search.SortOrder, 0, len(b.sorts))
	for _, field := range b.sorts {
		if field.name == "" {
			return nil, fmt.Errorf("sort field must not be empty")
		}
		sort := search.SortBy(search.Field(field.name))
		if field.name == "_score" {
			sort = search.SortBy(search.DocumentScore())
		}
		if !field.ascending {
			sort.Desc()
		}
		order = append(order, sort)
	}
	var expr riot.Query
	for _, c := range b.clauses {
		var q riot.Query = riot.NewMatchAllQuery()
		if !c.allDocs {
			field := c.field
			if field == "" {
				field = defaultField
			}
			match := riot.NewMatchQuery(c.text).SetField(field).SetAnalyzer(analyzer)
			if c.allTerms {
				match.SetOperator(riot.MatchQueryOperatorAnd)
			}
			q = match
		}
		if expr == nil {
			expr = q
		} else if c.or {
			expr = riot.NewBooleanQuery().AddShould(expr, q).SetMinShould(1)
		} else {
			expr = riot.NewBooleanQuery().AddMust(expr, q)
		}
	}
	if expr == nil {
		expr = riot.NewMatchAllQuery()
	}
	request := riot.NewTopNSearch(size, expr).SetFrom(b.from)
	if len(order) > 0 {
		request.SortByCustom(order)
	}
	if b.highlight {
		request.IncludeLocations()
	}
	return request, nil
}
