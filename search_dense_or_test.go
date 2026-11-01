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

package riot

import (
	"context"
	"fmt"
	"strconv"
	"testing"
)

// A dense term (bitmap container postings) ORed after sparse terms must
// not erase their documents in the constant-score multi-term paths.
func TestMultiTermDenseTermKeepsSparseDocs(t *testing.T) {
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	}()
	batch := NewBatch()
	for i := range 10000 {
		body := "pad"
		switch {
		case i%2 == 0:
			body = "tzz"
		case i <= 67:
			body = fmt.Sprintf("ta%02d", (i-1)/4)
		}
		doc := NewDocument(strconv.Itoa(i)).AddField(NewTextField("body", body))
		batch.Update(doc.ID(), doc)
	}
	if err := w.Batch(batch); err != nil {
		t.Fatal(err)
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	}()
	for name, q := range map[string]Query{
		"wildcard": NewWildcardQuery("t*").SetField("body"),
		"prefix":   NewPrefixQuery("t").SetField("body"),
		"regexp":   NewRegexpQuery("t.*").SetField("body"),
	} {
		dmi, err := r.Search(context.Background(), NewTopNSearch(10, q).WithStandardAggregations())
		if err != nil {
			t.Fatal(err)
		}
		if got := dmi.Aggregations().Count(); got != 5034 {
			t.Errorf("%s: %d hits, want 5034", name, got)
		}
	}
}

// Prefix ORs a term range in one dictionary pass; two terms that are both
// dense (bitmap containers) in one chunk must union, not overwrite, across
// segments and with deletions.
func TestPrefixDenseTermsUnion(t *testing.T) {
	for name, config := range map[string]Config{
		"mem":  InMemoryOnlyConfig(),
		"disk": DefaultConfig(t.TempDir()),
	} {
		t.Run(name, func(t *testing.T) {
			w, err := OpenWriter(config)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := w.Close(); err != nil {
					t.Error(err)
				}
			}()
			batch := NewBatch()
			for i := range 10000 {
				body := "pa"
				if i%2 == 1 {
					body = "pb"
				}
				doc := NewDocument(strconv.Itoa(i)).AddField(NewTextField("body", body))
				batch.Update(doc.ID(), doc)
			}
			if err := w.Batch(batch); err != nil {
				t.Fatal(err)
			}
			batch = NewBatch()
			for i := 10000; i < 11000; i++ {
				doc := NewDocument(strconv.Itoa(i)).AddField(NewTextField("body", "pc"))
				batch.Update(doc.ID(), doc)
			}
			for i := range 100 {
				batch.Delete(Identifier(strconv.Itoa(i)))
			}
			if err := w.Batch(batch); err != nil {
				t.Fatal(err)
			}
			r, err := w.Reader()
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := r.Close(); err != nil {
					t.Error(err)
				}
			}()
			for prefix, want := range map[string]uint64{"p": 10900, "pb": 4950, "pc": 1000, "q": 0} {
				q := NewPrefixQuery(prefix).SetField("body")
				dmi, err := r.Search(context.Background(), NewTopNSearch(10, q).WithStandardAggregations())
				if err != nil {
					t.Fatal(err)
				}
				if got := dmi.Aggregations().Count(); got != want {
					t.Errorf("prefix %q: %d hits, want %d", prefix, got, want)
				}
			}
		})
	}
}

func TestEmptyMultiPhraseQueryNoPanic(t *testing.T) {
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	}()
	doc := NewDocument("1").AddField(NewTextField("body", "hello world"))
	if err := w.Update(doc.ID(), doc); err != nil {
		t.Fatal(err)
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	}()
	dmi, err := r.Search(context.Background(), NewTopNSearch(10, NewMultiPhraseQuery(nil).SetField("body")))
	if err != nil {
		t.Fatal(err)
	}
	if next, err := dmi.Next(); err != nil || next != nil {
		t.Fatalf("got %v, %v; want no hits", next, err)
	}
}
