package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// widgetActionsPDF builds a page whose only annotation, object 4, is a widget
// carrying both an /A and an /AA holding a GoTo, an action type no PDF/A part
// forbids.
func widgetActionsPDF() []byte {
	return buildSimplePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Rect [0 0 10 10] /F 4 /FT /Btn /T (b1) " +
			"/A << /S /GoTo /D [3 0 R /Fit] >> /AA << /U << /S /GoTo /D [3 0 R /Fit] >> >> >>",
	})
}

// TestFixAnnotations_WidgetAAFollowsPart pins that the annotations fixer
// removes a widget's /AA only where the PDF/A part forbids it. ISO 19005-1
// §6.6.2 t1 and ISO 19005-2/3 §6.4.1 t1 forbid the key on a widget. ISO
// 19005-4 §6.4.1 t1 forbids only /A, and §6.6.3 exempts a widget's /AA from its
// key restriction, so every part-4 level keeps it. The fixer used to drop it at
// part 4 whenever another annotation violation brought it in: annotstruct_ua2
// lost its widget /AA at PDF/A-4 and PDF/A-4e and kept it at PDF/A-4f, where
// no 6.3.1 violation ran the fixer, and veraPDF 1.30.2 accepts the kept /AA at
// all three levels.
func TestFixAnnotations_WidgetAAFollowsPart(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		profile   string
		aaRemoved bool
	}{
		{"PDF/A-1b", true},
		{"PDF/A-2b", true},
		{"PDF/A-3b", true},
		{"PDF/A-4", false},
		{"PDF/A-4e", false},
		{"PDF/A-4f", false},
	} {
		t.Run(tc.profile, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, widgetActionsPDF())
			if _, err := fixAnnotations(ctx, alloc, doc, Options{}, nil, tc.profile); err != nil {
				t.Fatalf("fixAnnotations: %v", err)
			}

			widget := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 4})
			if widget == nil {
				t.Fatal("widget object 4 is gone")
			}
			if _, hasA := widget["A"]; hasA {
				t.Error("/A survived: every PDF/A part forbids it on a widget")
			}
			if _, hasAA := widget["AA"]; hasAA == tc.aaRemoved {
				t.Errorf("/AA present = %v, want %v", hasAA, !tc.aaRemoved)
			}
		})
	}
}
