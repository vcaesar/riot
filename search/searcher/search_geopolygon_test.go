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

	"github.com/vcaesar/riot/numeric/geo"
	"github.com/vcaesar/riot/search"
)

func TestSimpleGeoPolygons(t *testing.T) {
	indexReader := setupGeoPolygonPoints()

	tests := []struct {
		polygon []geo.Point
		field   string
		want    []uint64
	}{
		// test points inside a triangle & on vertices
		// r, s - inside and t,u - on vertices.
		{[]geo.Point{{Lon: 1.0, Lat: 1.0}, {Lon: 2.0, Lat: 1.9}, {Lon: 2.0, Lat: 1.0}}, "loc",
			[]uint64{
				indexReader.docNumByID("r"),
				indexReader.docNumByID("s"),
				indexReader.docNumByID("t"),
				indexReader.docNumByID("u"),
			},
		},
		// non overlapping polygon for the indexed documents
		{[]geo.Point{{Lon: 3.0, Lat: 1.0}, {Lon: 4.0, Lat: 2.5}, {Lon: 3.0, Lat: 2}}, "loc", nil},
	}

	for _, test := range tests {
		got, err := testGeoPolygonSearch(indexReader, test.polygon, test.field)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, test.want) {
			t.Errorf("expected %v, got %v for polygon: %+v", test.want, got, test.polygon)
		}
	}
}

func TestRealGeoPolygons(t *testing.T) {
	indexReader := setupGeoPolygonPoints()

	tests := []struct {
		polygon []geo.Point
		field   string
		want    []uint64
	}{
		{[]geo.Point{{Lon: -80.881, Lat: 35.282}, {Lon: -80.858, Lat: 35.281},
			{Lon: -80.864, Lat: 35.270}}, "loc",
			[]uint64{
				indexReader.docNumByID("k"),
				indexReader.docNumByID("l"),
			},
		},
		{[]geo.Point{{Lon: -82.467, Lat: 36.356}, {Lon: -78.127, Lat: 36.321}, {Lon: -80.555, Lat: 32.932},
			{Lon: -84.807, Lat: 33.111}}, "loc",
			[]uint64{
				indexReader.docNumByID("k"),
				indexReader.docNumByID("l"),
				indexReader.docNumByID("m"),
			},
		},
		// same polygon vertices
		{[]geo.Point{{Lon: -82.467, Lat: 36.356}, {Lon: -82.467, Lat: 36.356}, {Lon: -82.467, Lat: 36.356}, {Lon: -82.467, Lat: 36.356}}, "loc", nil},
		// non-overlapping polygon
		{[]geo.Point{{Lon: -89.113, Lat: 36.400}, {Lon: -93.947, Lat: 36.471}, {Lon: -93.947, Lat: 34.031}}, "loc", nil},
		// concave polygon with a document `n` residing inside the hands, but outside the polygon
		{[]geo.Point{{Lon: -71.65, Lat: 42.446}, {Lon: -71.649, Lat: 42.428}, {Lon: -71.640, Lat: 42.445}, {Lon: -71.649, Lat: 42.435}}, "loc", nil},
		// V like concave polygon with a document 'p' residing inside the bottom corner
		{[]geo.Point{{Lon: -80.304, Lat: 40.740}, {Lon: -80.038, Lat: 40.239}, {Lon: -79.562, Lat: 40.786}, {Lon: -80.018, Lat: 40.328}}, "loc",
			[]uint64{
				indexReader.docNumByID("p"),
			},
		},
		{[]geo.Point{{Lon: -111.918, Lat: 33.515}, {Lon: -111.938, Lat: 33.494}, {Lon: -111.944, Lat: 33.481}, {Lon: -111.886, Lat: 33.517},
			{Lon: -111.919, Lat: 33.468}, {Lon: -111.929, Lat: 33.508}}, "loc",
			[]uint64{
				indexReader.docNumByID("q"),
			},
		},
		// real points near cb bangalore
		{[]geo.Point{{Lat: 12.974872, Lon: 77.607749}, {Lat: 12.971725, Lon: 77.610110},
			{Lat: 12.972530, Lon: 77.606912}, {Lat: 12.975112, Lon: 77.603780},
		}, "loc",
			[]uint64{
				indexReader.docNumByID("amoeba"),
				indexReader.docNumByID("communiti"),
			},
		},
	}

	for _, test := range tests {
		got, err := testGeoPolygonSearch(indexReader, test.polygon, test.field)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, test.want) {
			t.Errorf("expected %v, got %v for polygon: %+v", test.want, got, test.polygon)
		}
	}
}

func TestGeoRectanglePolygon(t *testing.T) {
	indexReader := setupGeo()

	tests := []struct {
		polygon []geo.Point
		field   string
		want    []uint64
	}{
		{[]geo.Point{{Lon: 0, Lat: 0}, {Lon: 0, Lat: 50}, {Lon: 50, Lat: 50}, {Lon: 50, Lat: 0}, {Lon: 0, Lat: 0}}, "loc",
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
	}

	for _, test := range tests {
		got, err := testGeoPolygonSearch(indexReader, test.polygon, test.field)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, test.want) {
			t.Errorf("expected %v, got %v for polygon: %+v", test.want, got, test.polygon)
		}
	}
}

func testGeoPolygonSearch(i search.Reader, polygon []geo.Point, field string) ([]uint64, error) {
	var rv []uint64
	gbs, err := NewGeoBoundedPolygonSearcher(i, polygon, field, 1.0,
		similarity.ConstantScorer(1.0), similarity.NewCompositeSumScorer(),
		search.SearcherOptions{}, testGeoPrecisionStep)
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

func setupGeoPolygonPoints() *stubIndexReader {
	docs := []segment.Document{
		&FakeDocument{
			NewFakeField("_id", "k", true, false, false, nil),
			NewFakeGeoField("loc", -80.86469327, 35.2782),
		},
		&FakeDocument{
			NewFakeField("_id", "l", true, false, false, nil),
			NewFakeGeoField("loc", -80.8713, 35.28138),
		},
		&FakeDocument{
			NewFakeField("_id", "m", true, false, false, nil),
			NewFakeGeoField("loc", -84.25, 33.153),
		},
		&FakeDocument{
			NewFakeField("_id", "n", true, false, false, nil),
			NewFakeGeoField("loc", -89.992, 35.063),
		},
		&FakeDocument{
			NewFakeField("_id", "o", true, false, false, nil),
			NewFakeGeoField("loc", -71.648, 42.437),
		},
		&FakeDocument{
			NewFakeField("_id", "p", true, false, false, nil),
			NewFakeGeoField("loc", -80.016, 40.314),
		},
		&FakeDocument{
			NewFakeField("_id", "q", true, false, false, nil),
			NewFakeGeoField("loc", -111.919, 33.494),
		},
		&FakeDocument{
			NewFakeField("_id", "r", true, false, false, nil),
			NewFakeGeoField("loc", 1.5, 1.1),
		},
		&FakeDocument{
			NewFakeField("_id", "s", true, false, false, nil),
			NewFakeGeoField("loc", 2, 1.5),
		},
		&FakeDocument{
			NewFakeField("_id", "t", true, false, false, nil),
			NewFakeGeoField("loc", 2.0, 1.9),
		},
		&FakeDocument{
			NewFakeField("_id", "u", true, false, false, nil),
			NewFakeGeoField("loc", 2.0, 1.0),
		},
		&FakeDocument{
			NewFakeField("_id", "amoeba", true, false, false, nil),
			NewFakeGeoField("loc", 77.60490, 12.97467),
		},
		&FakeDocument{
			NewFakeField("_id", "communiti", true, false, false, nil),
			NewFakeGeoField("loc", 77.608237, 12.97237),
		},
	}

	geoTestStubIndexReader := newStubIndexReader()
	for _, doc := range docs {
		geoTestStubIndexReader.add(doc)
	}

	return geoTestStubIndexReader
}

type geoPoint struct {
	title string
	lon   float64
	lat   float64
}

// Test points inside a complex self intersecting polygon
func TestComplexGeoPolygons(t *testing.T) {
	tests := []struct {
		polygon []geo.Point
		points  []geoPoint
		field   string
		want    []string
	}{
		/*
			 /\      /\
			/__\____/__\
			    \  /
			     \/
		*/
		// a, b, c - inside and d - on vertices.
		{[]geo.Point{{Lon: 6.0, Lat: 2.0}, {Lon: 3.0, Lat: 4.0}, {Lon: 9.0, Lat: 6.0},
			{Lon: 3.0, Lat: 8.0}, {Lon: 6.0, Lat: 10.0}, {Lon: 6.0, Lat: 2.0}},
			[]geoPoint{{title: "a", lon: 3, lat: 4}, {title: "b", lon: 7, lat: 6}, {title: "c", lon: 4, lat: 8.1},
				{title: "d", lon: 6, lat: 10.0}, {title: "e", lon: 5, lat: 6}, {title: "f", lon: 7, lat: 5}},
			"loc", []string{"a", "b", "c", "d"}},
		/*
			____
			\  /
			 \/
			 /\
			/__\
		*/
		{[]geo.Point{{Lon: 7.0, Lat: 2.0}, {Lon: 1.0, Lat: 8.0}, {Lon: 1.0, Lat: 2.0},
			{Lon: 7.0, Lat: 8.0}, {Lon: 7.0, Lat: 2.0}},
			[]geoPoint{{title: "a", lon: 6, lat: 5}, {title: "b", lon: 5, lat: 5}, {title: "c", lon: 3, lat: 5.0},
				{title: "d", lon: 2, lat: 4.0}, {title: "e", lon: 5, lat: 3}, {title: "f", lon: 4, lat: 4}},
			"loc", []string{"a", "b", "c", "d"}},
	}

	for _, test := range tests {
		indexReader := setupComplexGeoPolygonPoints(test.points)
		got, err := testGeoPolygonSearch(indexReader, test.polygon, test.field)
		if err != nil {
			t.Fatal(err)
		}
		// convert wanted ids to doc nums
		var convertedWant []uint64
		for _, w := range test.want {
			convertedWant = append(convertedWant, indexReader.docNumByID(w))
		}
		if !reflect.DeepEqual(got, convertedWant) {
			t.Errorf("expected %v, got %v for polygon: %+v", test.want, got, test.polygon)
		}
		err = indexReader.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}

func setupComplexGeoPolygonPoints(points []geoPoint) *stubIndexReader {
	geoTestStubIndexReader := newStubIndexReader()
	for _, point := range points {
		geoTestStubIndexReader.add(
			&FakeDocument{
				NewFakeField("_id", point.title, true, false, false, nil),
				NewFakeGeoField("loc", point.lon, point.lat),
			})
	}
	return geoTestStubIndexReader
}
