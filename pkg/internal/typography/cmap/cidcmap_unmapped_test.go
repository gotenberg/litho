package cmap

import (
	"context"
	"testing"
)

const (
	unmappedTestHead = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap /CMapName /T def\n" +
		"1 begincodespacerange <0000> <FFFF> endcodespacerange\n"
	unmappedTestTail = "endcmap CMapName currentdict /CMap defineresource pop end end"
)

// TestParseEmbeddedCIDMapUnmappedCodes pins how an embedded encoding CMap reads
// a code no cidchar or cidrange of its own covers. ISO 32000-1, 9.7.6.3 sends
// it to CID 0, and a bfchar section maps to Unicode rather than to CIDs
// (9.10.3), so a CMap written with bfchar alone still decodes, every code to
// CID 0. A base named by usecmap supplies the codes the CMap leaves unmapped,
// then a notdef section does, as veraPDF's CMap.toCID reads them. A base litho
// cannot resolve but veraPDF bundles leaves the code unknown; a name no reader
// bundles names no base at all. Every case was measured against veraPDF 1.30.2.
func TestParseEmbeddedCIDMapUnmappedCodes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

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
		{"bfchar over the Identity-H base", "/Identity-H usecmap\n1 beginbfchar <0041> <0041> endbfchar\n", false, true, true, 0x41},
		{"a cidchar mapping over a base CMap", "/Identity-H usecmap\n1 begincidchar <0042> 7 endcidchar\n", false, true, true, 0x41},
		{"an own mapping after usecmap wins over the base", "/Identity-H usecmap\n1 begincidchar <0041> 7 endcidchar\n", false, true, true, 7},
		{"an own mapping before usecmap wins over the base", "1 begincidchar <0041> 7 endcidchar\n/Identity-H usecmap\n", false, true, true, 7},
		{"a notdef range", "1 beginnotdefrange <0000> <00FF> 1 endnotdefrange\n", false, true, true, 1},
		{"a notdef char", "1 beginnotdefchar <0041> 3 endnotdefchar\n", false, true, true, 3},
		{"a cid mapping wins over a notdef range", "1 beginnotdefrange <0000> <00FF> 1 endnotdefrange\n1 begincidchar <0041> 7 endcidchar\n", false, true, true, 7},
		{"the first notdef range covering the code wins", "2 beginnotdefrange <0000> <00FF> 1 <0040> <0041> 2 endnotdefrange\n", false, true, true, 1},
		{"a base no reader bundles names no base", "/No-Such-H usecmap\n1 beginbfchar <0041> <0041> endbfchar\n", false, true, false, 0},
		{"a base only veraPDF bundles leaves the code unknown", "/Adobe-KR-9 usecmap\n1 beginbfchar <0041> <0041> endbfchar\n", true, false, false, 0},
		{"an own mapping over a base only veraPDF bundles", "/Adobe-KR-9 usecmap\n1 begincidchar <0042> 7 endcidchar\n", false, false, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := ParseEmbeddedCIDMap(ctx, []byte(unmappedTestHead+tt.body+unmappedTestTail))
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

// TestParseEmbeddedCIDMapOverDictBase pins the base a CMap stream dictionary's
// /UseCMap entry names (ISO 32000-1, 9.7.5.3, Table 120). veraPDF's
// PDCMap.getCMapFile adds it once the program has run, so the program's own
// mappings and its usecmap base win over it, and its notdef mappings come after
// the program's.
func TestParseEmbeddedCIDMapOverDictBase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	base := ParseEmbeddedCIDMap(ctx, []byte(unmappedTestHead+
		"1 begincidrange <0040> <004F> 100 endcidrange\n1 beginnotdefrange <0000> <00FF> 5 endnotdefrange\n"+unmappedTestTail))
	if base == nil {
		t.Fatal("the base CMap did not parse")
	}

	tests := []struct {
		name    string
		body    string
		base    Base
		code    byte
		notdef  bool
		mapped  bool
		wantCID int
	}{
		{"the base maps what the stream leaves", "1 begincidchar <0041> 7 endcidchar\n", Base{Map: base}, 0x42, true, true, 102},
		{"the stream's own mapping wins", "1 begincidchar <0041> 7 endcidchar\n", Base{Map: base}, 0x41, true, true, 7},
		{"the base's notdef range applies last", "1 beginnotdefrange <0060> <0060> 9 endnotdefrange\n", Base{Map: base}, 0x61, true, true, 5},
		{"the stream's notdef range precedes the base's", "1 beginnotdefrange <0061> <0061> 9 endnotdefrange\n", Base{Map: base}, 0x61, true, true, 9},
		{"a cid mapping of the base wins over the stream's notdef", "1 beginnotdefrange <0000> <00FF> 9 endnotdefrange\n", Base{Map: base}, 0x42, true, true, 102},
		{"a usecmap base precedes the dictionary's", "/Identity-H usecmap\n", Base{Map: base}, 0x42, true, true, 0x42},
		{"an unresolved base leaves the code unknown", "1 begincidchar <0041> 7 endcidchar\n", Base{Unresolved: true}, 0x42, false, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := ParseEmbeddedCIDMapOver(ctx, []byte(unmappedTestHead+tt.body+unmappedTestTail), tt.base)
			if m == nil {
				t.Fatal("map is nil")
			}
			if m.UnmappedNotdef() != tt.notdef {
				t.Errorf("UnmappedNotdef = %v, want %v", m.UnmappedNotdef(), tt.notdef)
			}
			got := m.Decode([]byte{0x00, tt.code})
			if len(got) != 1 || got[0].Mapped != tt.mapped || (tt.mapped && got[0].CID != tt.wantCID) {
				t.Errorf("Decode(<00%02X>) = %+v, want mapped=%v cid=%d", tt.code, got, tt.mapped, tt.wantCID)
			}
		})
	}
}

// TestParseEmbeddedCIDMapBaseCodespace pins that a CMap declaring no codespace
// of its own reads codes through its base's: veraPDF's CMap.useCMap appends
// the base's codespace ranges to the using CMap's.
func TestParseEmbeddedCIDMapBaseCodespace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const program = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap /CMapName /T def\n" +
		"/Identity-H usecmap\n" + unmappedTestTail
	m := ParseEmbeddedCIDMap(ctx, []byte(program))
	if m == nil {
		t.Fatal("a CMap with its base's codespace did not parse")
	}
	if got := m.Decode([]byte{0x01, 0x02}); len(got) != 1 || !got[0].Mapped || got[0].CID != 0x0102 {
		t.Errorf("Decode(<0102>) = %+v, want CID 0x0102", got)
	}
	if m := ParseEmbeddedCIDMap(ctx, []byte("/CIDInit /ProcSet findresource begin 12 dict begin begincmap\n"+
		"1 begincidchar <0041> 7 endcidchar\n"+unmappedTestTail)); m != nil {
		t.Error("a CMap with no codespace and no base decoded")
	}
}

// TestPredefinedCIDBase pins that a predefined base is read complete: a
// vertical CMap such as UniJIS-UCS2-V is a few overrides on UniJIS-UCS2-H,
// which it names through usecmap, so its base holds both.
func TestPredefinedCIDBase(t *testing.T) {
	t.Parallel()
	h := PredefinedCIDBase("UniJIS-UCS2-H")
	v := PredefinedCIDBase("UniJIS-UCS2-V")
	if h == nil || v == nil {
		t.Fatal("expected the UniJIS-UCS2 bases")
	}
	// U+0041 maps through UniJIS-UCS2-H alone; UniJIS-UCS2-V must reach it.
	hc, hok := h.lookup(0x41)
	vc, vok := v.lookup(0x41)
	if !hok || !vok || hc != vc {
		t.Errorf("U+0041: H gives %d (%v), V gives %d (%v)", hc, hok, vc, vok)
	}
	if !v.UnmappedNotdef() {
		t.Error("a fully resolved predefined base is not closed")
	}
	for _, name := range []string{"No-Such-CMap", "", "../bcmaps/H"} {
		if PredefinedCIDBase(name) != nil {
			t.Errorf("PredefinedCIDBase(%q) resolved", name)
		}
	}
	if id := PredefinedCIDBase("Identity-V"); id == nil {
		t.Error("Identity-V did not resolve")
	} else if c, ok := id.lookup(0xABCD); !ok || c != 0xABCD {
		t.Errorf("Identity-V maps 0xABCD to %d (%v)", c, ok)
	}
}
