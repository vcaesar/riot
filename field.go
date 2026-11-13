//  Copyright (c) 2020 Couchbase, Inc.
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
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"slices"
	"strconv"
	"sync"
	"time"

	segment "github.com/vcaesar/bluge_segment_api"

	"github.com/vcaesar/riot/analysis/analyzer"
	"github.com/vcaesar/riot/analysis/tokenizer"

	"github.com/vcaesar/riot/analysis"
	"github.com/vcaesar/riot/numeric"
	"github.com/vcaesar/riot/numeric/geo"
)

type FieldOptions int

const (
	Index FieldOptions = 1 << iota
	Store
	SearchTermPositions
	HighlightMatches
	Sortable
	Aggregatable
)

func (o FieldOptions) Index() bool {
	return o&Index != 0
}

func (o FieldOptions) Store() bool {
	return o&Store != 0
}

func (o FieldOptions) IncludeLocations() bool {
	return o&SearchTermPositions != 0 || o&HighlightMatches != 0
}

func (o FieldOptions) IndexDocValues() bool {
	return o&Sortable != 0 || o&Aggregatable != 0
}

type Field interface {
	segment.Field

	Analyze(int) int
	AnalyzedTokenFrequencies() analysis.TokenFrequencies

	PositionIncrementGap() int

	Size() int
}

type TermField struct {
	FieldOptions
	name               string
	value              []byte
	numPlainTextBytes  int
	analyzedLength     int
	analyzedTokenFreqs analysis.TokenFrequencies
	// analyzedTerms holds the terms Analyze produced; the map form is only
	// built on demand by AnalyzedTokenFrequencies.
	analyzedTerms        []analysis.TokenFreq
	freqBlock            *analysis.FreqBlock // analyzedTerms' storage when pooled
	analyzer             Analyzer
	positionIncrementGap int
	noColumn             bool // WithoutNumericColumn
}

func (b *TermField) PositionIncrementGap() int {
	return b.positionIncrementGap
}

func (b *TermField) SetPositionIncrementGap(positionIncrementGap int) *TermField {
	b.positionIncrementGap = positionIncrementGap
	return b
}

func (b *TermField) Name() string {
	return b.name
}

func (b *TermField) Size() int {
	return reflectStaticSizeBaseField + sizeOfPtr +
		len(b.name) +
		len(b.value)
}

func (b *TermField) AnalyzedLength() int {
	return b.analyzedLength
}

func (b *TermField) AnalyzedTokenFrequencies() analysis.TokenFrequencies {
	if b.analyzedTokenFreqs == nil && b.analyzedTerms != nil {
		return analysis.NewTokenFrequencies(b.analyzedTerms)
	}
	return b.analyzedTokenFreqs
}

func (b *TermField) Value() []byte {
	return b.value
}

func (b *TermField) NumPlainTextBytes() int {
	return b.numPlainTextBytes
}

// NumericValue returns the int64 behind a numeric, date or geo field so the
// segment can store it as a typed column; ok is false for other fields.
func (b *TermField) NumericValue() (int64, bool) {
	if _, ok := b.analyzer.(*numericAnalyzer); !ok || b.noColumn {
		return 0, false
	}
	v, err := numeric.PrefixCoded(b.value).Int64()
	return v, err == nil
}

func (b *TermField) StoreValue() *TermField {
	b.FieldOptions |= Store
	return b
}

func (b *TermField) Sortable() *TermField {
	b.FieldOptions |= Sortable
	return b
}

func (b *TermField) Aggregatable() *TermField {
	b.FieldOptions |= Aggregatable
	return b
}

// WithoutDocValues drops Sortable and Aggregatable, which numeric, date,
// geo, boolean and ip fields have by default: the field is then stored
// without a column or term doc values, so it is smaller and faster to index
// but cannot be sorted or aggregated on. Queries on it still match.
// Segments keep doc values per field: when any document of a segment has
// them for the field, every document of that segment does, so use it on all
// documents of a field.
func (b *TermField) WithoutDocValues() *TermField {
	b.FieldOptions &^= Sortable | Aggregatable
	return b
}

func (b *TermField) SearchTermPositions() *TermField {
	b.FieldOptions |= SearchTermPositions
	return b
}

func (b *TermField) HighlightMatches() *TermField {
	b.FieldOptions |= HighlightMatches
	return b
}

func (b *TermField) EachTerm(vt segment.VisitTerm) {
	for i := range b.analyzedTerms {
		vt(&b.analyzedTerms[i])
	}
	for _, v := range b.analyzedTokenFreqs {
		vt(v)
	}
}

func (b *TermField) Length() int {
	return b.analyzedLength
}

// ReleaseAnalysis drops the analyzed terms; Analyze rebuilds them.
func (b *TermField) ReleaseAnalysis() {
	b.analyzedTerms = nil
	if b.freqBlock != nil {
		analysis.ReleaseFreqBlock(b.freqBlock)
		b.freqBlock = nil
	}
}

func (b *TermField) baseAnalayze(typ analysis.TokenType) analysis.TokenStream {
	// the token and the stream pointing to it in one allocation
	one := &struct {
		token  analysis.Token
		tokens [1]*analysis.Token
	}{token: analysis.Token{
		Start:        0,
		End:          len(b.value),
		Term:         b.value,
		PositionIncr: 1,
		Type:         typ,
	}}
	one.tokens[0] = &one.token
	return one.tokens[:]
}

func (b *TermField) WithAnalyzer(fieldAnalyzer Analyzer) *TermField {
	b.analyzer = fieldAnalyzer
	return b
}

func (b *TermField) Analyze(startOffset int) (lastPos int) {
	if !b.Index() {
		b.analyzedLength = 0
		b.analyzedTerms = nil
		return startOffset
	}
	if n, ok := b.analyzer.(*numericAnalyzer); ok && n.shiftBy == 0 && !b.IncludeLocations() {
		// the terms are known without a token stream, and the analyzer
		// does not change the value, so it needs no copy either
		b.analyzedTerms, b.analyzedLength = n.terms(b.value)
		return 0
	}
	var tokens analysis.TokenStream
	if b.analyzer != nil {
		bytesToAnalyze := b.Value()
		if b.analyzer == Analyzer(standardAnalyzer) {
			// a composite field may still point into the previous storage,
			// so only ReleaseAnalysis recycles it
			blk := analysis.NewFreqBlock()
			if b.Store() {
				// the terms alias the copy, so it lives in blk with them
				bytesToAnalyze = blk.CopyText(bytesToAnalyze)
			}
			// the tokens are dead once their frequencies are counted
			// (locations are copied out), so reuse their storage
			s := tokenScratch.Get().(*tokenizer.Scratch)
			tokens = standardTokenizer.TokenizeInto(bytesToAnalyze, s)
			for _, tf := range standardAnalyzer.TokenFilters {
				tokens = tf.Filter(tokens)
			}
			b.analyzedLength = len(tokens)
			b.analyzedTerms, lastPos = blk.TokenFrequency(tokens, b.IncludeLocations(), startOffset)
			b.freqBlock = blk
			tokenScratch.Put(s)
			b.dropOffsets()
			return lastPos
		}
		if b.Store() {
			// need to copy
			bytesCopied := make([]byte, len(bytesToAnalyze))
			copy(bytesCopied, bytesToAnalyze)
			bytesToAnalyze = bytesCopied
		}
		tokens = b.analyzer.Analyze(bytesToAnalyze)
	} else {
		// a keyword is the one token baseAnalayze would make
		b.analyzedLength = 1
		b.analyzedTerms, lastPos = analysis.SingleTokenFrequency(b.value, b.IncludeLocations(), startOffset)
		b.dropOffsets()
		return lastPos
	}
	b.analyzedLength = len(tokens) // number of tokens in this doc field
	b.analyzedTerms, lastPos = analysis.TokenFrequencySlice(tokens, b.IncludeLocations(), startOffset)
	b.dropOffsets()
	return lastPos
}

// dropOffsets clears the start and end offsets of a field that searches
// term positions but does not highlight matches: phrase queries read only
// positions, so, as in Lucene, offsets are kept for highlighting alone.
func (b *TermField) dropOffsets() {
	if b.FieldOptions&(SearchTermPositions|HighlightMatches) != SearchTermPositions {
		return
	}
	for i := range b.analyzedTerms {
		locs := b.analyzedTerms[i].Locations
		for j := range locs {
			locs[j].StartVal, locs[j].EndVal = 0, 0
		}
	}
}

const defaultTextIndexingOptions = Index

type Analyzer interface {
	Analyze(input []byte) analysis.TokenStream
}

var standardAnalyzer = analyzer.NewStandardAnalyzer()

// standardTokenizer is standardAnalyzer's tokenizer, whose token storage
// Analyze reuses from tokenScratch.
var standardTokenizer = standardAnalyzer.Tokenizer.(*tokenizer.UnicodeTokenizer)

var tokenScratch = sync.Pool{New: func() any { return new(tokenizer.Scratch) }}

func NewKeywordField(name, value string) *TermField {
	return newOwnedTextField(name, value, nil)
}

func NewKeywordFieldBytes(name string, value []byte) *TermField {
	return newTextField(name, value, nil)
}

func NewTextField(name, value string) *TermField {
	return newOwnedTextField(name, value, standardAnalyzer)
}

func NewTextFieldBytes(name string, value []byte) *TermField {
	return newTextField(name, value, standardAnalyzer)
}

// termFieldInline is the longest value newOwnedTextField copies into the
// field's own allocation
const termFieldInline = 16

// termFieldBlock is a TermField and the copy of its short value. It holds
// no analysis: a Batch keeps every document's fields alive until it is
// indexed, so per-field analysis room would be retained for each of them.
type termFieldBlock struct {
	field TermField
	buf   [termFieldInline]byte
}

// newOwnedTextField is newTextField over a copy of value; a short copy
// shares the field's allocation.
func newOwnedTextField(name, value string, fieldAnalyzer Analyzer) *TermField {
	if len(value) > termFieldInline {
		return newTextField(name, []byte(value), fieldAnalyzer)
	}
	blk := &termFieldBlock{}
	n := copy(blk.buf[:], value)
	blk.field = textField(name, blk.buf[:n:n], fieldAnalyzer)
	return &blk.field
}

func newTextField(name string, value []byte, fieldAnalyzer Analyzer) *TermField {
	f := textField(name, value, fieldAnalyzer)
	return &f
}

func textField(name string, value []byte, fieldAnalyzer Analyzer) TermField {
	return TermField{
		FieldOptions:         defaultTextIndexingOptions,
		name:                 name,
		value:                value,
		numPlainTextBytes:    len(value),
		analyzer:             fieldAnalyzer,
		positionIncrementGap: 100,
	}
}

const defaultNumericIndexingOptions = Index | Sortable | Aggregatable

// defaultNumericPrecisionStep 0 indexes one exact term per value: ranges
// are served from the numeric column, not from trie terms.
const defaultNumericPrecisionStep uint = 0

func addShiftTokens(tokens analysis.TokenStream, original int64, shiftBy uint, typ analysis.TokenType) analysis.TokenStream {
	// size one token block and one term buffer for every shift up front
	n, size := 0, 0
	for shift := shiftBy; shift < 64; shift += shiftBy {
		n++
		size += int((63-shift)/7) + 2
	}
	block := make([]analysis.Token, n)
	buf := make([]byte, size)
	tokens = slices.Grow(tokens, n)
	i := 0
	for shift := shiftBy; shift < 64; shift += shiftBy {
		var shiftEncoded numeric.PrefixCoded
		var err error
		shiftEncoded, buf, err = numeric.NewPrefixCodedInt64Prealloc(original, shift, buf)
		if err != nil {
			break
		}
		block[i] = analysis.Token{
			End:  len(shiftEncoded),
			Term: shiftEncoded,
			Type: typ,
		}
		tokens = append(tokens, &block[i])
		i++
	}
	return tokens
}

type numericAnalyzer struct {
	tokenType analysis.TokenType
	shiftBy   uint
	// emitOriginal also indexes the human-readable value, so a numeric
	// field can be matched by a plain term query like "3.4".
	emitOriginal bool
	// format and prec format the human-readable value as
	// strconv.FormatFloat; a zero format is 'f', -1 ("3.25").
	format byte
	prec   int
}

func (n *numericAnalyzer) decimal(v float64) []byte {
	return n.appendDecimal(make([]byte, 0, 24), v)
}

func (n *numericAnalyzer) appendDecimal(dst []byte, v float64) []byte {
	format, prec := n.format, n.prec
	if format == 0 {
		format, prec = 'f', -1
	}
	return strconv.AppendFloat(dst, v, format, prec, 64)
}

func (n *numericAnalyzer) Analyze(input []byte) analysis.TokenStream {
	// the value and original tokens share a block, and the stream has room
	// for the shift tokens, so nothing regrows
	numShifts := 0
	if n.shiftBy > 0 {
		numShifts = int(63 / n.shiftBy)
	}
	block := make([]analysis.Token, 1, 2)
	block[0] = analysis.Token{
		Start:        0,
		End:          len(input),
		Term:         input,
		PositionIncr: 1,
		Type:         n.tokenType,
	}
	tokens := make(analysis.TokenStream, 1, 2+numShifts)
	tokens[0] = &block[0]
	if n.emitOriginal {
		if origVal, err := DecodeNumericFloat64(input); err == nil {
			orig := n.decimal(origVal)
			block = append(block, analysis.Token{
				Start:        0,
				End:          len(orig),
				Term:         orig,
				PositionIncr: 0,
				Type:         analysis.AlphaNumeric,
			})
			tokens = append(tokens, &block[1])
		}
	}
	if n.shiftBy == 0 {
		return tokens
	}
	original, err := numeric.PrefixCoded(input).Int64()
	if err == nil {
		tokens = addShiftTokens(tokens, original, n.shiftBy, n.tokenType)
	}
	return tokens
}

// terms is Analyze's term frequencies without term vectors when shiftBy is
// zero, and the number of tokens Analyze would emit.
func (n *numericAnalyzer) terms(input []byte) (tfs []analysis.TokenFreq, count int) {
	if !n.emitOriginal {
		return analysis.AppendTerm(make([]analysis.TokenFreq, 0, 1), input), 1
	}
	// both terms and the decimal text in one allocation
	blk := &struct {
		tfs [2]analysis.TokenFreq
		dec [24]byte
	}{}
	tfs = analysis.AppendTerm(blk.tfs[:0], input)
	if v, err := DecodeNumericFloat64(input); err == nil {
		return analysis.AppendTerm(tfs, n.appendDecimal(blk.dec[:0], v)), 2
	}
	return tfs, 1
}

// numberAnalyzer and dateTimeAnalyzer are shared by every numeric and date
// field: a numericAnalyzer is never modified, so a field needs no copy.
var (
	numberAnalyzer   = &numericAnalyzer{tokenType: analysis.Numeric, shiftBy: defaultNumericPrecisionStep, emitOriginal: true}
	dateTimeAnalyzer = &numericAnalyzer{tokenType: analysis.DateTime, shiftBy: defaultDateTimePrecisionStep}
)

func NewNumericField(name string, number float64) *TermField {
	return newNumericFieldWithIndexingOptions(name, number, defaultNumericIndexingOptions)
}

// numericFieldBlock is a TermField and its shift-0 prefix-coded value
type numericFieldBlock struct {
	field TermField
	code  [11]byte // shift byte and 10 seven-bit groups
}

func newNumericFieldWithIndexingOptions(name string, number float64, options FieldOptions) *TermField {
	blk := &numericFieldBlock{}
	prefixCoded := numeric.AppendPrefixCodedInt64(blk.code[:0:len(blk.code)], numeric.Float64ToInt64(number))
	blk.field = TermField{
		FieldOptions:         options,
		name:                 name,
		value:                prefixCoded,
		numPlainTextBytes:    8,
		analyzer:             numberAnalyzer,
		positionIncrementGap: 100,
	}
	return &blk.field
}

// WithoutDecimalTerm stops a numeric field from also indexing its decimal
// text term (e.g. "3.4"), which is on by default; term queries on the
// decimal text then no longer match it, numeric range queries still do.
func (b *TermField) WithoutDecimalTerm() *TermField {
	if n, ok := b.analyzer.(*numericAnalyzer); ok && n.emitOriginal {
		b.analyzer = &numericAnalyzer{tokenType: n.tokenType, shiftBy: n.shiftBy}
	}
	return b
}

// WithDecimalTermFormat formats the decimal text term of a numeric field as
// strconv.FormatFloat(v, format, prec, 64); the default is 'f', -1 ("3.25").
// Term queries must use the same text. It has no effect on fields without
// the decimal term (after WithoutDecimalTerm, or date and geo fields).
func (b *TermField) WithDecimalTermFormat(format byte, prec int) *TermField {
	if n, ok := b.analyzer.(*numericAnalyzer); ok && n.emitOriginal {
		b.analyzer = &numericAnalyzer{tokenType: n.tokenType, shiftBy: n.shiftBy, emitOriginal: true, format: format, prec: prec}
	}
	return b
}

// WithoutNumericColumn stops a numeric, date or geo field from being stored
// as a numeric column; its sort and aggregation values are kept as terms
// instead. Numeric ranges then read the exact terms, which is slower on wide
// ranges, and Reader.VisitNumbers fails for the field.
func (b *TermField) WithoutNumericColumn() *TermField {
	if _, ok := b.analyzer.(*numericAnalyzer); ok {
		b.noColumn = true
	}
	return b
}

func DecodeNumericFloat64(value []byte) (float64, error) {
	i64, err := numeric.PrefixCoded(value).Int64()
	if err != nil {
		return 0, err
	}
	return numeric.Int64ToFloat64(i64), nil
}

const defaultDateTimeIndexingOptions = Index | Sortable | Aggregatable

const defaultDateTimePrecisionStep uint = 0

func NewDateTimeField(name string, dt time.Time) *TermField {
	dtInt64 := dt.UnixNano()
	prefixCoded := numeric.MustNewPrefixCodedInt64(dtInt64, 0)
	return &TermField{
		FieldOptions:         defaultDateTimeIndexingOptions,
		name:                 name,
		value:                prefixCoded,
		numPlainTextBytes:    8,
		analyzer:             dateTimeAnalyzer,
		positionIncrementGap: 100,
	}
}

func DecodeDateTime(value []byte) (time.Time, error) {
	i64, err := numeric.PrefixCoded(value).Int64()
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, i64).UTC(), nil
}

var geoPrecisionStep uint = 9

func NewGeoPointField(name string, lon, lat float64) *TermField {
	mHash := geo.MortonHash(lon, lat)
	//nolint:gosec // G115: preserve all Morton hash bits in the signed prefix-coding API.
	prefixCoded := numeric.MustNewPrefixCodedInt64(int64(mHash), 0)
	return &TermField{
		FieldOptions:         defaultNumericIndexingOptions,
		name:                 name,
		value:                prefixCoded,
		numPlainTextBytes:    8,
		analyzer:             &numericAnalyzer{tokenType: analysis.Numeric, shiftBy: geoPrecisionStep},
		positionIncrementGap: 100,
	}
}

func DecodeGeoLonLat(value []byte) (lon, lat float64, err error) {
	i64, err := numeric.PrefixCoded(value).Int64()
	if err != nil {
		return 0, 0, err
	}
	//nolint:gosec // G115: restore all Morton hash bits from the signed prefix-coding API.
	return geo.MortonUnhashLon(uint64(i64)), geo.MortonUnhashLat(uint64(i64)), nil
}

const defaultBooleanIndexingOptions = Index | Sortable | Aggregatable

const (
	booleanTrueTerm  = "T"
	booleanFalseTerm = "F"
)

// NewBooleanField indexes a boolean as the single keyword term "T" or "F".
func NewBooleanField(name string, value bool) *TermField {
	term := booleanFalseTerm
	if value {
		term = booleanTrueTerm
	}
	return &TermField{
		FieldOptions:      defaultBooleanIndexingOptions,
		name:              name,
		value:             []byte(term),
		numPlainTextBytes: 1,
		analyzer:          &singleTokenAnalyzer{tokenType: analysis.Boolean},
	}
}

func DecodeBoolean(value []byte) (bool, error) {
	switch string(value) {
	case booleanTrueTerm:
		return true, nil
	case booleanFalseTerm:
		return false, nil
	}
	return false, fmt.Errorf("invalid boolean term %q", value)
}

const defaultIPIndexingOptions = Index | Sortable | Aggregatable

// NewIPField indexes an IPv4 or IPv6 address as its 16-byte form so that
// IPv4 and IPv6 share one sortable term space; see NewIPRangeQuery.
// A nil or invalid ip yields a field with no value.
func NewIPField(name string, ip net.IP) *TermField {
	return &TermField{
		FieldOptions:      defaultIPIndexingOptions,
		name:              name,
		value:             ip.To16(),
		numPlainTextBytes: net.IPv6len,
		analyzer:          &singleTokenAnalyzer{tokenType: analysis.IP},
	}
}

func DecodeIP(value []byte) (net.IP, error) {
	if len(value) != net.IPv6len {
		return nil, fmt.Errorf("invalid ip term length %d, want %d", len(value), net.IPv6len)
	}
	return net.IP(value), nil
}

var ipv4Analyzer = &numericAnalyzer{tokenType: analysis.Numeric}

// NewIPv4Field indexes an IPv4 address as the number of its 32 bits, so
// NewIPv4RangeQuery and numeric range queries match it. It is indexed
// only; Sortable or Aggregatable add a numeric column that sorts and
// aggregates it in address order. ip must be an IPv4 address.
func NewIPv4Field(name string, ip net.IP) (*TermField, error) {
	ip4 := ip.To4()
	if ip4 == nil {
		return nil, fmt.Errorf("error creating ipv4 field %s: %v is not an ipv4 address", name, ip)
	}
	return &TermField{
		FieldOptions:         Index,
		name:                 name,
		value:                numeric.MustNewPrefixCodedInt64(numeric.Float64ToInt64(float64(binary.BigEndian.Uint32(ip4))), 0),
		numPlainTextBytes:    net.IPv4len,
		analyzer:             ipv4Analyzer,
		positionIncrementGap: 100,
	}, nil
}

// DecodeIPv4 decodes a NewIPv4Field value, stored or from doc values.
func DecodeIPv4(value []byte) (net.IP, error) {
	v, err := DecodeNumericFloat64(value)
	if err != nil {
		return nil, err
	}
	if v < 0 || v > math.MaxUint32 || v != math.Trunc(v) {
		return nil, fmt.Errorf("invalid ipv4 value %v", v)
	}
	ip := make(net.IP, net.IPv4len)
	binary.BigEndian.PutUint32(ip, uint32(v))
	return ip, nil
}

// singleTokenAnalyzer emits the raw value as one token of the given type.
type singleTokenAnalyzer struct {
	tokenType analysis.TokenType
}

func (s *singleTokenAnalyzer) Analyze(input []byte) analysis.TokenStream {
	return analysis.TokenStream{
		&analysis.Token{
			Start:        0,
			End:          len(input),
			Term:         input,
			PositionIncr: 1,
			Type:         s.tokenType,
		},
	}
}

const defaultCompositeIndexingOptions = Index

type CompositeField struct {
	*TermField
	includedFields map[string]bool
	excludedFields map[string]bool
	defaultInclude bool
}

func NewCompositeFieldIncluding(name string, including []string) *CompositeField {
	return newCompositeFieldWithIndexingOptions(name, false, including,
		nil, defaultCompositeIndexingOptions)
}

func NewCompositeFieldExcluding(name string, excluding []string) *CompositeField {
	return newCompositeFieldWithIndexingOptions(name, true, nil,
		excluding, defaultCompositeIndexingOptions)
}

func NewCompositeField(name string, defaultInclude bool, include, exclude []string) *CompositeField {
	return newCompositeFieldWithIndexingOptions(name, defaultInclude, include, exclude, defaultCompositeIndexingOptions)
}

func newCompositeFieldWithIndexingOptions(name string, defaultInclude bool, include, exclude []string,
	options FieldOptions) *CompositeField {
	rv := &CompositeField{
		TermField: &TermField{
			FieldOptions:       options,
			name:               name,
			analyzedTokenFreqs: make(analysis.TokenFrequencies),
		},
		defaultInclude: defaultInclude,
		includedFields: make(map[string]bool, len(include)),
		excludedFields: make(map[string]bool, len(exclude)),
	}

	for _, i := range include {
		rv.includedFields[i] = true
	}
	for _, e := range exclude {
		rv.excludedFields[e] = true
	}

	return rv
}

func (c *CompositeField) Size() int {
	sizeInBytes := c.TermField.Size()

	for k := range c.includedFields {
		sizeInBytes += sizeOfString + len(k) + sizeOfBool
	}

	for k := range c.excludedFields {
		sizeInBytes += sizeOfString + len(k) + sizeOfBool
	}

	return sizeInBytes
}

func (c *CompositeField) Analyze(int) int {
	return 0
}

func (c *CompositeField) PositionIncrementGap() int {
	return 0
}

func (c *CompositeField) includesField(field string) bool {
	shouldInclude := c.defaultInclude
	_, fieldShouldBeIncluded := c.includedFields[field]
	if fieldShouldBeIncluded {
		shouldInclude = true
	}
	_, fieldShouldBeExcluded := c.excludedFields[field]
	if fieldShouldBeExcluded {
		shouldInclude = false
	}
	return shouldInclude
}

func (c *CompositeField) Consume(field Field) {
	if c.includesField(field.Name()) {
		c.analyzedLength += field.Length()
		c.analyzedTokenFreqs.MergeAll(field.Name(), field.AnalyzedTokenFrequencies())
	}
}

// ReleaseAnalysis drops the consumed terms, so the next Analyze of the
// document starts from empty.
func (c *CompositeField) ReleaseAnalysis() {
	clear(c.analyzedTokenFreqs)
	c.analyzedLength = 0
}

func (c *CompositeField) EachTerm(vt segment.VisitTerm) {
	for _, v := range c.analyzedTokenFreqs {
		vt(v)
	}
}

func (c *CompositeField) Length() int {
	return c.analyzedLength
}

func NewStoredOnlyField(name string, value []byte) *TermField {
	return &TermField{
		FieldOptions:      Store,
		name:              name,
		value:             value,
		numPlainTextBytes: len(value),
	}
}
