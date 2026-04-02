package compliance

import (
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
