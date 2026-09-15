package cmap

import (
	"context"
	"testing"
)

// TestParseEmbeddedCIDMapUnmappedCodes pins how an embedded encoding CMap reads
// a code no cidchar or cidrange covers. ISO 32000-1, 9.7.6.3 sends it to CID 0,
// and a bfchar section maps to Unicode rather than to CIDs (9.10.3), so a CMap
// written with bfchar alone still decodes, every code to CID 0. A usecmap or a
// notdef section could still supply the code, and litho resolves neither, so
// the code stays unknown there.
func TestParseEmbeddedCIDMapUnmappedCodes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const head = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap /CMapName /T def\n" +
		"1 begincodespacerange <0000> <FFFF> endcodespacerange\n"
	const tail = "endcmap CMapName currentdict /CMap defineresource pop end end"

	tests := []struct {
		name     string
		body     string
		wantNil  bool
		notdef   bool
		mappedA  bool
		wantCIDA int
	}{
		{"bfchar alone", "1 beginbfchar <0041> <0041> endbfchar\n", false, true, false, 0},
		{"a cidchar mapping", "1 begincidchar <0041> 7 endcidchar\n", false, true, true, 7},
		{"bfchar over a base CMap", "/Identity-H usecmap\n1 beginbfchar <0041> <0041> endbfchar\n", true, false, false, 0},
		{"a cidchar mapping over a base CMap", "/Identity-H usecmap\n1 begincidchar <0042> 7 endcidchar\n", false, false, false, 0},
		{"a notdef range", "1 beginnotdefrange <0000> <00FF> 1 endnotdefrange\n", true, false, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := ParseEmbeddedCIDMap(ctx, []byte(head+tt.body+tail))
			if (m == nil) != tt.wantNil {
				t.Fatalf("map nil = %v, want %v", m == nil, tt.wantNil)
			}
			if m == nil {
				return
			}
			if m.UnmappedNotdef() != tt.notdef {
				t.Errorf("UnmappedNotdef = %v, want %v", m.UnmappedNotdef(), tt.notdef)
			}
			got := m.Decode([]byte{0x00, 0x41})
			if len(got) != 1 || got[0].Mapped != tt.mappedA || (tt.mappedA && got[0].CID != tt.wantCIDA) {
				t.Errorf("Decode(<0041>) = %+v, want mapped=%v cid=%d", got, tt.mappedA, tt.wantCIDA)
			}
		})
	}
}
