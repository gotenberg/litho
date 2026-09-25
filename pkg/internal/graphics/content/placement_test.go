package content

import (
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
)

// TestFormatViolationPlacement pins where [ParseStreamDataForCompliance]
// places each violation: on the operation whose operands hold it, at the
// operand holding it, with no operator for the operands none follows.
func TestFormatViolationPlacement(t *testing.T) {
	t.Parallel()

	type want struct {
		operator string
		operand  string // the operand's Go type, "" for none
		last     bool
	}
	for _, tc := range []struct {
		name    string
		content string
		want    []want
	}{
		{"the string Tj shows", "<414> Tj", []want{{"Tj", "ast.PdfString", true}}},
		{"an operand before the last", "<414> 5 Tj", []want{{"Tj", "ast.PdfString", false}}},
		{"inside a TJ array", "[(a) <4G>] TJ", []want{{"TJ", "ast.PdfArray", true}}},
		{"inside a property list", "/Span << /ActualText <414> >> BDC EMC", []want{{"BDC", "ast.PdfDict", true}}},
		{
			"inside an inline image dictionary", "BI /W 1 /H 1 /BPC 8 /CS /G /F <414> ID \x80 EI",
			[]want{{"BI", "ast.PdfDict", true}},
		},
		{"an operand an inline image drops", "<414> BI /W 1 /H 1 /BPC 8 /CS /G ID \x80 EI", []want{{"BI", "", false}}},
		{"operands no operator follows", "0 0 m <414>", []want{{"", "", false}}},
		{"one per operation", "<414> zz <434> Tj", []want{{"zz", "ast.PdfString", true}, {"Tj", "ast.PdfString", true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, violations, err := ParseStreamDataForCompliance(context.Background(), []byte(tc.content))
			if err != nil {
				t.Fatalf("ParseStreamDataForCompliance: %v", err)
			}
			if len(violations) != len(tc.want) {
				t.Fatalf("got %d violations, want %d: %v", len(violations), len(tc.want), violations)
			}
			for i, v := range violations {
				if !errors.Is(v.Err, parser.ErrHexStringOddDigits) && !errors.Is(v.Err, parser.ErrHexStringInvalidChar) {
					t.Errorf("violation %d is %v, want a hex string violation", i, v.Err)
				}
				operand := ""
				switch v.Operand.(type) {
				case ast.PdfString:
					operand = "ast.PdfString"
				case ast.PdfArray:
					operand = "ast.PdfArray"
				case ast.PdfDict:
					operand = "ast.PdfDict"
				case nil:
				default:
					operand = "other"
				}
				got := want{v.Operator, operand, v.Last}
				if got != tc.want[i] {
					t.Errorf("violation %d placed at %+v, want %+v", i, got, tc.want[i])
				}
			}
		})
	}
}
