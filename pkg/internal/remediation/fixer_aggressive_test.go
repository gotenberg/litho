package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// annotatedSinglePagePDF is the shared fixture for appearance-stream tests:
// one page with one annotation (object 4) that lacks /AP.
func annotatedSinglePagePDF() []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Rect [0 0 10 10] >>",
	)
}

func TestFixAnnotationAppearances_AddsAP(t *testing.T) {
	doc, alloc := newTestDoc(t, annotatedSinglePagePDF())

	descs, err := fixAnnotationAppearances(context.Background(), alloc, doc)
	if err != nil {
		t.Fatalf("fixAnnotationAppearances: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	annot := pendingDict(t, alloc, 4)
	if _, hasAP := annot["AP"]; !hasAP {
		t.Error("annotation /AP not added")
	}
}

// TestFixAnnotationAppearances_RespectsPendingAnnot guards the alloc-first
// read: when an earlier fixer queued a rewrite of the annotation (here a /F
// flag fix), the /AP patch must start from that pending dict or the earlier
// fix is clobbered on AllocAt.
func TestFixAnnotationAppearances_RespectsPendingAnnot(t *testing.T) {
	doc, alloc := newTestDoc(t, annotatedSinglePagePDF())

	alloc.AllocAt(4, ast.PdfDict{
		"Type":    ast.PdfName("Annot"),
		"Subtype": ast.PdfName("Widget"),
		"Rect":    ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(10), ast.PdfInt(10)},
		"F":       ast.PdfInt(4),
	})

	if _, err := fixAnnotationAppearances(context.Background(), alloc, doc); err != nil {
		t.Fatalf("fixAnnotationAppearances: %v", err)
	}
	annot := pendingDict(t, alloc, 4)
	if annot["F"] != ast.PdfInt(4) {
		t.Error("fixAnnotationAppearances clobbered the pending /F fix")
	}
	if _, hasAP := annot["AP"]; !hasAP {
		t.Error("annotation /AP not added to the pending dict")
	}
}

// TestFixAnnotationAppearances_SkipsAnnotsRemovedFromPage guards against
// resurrecting annotations: when an earlier fixer removed the page's
// /Annots, the orphaned annotation must not be patched back to life.
func TestFixAnnotationAppearances_SkipsAnnotsRemovedFromPage(t *testing.T) {
	doc, alloc := newTestDoc(t, annotatedSinglePagePDF())

	alloc.AllocAt(3, ast.PdfDict{
		"Type":     ast.PdfName("Page"),
		"Parent":   ast.PdfIndirectRef{ObjectNum: 2},
		"MediaBox": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(612), ast.PdfInt(792)},
	})

	descs, err := fixAnnotationAppearances(context.Background(), alloc, doc)
	if err != nil {
		t.Fatalf("fixAnnotationAppearances: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("expected no fixes, got %v", descs)
	}
	if _, resurrect := alloc.Pending()[4]; resurrect {
		t.Error("fixAnnotationAppearances patched an annotation removed by an earlier fixer")
	}
}
