package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestPUAActualText_GlyphNameReachesPUA pins that a simple font reaches the
// Private Use Area through a glyph name as well as through a /ToUnicode CMap.
//
// veraPDF gives a simple font's glyph its /ToUnicode value when the CMap maps
// the code, and otherwise the Unicode value of its encoding's glyph name
// (PDSimpleFont.toUnicode), which ISO 32000-1, 9.10.2 derives through the Adobe
// Glyph List. That list assigns 192 names to the PUA, dotlessj among them
// (U+F6BE). govdocs thread_300_300849.pdf shows code 26 of a TeX-cmex9 font
// whose encoding names it dotlessj and which has no /ToUnicode; veraPDF reports
// ISO 14289-2, 8.4.3, and litho, which read PUA values from /ToUnicode alone,
// did not. Measured against veraPDF on fixtures of each shape.
func TestPUAActualText_GlyphNameReachesPUA(t *testing.T) {
	t.Parallel()

	const tounicode = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap\n" +
		"1 begincodespacerange <00> <ff> endcodespacerange\n" +
		"1 beginbfchar <1a> <006A> endbfchar\n" +
		"endcmap CMapName currentdict /CMap defineresource pop end end"

	// doc shows code 26 through a Type 1 font whose /Differences names it
	// glyph; withToUnicode adds a /ToUnicode CMap mapping it to U+006A.
	doc := func(glyph string, withToUnicode bool) []byte {
		body := "BT /F1 12 Tf 10 50 Td <1A> Tj ET"
		tu := ""
		if withToUnicode {
			tu = " /ToUnicode 6 0 R"
		}
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 7 0 R /MarkInfo << /Marked true >> >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
				"/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
			"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica "+
				"/Encoding << /Type /Encoding /BaseEncoding /WinAnsiEncoding /Differences [26 /"+glyph+"] >> "+
				"/FirstChar 26 /LastChar 26 /Widths [500]"+tu+" >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(tounicode), tounicode),
			"<< /Type /StructTreeRoot >>",
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

	tests := []struct {
		name          string
		glyph         string
		withToUnicode bool
		want          bool
	}{
		{"a glyph name the AGL puts in the PUA", "dotlessj", false, true},
		{"a glyph name outside the PUA", "a", false, false},
		{"a /ToUnicode entry outside the PUA wins over the name", "dotlessj", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := fires(t, doc(tt.glyph, tt.withToUnicode)); got != tt.want {
				t.Errorf("8.4.3 fired = %v, want %v", got, tt.want)
			}
		})
	}
}
