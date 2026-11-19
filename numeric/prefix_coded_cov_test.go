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

package numeric

import (
	"bytes"
	"math"
	"testing"
)

func TestNewPrefixCodedInt64Errors(t *testing.T) {
	rv, err := NewPrefixCodedInt64(1, 64)
	if err == nil || rv != nil {
		t.Fatalf("expected error for shift 64, got %v, %v", rv, err)
	}

	prealloc := make([]byte, 4)
	rv, rest, err := NewPrefixCodedInt64Prealloc(1, 100, prealloc)
	if err == nil || rv != nil {
		t.Fatalf("expected error for shift 100, got %v, %v", rv, err)
	}
	if len(rest) != len(prealloc) {
		t.Errorf("expected prealloc returned untouched, got len %d", len(rest))
	}
}

func TestNewPrefixCodedInt64Prealloc(t *testing.T) {
	tests := []struct {
		name     string
		shift    uint
		prealloc int
		wantRest int
	}{
		{name: "fits", shift: 0, prealloc: 15, wantRest: 4},
		{name: "exact", shift: 32, prealloc: 6, wantRest: 0},
		{name: "too small", shift: 0, prealloc: 3, wantRest: 0},
		{name: "nil", shift: 8, prealloc: 0, wantRest: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var prealloc []byte
			if tt.prealloc > 0 {
				prealloc = make([]byte, tt.prealloc)
			}
			rv, rest, err := NewPrefixCodedInt64Prealloc(314729851, tt.shift, prealloc)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(rest) != tt.wantRest {
				t.Errorf("expected rest len %d, got %d", tt.wantRest, len(rest))
			}
			want := MustNewPrefixCodedInt64(314729851, tt.shift)
			if !bytes.Equal(rv, want) {
				t.Errorf("expected %#v, got %#v", want, rv)
			}
			if cap(rv) != len(rv) {
				t.Errorf("expected cap == len, got cap %d len %d", cap(rv), len(rv))
			}
		})
	}
}

func TestMustNewPrefixCodedInt64Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic for invalid shift")
		}
	}()
	MustNewPrefixCodedInt64(1, 64)
}

func TestAppendPrefixCodedInt64(t *testing.T) {
	tests := []struct {
		name string
		in   int64
		buf  []byte
	}{
		{name: "nil buf", in: 1},
		{name: "negative", in: -94582},
		{name: "min", in: math.MinInt64, buf: []byte("x")},
		{name: "max", in: math.MaxInt64, buf: []byte("prefix")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix := append([]byte(nil), tt.buf...)
			got := AppendPrefixCodedInt64(tt.buf, tt.in)
			want := append(append([]byte(nil), prefix...), MustNewPrefixCodedInt64(tt.in, 0)...)
			if !bytes.Equal(got, want) {
				t.Fatalf("expected %#v, got %#v", want, got)
			}
			back, err := PrefixCoded(got[len(prefix):]).Int64()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if back != tt.in {
				t.Errorf("expected %d, got %d", tt.in, back)
			}
		})
	}
}

func TestPrefixCodedInvalid(t *testing.T) {
	tests := []struct {
		name string
		p    PrefixCoded
	}{
		{name: "nil", p: nil},
		{name: "empty", p: PrefixCoded{}},
		{name: "shift too high", p: PrefixCoded{ShiftStartInt64 + 64}},
		{name: "below start", p: PrefixCoded{ShiftStartInt64 - 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.p.Shift(); err == nil {
				t.Errorf("expected Shift error")
			}
			if v, err := tt.p.Int64(); err == nil || v != 0 {
				t.Errorf("expected Int64 error, got %d, %v", v, err)
			}
		})
	}
}

func TestPrefixCodedInt64Shifted(t *testing.T) {
	p := MustNewPrefixCodedInt64(314729851, 8)
	got, err := p.Int64()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := int64(314729851) &^ 0xff; got != want {
		t.Errorf("expected %d, got %d", want, got)
	}
}

func TestValidPrefixCodedTermBytesEmpty(t *testing.T) {
	for _, p := range [][]byte{nil, {}} {
		valid, shift := ValidPrefixCodedTermBytes(p)
		if valid || shift != 0 {
			t.Errorf("expected invalid for %#v, got %v, %d", p, valid, shift)
		}
	}
	valid, shift := ValidPrefixCodedTermBytes(MustNewPrefixCodedInt64(7, 16))
	if !valid || shift != 16 {
		t.Errorf("expected valid shift 16, got %v, %d", valid, shift)
	}
}
