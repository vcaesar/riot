//  Copyright (c) 2020 The Bluge Authors.
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
	"fmt"
	"sort"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/similarity"

	segment "github.com/vcaesar/bluge_segment_api"
)

type thingLoc struct {
	fieldVal    string
	startVal    int
	endVal      int
	positionVal int
}

type thingFreq struct {
	term string
	locs []*thingLoc
	freq int
}

type thing struct {
	num    uint64
	freq   *thingFreq
	length int
}

type stubList []*thing

type stubDict map[string]stubList

func (s stubDict) Contains(key []byte) (bool, error) {
	if _, ok := s[string(key)]; ok {
		return true, nil
	}
	return false, nil
}

func (s stubDict) Close() error {
	return nil
}

type stubIndexReader struct {
	inv       map[string]stubDict
	doc       map[uint64]segment.Document
	docExtInt map[string]uint64
	count     uint64
	uninv     map[uint64]map[string][]string

	fieldDocs  map[string]uint64
	fieldFreqs map[string]uint64
}

func newStubIndexReader() *stubIndexReader {
	return &stubIndexReader{
		inv:        make(map[string]stubDict),
		doc:        make(map[uint64]segment.Document),
		docExtInt:  make(map[string]uint64),
		uninv:      make(map[uint64]map[string][]string),
		fieldDocs:  make(map[string]uint64),
		fieldFreqs: make(map[string]uint64),
	}
}

func (s *stubIndexReader) field(f string) stubDict {
	fd, ok := s.inv[f]
	if !ok {
		s.inv[f] = make(stubDict)
		return s.inv[f]
	}
	return fd
}

func (s *stubIndexReader) add(d segment.Document) {
	docNum := s.count
	s.count++

	s.uninv[docNum] = make(map[string][]string)

	// analyze it
	d.Analyze()

	var docID string

	// process fields
	fieldsSeen := map[string]struct{}{}
	d.EachField(func(field segment.Field) {
		if field.Index() {
			fieldLength := field.Length()
			fieldsSeen[field.Name()] = struct{}{}
			//nolint:gosec // G115: fixture fields derive nonnegative lengths from len or sums of lengths.
			s.fieldFreqs[field.Name()] += uint64(fieldLength)
			fd := s.field(field.Name())
			field.EachTerm(func(term segment.FieldTerm) {
				termStr := string(term.Term())
				if field.Name() == "_id" {
					docID = termStr
				}
				newThing := &thing{
					num:    docNum,
					length: fieldLength,
					freq: &thingFreq{
						term: termStr,
						freq: term.Frequency(),
					},
				}
				term.EachLocation(func(location segment.Location) {
					newThing.freq.locs = append(newThing.freq.locs, &thingLoc{
						fieldVal:    location.Field(),
						startVal:    location.Start(),
						endVal:      location.End(),
						positionVal: location.Pos(),
					})
				})
				fd[termStr] = append(fd[termStr], newThing)

				if field.IndexDocValues() {
					s.uninv[docNum][field.Name()] = append(s.uninv[docNum][field.Name()], termStr)
				}
			})
		}
	})
	// record fields seen by this doc
	for k := range fieldsSeen {
		s.fieldDocs[k]++
	}
	// record entry in special field "" value nil term
	newThing := &thing{
		num:    docNum,
		length: 1,
		freq: &thingFreq{
			term: "",
			freq: 1,
		},
	}
	fd := s.field("")
	fd[""] = append(fd[""], newThing)

	// backindex
	s.doc[docNum] = d
	s.docExtInt[docID] = docNum
}

type stubTermFieldReader struct {
	similarity                                   search.Similarity
	field                                        string
	term                                         string
	list                                         stubList
	i                                            int
	includeFreq, includeNorm, includeTermVectors bool
}

func newStubTermFieldReader(field, term string, list stubList,
	includeFreq, includeNorm, includeTermVectors bool) *stubTermFieldReader {
	return &stubTermFieldReader{
		field:              field,
		term:               term,
		list:               list,
		includeFreq:        includeFreq,
		includeNorm:        includeNorm,
		includeTermVectors: includeTermVectors,
		similarity:         similarity.NewBM25Similarity(),
	}
}

// Next returns the next document containing the term in this field, or nil
// when it reaches the end of the enumeration.  The preAlloced TermFieldDoc
// is optional, and when non-nil, will be used instead of allocating memory.
func (s *stubTermFieldReader) Next() (segment.Posting, error) {
	if s.i > (len(s.list) - 1) {
		return nil, nil
	}
	rv := stubTermFieldDoc{
		term: s.term,
	}
	rv.number = s.list[s.i].num
	if s.includeFreq {
		rv.freq = s.list[s.i].freq.freq
	}
	if s.includeNorm {
		tmp := s.similarity.ComputeNorm(s.list[s.i].length)
		rv.norm = float64(tmp)
	}
	if s.includeTermVectors {
		// reshape locations into tv
		for _, tocLoc := range s.list[s.i].freq.locs {
			tv := &stubTermFieldVector{
				start: tocLoc.startVal,
				end:   tocLoc.endVal,
				pos:   tocLoc.positionVal,
				field: tocLoc.fieldVal,
			}
			if tv.field == "" {
				tv.field = s.field
			}
			rv.vectors = append(rv.vectors, tv)
		}
	}
	s.i++
	return &rv, nil
}

// Advance resets the enumeration at specified document or its immediate
// follower.
func (s *stubTermFieldReader) Advance(number uint64) (segment.Posting, error) {
	// start over at beginning and brute force till we find it
	s.i = 0
	for s.i < len(s.list) && s.list[s.i].num < number {
		s.i++
	}
	return s.Next()
}

// Count returns the number of documents contains the term in this field.
func (s *stubTermFieldReader) Count() uint64 {
	return uint64(len(s.list))
}

func (s *stubTermFieldReader) Empty() bool {
	return s.Count() == 0
}

func (s *stubTermFieldReader) Close() error {
	return nil
}

func (s *stubTermFieldReader) Size() int {
	return 0
}

func (s *stubIndexReader) PostingsIterator(term []byte, field string, includeFreq, includeNorm, includeTermVectors bool) (segment.PostingsIterator, error) {
	fd := s.field(field)
	dl := fd[string(term)]
	return newStubTermFieldReader(field, string(term), dl, includeFreq, includeNorm, includeTermVectors), nil
}

type stubDictItr struct {
	sd   stubDict
	keys []string
	i    int
}

func newStubDictItr(sd stubDict) *stubDictItr {
	var keys []string
	for k := range sd {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return &stubDictItr{
		sd:   sd,
		keys: keys,
	}
}

type stubDictEntry struct {
	term  string
	count uint64
}

func (d *stubDictEntry) Term() string {
	return d.term
}

func (d *stubDictEntry) Count() uint64 {
	return d.count
}

func (sd *stubDictItr) Next() (segment.DictionaryEntry, error) {
	if sd.i > (len(sd.keys) - 1) {
		return nil, nil
	}
	rv := stubDictEntry{
		term:  sd.keys[sd.i],
		count: uint64(len(sd.keys[sd.i])),
	}
	sd.i++
	return &rv, nil
}

func (sd *stubDictItr) Close() error {
	return nil
}

func (s *stubIndexReader) DictionaryIterator(field string, a segment.Automaton, startTerm, endTerm []byte) (segment.DictionaryIterator, error) {
	fd := s.field(field)
	sdi := newStubDictItr(fd)

	// first filter keys by range
	var updatedKeys []string
	for k := range fd {
		if (startTerm == nil || k >= string(startTerm)) && (endTerm == nil || k < string(endTerm)) {
			updatedKeys = append(updatedKeys, k)
		}
	}
	sort.Strings(updatedKeys)

	// if no automaton, stop now
	if a == nil {
		sdi.keys = updatedKeys
		return sdi, nil
	}

	// now filter by automaton
	var filteredKeys []string
	for _, k := range updatedKeys {
		if automatonAccepts(a, k) {
			filteredKeys = append(filteredKeys, k)
		}
	}
	sdi.keys = filteredKeys
	return sdi, nil
}

func automatonAccepts(a segment.Automaton, val string) bool {
	valBytes := []byte(val)
	var i int
	curr := a.Start()
	for i < len(valBytes) && a.CanMatch(curr) {
		next := a.Accept(curr, valBytes[i])
		if a.IsMatch(next) {
			return true
		}
		curr = next
		i++
	}
	return false
}

func (s *stubIndexReader) VisitStoredFields(number uint64, visitor segment.StoredFieldVisitor) error {
	if doc, ok := s.doc[number]; ok {
		doc.EachField(func(field segment.Field) {
			visitor(field.Name(), field.Value())
		})
	}
	return fmt.Errorf("no such doc numbered: %d", number)
}

type CollectionStats struct {
	totalDocCount    uint64
	docCount         uint64
	sumTotalTermFreq uint64
}

func (c *CollectionStats) TotalDocumentCount() uint64 {
	return c.totalDocCount
}

func (c *CollectionStats) DocumentCount() uint64 {
	return c.docCount
}

func (c *CollectionStats) SumTotalTermFrequency() uint64 {
	return c.sumTotalTermFreq
}

func (c *CollectionStats) Merge(other segment.CollectionStats) {
	c.totalDocCount += other.TotalDocumentCount()
	c.docCount += other.DocumentCount()
	c.sumTotalTermFreq += other.SumTotalTermFrequency()
}

func (s *stubIndexReader) CollectionStats(field string) (segment.CollectionStats, error) {
	return &CollectionStats{
		totalDocCount:    s.count,
		docCount:         s.fieldDocs[field],
		sumTotalTermFreq: s.fieldFreqs[field],
	}, nil
}

func (s *stubIndexReader) DictionaryLookup(field string) (segment.DictionaryLookup, error) {
	fd := s.field(field)
	return fd, nil
}

func (s *stubIndexReader) DocumentVisitFieldTerms(_ int, _ []string, _ segment.DocumentValueVisitor) error {
	return nil
}

type stubDocValueReader struct {
	sir    *stubIndexReader
	fields []string
}

func newStubDocValueReader(sir *stubIndexReader, fields []string) *stubDocValueReader {
	return &stubDocValueReader{
		sir:    sir,
		fields: fields,
	}
}

func (s *stubDocValueReader) VisitDocumentValues(docNum uint64, visitor segment.DocumentValueVisitor) error {
	for _, field := range s.fields {
		for _, term := range s.sir.uninv[docNum][field] {
			visitor(field, []byte(term))
		}
	}
	return nil
}

func (s *stubIndexReader) DocumentValueReader(fields []string) (segment.DocumentValueReader, error) {
	dvr := newStubDocValueReader(s, fields)
	return dvr, nil
}

func (s *stubIndexReader) Fields() ([]string, error) {
	var fnames []string
	for k := range s.inv {
		fnames = append(fnames, k)
	}
	sort.Strings(fnames)
	return fnames, nil
}

func (s *stubIndexReader) GetInternal(_ []byte) ([]byte, error) {
	return nil, nil
}

func (s *stubIndexReader) Count() (uint64, error) {
	return s.count, nil
}

func (s *stubIndexReader) Close() error {
	return nil
}

func (s *stubIndexReader) docNumByID(id string) uint64 {
	return s.docExtInt[id]
}

type stubTermFieldVector struct {
	field string
	pos   int
	start int
	end   int
}

func (tfv *stubTermFieldVector) Field() string {
	return tfv.field
}
func (tfv *stubTermFieldVector) Pos() int {
	return tfv.pos
}
func (tfv *stubTermFieldVector) Start() int {
	return tfv.start
}
func (tfv *stubTermFieldVector) End() int {
	return tfv.end
}

func (tfv *stubTermFieldVector) Size() int {
	return 0
}

type stubTermFieldDoc struct {
	term    string
	number  uint64
	freq    int
	norm    float64
	vectors []segment.Location
}

func (tfd *stubTermFieldDoc) Term() string {
	return tfd.term
}
func (tfd *stubTermFieldDoc) Number() uint64 {
	return tfd.number
}
func (tfd *stubTermFieldDoc) SetNumber(n uint64) {
	tfd.number = n
}
func (tfd *stubTermFieldDoc) Frequency() int {
	return tfd.freq
}
func (tfd *stubTermFieldDoc) Norm() float64 {
	return tfd.norm
}
func (tfd *stubTermFieldDoc) Locations() []segment.Location {
	return tfd.vectors
}
func (tfd *stubTermFieldDoc) Size() int {
	return 0
}
