package remediation

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// cmapFormat4Single builds a Format 4 cmap subtable mapping one character code
// to one glyph, plus the mandatory 0xFFFF sentinel segment. The segment arrays
// run endCode, a reserved pad, startCode, idDelta, idRangeOffset, in that order.
func cmapFormat4Single(code, gid uint16) []byte {
	const segCount = 2
	sub := make([]byte, 14+segCount*8+2)
	binary.BigEndian.PutUint16(sub[0:], 4)                // format
	binary.BigEndian.PutUint16(sub[2:], uint16(len(sub))) //nolint:gosec // 30 bytes, fixed by segCount
	binary.BigEndian.PutUint16(sub[6:], segCount*2)       // segCountX2
	binary.BigEndian.PutUint16(sub[14:], code)            // endCode[0]
	binary.BigEndian.PutUint16(sub[16:], 0xFFFF)          // endCode[1]
	binary.BigEndian.PutUint16(sub[20:], code)            // startCode[0], past the reserved pad
	binary.BigEndian.PutUint16(sub[22:], 0xFFFF)          // startCode[1]
	binary.BigEndian.PutUint16(sub[24:], gid-code)        // idDelta[0]
	binary.BigEndian.PutUint16(sub[26:], 1)               // idDelta[1]
	return sub
}

// cmapTableWindowsBMP wraps a subtable in a cmap header declaring platform 3,
// encoding 1. [truetype.CharCodeToGID] ranks (3,10) and (3,1) highest and
// resolves a non-symbolic font through them; (3,0) is the separate symbol path
// a symbolic font takes instead.
func cmapTableWindowsBMP(subtable []byte) []byte {
	header := make([]byte, 12)
	binary.BigEndian.PutUint16(header[0:2], 0)                    // version
	binary.BigEndian.PutUint16(header[2:4], 1)                    // numSubtables
	binary.BigEndian.PutUint16(header[4:6], 3)                    // platformID: Windows
	binary.BigEndian.PutUint16(header[6:8], 1)                    // encodingID: BMP
	binary.BigEndian.PutUint32(header[8:12], uint32(len(header))) //nolint:gosec // 12, a constant
	return append(header, subtable...)
}

// probeTrueTypeWithCmap is [probeTrueType] plus a (3,1) Format 4 cmap.
//
// probeTrueType deliberately ships only head, hhea, hmtx and maxp, which is
// enough for a CIDFont whose /W is CID-indexed. A SIMPLE font resolves its
// codes through the program's cmap, so without one
// [compliance.ExpectedSimpleFontWidths] returns ok=false and the width repair
// is unreachable, which is the branch this fixture exists to cover.
//
// Records stay sorted by tag, as the sfnt format expects.
func probeTrueTypeWithCmap(advance, code, gid uint16) []byte {
	head := make([]byte, 54)
	binary.BigEndian.PutUint16(head[18:20], 1000) // unitsPerEm

	hhea := make([]byte, 36)
	binary.BigEndian.PutUint16(hhea[34:36], 2) // numberOfHMetrics

	hmtx := make([]byte, 8)
	binary.BigEndian.PutUint16(hmtx[4:6], advance) // GID 1 advance

	maxp := make([]byte, 6)
	binary.BigEndian.PutUint16(maxp[4:6], 2) // numGlyphs

	tables := []struct {
		tag  string
		data []byte
	}{
		{"cmap", cmapTableWindowsBMP(cmapFormat4Single(code, gid))},
		{"head", head},
		{"hhea", hhea},
		{"hmtx", hmtx},
		{"maxp", maxp},
	}

	out := make([]byte, 12+len(tables)*16)
	binary.BigEndian.PutUint32(out[0:4], 0x00010000)
	binary.BigEndian.PutUint16(out[4:6], uint16(len(tables))) //nolint:gosec // five tables
	for i, t := range tables {
		rec := 12 + i*16
		copy(out[rec:rec+4], t.tag)
		binary.BigEndian.PutUint32(out[rec+8:rec+12], uint32(len(out)))     //nolint:gosec // fixture is a few hundred bytes
		binary.BigEndian.PutUint32(out[rec+12:rec+16], uint32(len(t.data))) //nolint:gosec // fixture table is small
		out = append(out, t.data...)
	}
	return out
}

// simpleWidthsPDF builds a page whose /F1 is a SIMPLE TrueType font declaring a
// width that disagrees with its embedded program. Code 65 maps to GID 1, the
// glyph probeTrueTypeWithCmap gives the advance.
//
// /Flags 32 is non-symbolic (bit 6). With the symbolic bit set,
// compliance.isSymbolicFont would divert to the (3,0) symbol cmap this
// fixture does not carry, and the metrics would not resolve.
func simpleWidthsPDF(declared int, advance uint16) []byte {
	program := probeTrueTypeWithCmap(advance, 'A', 1)
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /Font << /F1 4 0 R >> >> >>",
		fmt.Sprintf("<< /Type /Font /Subtype /TrueType /BaseFont /Probe "+
			"/FirstChar 65 /LastChar 65 /Widths [%d] /FontDescriptor 5 0 R >>", declared),
		"<< /Type /FontDescriptor /FontName /Probe /Flags 32 /FontFile2 6 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(program), program),
	)
}

// simpleWidthAfterFix reads back the width the fixer persisted onto the font
// dictionary, and reports whether that object was written at all.
func simpleWidthAfterFix(t *testing.T, alloc *writer.ObjAllocator) (float64, bool) {
	t.Helper()

	pending, ok := alloc.Pending()[4]
	if !ok {
		return 0, false
	}
	fd, ok := pending.(ast.PdfDict)
	if !ok {
		return 0, false
	}
	w, ok := fd["Widths"].(ast.PdfArray)
	if !ok || len(w) != 1 {
		return 0, false
	}
	return document.Float(w[0]), true
}

// TestFixFontIssues_RepairsSimpleFontWidthsUnderPDFUA covers the SIMPLE-font
// /Widths branch, which the Type0 /W test does not reach.
//
// The two branches sit behind one gate but are different code: fixSimpleFont
// in fixer_fonts.go rewrites /Widths for a simple font, and fixType0Font
// rewrites /W and /DW for a Type0 font. Most real carriers of the PDF/UA width
// rules are simple fonts, so real files and this probe cover the SIMPLE branch
// while TestFixFontIssues_RepairsWidthsReportedUnderPDFUA covers the Type0 one.
// Neither alone covers both.
//
// # Corpus evidence
//
//   - All corpora: of the carriers that exercise the PDF/UA width rules, 15 of
//     20 are simple fonts and only 5 are Type0.
//   - All corpora: the fixture used to verify the repair end to end was
//     /Subtype /Type1.
func TestFixFontIssues_RepairsSimpleFontWidthsUnderPDFUA(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		clause  string
		profile string
		repair  bool
	}{
		{"ISO 19005-2 6.2.11.5, the PDF/A control", "6.2.11.5", "PDF/A-2b", true},
		{"ISO 14289-1 7.21.5", "7.21.5", "PDF/UA-1", true},
		{"ISO 14289-2 8.4.5.6", "8.4.5.6", "PDF/UA-2", true},
		// The gate exists so widths are rewritten only where a mismatch is
		// reported. Without this arm the test would pass just as well if the
		// gate were deleted outright.
		{"an unrelated font clause leaves /Widths alone", "8.4.5.7", "PDF/UA-2", false},
	} {
		doc, alloc := newTestDoc(t, simpleWidthsPDF(999, 500))

		violation := compliance.Violation{
			Rule:    compliance.Rule{Clause: tc.clause, TestNumber: 1},
			Context: "font F1 on page 1: char 65 width mismatch (PDF=999.0, font=500.0)",
		}
		if _, err := fixFontIssues(context.Background(), alloc, doc, Options{},
			[]compliance.Violation{violation}, tc.profile); err != nil {
			t.Fatalf("%s: fixFontIssues: %v", tc.name, err)
		}

		got, written := simpleWidthAfterFix(t, alloc)
		if !tc.repair {
			if written {
				t.Errorf("%s: /Widths was rewritten to %v for a clause that reports no width mismatch",
					tc.name, got)
			}
			continue
		}
		if !written {
			t.Errorf("%s: clause %s reported a width mismatch and nothing was rewritten",
				tc.name, tc.clause)
			continue
		}
		// The font declares 999 where the program advances 500 at 1000 upm.
		if got != 500 {
			t.Errorf("%s: /Widths[0] is %v, want 500 (the embedded program's advance)",
				tc.name, got)
		}
	}
}
