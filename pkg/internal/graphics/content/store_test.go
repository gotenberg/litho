package content

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestOperandsBelongToTheirOperation locks that the operands of each
// operation read as a slice of their own: an append to one copies rather
// than overwriting the operands of the next, a write to one changes no other
// operation, and an operation without operands has none.
func TestOperandsBelongToTheirOperation(t *testing.T) {
	ops, err := ParseStreamData(context.Background(), []byte("q 1 0 0 1 10 20 cm /F1 12 Tf (a) Tj Q"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 5 {
		t.Fatalf("got %d operations, want 5", len(ops))
	}
	if ops[0].Operands != nil || ops[4].Operands != nil {
		t.Errorf("operations without operands hold %#v and %#v, want nil", ops[0].Operands, ops[4].Operands)
	}
	if cap(ops[1].Operands) != len(ops[1].Operands) {
		t.Errorf("cm operands have capacity %d past their length %d", cap(ops[1].Operands), len(ops[1].Operands))
	}

	grown := append(ops[1].Operands, ast.PdfInt(99))
	grown[0] = ast.PdfInt(7)
	if ops[2].Operands[0] != ast.PdfName("F1") {
		t.Errorf("appending to cm operands changed Tf's to %#v", ops[2].Operands)
	}
	if ops[1].Operands[0] != ast.PdfInt(1) {
		t.Errorf("writing to the grown copy changed cm's operands to %#v", ops[1].Operands)
	}

	ops[2].Operands[1] = ast.PdfInt(24)
	if ops[3].Operands[0] != ast.PdfString("a") || ops[1].Operands[5] != ast.PdfInt(20) {
		t.Errorf("writing to Tf's operands changed its neighbours: %#v, %#v", ops[1].Operands, ops[3].Operands)
	}
}

// TestOperandsSpanBlocks locks that an operation whose operands run past the
// block they started in keeps them all, in order.
func TestOperandsSpanBlocks(t *testing.T) {
	var b strings.Builder
	for range 3 * maxOperandBlock {
		b.WriteString("1 ")
	}
	b.WriteString("n 2 3 m")
	ops, err := ParseStreamData(context.Background(), []byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 2 || len(ops[0].Operands) != 3*maxOperandBlock || len(ops[1].Operands) != 2 {
		t.Fatalf("got %d operations, the first with %d operands", len(ops), len(ops[0].Operands))
	}
	for i, o := range ops[0].Operands {
		if o != ast.PdfInt(1) {
			t.Fatalf("operand %d is %#v", i, o)
		}
	}
	if ops[1].Operands[0] != ast.PdfInt(2) || ops[1].Operands[1] != ast.PdfInt(3) {
		t.Errorf("m holds %#v", ops[1].Operands)
	}
}
