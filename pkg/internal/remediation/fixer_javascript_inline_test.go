package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// javaScriptInlineAnnotPDF builds a one-page document whose inline /Annots
// array holds an inline Link annotation with a Launch action and no /F. ISO
// 32000-1, 12.5.2 does not require an annotation to be an indirect object, so
// the only way to write this one back is through the page.
func javaScriptInlineAnnotPDF() []byte {
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /A << /S /Launch /F (a.exe) >> >>] >>",
	})
}

// TestFixJavaScript_WritesInlineAnnotationRepair pins that the repair of an
// inline annotation reaches the output. It was reported ("removed forbidden /A
// action from annotation on page 1") and never written, since the annotation
// has no object number to address, so the Launch action survived every
// fixpoint round.
//
// The peer is the real annot-flags fixer, which precedes javascript and
// installs its /F on the same inline annotation by rewriting the page: the
// repair must start from that page, not the document's.
func TestFixJavaScript_WritesInlineAnnotationRepair(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, javaScriptInlineAnnotPDF())

	if _, err := fixAnnotFlags(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixAnnotFlags: %v", err)
	}
	if _, rewritten := alloc.Pending()[3]; !rewritten {
		t.Fatal("control: annot-flags did not rewrite page 3, so this test proves nothing about its write")
	}

	fixes, err := fixJavaScript(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixJavaScript: %v", err)
	}
	if len(fixes) != 1 {
		t.Errorf("got %d fixes, want 1: %q", len(fixes), fixes)
	}

	annots := liveArray(ctx, alloc, doc, pendingDict(t, alloc, 3)["Annots"])
	if len(annots) != 1 {
		t.Fatalf("/Annots has %d entries, want 1: %v", len(annots), annots)
	}
	annot, ok := annots[0].(ast.PdfDict)
	if !ok {
		t.Fatalf("/Annots[0] is %T, want the inline annotation", annots[0])
	}
	if _, has := annot["A"]; has {
		t.Errorf("the inline annotation kept its forbidden /A: the repair was reported and never written; annot = %v", annot)
	}
	if f, _ := annot["F"].(ast.PdfInt); f != 4 {
		t.Errorf("the inline annotation carries /F %v, want the 4 annot-flags wrote; annot = %v", annot["F"], annot)
	}
}

// javaScriptIndirectAnnotsPDF builds a one-page document whose /Annots is an
// indirect array (object 4) holding an inline Link with a Launch action and a
// Sound annotation (object 5), which every PDF/A part forbids (ISO 19005-2/3,
// 6.3.1).
func javaScriptIndirectAnnotsPDF() []byte {
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots 4 0 R >>",
		"[<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /F 4 /A << /S /Launch /F (a.exe) >> >> 5 0 R]",
		"<< /Type /Annot /Subtype /Sound /Rect [0 0 10 10] /F 4 >>",
	})
}

// TestFixJavaScript_InlineAnnotationKeepsAnnotationsArrayWrite pins that an
// inline repair rewrites the indirect /Annots array from the copy the real
// annotations fixer left. That fixer precedes javascript, removes the Sound
// annotation and writes the compacted array at its own object number, so a
// rebuild from the document's array would bring the Sound annotation back.
func TestFixJavaScript_InlineAnnotationKeepsAnnotationsArrayWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, javaScriptIndirectAnnotsPDF())

	if _, err := fixAnnotations(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixAnnotations: %v", err)
	}
	if peer, ok := alloc.Pending()[4].(ast.PdfArray); !ok || len(peer) != 1 {
		t.Fatalf("control: annotations did not compact /Annots to 1 entry at object 4: %v", alloc.Pending()[4])
	}

	if _, err := fixJavaScript(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixJavaScript: %v", err)
	}

	got, ok := alloc.Pending()[4].(ast.PdfArray)
	if !ok {
		t.Fatalf("object 4 is no longer a pending array: %T", alloc.Pending()[4])
	}
	if len(got) != 1 {
		t.Fatalf("/Annots has %d entries, want 1: the rebuild restored the Sound annotation annotations removed; array = %v", len(got), got)
	}
	annot, ok := got[0].(ast.PdfDict)
	if !ok {
		t.Fatalf("/Annots[0] is %T, want the inline Link", got[0])
	}
	if _, has := annot["A"]; has {
		t.Errorf("the inline Link kept its forbidden /A; annot = %v", annot)
	}
}
