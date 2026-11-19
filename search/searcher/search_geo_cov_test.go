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

package searcher

import (
	"slices"
	"testing"

	"github.com/vcaesar/riot/numeric/geo"
	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/similarity"
)

func geoBox(r search.Reader, checkBoundaries bool) (search.Searcher, error) {
	return NewGeoBoundingBoxSearcher(r, 0.001, 0.001, 9.002, 9.002, "loc", 1, nil, similarity.NewCompositeSumScorer(),
		testSearchOptions, checkBoundaries, testGeoPrecisionStep)
}

func TestGeoBoundingBoxSearcherCov(t *testing.T) {
	setInt(t, &MultiTermConstantScoreThreshold, 0)
	on, off, err := ComputeGeoRange(0, GeoBitsShift1Minus1, 0.001, 0.001, 9.002, 9.002, true, setupGeo(), "loc",
		testGeoPrecisionStep)
	if err != nil || len(on) == 0 || len(off) == 0 {
		t.Fatalf("boundary terms %d, inner %d: %v", len(on), len(off), err)
	}
	tests := []struct {
		name     string
		setup    func(r *covReader)
		maxCount int
	}{
		{name: "lookup", setup: func(r *covReader) { r.lookupFailAt = 1 }},
		{name: "lookup close", setup: func(r *covReader) { r.lookupCloseErr = errCov }},
		{name: "doc values", setup: func(r *covReader) { r.dvFailAt = 1 }},
		{name: "boundary terms", setup: func(r *covReader) { r.postFailAt = 1 }},
		{name: "inner terms", setup: func(r *covReader) { r.postFailAt = len(on) + 1 }},
		{name: "disjunction", setup: func(*covReader) {}, maxCount: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setInt(t, &DisjunctionMaxClauseCount, tc.maxCount)
			r := covReaderOf(setupGeo())
			tc.setup(r)
			if _, err := geoBox(r, true); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
	if _, _, err := ComputeGeoRange(0, GeoBitsShift1Minus1, 0, 0, 1, 1, false, nil, "loc", 16); err != nil {
		t.Fatalf("without a reader: %v", err)
	}
}

func TestGeoPointDistanceSearcherCov(t *testing.T) {
	ctx := covCtx()
	// a circle around the dateline is two boxes
	r := covReaderOf(setupGeo())
	s, err := NewGeoPointDistanceSearcher(r, 179.9, 0, 200000, "loc", 1, nil, similarity.NewCompositeSumScorer(),
		testSearchOptions, 16)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := covDrain(ctx, s); err != nil || len(got) != 0 {
		t.Fatalf("got %v %v", got, err)
	}
	tests := []struct {
		name     string
		lon      float64
		setup    func(r *covReader)
		maxCount int
	}{
		{name: "center", lon: 200, setup: func(*covReader) {}},
		{name: "box", lon: 0, setup: func(r *covReader) { r.lookupFailAt = 1 }},
		{name: "doc values", lon: 0, setup: func(r *covReader) { r.dvFailAt = 2 }},
		{name: "left box", lon: 179.9, setup: func(r *covReader) { r.lookupFailAt = 1 }},
		{name: "right box", lon: 179.9, setup: func(r *covReader) { r.lookupFailAt = 2 }},
		{name: "both boxes", lon: 179.9, setup: func(*covReader) {}, maxCount: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setInt(t, &DisjunctionMaxClauseCount, tc.maxCount)
			r := covReaderOf(setupGeo())
			tc.setup(r)
			if _, err := NewGeoPointDistanceSearcher(r, tc.lon, 0, 200000, "loc", 1, nil,
				similarity.NewCompositeSumScorer(), testSearchOptions, 16); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestGeoPolygonSearcherCov(t *testing.T) {
	square := []geo.Point{{Lon: 0, Lat: 0}, {Lon: 2.5, Lat: 0}, {Lon: 2.5, Lat: 2.5}, {Lon: 0, Lat: 2.5}}
	tests := []struct {
		name    string
		polygon []geo.Point
		setup   func(r *covReader)
	}{
		{name: "too few points", polygon: square[:2], setup: func(*covReader) {}},
		{name: "bad point", polygon: []geo.Point{{Lon: 200}, {Lon: 1}, {Lat: 1}}, setup: func(*covReader) {}},
		{name: "box", polygon: square, setup: func(r *covReader) { r.lookupFailAt = 1 }},
		{name: "doc values", polygon: square, setup: func(r *covReader) { r.dvFailAt = 2 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := covReaderOf(setupGeo())
			tc.setup(r)
			if _, err := NewGeoBoundedPolygonSearcher(r, tc.polygon, "loc", 1, nil,
				similarity.NewCompositeSumScorer(), testSearchOptions, 16); err == nil {
				t.Fatal("expected an error")
			}
		})
	}

	sir := setupGeo()
	dvr, err := sir.DocumentValueReader([]string{"loc"})
	if err != nil {
		t.Fatal(err)
	}
	b := &search.DocumentMatch{Number: sir.docNumByID("b")}
	// b, as indexed, lies on the first vertex
	hash := geo.MortonHash(1.0015, 1.0015)
	onVertex := []geo.Point{{Lon: geo.MortonUnhashLon(hash), Lat: geo.MortonUnhashLat(hash)}, {Lon: 0, Lat: 0}, {Lon: 2, Lat: 0}}
	if !buildPolygonFilter(dvr, onVertex)(b) {
		t.Fatal("a point on a vertex is inside")
	}
	if buildPolygonFilter(dvr, onVertex[:2])(b) {
		t.Fatal("a polygon of two points holds nothing")
	}
	s, err := NewGeoBoundedPolygonSearcher(covReaderOf(sir), square, "loc", 1, nil,
		similarity.NewCompositeSumScorer(), testSearchOptions, 16)
	if err != nil {
		t.Fatal(err)
	}
	got, err := covDrain(covCtx(), s)
	if err != nil || !slices.Contains(got, sir.docNumByID("b")) || slices.Contains(got, sir.docNumByID("d")) {
		t.Fatalf("got %v %v", got, err)
	}
}
