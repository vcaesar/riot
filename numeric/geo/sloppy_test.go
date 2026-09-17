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
		if math.Abs(got-test.want) > 1e-9 {
			t.Errorf("earthDiameter(%f): want %f, got %f", test.lat, test.want, got)
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
