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
// This implementation is inspired from the geohash-js
// ref: https://github.com/davetroy/geohash-js

package geo

// encoding encapsulates an encoding defined by a given base32 alphabet.
type encoding struct {
	enc string
	dec [256]byte
}

// newEncoding constructs a new encoding defined by the given alphabet,
// which must be a 32-byte string.
func newEncoding(encoder string) *encoding {
	e := new(encoding)
	e.enc = encoder
	for i := 0; i < len(e.dec); i++ {
		e.dec[i] = 0xff
	}
	for i := 0; i < len(encoder); i++ {
		e.dec[encoder[i]] = byte(i)
	}
	return e
}

// base32encoding with the Geohash alphabet.
var base32encoding = newEncoding("0123456789bcdefghjkmnpqrstuvwxyz")

var masks = []uint64{16, 8, 4, 2, 1}

// DecodeGeoHash decodes the string geohash faster with
// higher precision. This api is in experimental phase.
func DecodeGeoHash(geoHash string) (lat, lon float64) {
	even := true
	minLat, maxLat := -90.0, 90.0
	minLon, maxLon := -180.0, 180.0

	for i := 0; i < len(geoHash); i++ {
		cd := uint64(base32encoding.dec[geoHash[i]])
		for j := 0; j < bitsPerChar; j++ {
			if even {
				if cd&masks[j] > 0 {
					minLon = (minLon + maxLon) / 2
				} else {
					maxLon = (minLon + maxLon) / 2
				}
			} else {
				if cd&masks[j] > 0 {
					minLat = (minLat + maxLat) / 2
				} else {
					maxLat = (minLat + maxLat) / 2
				}
			}
			even = !even
		}
	}

	return (minLat + maxLat) / 2, (minLon + maxLon) / 2
}

const bitsPerChar = 5

func EncodeGeoHash(lat, lon float64) string {
	even := true
	minLat, maxLat := -90.0, 90.0
	minLon, maxLon := -180.0, 180.0
	var ch, bit uint64
	var geoHash [geoHashMaxLength]byte

	for n := 0; n < geoHashMaxLength; {
		if even {
			mid := (minLon + maxLon) / 2
			if lon > mid {
				ch |= masks[bit]
				minLon = mid
			} else {
				maxLon = mid
			}
		} else {
			mid := (minLat + maxLat) / 2
			if lat > mid {
				ch |= masks[bit]
				minLat = mid
			} else {
				maxLat = mid
			}
		}
		even = !even
		if bit < (bitsPerChar - 1) {
			bit++
		} else {
			geoHash[n] = base32encoding.enc[ch]
			n++
			ch = 0
			bit = 0
		}
	}

	return string(geoHash[:])
}
