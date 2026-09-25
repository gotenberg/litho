package remediation

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// TestFixCIDSystemInfo_FontInsideObjectStream pins that a Type 0 font stored
// in an object stream is relabelled. Its cross-reference entry is type 2 (ISO
// 32000-1, 7.5.7 and Table 18), which the walk skipped, so the relabelling
// never reached the fonts of files that pack their objects.
func TestFixCIDSystemInfo_FontInsideObjectStream(t *testing.T) {
	t.Parallel()

	doc, alloc := newTestDoc(t, objStmCIDFontPDF(
		"<< /Type /Font /Subtype /Type0 /BaseFont /Probe /DescendantFonts [<< /Type /Font "+
			"/Subtype /CIDFontType0 /BaseFont /Probe "+
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> >>] >>"))
	if got := doc.Index.Entries[5].Type; got != xref.XRefTypeCompressed {
		t.Fatalf("fixture: object 5 has xref type %d, want %d (in an object stream)",
			got, xref.XRefTypeCompressed)
	}

	fixes, err := fixCIDSystemInfo(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixCIDSystemInfo: %v", err)
	}
	if len(fixes) != 1 {
		t.Fatalf("a Type 0 font inside an object stream must be relabelled, got %v", fixes)
	}
	got := pendingDict(t, alloc, 5)
	if enc := got["Encoding"]; enc != ast.PdfName("Identity-H") {
		t.Errorf("/Encoding is %v, want /Identity-H", enc)
	}
	if bf := got["BaseFont"]; bf != ast.PdfName("Probe") {
		t.Errorf("/BaseFont is %v, want the font's own /Probe", bf)
	}
}

// TestFixCIDSystemInfo_SharedCIDSystemInfoKeepsTheLowestSupplement pins that
// two CIDFonts sharing one indirect CIDSystemInfo dictionary are aligned from
// the copy the first alignment queued, not from the document's.
//
// Object 8 declares /Supplement 6. Font 4's CMap stops at supplement 2 and
// font 6's at 4, and ISO 19005-2, 6.2.11.3.1 wants the CIDFont's supplement no
// greater than its CMap's. Aligning font 4 lowers it to 2. Read from the
// document, font 6 saw 6 again and wrote 4 over that edit, which left font 4
// past its CMap. Read through the allocator, font 6 sees 2 and has nothing to
// lower.
func TestFixCIDSystemInfo_SharedCIDSystemInfoKeepsTheLowestSupplement(t *testing.T) {
	t.Parallel()

	cmap := func(sup int) string {
		const program = "%!PS-Adobe-3.0 Resource-CMap\n/CMapName /Test-H def\n"
		return fmt.Sprintf("<< /Type /CMap /CMapName /Test-H "+
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (Japan1) /Supplement %d >> /Length %d >>\n"+
			"stream\n%s\nendstream", sup, len(program), program)
	}
	ctx := context.Background()
	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F0 4 0 R /F1 6 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type0 /BaseFont /A /Encoding 9 0 R /DescendantFonts [5 0 R] >>",
		"<< /Type /Font /Subtype /CIDFontType0 /BaseFont /A /CIDSystemInfo 8 0 R >>",
		"<< /Type /Font /Subtype /Type0 /BaseFont /B /Encoding 10 0 R /DescendantFonts [7 0 R] >>",
		"<< /Type /Font /Subtype /CIDFontType0 /BaseFont /B /CIDSystemInfo 8 0 R >>",
		"<< /Registry (Adobe) /Ordering (Japan1) /Supplement 6 >>",
		cmap(2),
		cmap(4),
	))

	if _, err := fixCIDSystemInfo(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixCIDSystemInfo: %v", err)
	}
	if sup := pendingDict(t, alloc, 8)["Supplement"]; sup != ast.PdfInt(2) {
		t.Errorf("the shared /CIDSystemInfo has /Supplement %v, want 2, the lower of the two CMaps'", sup)
	}
}
