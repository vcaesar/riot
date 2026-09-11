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

package searcher

import (
	"reflect"
	"testing"

	"github.com/vcaesar/riot/search/similarity"

	segment "github.com/vcaesar/bluge_segment_api"

	"github.com/vcaesar/riot/numeric"
	"github.com/vcaesar/riot/numeric/geo"
	"github.com/vcaesar/riot/search"
)

const testGeoPrecisionStep uint = 9

func TestGeoBoundingBox(t *testing.T) {
	indexReader := setupGeo()

	tests := []struct {
		minLon float64
		minLat float64
		maxLon float64
		maxLat float64
		field  string
		want   []uint64
	}{
		{10.001, 10.001, 20.002, 20.002, "loc", nil},
		{0.001, 0.001, 0.002, 0.002, "loc",
			[]uint64{
				indexReader.docNumByID("a"),
			},
		},
		{0.001, 0.001, 1.002, 1.002, "loc",
			[]uint64{
				indexReader.docNumByID("a"),
				indexReader.docNumByID("b"),
			},
		},
		{0.001, 0.001, 9.002, 9.002, "loc",
			[]uint64{
				indexReader.docNumByID("a"),
				indexReader.docNumByID("b"),
				indexReader.docNumByID("c"),
				indexReader.docNumByID("d"),
				indexReader.docNumByID("e"),
				indexReader.docNumByID("f"),
				indexReader.docNumByID("g"),
				indexReader.docNumByID("h"),
				indexReader.docNumByID("i"),
				indexReader.docNumByID("j"),
			},
		},
		// same upper-left, bottom-right point
		{25, 25, 25, 25, "loc", nil},
		// box that would return points, but points reversed
		{0.002, 0.002, 0.001, 0.001, "loc", nil},
	}

	for _, test := range tests {
		got, err := testGeoBoundingBoxSearch(indexReader, test.minLon, test.minLat, test.maxLon, test.maxLat, test.field)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, test.want) {
			t.Errorf("expected %v, got %v for %f %f %f %f %s", test.want, got, test.minLon, test.minLat, test.maxLon, test.maxLat, test.field)
		}
	}
}

func testGeoBoundingBoxSearch(i search.Reader, minLon, minLat, maxLon, maxLat float64, field string) ([]uint64, error) {
	var rv []uint64
	gbs, err := NewGeoBoundingBoxSearcher(i, minLon, minLat, maxLon, maxLat, field,
		1.0, similarity.ConstantScorer(1.0), similarity.NewCompositeSumScorer(),
		search.SearcherOptions{}, true, testGeoPrecisionStep)
	if err != nil {
		return nil, err
	}
	ctx := &search.Context{
		DocumentMatchPool: search.NewDocumentMatchPool(gbs.DocumentMatchPoolSize(), 0),
	}
	docMatch, err := gbs.Next(ctx)
	for docMatch != nil && err == nil {
		rv = append(rv, docMatch.Number)
		docMatch, err = gbs.Next(ctx)
	}
	if err != nil {
		return nil, err
	}
	return rv, nil
}

func setupGeo() *stubIndexReader {
	docs := []segment.Document{
		&FakeDocument{
			NewFakeField("_id", "a", true, false, false, nil),
			NewFakeGeoField("loc", 0.0015, 0.0015),
		},
		&FakeDocument{
			NewFakeField("_id", "b", true, false, false, nil),
			NewFakeGeoField("loc", 1.0015, 1.0015),
		},
		&FakeDocument{
			NewFakeField("_id", "c", true, false, false, nil),
			NewFakeGeoField("loc", 2.0015, 2.0015),
		},
		&FakeDocument{
			NewFakeField("_id", "d", true, false, false, nil),
			NewFakeGeoField("loc", 3.0015, 3.0015),
		},
		&FakeDocument{
			NewFakeField("_id", "e", true, false, false, nil),
			NewFakeGeoField("loc", 4.0015, 4.0015),
		},
		&FakeDocument{
			NewFakeField("_id", "f", true, false, false, nil),
			NewFakeGeoField("loc", 5.0015, 5.0015),
		},
		&FakeDocument{
			NewFakeField("_id", "g", true, false, false, nil),
			NewFakeGeoField("loc", 6.0015, 6.0015),
		},
		&FakeDocument{
			NewFakeField("_id", "h", true, false, false, nil),
			NewFakeGeoField("loc", 7.0015, 7.0015),
		},
		&FakeDocument{
			NewFakeField("_id", "i", true, false, false, nil),
			NewFakeGeoField("loc", 8.0015, 8.0015),
		},
		&FakeDocument{
			NewFakeField("_id", "j", true, false, false, nil),
			NewFakeGeoField("loc", 9.0015, 9.0015),
		},
	}

	geoTestStubIndexReader := newStubIndexReader()
	for _, doc := range docs {
		geoTestStubIndexReader.add(doc)
	}

	return geoTestStubIndexReader
}

func TestComputeGeoRange(t *testing.T) {
	tests := []struct {
		degs        float64
		onBoundary  int
		offBoundary int
		err         string
	}{
		{0.01, 4, 0, ""},
		{0.1, 56, 144, ""},
		{100.0, 32768, 258560, ""},
	}

	for testi, test := range tests {
		onBoundaryRes, offBoundaryRes, err := ComputeGeoRange(0, GeoBitsShift1Minus1,
			-1.0*test.degs, -1.0*test.degs, test.degs, test.degs, true, nil, "",
			testGeoPrecisionStep)
		if (err != nil) != (test.err != "") {
			t.Errorf("test: %+v, err: %v", test, err)
		}
		if len(onBoundaryRes) != test.onBoundary {
			t.Errorf("test: %+v, onBoundaryRes: %v", test, len(onBoundaryRes))
		}
		if len(offBoundaryRes) != test.offBoundary {
			t.Errorf("test: %+v, offBoundaryRes: %v", test, len(offBoundaryRes))
		}

		onBROrig, offBROrig := origComputeGeoRange(0, GeoBitsShift1Minus1,
			-1.0*test.degs, -1.0*test.degs, test.degs, test.degs, true)
		if !reflect.DeepEqual(onBoundaryRes, onBROrig) {
			t.Errorf("testi: %d, test: %+v, onBoundaryRes != onBROrig,\n onBoundaryRes:%v,\n onBROrig: %v",
				testi, test, onBoundaryRes, onBROrig)
		}
		if !reflect.DeepEqual(offBoundaryRes, offBROrig) {
			t.Errorf("testi: %d, test: %+v, offBoundaryRes, offBROrig,\n offBoundaryRes: %v,\n offBROrig: %v",
				testi, test, offBoundaryRes, offBROrig)
		}
	}
}

// --------------------------------------------------------------------

func BenchmarkComputeGeoRangePt01(b *testing.B) {
	onBoundary := 4
	offBoundary := 0
	benchmarkComputeGeoRange(b, -0.01, -0.01, 0.01, 0.01, onBoundary, offBoundary)
}

func BenchmarkComputeGeoRangePt1(b *testing.B) {
	onBoundary := 56
	offBoundary := 144
	benchmarkComputeGeoRange(b, -0.1, -0.1, 0.1, 0.1, onBoundary, offBoundary)
}

func BenchmarkComputeGeoRange10(b *testing.B) {
	onBoundary := 5464
	offBoundary := 53704
	benchmarkComputeGeoRange(b, -10.0, -10.0, 10.0, 10.0, onBoundary, offBoundary)
}

func BenchmarkComputeGeoRange100(b *testing.B) {
	onBoundary := 32768
	offBoundary := 258560
	benchmarkComputeGeoRange(b, -100.0, -100.0, 100.0, 100.0, onBoundary, offBoundary)
}

func BenchmarkOrigComputeGeoRangePt01(b *testing.B) {
	onBoundary := 4
	offBoundary := 0
	benchmarkOrigComputeGeoRange(b, -0.01, -0.01, 0.01, 0.01, onBoundary, offBoundary)
}

func BenchmarkOrigComputeGeoRangePt1(b *testing.B) {
	onBoundary := 56
	offBoundary := 144
	benchmarkOrigComputeGeoRange(b, -0.1, -0.1, 0.1, 0.1, onBoundary, offBoundary)
}

func BenchmarkOrigComputeGeoRange10(b *testing.B) {
	onBoundary := 5464
	offBoundary := 53704
	benchmarkOrigComputeGeoRange(b, -10.0, -10.0, 10.0, 10.0, onBoundary, offBoundary)
}

func BenchmarkOrigComputeGeoRange100(b *testing.B) {
	onBoundary := 32768
	offBoundary := 258560
	benchmarkOrigComputeGeoRange(b, -100.0, -100.0, 100.0, 100.0, onBoundary, offBoundary)
}

func BenchmarkOrigComputeGeoRangePt012(b *testing.B) {
	onBoundary := 4
	offBoundary := 0
	benchmarkOrigComputeGeoRange2(b, -0.01, -0.01, 0.01, 0.01, onBoundary, offBoundary)
}

func BenchmarkOrigComputeGeoRangePt12(b *testing.B) {
	onBoundary := 56
	offBoundary := 144
	benchmarkOrigComputeGeoRange2(b, -0.1, -0.1, 0.1, 0.1, onBoundary, offBoundary)
}

func BenchmarkOrigComputeGeoRange102(b *testing.B) {
	onBoundary := 5464
	offBoundary := 53704
	benchmarkOrigComputeGeoRange2(b, -10.0, -10.0, 10.0, 10.0, onBoundary, offBoundary)
}

func BenchmarkOrigComputeGeoRange1002(b *testing.B) {
	onBoundary := 32768
	offBoundary := 258560
	benchmarkOrigComputeGeoRange2(b, -100.0, -100.0, 100.0, 100.0, onBoundary, offBoundary)
}

// --------------------------------------------------------------------

func benchmarkComputeGeoRange(b *testing.B,
	minLon, minLat, maxLon, maxLat float64, onBoundary, offBoundary int) {
	checkBoundaries := true

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		onBoundaryRes, offBoundaryRes, err :=
			ComputeGeoRange(0, GeoBitsShift1Minus1, minLon, minLat, maxLon, maxLat, checkBoundaries,
				nil, "", testGeoPrecisionStep)
		if err != nil {
			b.Fatalf("expected no err")
		}
		if len(onBoundaryRes) != onBoundary || len(offBoundaryRes) != offBoundary {
			b.Fatalf("boundaries not matching")
		}
	}
}

func benchmarkOrigComputeGeoRange(b *testing.B,
	minLon, minLat, maxLon, maxLat float64, onBoundary, offBoundary int) {
	checkBoundaries := true

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		onBoundaryRes, offBoundaryRes :=
			origComputeGeoRange(0, GeoBitsShift1Minus1, minLon, minLat, maxLon, maxLat, checkBoundaries)
		if len(onBoundaryRes) != onBoundary || len(offBoundaryRes) != offBoundary {
			b.Fatalf("boundaries not matching")
		}
	}
}

func benchmarkOrigComputeGeoRange2(b *testing.B,
	minLon, minLat, maxLon, maxLat float64, onBoundary, offBoundary int) {
	checkBoundaries := true

	preallocBytesLen := 32
	preallocBytes := make([]byte, preallocBytesLen)

	makePrefixCoded := func(in int64, shift uint) (rv numeric.PrefixCoded) {
		if len(preallocBytes) == 0 {
			preallocBytesLen *= 2
			preallocBytes = make([]byte, preallocBytesLen)
		}

		rv, preallocBytes, _ =
			numeric.NewPrefixCodedInt64Prealloc(in, shift, preallocBytes)

		return rv
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		onBoundaryRes, offBoundaryRes :=
			origComputeGeoRange2(0, GeoBitsShift1Minus1, minLon, minLat, maxLon, maxLat, checkBoundaries, nil, nil, makePrefixCoded)
		if len(onBoundaryRes) != onBoundary || len(offBoundaryRes) != offBoundary {
			b.Fatalf("boundaries not matching %d - %d, %d - %d", len(onBoundaryRes), onBoundary, len(offBoundaryRes), offBoundary)
		}
	}
}

// --------------------------------------------------------------------

// original, non-optimized implementation of ComputeGeoRange
func origComputeGeoRange(term uint64, shift uint,
	sminLon, sminLat, smaxLon, smaxLat float64,
	checkBoundaries bool) (
	onBoundary, notOnBoundary [][]byte) {
	split := term | uint64(0x1)<<shift
	var upperMax uint64
	if shift < 63 {
		upperMax = term | ((uint64(1) << (shift + 1)) - 1)
	} else {
		upperMax = 0xffffffffffffffff
	}
	lowerMax := split - 1
	onBoundary, notOnBoundary = origRelateAndRecurse(term, lowerMax, shift,
		sminLon, sminLat, smaxLon, smaxLat, checkBoundaries)
	plusOnBoundary, plusNotOnBoundary := origRelateAndRecurse(split, upperMax, shift,
		sminLon, sminLat, smaxLon, smaxLat, checkBoundaries)
	onBoundary = append(onBoundary, plusOnBoundary...)
	notOnBoundary = append(notOnBoundary, plusNotOnBoundary...)
	return
}

// original, non-optimized implementation of relateAndRecurse
func origRelateAndRecurse(start, end uint64, res uint,
	sminLon, sminLat, smaxLon, smaxLat float64,
	checkBoundaries bool) (
	onBoundary, notOnBoundary [][]byte) {
	minLon := geo.MortonUnhashLon(start)
	minLat := geo.MortonUnhashLat(start)
	maxLon := geo.MortonUnhashLon(end)
	maxLat := geo.MortonUnhashLat(end)

	level := ((geo.GeoBits << 1) - res) >> 1

	var geoMaxShift = testGeoPrecisionStep * 4
	var geoDetailLevel = ((geo.GeoBits << 1) - geoMaxShift) / 2

	within := res%testGeoPrecisionStep == 0 &&
		geo.RectWithin(minLon, minLat, maxLon, maxLat,
			sminLon, sminLat, smaxLon, smaxLat)
	if within || (level == geoDetailLevel &&
		geo.RectIntersects(minLon, minLat, maxLon, maxLat,
			sminLon, sminLat, smaxLon, smaxLat)) {
		if !within && checkBoundaries {
			return [][]byte{
				//nolint:gosec // G115: preserve all Morton hash bits in the signed prefix-coding API.
				numeric.MustNewPrefixCodedInt64(int64(start), res),
			}, nil
		}
		return nil,
			[][]byte{
				//nolint:gosec // G115: preserve all Morton hash bits in the signed prefix-coding API.
				numeric.MustNewPrefixCodedInt64(int64(start), res),
			}
	} else if level < geoDetailLevel &&
		geo.RectIntersects(minLon, minLat, maxLon, maxLat,
			sminLon, sminLat, smaxLon, smaxLat) {
		return origComputeGeoRange(start, res-1, sminLon, sminLat, smaxLon, smaxLat,
			checkBoundaries)
	}
	return nil, nil
}

// original, non-optimized implementation of ComputeGeoRange
func origComputeGeoRange2(term uint64, shift uint,
	sminLon, sminLat, smaxLon, smaxLat float64,
	checkBoundaries bool, onBoundary, notOnBoundary [][]byte,
	makePrefixCoded func(in int64, shift uint) (rv numeric.PrefixCoded)) (
	onBoundaryOut, notOnBoundarOut [][]byte) {
	split := term | uint64(0x1)<<shift
	var upperMax uint64
	if shift < 63 {
		upperMax = term | ((uint64(1) << (shift + 1)) - 1)
	} else {
		upperMax = 0xffffffffffffffff
	}
	lowerMax := split - 1
	onBoundary, notOnBoundary = origRelateAndRecurse2(term, lowerMax, shift,
		sminLon, sminLat, smaxLon, smaxLat, checkBoundaries, onBoundary, notOnBoundary, makePrefixCoded)
	onBoundary, notOnBoundary = origRelateAndRecurse2(split, upperMax, shift,
		sminLon, sminLat, smaxLon, smaxLat, checkBoundaries, onBoundary, notOnBoundary, makePrefixCoded)
	return onBoundary, notOnBoundary
}

// original, non-optimized implementation of relateAndRecurse
func origRelateAndRecurse2(start, end uint64, res uint,
	sminLon, sminLat, smaxLon, smaxLat float64,
	checkBoundaries bool, onBoundary, notOnBoundary [][]byte,
	makePrefixCoded func(in int64, shift uint) (rv numeric.PrefixCoded)) (
	onBoundaryOut, notOnBoundaryOut [][]byte) {
	minLon := geo.MortonUnhashLon(start)
	minLat := geo.MortonUnhashLat(start)
	maxLon := geo.MortonUnhashLon(end)
	maxLat := geo.MortonUnhashLat(end)

	level := ((geo.GeoBits << 1) - res) >> 1

	var geoMaxShift = testGeoPrecisionStep * 4
	var geoDetailLevel = ((geo.GeoBits << 1) - geoMaxShift) / 2

	within := res%testGeoPrecisionStep == 0 &&
		geo.RectWithin(minLon, minLat, maxLon, maxLat,
			sminLon, sminLat, smaxLon, smaxLat)
	if within || (level == geoDetailLevel &&
		geo.RectIntersects(minLon, minLat, maxLon, maxLat,
			sminLon, sminLat, smaxLon, smaxLat)) {
		if !within && checkBoundaries {
			//nolint:gosec // G115: preserve all Morton hash bits in the signed prefix-coding API.
			onBoundary = append(onBoundary, makePrefixCoded(int64(start), res))
			return onBoundary, notOnBoundary
		}
		//nolint:gosec // G115: preserve all Morton hash bits in the signed prefix-coding API.
		notOnBoundary = append(notOnBoundary, makePrefixCoded(int64(start), res))
		return onBoundary, notOnBoundary
	} else if level < geoDetailLevel &&
		geo.RectIntersects(minLon, minLat, maxLon, maxLat,
			sminLon, sminLat, smaxLon, smaxLat) {
		return origComputeGeoRange2(start, res-1, sminLon, sminLat, smaxLon, smaxLat,
			checkBoundaries, onBoundary, notOnBoundary, makePrefixCoded)
	}
	return onBoundary, notOnBoundary
}
