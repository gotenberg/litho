package cmap

import (
	"context"
	"testing"
)

// TestCIDMap_CodeLength pins that CodeLength splits a code as Decode does,
// re-aligning one byte past a two-byte code the mixed-width CMap leaves
// unmapped, and reporting a byte no codespace range holds.
func TestCIDMap_CodeLength(t *testing.T) {
	t.Parallel()

	m := ParseEmbeddedCIDMap(context.Background(), []byte(`begincmap
2 begincodespacerange
<00> <7F>
<C280> <DFBF>
endcodespacerange
2 begincidrange
<20> <7E> 32
<C380> <C3BF> 192
endcidrange
endcmap`))
	if m == nil {
		t.Fatal("ParseEmbeddedCIDMap returned nil")
	}
	tests := []struct {
		data      string
		n         int
		undefined bool
	}{
		{data: ""},
		{data: "A", n: 1},
		{data: "\xC3\xA9", n: 2},
		{data: "\xC2\x80", n: 1},
		{data: "\xFF", n: 1, undefined: true},
	}
	for _, tt := range tests {
		if n, undefined := m.CodeLength([]byte(tt.data)); n != tt.n || undefined != tt.undefined {
			t.Errorf("CodeLength(%q) = (%d, %v), want (%d, %v)", tt.data, n, undefined, tt.n, tt.undefined)
		}
	}
}
