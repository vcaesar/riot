// Copyright (c) 2026 The Bluge Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package geo

import "testing"

var benchmarkDistance float64
var benchmarkBounds [4]float64

func BenchmarkHaversinCases(b *testing.B) {
	for _, tc := range []struct {
		name                   string
		lon1, lat1, lon2, lat2 float64
	}{
		{"local", -74.0059731, 40.7143528, -73.95, 40.65},
		{"global", -74, 40.7, 151.2, -33.8},
		{"tiny", 0, 0, 1e-7, 0},
		{"antipodal", 0, 0, 180, 0},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				benchmarkDistance = Haversin(tc.lon1, tc.lat1, tc.lon2, tc.lat2)
			}
		})
	}
}

func BenchmarkRectFromPointDistance(b *testing.B) {
	for _, tc := range []struct {
		name             string
		lon, lat, meters float64
	}{
		{"local", -74, 40.7, 1000},
		{"dateline", 179.9, 45, 50000},
		{"polar", 0, 89.9, 50000},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				left, top, right, bottom, err := RectFromPointDistance(tc.lon, tc.lat, tc.meters)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkBounds = [4]float64{left, top, right, bottom}
			}
		})
	}
}
