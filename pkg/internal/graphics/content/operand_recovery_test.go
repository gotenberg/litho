package content

import (
	"context"
	"reflect"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// operators returns the operator of every operation, in order.
func operators(ops []Operation) []string {
	out := make([]string, len(ops))
	for i, op := range ops {
		out[i] = op.Operator
	}
	return out
}

// TestParseStreamDataDropsOnlyTheMalformedOperand pins that an operand the
// parser cannot read costs itself and nothing after it.
//
// Recovering from a malformed operand used to consume one token more than the
// failed parse had, and the token after an operand is often the operator: "/F1
// 1e2 Tf" lost its Tf, the stray /F1 joined the Td that followed, and a text
// rewrite that re-serialised the page wrote "/F1 50 600 Td". Inside an array
// the extra token could be the "]" closing it, which ran the array on through
// the operators after it, and inside an inline image dictionary it could be
// the ID that ends it.
//
// The streams are read as PDF.js reads them ([SplitGluedOperators]), where
// "1e2" is one number the parser cannot read. The reading of veraPDF ends the
// number at the "e", so there it is the number 1, the operator e and the
// number 2 ([lexer.Lexer.WithVeraPDFTokens]).
//
// # Corpus evidence
//
//   - pdfjs sci-notation.pdf: writes "/F1 1e2 Tf", and the Tf it lost made the
//     PDF/A-2b remediation of the page roll back its content repairs.
func TestParseStreamDataDropsOnlyTheMalformedOperand(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		src      string
		want     []string
		operands map[int][]ast.PdfObject
	}{
		{
			name: "exponent before its operator",
			src:  "BT /F1 1e2 Tf 50 600 Td (a) Tj ET",
			want: []string{"BT", "Tf", "Td", "Tj", "ET"},
			operands: map[int][]ast.PdfObject{
				1: {ast.PdfName("F1")},
				2: {ast.PdfInt(50), ast.PdfInt(600)},
			},
		},
		{
			name:     "exponent last before the operator",
			src:      "1 0 0 1 0 1e2 cm",
			want:     []string{"cm"},
			operands: map[int][]ast.PdfObject{0: {ast.PdfInt(1), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(0)}},
		},
		{
			name:     "stray delimiter between operands",
			src:      "10 ] 20 m",
			want:     []string{"m"},
			operands: map[int][]ast.PdfObject{0: {ast.PdfInt(10), ast.PdfInt(20)}},
		},
		{
			name: "exponent inside a TJ array",
			src:  "[(a) 1e2 (b)] TJ (c) Tj",
			want: []string{"TJ", "Tj"},
			operands: map[int][]ast.PdfObject{
				0: {ast.PdfArray{ast.PdfString("a"), ast.PdfString("b")}},
				1: {ast.PdfString("c")},
			},
		},
		{
			name: "exponent last in a TJ array",
			src:  "[(a) 1e2] TJ (c) Tj",
			want: []string{"TJ", "Tj"},
			operands: map[int][]ast.PdfObject{
				0: {ast.PdfArray{ast.PdfString("a")}},
			},
		},
		{
			name: "exponent in an inline image dictionary",
			src:  "BI /W 1e2 /H 2 ID \x00\x01 EI Q",
			want: []string{"BI", "Q"},
			operands: map[int][]ast.PdfObject{
				0: {ast.PdfDict{"H": ast.PdfInt(2)}},
			},
		},
		{
			name: "exponent right before ID",
			src:  "BI /H 2 /W 1e2 ID \x00\x01 EI (a) Tj",
			want: []string{"BI", "Tj"},
			operands: map[int][]ast.PdfObject{
				0: {ast.PdfDict{"H": ast.PdfInt(2)}},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ops, err := ParseStreamData(context.Background(), []byte(tc.src), SplitGluedOperators())
			if err != nil {
				t.Fatalf("ParseStreamData(%q): %v", tc.src, err)
			}
			if got := operators(ops); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseStreamData(%q) operators = %v, want %v", tc.src, got, tc.want)
			}
			for i, want := range tc.operands {
				if got := ops[i].Operands; !reflect.DeepEqual(got, want) {
					t.Errorf("ParseStreamData(%q) %s operands = %v, want %v", tc.src, ops[i].Operator, got, want)
				}
			}
		})
	}
}

// TestParseStreamDataWithOffsetsKeepsRangesAfterAMalformedOperand pins that the
// byte ranges stay parallel to the operations when an operand is dropped: the
// operator after it still closes its own range. The stream is read as PDF.js
// reads it, where "1e2" is one number the parser cannot read.
func TestParseStreamDataWithOffsetsKeepsRangesAfterAMalformedOperand(t *testing.T) {
	t.Parallel()

	src := "/F1 1e2 Tf (a) Tj"
	ops, ranges, err := ParseStreamDataWithOffsets(context.Background(), []byte(src), SplitGluedOperators())
	if err != nil {
		t.Fatalf("ParseStreamDataWithOffsets: %v", err)
	}
	if got := operators(ops); !reflect.DeepEqual(got, []string{"Tf", "Tj"}) {
		t.Fatalf("operators = %v, want [Tf Tj]", got)
	}
	if len(ranges) != len(ops) {
		t.Fatalf("got %d ranges for %d operations", len(ranges), len(ops))
	}
	if got := src[ranges[0].Start:ranges[0].End]; got != "/F1 1e2 Tf" {
		t.Errorf("Tf range covers %q, want %q", got, "/F1 1e2 Tf")
	}
	if got := src[ranges[1].Start:ranges[1].End]; got != " (a) Tj" {
		t.Errorf("Tj range covers %q, want %q", got, " (a) Tj")
	}
}
