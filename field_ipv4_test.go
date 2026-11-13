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
	"encoding/binary"
	"net"
	"slices"
	"strconv"
	"testing"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/aggregations"
)

func mustIPv4Field(t *testing.T, name string, ip net.IP) *TermField {
	t.Helper()
	f, err := NewIPv4Field(name, ip)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func ipv4(u uint32) net.IP {
	ip := make(net.IP, net.IPv4len)
	binary.BigEndian.PutUint32(ip, u)
	return ip
}

func TestIPv4Field(t *testing.T) {
	for _, addr := range []string{"0.0.0.0", "10.0.0.1", "128.0.0.0", "255.255.255.255", "::ffff:192.168.1.10"} {
		ip := net.ParseIP(addr)
		f := mustIPv4Field(t, "ip", ip)
		if f.IndexDocValues() || !f.Index() || f.Store() {
			t.Fatalf("%s: default options %v, want index only", addr, f.FieldOptions)
		}
		if !f.Sortable().IndexDocValues() {
			t.Fatalf("%s: Sortable has no doc values", addr)
		}
		if _, ok := f.NumericValue(); !ok {
			t.Fatalf("%s: no numeric column value", addr)
		}
		f.Analyze(0)
		if f.AnalyzedLength() != 1 || len(f.AnalyzedTokenFrequencies()) != 1 {
			t.Fatalf("%s: terms %v", addr, f.AnalyzedTokenFrequencies())
		}
		got, err := DecodeIPv4(f.Value())
		if err != nil || !got.Equal(ip) || len(got) != net.IPv4len {
			t.Fatalf("%s: decoded %v %v", addr, got, err)
		}
	}
	for _, ip := range []net.IP{nil, net.ParseIP("2001:db8::1"), {1, 2}} {
		if f, err := NewIPv4Field("ip", ip); err == nil || f != nil {
			t.Fatalf("%v: no error", ip)
		}
	}
	for _, v := range []float64{-1, 1.5, 1 << 32} {
		if _, err := DecodeIPv4(NewNumericField("n", v).Value()); err == nil {
			t.Fatalf("decoded %v", v)
		}
	}
	if _, err := DecodeIPv4([]byte{1, 2, 3}); err == nil {
		t.Fatal("decoded garbage")
	}
	for _, cidr := range []string{"2001:db8::/32", "::1", "x", "10.0.0.0/33"} {
		if _, err := NewIPv4RangeQuery(cidr); err == nil {
			t.Fatalf("%s: no error", cidr)
		}
	}
}

// IPv4 fields with a column, without one (WithoutNumericColumn) and index
// only, across segments and a reopen: CIDR ranges match a brute-force
// filter, the column sorts in address order and terms buckets decode.
func TestIPv4FieldSearch(t *testing.T) {
	path := createTmpIndexPath(t)
	defer cleanupTmpIndexPath(t, path)
	w := openConfigWriter(t, DefaultConfig(path))
	const n = 3000
	addr := func(i int) uint32 { return uint32(i) * 2654435761 } //nolint:gosec // G115: i < n; unique, spread over all bits
	for lo := 0; lo < n; lo += 1000 {
		b := NewBatch()
		for i := lo; i < lo+1000; i++ {
			f := mustIPv4Field(t, "ip", ipv4(addr(i))).Sortable().StoreValue()
			if lo == 1000 {
				f.WithoutNumericColumn()
			}
			d := NewDocument(strconv.Itoa(i)).AddField(f).
				AddField(mustIPv4Field(t, "net", ipv4(0x0a000000+uint32(i%5))).Aggregatable()).
				AddField(mustIPv4Field(t, "plain", ipv4(addr(i))))
			b.Insert(d)
		}
		if err := w.Batch(b); err != nil {
			t.Fatal(err)
		}
	}
	oracle := func(keep func(u uint32) bool) []int {
		var ids []int
		for i := range n {
			if keep(addr(i)) {
				ids = append(ids, i)
			}
		}
		return ids
	}
	check := func(r *Reader) {
		t.Helper()
		for _, c := range []struct {
			cidr     string
			lo, mask uint32
		}{
			{"0.0.0.0/0", 0, 0},
			{"128.0.0.0/1", 0x80000000, 0x80000000},
			{"10.0.0.0/8", 0x0a000000, 0xff000000},
			{"::ffff:10.0.0.0/104", 0x0a000000, 0xff000000},
			{"200.0.0.0/6", 0xc8000000, 0xfc000000},
			{ipv4(addr(1234)).String(), addr(1234), 0xffffffff},
		} {
			want := oracle(func(u uint32) bool { return u&c.mask == c.lo })
			for _, field := range []string{"ip", "plain"} {
				q, err := NewIPv4RangeQuery(c.cidr)
				if err != nil {
					t.Fatal(err)
				}
				if got := rangeIDs(t, r, q.SetField(field)); !slices.Equal(got, want) {
					t.Fatalf("%s %s: %d docs want %d", field, c.cidr, len(got), len(want))
				}
			}
		}

		order := oracle(func(uint32) bool { return true })
		slices.SortFunc(order, func(a, b int) int { return int(int64(addr(a)) - int64(addr(b))) })
		for _, desc := range []bool{false, true} {
			sortBy, want := "ip", order[:5]
			if desc {
				sortBy, want = "-ip", slices.Clone(order[len(order)-5:])
				slices.Reverse(want)
			}
			var got []int
			for _, m := range collectHits(t, r, NewTopNSearch(5, NewMatchAllQuery()).SortBy([]string{sortBy})) {
				if err := m.VisitStoredFields(func(f string, v []byte) bool {
					switch f {
					case "_id":
						id, err := strconv.Atoi(string(v))
						if err != nil {
							t.Fatal(err)
						}
						got = append(got, id)
					case "ip":
						if _, err := DecodeIPv4(v); err != nil {
							t.Fatalf("stored ip: %v", err)
						}
					}
					return true
				}); err != nil {
					t.Fatal(err)
				}
			}
			if !slices.Equal(got, want) {
				t.Fatalf("sort %s: %v want %v", sortBy, got, want)
			}
		}

		req := NewTopNSearch(0, NewMatchAllQuery())
		req.AddAggregation("net", aggregations.NewTermsAggregation(search.Field("net"), 10))
		it, err := r.Search(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if err := drain(it); err != nil {
			t.Fatal(err)
		}
		buckets := it.Aggregations().Buckets("net")
		if len(buckets) != 5 {
			t.Fatalf("%d net buckets", len(buckets))
		}
		for _, b := range buckets {
			ip, err := DecodeIPv4([]byte(b.Name()))
			if err != nil || !ip.Mask(net.CIDRMask(29, 32)).Equal(net.IPv4(10, 0, 0, 0)) || b.Count() != n/5 {
				t.Fatalf("bucket %v %d %v", ip, b.Count(), err)
			}
		}
		var sum float64
		if err := r.VisitNumbers("net", func(_ uint64, v float64) error { sum += v; return nil }); err != nil {
			t.Fatal(err)
		}
		if want := float64(n)*0x0a000000 + n/5*(0+1+2+3+4); sum != want {
			t.Fatalf("net column sum %v want %v", sum, want)
		}
	}
	r, err := w.Reader()
	if err != nil {
		t.Fatal(err)
	}
	check(r)
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	w = openConfigWriter(t, DefaultConfig(path))
	defer func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	}()
	if r, err = w.Reader(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	}()
	check(r)
}
