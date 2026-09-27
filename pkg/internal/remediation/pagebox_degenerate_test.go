package remediation

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestDegenerateBoxRepairKeepsContent pins which boxes the page box repair
// reads as no box a viewer uses, one enclosing no area or that is not four
// numbers, and so replaces rather than grows. Growing such a box to the
// 3-unit minimum at its lower-left corner is conforming but leaves the page's
// content outside the page, and the text drawn there is lost. A media box of
// five entries, or holding a name, is no box either: a viewer draws its page on
// US Letter.
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
}
