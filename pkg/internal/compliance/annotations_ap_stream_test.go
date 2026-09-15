package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestAnnotAppearanceStream pins ISO 19005-2, 6.3.3 t1 on an /AP that names a
// stream: veraPDF's PDAnnotation.getCOSAP keeps /AP only when it is a
// dictionary, so a stream is no appearance dictionary and the annotation carries
// none. Measured against veraPDF on a Square and on a widget.
func TestAnnotAppearanceStream(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Square /Rect [10 10 60 60] /F 4 /AP 5 0 R >>",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 50 50] /Length 0 >>\nstream\n\nendstream",
	)
	viols, err := ValidatePDFA(ctx, doc, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	if got := countRuleErrs(t, viols, Rule_19005_2_6_3_3_t01); got != 1 {
		t.Errorf("6.3.3 t1 fired %d times, want 1", got)
	}
}
