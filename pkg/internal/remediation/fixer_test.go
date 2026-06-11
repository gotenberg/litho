package remediation

import (
	"context"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestWalkIndirectObjects_ReadsPendingFirst guards the alloc-first read: a
// transformer must see the replacement an earlier fixer queued at an object
// number, not the parsed original, or the earlier fix is clobbered when the
// transformed object is allocated back.
func TestWalkIndirectObjects_ReadsPendingFirst(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	alloc.AllocAt(1, ast.PdfDict{
		"Type":   ast.PdfName("Catalog"),
		"Pages":  ast.PdfIndirectRef{ObjectNum: 2},
		"Marker": ast.PdfName("FromPending"),
	})

	count, err := walkIndirectObjects(context.Background(), alloc, doc,
		func(d ast.PdfDict) (ast.PdfDict, bool) {
			if d["Type"] != ast.PdfName("Catalog") {
				return d, false
			}
			nd := make(ast.PdfDict, len(d)+1)
			maps.Copy(nd, d)
			nd["Seen"] = ast.PdfBool(true)
			return nd, true
		},
		func(a ast.PdfArray) (ast.PdfArray, bool) { return a, false },
	)
	if err != nil {
		t.Fatalf("walkIndirectObjects: %v", err)
	}
	if count != 1 {
		t.Fatalf("count: got %d, want 1", count)
	}

	cat := pendingDict(t, alloc, 1)
	if cat["Marker"] != ast.PdfName("FromPending") {
		t.Error("walk clobbered the pending catalog rewrite")
	}
	if cat["Seen"] != ast.PdfBool(true) {
		t.Error("walk did not transform the pending catalog")
	}
}
