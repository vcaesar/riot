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
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"
	"github.com/vcaesar/ice"

	"github.com/vcaesar/riot/index"
	"github.com/vcaesar/riot/search/aggregations"
)

func streamTestDoc(i int) *Document {
	return NewDocument(strconv.Itoa(i)).
		AddField(NewTextField("body", fmt.Sprintf("common search engine term%d term%d", i%97, i%13)).
			StoreValue().SearchTermPositions()).
		AddField(NewTextField("body", fmt.Sprintf("second value %d", i%5)).SearchTermPositions()).
		AddField(NewNumericField("id", float64(i)).StoreValue()).
		AddField(NewKeywordField("category", fmt.Sprintf("category%d", i%16)).StoreValue().Aggregatable()).
		AddField(NewCompositeFieldExcluding("_all", []string{"_id"})).
		SetTimestamp(int64(i))
}

// streamHits returns the sorted external IDs, stored body and counts of
// every query, plus the top-3 scores when withScores is set: scores depend
// on the segment layout, which background merges change.
func streamHits(t *testing.T, w *Writer, withScores bool) []string {
	t.Helper()
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	}()
	queries := []Query{
		NewTermQuery("common").SetField("body"),
		NewTermQuery("term42").SetField("body"),
		NewTermQuery("value").SetField("body"),
		NewMatchPhraseQuery("search engine").SetField("body"),
		NewMatchPhraseQuery("second value 3").SetField("body"),
		NewNumericRangeInclusiveQuery(100, 199, true, true).SetField("id"),
		NewTermQuery("category7").SetField("category"),
		NewTermQuery("term7").SetField("_all"),
		NewPrefixQuery("term1").SetField("body"),
	}
	var out []string
	for qi, q := range queries {
		var ids []string
		it, err := r.Search(context.Background(), NewAllMatches(q))
		if err != nil {
			t.Fatal(err)
		}
		for {
			m, err := it.Next()
			if err != nil {
				t.Fatal(err)
			}
			if m == nil {
				break
			}
			var id, body string
			if err := m.VisitStoredFields(func(field string, value []byte) bool {
				switch field {
				case "_id":
					id = string(value)
				case "body":
					body = string(value)
				}
				return true
			}); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id+"="+body)
		}
		slices.Sort(ids)
		req := NewTopNSearch(3, q)
		req.AddAggregation("count", aggregations.CountMatches())
		it, err = r.Search(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		var scores []string
		for {
			m, err := it.Next()
			if err != nil {
				t.Fatal(err)
			}
			if m == nil {
				break
			}
			if withScores {
				scores = append(scores, strconv.FormatFloat(m.Score, 'g', 6, 64))
			}
		}
		out = append(out, fmt.Sprintf("q%d n=%d count=%d top=%v ids=%v", qi, len(ids), it.Aggregations().Count(), scores, ids))
	}
	return out
}

func openTestWriter(t *testing.T) *Writer {
	t.Helper()
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	return w
}

func TestStreamMatchesBatch(t *testing.T) {
	const n = 5000
	bw := openConfigWriter(t, InMemoryOnlyConfig().WithBatchIndexing(0))
	b := NewBatch()
	for i := range n {
		b.Insert(streamTestDoc(i))
	}
	if err := bw.Batch(b); err != nil {
		t.Fatal(err)
	}
	for _, budget := range []int{1 << 30, 64 << 10} {
		t.Run(strconv.Itoa(budget), func(t *testing.T) {
			sw := openTestWriter(t)
			st := sw.NewStream(budget)
			for i := range n {
				if err := st.Insert(streamTestDoc(i)); err != nil {
					t.Fatal(err)
				}
			}
			if err := st.Commit(); err != nil {
				t.Fatal(err)
			}
			segs := sw.Status().TotIntroducedSegmentsBatch
			if budget > n*1024 && segs != 1 || budget < n*1024 && segs < 2 {
				t.Fatalf("budget %d built %d segments", budget, segs)
			}
			want, got := streamHits(t, bw, segs == 1), streamHits(t, sw, segs == 1)
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("query %d:\n got %.300s\nwant %.300s", i, got[i], want[i])
				}
			}
		})
	}
}

// The ice builder a Stream uses writes the segment New writes over the
// same analyzed riot documents: repeated fields, composite _all, numeric
// and keyword doc values, stored values and timestamps.
func TestSegmentBuilderMatchesNew(t *testing.T) {
	docs := make([]segment.Document, 3000)
	for i := range docs {
		d := streamTestDoc(i)
		d.Analyze()
		docs[i] = d
	}
	norm := InMemoryOnlyConfig().indexConfig.NormCalc
	seg, _, err := ice.New(docs, norm)
	if err != nil {
		t.Fatal(err)
	}
	b := ice.NewBuilder(norm)
	for _, d := range docs {
		if err := b.Add(d); err != nil {
			t.Fatal(err)
		}
	}
	built, _, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	var want, got bytes.Buffer
	if _, err := seg.WriteTo(&want, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := built.WriteTo(&got, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want.Bytes(), got.Bytes()) {
		t.Fatalf("builder segment %d bytes, New %d; contents differ", got.Len(), want.Len())
	}
}

func TestStreamCommitIsAtomic(t *testing.T) {
	w := openTestWriter(t)
	st := w.NewStream(16 << 10) // several segments before Commit
	for i := range 3000 {
		if err := st.Insert(streamTestDoc(i)); err != nil {
			t.Fatal(err)
		}
	}
	count := func() uint64 {
		r, err := w.Reader()
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := r.Close(); err != nil {
				t.Error(err)
			}
		}()
		n, err := r.Count()
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(); n != 0 {
		t.Fatalf("visible before Commit: %d", n)
	}
	if err := st.Commit(); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 3000 {
		t.Fatalf("count %d after Commit", n)
	}
	if s := w.Status(); s.TotBatches != 1 || s.TotUpdates != 3000 {
		t.Fatalf("stats %+v", s)
	}

	// update, delete and discard against the committed documents
	if err := st.Update(Identifier("1"), NewDocument("1").AddField(NewTextField("body", "replaced"))); err != nil {
		t.Fatal(err)
	}
	st.Delete(Identifier("2"))
	if err := st.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := st.Insert(streamTestDoc(9999)); err != nil {
		t.Fatal(err)
	}
	st.Discard()
	if err := st.Commit(); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 2999 {
		t.Fatalf("count %d after update/delete/discard", n)
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
	for q, want := range map[string]uint64{"replaced": 1, "common": 2998} {
		req := NewTopNSearch(0, NewTermQuery(q).SetField("body"))
		req.AddAggregation("count", aggregations.CountMatches())
		it, err := r.Search(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := it.Next(); err != nil {
			t.Fatal(err)
		}
		if got := it.Aggregations().Count(); got != want {
			t.Fatalf("%s: %d hits, want %d", q, got, want)
		}
	}
}

func openConfigWriter(t *testing.T, config Config) *Writer {
	t.Helper()
	w, err := OpenWriter(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	return w
}

func TestIndexModeConfig(t *testing.T) {
	const n = 5000
	batchOf := func() *index.Batch {
		b := NewBatch()
		for i := range n {
			b.Insert(streamTestDoc(i))
		}
		return b
	}
	base := openConfigWriter(t, InMemoryOnlyConfig().WithBatchIndexing(0))
	if err := base.Batch(batchOf()); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		config   Config
		segments uint64
	}{
		{"batch-default", InMemoryOnlyConfig().WithBatchIndexing(0), 1},
		{"batch-1000", InMemoryOnlyConfig().WithBatchIndexing(1000), 5},
		{"batch-larger", InMemoryOnlyConfig().WithBatchIndexing(n * 2), 1},
		{"stream-64KiB", InMemoryOnlyConfig().WithStreamIndexing(64 << 10), 0},
		{"stream-default", InMemoryOnlyConfig().WithStreamIndexing(0), 1},
		{"default", InMemoryOnlyConfig(), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := openConfigWriter(t, tc.config)
			b := batchOf()
			persisted := make(chan error, 1)
			b.SetPersistedCallback(func(err error) { persisted <- err })
			if err := w.Batch(b); err != nil {
				t.Fatal(err)
			}
			if err := <-persisted; err != nil {
				t.Fatal(err)
			}
			s := w.Status()
			if tc.segments > 0 && s.TotIntroducedSegmentsBatch != tc.segments ||
				tc.segments == 0 && s.TotIntroducedSegmentsBatch < 2 {
				t.Fatalf("%d segments, want %d", s.TotIntroducedSegmentsBatch, tc.segments)
			}
			if s.TotBatches != 1 || s.TotUpdates != n {
				t.Fatalf("stats %+v", s)
			}
			one := s.TotIntroducedSegmentsBatch == 1
			want, got := streamHits(t, base, one), streamHits(t, w, one)
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("query %d:\n got %.300s\nwant %.300s", i, got[i], want[i])
				}
			}
			// an update batch replaces across all the segments
			up := NewBatch()
			doc := NewDocument("3").AddField(NewTextField("body", "replaced"))
			up.Update(doc.ID(), doc)
			up.Delete(Identifier("4"))
			if err := w.Batch(up); err != nil {
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
			if c, err := r.Count(); err != nil || c != n-1 {
				t.Fatalf("count %d err %v", c, err)
			}
		})
	}
}

// Default configurations index a Batch like a Stream.
func TestDefaultIndexModeIsStream(t *testing.T) {
	for _, c := range []Config{DefaultConfig(t.TempDir()), InMemoryOnlyConfig(),
		DefaultConfigWithIndexConfig(index.InMemoryOnlyConfig())} {
		if c.indexConfig.IndexMode != IndexModeStream || c.indexConfig.StreamMemoryBudget != 0 {
			t.Fatalf("mode %d stream budget %d", c.indexConfig.IndexMode, c.indexConfig.StreamMemoryBudget)
		}
	}
	if c := InMemoryOnlyConfig().WithBatchIndexing(0); c.indexConfig.IndexMode != IndexModeBatch {
		t.Fatal("WithBatchIndexing did not select batch mode")
	}
}

// Batch modes that build several segments release each document's
// analysis once indexed; Analyze rebuilds it.
func TestReleaseAnalysis(t *testing.T) {
	w := openConfigWriter(t, InMemoryOnlyConfig().WithBatchIndexing(2))
	docs := []*Document{streamTestDoc(1), streamTestDoc(2), streamTestDoc(3)}
	b := NewBatch()
	for _, d := range docs {
		b.Insert(d)
	}
	if err := w.Batch(b); err != nil {
		t.Fatal(err)
	}
	terms := func(d *Document) int {
		n := 0
		for _, f := range *d {
			f.EachTerm(func(segment.FieldTerm) { n++ })
		}
		return n
	}
	for _, d := range docs {
		if n := terms(d); n != 0 {
			t.Fatalf("%d terms kept after indexing", n)
		}
	}
	docs[0].Analyze()
	if terms(docs[0]) == 0 {
		t.Fatal("Analyze did not rebuild the terms")
	}
}

// withoutBuilder registers the ice plugin without NewBuilder, so batches
// are built with New.
func withoutBuilder(config Config) Config {
	config.indexConfig = config.indexConfig.WithSegmentPlugin(&index.SegmentPlugin{
		Type: ice.Type, Version: ice.Version, New: ice.New, Load: ice.Load, Merge: ice.Merge,
	})
	return config
}

// Batch mode builds through the plugin's builder: one segment per batch,
// the results of New, and each document's analysis released once added
// even when the whole batch is one segment.
func TestBatchModeUsesBuilder(t *testing.T) {
	const n = 5000
	var docs []*Document
	batchOf := func() *index.Batch {
		docs = docs[:0]
		b := NewBatch()
		for i := range n {
			d := streamTestDoc(i)
			docs = append(docs, d)
			b.Insert(d)
		}
		return b
	}
	base := openConfigWriter(t, withoutBuilder(InMemoryOnlyConfig().WithBatchIndexing(0)))
	if err := base.Batch(batchOf()); err != nil {
		t.Fatal(err)
	}
	if terms := docTerms(docs[0]); terms == 0 {
		t.Fatal("New released the analysis of a one-segment batch")
	}
	w := openConfigWriter(t, InMemoryOnlyConfig().WithBatchIndexing(0))
	if err := w.Batch(batchOf()); err != nil {
		t.Fatal(err)
	}
	if s := w.Status(); s.TotIntroducedSegmentsBatch != 1 || s.TotUpdates != n {
		t.Fatalf("%d segments, %d updates", s.TotIntroducedSegmentsBatch, s.TotUpdates)
	}
	for _, d := range []*Document{docs[0], docs[n/2], docs[n-1]} {
		if terms := docTerms(d); terms != 0 {
			t.Fatalf("%d terms kept after indexing", terms)
		}
	}
	want, got := streamHits(t, base, true), streamHits(t, w, true)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("query %d:\n got %.300s\nwant %.300s", i, got[i], want[i])
		}
	}
}

func docTerms(d *Document) int {
	n := 0
	for _, f := range *d {
		f.EachTerm(func(segment.FieldTerm) { n++ })
	}
	return n
}

// A Stream analyzes each 2048 documents while it adds the 2048 before:
// Discard waits for that analysis, so the caller owns the documents again
// (the race detector checks), and Commit adds the last analyzed part even
// when no document is pending.
func TestStreamPipelinedAnalysis(t *testing.T) {
	const part = 2048 // index.streamAnalyzeDocs
	w := openTestWriter(t)
	st := w.NewStream(1 << 30)
	var docs []*Document
	for i := range 2 * part {
		d := streamTestDoc(i)
		docs = append(docs, d)
		if err := st.Insert(d); err != nil {
			t.Fatal(err)
		}
	}
	st.Discard()
	if docTerms(docs[part]) == 0 || docTerms(docs[2*part-1]) == 0 {
		t.Fatal("the part in flight was not analyzed")
	}
	for i := range 2 * part {
		if err := st.Insert(streamTestDoc(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Commit(); err != nil {
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
	if n, err := r.Count(); err != nil || n != 2*part {
		t.Fatalf("count %d err %v", n, err)
	}
	if s := w.Status(); s.TotUpdates != 2*part {
		t.Fatalf("%d updates", s.TotUpdates)
	}
}

func segmentFiles(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.seg"))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// On disk a Stream writes each flushed segment to its own file at flush,
// like Tantivy, and Commit publishes those files without merging them.
// Each 2048 documents are added once the next 2048 are in, so 7000 add
// two parts before Commit.
func TestStreamPersistsFlushedSegments(t *testing.T) {
	const n = 7000
	dir := t.TempDir()
	w, err := OpenWriter(DefaultConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	st := w.NewStream(16 << 10)
	for i := range n {
		if err := st.Insert(streamTestDoc(i)); err != nil {
			t.Fatal(err)
		}
	}
	flushed := len(segmentFiles(t, dir))
	if flushed < 2 {
		t.Fatalf("%d segment files before Commit", flushed)
	}
	if err := st.Commit(); err != nil {
		t.Fatal(err)
	}
	if s := w.Status(); s.TotMemMergeDone != 0 || s.TotPersistedSegments != 0 {
		t.Fatalf("flushed segments were rewritten: %+v", s)
	}
	want := streamHits(t, w, false)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w, err = OpenWriter(DefaultConfig(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	}()
	got := streamHits(t, w, false)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("query %d after reopen:\n got %.300s\nwant %.300s", i, got[i], want[i])
		}
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
	if c, err := r.Count(); err != nil || c != n {
		t.Fatalf("count %d err %v", c, err)
	}
}

// Discard removes the files of the segments it flushed.
func TestStreamDiscardRemovesFlushedFiles(t *testing.T) {
	dir := t.TempDir()
	w := openConfigWriter(t, DefaultConfig(dir))
	before := len(segmentFiles(t, dir))
	st := w.NewStream(16 << 10)
	for i := range 5000 {
		if err := st.Insert(streamTestDoc(i)); err != nil {
			t.Fatal(err)
		}
	}
	if len(segmentFiles(t, dir)) == before {
		t.Fatal("nothing flushed")
	}
	st.Discard()
	if n := len(segmentFiles(t, dir)); n != before {
		t.Fatalf("%d segment files after Discard, want %d", n, before)
	}
	if err := st.Commit(); err != nil {
		t.Fatal(err)
	}
}

func logTestDoc(i int) *Document {
	return NewDocument(strconv.Itoa(i)).
		AddField(NewTextField("msg", fmt.Sprintf("GET /api/v1/items/%d status=%d latency_ms=%d host=web-%d user agent curl",
			i%5000, 200+i%3*100, i%997, i%32)).StoreValue()).
		AddField(NewKeywordField("level", []string{"info", "warn", "error"}[i%3]).StoreValue().Aggregatable()).
		AddField(NewNumericField("ts", float64(1_700_000_000+i)).StoreValue()).
		SetTimestamp(int64(1_700_000_000 + i))
}

// Log-style ingestion: many small batches, through the builder (default)
// and through New (plugin without a builder).
func BenchmarkSmallBatches(b *testing.B) {
	const total = 20000
	for _, size := range []int{10, 100, 1000} {
		for _, path := range []struct {
			name   string
			config func() Config
		}{
			{"builder", func() Config { return InMemoryOnlyConfig().WithBatchIndexing(0) }},
			{"new", func() Config { return withoutBuilder(InMemoryOnlyConfig().WithBatchIndexing(0)) }},
		} {
			b.Run(fmt.Sprintf("%s/%d", path.name, size), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					w, err := OpenWriter(path.config())
					if err != nil {
						b.Fatal(err)
					}
					for lo := 0; lo < total; lo += size {
						batch := NewBatch()
						for i := lo; i < lo+size; i++ {
							batch.Insert(logTestDoc(i))
						}
						if err := w.Batch(batch); err != nil {
							b.Fatal(err)
						}
					}
					if err := w.Close(); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// The memory budget is checked per document, so a tiny budget builds a
// segment for each document rather than one per analysis batch.
func TestStreamBudgetPerDocument(t *testing.T) {
	w := openConfigWriter(t, InMemoryOnlyConfig())
	st := w.NewStream(1)
	for i := range 3 {
		if err := st.Insert(NewDocument(strconv.Itoa(i))); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Commit(); err != nil {
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
	if c, err := r.Count(); err != nil || c != 3 {
		t.Fatalf("count %d err %v", c, err)
	}
	if n := w.Status().TotIntroducedSegmentsBatch; n != 3 {
		t.Fatalf("%d segments introduced, want 3", n)
	}
}

// A bulk Batch in the default stream mode is built into one segment, where
// a NewStream-sized budget would split it, unless a budget is configured.
func TestBatchStreamDefaultBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("indexes 40K documents")
	}
	const n = 40_000
	segments := func(config Config) uint64 {
		w := openConfigWriter(t, config)
		b := NewBatch()
		for i := range n {
			b.Insert(streamTestDoc(i))
		}
		if err := w.Batch(b); err != nil {
			t.Fatal(err)
		}
		return w.Status().TotIntroducedSegmentsBatch
	}
	if got := segments(InMemoryOnlyConfig().WithStreamIndexing(index.DefaultStreamMemoryBudget)); got < 2 {
		t.Fatalf("%d segments at the NewStream budget; the batch is too small to test", got)
	}
	if got := segments(InMemoryOnlyConfig()); got != 1 {
		t.Fatalf("default config: %d segments, want 1", got)
	}
}
