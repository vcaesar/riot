//  Copyright (c) 2017 Couchbase, Inc.
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

func BenchmarkEarthDiameterLookup(b *testing.B) {
	b.Run("modulo", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			lat := float64(i%180-90) * degreesToRadian
			index := math.Mod(math.Abs(lat)*radiusIndexer+0.5, float64(len(earthDiameterPerLatitude)))
			benchmarkDistance = earthDiameterPerLatitude[int(index)]
		}
	})
	b.Run("fast", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			lat := float64(i%180-90) * degreesToRadian
			benchmarkDistance = earthDiameter(lat)
		}
	})
}

func TestEarthDiameterLookupCompatibility(t *testing.T) {
	latitudes := []float64{math.NaN(), math.Inf(1), math.Inf(-1), math.MaxFloat64, -math.MaxFloat64}
	for i := -4096; i <= 4096; i++ {
		lat := float64(i) * radiusDelta / 2
		latitudes = append(latitudes, lat, math.Nextafter(lat, math.Inf(-1)), math.Nextafter(lat, math.Inf(1)))
	}
	for _, lat := range latitudes {
		index := math.Mod(math.Abs(lat)*radiusIndexer+0.5, float64(len(earthDiameterPerLatitude)))
		var want float64
		if !math.IsNaN(index) {
			want = earthDiameterPerLatitude[int(index)]
		}
		if got := earthDiameter(lat); got != want {
			t.Fatalf("earthDiameter(%g): want %g, got %g", lat, want, got)
		}
	}
}

func TestEarthDiameter(t *testing.T) {
	const equatorial = 2 * 6378.137
	const polar = 2 * 6356.75231420

	tests := []struct {
		lat  float64
		want float64
	}{
		{math.NaN(), 0},
		{0, equatorial},
		{math.Pi / 2, polar},
		{-math.Pi / 2, polar},
	}

	for _, test := range tests {
		got := earthDiameter(test.lat)
		if math.IsNaN(got) || math.Abs(got-test.want) > 1e-9 {
			t.Errorf("earthDiameter(%f): want %f, got %f", test.lat, test.want, got)
		}
	}

	for i := 0; i < radiusTabsSize; i++ {
		lat := float64(i) * radiusDelta
		cos, sin := math.Cos(lat), math.Sin(lat)
		a, b := equatorial/2, polar/2
		x, y := a*cos, b*sin
		ax, by := a*x, b*y
		want := 2 * math.Sqrt((ax*ax+by*by)/(x*x+y*y))
		for _, signedLat := range []float64{lat, -lat} {
			got := earthDiameter(signedLat)
			if math.IsNaN(got) || math.Abs(got-want) > 1e-9 {
				t.Fatalf("earthDiameter(%g): want %.12f, got %.12f", signedLat, want, got)
			}
		}
	}

	// diameter must shrink monotonically from the equator to the pole
	prev := earthDiameter(0)
	for lat := 0.01; lat <= math.Pi/2; lat += 0.01 {
		d := earthDiameter(lat)
		if d > prev {
			t.Fatalf("earthDiameter not monotonic at lat %f: %f > %f", lat, d, prev)
		}
		prev = d
	}
}
