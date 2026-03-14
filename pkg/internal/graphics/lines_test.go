package graphics

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/models"
)

func assertLine(t *testing.T, l models.Line, x1, y1, x2, y2 float64) {
	t.Helper()
	const tol = 0.01
	if math.Abs(l.X1-x1) > tol || math.Abs(l.Y1-y1) > tol || math.Abs(l.X2-x2) > tol || math.Abs(l.Y2-y2) > tol {
		t.Errorf("line mismatch: got (%.1f,%.1f)→(%.1f,%.1f), want (%.1f,%.1f)→(%.1f,%.1f)",
			l.X1, l.Y1, l.X2, l.Y2, x1, y1, x2, y2)
	}
}

func TestExtractLines_StrokedPath(t *testing.T) {
	ops := []content.Operation{
		{Operator: "m", Operands: []ast.PdfObject{ast.PdfInt(10), ast.PdfInt(20)}},
		{Operator: "l", Operands: []ast.PdfObject{ast.PdfInt(100), ast.PdfInt(20)}},
		{Operator: "l", Operands: []ast.PdfObject{ast.PdfInt(100), ast.PdfInt(50)}},
		{Operator: "S"},
	}

	lines := ExtractLines(ops)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	assertLine(t, lines[0], 10, 20, 100, 20)
	assertLine(t, lines[1], 100, 20, 100, 50)
}

func TestExtractLines_Rectangle(t *testing.T) {
	ops := []content.Operation{
		{Operator: "re", Operands: []ast.PdfObject{
			ast.PdfInt(50), ast.PdfInt(60), ast.PdfInt(100), ast.PdfInt(40),
		}},
		{Operator: "S"},
	}

	lines := ExtractLines(ops)
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d", len(lines))
	}
	assertLine(t, lines[0], 50, 60, 150, 60)
	assertLine(t, lines[1], 150, 60, 150, 100)
	assertLine(t, lines[2], 150, 100, 50, 100)
	assertLine(t, lines[3], 50, 100, 50, 60)
}

func TestExtractLines_FillDiscarded(t *testing.T) {
	ops := []content.Operation{
		{Operator: "m", Operands: []ast.PdfObject{ast.PdfInt(0), ast.PdfInt(0)}},
		{Operator: "l", Operands: []ast.PdfObject{ast.PdfInt(100), ast.PdfInt(0)}},
		{Operator: "f"},
	}

	lines := ExtractLines(ops)
	if len(lines) != 0 {
		t.Fatalf("expected 0 lines for fill-only path, got %d", len(lines))
	}
}

func TestExtractLines_CloseAndStroke(t *testing.T) {
	ops := []content.Operation{
		{Operator: "m", Operands: []ast.PdfObject{ast.PdfInt(0), ast.PdfInt(0)}},
		{Operator: "l", Operands: []ast.PdfObject{ast.PdfInt(100), ast.PdfInt(0)}},
		{Operator: "l", Operands: []ast.PdfObject{ast.PdfInt(100), ast.PdfInt(100)}},
		{Operator: "s"},
	}

	lines := ExtractLines(ops)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	assertLine(t, lines[0], 0, 0, 100, 0)
	assertLine(t, lines[1], 100, 0, 100, 100)
	assertLine(t, lines[2], 100, 100, 0, 0)
}

func TestExtractLines_CTMTransform(t *testing.T) {
	// CTM scales X by 2: cm 2 0 0 1 0 0
	ops := []content.Operation{
		{Operator: "cm", Operands: []ast.PdfObject{
			ast.PdfInt(2), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(0), ast.PdfInt(0),
		}},
		{Operator: "m", Operands: []ast.PdfObject{ast.PdfInt(10), ast.PdfInt(20)}},
		{Operator: "l", Operands: []ast.PdfObject{ast.PdfInt(50), ast.PdfInt(20)}},
		{Operator: "S"},
	}

	lines := ExtractLines(ops)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	assertLine(t, lines[0], 20, 20, 100, 20)
}

func TestExtractLines_QRestoresCTM(t *testing.T) {
	// q / cm 2 0 0 1 0 0 / Q → CTM should be identity again
	ops := []content.Operation{
		{Operator: "q"},
		{Operator: "cm", Operands: []ast.PdfObject{
			ast.PdfInt(2), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(0), ast.PdfInt(0),
		}},
		{Operator: "Q"},
		{Operator: "m", Operands: []ast.PdfObject{ast.PdfInt(10), ast.PdfInt(20)}},
		{Operator: "l", Operands: []ast.PdfObject{ast.PdfInt(50), ast.PdfInt(20)}},
		{Operator: "S"},
	}

	lines := ExtractLines(ops)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	// CTM was restored to identity, so coordinates should be unchanged.
	assertLine(t, lines[0], 10, 20, 50, 20)
}

func TestExtractLines_EndPathDiscards(t *testing.T) {
	// n (end path) should discard accumulated segments.
	ops := []content.Operation{
		{Operator: "m", Operands: []ast.PdfObject{ast.PdfInt(0), ast.PdfInt(0)}},
		{Operator: "l", Operands: []ast.PdfObject{ast.PdfInt(100), ast.PdfInt(0)}},
		{Operator: "n"},
	}

	lines := ExtractLines(ops)
	if len(lines) != 0 {
		t.Fatalf("expected 0 lines after end-path, got %d", len(lines))
	}
}
