package compliance

import (
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFieldAppearanceZeroAreaSplit pins which function owns the missing-/AP
// verdict, and with it the zero-area exemption.
//
// ISO 19005-1, 6.9 t2 states the requirement on the form field and grants no
// exemption, so a zero-area /Rect (an invisible signature field) still answers.
// ISO 19005-2/3/4, 6.3.3 t1 states it on the widget annotation instead and does
// exempt a zero-area one, matching veraPDF's PDWidgetAnnot test
// `(width == 0 && height == 0) || ... || AP != null`.
//
// The two must not both report: a merged field/widget (ISO 32000-1, 12.7.4.1)
// is reached by the field walk and by the page /Annots walk alike, so parts 2
// and up never call checkFieldAppearance at all.
func TestFieldAppearanceZeroAreaSplit(t *testing.T) {
	t.Parallel()
	ctx := testCtx()

	zeroRect := ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0)}
	realRect := ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(100), ast.PdfInt(100)}

	t.Run("part 1 on the field", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			name     string
			field    ast.PdfDict
			wantFail bool
		}{
			{"zero-area no AP fires", ast.PdfDict{"FT": ast.PdfName("Sig"), "Rect": zeroRect}, true},
			{"real-area no AP fires", ast.PdfDict{"FT": ast.PdfName("Tx"), "Rect": realRect}, true},
			{"zero-area with AP passes", ast.PdfDict{"FT": ast.PdfName("Sig"), "Rect": zeroRect, "AP": ast.PdfDict{}}, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				var results []Violation
				checkFieldAppearance(ctx, nopDoc(), tc.field, 1, 0, true, &results)
				if (len(results) > 0) != tc.wantFail {
					t.Errorf("got %d violations, wantFail=%v", len(results), tc.wantFail)
				}
			})
		}
	})

	t.Run("part 2 on the annotation", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			name     string
			annot    ast.PdfDict
			wantFail bool
		}{
			{"zero-area no AP exempt", ast.PdfDict{"FT": ast.PdfName("Sig"), "Rect": zeroRect}, false},
			{"real-area no AP fires", ast.PdfDict{"FT": ast.PdfName("Tx"), "Rect": realRect}, true},
			{"zero-area with AP passes", ast.PdfDict{"FT": ast.PdfName("Sig"), "Rect": zeroRect, "AP": ast.PdfDict{}}, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				// An /AP that is present but empty also answers 6.3.3 t3, a
				// different clause. This is about t1, so keep only that.
				got := slices.DeleteFunc(
					checkAnnotAppearance(ctx, nopDoc(), tc.annot, "Widget", 2, 'b', "annot"),
					func(v Violation) bool { return v.Rule != Rule_19005_2_6_3_3_t01 })
				if (len(got) > 0) != tc.wantFail {
					t.Errorf("got %d violations, wantFail=%v", len(got), tc.wantFail)
				}
			})
		}
	})
}
