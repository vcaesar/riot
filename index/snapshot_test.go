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

package index

import (
	"math"
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"
)

func TestIndexReader(t *testing.T) {
	cfg, cleanup := CreateConfig("TestIndexReader")
	defer func() {
		err := cleanup()
		if err != nil {
			t.Log(err)
		}
	}()

	idx, err := OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err = idx.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()

	var expectedCount uint64
	doc := &FakeDocument{
		NewFakeField("_id", "1", true, false, false),
		NewFakeField("name", "test", false, false, true),
	}
	b := NewBatch()
	b.Update(testIdentifier("1"), doc)
	err = idx.Batch(b)
	if err != nil {
		t.Errorf("Error updating index: %v", err)
	}
	expectedCount++

	doc = &FakeDocument{
		NewFakeField("_id", "2", true, false, false),
		NewFakeField("name", "test test test", false, false, true),
		NewFakeField("desc", "eat more rice", false, true, true),
	}
	b2 := NewBatch()
	b2.Update(testIdentifier("2"), doc)
	err = idx.Batch(b2)
	if err != nil {
		t.Errorf("Error updating index: %v", err)
	}
	expectedCount++

	indexReader, err := idx.Reader()
	if err != nil {
		t.Error(err)
	}
	defer func() {
		err = indexReader.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()

	// first look for a term that doesn't exist
	reader, err := indexReader.PostingsIterator([]byte("nope"), "name", true, true, true)
	if err != nil {
		t.Errorf("Error accessing term field reader: %v", err)
	}
	count := reader.Count()
	if count != 0 {
		t.Errorf("Expected doc count to be: %d got: %d", 0, count)
	}
	err = reader.Close()
	if err != nil {
		t.Fatal(err)
	}

	reader, err = indexReader.PostingsIterator([]byte("test"), "name", true, true, true)
	if err != nil {
		t.Errorf("Error accessing term field reader: %v", err)
	}

	count = reader.Count()
	if count != expectedCount {
		t.Errorf("Exptected doc count to be: %d got: %d", expectedCount, count)
	}

	var match segment.Posting
	var actualCount uint64
	match, err = reader.Next()
	for err == nil && match != nil {
		match, err = reader.Next()
		if err != nil {
			t.Errorf("unexpected error reading next")
		}
		actualCount++
	}
	if actualCount != count {
		t.Errorf("count was 2, but only saw %d", actualCount)
	}

	tfr, err := indexReader.PostingsIterator([]byte("rice"), "desc", true, true, true)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	match, err = tfr.Next()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// remember the doc number for later in the test
	doc2Number := match.Number()

	if match.Frequency() != 1 {
		t.Errorf("expected match freq to be 1, got %d", match.Frequency())
	}
	expectedNorm := float64(math.Float32frombits(uint32(3)))
	if match.Norm() != expectedNorm {
		t.Errorf("expected match norm to be %f, got %f", expectedNorm, match.Norm())
	}
	if len(match.Locations()) != 1 {
		t.Errorf("expected 1 location, got %d", len(match.Locations()))
	} else {
		loc := match.Locations()[0]
		if loc.Field() != "desc" {
			t.Errorf("expected location field desc, got %s", loc.Field())
		}
		if loc.Pos() != 3 {
			t.Errorf("expected location pos 3, got %d", loc.Pos())
		}
		if loc.Start() != 9 {
			t.Errorf("expected location start 9, got %d", loc.Start())
		}
		if loc.End() != 13 {
			t.Errorf("expected location end 13, got %d", loc.End())
		}
	}

	err = reader.Close()
	if err != nil {
		t.Fatal(err)
	}

	// now test usage of advance
	reader, err = indexReader.PostingsIterator([]byte("test"), "name", true, true, true)
	if err != nil {
		t.Errorf("Error accessing term field reader: %v", err)
	}

	match, err = reader.Advance(doc2Number)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if match == nil {
		t.Fatalf("Expected match, got nil")
	}
	if match.Number() != doc2Number {
		t.Errorf("Expected doc number %d, got %d", doc2Number, match.Number())
	}
	// advance to a doc num that doesn't exist
	match, err = reader.Advance(600)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if match != nil {
		t.Errorf("expected nil, got %v", match)
	}
	err = reader.Close()
	if err != nil {
		t.Fatal(err)
	}

	// now test creating a reader for a field that doesn't exist
	reader, err = indexReader.PostingsIterator([]byte("water"), "doesnotexist", true, true, true)
	if err != nil {
		t.Errorf("Error accessing term field reader: %v", err)
	}
	count = reader.Count()
	if count != 0 {
		t.Errorf("expected count 0 for reader of non-existent field")
	}
	match, err = reader.Next()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if match != nil {
		t.Errorf("expected nil, got %v", match)
	}
	match, err = reader.Advance(600)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if match != nil {
		t.Errorf("expected nil, got %v", match)
	}
}

func TestIndexDocIdReader(t *testing.T) {
	cfg, cleanup := CreateConfig("TestIndexDocIdReader")
	defer func() {
		err := cleanup()
		if err != nil {
			t.Log(err)
		}
	}()

	idx, err := OpenWriter(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err = idx.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()

	var expectedCount uint64
	doc := &FakeDocument{
		NewFakeField("_id", "1", true, false, false),
		NewFakeField("name", "test", false, false, true),
	}
	b := NewBatch()
	b.Update(testIdentifier("1"), doc)
	err = idx.Batch(b)
	if err != nil {
		t.Errorf("Error updating index: %v", err)
	}
	expectedCount++

	doc = &FakeDocument{
		NewFakeField("_id", "2", true, false, false),
		NewFakeField("name", "test test test", false, false, true),
		NewFakeField("desc", "eat more rice", false, true, true),
	}
	b2 := NewBatch()
	b2.Update(testIdentifier("2"), doc)
	err = idx.Batch(b2)
	if err != nil {
		t.Errorf("Error updating index: %v", err)
	}
	expectedCount++

	indexReader, err := idx.Reader()
	if err != nil {
		t.Error(err)
	}
	defer func() {
		err = indexReader.Close()
		if err != nil {
			t.Error(err)
		}
	}()

	// first get all doc ids
	postingsIterator, err := indexReader.PostingsIterator(nil, "", false, false, false)
	if err != nil {
		t.Errorf("Error accessing doc id reader: %v", err)
	}
	defer func() {
		err = postingsIterator.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()

	tfd, err := postingsIterator.Next()
	if err != nil {
		t.Error(err)
	}
	count := uint64(0)
	var secondNumber uint64
	for tfd != nil {
		count++
		tfd, err = postingsIterator.Next()
		if err != nil {
			t.Error(err)
		}
		if secondNumber == 0 {
			secondNumber = tfd.Number()
		}
	}
	if count != expectedCount {
		t.Errorf("expected %d, got %d", expectedCount, count)
	}

	// try it again, but jump to the second doc this time
	postingsIterator2, err := indexReader.PostingsIterator(nil, "", false, false, false)
	if err != nil {
		t.Errorf("Error accessing doc id reader: %v", err)
	}
	defer func() {
		err = postingsIterator2.Close()
		if err != nil {
			t.Error(err)
		}
	}()

	tfd, err = postingsIterator2.Advance(secondNumber)
	if err != nil {
		t.Error(err)
	}
	if tfd.Number() != secondNumber {
		t.Errorf("expected to find number %d, got %d", secondNumber, tfd.Number())
	}

	// advance to a doc number that doesn't exist
	tfd, err = postingsIterator2.Advance(600)
	if err != nil {
		t.Error(err)
	}
	if tfd != nil {
		t.Errorf("expected to find no tfd, got %d", tfd.Number())
	}
}

func TestSegmentIndexAndLocalDocNumFromGlobal(t *testing.T) {
	tests := []struct {
		offsets      []uint64
		globalDocNum uint64
		segmentIndex int
		localDocNum  uint64
	}{
		// just 1 segment
		{
			offsets:      []uint64{0},
			globalDocNum: 0,
			segmentIndex: 0,
			localDocNum:  0,
		},
		{
			offsets:      []uint64{0},
			globalDocNum: 1,
			segmentIndex: 0,
			localDocNum:  1,
		},
		{
			offsets:      []uint64{0},
			globalDocNum: 25,
			segmentIndex: 0,
			localDocNum:  25,
		},
		// now 2 segments, 30 docs in first
		{
			offsets:      []uint64{0, 30},
			globalDocNum: 0,
			segmentIndex: 0,
			localDocNum:  0,
		},
		{
			offsets:      []uint64{0, 30},
			globalDocNum: 1,
			segmentIndex: 0,
			localDocNum:  1,
		},
		{
			offsets:      []uint64{0, 30},
			globalDocNum: 25,
			segmentIndex: 0,
			localDocNum:  25,
		},
		{
			offsets:      []uint64{0, 30},
			globalDocNum: 30,
			segmentIndex: 1,
			localDocNum:  0,
		},
		{
			offsets:      []uint64{0, 30},
			globalDocNum: 35,
			segmentIndex: 1,
			localDocNum:  5,
		},
		// lots of segments
		{
			offsets:      []uint64{0, 30, 40, 70, 99, 172, 800, 25000},
			globalDocNum: 0,
			segmentIndex: 0,
			localDocNum:  0,
		},
		{
			offsets:      []uint64{0, 30, 40, 70, 99, 172, 800, 25000},
			globalDocNum: 25,
			segmentIndex: 0,
			localDocNum:  25,
		},
		{
			offsets:      []uint64{0, 30, 40, 70, 99, 172, 800, 25000},
			globalDocNum: 35,
			segmentIndex: 1,
			localDocNum:  5,
		},
		{
			offsets:      []uint64{0, 30, 40, 70, 99, 172, 800, 25000},
			globalDocNum: 100,
			segmentIndex: 4,
			localDocNum:  1,
		},
		{
			offsets:      []uint64{0, 30, 40, 70, 99, 172, 800, 25000},
			globalDocNum: 825,
			segmentIndex: 6,
			localDocNum:  25,
		},
	}

	for _, test := range tests {
		i := &Snapshot{
			offsets: test.offsets,
			refs:    1,
		}
		gotSegmentIndex, gotLocalDocNum := i.segmentIndexAndLocalDocNumFromGlobal(test.globalDocNum)
		if gotSegmentIndex != test.segmentIndex {
			t.Errorf("got segment index %d expected %d for offsets %v globalDocNum %d", gotSegmentIndex, test.segmentIndex, test.offsets, test.globalDocNum)
		}
		if gotLocalDocNum != test.localDocNum {
			t.Errorf("got localDocNum %d expected %d for offsets %v globalDocNum %d", gotLocalDocNum, test.localDocNum, test.offsets, test.globalDocNum)
		}
		err := i.Close()
		if err != nil {
			t.Errorf("expected no err, got: %v", err)
		}
	}
}
