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
	"sync/atomic"
	"testing"
)

func TestStreamPackedBuffer(t *testing.T) {
	cfgs := map[string]Config{
		"packed":  covWithPlugin(covDirConfig(t, nil), covIcePlugin()),
		"builder": covDirConfig(t, nil),
	}
	for name, cfg := range cfgs {
		t.Run(name, func(t *testing.T) {
			w := covWriter(t, cfg)
			if st := w.NewStream(0); st.budget != DefaultStreamMemoryBudget {
				t.Fatalf("budget %d", st.budget)
			}
			w.config.StreamMemoryBudget = 1
			st := w.NewStream(0)
			if st.budget != 1 {
				t.Fatalf("budget %d", st.budget)
			}
			for _, doc := range covDocs() {
				if err := st.Insert(doc); err != nil {
					t.Fatal(err)
				}
			}
			if err := st.Insert(nil); err != nil {
				t.Fatal(err)
			}
			if err := st.Commit(); err != nil {
				t.Fatal(err)
			}
			if err := st.Commit(); err != nil {
				t.Fatal(err)
			}
			if n := covCount(t, w); n != 4 {
				t.Fatalf("count %d", n)
			}

			// replace 0, delete 1, insert one without _id
			if err := st.Update(testIdentifier("0"), covDoc("0", "z", 1)); err != nil {
				t.Fatal(err)
			}
			if err := st.Update(nil, covDoc("x", "z", 2)); err != nil {
				t.Fatal(err)
			}
			st.Delete(testIdentifier("1"))
			st.Delete(nil)
			if err := st.Commit(); err != nil {
				t.Fatal(err)
			}
			if n := covCount(t, w); n != 4 {
				t.Fatalf("count after update %d", n)
			}

			// discarded documents and their flushed files go away
			for _, doc := range covDocs() {
				if err := st.Insert(doc); err != nil {
					t.Fatal(err)
				}
			}
			if err := st.pack(); err != nil {
				t.Fatal(err)
			}
			if err := st.pack(); err != nil {
				t.Fatal(err)
			}
			if len(st.segments) == 0 {
				t.Fatal("nothing flushed")
			}
			st.Discard()
			if err := st.Commit(); err != nil {
				t.Fatal(err)
			}
			if n := covCount(t, w); n != 4 {
				t.Fatalf("count after discard %d", n)
			}
		})
	}
}

func TestStreamErrors(t *testing.T) {
	tests := []struct {
		name   string
		cfg    func(t *testing.T) Config
		budget int
	}{
		{"flush in pack", func(t *testing.T) Config { return covFailingNew(covDirConfig(t, nil)) }, 1},
		{"flush in commit", func(t *testing.T) Config { return covFailingNew(covDirConfig(t, nil)) }, 1 << 30},
		{"persist flushed", func(t *testing.T) Config {
			return covDirConfig(t, func(d *covFaultDir) { d.persist = covFailKind(ItemKindSegment) })
		}, 1},
		{"load flushed", func(t *testing.T) Config { return covFailingLoad(covDirConfig(t, nil)) }, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := covWriter(t, tt.cfg(t))
			st := w.NewStream(tt.budget)
			if err := st.Insert(covDoc("0", "a", 1)); err != nil {
				t.Fatal(err)
			}
			if err := st.Commit(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestStreamDiscardRemoveError(t *testing.T) {
	var asyncErrs atomic.Int64
	cfg := covDirConfig(t, func(d *covFaultDir) { d.remove = covFailKind(ItemKindSegment) })
	cfg.AsyncError = func(error) { asyncErrs.Add(1) }
	w := covWriter(t, cfg)
	st := w.NewStream(1)
	for _, doc := range covDocs() {
		if err := st.Insert(doc); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.pack(); err != nil {
		t.Fatal(err)
	}
	if err := st.pack(); err != nil {
		t.Fatal(err)
	}
	st.Discard()
	if asyncErrs.Load() == 0 {
		t.Fatal("discard error not reported")
	}
}
