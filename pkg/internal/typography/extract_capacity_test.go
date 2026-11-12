package typography

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

func TestShownCodeBoundCountsShownBytes(t *testing.T) {
	str := func(s string) ast.PdfObject { return ast.PdfString(s) }
	ops := []content.Operation{
		{Operator: "Tj", Operands: []ast.PdfObject{str(strings.Repeat("a", 40))}},
		{Operator: "'", Operands: []ast.PdfObject{str(strings.Repeat("b", 30))}},
		{Operator: "\"", Operands: []ast.PdfObject{ast.PdfInt(1), ast.PdfInt(2), str(strings.Repeat("c", 20))}},
		{Operator: "TJ", Operands: []ast.PdfObject{ast.PdfArray{str("dd"), ast.PdfInt(-120), str("eee")}}},
		{Operator: "Tj"},
		{Operator: "re", Operands: []ast.PdfObject{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(1)}},
	}
	if got, want := shownCodeBound(ops), 40+30+20+5; got != want {
		t.Errorf("shownCodeBound = %d, want %d", got, want)
	}
}

func TestShownCodeBoundStaysWithinItsBounds(t *testing.T) {
	if got := shownCodeBound(nil); got != minElementCapacity {
		t.Errorf("shownCodeBound(nil) = %d, want %d", got, minElementCapacity)
	}
	long := ast.PdfString(strings.Repeat("x", maxElementCapacity))
	ops := []content.Operation{
		{Operator: "Tj", Operands: []ast.PdfObject{long}},
		{Operator: "Tj", Operands: []ast.PdfObject{long}},
	}
	if got := shownCodeBound(ops); got != maxElementCapacity {
		t.Errorf("shownCodeBound = %d, want %d", got, maxElementCapacity)
	}
}
