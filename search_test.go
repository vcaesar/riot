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

package bluge

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"testing"

	"github.com/vcaesar/riot/search/aggregations"
	"github.com/vcaesar/riot/search/highlight"

	"github.com/vcaesar/riot/analysis/char"

	"github.com/vcaesar/riot/numeric/geo"

	"github.com/vcaesar/riot/search"

	"github.com/vcaesar/riot/analysis"
	"github.com/vcaesar/riot/analysis/lang/en"
	"github.com/vcaesar/riot/analysis/token"
	"github.com/vcaesar/riot/analysis/tokenizer"
)

// https://github.com/blevesearch/bleve/issues/954
func TestNestedBooleanSearchers(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	customAnalyzer := &analysis.Analyzer{
		Tokenizer: tokenizer.NewWhitespaceTokenizer(),
		TokenFilters: []analysis.TokenFilter{
			token.NewLowerCaseFilter(),
			en.StopWordsFilter(),
		},
	}

	singleLowercase := &analysis.Analyzer{
		Tokenizer: tokenizer.NewSingleTokenTokenizer(),
		TokenFilters: []analysis.TokenFilter{
			token.NewLowerCaseFilter(),
		},
	}

	// create and insert documents as a batch
	batch := NewBatch()
	matches := 0
	for i := 0; i < 100; i++ {
		hostname := fmt.Sprintf("planner_hostname_%d", i%5)
		metadataRegion := fmt.Sprintf("planner_us-east-%d", i%5)

		// Expected matches
		if (hostname == "planner_hostname_1" || hostname == "planner_hostname_2") &&
			metadataRegion == "planner_us-east-1" {
			matches++
		}

		doc := NewDocument(strconv.Itoa(i)).
			AddField(NewTextField("hostname", hostname).WithAnalyzer(singleLowercase)).
			AddField(NewTextField("metadata.region", metadataRegion).WithAnalyzer(customAnalyzer)).
			AddField(NewCompositeFieldExcluding("_all", []string{"_id"}))

		batch.Update(doc.ID(), doc)
	}

	if err = indexWriter.Batch(batch); err != nil {
		t.Fatal(err)
	}

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting reader: %v", err)
	}

	query := NewBooleanQuery()
	query.AddMust(
		NewBooleanQuery().
			AddMust(
				NewBooleanQuery().
					AddShould(
						NewMatchQuery("planner_hostname_1").
							SetField("hostname"),
						NewMatchQuery("planner_hostname_2").
							SetField("hostname"))),
		NewBooleanQuery().
			AddMust(NewMatchQuery("planner_us-east-1").
				SetField("metadata.region").
				SetAnalyzer(customAnalyzer)))

	req := NewTopNSearch(100, query)

	dmi, err := indexReader.Search(context.Background(), req)
	if err != nil {
		t.Fatalf("error executing search: %v", err)
	}

	var count int
	next, err := dmi.Next()
	for err == nil && next != nil {
		count++
		next, err = dmi.Next()
	}
	if err != nil {
		t.Fatalf("error iterating document matches: %v", err)
	}

	if matches != count {
		t.Fatalf("Unexpected result set, %v != %v", matches, count)
	}

	err = indexReader.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestNestedBooleanMustNotSearcher(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	// create and insert documents as a batch
	batch := NewBatch()

	docs := []struct {
		id              string
		hasRole         bool
		investigationID string
	}{
		{
			id:              "1@1",
			hasRole:         true,
			investigationID: "1",
		},
		{
			id:              "1@2",
			hasRole:         false,
			investigationID: "2",
		},
		{
			id:              "2@1",
			hasRole:         true,
			investigationID: "1",
		},
		{
			id:              "2@2",
			hasRole:         false,
			investigationID: "2",
		},
		{
			id:              "3@1",
			hasRole:         true,
			investigationID: "1",
		},
		{
			id:              "3@2",
			hasRole:         false,
			investigationID: "2",
		},
		{
			id:              "4@1",
			hasRole:         true,
			investigationID: "1",
		},
		{
			id:              "5@1",
			hasRole:         true,
			investigationID: "1",
		},
		{
			id:              "6@1",
			hasRole:         true,
			investigationID: "1",
		},
		{
			id:              "7@1",
			hasRole:         true,
			investigationID: "1",
		},
	}

	for i := 0; i < len(docs); i++ {
		doc := NewDocument(docs[i].id).
			AddField(NewTextField("id", docs[i].id)).
			AddField(NewTextField("investigationID", docs[i].investigationID))

		if docs[i].hasRole {
			doc.AddField(NewKeywordField("hasRole", "t"))
		} else {
			doc.AddField(NewKeywordField("hasRole", "f"))
		}

		doc.AddField(NewCompositeFieldExcluding("_all", []string{"_id"}))

		batch.Update(doc.ID(), doc)
	}

	if err = indexWriter.Batch(batch); err != nil {
		t.Fatal(err)
	}

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	tq := NewTermQuery("1").SetField("investigationID")

	// using must not, for cases that the field did not exists at all
	hasRole := NewTermQuery("t").SetField("hasRole")

	noRole := NewBooleanQuery()
	noRole.AddMustNot(hasRole)

	oneRolesOrNoRoles := NewBooleanQuery()
	oneRolesOrNoRoles.AddShould(noRole)
	oneRolesOrNoRoles.SetMinShould(1)

	q := NewBooleanQuery()
	q.AddMust(tq, oneRolesOrNoRoles)

	sr := NewTopNSearch(100, q).
		WithStandardAggregations()

	dmi, err := indexReader.Search(context.Background(), sr)
	if err != nil {
		t.Fatal(err)
	}

	if dmi.Aggregations().Count() != 0 {
		t.Fatalf("Unexpected result, %v != 0", dmi.Aggregations().Count())
	}

	err = indexReader.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestSearchOverEmptyKeyword(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 10; i++ {
		doc := NewDocument(fmt.Sprint(i)).
			AddField(NewKeywordField("id", "")).
			AddField(NewTextField("name", fmt.Sprintf("test%d", i))).
			AddField(NewCompositeFieldExcluding("_all", []string{"_id"}))
		err = indexWriter.Update(doc.ID(), doc)
		if err != nil {
			t.Fatal(err)
		}
	}

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	count, err := indexReader.Count()
	if err != nil {
		t.Fatal(err)
	}
	if count != 10 {
		t.Fatalf("Unexpected doc count: %v, expected 10", count)
	}

	q := NewWildcardQuery("test*")
	sr := NewTopNSearch(40, q).WithStandardAggregations()
	res, err := indexReader.Search(context.Background(), sr)
	if err != nil {
		t.Fatal(err)
	}
	if res.Aggregations().Count() != 10 {
		t.Fatalf("Unexpected search hits: %v, expected 10", res.Aggregations().Count())
	}

	err = indexReader.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestMultipleNestedBooleanMustNotSearchers(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	// create and insert documents as a batch
	batch := NewBatch()

	doc := NewDocument("1-child-0").
		AddField(NewTextField("id", "1-child-0")).
		AddField(NewKeywordField("hasRole", "f")).
		AddField(NewKeywordField("roles", "R1")).
		AddField(NewNumericField("type", 0)).
		AddField(NewCompositeFieldExcluding("_all", []string{"_id"}))
	batch.Update(doc.ID(), doc)

	docs := []struct {
		id      string
		hasRole bool
		typ     int
	}{
		{
			id:      "16d6fa37-48fd-4dea-8b3d-a52bddf73951",
			hasRole: false,
			typ:     9,
		},
		{
			id:      "18fa9eb2-8b1f-46f0-8b56-b4c551213f78",
			hasRole: false,
			typ:     9,
		},
		{
			id:      "3085855b-d74b-474a-86c3-9bf3e4504382",
			hasRole: false,
			typ:     9,
		},
		{
			id:      "38ef5d28-0f85-4fb0-8a94-dd20751c3364",
			hasRole: false,
			typ:     9,
		},
	}

	for i := 0; i < len(docs); i++ {
		doc = NewDocument(docs[i].id).
			AddField(NewTextField("id", docs[i].id)).
			AddField(NewNumericField("type", float64(docs[i].typ)))

		if docs[i].hasRole {
			doc.AddField(NewKeywordField("hasRole", "t"))
		} else {
			doc.AddField(NewKeywordField("hasRole", "f"))
		}

		doc.AddField(NewCompositeFieldExcluding("_all", []string{"_id"}))

		batch.Update(doc.ID(), doc)
	}

	if err = indexWriter.Batch(batch); err != nil {
		t.Fatal(err)
	}

	// Update 1st doc
	batch = NewBatch()
	doc = NewDocument("1-child-0").
		AddField(NewTextField("id", "1-child-0")).
		AddField(NewKeywordField("hasRole", "f")).
		AddField(NewNumericField("type", 0)).
		AddField(NewCompositeFieldExcluding("_all", []string{"_id"}))

	batch.Update(doc.ID(), doc)

	if err = indexWriter.Batch(batch); err != nil {
		t.Fatal(err)
	}

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	q := NewNumericRangeInclusiveQuery(9, 9, true, true).SetField("type")
	initialQuery := NewBooleanQuery().AddMustNot(q)

	// using must not, for cases that the field did not exists at all
	hasRole := NewTermQuery("t").SetField("hasRole")
	noRole := NewBooleanQuery().AddMustNot(hasRole)

	rq := NewBooleanQuery().AddMust(initialQuery, noRole)

	sr := NewTopNSearch(100, rq).WithStandardAggregations()

	dmi, err := indexReader.Search(context.Background(), sr)
	if err != nil {
		t.Fatal(err)
	}

	if dmi.Aggregations().Count() != 1 {
		t.Fatalf("Unexpected result, %v != 1", dmi.Aggregations().Count())
	}

	err = indexReader.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestBooleanMustNotSearcher(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	docs := []struct {
		Name    string
		HasRole bool
	}{
		{
			Name: "13900",
		},
		{
			Name: "13901",
		},
		{
			Name: "13965",
		},
		{
			Name:    "13966",
			HasRole: true,
		},
		{
			Name:    "13967",
			HasRole: true,
		},
	}

	for _, doc := range docs {
		bdoc := NewDocument(doc.Name)
		if doc.HasRole {
			bdoc.AddField(NewKeywordField("hasRole", "t"))
		}
		err = indexWriter.Update(bdoc.ID(), bdoc)
		if err != nil {
			t.Fatal(err)
		}
	}

	lhs := NewBooleanQuery().AddShould(
		NewTermQuery("13965").SetField("_id"),
		NewTermQuery("13966").SetField("_id"),
		NewTermQuery("13967").SetField("_id"))
	hasRole := NewTermQuery("t").SetField("hasRole")
	rhs := NewBooleanQuery()
	rhs.AddMustNot(hasRole)

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	var compareLeftRightAndConjunction = func(idxReader *Reader, left, right Query) error {
		// left
		lr := NewTopNSearch(100, left)
		lres, err2 := idxReader.Search(context.Background(), lr)
		if err2 != nil {
			return fmt.Errorf("error left: %v", err2)
		}
		lresIDs := map[string]struct{}{}
		lresNext, err2 := lres.Next()
		for err2 == nil && lresNext != nil {
			err2 = lresNext.VisitStoredFields(func(field string, value []byte) bool {
				if field == "_id" {
					lresIDs[string(value)] = struct{}{}
				}
				return true
			})
			if err2 != nil {
				t.Fatalf("error visitng stored fields: %v", err2)
			}
			lresNext, err2 = lres.Next()
		}
		if err2 != nil {
			t.Fatalf("error iterating results: %v", err2)
		}
		// right
		rr := NewTopNSearch(100, right)
		rres, err2 := idxReader.Search(context.Background(), rr)
		if err2 != nil {
			return fmt.Errorf("error right: %v", err2)
		}
		rresIDs := map[string]struct{}{}
		rresNext, err2 := rres.Next()
		for err2 == nil && rresNext != nil {
			err2 = rresNext.VisitStoredFields(func(field string, value []byte) bool {
				if field == "_id" {
					rresIDs[string(value)] = struct{}{}
				}
				return true
			})
			if err2 != nil {
				t.Fatalf("error visitng stored fields: %v", err2)
			}
			rresNext, err2 = rres.Next()
		}
		if err2 != nil {
			t.Fatalf("error iterating results: %v", err2)
		}
		// conjunction
		conj := NewBooleanQuery()
		conj.AddMust(left, right)
		cr := NewTopNSearch(100, conj)
		cres, err2 := idxReader.Search(context.Background(), cr)
		if err2 != nil {
			return fmt.Errorf("error conjunction: %v", err2)
		}

		cresNext, err2 := cres.Next()
		for err2 == nil && cresNext != nil {
			var theID string
			err2 = cresNext.VisitStoredFields(func(field string, value []byte) bool {
				if field == "_id" {
					theID = string(value)
				}
				return true
			})
			if err2 != nil {
				t.Fatalf("error visitng stored fields: %v", err2)
			}

			if _, ok := lresIDs[theID]; ok {
				if _, ok := rresIDs[theID]; !ok {
					return fmt.Errorf("error id %s missing from right", theID)
				}
			} else {
				return fmt.Errorf("error id %s missing from left", theID)
			}

			cresNext, err2 = cres.Next()
		}
		if err2 != nil {
			t.Fatalf("error iterating results: %v", err)
		}

		return nil
	}

	err = compareLeftRightAndConjunction(indexReader, lhs, rhs)
	if err != nil {
		t.Fatal(err)
	}

	err = indexReader.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestDisjunctionQueryIncorrectMin(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	// create and insert documents as a batch
	batch := NewBatch()
	docs := []struct {
		field1 string
		field2 int
	}{
		{
			field1: "one",
			field2: 1,
		},
		{
			field1: "two",
			field2: 2,
		},
	}

	for i := 0; i < len(docs); i++ {
		doc := NewDocument(strconv.Itoa(docs[i].field2)).
			AddField(NewTextField("field1", docs[i].field1)).
			AddField(NewNumericField("field2", float64(docs[i].field2))).
			AddField(NewCompositeFieldExcluding("_id", []string{"_id"}))

		batch.Update(doc.ID(), doc)
	}

	if err = indexWriter.Batch(batch); err != nil {
		t.Fatal(err)
	}

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	tq := NewTermQuery("one")
	dq := NewBooleanQuery().AddShould(tq)
	dq.SetMinShould(2)
	sr := NewTopNSearch(1, dq).WithStandardAggregations()
	res, err := indexReader.Search(context.Background(), sr)
	if err != nil {
		t.Fatal(err)
	}

	if res.Aggregations().Count() > 0 {
		t.Fatalf("Expected 0 matches as disjunction query contains a single clause"+
			" but got: %v", res.Aggregations().Count())
	}

	err = indexReader.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestBooleanShouldMinPropagation(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	doc1 := NewDocument("doc1").
		AddField(NewTextField("name", "cersei lannister")).
		AddField(NewTextField("dept", "queen"))

	doc2 := NewDocument("doc2").
		AddField(NewTextField("name", "jaime lannister")).
		AddField(NewTextField("dept", "kings guard"))

	batch := NewBatch()
	batch.Update(doc1.ID(), doc1)
	batch.Update(doc2.ID(), doc2)

	if err = indexWriter.Batch(batch); err != nil {
		t.Fatal(err)
	}

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	// term dictionaries in the index for field..
	//  dept: queen kings guard
	//  name: cersei jaime lannister

	// the following match query would match doc2
	mq1 := NewMatchQuery("kings guard").SetField("dept")

	// the following match query would match both doc1 and doc2,
	// as both docs share common lastname
	mq2 := NewMatchQuery("jaime lannister").SetField("name")

	bq := NewBooleanQuery().
		AddShould(mq1).
		AddMust(mq2)

	sr := NewTopNSearch(10, bq).WithStandardAggregations()
	res, err := indexReader.Search(context.Background(), sr)
	if err != nil {
		t.Fatal(err)
	}

	if res.Aggregations().Count() != 2 {
		t.Errorf("Expected 2 results, but got: %v", res.Aggregations().Count())
	}

	err = indexReader.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestDisjunctionMinPropagation(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	doc1 := NewDocument("doc1").
		AddField(NewTextField("dept", "finance")).
		AddField(NewTextField("name", "xyz")).
		AddField(NewCompositeFieldExcluding("_all", []string{"_id"}))

	doc2 := NewDocument("doc2").
		AddField(NewTextField("dept", "marketing")).
		AddField(NewTextField("name", "xyz")).
		AddField(NewCompositeFieldExcluding("_all", []string{"_id"}))

	doc3 := NewDocument("doc3").
		AddField(NewTextField("dept", "engineering")).
		AddField(NewTextField("name", "abc")).
		AddField(NewCompositeFieldExcluding("_all", []string{"_id"}))

	batch := NewBatch()
	batch.Update(doc1.ID(), doc1)
	batch.Update(doc2.ID(), doc2)
	batch.Update(doc3.ID(), doc3)

	if err = indexWriter.Batch(batch); err != nil {
		t.Fatal(err)
	}

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	mq1 := NewMatchQuery("finance")
	mq2 := NewMatchQuery("marketing")
	dq := NewBooleanQuery().AddShould(mq1, mq2)
	dq.SetMinShould(3)

	dq2 := NewBooleanQuery().AddShould(dq)
	dq2.SetMinShould(1)

	sr := NewTopNSearch(10, dq2).WithStandardAggregations()
	res, err := indexReader.Search(context.Background(), sr)
	if err != nil {
		t.Fatal(err)
	}

	if res.Aggregations().Count() != 0 {
		t.Fatalf("Expect 0 results, but got: %v", res.Aggregations().Count())
	}

	err = indexReader.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestDuplicateLocationsIssue1168(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	doc := NewDocument("x").
		AddField(NewKeywordField("name", "marty").SearchTermPositions()).
		AddField(NewCompositeFieldExcluding("_all", []string{"_id"}))
	err = indexWriter.Update(doc.ID(), doc)
	if err != nil {
		t.Fatalf("bleve index err: %v", err)
	}

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	q1 := NewTermQuery("marty")
	q2 := NewTermQuery("marty")
	dq := NewBooleanQuery().AddShould(q1, q2)

	sreq := NewTopNSearch(10, dq).IncludeLocations()

	sres, err := indexReader.Search(context.Background(), sreq)
	if err != nil {
		t.Fatalf("bleve search err: %v", err)
	}
	next, err := sres.Next()
	if err != nil {
		t.Fatalf("error getting first hit")
	}
	if next == nil {
		t.Fatalf("expected at least one hit")
	}
	if len(next.Locations["name"]["marty"]) != 1 {
		t.Fatalf("expected 1, there are %d marty", len(next.Locations["name1"]["marty"]))
	}

	err = indexReader.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestBooleanMustSingleMatchNone(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	customAnalyzer := &analysis.Analyzer{
		Tokenizer: tokenizer.NewSingleTokenTokenizer(),
		TokenFilters: []analysis.TokenFilter{
			token.NewLengthFilter(3, 5),
		},
	}

	doc := NewDocument("doc").
		AddField(NewTextField("languages_known", "Dutch").WithAnalyzer(customAnalyzer)).
		AddField(NewTextField("dept", "Sales").WithAnalyzer(customAnalyzer))

	batch := NewBatch()
	batch.Update(doc.ID(), doc)

	if err = indexWriter.Batch(batch); err != nil {
		t.Fatal(err)
	}

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	// this is a successful match
	matchSales := NewMatchQuery("Sales").SetField("dept")

	// this would spin off a MatchNoneSearcher as the
	// token filter rules out the word "French"
	matchFrench := NewMatchQuery("French").SetField("languages_known")

	bq := NewBooleanQuery()
	bq.AddShould(matchSales)
	bq.AddMust(matchFrench)

	sr := NewTopNSearch(10, bq).WithStandardAggregations()
	res, err := indexReader.Search(context.Background(), sr)
	if err != nil {
		t.Fatal(err)
	}

	if res.Aggregations().Count() != 0 {
		t.Fatalf("Expected 0 results but got: %v", res.Aggregations().Count())
	}

	err = indexReader.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestBooleanMustNotSingleMatchNone(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	customAnalyzer := &analysis.Analyzer{
		Tokenizer: tokenizer.NewUnicodeTokenizer(),
		TokenFilters: []analysis.TokenFilter{
			token.NewShingleFilter(3, 5, false, " ", "_"),
		},
	}

	doc := NewDocument("doc").
		AddField(NewTextField("languages_known", "Dutch").WithAnalyzer(customAnalyzer)).
		AddField(NewTextField("dept", "Sales").WithAnalyzer(customAnalyzer))

	batch := NewBatch()
	batch.Update(doc.ID(), doc)

	if err = indexWriter.Batch(batch); err != nil {
		t.Fatal(err)
	}

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	// this is a successful match
	matchSales := NewMatchQuery("Sales").SetField("dept")

	// this would spin off a MatchNoneSearcher as the
	// token filter rules out the word "Dutch"
	matchDutch := NewMatchQuery("Dutch").SetField("languages_known")

	matchEngineering := NewMatchQuery("Engineering").SetField("dept")

	bq := NewBooleanQuery()
	bq.AddShould(matchSales)
	bq.AddMustNot(matchDutch, matchEngineering)

	sr := NewTopNSearch(10, bq).WithStandardAggregations()
	res, err := indexReader.Search(context.Background(), sr)
	if err != nil {
		t.Fatal(err)
	}

	if res.Aggregations().Count() != 0 {
		t.Fatalf("Expected 0 results but got: %v", res.Aggregations().Count())
	}

	err = indexReader.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestBooleanSearchBug1185(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	doc := NewDocument("17112").
		AddField(NewKeywordField("owner", "marty")).
		AddField(NewTextField("type", "A Demo Type").SearchTermPositions())
	err = indexWriter.Update(doc.ID(), doc)
	if err != nil {
		t.Fatal(err)
	}

	doc = NewDocument("17139").
		AddField(NewTextField("type", "A Demo Type").SearchTermPositions())
	err = indexWriter.Update(doc.ID(), doc)
	if err != nil {
		t.Fatal(err)
	}

	doc = NewDocument("177777").
		AddField(NewTextField("type", "x").SearchTermPositions())
	err = indexWriter.Update(doc.ID(), doc)
	if err != nil {
		t.Fatal(err)
	}

	doc = NewDocument("177778").
		AddField(NewTextField("type", "A Demo Type").SearchTermPositions())
	err = indexWriter.Update(doc.ID(), doc)
	if err != nil {
		t.Fatal(err)
	}

	doc = NewDocument("17140").
		AddField(NewTextField("type", "A Demo Type").SearchTermPositions())
	err = indexWriter.Update(doc.ID(), doc)
	if err != nil {
		t.Fatal(err)
	}

	doc = NewDocument("17000").
		AddField(NewKeywordField("owner", "marty")).
		AddField(NewTextField("type", "x").SearchTermPositions())
	err = indexWriter.Update(doc.ID(), doc)
	if err != nil {
		t.Fatal(err)
	}

	doc = NewDocument("17141").
		AddField(NewTextField("type", "A Demo Type").SearchTermPositions())
	err = indexWriter.Update(doc.ID(), doc)
	if err != nil {
		t.Fatal(err)
	}

	doc = NewDocument("17428").
		AddField(NewKeywordField("owner", "marty")).
		AddField(NewTextField("type", "A Demo Type").SearchTermPositions())
	err = indexWriter.Update(doc.ID(), doc)
	if err != nil {
		t.Fatal(err)
	}

	doc = NewDocument("17113").
		AddField(NewKeywordField("owner", "marty")).
		AddField(NewTextField("type", "x").SearchTermPositions())
	err = indexWriter.Update(doc.ID(), doc)
	if err != nil {
		t.Fatal(err)
	}

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	matchTypeQ := NewMatchPhraseQuery("A Demo Type").SetField("type")

	matchAnyOwnerRegQ := NewRegexpQuery(".+").SetField("owner")

	matchNoOwner := NewBooleanQuery()
	matchNoOwner.AddMustNot(matchAnyOwnerRegQ)

	notNoOwner := NewBooleanQuery()
	notNoOwner.AddMustNot(matchNoOwner)

	matchTypeAndNoOwner := NewBooleanQuery()
	matchTypeAndNoOwner.AddMust(matchTypeQ)
	matchTypeAndNoOwner.AddMust(notNoOwner)

	req := NewTopNSearch(10, matchTypeAndNoOwner)
	res, err := indexReader.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	resHits, err := countHits(res)
	if err != nil {
		t.Fatalf("error counting hits: %v", err)
	}

	// query 2
	matchTypeAndNoOwnerBoolean := NewBooleanQuery()
	matchTypeAndNoOwnerBoolean.AddMust(matchTypeQ)
	matchTypeAndNoOwnerBoolean.AddMustNot(matchNoOwner)

	req2 := NewTopNSearch(10, matchTypeAndNoOwnerBoolean)
	res2, err := indexReader.Search(context.Background(), req2)
	if err != nil {
		t.Fatal(err)
	}

	res2Hits, err := countHits(res2)
	if err != nil {
		t.Fatalf("error counting hits: %v", err)
	}

	if resHits != res2Hits {
		t.Fatalf("expected same number of hits, got: %d and %d", resHits, res2Hits)
	}

	err = indexReader.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func countHits(dmi search.DocumentMatchIterator) (n int, err error) {
	var next *search.DocumentMatch
	next, err = dmi.Next()
	for err == nil && next != nil {
		n++
		next, err = dmi.Next()
	}
	return n, err
}

func TestGeoDistanceIssue1301(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	for i, g := range []string{"wecpkbeddsmf", "wecpk8tne453", "wecpkb80s09t"} {
		lat, lon := geo.DecodeGeoHash(g)
		doc := NewDocument(strconv.Itoa(i)).
			AddField(NewNumericField("ID", float64(i))).
			AddField(NewGeoPointField("GEO", lon, lat))
		if err = indexWriter.Update(doc.ID(), doc); err != nil {
			t.Fatal(err)
		}
	}

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	// Not setting "Field" for the following query, targets it against the _all
	// field and this is returning inconsistent results, when there's another
	// field indexed along with the geopoint which is numeric.
	// As reported in: https://github.com/blevesearch/bleve/issues/1301
	lat, lon := 22.371154, 114.112603
	q := NewGeoDistanceQuery(lon, lat, "1km").SetField("GEO")

	req := NewTopNSearch(10, q).WithStandardAggregations()
	sr, err := indexReader.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	if sr.Aggregations().Count() != 3 {
		t.Fatalf("Size expected: 3, actual %d\n", sr.Aggregations().Count())
	}

	err = indexReader.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestSearchHighlightingWithRegexpReplacement(t *testing.T) {
	r := regexp.MustCompile(`([a-z])\s+(\d)`)
	regexpReplace := char.NewRegexpCharFilter(r, []byte("ooooo$1-$2"))
	customAnalyzer := &analysis.Analyzer{
		CharFilters: []analysis.CharFilter{
			regexpReplace,
		},
		Tokenizer: tokenizer.NewUnicodeTokenizer(),
	}

	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	doc := NewDocument("doc").
		AddField(NewTextField("status", "fool 10").
			StoreValue().
			HighlightMatches().
			WithAnalyzer(customAnalyzer))

	batch := NewBatch()
	batch.Update(doc.ID(), doc)

	if err = indexWriter.Batch(batch); err != nil {
		t.Fatal(err)
	}

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	query := NewMatchQuery("fool 10").SetAnalyzer(customAnalyzer).SetField("status")
	sreq := NewTopNSearch(10, query).WithStandardAggregations().IncludeLocations()

	dmi, err := indexReader.Search(context.Background(), sreq)
	if err != nil {
		t.Fatal(err)
	}

	ansiHighligher := highlight.NewANSIHighlighter()

	next, err := dmi.Next()
	for err == nil && next != nil {
		err = next.VisitStoredFields(func(field string, value []byte) bool {
			if field == "status" {
				// will panic without fix
				_ = ansiHighligher.BestFragment(next.Locations["status"], value)
			}
			return true
		})
		if err != nil {
			t.Fatalf("error visiting stored fields: %v", err)
		}
		next, err = dmi.Next()
	}
	if err != nil {
		t.Fatalf("error iterating search results: %v", err)
	}

	if dmi.Aggregations().Count() != 1 {
		t.Fatalf("Expected 1 hit, got: %v", dmi.Aggregations().Count())
	}

	err = indexReader.Close()
	if err != nil {
		t.Fatal(err)
	}

	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestAllMatchesWithAggregationIssue31(_ *testing.T) {
	query := NewMatchQuery("bluge").SetField("name")
	request := NewAllMatches(query)

	// This line would panic because aggregations map was not initialized internally
	// should not panic with the fix
	request.AddAggregation("score", aggregations.MaxStartingAt(search.DocumentScore(), 0))
}

func TestNumericRangeSearchBoost(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	doc := NewDocument("doc").
		AddField(NewNumericField("age", 25.0))

	batch := NewBatch()
	batch.Update(doc.ID(), doc)

	if err = indexWriter.Batch(batch); err != nil {
		t.Fatal(err)
	}

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	// numeric range query with boost 5.0
	q := NewNumericRangeQuery(20, 30).SetField("age").SetBoost(5.0)
	sr := NewTopNSearch(10, q)
	res, err := indexReader.Search(context.Background(), sr)
	if err != nil {
		t.Fatal(err)
	}

	next, err := res.Next()
	if err != nil {
		t.Fatalf("error getting first hit")
	}
	if next == nil {
		t.Fatalf("expected at least one hit")
	}
	if next.Score != 5.0 {
		t.Fatalf("expected score to be 5.0, got %f", next.Score)
	}

	err = indexReader.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestBooleanSearchBoost(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	doc := NewDocument("doc").
		AddField(NewNumericField("age", 25.0))

	batch := NewBatch()
	batch.Update(doc.ID(), doc)

	if err = indexWriter.Batch(batch); err != nil {
		t.Fatal(err)
	}

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	// numeric range query with no boost
	nrq := NewNumericRangeQuery(20, 30).SetField("age")
	bq := NewBooleanQuery().AddMust(nrq).SetBoost(3.0)
	sr := NewTopNSearch(10, bq)
	res, err := indexReader.Search(context.Background(), sr)
	if err != nil {
		t.Fatal(err)
	}

	next, err := res.Next()
	if err != nil {
		t.Fatalf("error getting first hit")
	}
	if next == nil {
		t.Fatalf("expected at least one hit")
	}
	if next.Score != 3.0 {
		t.Fatalf("expected score to be 3.0, got %f", next.Score)
	}

	err = indexReader.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}
}
