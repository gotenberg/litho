package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
)

// TestNextEncodingCode_SplitsByCodespaceAndReadsTheCID pins the code->CID
// reading the CID-keyed fallback tables share with the metrics: the encoding
// CMap's codespaces split the code (ISO 32000-1, 9.7.6.2), its cidrange gives
// the CID, and a code nothing maps keeps its value, as [Font.metricCID] reads
// it. An unmapped two-byte code of the mixed-width CMap re-aligns one byte on,
// as [cmap.CIDMap.Decode] splits it.
func TestNextEncodingCode_SplitsByCodespaceAndReadsTheCID(t *testing.T) {
	t.Parallel()

	doc := encodingCIDDocument(t, "")
	font := encodingCIDFont(t, doc, ast.PdfIndirectRef{ObjectNum: 6})
	tests := []struct {
		data          string
		n, value, cid int
	}{
		{data: "A\xD0\x9F", n: 1, value: 0x41, cid: 0x41},
		{data: "\xD0\x9FA", n: 2, value: 0xD09F, cid: 1055},
		{data: "\xE0\xA0\x85", n: 3, value: 0xE0A085, cid: 2053},
		{data: "\xC2\x80", n: 1, value: 0xC2, cid: 0xC2},
	}
	for _, tt := range tests {
		code, ok := font.nextEncodingCode([]byte(tt.data))
		if !ok || code.n != tt.n || code.value != tt.value || code.cid != tt.cid || code.undefined {
			t.Errorf("nextEncodingCode(%q) = (%+v, %v), want n %d, value %#x, CID %d",
				tt.data, code, ok, tt.n, tt.value, tt.cid)
		}
	}

	// Identity-H gives no code->CID reading: the two-byte code is the CID.
	identity := encodingCIDFont(t, doc, ast.PdfName("Identity-H"))
	if _, ok := identity.nextEncodingCode([]byte("\x00\x41")); ok {
		t.Error("nextEncodingCode under Identity-H reported a reading, want none")
	}
}

// TestDecodeNextChar_ReadsTheTableAtTheCID pins that a Type 0 font with no
// /ToUnicode CMap reads its CID-keyed table at the CID its encoding CMap gives
// the code (ISO 32000-1, 9.10.2), and reads a CID the table does not name as
// the code's value.
func TestDecodeNextChar_ReadsTheTableAtTheCID(t *testing.T) {
	t.Parallel()

	doc := encodingCIDDocument(t, "")
	font := encodingCIDFont(t, doc, ast.PdfIndirectRef{ObjectNum: 6})
	font.ToUnicode = nil
	font.GIDToUnicode = map[uint16]rune{1055: 'Ж', 0xD09F: 'x'}
	if char, n, skip := decodeNextChar(font, []byte("\xD0\x9F")); char != "Ж" || n != 2 || skip {
		t.Errorf("decodeNextChar(<D09F>) = (%q, %d, %v), want (Ж, 2, false) from CID 1055", char, n, skip)
	}
	if char, n, skip := decodeNextChar(font, []byte("B")); char != "B" || n != 1 || skip {
		t.Errorf("decodeNextChar(<42>) = (%q, %d, %v), want (B, 1, false)", char, n, skip)
	}
	// <FF> lies in no codespace range: it selects .notdef and names nothing.
	if char, n, skip := decodeNextChar(font, []byte("\xFF")); char != "" || n != 1 || skip {
		t.Errorf("decodeNextChar(<FF>) = (%q, %d, %v), want (\"\", 1, false)", char, n, skip)
	}
}

// TestDecodeNextCode_ReadsAnUnmappedCodeAsNotdef pins that a code no mapping
// covers, in an encoding CMap litho holds whole, selects CID 0 (ISO 32000-1,
// 9.7.6.3) and names no character, where it once read as its own value. It is
// still reported as a stand-in, so the integrity probe weighs it as it
// weighed the value.
func TestDecodeNextCode_ReadsAnUnmappedCodeAsNotdef(t *testing.T) {
	t.Parallel()

	program := "/CIDInit /ProcSet findresource begin 12 dict begin begincmap\n" +
		"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n" +
		"1 begincidrange\n<0041> <0042> 1\nendcidrange\nendcmap end end\n"
	cids := cmap.ParseEmbeddedCIDMap(context.Background(), []byte(program))
	if cids == nil || !cids.UnmappedNotdef() {
		t.Fatal("the encoding CMap did not parse whole")
	}
	font := &Font{
		Subtype:      "Type0",
		GIDToUnicode: map[uint16]rune{1: 'A', 0x5A20: 'x'},
		encodingCIDs: &encodingCIDs{cids: cids},
	}
	if char, n, skip, standIn := decodeNextCode(font, []byte("\x5A\x20")); char != "" || n != 2 || skip || !standIn {
		t.Errorf("decodeNextCode(<5A20>) = (%q, %d, %v, %v), want (\"\", 2, false, true)", char, n, skip, standIn)
	}
	if char, _, _, standIn := decodeNextCode(font, []byte("\x00\x41")); char != "A" || standIn {
		t.Errorf("decodeNextCode(<0041>) = (%q, standIn %v), want (A, false)", char, standIn)
	}
}
