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

package index

import (
	"errors"
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"
)

// dictErrSegment fails Dictionary(); dictEmptySegment yields an empty
// iterator whose Close we can observe.
type dictErrSegment struct {
	segment.Segment
	err error
}

func (s *dictErrSegment) Dictionary(string) (segment.Dictionary, error) { return nil, s.err }

type dictEmptySegment struct {
	segment.Segment
	itr *closeTrackingItr
}

func (s *dictEmptySegment) Dictionary(string) (segment.Dictionary, error) {
	return &emptyDict{itr: s.itr}, nil
}

type emptyDict struct {
	segment.Dictionary
	itr *closeTrackingItr
}

func (d *emptyDict) Iterator(segment.Automaton, []byte, []byte) segment.DictionaryIterator {
	return d.itr
}

type closeTrackingItr struct {
	segment.DictionaryIterator
	closed int
}

func (i *closeTrackingItr) Next() (segment.DictionaryEntry, error) { return nil, nil }
func (i *closeTrackingItr) Close() error                           { i.closed++; return nil }

func snapshotWith(segs ...segment.Segment) *Snapshot {
	s := &Snapshot{}
	for _, seg := range segs {
		s.segment = append(s.segment, &segmentSnapshot{segment: &segmentWrapper{Segment: seg}})
	}
	return s
}

// Two failing segments used to hit the else-branch with a nil iterator and
// panic on the second error; every error must instead be drained.
func TestNewDictionaryMultipleSegmentErrors(t *testing.T) {
	boom := errors.New("dictionary failure")
	s := snapshotWith(&dictErrSegment{err: boom}, &dictErrSegment{err: boom}, &dictErrSegment{err: boom})
	_, err := s.DictionaryIterator("f", nil, nil, nil)
	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want %v", err, boom)
	}
	if _, err = s.DictionaryLookup("f"); !errors.Is(err, boom) {
		t.Fatalf("lookup: got %v, want %v", err, boom)
	}
}

// An iterator that is empty on first Next never joins the heap and would
// otherwise never be closed, on both the success and the error path.
func TestNewDictionaryClosesEmptyIterators(t *testing.T) {
	okItr := &closeTrackingItr{}
	s := snapshotWith(&dictEmptySegment{itr: okItr})
	itr, err := s.DictionaryIterator("f", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next, err := itr.Next(); err != nil || next != nil {
		t.Fatalf("expected empty dictionary, got %v %v", next, err)
	}
	if okItr.closed != 1 {
		t.Fatalf("empty iterator closed %d times on success path, want 1", okItr.closed)
	}

	errItr := &closeTrackingItr{}
	s = snapshotWith(&dictEmptySegment{itr: errItr}, &dictErrSegment{err: errors.New("boom")})
	if _, err = s.DictionaryIterator("f", nil, nil, nil); err == nil {
		t.Fatal("expected error")
	}
	if errItr.closed != 1 {
		t.Fatalf("empty iterator closed %d times on error path, want 1", errItr.closed)
	}
}
