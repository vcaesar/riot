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

package geo

import (
	"math"
)

var earthDiameterPerLatitude []float64

const (
	radiusTabsSize = (1 << 10) + 1
	radiusDelta    = (math.Pi / 2) / (radiusTabsSize - 1)
	radiusIndexer  = 1 / radiusDelta
)

func init() {
	// initializes the table used to estimate the earth's diameter
	a := 6378137.0
	b := 6356752.31420
	a2 := a * a
	b2 := b * b
	earthDiameterPerLatitude = make([]float64, radiusTabsSize)
	earthDiameterPerLatitude[0] = 2.0 * a / 1000
	earthDiameterPerLatitude[radiusTabsSize-1] = 2.0 * b / 1000
	for i := 1; i < radiusTabsSize-1; i++ {
		lat := float64(i) * radiusDelta
		sin, cos := math.Sincos(lat)
		cos2, sin2 := cos*cos, sin*sin
		radius := math.Sqrt((a2*a2*cos2 + b2*b2*sin2) / (a2*cos2 + b2*sin2))
		earthDiameterPerLatitude[i] = 2 * radius / 1000
	}
}

// earthDiameter returns an estimation of the earth's diameter at the specified
// latitude in kilometers
func earthDiameter(lat float64) float64 {
	index := math.Abs(lat)*radiusIndexer + 0.5
	if index < float64(len(earthDiameterPerLatitude)) {
		return earthDiameterPerLatitude[int(index)]
	}
	index = math.Mod(index, float64(len(earthDiameterPerLatitude)))
	if math.IsNaN(index) {
		return 0
	}
	return earthDiameterPerLatitude[int(index)]
}
