package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestTrueTypeAGLExemption pins which TrueType fonts earn the Adobe Glyph List
// exemption from /ToUnicode under PDF/UA.
//
// ISO 14289-1, 7.21.7 and ISO 14289-2, 8.4.5.8 ask only that each shown glyph
// carry a Unicode value, which ISO 32000-1, 9.10.2 derives from the glyph name
// whatever the font's subtype; veraPDF computes it per Glyph. litho reserved
// the exemption for Type 1, so an unembedded TrueType with resolvable names was
// reported: pdfjs bug1727053.pdf shows Arial,Bold with no /ToUnicode and
// veraPDF reports nothing.
//
// The exemption reaches a TrueType only through an /Encoding dictionary, which
// states its glyph names outright. ISO 32000-1, 9.6.6.4 resolves a TrueType
// code through the font's own cmap, so a name a predefined table merely implies
// yields Unicode only when the program carries that glyph, which this check
// does not establish. Widening it to named encodings suppressed the rule on 16
// govdocs files veraPDF still reports.
func TestTrueTypeAGLExemption(t *testing.T) {
	t.Parallel()

	// pdf shows code 65 through an unembedded TrueType carrying the given
	// /Encoding entry and no /ToUnicode.
	pdf := func(encoding string) []byte {
		const stream = "BT /F1 12 Tf 10 50 Td (A) Tj ET"
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 6 0 R /MarkInfo << /Marked true >> >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
				"/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
			"<< /Type /Font /Subtype /TrueType /BaseFont /Arial,Bold /FirstChar 65 /LastChar 65 "+
				"/Widths [722] /FontDescriptor 7 0 R "+encoding+" >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
			"<< /Type /StructTreeRoot >>",
			"<< /Type /FontDescriptor /FontName /Arial,Bold /Flags 32 /ItalicAngle 0 /Ascent 718 "+
				"/Descent -207 /CapHeight 718 /StemV 88 /MissingWidth 0 /FontBBox [0 0 1000 1000] >>",
		)
	}

	fires := func(t *testing.T, data []byte) bool {
		t.Helper()
		doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		viols, err := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
		if err != nil {
			t.Fatalf("ValidatePDFUA: %v", err)
		}
		for _, e := range viols {
			if v, ok := e.(Violation); ok && v.Rule.Clause == "7.21.7" && v.Rule.TestNumber == 1 {
				return true
			}
		}
		return false
	}

	t.Run("an /Encoding dictionary states the names and earns the exemption", func(t *testing.T) {
		t.Parallel()
		if fires(t, pdf("/Encoding << /Differences [65 /A] >>")) {
			t.Error("the file names code 65 /A, which the AGL maps to U+0041")
		}
	})

	// The exemption still turns on the names resolving: a /Differences entry
	// naming a glyph no published glyph list defines carries no Unicode value.
	t.Run("a dictionary naming an unresolvable glyph still fires", func(t *testing.T) {
		t.Parallel()
		if !fires(t, pdf("/Encoding << /Differences [65 /negationslash] >>")) {
			t.Error("negationslash is in neither the standard Latin nor the Symbol set")
		}
	})
}
