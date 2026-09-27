package pdf

import (
	"context"
	"fmt"
	"testing"
)

// TestExtractText_LaysAMissingFontOutInHelvetica pins that text drawn in a
// font the page's resources do not define is laid out in Helvetica's widths,
// as pdf.js lays it out, so what fits the page in every reader is not
// dropped as clipped.
//
// "Adobe tweet" at 105 points is 583.69 points wide in Helvetica and fits a
// 612-point page from x = 0. At one em a glyph it reached 1155 points, and
// litho dropped "tweet" as drawn past the page's edge.
//
// # Corpus evidence
//
//   - pdfjs issue9105_other.pdf: draws this line in a font it does not define,
//     on a page with no media box, and read as "Adobe" once a rewrite gave the
//     page the US Letter box every reader assumes for it.
func TestExtractText_LaysAMissingFontOutInHelvetica(t *testing.T) {
	t.Parallel()

	const body = "BT /F1 105 Tf 0 400 Td (Adobe tweet) Tj ET"
	r := openDirect(t, buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
	}))
	text, err := r.ExtractText(context.Background(), 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if text != "Adobe tweet" {
		t.Errorf("page 1 reads %q, want %q", text, "Adobe tweet")
	}
}
