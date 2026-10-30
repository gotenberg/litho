package pdf

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

// annotationTextPDF is a page whose content draws BODY under a flipping cm it
// never restores, as a converter that writes text only into annotations
// leaves it, with a FreeText annotation whose appearance draws NOTE and a
// hidden one whose appearance draws HIDDEN (ISO 32000-1, 12.5.3, Table 165).
// The crop box keeps the top half of the page, where both annotations lie.
func annotationTextPDF() []byte {
	const page = "1 0 0 -1 0 792 cm\nBT /F1 12 Tf 1 0 0 -1 72 72 Tm (BODY) Tj ET\n"
	const note = "BT /F1 12 Tf 2 4 Td (NOTE) Tj ET\n"
	const hidden = "BT /F1 12 Tf 2 4 Td (HIDDEN) Tj ET\n"
	return buildTestPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /CropBox [0 400 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R /Annots [6 0 R 8 0 R] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page), page),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Annot /Subtype /FreeText /Rect [70 740 130 760] /F 4 /DA (/F1 12 Tf) /Contents (NOTE) /AP << /N 7 0 R >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 60 20] /Resources << /Font << /F1 5 0 R >> >> /Length %d >>\nstream\n%s\nendstream", len(note), note),
		"<< /Type /Annot /Subtype /FreeText /Rect [300 600 360 620] /F 6 /DA (/F1 12 Tf) /Contents (HIDDEN) /AP << /N 9 0 R >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 60 20] /Resources << /Font << /F1 5 0 R >> >> /Length %d >>\nstream\n%s\nendstream", len(hidden), hidden),
	}, "")
}

// TestExtractText_AnnotationText pins that annotation appearances are left
// out by default and read with [WithAnnotationText], from the page's initial
// graphics state rather than the one its content leaves behind, and that a
// hidden annotation stays out either way.
func TestExtractText_AnnotationText(t *testing.T) {
	t.Parallel()

	data := annotationTextPDF()
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
	if !strings.Contains(got, "BODY") || strings.Contains(got, "NOTE") {
		t.Errorf("ExtractText = %q, want BODY without NOTE", got)
	}

	got, err = r.ExtractText(ctx, 1, WithAnnotationText())
	if err != nil {
		t.Fatalf("ExtractText(WithAnnotationText): %v", err)
	}
	if !strings.Contains(got, "BODY") || !strings.Contains(got, "NOTE") {
		t.Errorf("ExtractText(WithAnnotationText) = %q, want BODY and NOTE", got)
	}
	if strings.Contains(got, "HIDDEN") {
		t.Errorf("ExtractText(WithAnnotationText) = %q, read a hidden annotation", got)
	}

	// Drawn under the flipped CTM the page leaves, NOTE would land near y
	// 48, below the crop box, and read as no text.
}
