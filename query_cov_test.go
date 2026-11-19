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
	"net"
	"reflect"
	"testing"
	"time"

	segment "github.com/vcaesar/bluge_segment_api"

	"github.com/vcaesar/riot/analysis/analyzer"
	"github.com/vcaesar/riot/numeric/geo"
	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/searcher"
)

var covDate = time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)

// covIndex returns a reader over a small in-memory index whose documents
// populate the default "_all" field.
func covIndex(t *testing.T) *Reader {
	t.Helper()
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	docs := []*Document{
		NewDocument("a").
			AddField(NewTextField("name", "marty hello world").SearchTermPositions()).
			AddField(NewNumericField("n", 5)).
			AddField(NewDateTimeField("d", covDate)).
			AddField(NewGeoPointField("geo", 10, 10)).
			AddField(NewIPField("ip", net.ParseIP("10.0.0.1"))).
			AddField(NewCompositeFieldExcluding("_all", nil)),
		NewDocument("b").
			AddField(NewTextField("name", "jasper hello").SearchTermPositions()).
			AddField(NewNumericField("n", 7)).
			AddField(NewGeoPointField("geo", 170, 10)).
			AddField(NewCompositeFieldExcluding("_all", nil)),
	}
	for _, doc := range docs {
		if err := w.Insert(doc); err != nil {
			t.Fatal(err)
		}
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	return r
}

func covSearch(ctx context.Context, r *Reader, q Query) (int, error) {
	it, err := r.Search(ctx, NewTopNSearch(10, q))
	if err != nil {
		return 0, err
	}
	n := 0
	for {
		dm, err := it.Next()
		if err != nil || dm == nil {
			return n, err
		}
		n++
	}
}

func TestQueryAccessorsCov(t *testing.T) {
	points := []geo.Point{{Lon: 1, Lat: 2}, {Lon: 3, Lat: 4}, {Lon: 5, Lat: 0}}
	a := analyzer.NewKeywordAnalyzer()
	tests := []struct {
		name      string
		got, want any
	}{
		{"boolean", func() any {
			q := NewBooleanQuery().AddMust(NewTermQuery("a")).AddShould(NewTermQuery("b")).
				AddMustNot(NewTermQuery("c")).SetMinShould(1).SetBoost(2)
			return []any{len(q.Musts()), len(q.Shoulds()), len(q.MustNots()), q.MinShould(), q.Boost()}
		}(), []any{1, 1, 1, 1, 2.0}},
		{"date range", func() any {
			q := NewDateRangeQuery(covDate, covDate.Add(time.Hour)).SetBoost(2).SetField("d")
			s, si := q.Start()
			e, ei := q.End()
			return []any{s, si, e, ei, q.Boost(), q.Field()}
		}(), []any{covDate, true, covDate.Add(time.Hour), false, 2.0, "d"}},
		{"fuzzy", func() any {
			q := NewFuzzyQuery("x").SetPrefix(1).SetFuzziness(2).SetBoost(3).SetField("f")
			return []any{q.Term(), q.Prefix(), q.Fuzziness(), q.Boost(), q.Field()}
		}(), []any{"x", 1, 2, 3.0, "f"}},
		{"geo bounding box", func() any {
			q := NewGeoBoundingBoxQuery(1, 2, 3, 4).SetBoost(2).SetField("g")
			return []any{q.TopLeft(), q.BottomRight(), q.Boost(), q.Field()}
		}(), []any{[]float64{1, 2}, []float64{3, 4}, 2.0, "g"}},
		{"geo distance", func() any {
			q := NewGeoDistanceQuery(1, 2, "1km").SetBoost(2).SetField("g")
			return []any{q.Location(), q.Distance(), q.Boost(), q.Field()}
		}(), []any{[]float64{1, 2}, "1km", 2.0, "g"}},
		{"geo polygon", func() any {
			q := NewGeoBoundingPolygonQuery(points).SetBoost(2).SetField("g")
			return []any{q.Points(), q.Boost(), q.Field()}
		}(), []any{points, 2.0, "g"}},
		{"match all", NewMatchAllQuery().SetBoost(2).Boost(), 2.0},
		{"match none", NewMatchNoneQuery().SetBoost(2).Boost(), 2.0},
		{"match phrase", func() any {
			q := NewMatchPhraseQuery("a b").SetBoost(2).SetField("f").SetSlop(1).SetAnalyzer(a)
			return []any{q.Phrase(), q.Boost(), q.Field(), q.Slop(), q.Analyzer() == a}
		}(), []any{"a b", 2.0, "f", 1, true}},
		{"match", func() any {
			q := NewMatchQuery("a").SetBoost(2).SetField("f").SetFuzziness(1).SetPrefix(2).
				SetAnalyzer(a).SetOperator(MatchQueryOperatorAnd)
			return []any{q.Match(), q.Boost(), q.Field(), q.Fuzziness(), q.Prefix(), q.Analyzer() == a, q.Operator()}
		}(), []any{"a", 2.0, "f", 1, 2, true, MatchQueryOperator(MatchQueryOperatorAnd)}},
		{"multi phrase", func() any {
			q := NewMultiPhraseQuery([][]string{{"a"}}).SetBoost(2).SetField("f").SetSlop(1)
			return []any{q.Terms(), q.Boost(), q.Field(), q.Slop()}
		}(), []any{[][]string{{"a"}}, 2.0, "f", 1}},
		{"numeric range", func() any {
			q := NewNumericRangeQuery(1, 2).SetBoost(2).SetField("n")
			lo, loi := q.Min()
			hi, hii := q.Max()
			return []any{lo, loi, hi, hii, q.Boost(), q.Field()}
		}(), []any{1.0, true, 2.0, false, 2.0, "n"}},
		{"prefix", func() any {
			q := NewPrefixQuery("p").SetBoost(2).SetField("f")
			return []any{q.Prefix(), q.Boost(), q.Field()}
		}(), []any{"p", 2.0, "f"}},
		{"regexp", func() any {
			q := NewRegexpQuery("r.*").SetBoost(2).SetField("f")
			return []any{q.Regexp(), q.Boost(), q.Field()}
		}(), []any{"r.*", 2.0, "f"}},
		{"term", func() any {
			q := NewTermQuery("t").SetBoost(2).SetField("f")
			return []any{q.Term(), q.Boost(), q.Field()}
		}(), []any{"t", 2.0, "f"}},
		{"term range", func() any {
			q := NewTermRangeQuery("a", "b").SetBoost(2).SetField("f")
			lo, loi := q.Min()
			hi, hii := q.Max()
			return []any{lo, loi, hi, hii, q.Boost(), q.Field()}
		}(), []any{"a", true, "b", false, 2.0, "f"}},
		{"ip range", func() any {
			q := NewIPRangeQuery("10.0.0.0/8").SetBoost(2).SetField("ip")
			return []any{q.CIDR(), q.Boost(), q.Field()}
		}(), []any{"10.0.0.0/8", 2.0, "ip"}},
		{"wildcard", func() any {
			q := NewWildcardQuery("w*").SetBoost(2).SetField("f")
			return []any{q.Wildcard(), q.Boost(), q.Field()}
		}(), []any{"w*", 2.0, "f"}},
		{"knn", func() any {
			q := NewKNNQuery("v", []float32{1, 2}, 3).SetField("w").SetMetric(L2).SetBoost(2)
			return []any{q.Field(), q.Metric(), q.Boost(), q.K(), q.Vector()}
		}(), []any{"w", L2, 2.0, 3, []float32{1, 2}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !reflect.DeepEqual(tt.got, tt.want) {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}

func TestQueryValidateCov(t *testing.T) {
	future := time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC)
	bad := NewTermRangeQuery("", "")
	tests := []struct {
		name    string
		q       validatableQuery
		wantErr bool
	}{
		{"boolean empty", NewBooleanQuery(), true},
		{"boolean bad must", NewBooleanQuery().AddMust(bad), true},
		{"boolean bad should", NewBooleanQuery().AddShould(bad), true},
		{"boolean bad must not", NewBooleanQuery().AddMustNot(bad), true},
		{"boolean ok", NewBooleanQuery().AddMust(NewTermQuery("a"), NewTermRangeQuery("a", "")).
			AddShould(NewTermQuery("b"), NewTermRangeQuery("a", "")).
			AddMustNot(NewTermQuery("c"), NewTermRangeQuery("a", "")), false},
		{"date range empty", NewDateRangeQuery(time.Time{}, time.Time{}), true},
		{"date range start overflow", NewDateRangeQuery(future, time.Time{}), true},
		{"date range end overflow", NewDateRangeQuery(time.Time{}, future), true},
		{"date range ok", NewDateRangeQuery(covDate, time.Time{}), false},
		{"geo bounding box", NewGeoBoundingBoxQuery(0, 1, 1, 0), false},
		{"geo distance", NewGeoDistanceQuery(0, 0, "1km"), false},
		{"geo polygon", NewGeoBoundingPolygonQuery(nil), false},
		{"multi phrase empty", NewMultiPhraseQuery(nil), true},
		{"multi phrase ok", NewMultiPhraseQuery([][]string{{"a"}}), false},
		{"numeric range unbounded", NewNumericRangeQuery(MinNumeric, MaxNumeric), true},
		{"numeric range ok", NewNumericRangeQuery(1, MaxNumeric), false},
		{"regexp", NewRegexpQuery("["), false},
		{"term range empty", bad, true},
		{"term range ok", NewTermRangeQuery("a", ""), false},
		{"wildcard", NewWildcardQuery("["), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.q.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestQueryDefaultFieldCov searches every query type without a field, so the
// default "_all" field is used.
func TestQueryDefaultFieldCov(t *testing.T) {
	r := covIndex(t)
	square := []geo.Point{{Lon: 0, Lat: 0}, {Lon: 20, Lat: 0}, {Lon: 20, Lat: 20}, {Lon: 0, Lat: 20}}
	tests := []struct {
		name string
		q    Query
		want int
	}{
		{"fuzzy", NewFuzzyQuery("helo"), 2},
		{"geo bounding box", NewGeoBoundingBoxQuery(0, 20, 20, 0), 1},
		{"geo bounding box across date line", NewGeoBoundingBoxQuery(160, 20, 20, 0), 2},
		// _all holds the geo terms but not the doc values these check
		{"geo distance", NewGeoDistanceQuery(10, 10, "10km"), 0},
		{"geo polygon", NewGeoBoundingPolygonQuery(square), 0},
		{"geo distance field", NewGeoDistanceQuery(10, 10, "10km").SetField("geo"), 1},
		{"geo polygon field", NewGeoBoundingPolygonQuery(square).SetField("geo"), 1},
		{"date range", NewDateRangeQuery(covDate.Add(-time.Hour), covDate.Add(time.Hour)), 1},
		{"match phrase", NewMatchPhraseQuery("hello world"), 1},
		{"match phrase analyzer", NewMatchPhraseQuery("hello").SetAnalyzer(analyzer.NewKeywordAnalyzer()), 2},
		{"match phrase no tokens", NewMatchPhraseQuery("  "), 0},
		{"match no tokens", NewMatchQuery("  "), 0},
		{"multi phrase", NewMultiPhraseQuery([][]string{{"hello"}, {"world", "x"}}), 1},
		{"numeric range", NewNumericRangeQuery(6, 8), 1},
		{"prefix", NewPrefixQuery("mar"), 1},
		{"regexp", NewRegexpQuery("^jas.*"), 1},
		{"term", NewTermQuery("hello"), 2},
		{"term range", NewTermRangeQuery("j", "k"), 1},
		{"ip range", NewIPRangeQuery("10.0.0.0/8"), 1},
		{"wildcard", NewWildcardQuery("wor?d"), 1},
		{"boolean empty", NewBooleanQuery(), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := covSearch(context.Background(), r, tt.q)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %d matches, want %d", got, tt.want)
			}
		})
	}
}

// TestQueryNoDefaultAnalyzerCov builds match searchers with options that
// lack a default analyzer, so the query text is a single token.
func TestQueryNoDefaultAnalyzerCov(t *testing.T) {
	r := covIndex(t)
	options := searchOptionsFromConfig(r.config, SearchOptions{})
	options.DefaultSearchField, options.DefaultAnalyzer = "name", nil
	for _, q := range []Query{NewMatchQuery("hello"), NewMatchPhraseQuery("hello")} {
		s, err := q.Searcher(r.reader, options)
		if err != nil {
			t.Fatal(err)
		}
		dm, err := s.Next(search.NewSearchContext(s.DocumentMatchPoolSize(), 0))
		if err != nil || dm == nil {
			t.Errorf("%T: match %v, err %v", q, dm, err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestQuerySearcherErrorsCov(t *testing.T) {
	r := covIndex(t)
	future := time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC)
	badRe := NewRegexpQuery("[").SetField("name")
	term := func(s string) Query { return NewTermQuery(s).SetField("name") }
	tests := []struct {
		name string
		q    Query
	}{
		{"date range start", NewDateRangeQuery(future, time.Time{})},
		{"date range end", NewDateRangeQuery(time.Time{}, future)},
		{"geo distance", NewGeoDistanceQuery(0, 0, "far")},
		{"match operator", NewMatchQuery("hello").SetOperator(7)},
		{"ip range", NewIPRangeQuery("nope")},
		{"boolean bad must not", NewBooleanQuery().AddMust(term("hello")).AddMustNot(badRe).SetBoost(2)},
		{"boolean bad must", NewBooleanQuery().AddMust(term("hello"), badRe).AddMustNot(term("x"))},
		{"boolean bad should", NewBooleanQuery().AddMust(term("hello")).AddMustNot(term("x")).AddShould(badRe)},
		{"boolean bad musts", NewBooleanQuery().AddMust(term("hello"), badRe)},
		{"boolean bad shoulds", NewBooleanQuery().AddShould(term("hello"), badRe).SetBoost(2)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := covSearch(context.Background(), r, tt.q); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestTokenStreamToPhraseEmptyCov(t *testing.T) {
	if got := tokenStreamToPhrase(nil); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestSearcherHelpersCov(t *testing.T) {
	none := &searcher.MatchNoneSearcher{}
	if got := replaceMatchNoneWithNil(none); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
	cause := errors.New("cause")
	closeErr := errors.New("close")
	err := closeSearchers(cause, nil, &errSearcher{closeErr: closeErr}, none)
	if !errors.Is(err, cause) || !errors.Is(err, closeErr) {
		t.Fatalf("got %v, want both errors", err)
	}
}

// errSearcher is a searcher whose Next and Close return the given errors.
type errSearcher struct {
	searcher.MatchNoneSearcher
	nextErr, closeErr error
}

func (s *errSearcher) Next(*search.Context) (*search.DocumentMatch, error) { return nil, s.nextErr }

func (s *errSearcher) Close() error { return s.closeErr }

// failingReader fails the reader call selected by fail, which sees the call
// kind ("postings" or "docvalues") and a per-kind count starting at 1.
type failingReader struct {
	search.Reader
	fail  func(kind string, n int) bool
	calls map[string]int
}

var errReaderCov = errors.New("reader failure")

func (f *failingReader) failed(kind string) bool {
	f.calls[kind]++
	return f.fail(kind, f.calls[kind])
}

func (f *failingReader) PostingsIterator(term []byte, field string, includeFreq, includeNorm,
	includeTermVectors bool) (segment.PostingsIterator, error) {
	if f.failed("postings") {
		return nil, errReaderCov
	}
	return f.Reader.PostingsIterator(term, field, includeFreq, includeNorm, includeTermVectors)
}

func (f *failingReader) DocumentValueReader(fields []string) (segment.DocumentValueReader, error) {
	if f.failed("docvalues") {
		return nil, errReaderCov
	}
	return f.Reader.DocumentValueReader(fields)
}

func TestQueryReaderFailuresCov(t *testing.T) {
	r := covIndex(t)
	tests := []struct {
		name string
		q    Query
		fail func(kind string, n int) bool
	}{
		{"must not only, match all fails", NewBooleanQuery().AddMustNot(NewTermQuery("hello").SetField("name")),
			func(kind string, n int) bool { return kind == "postings" && n == 2 }},
		{"date line left half", NewGeoBoundingBoxQuery(160, 20, 20, 0).SetField("geo"),
			func(kind string, n int) bool { return kind == "docvalues" && n == 1 }},
		{"date line right half", NewGeoBoundingBoxQuery(160, 20, 20, 0).SetField("geo"),
			func(kind string, n int) bool { return kind == "docvalues" && n == 2 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fr := &failingReader{Reader: r.reader, fail: tt.fail, calls: map[string]int{}}
			s, err := tt.q.Searcher(fr, searchOptionsFromConfig(r.config, SearchOptions{}))
			if !errors.Is(err, errReaderCov) {
				if s != nil {
					_ = s.Close()
				}
				t.Fatalf("got %v, want reader failure", err)
			}
		})
	}
}
