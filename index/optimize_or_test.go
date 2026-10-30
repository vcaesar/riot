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

package index

import (
	"math/rand"
	"testing"

	"github.com/RoaringBitmap/roaring/v2"
)

func TestOrBitmapsMatchesFastOr(t *testing.T) {
	rnd := rand.New(rand.NewSource(1))
	for _, docs := range []uint32{1, 63, 64, 1000, 70_000, 200_000} {
		numDocs := uint64(docs)
		for _, n := range []int{0, 1, 2, 3, 50} {
			bms := make([]*roaring.Bitmap, n)
			for i := range bms {
				bms[i] = roaring.New()
				for range rnd.Intn(300) {
					bms[i].Add(rnd.Uint32() % docs)
				}
			}
			want := roaring.FastOr(bms...)
			got := orBitmaps(bms, numDocs)
			if !got.Equals(want) {
				t.Fatalf("numDocs %d, %d bitmaps: got %d docs, want %d", numDocs, n, got.GetCardinality(), want.GetCardinality())
			}
			if n > 0 {
				before := bms[0].Clone()
				got.Add(docs - 1)
				if !bms[0].Equals(before) {
					t.Fatalf("numDocs %d, %d bitmaps: result aliases its input", numDocs, n)
				}
			}
		}
	}
	// postings beyond the stated size must still be ORed correctly
	over := roaring.BitmapOf(5, 1_000_000)
	if got := orBitmaps([]*roaring.Bitmap{over, roaring.BitmapOf(1), roaring.BitmapOf(2)}, 10); !got.Equals(roaring.BitmapOf(1, 2, 5, 1_000_000)) {
		t.Fatalf("out of range postings: %v", got.ToArray())
	}
}
