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
	"testing"

	"github.com/RoaringBitmap/roaring/v2"
)

// TestDisjointBitmaps covers the pigeonhole shortcut, empty bitmaps and
// run containers, whose Intersects materializes the intersection.
func TestDisjointBitmaps(t *testing.T) {
	runs := func(ranges ...uint64) *roaring.Bitmap {
		bm := roaring.New()
		for k := 0; k+1 < len(ranges); k += 2 {
			bm.AddRange(ranges[k], ranges[k+1])
		}
		bm.RunOptimize()
		return bm
	}
	for _, tc := range []struct {
		name string
		bms  []*roaring.Bitmap
		want bool
	}{
		{"none", nil, true},
		{"empty", []*roaring.Bitmap{roaring.New(), runs(0, 100)}, true},
		{"apart runs", []*roaring.Bitmap{runs(0, 100, 300, 400), runs(100, 300, 400, 500)}, true},
		{"touching runs", []*roaring.Bitmap{runs(0, 101), runs(100, 200)}, false},
		{"pigeonhole", []*roaring.Bitmap{runs(0, 60), runs(50, 100)}, false},
		{"sparse apart", []*roaring.Bitmap{roaring.BitmapOf(1, 3, 5), roaring.BitmapOf(2, 4, 70000)}, true},
		{"third shares", []*roaring.Bitmap{roaring.BitmapOf(1), roaring.BitmapOf(2), roaring.BitmapOf(9, 1)}, false},
	} {
		if got := disjointBitmaps(tc.bms); got != tc.want {
			t.Errorf("%s: disjointBitmaps = %v, want %v", tc.name, got, tc.want)
		}
	}
}
