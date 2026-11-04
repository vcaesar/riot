package search

import (
	"testing"

	segment "github.com/vcaesar/bluge_segment_api"
)

type stubDVR struct{ id int }

func (stubDVR) VisitDocumentValues(uint64, segment.DocumentValueVisitor) error { return nil }

type stubReadable struct {
	dvr   stubDVR
	opens int
}

func (r *stubReadable) DocumentValueReader([]string) (segment.DocumentValueReader, error) {
	r.opens++
	return r.dvr, nil
}

// Alternating readers must each get their own doc value reader, opened once.
func TestDocValueReaderForReaderCache(t *testing.T) {
	ctx := NewSearchContext(1, 0)
	a, b := &stubReadable{dvr: stubDVR{1}}, &stubReadable{dvr: stubDVR{2}}
	for _, r := range []*stubReadable{a, a, b, a, b, b} {
		got, err := ctx.DocValueReaderForReader(r, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got.(stubDVR).id != r.dvr.id {
			t.Fatalf("got reader %d want %d", got.(stubDVR).id, r.dvr.id)
		}
	}
	if a.opens != 1 || b.opens != 1 {
		t.Fatalf("opens a=%d b=%d", a.opens, b.opens)
	}
}
