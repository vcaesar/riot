//  Copyright (c) 2020 The Bluge Authors.
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
	"net"
	"testing"

	"github.com/vcaesar/riot/analysis"
)

func TestBooleanField(t *testing.T) {
	for _, tc := range []struct {
		value bool
		term  string
	}{
		{true, "T"},
		{false, "F"},
	} {
		f := NewBooleanField("flag", tc.value)
		_ = f.Analyze(0)
		if got := f.AnalyzedLength(); got != 1 {
			t.Fatalf("value %v: expected 1 token, got %d", tc.value, got)
		}
		if _, ok := f.AnalyzedTokenFrequencies()[tc.term]; !ok {
			t.Fatalf("value %v: expected term %q", tc.value, tc.term)
		}
		if typ := f.analyzer.Analyze(f.Value())[0].Type; typ != analysis.Boolean {
			t.Fatalf("value %v: expected boolean token type, got %v", tc.value, typ)
		}
		decoded, err := DecodeBoolean(f.Value())
		if err != nil {
			t.Fatal(err)
		}
		if decoded != tc.value {
			t.Fatalf("decoded %v, want %v", decoded, tc.value)
		}
	}
	if _, err := DecodeBoolean([]byte("x")); err == nil {
		t.Fatal("expected error decoding invalid boolean term")
	}
}

func TestIPField(t *testing.T) {
	for _, addr := range []string{"192.168.1.10", "2001:db8::1"} {
		ip := net.ParseIP(addr)
		f := NewIPField("addr", ip)
		_ = f.Analyze(0)
		if got := f.AnalyzedLength(); got != 1 {
			t.Fatalf("%s: expected 1 token, got %d", addr, got)
		}
		if _, ok := f.AnalyzedTokenFrequencies()[string(ip.To16())]; !ok {
			t.Fatalf("%s: expected 16-byte term", addr)
		}
		if typ := f.analyzer.Analyze(f.Value())[0].Type; typ != analysis.IP {
			t.Fatalf("%s: expected ip token type, got %v", addr, typ)
		}
		decoded, err := DecodeIP(f.Value())
		if err != nil {
			t.Fatal(err)
		}
		if !decoded.Equal(ip) {
			t.Fatalf("decoded %v, want %v", decoded, ip)
		}
	}
	if _, err := DecodeIP([]byte{1, 2, 3, 4}); err == nil {
		t.Fatal("expected error decoding 4-byte ip term")
	}
}

func boolIPIndex(t *testing.T) *Reader {
	t.Helper()
	w, err := OpenWriter(InMemoryOnlyConfig())
	if err != nil {
		t.Fatal(err)
	}
	b := NewBatch()
	docs := []struct {
		id   string
		on   bool
		addr string
	}{
		{"a", true, "10.0.0.1"},
		{"b", false, "10.0.1.2"},
		{"c", true, "192.168.0.1"},
		{"d", false, "2001:db8::1"},
		{"e", true, "2001:db8:1::1"},
	}
	for _, d := range docs {
		doc := NewDocument(d.id).
			AddField(NewBooleanField("on", d.on)).
			AddField(NewIPField("addr", net.ParseIP(d.addr)))
		b.Update(doc.ID(), doc)
	}
	if err = w.Batch(b); err != nil {
		t.Fatal(err)
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	return r
}

func TestBooleanFieldQuery(t *testing.T) {
	r := boolIPIndex(t)
	for _, tc := range []struct {
		value bool
		want  int
	}{
		{true, 3},
		{false, 2},
	} {
		q := NewBooleanFieldQuery(tc.value).SetField("on")
		hits := collectHits(t, r, NewTopNSearch(10, q))
		if len(hits) != tc.want {
			t.Fatalf("value %v: got %d hits, want %d", tc.value, len(hits), tc.want)
		}
	}
}

func TestIPRangeQuery(t *testing.T) {
	r := boolIPIndex(t)
	for _, tc := range []struct {
		cidr string
		want int
	}{
		{"10.0.0.0/8", 2},
		{"10.0.0.0/24", 1},
		{"10.0.1.2", 1},
		{"192.168.0.0/16", 1},
		{"0.0.0.0/0", 3},
		{"2001:db8::/32", 2},
		{"2001:db8::/48", 1},
		{"2001:db8:1::1", 1},
		{"::/0", 5},
		{"172.16.0.0/12", 0},
	} {
		q := NewIPRangeQuery(tc.cidr).SetField("addr")
		if err := q.Validate(); err != nil {
			t.Fatalf("%s: %v", tc.cidr, err)
		}
		hits := collectHits(t, r, NewTopNSearch(10, q))
		if len(hits) != tc.want {
			t.Fatalf("%s: got %d hits, want %d", tc.cidr, len(hits), tc.want)
		}
	}

	q := NewIPRangeQuery("not-an-ip")
	if err := q.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
	if _, err := r.Search(t.Context(), NewTopNSearch(10, q.SetField("addr"))); err == nil {
		t.Fatal("expected search error for invalid cidr")
	}
}
