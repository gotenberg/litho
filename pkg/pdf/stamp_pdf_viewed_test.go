package pdf

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
)

// viewedSourcePDF builds a one-page document whose page states pageEntries
// (its boxes and /Rotate) and shows word with its origin at (x, y).
func viewedSourcePDF(pageEntries, word string, x, y float64) []byte {
	content := fmt.Sprintf("BT /F1 12 Tf %g %g Td (%s) Tj ET\n", x, y, word)
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R " + pageEntries + " /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
}

// firstGlyphOf returns the origin of the first glyph of word on page 1 of r.
func firstGlyphOf(t *testing.T, r *Reader, word string) (x, y float64) {
	t.Helper()

	elements, err := r.ExtractTextElements(context.Background(), 1)
	if err != nil {
		t.Fatalf("ExtractTextElements: %v", err)
	}
	for _, e := range elements {
		if strings.HasPrefix(e.Text, word[:1]) {
			return e.X, e.Y
		}
	}
	t.Fatalf("no glyph of %q on the page among %d elements", word, len(elements))
	return 0, 0
}

// TestStampPDFPlacesThePageAsViewed pins that X and Y position the bottom-left
// corner of the source page as a viewer shows it. The source's box starts at
// (100, 200), so its text, 50 points in from that corner, lands 50 points in
// from (X, Y). The stamp used to land offset by the box's origin, at
// (X + 100 + 50, Y + 200 + 50). A page turned a quarter is drawn turned.
func TestStampPDFPlacesThePageAsViewed(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		entries      string
		srcX, srcY   float64
		wantX, wantY float64
	}{
		{
			name:    "an offset box",
			entries: "/MediaBox [100 200 712 992]",
			srcX:    150, srcY: 250,
			wantX: 60, wantY: 60,
		},
		{
			name:    "a crop box inside the media box",
			entries: "/MediaBox [0 0 612 792] /CropBox [100 200 400 600]",
			srcX:    150, srcY: 250,
			wantX: 60, wantY: 60,
		},
		{
			// (x, y) shows at (y - lly, urx - x) on a page turned a quarter
			// clockwise: (20, 190), then moved by (10, 10).
			name:    "a quarter turn",
			entries: "/MediaBox [0 0 200 100] /Rotate 90",
			srcX:    10, srcY: 20,
			wantX: 30, wantY: 200,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			target := openBytes(t, buildPDF([]string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			}))
			defer target.Close()
			src := openBytes(t, viewedSourcePDF(tc.entries, "Viewed", tc.srcX, tc.srcY))
			defer src.Close()

			w := NewWriter(target)
			defer w.Close()
			if err := w.StampPDF(PDFStamp{Source: src, X: 10, Y: 10}); err != nil {
				t.Fatalf("StampPDF: %v", err)
			}
			x, y := firstGlyphOf(t, writeAndReopen(t, w), "Viewed")
			if math.Abs(x-tc.wantX) > 0.01 || math.Abs(y-tc.wantY) > 0.01 {
				t.Errorf("the stamp's text starts at (%g, %g), want (%g, %g)", x, y, tc.wantX, tc.wantY)
			}
		})
	}
}
