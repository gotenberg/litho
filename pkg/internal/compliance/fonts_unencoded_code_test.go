package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestUnencodedRenderedCode pins that a rendered character code the font's
// predefined encoding leaves unnamed is reported as having no Unicode value.
//
// A code the encoding does not name selects no glyph, so it stands for no
// character, which ISO 19005-4, 6.2.10.7 forbids for a shown glyph. litho only
// caught the explicit /Differences .notdef spelling, and a font whose /Encoding
// is the bare name of a predefined encoding carries no /Differences at all.
//
// # Corpus evidence
//
//   - pdfjs issue18059.pdf: sets /Helvetica with /WinAnsiEncoding and shows
//     <0044004500460047...>. A simple font takes one byte per code, so every
//     other code is 0, and veraPDF reports it.
//   - verapdf 6-3-8-t01-pass-b.pdf, 6-2-11-7-2-t01-pass-b.pdf: both built to
//     conform, whose only shown code is 32. Reading the gap in litho's
//     MacExpertEncoding table as "no glyph" reported them.
func TestUnencodedRenderedCode(t *testing.T) {
	t.Parallel()

	// pdf shows the given hex string through a non-embedded simple font using
	// the named encoding.
	pdf := func(encoding, hexShown, extra string) []byte {
		stream := fmt.Sprintf("BT /F1 12 Tf 10 50 Td <%s> Tj ET", hexShown)
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
				"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
			"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /"+encoding+" "+extra+">>",
		)
	}

	fires := func(t *testing.T, data []byte) bool {
		t.Helper()
		doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		viols, err := ValidatePDFA(context.Background(), doc, models.PDFA4)
		if err != nil {
			t.Fatalf("ValidatePDFA: %v", err)
		}
		for _, e := range viols {
			if v, ok := e.(Violation); ok && v.Rule.Clause == "6.2.10.7" {
				return true
			}
		}
		return false
	}

	t.Run("a rendered control code names no glyph", func(t *testing.T) {
		t.Parallel()
		if !fires(t, pdf("WinAnsiEncoding", "0041", "")) {
			t.Error("WinAnsiEncoding names nothing at code 0, so the glyph has no Unicode value")
		}
	})

	t.Run("codes the encoding names do not fire", func(t *testing.T) {
		t.Parallel()
		if fires(t, pdf("WinAnsiEncoding", "414243", "")) {
			t.Error("codes 65 to 67 are A, B and C in WinAnsiEncoding")
		}
	})

	// litho's own MacExpertEncoding table omits the space at code 32 that
	// ISO 32000-2, Annex D defines. Reading that gap as "no glyph" reports
	// files built to conform. Judging just the control range keeps the
	// conclusion independent of which table litho carries.
	t.Run("a gap in litho's own table above the control range does not fire", func(t *testing.T) {
		t.Parallel()
		if fires(t, pdf("MacExpertEncoding", "20", "")) {
			t.Error("code 32 is the space in MacExpertEncoding; a gap in litho's table is not the file's fault")
		}
	})

	t.Run("a ToUnicode covering the code supersedes the encoding", func(t *testing.T) {
		t.Parallel()
		const cmap = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap\n" +
			"1 begincodespacerange <00> <ff> endcodespacerange\n" +
			"1 beginbfchar <00> <0041> endbfchar\n" +
			"endcmap CMapName currentdict /CMap defineresource pop end end"
		data := cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
				"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
			"<< /Length 36 >>\nstream\nBT /F1 12 Tf 10 50 Td <0041> Tj ET\nendstream",
			"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding "+
				"/ToUnicode 6 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmap), cmap),
		)
		if fires(t, data) {
			t.Error("the CMap maps code 0 to U+0041, which ISO 32000-1 9.10.2 makes the primary mapping")
		}
	})
}
