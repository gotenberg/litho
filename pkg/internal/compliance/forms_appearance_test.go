package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestCheckFieldAppearanceZeroArea pins the PDF/A-1 zero-area exemption split.
// ISO 19005-1 §6.9 requires every widget annotation to carry an appearance
// dictionary regardless of its /Rect area, so a zero-area /Rect (e.g. an
// invisible signature field) still fires under part 1. Parts 2/3/4 keep the
// zero-area exemption.
func TestCheckFieldAppearanceZeroArea(t *testing.T) {
	t.Parallel()
	ctx := testCtx()

	zeroRect := ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0)}
	realRect := ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(100), ast.PdfInt(100)}

	tests := []struct {
		name     string
		field    ast.PdfDict
		part     int
		wantFail bool
	}{
		{"part1 zero-area no AP fires", ast.PdfDict{"FT": ast.PdfName("Sig"), "Rect": zeroRect}, 1, true},
		{"part2 zero-area no AP exempt", ast.PdfDict{"FT": ast.PdfName("Sig"), "Rect": zeroRect}, 2, false},
		{"part1 real-area no AP fires", ast.PdfDict{"FT": ast.PdfName("Tx"), "Rect": realRect}, 1, true},
		{"part2 real-area no AP fires", ast.PdfDict{"FT": ast.PdfName("Tx"), "Rect": realRect}, 2, true},
		{"part1 zero-area with AP passes", ast.PdfDict{"FT": ast.PdfName("Sig"), "Rect": zeroRect, "AP": ast.PdfDict{}}, 1, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var results []Violation
			checkFieldAppearance(ctx, nopDoc(), tc.field, tc.part, 0, &results)
			if (len(results) > 0) != tc.wantFail {
				t.Errorf("got %d violations, wantFail=%v", len(results), tc.wantFail)
			}
		})
	}
}
