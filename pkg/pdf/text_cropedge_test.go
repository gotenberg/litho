package pdf

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestExtractText_GlyphPastTheCropBoxRightEdgeStaysOut pins that a glyph
// whose origin lies past the right edge of the crop box reads as no text,
// even when it continues a word inside the box: upright text advances
// rightward from its origin, so the whole glyph paints outside the visible
// region (ISO 32000-1, 14.11.2). "B" starts 0.404 units past the edge.
func TestExtractText_GlyphPastTheCropBoxRightEdgeStaysOut(t *testing.T) {
	t.Parallel()

	const content = "BT /F1 12 Tf 112.4 700 Td (AB) Tj ET\n"
	data := buildTestPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /CropBox [0 0 120 792] /Resources 4 0 R /Contents 5 0 R >>",
		"<< /Font << /F1 6 0 R >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}, "")

	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	got, err := r.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if got = strings.TrimSpace(got); got != "A" {
		t.Fatalf("ExtractText = %q, want %q", got, "A")
	}
}
