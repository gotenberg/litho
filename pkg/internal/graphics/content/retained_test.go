package content

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestRetainedBytesGrowsWithOperands pins that [RetainedBytes]
// charges for what an operand holds, nested arrays and dictionaries included,
// so a larger form takes a larger share of the cache bound.
func TestRetainedBytesGrowsWithOperands(t *testing.T) {
	t.Parallel()

	small := []Operation{{Operator: "TJ", Operands: []ast.PdfObject{ast.PdfArray{ast.PdfString("a")}}}}
	large := []Operation{{Operator: "TJ", Operands: []ast.PdfObject{ast.PdfArray{
		ast.PdfString(strings.Repeat("a", 1000)),
		ast.PdfArray{ast.PdfDict{"K": ast.PdfString(strings.Repeat("b", 1000))}},
	}}}}
	s, l := RetainedBytes(small), RetainedBytes(large)
	if s <= 0 || l < s+2000 {
		t.Errorf("RetainedBytes = %d for one byte of string, %d for 2000 nested bytes", s, l)
	}
}
