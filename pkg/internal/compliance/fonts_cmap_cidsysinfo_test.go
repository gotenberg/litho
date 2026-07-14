package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestCheckCIDSystemInfo_PredefinedCMap guards the named-predefined-CMap path of
// the Type 0 font CIDSystemInfo compatibility check (ISO 19005-2 §6.2.11.3.1).
// The predefined encoding names (UniKS-UTF16-H, UniJIS-UTF16-H, ...) carry no
// Registry-Ordering-Supplement text, so their CIDSystemInfo is resolved from the
// tabulated Adobe values; a CIDFont whose collection matches the CMap must pass,
// and only a genuine registry/ordering/supplement mismatch fires. Mirrors the
// pdfcpu korean/japanese samples that veraPDF passes/fails respectively.
func TestCheckCIDSystemInfo_PredefinedCMap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	check := func(encoding, ordering string, supplement int) []Violation {
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			fmt.Sprintf("<< /Type /Font /Subtype /Type0 /BaseFont /X /Encoding /%s /DescendantFonts [5 0 R] >>", encoding),
			fmt.Sprintf("<< /Type /Font /Subtype /CIDFontType2 /BaseFont /X /CIDSystemInfo << /Registry (Adobe) /Ordering (%s) /Supplement %d >> >>", ordering, supplement),
		)
		type0, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
		if err != nil {
			t.Fatalf("resolve Type0 dict: %v", err)
		}
		cidFont, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 5})
		if err != nil {
			t.Fatalf("resolve CIDFont dict: %v", err)
		}
		return checkCIDSystemInfo(ctx, doc, type0, cidFont, "F0", 1, "PDF/A-2b")
	}

	// UniKS-UTF16-H is Adobe-Korea1-2; a CIDFont declaring Adobe-Korea1-1 is
	// compatible (1 <= 2). veraPDF passes this (the pdfcpu korean sample).
	if v := check("UniKS-UTF16-H", "Korea1", 1); len(v) != 0 {
		t.Errorf("UniKS-UTF16-H with Adobe-Korea1-1 must pass, got %v", v)
	}
	// UniJIS-UTF16-H is Adobe-Japan1-5 at ISO 32000-1; a CIDFont declaring
	// Adobe-Japan1-7 exceeds it (7 > 5) and fires (the pdfcpu japanese sample).
	if v := check("UniJIS-UTF16-H", "Japan1", 7); len(v) == 0 {
		t.Error("UniJIS-UTF16-H with Adobe-Japan1-7 must fire (supplement 7 > 5)")
	}
	// Ordering mismatch: UniKS (Korea1) against a Japan1 CIDFont must fire.
	if v := check("UniKS-UTF16-H", "Japan1", 0); len(v) == 0 {
		t.Error("UniKS-UTF16-H against an Adobe-Japan1 CIDFont must fire (ordering mismatch)")
	}
	// A CMap name that is neither embedded nor predefined must fire.
	if v := check("Bogus-CMap", "Adobe", 0); len(v) == 0 {
		t.Error("a non-predefined CMap name must fire")
	}
}
