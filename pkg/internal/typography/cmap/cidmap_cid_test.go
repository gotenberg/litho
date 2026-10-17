package cmap

import (
	"context"
	"testing"
)

// TestCIDMap_MappedCID pins the metrics lookup on a UTF-8 CMap of the shape
// Haru writes: <C3A9> selects CID 233 through its cidrange, and a code that
// only a notdef range or no range covers has no mapped CID.
func TestCIDMap_MappedCID(t *testing.T) {
	t.Parallel()

	data := []byte(`/CIDInit /ProcSet findresource begin
12 dict begin
begincmap
2 begincodespacerange
<00> <7F>
<C280> <DFBF>
endcodespacerange
1 beginnotdefrange
<00> <1F> 1
endnotdefrange
2 begincidrange
<20> <7E> 32
<C380> <C3BF> 192
endcidrange
endcmap
end end`)
	m := ParseEmbeddedCIDMap(context.Background(), data)
	if m == nil {
		t.Fatal("ParseEmbeddedCIDMap returned nil")
	}
	tests := []struct {
		name string
		code int
		cid  int
		ok   bool
	}{
		{name: "one-byte code", code: 0x41, cid: 0x41, ok: true},
		{name: "two-byte code", code: 0xC3A9, cid: 233, ok: true},
		{name: "notdef range only", code: 0x05, ok: false},
		{name: "unmapped", code: 0xC280, ok: false},
	}
	for _, tt := range tests {
		cid, ok := m.MappedCID(tt.code)
		if ok != tt.ok || (ok && cid != tt.cid) {
			t.Errorf("%s: MappedCID(%#x) = (%d, %v), want (%d, %v)", tt.name, tt.code, cid, ok, tt.cid, tt.ok)
		}
	}
}
