package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// inlineNoAPPDF builds a page whose /Annots array holds a /Square annotation
// inline with no appearance stream. Object 3 is the page, the only object that
// can carry the repair.
func inlineNoAPPDF() []byte {
	return buildSimplePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [" +
			"<< /Type /Annot /Subtype /Square /Rect [10 10 60 60] /F 4 >>] >>",
		"<< /Length 3 >>\nstream\nabc\nendstream",
	})
}

// TestFixAnnotationAppearancesReachesInlineAnnotation pins that an annotation
// written inline in /Annots receives its generated appearance stream.
//
// ISO 32000-1, 12.5.2 does not require an annotation to be an indirect object.
// The repair wrote only through the allocator, so for an inline annotation it
// was counted and discarded while the appearance stream allocated for it was
// left orphaned in the output on every fixpoint round.
func TestFixAnnotationAppearancesReachesInlineAnnotation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, inlineNoAPPDF())

	fixes, err := fixAnnotationAppearances(ctx, alloc, doc)
	if err != nil {
		t.Fatalf("fixAnnotationAppearances: %v", err)
	}
	if len(fixes) != 1 {
		t.Fatalf("fixes = %v, want one repair reported", fixes)
	}

	page := pendingDict(t, alloc, 3)
	annots, ok := page["Annots"].(ast.PdfArray)
	if !ok {
		t.Fatalf("/Annots = %T, want the rebuilt array", page["Annots"])
	}
	annot, ok := annots[0].(ast.PdfDict)
	if !ok {
		t.Fatalf("/Annots[0] = %T, want the inline dictionary", annots[0])
	}
	ap, ok := annot["AP"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/AP = %T, want a dictionary; the repair was reported but discarded", annot["AP"])
	}
	if _, hasN := ap["N"]; !hasN {
		t.Error("/AP lacks /N")
	}
	if annot["Subtype"] != ast.PdfName("Square") {
		t.Errorf("/Subtype = %v, want /Square", annot["Subtype"])
	}
}

// TestFixAnnotationAppearancesInlineIsIdempotent checks a second run reports
// nothing and allocates nothing, so the fixpoint loop neither re-reports the
// repair nor grows the file with orphaned appearance streams.
func TestFixAnnotationAppearancesInlineIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, inlineNoAPPDF())

	if _, err := fixAnnotationAppearances(ctx, alloc, doc); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	before := len(alloc.Pending())

	fixes, err := fixAnnotationAppearances(ctx, alloc, doc)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if len(fixes) != 0 {
		t.Errorf("second pass reported %v, want no repair", fixes)
	}
	if got := len(alloc.Pending()); got != before {
		t.Errorf("second pass allocated %d more objects, want none orphaned", got-before)
	}
}
