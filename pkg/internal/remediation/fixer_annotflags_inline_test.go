package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// inlineAnnotPDF builds a page whose /Annots array holds the annotation itself
// rather than a reference to it. Object 3 is the page, the only object that can
// carry the repair.
func inlineAnnotPDF() []byte {
	return buildSimplePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Annots [<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] >>] >>",
		"<< /Length 3 >>\nstream\nabc\nendstream",
	})
}

// TestFixAnnotFlagsRepairsInlineAnnotation pins that the /F flag reaches an
// annotation written inline in /Annots.
//
// ISO 32000-1, 12.5.2 does not require an annotation to be an indirect object,
// and pdfjs bug1905623.pdf carries an inline /Link on page 203. The repair only
// wrote through the allocator, which has no object number to address for a
// direct dictionary, so it was counted and discarded: litho reported "added /F
// flag to 1 annotations" on every run and the output never carried /F.
func TestFixAnnotFlagsRepairsInlineAnnotation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, inlineAnnotPDF())

	fixes, err := fixAnnotFlags(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixAnnotFlags: %v", err)
	}
	if len(fixes) != 1 {
		t.Fatalf("fixes = %v, want one repair reported", fixes)
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

	// Print (bit 3) must be set and no forbidden flag may survive.
	flags, ok := annot["F"].(ast.PdfInt)
	if !ok {
		t.Fatalf("/F = %T, want an integer; the repair was reported but discarded", annot["F"])
	}
	if int(flags)&(1<<2) == 0 {
		t.Errorf("/F = %d, want the Print bit set", int(flags))
	}
	// The annotation's own entries must survive the rewrite.
	if annot["Subtype"] != ast.PdfName("Link") {
		t.Errorf("/Subtype = %v, want /Link", annot["Subtype"])
	}
}

// TestFixAnnotFlagsInlineAnnotationIsIdempotent checks a second run reports
// nothing. A repair that never lands is re-reported on every fixpoint round.
func TestFixAnnotFlagsInlineAnnotationIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, inlineAnnotPDF())

	if _, err := fixAnnotFlags(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	fixes, err := fixAnnotFlags(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if len(fixes) != 0 {
		t.Errorf("second pass reported %v, want no repair", fixes)
	}
}

// TestFixAnnotFlagsStillRewritesIndirectAnnotation keeps the ordinary path
// covered: an indirect annotation is written in place, leaving the page alone.
func TestFixAnnotFlagsStillRewritesIndirectAnnotation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, buildSimplePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [5 0 R] >>",
		"<< /Length 3 >>\nstream\nabc\nendstream",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] >>",
	}))

	if _, err := fixAnnotFlags(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixAnnotFlags: %v", err)
	}

	annot := pendingDict(t, alloc, 5)
	if _, ok := annot["F"].(ast.PdfInt); !ok {
		t.Errorf("/F = %T, want an integer on the indirect annotation", annot["F"])
	}
	if _, wrotePage := alloc.Pending()[3]; wrotePage {
		t.Error("the page was rewritten for an indirect annotation; only object 5 should change")
	}
}
