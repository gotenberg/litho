package remediation

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// TestSerializeOpsRoundTripsEveryName pins that a content stream rewritten by
// [serializeOps] parses back to the operations it was written from, whatever
// bytes a name holds.
//
// The lexer decodes a name's "#" escapes (ISO 32000-1, 7.3.5), so a parsed name
// can hold white space, a delimiter, a "#" or a byte outside 0x21 to 0x7E.
// Written back raw, such a name ended early or read as an escape: "/Font#20A
// Tf" came back as the name Font, a keyword A and a Tf short an operand.
func TestSerializeOpsRoundTripsEveryName(t *testing.T) {
	t.Parallel()

	for _, name := range []ast.PdfName{
		"Font A",
		"F#1",
		"F(1)",
		"F[1]",
		"F<1>",
		"F{1}",
		"F/1",
		"F%1",
		"F\t1",
		"F\n1",
		"F\x7f1",
		"Fé1",
		"\xff",
		"F1",
	} {
		t.Run(string(name), func(t *testing.T) {
			t.Parallel()

			ops := []content.Operation{
				{Operator: "BT"},
				{Operator: "Tf", Operands: []ast.PdfObject{name, ast.PdfInt(12)}},
				{Operator: "BDC", Operands: []ast.PdfObject{name, ast.PdfDict{
					name:   ast.PdfArray{name, ast.PdfString("a")},
					"MCID": ast.PdfInt(0),
				}}},
				{Operator: "EMC"},
				{Operator: "ET"},
			}
			data, err := serializeOps(ops)
			if err != nil {
				t.Fatalf("serializeOps: %v", err)
			}
			got, err := content.ParseStreamData(context.Background(), data)
			if err != nil {
				t.Fatalf("parsing %q: %v", data, err)
			}
			if !sameOperations(got, ops) {
				t.Errorf("the rewrite %q parsed back to\n  %v\nwant\n  %v", data, got, ops)
			}
		})
	}
}

// TestRepairContentDataKeepsEscapedNames pins the round trip through a fixer
// that rewrites a page: stripping an undefined operator re-serialises every
// other operation, and the names the source escaped must come back as the same
// names, or the Tf selects no font and the gs no graphics state.
func TestRepairContentDataKeepsEscapedNames(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	src := []byte("/GS#20A gs BT /F#231 12 Tf /Span <</ActualText (x) /Lang#28 (en)>> BDC (a) Tj EMC ET junk")
	out, tally := repairContentData(ctx, src, 2)
	if tally.removed != 1 {
		t.Fatalf("removed %d undefined operators, want 1 (the rewrite did not run)", tally.removed)
	}

	before, err := content.ParseStreamData(ctx, src)
	if err != nil {
		t.Fatalf("parsing the source: %v", err)
	}
	after, err := content.ParseStreamData(ctx, out)
	if err != nil {
		t.Fatalf("parsing the rewrite %q: %v", out, err)
	}
	if !sameOperations(after, before[:len(before)-1]) {
		t.Errorf("the rewrite %q parsed back to\n  %v\nwant\n  %v", out, after, before[:len(before)-1])
	}
}

// sameOperations reports whether a and b hold the same operators with the same
// operands, an empty operand list matching a nil one.
func sameOperations(a, b []content.Operation) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Operator != b[i].Operator || len(a[i].Operands) != len(b[i].Operands) {
			return false
		}
		if len(a[i].Operands) > 0 && !reflect.DeepEqual(a[i].Operands, b[i].Operands) {
			return false
		}
	}
	return true
}

// TestSerializeOpsRefusesANameHoldingNull pins the one name no spelling
// carries: ISO 32000-1, 7.3.5 excludes NUL from every name, so the stream is
// refused rather than written with "#00", and a caller leaves it as it was.
func TestSerializeOpsRefusesANameHoldingNull(t *testing.T) {
	t.Parallel()

	for _, operand := range []ast.PdfObject{
		ast.PdfName("F\x001"),
		ast.PdfArray{ast.PdfName("F\x001")},
		ast.PdfDict{"F\x001": ast.PdfInt(1)},
	} {
		ops := []content.Operation{{Operator: "gs", Operands: []ast.PdfObject{operand}}}
		if data, err := serializeOps(ops); !errors.Is(err, writer.ErrNameHoldsNull) {
			t.Errorf("serializeOps(%v) = %q, %v; want %v", operand, data, err, writer.ErrNameHoldsNull)
		}
	}
}
