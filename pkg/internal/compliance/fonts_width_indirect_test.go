package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestWidthValue_ResolvesIndirectReference pins that a width-array entry stored
// as an indirect reference (ISO 32000-1 §7.3.10 — legal inside /Widths and /W)
// is resolved. [document.Float] cannot chase references, so it reads such an
// entry as 0, which fired a spurious glyph-width mismatch (pdfjs issue7855:
// /Widths held `9 0 R` = 229 for char 105, litho read 0.0, veraPDF resolved it).
func TestWidthValue_ResolvesIndirectReference(t *testing.T) {
	ctx := context.Background()
	// obj1 catalog, obj2 page, obj3 = the width value 229.
	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog >>",
		"<< /Type /Page >>",
		"229",
	)

	ref := ast.PdfIndirectRef{ObjectNum: 3}

	// Plain Float cannot resolve the reference — this is the defect.
	if got := document.Float(ref); got != 0 {
		t.Errorf("document.Float(ref) = %v, want 0 (Float cannot resolve; the bug widthValue fixes)", got)
	}

	// widthValue resolves it.
	if v, ok := widthValue(ctx, doc, ref); !ok || v != 229 {
		t.Errorf("widthValue(ref) = %v, %v; want 229, true", v, ok)
	}

	// A direct number passes through unchanged.
	if v, ok := widthValue(ctx, doc, ast.PdfInt(500)); !ok || v != 500 {
		t.Errorf("widthValue(500) = %v, %v; want 500, true", v, ok)
	}
}
