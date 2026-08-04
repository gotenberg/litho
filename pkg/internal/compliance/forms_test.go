package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestCheckFieldAppearance covers the ISO 19005-1, 6.9 t2 widget appearance
// decision. Only part 1 reaches this function: parts 2 and up state the
// requirement on the annotation, which checkAnnotAppearance answers. See
// TestFieldAppearanceZeroAreaSplit for the two sides together.
func TestCheckFieldAppearance(t *testing.T) {
	t.Parallel()
	ctx := testCtx()

	zeroRect := ast.PdfArray{ast.PdfReal(0), ast.PdfReal(0), ast.PdfReal(0), ast.PdfReal(0)}
	area := ast.PdfArray{ast.PdfReal(0), ast.PdfReal(0), ast.PdfReal(100), ast.PdfReal(100)}

	fires := func(field ast.PdfDict) bool {
		var results []Violation
		checkFieldAppearance(ctx, nopDoc(), field, 1, 0, true, &results)
		return len(results) > 0
	}

	tests := []struct {
		name  string
		field ast.PdfDict
		want  bool
	}{
		{"zero-area widget (no exemption)", ast.PdfDict{"Subtype": ast.PdfName("Widget"), "Rect": zeroRect}, true},
		{"non-zero widget without AP", ast.PdfDict{"Subtype": ast.PdfName("Widget"), "Rect": area}, true},
		{"widget with AP", ast.PdfDict{"Subtype": ast.PdfName("Widget"), "AP": ast.PdfDict{}, "Rect": zeroRect}, false},
		{"non-terminal field (Kids, no FT)", ast.PdfDict{"Kids": ast.PdfArray{}}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := fires(tc.field); got != tc.want {
				t.Errorf("checkFieldAppearance(%v) fired=%v, want %v", tc.field, got, tc.want)
			}
		})
	}
}
