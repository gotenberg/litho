package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestCheckFieldAppearance covers the §6.9 t2 widget appearance decision,
// including the part-1-only loss of the zero-area exemption that the page
// /Annots widget scan relies on.
func TestCheckFieldAppearance(t *testing.T) {
	t.Parallel()
	ctx := testCtx()

	zeroRect := ast.PdfArray{ast.PdfReal(0), ast.PdfReal(0), ast.PdfReal(0), ast.PdfReal(0)}
	area := ast.PdfArray{ast.PdfReal(0), ast.PdfReal(0), ast.PdfReal(100), ast.PdfReal(100)}

	fires := func(field ast.PdfDict, part int) bool {
		var results []Violation
		checkFieldAppearance(ctx, nopDoc(), field, part, 0, true, &results)
		return len(results) > 0
	}

	tests := []struct {
		name  string
		field ast.PdfDict
		part  int
		want  bool
	}{
		{"zero-area widget, part 1 (no exemption)", ast.PdfDict{"Subtype": ast.PdfName("Widget"), "Rect": zeroRect}, 1, true},
		{"zero-area widget, part 2 (exempt)", ast.PdfDict{"Subtype": ast.PdfName("Widget"), "Rect": zeroRect}, 2, false},
		{"non-zero widget without AP, part 2", ast.PdfDict{"Subtype": ast.PdfName("Widget"), "Rect": area}, 2, true},
		{"widget with AP", ast.PdfDict{"Subtype": ast.PdfName("Widget"), "AP": ast.PdfDict{}, "Rect": zeroRect}, 1, false},
		{"non-terminal field (Kids, no FT)", ast.PdfDict{"Kids": ast.PdfArray{}}, 1, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := fires(tc.field, tc.part); got != tc.want {
				t.Errorf("checkFieldAppearance(%v, part=%d) fired=%v, want %v", tc.field, tc.part, got, tc.want)
			}
		})
	}
}
