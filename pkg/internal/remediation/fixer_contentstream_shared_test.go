package remediation

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFixTextControl_KeepsASingleStreamSharedWithAnotherPage pins that
// rewriting a page whose /Contents is one stream another page's /Contents also
// names leaves that stream as the other page reads it.
//
// ISO 32000-1, 7.7.3.3 lets two pages name the same content stream, alone or
// in an array, and each page reads it through its own /Resources (7.8.3). The
// control-byte strip judges a code under the font the page names: page 3's F1
// is simple and gives 0x01 no glyph, so the strip removes it, while page 4's
// F1 is composite, where 0x01 is half of a two-byte code the strip never
// touches. The rewrite of the single stream in place took the byte off page 4
// too, changing which glyphs its composite font shows.
func TestFixTextControl_KeepsASingleStreamSharedWithAnotherPage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	const shown = `BT /F1 12 Tf (A\001B) Tj ET`
	for _, tc := range []struct {
		name  string
		page4 string
	}{
		{name: "the other page names it in an array", page4: "[5 0 R]"},
		{name: "the other page names it alone", page4: "5 0 R"},
	} {
		doc, alloc := newTestDoc(t, buildStructPDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R /Resources << /Font << /F1 6 0 R >> >> >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents "+tc.page4+" /Resources << /Font << /F1 7 0 R >> >> >>",
			"<< /Length 27 >>\nstream\n"+shown+"\nendstream",
			"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
			"<< /Type /Font /Subtype /Type0 /BaseFont /Composite /Encoding /Identity-H /DescendantFonts [] >>",
		))
		if _, err := fixTextControl(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
			t.Fatalf("%s: fixTextControl: %v", tc.name, err)
		}
		contents := pageContents(t, ctx, doc, alloc, 3, 4)
		page3 := reachableContent(t, ctx, alloc, doc, contents[0], 0)
		if strings.Contains(page3, `\001`) || strings.Contains(page3, "\x01") || !strings.Contains(page3, "A") {
			t.Errorf("%s: page 3 shows %q, want the control byte stripped", tc.name, strings.TrimSpace(page3))
		}
		page4 := reachableContent(t, ctx, alloc, doc, contents[1], 0)
		if !strings.Contains(page4, shown) {
			t.Errorf("%s: page 4 shows %q, want %q as written", tc.name, strings.TrimSpace(page4), shown)
		}
		if _, ok := alloc.Pending()[5].(ast.PdfStream); ok {
			t.Errorf("%s: the shared stream was rewritten in place", tc.name)
		}
	}
}
