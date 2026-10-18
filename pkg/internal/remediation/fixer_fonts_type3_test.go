package remediation

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// type3UnicodeFontPDF builds a page whose font 4 is a Type 3 font with the given
// /Encoding dictionary entries and extra font dictionary entries.
func type3UnicodeFontPDF(encoding, extra string) []byte {
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /Font << /F1 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1 1] /FontMatrix [1 0 0 1 0 0]" +
			" /CharProcs << >> /FirstChar 65 /LastChar 70 /Widths [1 1 1 1 1 1]" +
			" /Encoding << " + encoding + " >> " + extra + " >>",
	})
}

// type3ToUnicode runs the Type 3 repair for the shown codes and returns the
// CMap it attached, or "" when it attached none.
func type3ToUnicode(t *testing.T, data []byte, shown map[byte]bool) (string, []string) {
	t.Helper()

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
	if err != nil {
		t.Fatalf("resolving the font: %v", err)
	}
	fixes := fixType3FontToUnicode(ctx, doc, alloc, fontDict, ast.PdfIndirectRef{ObjectNum: 4}, shown)
	font, _ := alloc.Pending()[4].(ast.PdfDict)
	if font == nil {
		return "", fixes
	}
	ref, ok := font["ToUnicode"].(ast.PdfIndirectRef)
	if !ok {
		return "", fixes
	}
	stream, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("/ToUnicode points at %T, want a stream", alloc.Pending()[ref.ObjectNum])
	}
	raw, err := stream.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the CMap: %v", err)
	}
	return string(raw), fixes
}

// TestFixType3FontToUnicodeFromGlyphNames pins that a Type 3 font gains a
// /ToUnicode built from its /Differences names: an AGL name, a Symbol font
// name, a ZapfDingbats name, and the uniXXXX and uXXXXX spellings.
func TestFixType3FontToUnicodeFromGlyphNames(t *testing.T) {
	t.Parallel()

	data := type3UnicodeFontPDF("/Differences [65 /A /alpha /a12 /uni00E9 /u1F600]", "")
	got, fixes := type3ToUnicode(t, data, codes(65, 66, 67, 68, 69))
	if len(fixes) != 1 {
		t.Fatalf("fixes = %v, want one", fixes)
	}
	for _, want := range []string{"<41> <0041>", "<42> <03B1>", "<43> <261E>", "<44> <00E9>", "<45> <D83DDE00>"} {
		if !strings.Contains(got, want) {
			t.Errorf("CMap misses %q:\n%s", want, got)
		}
	}
}

// TestFixType3FontToUnicodeLeavesWhatItCannotGround pins the refusals: a shown
// code whose name gives no value (a producer's own name, a lowercase or
// multi-character uni spelling, a .suffix, a ligature), a code only a
// /BaseEncoding names, and a font with a /ToUnicode of its own all leave the
// font as it was, so no partial map reports a repair veraPDF does not see.
func TestFixType3FontToUnicodeLeavesWhatItCannotGround(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, encoding, extra string
		shown                 map[byte]bool
	}{
		{"producer name", "/Differences [65 /A /g12]", "", codes(65, 66)},
		{"numeric name", "/Differences [65 /A /G4F]", "", codes(65, 66)},
		{"lowercase uni", "/Differences [65 /uni00e9]", "", codes(65)},
		{"two-character uni", "/Differences [65 /uni00410042]", "", codes(65)},
		{"suffixed name", "/Differences [65 /A.sc]", "", codes(65)},
		{"ligature", "/Differences [65 /f_f]", "", codes(65)},
		{"surrogate", "/Differences [65 /uD800]", "", codes(65)},
		{"code only the base names", "/BaseEncoding /WinAnsiEncoding /Differences [65 /A]", "", codes(65, 66)},
		{"producer map kept", "/Differences [65 /A]", "/ToUnicode 5 0 R", codes(65)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, fixes := type3ToUnicode(t, type3UnicodeFontPDF(tt.encoding, tt.extra), tt.shown)
			if got != "" || len(fixes) != 0 {
				t.Errorf("attached a /ToUnicode (fixes %v):\n%s", fixes, got)
			}
		})
	}
}

// TestFixFontIssues_ReachesType3FontInUncolouredCell pins that a font only an
// uncoloured tiling cell shows gets its /ToUnicode. veraPDF judges the glyphs
// of such a cell (ISO 32000-1, 8.7.3.3 turns its colour operators off, not its
// text), so the fixer's walk enters it as compliance's does.
//
// # Corpus evidence
//
//   - litho offlimits_1b.pdf: page 12's cell shows a Type 3 glyph named a, and
//     the remediated PDF/A-1a output kept ISO 19005-1, 6.3.8 t1 on veraPDF.
func TestFixFontIssues_ReachesType3FontInUncolouredCell(t *testing.T) {
	t.Parallel()

	const cell = "BT /T3 12 Tf 1 1 Td (a) Tj ET"
	data := assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 7 0 R" +
			" /Resources << /ColorSpace << /CSp [/Pattern /DeviceGray] >> /Pattern << /P2 4 0 R >> >> >>",
		"<< /Type /Pattern /PatternType 1 /PaintType 2 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10" +
			" /Resources << /Font << /T3 5 0 R >> >> /Length " + strconv.Itoa(len(cell)) + " >>\nstream\n" + cell + "\nendstream",
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1 1] /FontMatrix [0.001 0 0 0.001 0 0]" +
			" /CharProcs << /a 6 0 R >> /Encoding << /Differences [97 /a] >> /FirstChar 97 /LastChar 97 /Widths [1000] /Resources << >> >>",
		"<< /Length 8 >>\nstream\n1000 0 d0\nendstream",
		"<< /Length 36 >>\nstream\n/CSp cs 0.5 /P2 scn 0 0 100 100 re f\nendstream",
	})
	ctx := context.Background()
	doc, alloc := newTestDoc(t, data)
	fixes, err := fixFontIssues(ctx, alloc, doc, Options{},
		[]compliance.Violation{{Rule: compliance.Rule_19005_1_6_3_8_t01}}, "PDF/A-1a")
	if err != nil {
		t.Fatalf("fixFontIssues: %v", err)
	}
	font, _ := alloc.Pending()[5].(ast.PdfDict)
	if _, ok := font["ToUnicode"]; !ok {
		t.Errorf("the Type 3 font the uncoloured cell shows got no /ToUnicode; fixes=%v", fixes)
	}
}
