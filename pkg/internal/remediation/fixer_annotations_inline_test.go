package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// inlineWidgetPDF builds a page whose /Annots array holds a widget annotation
// inline, carrying the /A and /AA entries PDF/A forbids. Object 3 is the page,
// the only object that can carry the repair.
func inlineWidgetPDF() []byte {
	return buildSimplePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [" +
			"<< /Type /Annot /Subtype /Widget /Rect [0 0 10 10] /F 4 /FT /Btn /T (b1) " +
			"/A << /S /JavaScript /JS (x) >> /AA << /K << /S /JavaScript /JS (y) >> >> >>] >>",
		"<< /Length 3 >>\nstream\nabc\nendstream",
	})
}

// TestFixAnnotationsCleansInlineWidget pins that /A and /AA are stripped from a
// widget annotation written inline in /Annots.
//
// ISO 19005-2, 6.6.1 and 6.6.2 forbid those entries on a widget. The repair only
// wrote through the allocator, which has no object number to address for a
// direct dictionary, so it was reported and discarded: litho printed "removed
// /AA from widget annotation" on all three fixpoint rounds and the output still
// carried both keys.
func TestFixAnnotationsCleansInlineWidget(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, inlineWidgetPDF())

	fixes, err := fixAnnotations(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixAnnotations: %v", err)
	}
	if len(fixes) == 0 {
		t.Fatal("no repair reported for a widget carrying /A and /AA")
	}

	page := pendingDict(t, alloc, 3)
	annots, ok := page["Annots"].(ast.PdfArray)
	if !ok {
		t.Fatalf("/Annots = %T, want the rebuilt array", page["Annots"])
	}
	if len(annots) != 1 {
		t.Fatalf("/Annots holds %d entries, want 1", len(annots))
	}
	annot, ok := annots[0].(ast.PdfDict)
	if !ok {
		t.Fatalf("/Annots[0] = %T, want the inline dictionary", annots[0])
	}

	if _, present := annot["AA"]; present {
		t.Error("/AA survived the repair that reported removing it")
	}
	if _, present := annot["A"]; present {
		t.Error("/A survived the repair that reported removing it")
	}
	// The widget itself must survive: the rule strips actions, not the field.
	if annot["Subtype"] != ast.PdfName("Widget") {
		t.Errorf("/Subtype = %v, want /Widget", annot["Subtype"])
	}
	if annot["T"] != ast.PdfString("b1") {
		t.Errorf("/T = %v, want (b1); the field name must survive", annot["T"])
	}
}

// TestFixAnnotationsInlineWidgetIsIdempotent checks a second run reports
// nothing, so the fixpoint loop terminates instead of re-reporting every round.
func TestFixAnnotationsInlineWidgetIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, inlineWidgetPDF())

	if _, err := fixAnnotations(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	fixes, err := fixAnnotations(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if len(fixes) != 0 {
		t.Errorf("second pass reported %v, want no repair", fixes)
	}
}
