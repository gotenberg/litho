package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestPUAActualText_EmbeddedEncodingCMap pins that a composite font whose
// /Encoding is an embedded CMap stream is judged on the codes it shows.
//
// ISO 32000-1, 9.7.6.2 splits a Type 0 font's string into character codes by
// its encoding CMap's codespace ranges, and the /ToUnicode CMap is keyed by
// those codes, so a PUA entry for a code nothing shows is out of scope for
// ISO 14289-2, 8.4.3. litho judged such a font by the whole /ToUnicode and
// reported pdfjs issue15292.pdf, whose AgfaRotis fonts map codes E000 and up
// into the PUA but show none of them; veraPDF passes it. Measured against
// veraPDF: showing a code the CMap maps to "A" passes, showing the code it maps
// into the PUA fails.
func TestPUAActualText_EmbeddedEncodingCMap(t *testing.T) {
	t.Parallel()

	const tounicode = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap\n" +
		"1 begincodespacerange <0000> <ffff> endcodespacerange\n" +
		"2 beginbfchar <0001> <E12D> <0041> <0041> endbfchar\n" +
		"endcmap CMapName currentdict /CMap defineresource pop end end"
	const encoding = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap\n" +
		"/CMapName /TestEnc def /CMapType 1 def\n" +
		"1 begincodespacerange <0000> <ffff> endcodespacerange\n" +
		"1 begincidrange <0000> <00ff> 0 endcidrange\n" +
		"endcmap CMapName currentdict /CMap defineresource pop end end"

	doc := func(shownHex string) []byte {
		body := fmt.Sprintf("BT /F1 12 Tf 10 50 Td <%s> Tj ET", shownHex)
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 7 0 R /MarkInfo << /Marked true >> >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
				"/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
			"<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding 10 0 R "+
				"/DescendantFonts [8 0 R] /ToUnicode 6 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(tounicode), tounicode),
			"<< /Type /StructTreeRoot >>",
			"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Test /DW 1000 "+
				"/CIDSystemInfo << /Registry (Test) /Ordering (Test) /Supplement 0 >> /FontDescriptor 9 0 R >>",
			"<< /Type /FontDescriptor /FontName /Test /Flags 4 /ItalicAngle 0 /Ascent 700 "+
				"/Descent -200 /CapHeight 700 /StemV 80 /FontBBox [0 0 1000 1000] >>",
			fmt.Sprintf("<< /Type /CMap /CMapName /TestEnc "+
				"/CIDSystemInfo << /Registry (Test) /Ordering (Test) /Supplement 0 >> /Length %d >>\nstream\n%s\nendstream",
				len(encoding), encoding),
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

	t.Run("a shown code outside the PUA passes", func(t *testing.T) {
		t.Parallel()
		if fires(t, doc("0041")) {
			t.Error("code 0x41 maps to A; the PUA entry for code 1 is never shown")
		}
	})
	t.Run("a shown code in the PUA fires", func(t *testing.T) {
		t.Parallel()
		if !fires(t, doc("0001")) {
			t.Error("code 1 is shown and maps to U+E12D with no ActualText")
		}
	})
}
