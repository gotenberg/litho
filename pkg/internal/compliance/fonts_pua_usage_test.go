package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestPUAActualText_ScopedToShownCodes pins that a composite font's PUA
// mapping is judged on the codes a page shows, not on the whole CMap.
//
// ISO 14289-2, 8.4.3 asks whether real content maps to a Private Use Area
// value, and veraPDF raises it per Glyph object, so an entry for a code nothing
// renders is out of scope. litho read any PUA entry anywhere in a Type 0 font's
// /ToUnicode as a violation whenever the document carried no /ActualText.
//
// Identity-H and Identity-V make the character code the CID, so the codes the
// CMap is keyed by are exactly the ones litho decodes. Any other encoding needs
// a CMap this check does not carry and keeps the conservative answer.
//
// # Corpus evidence
//
//   - pdfjs: the whole-CMap reading reported 19 files veraPDF passes.
//   - pdfjs bug1473809.pdf, mixedfonts.pdf: each maps 37 codes into the PUA
//     and shows none of them.
func TestPUAActualText_ScopedToShownCodes(t *testing.T) {
	t.Parallel()

	// pdf shows shownHex through an Identity-H font whose /ToUnicode maps code
	// 1 into the PUA. The document carries no /ActualText and no /Alt, so the
	// rule turns on whether a shown code reaches the PUA.
	pdf := func(shownHex string) []byte {
		const cmap = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap\n" +
			"1 begincodespacerange <0000> <ffff> endcodespacerange\n" +
			"1 beginbfchar <0001> <e000> endbfchar\n" +
			"endcmap CMapName currentdict /CMap defineresource pop end end"
		stream := fmt.Sprintf("BT /F1 12 Tf 10 50 Td <%s> Tj ET", shownHex)
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 7 0 R /MarkInfo << /Marked true >> >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
				"/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
			"<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding /Identity-H "+
				"/DescendantFonts [8 0 R] /ToUnicode 6 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmap), cmap),
			"<< /Type /StructTreeRoot >>",
			"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Test /DW 1000 "+
				"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> "+
				"/FontDescriptor 9 0 R >>",
			"<< /Type /FontDescriptor /FontName /Test /Flags 4 /ItalicAngle 0 /Ascent 700 "+
				"/Descent -200 /CapHeight 700 /StemV 80 /FontBBox [0 0 1000 1000] >>",
		)
	}

	fires := func(t *testing.T, data []byte) bool {
		t.Helper()
		doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		viols, err := ValidatePDFUA(context.Background(), doc, models.PDFUA2)
		if err != nil {
			t.Fatalf("ValidatePDFUA: %v", err)
		}
		for _, e := range viols {
			if v, ok := e.(Violation); ok && v.Rule.Clause == "8.4.3" {
				return true
			}
		}
		return false
	}

	t.Run("a shown PUA code fires", func(t *testing.T) {
		t.Parallel()
		if !fires(t, pdf("0001")) {
			t.Error("code 1 is shown and maps to U+E000 with no ActualText")
		}
	})

	t.Run("an unshown PUA code does not", func(t *testing.T) {
		t.Parallel()
		if fires(t, pdf("0002")) {
			t.Error("nothing shows code 1, so no real content reaches the PUA")
		}
	})
}
