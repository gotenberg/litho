package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

func TestCheckInlineImageInterpolate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		ops     []content.Operation
		wantHit bool
	}{
		{
			"full name true",
			[]content.Operation{{Operator: "BI", Operands: []ast.PdfObject{ast.PdfDict{"Interpolate": ast.PdfBool(true), "W": ast.PdfInt(100)}}}},
			true,
		},
		{
			"full name false",
			[]content.Operation{{Operator: "BI", Operands: []ast.PdfObject{ast.PdfDict{"Interpolate": ast.PdfBool(false), "W": ast.PdfInt(100)}}}},
			false,
		},
		{
			"abbreviated /I true",
			[]content.Operation{{Operator: "BI", Operands: []ast.PdfObject{ast.PdfDict{"I": ast.PdfBool(true), "W": ast.PdfInt(100)}}}},
			true,
		},
		{
			"abbreviated /I false",
			[]content.Operation{{Operator: "BI", Operands: []ast.PdfObject{ast.PdfDict{"I": ast.PdfBool(false), "W": ast.PdfInt(100)}}}},
			false,
		},
		{
			"no interpolate key",
			[]content.Operation{{Operator: "BI", Operands: []ast.PdfObject{ast.PdfDict{"W": ast.PdfInt(100), "H": ast.PdfInt(200)}}}},
			false,
		},
		{
			"empty ops",
			nil,
			false,
		},
		{
			"non-BI operator",
			[]content.Operation{{Operator: "BT"}},
			false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			violations := checkInlineImageInterpolate(tc.ops, 1, 1)
			gotHit := len(violations) > 0
			if gotHit != tc.wantHit {
				t.Errorf("checkInlineImageInterpolate() = %v violations, wantHit %v", len(violations), tc.wantHit)
			}
		})
	}
}

// TestXObjectRules_PartLabeling guards the image/form XObject rule selectors
// against the `case 2, 3:` collapse that reported every PDF/A-3 violation under
// the PDF/A-2 rule object, leaving the PDF/A-3 rules unreachable.
func TestXObjectRules_PartLabeling(t *testing.T) {
	t.Parallel()
	im := ast.PdfName("Im0")
	interp := []content.Operation{{Operator: "BI", Operands: []ast.PdfObject{ast.PdfDict{"Interpolate": ast.PdfBool(true)}}}}
	cases := []struct {
		name string
		got  []Violation
		want Rule
	}{
		{"ref XObject part 3", refXObjectRule(3, 1, im), Rule_19005_3_6_2_9_t02},
		{"ref XObject part 2 unchanged", refXObjectRule(2, 1, im), Rule_19005_2_6_2_9_t02},
		{"ps XObject part 3", psXObjectRule(3, 1, im), Rule_19005_3_6_2_9_t03},
		{"ps XObject part 2 unchanged", psXObjectRule(2, 1, im), Rule_19005_2_6_2_9_t03},
		{"inline interpolate part 3", checkInlineImageInterpolate(interp, 1, 3), Rule_19005_3_6_2_8_t03},
		{"form OPI part 3", checkFormXObject(context.Background(), nil, ast.PdfDict{"OPI": ast.PdfDict{}}, im, 1, 3, "PDF/A-3b"), Rule_19005_3_6_2_9_t01},
		{"image Alternates part 3", checkImageXObject(context.Background(), nil, ast.PdfDict{"Alternates": ast.PdfArray{}}, im, 1, 3), Rule_19005_3_6_2_8_t01},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if len(tc.got) == 0 {
				t.Fatal("expected a violation, got none")
			}
			r := tc.got[0].Rule
			if r.Specification != tc.want.Specification || r.Clause != tc.want.Clause || r.TestNumber != tc.want.TestNumber {
				t.Errorf("rule = %s/%s/t%d, want %s/%s/t%d",
					r.Specification, r.Clause, r.TestNumber,
					tc.want.Specification, tc.want.Clause, tc.want.TestNumber)
			}
		})
	}
}
