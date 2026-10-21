package cmap

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// wideToUnicode returns a /ToUnicode program with one two-byte codespace and
// a bfrange mapping codes 0000 through last, so every code above last is in
// the codespace but unmapped.
func wideToUnicode(last int) string {
	var b strings.Builder
	b.WriteString("begincmap\n1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")
	b.WriteString("1 beginbfrange\n")
	fmt.Fprintf(&b, "<0000> <%04X> <4E00>\n", last)
	b.WriteString("endbfrange\nendcmap\n")
	return b.String()
}

// BenchmarkCMapMapUnmapped measures one lookup of a code the codespace holds
// and the mapping does not, the case that once scanned every mapping entry
// for a one-byte key. The cost per lookup stays flat as the mapping grows.
func BenchmarkCMapMapUnmapped(b *testing.B) {
	for _, last := range []int{0x00FF, 0x0FFF, 0xEFFF} {
		cm, err := Parse(context.Background(), []byte(wideToUnicode(last)))
		if err != nil {
			b.Fatalf("Parse: %v", err)
		}
		code := []byte{0xFF, 0xFE}
		b.Run(fmt.Sprintf("entries=%d", len(cm.Mapping)), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if n, _, ok := cm.Map(code); ok || n != 2 {
					b.Fatalf("Map = %d, %v, want 2, false", n, ok)
				}
			}
		})
	}
}

// TestCMapMapUnmappedAllocationFree pins that a lookup builds nothing per
// call: the codespace widths and the mixed-width flag are computed once per
// CMap, not once per shown code. AllocsPerRun refuses a parallel test.
func TestCMapMapUnmappedAllocationFree(t *testing.T) {
	cm, err := Parse(context.Background(), []byte(wideToUnicode(0xEFFF)))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	code := []byte{0xFF, 0xFE}
	allocs := testing.AllocsPerRun(100, func() {
		cm.Map(code)
	})
	if allocs != 0 {
		t.Fatalf("Map allocates %v times per lookup, want 0", allocs)
	}
}

// TestCMapMapMixedWidthIndex pins the mixed-width reading the precomputed
// index serves: a one-byte key inside a two-byte codespace makes an unmapped
// two-byte code advance one byte, and a CMap without one consumes the full
// width. Both a parsed CMap and one built as a literal take that reading.
func TestCMapMapMixedWidthIndex(t *testing.T) {
	t.Parallel()

	twoByte := []CodeSpaceRange{{Low: []byte{0x00, 0x00}, High: []byte{0xFF, 0xFF}}}
	tests := []struct {
		name  string
		cm    *CMap
		wantN int
	}{
		{
			name:  "parsed without one-byte keys",
			cm:    mustParse(t, wideToUnicode(0x00FF)),
			wantN: 2,
		},
		{
			name: "parsed with a one-byte key",
			cm: mustParse(t, "begincmap\n1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n"+
				"1 beginbfchar\n<41> <0041>\nendbfchar\nendcmap\n"),
			wantN: 1,
		},
		{
			name:  "literal without one-byte keys",
			cm:    &CMap{Mapping: map[string]string{"\x00\x41": "A"}, CodeSpaceRanges: twoByte},
			wantN: 2,
		},
		{
			name:  "literal with a one-byte key",
			cm:    &CMap{Mapping: map[string]string{"A": "A"}, CodeSpaceRanges: twoByte},
			wantN: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			n, _, ok := tt.cm.Map([]byte{0xFF, 0xFE})
			if ok || n != tt.wantN {
				t.Fatalf("Map = %d, %v, want %d, false", n, ok, tt.wantN)
			}
		})
	}
}

func mustParse(t *testing.T, src string) *CMap {
	t.Helper()
	cm, err := Parse(context.Background(), []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return cm
}

// TestCMapMapByte pins that a one-byte code reads the entry whose source code
// has its value, at any width, the one-byte entry first, and never the entry
// of a wider value that begins with it.
func TestCMapMapByte(t *testing.T) {
	t.Parallel()

	cm, err := Parse(context.Background(), []byte("begincmap\n"+
		"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n"+
		"4 beginbfchar\n<0041> <0061>\n<4142> <0078>\n<42> <0062>\n<0042> <0079>\nendbfchar\n"+
		"1 beginbfrange\n<0050> <0051> <0070>\nendbfrange\nendcmap\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, tc := range []struct {
		code byte
		want string
		ok   bool
	}{
		{'A', "a", true},
		{'B', "b", true},
		{'Q', "q", true},
		{'C', "", false},
	} {
		if got, ok := cm.MapByte(tc.code); got != tc.want || ok != tc.ok {
			t.Errorf("MapByte(%q) = %q, %v; want %q, %v", tc.code, got, ok, tc.want, tc.ok)
		}
	}
}
