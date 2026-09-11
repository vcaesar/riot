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
	"bytes"
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vcaesar/riot/search"

	"github.com/vcaesar/riot/index"

	segment "github.com/vcaesar/bluge_segment_api"
)

type Fatalfable interface {
	Fatalf(format string, args ...interface{})
}

func createTmpIndexPath(f Fatalfable) string {
	tmpIndexPath, err := os.MkdirTemp("", "bluge-testidx")
	if err != nil {
		f.Fatalf("error creating temp dir: %v", err)
	}
	return tmpIndexPath
}

func cleanupTmpIndexPath(f Fatalfable, path string) {
	err := os.RemoveAll(path)
	if err != nil {
		f.Fatalf("error removing temp dir: %v", err)
	}
}

func TestCrud(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	docA := NewDocument("a").
		AddField(NewTextField("name", "marty").StoreValue()).
		AddField(NewTextField("desc", "gophercon india")).
		AddField(NewCompositeFieldExcluding("_all", nil))
	err = indexWriter.Update(docA.ID(), docA)
	if err != nil {
		t.Error(err)
	}

	docY := NewDocument("y").
		AddField(NewTextField("name", "jasper")).
		AddField(NewTextField("desc", "clojure"))
	err = indexWriter.Update(docY.ID(), docY)
	if err != nil {
		t.Error(err)
	}

	err = indexWriter.Delete(Identifier("y"))
	if err != nil {
		t.Error(err)
	}

	docX := NewDocument("x").
		AddField(NewTextField("name", "rose")).
		AddField(NewTextField("desc", "googler"))
	err = indexWriter.Update(docX.ID(), docX)
	if err != nil {
		t.Error(err)
	}

	docB := NewDocument("b").
		AddField(NewTextField("name", "steve")).
		AddField(NewTextField("desc", "cbft master"))
	batch := NewBatch()
	batch.Update(docB.ID(), docB)

	batch.Delete(Identifier("x"))
	err = indexWriter.Batch(batch)
	if err != nil {
		t.Error(err)
	}

	// close the indexWriter, open it again, and try some more things
	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}

	indexWriter, err = OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err = indexWriter.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting reader from indexWriter writer")
	}
	defer func() {
		err = indexReader.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()

	count, err := indexReader.Count()
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("expected doc count 2, got %d", count)
	}

	docANumber, err := docNumberForTerm(indexReader, Identifier("a"))
	if err != nil {
		t.Fatalf("error finding doc number for term: %v", err)
	}

	var foundName []byte
	err = indexReader.VisitStoredFields(docANumber, func(field string, value []byte) bool {
		if field == "name" {
			copy(foundName, value)
			return false
		}
		return true
	})
	if err != nil {
		t.Fatalf("error visiting stored fields: %v", err)
	}

	if bytes.Equal(foundName, []byte("marty")) {
		t.Errorf("expected to find field named 'name' with value 'marty'")
	}

	fields, err := indexReader.Fields()
	if err != nil {
		t.Fatal(err)
	}
	expectedFields := map[string]bool{
		"_all": false,
		"name": false,
		"desc": false,
	}
	if len(fields) < len(expectedFields) {
		t.Fatalf("expected %d fields got %d", len(expectedFields), len(fields))
	}
	for _, f := range fields {
		expectedFields[f] = true
	}
	for ef, efp := range expectedFields {
		if !efp {
			t.Errorf("field %s is missing", ef)
		}
	}
}

func docNumberForTerm(r *Reader, t segment.Term) (uint64, error) {
	q := NewTermQuery(string(t.Term())).SetField(t.Field())
	req := NewTopNSearch(1, q)
	dmi, err := r.Search(context.Background(), req)
	if err != nil {
		return 0, err
	}
	next, err := dmi.Next()
	if err != nil {
		return 0, err
	}
	return next.Number, nil
}

func TestMultipleClose(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	err = indexWriter.Close()
	if err != nil {
		t.Fatalf("expected first close to work, got: %v", err)
	}

	err = indexWriter.Close()
	if err != nil {
		t.Fatalf("expected second close to work, got: %v", err)
	}
}

type slowQuery struct {
	actual Query
	delay  time.Duration
}

func (s *slowQuery) Searcher(i search.Reader,
	options search.SearcherOptions) (search.Searcher, error) {
	time.Sleep(s.delay)
	return s.actual.Searcher(i, options)
}

func TestStoredFieldPreserved(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	docA := NewDocument("a").
		AddField(NewTextField("name", "Marty").StoreValue()).
		AddField(NewTextField("desc", "GopherCON India").StoreValue()).
		AddField(NewKeywordField("bool", "t").StoreValue()).
		AddField(NewNumericField("num", 1.0).StoreValue()).
		AddField(NewCompositeFieldExcluding("_all", []string{"_id"}))

	err = indexWriter.Update(docA.ID(), docA)
	if err != nil {
		t.Error(err)
	}

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	q := NewTermQuery("marty")
	req := NewTopNSearch(1, q).WithStandardAggregations()
	dmi, err := indexReader.Search(context.Background(), req)
	if err != nil {
		t.Error(err)
	}

	next, err := dmi.Next()
	if err != nil {
		t.Fatalf("error getting next hit: %v", err)
	}
	if next == nil {
		t.Fatal("nil result, expected at least one", err)
	}

	err = indexReader.VisitStoredFields(next.Number, func(field string, value []byte) bool {
		switch field {
		case "name":
			if !bytes.Equal(value, []byte("Marty")) {
				t.Fatalf("expected 'Marty' got '%s'", string(value))
			}
		case "desc":
			if !bytes.Equal(value, []byte("GopherCON India")) {
				t.Fatalf("expected 'GopherCON India' got '%s'", string(value))
			}
		case "num":
			num, err2 := DecodeNumericFloat64(value)
			if err2 != nil {
				t.Fatalf("error decoding float: %v", err2)
			}
			if num != 1 {
				t.Fatalf("expected 1 got '%f'", num)
			}
		case "bool":
			if !bytes.Equal(value, []byte("t")) {
				t.Fatalf("expected t' got '%s'", string(value))
			}
		}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}

	if dmi.Aggregations().Count() != 1 {
		t.Errorf("expected 1 hit, got %d", dmi.Aggregations().Count())
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

func TestDict(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	docA := NewDocument("a").
		AddField(NewTextField("name", "marty")).
		AddField(NewTextField("desc", "gophercon india"))
	err = indexWriter.Update(docA.ID(), docA)
	if err != nil {
		t.Error(err)
	}

	docY := NewDocument("y").
		AddField(NewTextField("name", "jasper")).
		AddField(NewTextField("desc", "clojure"))
	err = indexWriter.Update(docY.ID(), docY)
	if err != nil {
		t.Error(err)
	}

	docX := NewDocument("x").
		AddField(NewTextField("name", "rose")).
		AddField(NewTextField("desc", "googler"))
	err = indexWriter.Update(docX.ID(), docX)
	if err != nil {
		t.Error(err)
	}

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	dict, err := indexReader.DictionaryIterator("name", nil, nil, nil)
	if err != nil {
		t.Error(err)
	}

	var terms []string
	de, err := dict.Next()
	for err == nil && de != nil {
		terms = append(terms, de.Term())
		de, err = dict.Next()
	}

	expectedTerms := []string{"jasper", "marty", "rose"}
	if !reflect.DeepEqual(terms, expectedTerms) {
		t.Errorf("expected %v, got %v", expectedTerms, terms)
	}

	err = dict.Close()
	if err != nil {
		t.Fatal(err)
	}

	// test start and end range
	dict, err = indexReader.DictionaryIterator("name", nil, []byte("marty"), []byte("zeus"))
	if err != nil {
		t.Error(err)
	}

	terms = terms[:0]
	de, err = dict.Next()
	for err == nil && de != nil {
		terms = append(terms, de.Term())
		de, err = dict.Next()
	}

	expectedTerms = []string{"marty", "rose"}
	if !reflect.DeepEqual(terms, expectedTerms) {
		t.Errorf("expected %v, got %v", expectedTerms, terms)
	}

	err = dict.Close()
	if err != nil {
		t.Fatal(err)
	}

	err = indexReader.Close()
	if err != nil {
		t.Fatalf("error closing index reader")
	}

	docZ := NewDocument("z").
		AddField(NewTextField("name", "prefix")).
		AddField(NewTextField("desc", "bob cat cats catting dog doggy zoo"))
	err = indexWriter.Update(docZ.ID(), docZ)
	if err != nil {
		t.Error(err)
	}

	// open a new reader
	indexReader, err = indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	dict, err = indexReader.DictionaryIterator("desc", nil, []byte("cat"), []byte("cau"))
	if err != nil {
		t.Error(err)
	}

	terms = terms[:0]
	de, err = dict.Next()
	for err == nil && de != nil {
		terms = append(terms, de.Term())
		de, err = dict.Next()
	}

	expectedTerms = []string{"cat", "cats", "catting"}
	if !reflect.DeepEqual(terms, expectedTerms) {
		t.Errorf("expected %v, got %v", expectedTerms, terms)
	}

	err = dict.Close()
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

func TestIndexMetadataRaceBug198(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	wg := sync.WaitGroup{}
	wg.Add(1)
	done := make(chan struct{})
	go func() {
		indexReader, err2 := indexWriter.Reader()
		if err2 != nil {
			panic(err2)
		}
		for {
			select {
			case <-done:
				err2 = indexReader.Close()
				if err2 != nil {
					panic(err2)
				}
				wg.Done()
				return
			default:

				_, err2 = indexReader.Count()
				if err2 != nil {
					panic(err2)
				}
			}
		}
	}()

	for i := 0; i < 100; i++ {
		batch := index.NewBatch()
		doc := NewDocument("a")
		batch.Update(doc.ID(), doc)
		err = indexWriter.Batch(batch)
		if err != nil {
			t.Fatal(err)
		}
	}
	close(done)
	wg.Wait()
	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestSortMatchSearch(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	names := []string{"Noam", "Uri", "David", "Yosef", "Eitan", "Itay", "Ariel", "Daniel", "Omer", "Yogev", "Yehonatan", "Moshe", "Mohammed", "Yusuf", "Omar"}
	days := []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	numbers := []string{"One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine", "Ten", "Eleven", "Twelve"}
	b := index.NewBatch()
	for i := 0; i < 200; i++ {
		doc := NewDocument(fmt.Sprintf("%d", i)).
			AddField(NewKeywordField("Name", names[i%len(names)]).StoreValue()).
			AddField(NewKeywordField("day", days[i%len(days)]).StoreValue()).
			AddField(NewKeywordField("number", numbers[i%len(numbers)]).StoreValue())
		b.Update(doc.ID(), doc)
	}
	err = indexWriter.Batch(b)
	if err != nil {
		t.Fatal(err)
	}

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	req := NewTopNSearch(10, NewMatchQuery("One"))
	req.SortBy([]string{"Day", "Name"})
	dmi, err := indexReader.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	prev := ""

	next, err := dmi.Next()
	for err == nil && next != nil {
		err = next.VisitStoredFields(func(field string, value []byte) bool {
			if field == "Day" {
				if prev > string(value) {
					t.Errorf("Hits must be sorted by 'Day'. Found '%s' before '%s'", prev, string(value))
				}
				prev = string(value)
			}
			return true
		})
		if err != nil {
			t.Fatalf("error accessing stored fields: %v", err)
		}
	}
	if err != nil {
		t.Fatalf("error iterating results: %v", err)
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

func TestIndexCountMatchSearch(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			b := NewBatch()
			for j := 0; j < 200; j++ {
				id := fmt.Sprintf("%d", (i*200)+j)
				doc := NewDocument(id).
					AddField(NewTextField("Body", "match")).
					AddField(NewCompositeFieldExcluding("_all", []string{"_id"}))
				b.Update(doc.ID(), doc)
			}
			err2 := indexWriter.Batch(b)
			if err != nil {
				panic(err2)
			}
			wg.Done()
		}(i)
	}
	wg.Wait()

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	// search for something that should match all documents
	sr, err := indexReader.Search(context.Background(), NewTopNSearch(10, NewMatchQuery("match")).WithStandardAggregations())
	if err != nil {
		t.Fatal(err)
	}

	// get the index document count
	dc, err := indexReader.Count()
	if err != nil {
		t.Fatal(err)
	}

	// make sure test is working correctly, doc count should 2000
	if dc != 2000 {
		t.Errorf("expected doc count 2000, got %d", dc)
	}

	// make sure our search found all the documents
	if dc != sr.Aggregations().Count() {
		t.Errorf("expected search result total %d to match doc count %d", sr.Aggregations().Count(), dc)
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

func TestSearchTimeout(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	// first run a search with an absurdly long timeout (should succeed)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	query := NewTermQuery("water")
	req := NewTopNSearch(10, query)
	_, err = indexReader.Search(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	// now run a search again with an absurdly low timeout (should timeout)
	ctx, cancel = context.WithTimeout(context.Background(), 1*time.Microsecond)
	defer cancel()
	sq := &slowQuery{
		actual: query,
		delay:  50 * time.Millisecond, // on Windows timer resolution is 15ms
	}
	sQuery := sq
	req = NewTopNSearch(10, sQuery)
	_, err = indexReader.Search(ctx, req)
	if err != context.DeadlineExceeded {
		t.Fatalf("exected %v, got: %v", context.DeadlineExceeded, err)
	}

	// now run a search with a long timeout, but with a long query, and cancel it
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	sq = &slowQuery{
		actual: query,
		delay:  100 * time.Millisecond, // on Windows timer resolution is 15ms
	}
	req = NewTopNSearch(10, sq)
	cancel()
	_, err = indexReader.Search(ctx, req)
	if err != context.Canceled {
		t.Fatalf("exected %v, got: %v", context.Canceled, err)
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

func TestBatchRaceBug260(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}
	b := NewBatch()
	b.Update(Identifier("1"), NewDocument("1"))
	err = indexWriter.Batch(b)
	if err != nil {
		t.Fatal(err)
	}
	b.Reset()
	b.Update(Identifier("2"), NewDocument("2"))
	err = indexWriter.Batch(b)
	if err != nil {
		t.Fatal(err)
	}
	b.Reset()
	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func BenchmarkBatchOverhead(b *testing.B) {
	tmpIndexPath := createTmpIndexPath(b)
	defer cleanupTmpIndexPath(b, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		b.Fatal(err)
	}
	for n := 0; n < b.N; n++ {
		// put 1000 items in a batch
		batch := NewBatch()
		for i := 0; i < 1000; i++ {
			doc := NewDocument(fmt.Sprintf("%d", i)).
				AddField(NewKeywordField("name", "bluge"))
			batch.Update(doc.ID(), doc)
		}
		err = indexWriter.Batch(batch)
		if err != nil {
			b.Fatal(err)
		}
		batch.Reset()
	}
}

func TestOpenMultipleReaders(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	docA := NewDocument("a").
		AddField(NewKeywordField("name", "marty")).
		AddField(NewKeywordField("desc", "gophercon india"))
	err = indexWriter.Update(docA.ID(), docA)
	if err != nil {
		t.Fatal(err)
	}

	// close the index writer
	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}

	// now open a reader
	indexReader, err := OpenReader(config)
	if err != nil {
		t.Fatal(err)
	}

	// now open it again
	indexReader2, err := OpenReader(config)
	if err != nil {
		t.Fatal(err)
	}

	err = indexReader.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = indexReader2.Close()
	if err != nil {
		t.Fatal(err)
	}
}

// TestBug408 tests for VERY large values of size, even though actual result
// set may be reasonable size
func TestBug408(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	numToTest := 10
	matchUserID := "match"
	noMatchUserID := "no_match"
	matchingDocIDs := make(map[string]struct{})

	for i := 0; i < numToTest; i++ {
		id := strconv.Itoa(i)
		doc := NewDocument(id)
		if i%2 == 0 {
			doc.AddField(NewKeywordField("user_id", noMatchUserID))
		} else {
			doc.AddField(NewKeywordField("user_id", matchUserID))
			matchingDocIDs[id] = struct{}{}
		}
		err = indexWriter.Update(doc.ID(), doc)
		if err != nil {
			t.Fatal(err)
		}
	}

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	cnt, err := indexReader.Count()
	if err != nil {
		t.Fatal(err)
	}
	if cnt != uint64(numToTest) {
		t.Fatalf("expected %d documents in index, got %d", numToTest, cnt)
	}

	q := NewTermQuery(matchUserID)
	q.SetField("user_id")
	searchReq := NewTopNSearch(math.MaxInt32, q).WithStandardAggregations()
	dmi, err := indexReader.Search(context.Background(), searchReq)
	if err != nil {
		t.Fatal(err)
	}

	next, err := dmi.Next()
	for err == nil && next != nil {
		err = next.VisitStoredFields(func(field string, value []byte) bool {
			if field == "_id" {
				if _, found := matchingDocIDs[string(value)]; !found {
					t.Fatalf("document with ID %s not in results as expected", string(value))
				}
			}
			return true
		})
		if err != nil {
			t.Fatal(err)
		}
		next, err = dmi.Next()
	}
	if err != nil {
		t.Fatal(err)
	}

	if dmi.Aggregations().Count() != uint64(numToTest/2) {
		t.Fatalf("expected %d search hits, got %d", numToTest/2, dmi.Aggregations().Count())
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

func TestIndexAdvancedCountMatchSearch(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			b := NewBatch()
			for j := 0; j < 200; j++ {
				id := fmt.Sprintf("%d", (i*200)+j)
				doc := NewDocument(id).
					AddField(NewKeywordField("body", "match")).
					AddField(NewCompositeFieldExcluding("_all", []string{"_id"}))

				b.Update(doc.ID(), doc)
			}
			err2 := indexWriter.Batch(b)
			if err2 != nil {
				panic(err2)
			}
			wg.Done()
		}(i)
	}
	wg.Wait()

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	// search for something that should match all documents
	req := NewTopNSearch(10, NewMatchQuery("match")).WithStandardAggregations()
	dmi, err := indexReader.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	// get the index document count
	dc, err := indexReader.Count()
	if err != nil {
		t.Fatal(err)
	}

	// make sure test is working correctly, doc count should 2000
	if dc != 2000 {
		t.Errorf("expected doc count 2000, got %d", dc)
	}

	// make sure our search found all the documents
	if dc != dmi.Aggregations().Count() {
		t.Errorf("expected search result total %d to match doc count %d", dmi.Aggregations().Count(), dc)
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

func BenchmarkScorchSearchOverhead(b *testing.B) {
	tmpIndexPath := createTmpIndexPath(b)
	defer cleanupTmpIndexPath(b, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		b.Fatal(err)
	}

	elements := []string{"air", "water", "fire", "earth"}
	batch := NewBatch()
	for j := 1; j <= 10000; j++ {
		id := fmt.Sprintf("%d", j)
		batch.Update(Identifier(id),
			NewDocument(id).
				AddField(
					NewKeywordField("name", elements[j%len(elements)])))

		if j%1000 == 0 {
			err = indexWriter.Batch(batch)
			if err != nil {
				b.Fatal(err)
			}
		}
	}

	query1 := NewTermQuery("water")
	query2 := NewTermQuery("fire")
	query := NewBooleanQuery().AddShould(query1, query2)
	req := NewTopNSearch(10, query)

	indexReader, err := indexWriter.Reader()
	if err != nil {
		b.Fatalf("error getting indead reader: %v", err)
	}

	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		_, err = indexReader.Search(context.Background(), req)
		if err != nil {
			b.Fatal(err)
		}
	}

	err = indexReader.Close()
	if err != nil {
		b.Fatal(err)
	}
	err = indexWriter.Close()
	if err != nil {
		b.Fatal(err)
	}
}

func TestSearchQueryCallback(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	expErr := fmt.Errorf("MEM_LIMIT_EXCEEDED")
	f := func(_ uint64) error {
		// the intended usage of this callback is to see the estimated
		// memory usage before executing, and possibly abort early
		// in this test we simulate returning such an error
		return expErr
	}

	config := DefaultConfig(tmpIndexPath).
		WithSearchStartFunc(f)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	query := NewTermQuery("water")
	req := NewTopNSearch(10, query)
	_, err = indexReader.Search(context.Background(), req)
	if err != expErr {
		t.Fatalf("Expected: %v, Got: %v", expErr, err)
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

func TestBug1096(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	// create a single batch instance that we will reuse
	// this should be safe because we have single goroutine
	// and we always wait for batch execution to finish
	batch := NewBatch()

	// number of batches to execute
	for i := 0; i < 10; i++ {
		// number of documents to put into the batch
		for j := 0; j < 91; j++ {
			// create a doc id 0-90 (important so that we get id's 9 and 90)
			// this could duplicate something already in the index
			//   this too should be OK and update the item in the index
			id := fmt.Sprintf("%d", j)
			doc := NewDocument(id).
				AddField(NewKeywordField("name", id)).
				AddField(NewKeywordField("batch", fmt.Sprintf("%d", i)))
			batch.Update(doc.ID(), doc)
		}

		// execute the batch
		err = indexWriter.Batch(batch)
		if err != nil {
			t.Fatal(err)
		}

		// reset the batch before reusing it
		batch.Reset()
	}

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	// search for docs having name starting with the number 9
	q := NewWildcardQuery("9*")
	q.SetField("name")
	req := NewTopNSearch(1000, q).WithStandardAggregations()
	dmi, err := indexReader.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	var matchIDs []string
	next, err := dmi.Next()
	for err == nil && next != nil {
		err = next.VisitStoredFields(func(field string, value []byte) bool {
			if field == "_id" {
				matchIDs = append(matchIDs, string(value))
			}
			return true
		})
		if err != nil {
			t.Fatalf("error visiting stored fields: %v", err)
		}
		next, err = dmi.Next()
	}
	if err != nil {
		t.Fatalf("error iterating results: %v", err)
	}

	// we expect only 2 hits, for docs 9 and 90
	if dmi.Aggregations().Count() > 2 {
		t.Fatalf("expected only 2 hits '9' and '90', got %v", matchIDs)
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

func TestDataRaceBug1092(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	batch := NewBatch()
	for i := 0; i < 10; i++ {
		err = indexWriter.Batch(batch)
		if err != nil {
			t.Error(err)
		}
		batch.Reset()
	}

	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestBatchRaceBug1149(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}
	b := NewBatch()
	b.Delete(Identifier("1"))
	err = indexWriter.Batch(b)
	if err != nil {
		t.Fatal(err)
	}
	b.Reset()
	err = indexWriter.Batch(b)
	if err != nil {
		t.Fatal(err)
	}
	b.Reset()
	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestBackup(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	docA := NewDocument("a").
		AddField(NewTextField("name", "marty").StoreValue()).
		AddField(NewTextField("desc", "gophercon india")).
		AddField(NewCompositeFieldExcluding("_all", nil))
	err = indexWriter.Update(docA.ID(), docA)
	if err != nil {
		t.Error(err)
	}

	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}

	snapshotReader, err := OpenReader(config)
	if err != nil {
		t.Fatalf("error opening snapshot reader: %v", err)
	}

	tmpBackupPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpBackupPath)

	err = snapshotReader.Backup(tmpBackupPath, nil)
	if err != nil {
		t.Fatalf("error backing up index: %v", err)
	}

	err = snapshotReader.Close()
	if err != nil {
		t.Fatalf("error closing snapshot reader: %v", err)
	}

	// open up the backup
	config = DefaultConfig(tmpBackupPath)
	snapshotReader, err = OpenReader(config)
	if err != nil {
		t.Fatal(err)
	}

	q := NewMatchQuery("marty").SetField("name")
	req := NewTopNSearch(10, q).WithStandardAggregations()
	dmi, err := snapshotReader.Search(context.Background(), req)
	if err != nil {
		t.Fatalf("error searching: %v", err)
	}
	if dmi.Aggregations().Count() != 1 {
		t.Errorf("expected 1 match, got %d", dmi.Aggregations().Count())
	}

	err = snapshotReader.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestOptimisedConjunctionSearchHits(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath).DisableOptimizeDisjunctionUnadorned()
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	docA := NewDocument("a").
		AddField(NewTextField("country", "united")).
		AddField(NewTextField("name", "Mercure Hotel")).
		AddField(NewTextField("directions", "B560 and B56 Follow signs to the M56")).
		AddField(NewCompositeFieldExcluding("_all", nil))

	docB := NewDocument("b").
		AddField(NewTextField("country", "united")).
		AddField(NewTextField("name", "Mercure Altrincham Bowdon Hotel")).
		AddField(NewTextField("directions", "A570 and A57 Follow signs to the M56 Manchester Airport")).
		AddField(NewCompositeFieldExcluding("_all", nil))

	docC := NewDocument("c").
		AddField(NewTextField("country", "india united")).
		AddField(NewTextField("name", "Sonoma Hotel")).
		AddField(NewTextField("directions", "Northwest")).
		AddField(NewCompositeFieldExcluding("_all", nil))

	docD := NewDocument("d").
		AddField(NewTextField("country", "United Kingdom")).
		AddField(NewTextField("name", "Cresta Court Hotel")).
		AddField(NewTextField("directions", "junction of A560 and A56")).
		AddField(NewCompositeFieldExcluding("_all", nil))

	b := NewBatch()
	b.Update(docA.ID(), docA)
	b.Update(docB.ID(), docB)
	b.Update(docC.ID(), docC)
	b.Update(docD.ID(), docD)
	// execute the batch
	err = indexWriter.Batch(b)
	if err != nil {
		t.Fatal(err)
	}

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting index reader: %v", err)
	}

	mq := NewMatchQuery("united")
	mq.SetField("country")

	bq := NewBooleanQuery()
	bq.AddMust(mq)

	mq1 := NewMatchQuery("hotel")
	mq1.SetField("name")
	bq.AddMust(mq1)

	mq2 := NewMatchQuery("56")
	mq2.SetField("directions")
	mq2.SetFuzziness(1)
	bq.AddMust(mq2)

	req := NewTopNSearch(10, bq).WithStandardAggregations().SetScore("none")

	res, err := indexReader.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	hitsWithOutScore := res.Aggregations().Count()

	req = NewTopNSearch(10, bq).WithStandardAggregations()

	res, err = indexReader.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	hitsWithScore := res.Aggregations().Count()

	if hitsWithOutScore != hitsWithScore {
		t.Errorf("expected %d hits without score, got %d", hitsWithScore, hitsWithOutScore)
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

func TestInMemoryWriterDataRace(t *testing.T) {
	cfg := InMemoryOnlyConfig()
	w, err := OpenWriter(cfg)
	if err != nil {
		t.Fatalf("unable to open in memory writer: %+v", err)
	}
	for i := 0; i < 5; i++ {
		b := batchAddDocs(2)
		err = w.Batch(b)
		if err != nil {
			t.Fatalf("failed to add random docs: %+v", err)
		}
	}
}

func TestInMemoryUsage(t *testing.T) {
	cfg := InMemoryOnlyConfig()
	w, err := OpenWriter(cfg)
	if err != nil {
		t.Fatalf("unable to open in memory writer: %+v", err)
	}

	doc := NewDocument("town:1")
	doc.AddField(NewTextField("en", "Denia, Alicante"))

	err = w.Insert(doc)
	if err != nil {
		t.Fatalf("error updating document: %v", err)
	}

	defer func() {
		err = w.Close()
		if err != nil {
			t.Fatalf("error closing writer: %v", err)
		}
	}()

	reader, err := w.Reader()
	if err != nil {
		t.Fatalf("unable to open reader: %v", err)
	}

	defer func() {
		err = reader.Close()
		if err != nil {
			t.Fatalf("error closing reader: %v", err)
		}
	}()

	query := NewMatchQuery("denia")
	query.SetField("en")

	req := NewTopNSearch(5, query)

	dmi, err := reader.Search(context.Background(), req)
	if err != nil {
		t.Fatalf("error executing search: %v", err)
	}

	var found bool
	next, err := dmi.Next()
	for err == nil && next != nil {
		err = next.VisitStoredFields(func(field string, value []byte) bool {
			if field == "_id" && string(value) == "town:1" {
				found = true
			}
			return true
		})
		if err != nil {
			t.Fatalf("error accessing stored fields: %v", err)
		}
		next, err = dmi.Next()
	}
	if err != nil {
		t.Fatalf("error iterating results: %v", err)
	}

	if !found {
		t.Fatalf("expected to match doc _id 'town:1', did not")
	}
}

func batchAddDocs(docCount int) *index.Batch {
	batch := NewBatch()

	for i := 0; i < docCount; i++ {
		doc := randomDoc()
		batch.Update(doc.ID(), doc)
	}
	return batch
}

var (
	field1 = randStr()
	field2 = randStr()
)

func randomDoc() *Document {
	return NewDocument(randStr()).
		AddField(NewTextField(field1, randStr())).
		AddField(NewTextField(field2, randStr()))
}

const charset = "01234567890abcdefghijklmnopqrstuvwxyz<>{}[];'"
const maxRandStrLen = 30

func randStrn(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteByte(charset[rand.Intn(len(charset))])
	}

	return b.String()
}

func randStr() string {
	return randStrn(maxRandStrLen)
}

func TestBug54(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	// first index 2 documents
	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	doc := NewDocument("a1")
	doc.AddField(NewTextField("TestKey1", "TestKey Data1"))
	if err = indexWriter.Update(doc.ID(), doc); err != nil {
		t.Fatal(err)
	}

	doc = NewDocument("a2")
	doc.AddField(NewTextField("TestKey2", "TestKey Data2"))
	if err = indexWriter.Update(doc.ID(), doc); err != nil {
		t.Fatal(err)
	}

	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}

	// now delete both documents
	indexWriter, err = OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	if err = indexWriter.Delete(Identifier("a1")); err != nil {
		t.Fatal(err)
	}

	if err = indexWriter.Delete(Identifier("a2")); err != nil {
		t.Fatal(err)
	}

	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}

	// now open index again
	indexWriter, err = OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}
	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestSearchSizeZeroWithAggregations(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	docA := NewDocument("a").
		AddField(NewTextField("name", "marty").StoreValue()).
		AddField(NewTextField("desc", "gophercon india")).
		AddField(NewCompositeFieldExcluding("_all", nil))
	err = indexWriter.Update(docA.ID(), docA)
	if err != nil {
		t.Error(err)
	}
	defer func() {
		err = indexWriter.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting reader from indexWriter writer")
	}
	defer func() {
		err = indexReader.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()

	q := NewMatchAllQuery()
	req := NewTopNSearch(0, q).WithStandardAggregations()

	dmi, err := indexReader.Search(context.Background(), req)
	if err != nil {
		t.Fatalf("error executing search: %v", err)
	}

	var sawHit bool
	next, err := dmi.Next()
	for err == nil && next != nil {
		sawHit = true
		next, err = dmi.Next()
	}
	if err != nil {
		t.Fatalf("error iterating results: %v", err)
	}

	// with size 0, expect no hits
	if sawHit {
		t.Errorf("size 0, but saw a hit")
	}

	// assert that aggregations were computed, even with size 0
	if dmi.Aggregations().Count() != 1 {
		t.Errorf("expected count 1, got %d", dmi.Aggregations().Count())
	}
}

func TestCrudWithNoMMap(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfigWithDirectory(func() index.Directory {
		dir := index.NewFileSystemDirectory(tmpIndexPath)
		dir.SetLoadMMapFunc(index.LoadMMapNever)
		return dir
	})

	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}

	docA := NewDocument("a").
		AddField(NewTextField("name", "marty").StoreValue()).
		AddField(NewTextField("desc", "gophercon india")).
		AddField(NewCompositeFieldExcluding("_all", nil))
	err = indexWriter.Update(docA.ID(), docA)
	if err != nil {
		t.Error(err)
	}

	docY := NewDocument("y").
		AddField(NewTextField("name", "jasper")).
		AddField(NewTextField("desc", "clojure"))
	err = indexWriter.Update(docY.ID(), docY)
	if err != nil {
		t.Error(err)
	}

	err = indexWriter.Delete(Identifier("y"))
	if err != nil {
		t.Error(err)
	}

	docX := NewDocument("x").
		AddField(NewTextField("name", "rose")).
		AddField(NewTextField("desc", "googler"))
	err = indexWriter.Update(docX.ID(), docX)
	if err != nil {
		t.Error(err)
	}

	docB := NewDocument("b").
		AddField(NewTextField("name", "steve")).
		AddField(NewTextField("desc", "cbft master"))
	batch := NewBatch()
	batch.Update(docB.ID(), docB)

	batch.Delete(Identifier("x"))
	err = indexWriter.Batch(batch)
	if err != nil {
		t.Error(err)
	}

	// close the indexWriter, open it again, and try some more things
	err = indexWriter.Close()
	if err != nil {
		t.Fatal(err)
	}

	indexWriter, err = OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err = indexWriter.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()

	indexReader, err := indexWriter.Reader()
	if err != nil {
		t.Fatalf("error getting reader from indexWriter writer")
	}
	defer func() {
		err = indexReader.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()

	count, err := indexReader.Count()
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("expected doc count 2, got %d", count)
	}

	docANumber, err := docNumberForTerm(indexReader, Identifier("a"))
	if err != nil {
		t.Fatalf("error finding doc number for term: %v", err)
	}

	var foundName []byte
	err = indexReader.VisitStoredFields(docANumber, func(field string, value []byte) bool {
		if field == "name" {
			copy(foundName, value)
			return false
		}
		return true
	})
	if err != nil {
		t.Fatalf("error visiting stored fields: %v", err)
	}

	if bytes.Equal(foundName, []byte("marty")) {
		t.Errorf("expected to find field named 'name' with value 'marty'")
	}

	fields, err := indexReader.Fields()
	if err != nil {
		t.Fatal(err)
	}
	expectedFields := map[string]bool{
		"_all": false,
		"name": false,
		"desc": false,
	}
	if len(fields) < len(expectedFields) {
		t.Fatalf("expected %d fields got %d", len(expectedFields), len(fields))
	}
	for _, f := range fields {
		expectedFields[f] = true
	}
	for ef, efp := range expectedFields {
		if !efp {
			t.Errorf("field %s is missing", ef)
		}
	}
}

// TestBug87 reproduces a situation in which a search matches several documents
// and we compare the document's stored value for the _id field, with the
// document's sort value.  The sort value should be the same _id, but
// comes from the doc values storage.
// In this case, because doc values were loaded from multiple chunks, an
// "uncompressed" buffer is reused.  Incorrect use of of these doc values
// bytes in computed sort values may lead to incorrect sort order and other
// undesired behavior.
func TestBug87(t *testing.T) {
	tmpIndexPath := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, tmpIndexPath)

	config := DefaultConfig(tmpIndexPath)
	indexWriter, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err = indexWriter.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()

	// create 1025 documents in a batch
	// this should require more than one chunk in doc values
	batch := NewBatch()
	for i := 0; i < 1025; i++ {
		doc := NewDocument(fmt.Sprintf("%d", i)).
			AddField(NewTextField("name", "marty").Sortable())
		batch.Update(doc.ID(), doc)
	}

	err = indexWriter.Batch(batch)
	if err != nil {
		t.Error(err)
	}

	reader, err := indexWriter.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err = reader.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()

	q := NewTermQuery("marty").SetField("name")
	req := NewTopNSearch(2000, q).SortBy([]string{"_id"})

	dmi, err := reader.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	next, err := dmi.Next()
	for err == nil && next != nil {
		var id string
		err = next.VisitStoredFields(func(field string, value []byte) bool {
			if field == "_id" {
				id = string(value)
				return false
			}
			return true
		})
		if err != nil {
			t.Fatal(err)
		}
		if string(next.SortValue[0]) != id {
			t.Fatalf("expected id '%s' to match sort value '%s'", id, string(next.SortValue[0]))
		}
		next, err = dmi.Next()
	}
	if err != nil {
		t.Fatal(err)
	}
}
