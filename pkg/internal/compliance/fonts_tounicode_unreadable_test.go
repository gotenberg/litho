package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestToUnicodeCMapValues_UnreadableIsVacuous pins that a /ToUnicode which
// cannot be read as a CMap does not fire the value rule (ISO 14289-1 §7.21.7
// t2, ISO 19005-2 §6.2.11.7.2 t2). The rule constrains "the Unicode values
// specified in the ToUnicode CMap"; an unreadable entry specifies none, so the
// constraint is vacuous. Japanese producers write the CMap name /Identity-H
// where ISO 32000-1 §9.10.3 prescribes a stream, which litho reported as a
// value violation while veraPDF passed the file.
//
// # Corpus evidence
//
//   - pdfjs javauninstall-7r.pdf: font HGPGothicE writes /ToUnicode
//     /Identity-H, and veraPDF passes the file.
func TestToUnicodeCMapValues_UnreadableIsVacuous(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Object 2 is the Type0 font, 3 its descendant CIDFont.
	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog >>",
		"<< /Type /Font /Subtype /Type0 /BaseFont /HGPGothicE /Encoding /Identity-H /ToUnicode /Identity-H /DescendantFonts [ 3 0 R ] >>",
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /HGPGothicE /DW 1000 >>",
	)
	fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
	if err != nil {
		t.Fatalf("resolve font: %v", err)
	}

	// Two-byte Identity-H codes for U+306E and U+30A4, as the page shows them.
	usage := &fontUsageInfo{
		visible:    true,
		charCodes:  map[byte]bool{0x30: true, 0x6e: true, 0xa4: true},
		rawStrings: []ast.PdfString{"\x30\x6e\x30\xa4"},
	}

	if got := checkToUnicodeCMapValues(ctx, doc, fontDict, "F1", 1, "PDF/UA-1", usage); len(got) != 0 {
		t.Errorf("/ToUnicode naming a CMap must not fire the value rule, got %v", got)
	}
}

// TestToUnicodeCMapValues_ReadableStreamStillFires guards the fix above against
// over-relaxation: a /ToUnicode stream that does map a rendered code to U+0000
// still fires the value rule.
func TestToUnicodeCMapValues_ReadableStreamStillFires(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cmapData := "/CIDInit /ProcSet findresource begin\n" +
		"12 dict begin\nbegincmap\n/CMapName /Test def\n/CMapType 2 def\n" +
		"1 begincodespacerange\n<00> <FF>\nendcodespacerange\n" +
		"1 beginbfchar\n<41> <0000>\nendbfchar\n" +
		"endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n"

	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /ToUnicode 3 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmapData), cmapData),
	)
	fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
	if err != nil {
		t.Fatalf("resolve font: %v", err)
	}

	usage := &fontUsageInfo{
		visible:    true,
		charCodes:  map[byte]bool{0x41: true},
		rawStrings: []ast.PdfString{"A"},
	}

	if got := checkToUnicodeCMapValues(ctx, doc, fontDict, "F1", 1, "PDF/UA-1", usage); len(got) == 0 {
		t.Error("a readable /ToUnicode mapping a rendered code to U+0000 must fire the value rule")
	}
}
