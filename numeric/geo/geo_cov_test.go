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

package geo

import (
	"math"
	"testing"
)

func TestRectFromPointDistanceCases(t *testing.T) {
	tests := []struct {
		name    string
		lon     float64
		lat     float64
		dist    float64
		wantErr bool
		check   func(tlLon, tlLat, brLon, brLat float64) bool
	}{
		{name: "lon too low", lon: -181, lat: 0, dist: 1, wantErr: true},
		{name: "lon NaN", lon: math.NaN(), lat: 0, dist: 1, wantErr: true},
		{name: "lat too high", lon: 0, lat: 91, dist: 1, wantErr: true},
		{name: "lat NaN", lon: 0, lat: math.NaN(), dist: 1, wantErr: true},
		{
			name: "wrap min lon", lon: -179.9999, lat: 0, dist: 10000,
			check: func(tlLon, _, brLon, _ float64) bool { return tlLon > 179 && brLon < -179 },
		},
		{
			name: "wrap max lon", lon: 179.9999, lat: 0, dist: 10000,
			check: func(tlLon, _, brLon, _ float64) bool { return tlLon > 179 && brLon < -179 },
		},
		{
			name: "north pole", lon: 10, lat: 89.9999, dist: 10000,
			check: func(tlLon, tlLat, brLon, _ float64) bool { return tlLon == -180 && brLon == 180 && tlLat == 90 },
		},
		{
			name: "south pole", lon: 10, lat: -89.9999, dist: 10000,
			check: func(tlLon, _, brLon, brLat float64) bool { return tlLon == -180 && brLon == 180 && brLat == -90 },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tlLon, tlLat, brLon, brLat, err := RectFromPointDistance(tt.lon, tt.lat, tt.dist)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				if tlLon != 0 || tlLat != 0 || brLon != 0 || brLat != 0 {
					t.Errorf("expected zero rect on error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.check(tlLon, tlLat, brLon, brLat) {
				t.Errorf("unexpected rect %v %v %v %v", tlLon, tlLat, brLon, brLat)
			}
		})
	}
}

func TestBoundingRectangleForPolygon(t *testing.T) {
	tests := []struct {
		name    string
		polygon []Point
		wantErr bool
		want    [4]float64
	}{
		{name: "first lon invalid", polygon: []Point{{Lon: 200, Lat: 0}}, wantErr: true},
		{name: "first lat invalid", polygon: []Point{{Lon: 0, Lat: -95}}, wantErr: true},
		{name: "later lon invalid", polygon: []Point{{Lon: 0, Lat: 0}, {Lon: -200, Lat: 0}}, wantErr: true},
		{name: "later lat invalid", polygon: []Point{{Lon: 0, Lat: 0}, {Lon: 0, Lat: 95}}, wantErr: true},
		{name: "single point", polygon: []Point{{Lon: 1, Lat: 2}}, want: [4]float64{1, 2, 1, 2}},
		{
			name:    "triangle",
			polygon: []Point{{Lon: 1, Lat: 2}, {Lon: -3, Lat: 5}, {Lon: 4, Lat: -1}},
			want:    [4]float64{-3, 5, 4, -1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tlLon, tlLat, brLon, brLat, err := BoundingRectangleForPolygon(tt.polygon)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := [4]float64{tlLon, tlLat, brLon, brLat}; got != tt.want {
				t.Errorf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestExtractGeoPointString(t *testing.T) {
	hashLat, hashLon := DecodeGeoHash("u4pruydqqvj")
	tests := []struct {
		name    string
		in      interface{}
		lon     float64
		lat     float64
		success bool
	}{
		{name: "nil", in: nil},
		{name: "lat,lon", in: "40.5, -73.25", lon: -73.25, lat: 40.5, success: true},
		{name: "no spaces", in: "1,2", lon: 2, lat: 1, success: true},
		{name: "bad lat", in: "x,2", lon: 2},
		{name: "bad lon", in: "1,y", lat: 1},
		{name: "three parts", in: "1,2,3"},
		{name: "geohash", in: "u4pruydqqvj", lon: hashLon, lat: hashLat, success: true},
		{name: "too long for geohash", in: "u4pruydqqvjxyz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lon, lat, success := ExtractGeoPoint(tt.in)
			if success != tt.success || lon != tt.lon || lat != tt.lat {
				t.Errorf("expected %v %v %v, got %v %v %v", tt.lon, tt.lat, tt.success, lon, lat, success)
			}
		})
	}
}
