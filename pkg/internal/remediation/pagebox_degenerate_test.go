package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestDegenerateBoxRepairKeepsContent pins how a box no viewer uses, one
// enclosing no area or that is not four numbers, is repaired. Growing it to
// the 3-unit minimum at its lower-left corner is conforming but leaves the
// page's content outside the page, and the text drawn there is lost. A media
// box of five entries, or holding a name, is no size the document uses either:
// a viewer draws its page on US Letter.
//
// # Corpus evidence
//
//   - pdfjs boundingBox_invalid.pdf: lost the text of two pages that way, and
//     litho's own regression gate then refused to remediate the file at all.
func TestDegenerateBoxRepairKeepsContent(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		box  ast.PdfArray
		want bool
	}{
		{"empty", ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0)}, true},
		{"zero height", ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(800), ast.PdfInt(0)}, true},
		{"zero width", ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(600)}, true},
		{"normal", ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(612), ast.PdfInt(792)}, false},
		{"tiny but real", ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(1)}, false},
		{"five entries", ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(612), ast.PdfInt(792), ast.PdfInt(7)}, true},
		{"a name", ast.PdfArray{ast.PdfInt(0), ast.PdfName("Foo"), ast.PdfInt(612), ast.PdfInt(792)}, true},
	} {
		if got := !document.UsableBox(tc.box); got != tc.want {
			t.Errorf("no viewer uses %v: got %v, want %v", tc.box, got, tc.want)
		}
	}

	// The replacement takes the largest width and the largest height
	// independently. Choosing the whole box with the greatest AREA is not
	// enough: 800x600 and 600x800 tie on area, and picking either one crops
	// content the other would have kept.
	entries := []annotPageEntry{
		{dict: ast.PdfDict{"MediaBox": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(800), ast.PdfInt(600)}}},
		{dict: ast.PdfDict{"MediaBox": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(600), ast.PdfInt(800)}}},
		{dict: ast.PdfDict{"MediaBox": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0)}}},
		{dict: ast.PdfDict{"MediaBox": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(900), ast.PdfInt(900), ast.PdfInt(7)}}},
		{dict: ast.PdfDict{"MediaBox": ast.PdfArray{ast.PdfInt(0), ast.PdfName("Foo"), ast.PdfInt(900), ast.PdfInt(900)}}},
	}
	got := widestUsableMediaBox(context.Background(), nil, entries)
	want := ast.PdfArray{ast.PdfReal(0), ast.PdfReal(0), ast.PdfReal(800), ast.PdfReal(800)}
	if len(got) != 4 {
		t.Fatalf("widestUsableMediaBox returned %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("widestUsableMediaBox = %v, want %v", got, want)
		}
	}

	// With nothing usable in the document, fall back to a real page size.
	if fb := widestUsableMediaBox(context.Background(), nil, nil); len(fb) != 4 || fb[3] != ast.PdfReal(792) {
		t.Errorf("fallback = %v, want US Letter", fb)
	}
}
