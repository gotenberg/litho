package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestInjectDefaultColorSpace_KeepsInheritedResources pins that adding a
// /Default colour space to a page does not discard what the page inherits.
// A page with no /Resources of its own takes them from an ancestor
// (ISO 32000-2, 7.7.3.4), so writing a fresh dictionary onto the page shadows
// the inherited one and makes every inherited font and XObject unreachable.
// The fixer used to do exactly that, which cost the page its /Font.
func TestInjectDefaultColorSpace_KeepsInheritedResources(t *testing.T) {
	ctx := context.Background()

	// Object 3 is the page and carries no /Resources; object 2, the page tree
	// node, carries the dictionary the page inherits.
	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 /Resources << /Font << /F1 4 0 R >> >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	))
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}

	csArr := func() ast.PdfArray {
		return ast.PdfArray{ast.PdfName("ICCBased"), ast.PdfIndirectRef{ObjectNum: 4}}
	}
	n, err := injectDefaultColorSpace(ctx, alloc, doc, catalog.Dict, "DefaultCMYK", csArr)
	if err != nil {
		t.Fatalf("injectDefaultColorSpace: %v", err)
	}
	if n != 1 {
		t.Fatalf("patched %d pages, want 1", n)
	}

	page := pendingDict(t, alloc, 3)
	resources, ok := page["Resources"].(ast.PdfDict)
	if !ok {
		t.Fatalf("page /Resources: got %T, want a dictionary", page["Resources"])
	}
	if _, hasFont := resources["Font"]; !hasFont {
		t.Errorf("page /Resources lost the inherited /Font: %v", resources)
	}
	colorSpace, hasCS := resources["ColorSpace"].(ast.PdfDict)
	if !hasCS {
		t.Fatalf("page /Resources has no /ColorSpace: %v", resources)
	}
	if _, hasKey := colorSpace["DefaultCMYK"]; !hasKey {
		t.Errorf("/ColorSpace has no /DefaultCMYK: %v", colorSpace)
	}
}
