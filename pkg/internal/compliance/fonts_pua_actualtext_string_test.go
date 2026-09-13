package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestPUAActualText_MarkedContentNeedsString pins that marked content covers
// its PUA glyphs only through an /ActualText text string.
//
// ISO 32000-1, 14.9.4 makes /ActualText a text string, and veraPDF reads the
// enclosing marked-content sequences' /ActualText through
// getInheritedStringAttribute, which returns nothing for any other type, so
// ISO 14289-2, 8.4.3 still fails. litho accepted the key whatever its value.
// Measured against veraPDF: a /Span BDC carrying /ActualText /NotAString fails,
// the same span carrying a string passes.
func TestPUAActualText_MarkedContentNeedsString(t *testing.T) {
	t.Parallel()

	const cmap = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap\n" +
		"1 begincodespacerange <0000> <ffff> endcodespacerange\n" +
		"1 beginbfchar <0001> <E12D> endbfchar\n" +
		"endcmap CMapName currentdict /CMap defineresource pop end end"

	doc := func(actualText string) []byte {
		body := "/Span << /ActualText " + actualText + " >> BDC BT /F1 12 Tf 10 50 Td <0001> Tj ET EMC"
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 7 0 R /MarkInfo << /Marked true >> >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
				"/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
			"<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding /Identity-H "+
				"/DescendantFonts [8 0 R] /ToUnicode 6 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmap), cmap),
			"<< /Type /StructTreeRoot >>",
			"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Test /DW 1000 "+
				"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor 9 0 R >>",
			"<< /Type /FontDescriptor /FontName /Test /Flags 4 /ItalicAngle 0 /Ascent 700 "+
				"/Descent -200 /CapHeight 700 /StemV 80 /FontBBox [0 0 1000 1000] >>",
		)
	}

	fires := func(t *testing.T, data []byte) bool {
		t.Helper()
		d, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		viols, err := ValidatePDFUA(context.Background(), d, models.PDFUA2)
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

	t.Run("a name is not an ActualText", func(t *testing.T) {
		t.Parallel()
		if !fires(t, doc("/NotAString")) {
			t.Error("/ActualText /NotAString is not a text string and covers nothing")
		}
	})
	t.Run("a string covers the glyph", func(t *testing.T) {
		t.Parallel()
		if fires(t, doc("(x)")) {
			t.Error("/ActualText (x) covers the PUA glyph")
		}
	})
}
