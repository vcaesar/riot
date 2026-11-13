package riot

import (
	"context"
	"strconv"
	"testing"
)

func TestNewDocumentWithoutID(t *testing.T) {
	if NewDocumentWithoutID().ID() != nil {
		t.Fatal("ID of a document without _id is not nil")
	}
	if NewDocumentWithoutID().AddField(NewKeywordField("k", "v")).ID() != nil {
		t.Fatal("ID of a document without _id read another field")
	}
	if id := NewDocument("a").ID(); id == nil || string(id.Term()) != "a" {
		t.Fatalf("ID %v, want a", id)
	}

	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	}()
	b := NewBatch()
	for i := range 300 {
		b.Insert(NewDocumentWithoutID().AddField(NewKeywordField("k", "t"+strconv.Itoa(i%3)).StoreValue()))
	}
	if err := w.Batch(b); err != nil {
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
	it, err := r.Search(context.Background(), NewAllMatches(NewTermQuery("t1").SetField("k")))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for m, err := it.Next(); m != nil || err != nil; m, err = it.Next() {
		if err != nil {
			t.Fatal(err)
		}
		n++
		err = m.VisitStoredFields(func(field string, value []byte) bool {
			if field != "k" || string(value) != "t1" {
				t.Errorf("stored %s=%s", field, value)
			}
			return true
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if n != 100 {
		t.Fatalf("matches %d, want 100", n)
	}
}

// Documents without _id survive a streamed on-disk build of several
// segments, a reopen, and a merge with documents that have an _id.
func TestStreamWithoutIDOnDisk(t *testing.T) {
	const n = 5000
	dir := t.TempDir()
	w := openConfigWriter(t, DefaultConfig(dir))
	st := w.NewStream(16 << 10)
	for i := range n {
		d := NewDocumentWithoutID().
			AddField(NewTextField("body", "common term"+strconv.Itoa(i%7)).StoreValue().SearchTermPositions()).
			AddField(NewNumericField("id", float64(i)).StoreValue())
		if err := st.Insert(d); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Commit(); err != nil {
		t.Fatal(err)
	}
	if files := len(segmentFiles(t, dir)); files < 2 {
		t.Fatalf("%d segment files, want several", files)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	w = openConfigWriter(t, DefaultConfig(dir))
	b := NewBatch()
	b.Insert(NewDocument("x").AddField(NewTextField("body", "common term3")))
	if err := w.Batch(b); err != nil {
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
	if c, err := r.Count(); err != nil || c != n+1 {
		t.Fatalf("count %d err %v, want %d", c, err, n+1)
	}
	it, err := r.Search(context.Background(), NewAllMatches(NewTermQuery("term3").SetField("body")))
	if err != nil {
		t.Fatal(err)
	}
	hits, ids := 0, 0
	for m, err := it.Next(); m != nil || err != nil; m, err = it.Next() {
		if err != nil {
			t.Fatal(err)
		}
		hits++
		if err := m.VisitStoredFields(func(field string, value []byte) bool {
			if field == "_id" {
				ids++
				if string(value) != "x" {
					t.Errorf("_id %q", value)
				}
			}
			return true
		}); err != nil {
			t.Fatal(err)
		}
	}
	if want := n/7 + 1; hits != want || ids != 1 {
		t.Fatalf("hits %d (want %d), stored _id %d (want 1)", hits, want, ids)
	}
}

// TestSortByIDFromStored sorts by _id, whose doc values the segment serves
// from the stored _id.
func TestSortByIDFromStored(t *testing.T) {
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	}()
	b := NewBatch()
	for _, id := range []string{"c", "a", "d", "b"} {
		b.Insert(NewDocument(id).AddField(NewKeywordField("k", "v")))
	}
	if err := w.Batch(b); err != nil {
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
	it, err := r.Search(context.Background(), NewTopNSearch(10, NewTermQuery("v").SetField("k")).SortBy([]string{"-_id"}))
	if err != nil {
		t.Fatal(err)
	}
	var got string
	for m, err := it.Next(); m != nil || err != nil; m, err = it.Next() {
		if err != nil {
			t.Fatal(err)
		}
		if err = m.VisitStoredFields(func(field string, value []byte) bool {
			if field == "_id" {
				got += string(value)
			}
			return true
		}); err != nil {
			t.Fatal(err)
		}
	}
	if got != "dcba" {
		t.Fatalf("order %q, want dcba", got)
	}
}

// A document without _id passed to Update (its ID() is nil) is inserted,
// and Delete of a nil ID removes nothing, in a batch and on the Writer.
func TestUpdateDeleteWithoutID(t *testing.T) {
	w := openConfigWriter(t, InMemoryOnlyConfig())
	defer func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	}()
	b := NewBatch()
	b.Insert(NewDocument("").AddField(NewKeywordField("k", "v")))
	d := NewDocumentWithoutID().AddField(NewKeywordField("k", "v"))
	b.Update(d.ID(), d)
	b.Delete(nil)
	if err := w.Batch(b); err != nil {
		t.Fatal(err)
	}
	d = NewDocumentWithoutID().AddField(NewKeywordField("k", "v"))
	if err := w.Update(d.ID(), d); err != nil {
		t.Fatal(err)
	}
	if err := w.Delete(nil); err != nil {
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
		t.Fatalf("count %d err %v, want 3", c, err)
	}
}

// A Stream ignores nil ids as Batch does, so a document without _id never
// deletes the documents whose _id is empty.
func TestStreamUpdateDeleteWithoutID(t *testing.T) {
	w := openConfigWriter(t, InMemoryOnlyConfig())
	defer func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	}()
	b := NewBatch()
	b.Insert(NewDocument("").AddField(NewKeywordField("k", "v")))
	if err := w.Batch(b); err != nil {
		t.Fatal(err)
	}
	st := w.NewStream(0)
	d := NewDocumentWithoutID().AddField(NewKeywordField("k", "v"))
	if err := st.Update(d.ID(), d); err != nil {
		t.Fatal(err)
	}
	st.Delete(nil)
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
	if c, err := r.Count(); err != nil || c != 2 {
		t.Fatalf("count %d err %v, want 2", c, err)
	}
}
