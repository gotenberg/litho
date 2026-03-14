package graphics

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

func TestPattern_CsSetsPatternColorSpace(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})

	ops := []content.Operation{
		{Operator: "cs", Operands: []ast.PdfObject{ast.PdfName("Pattern")}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	state := m.Current()
	if state.FillColorSpace != "Pattern" {
		t.Errorf("FillColorSpace = %q, want 'Pattern'", state.FillColorSpace)
	}
}

func TestPattern_ScnSetsTilingPattern(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.Resources = ast.PdfDict{
		"Pattern": ast.PdfDict{
			"P1": ast.PdfStream{
				Dict: ast.PdfDict{
					"PatternType": ast.PdfInt(1),
					"PaintType":   ast.PdfInt(1),
					"TilingType":  ast.PdfInt(1),
					"BBox":        ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(10), ast.PdfInt(10)},
				},
			},
		},
	}

	ops := []content.Operation{
		{Operator: "cs", Operands: []ast.PdfObject{ast.PdfName("Pattern")}},
		{Operator: "scn", Operands: []ast.PdfObject{ast.PdfName("P1")}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	state := m.Current()
	if state.FillPatternName != "P1" {
		t.Errorf("FillPatternName = %q, want 'P1'", state.FillPatternName)
	}
	if state.FillPatternType != "Tiling" {
		t.Errorf("FillPatternType = %q, want 'Tiling'", state.FillPatternType)
	}
}

func TestPattern_ScnSetsShadingPattern(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.Resources = ast.PdfDict{
		"Pattern": ast.PdfDict{
			"P2": ast.PdfDict{
				"PatternType": ast.PdfInt(2),
				"Shading": ast.PdfDict{
					"ShadingType": ast.PdfInt(2),
					"ColorSpace":  ast.PdfName("DeviceRGB"),
				},
			},
		},
	}

	ops := []content.Operation{
		{Operator: "cs", Operands: []ast.PdfObject{ast.PdfName("Pattern")}},
		{Operator: "scn", Operands: []ast.PdfObject{ast.PdfName("P2")}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	state := m.Current()
	if state.FillPatternName != "P2" {
		t.Errorf("FillPatternName = %q, want 'P2'", state.FillPatternName)
	}
	if state.FillPatternType != "Shading" {
		t.Errorf("FillPatternType = %q, want 'Shading'", state.FillPatternType)
	}
}

func TestPattern_ScnWithUnderlyingColor(t *testing.T) {
	// An uncolored tiling pattern can carry underlying color components.
	// scn 0.5 0.3 0.8 /P1 — three floats + pattern name.
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.Resources = ast.PdfDict{
		"Pattern": ast.PdfDict{
			"P1": ast.PdfStream{
				Dict: ast.PdfDict{
					"PatternType": ast.PdfInt(1),
					"PaintType":   ast.PdfInt(2), // Uncolored
				},
			},
		},
	}

	ops := []content.Operation{
		{Operator: "cs", Operands: []ast.PdfObject{ast.PdfName("Pattern")}},
		{Operator: "scn", Operands: []ast.PdfObject{
			ast.PdfReal(0.5), ast.PdfReal(0.3), ast.PdfReal(0.8),
			ast.PdfName("P1"),
		}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	state := m.Current()
	if state.FillPatternName != "P1" {
		t.Errorf("FillPatternName = %q, want 'P1'", state.FillPatternName)
	}
	if state.FillPatternType != "Tiling" {
		t.Errorf("FillPatternType = %q, want 'Tiling'", state.FillPatternType)
	}
}

func TestPattern_CsNonPatternClearsState(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.Resources = ast.PdfDict{
		"Pattern": ast.PdfDict{
			"P1": ast.PdfStream{
				Dict: ast.PdfDict{
					"PatternType": ast.PdfInt(1),
				},
			},
		},
	}

	// First set Pattern color space and pattern.
	ops := []content.Operation{
		{Operator: "cs", Operands: []ast.PdfObject{ast.PdfName("Pattern")}},
		{Operator: "scn", Operands: []ast.PdfObject{ast.PdfName("P1")}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	// Now switch to DeviceRGB — should clear pattern fields.
	ops = []content.Operation{
		{Operator: "cs", Operands: []ast.PdfObject{ast.PdfName("DeviceRGB")}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	state := m.Current()
	if state.FillPatternName != "" {
		t.Errorf("FillPatternName should be empty after switching to DeviceRGB, got %q", state.FillPatternName)
	}
	if state.FillPatternType != "" {
		t.Errorf("FillPatternType should be empty after switching to DeviceRGB, got %q", state.FillPatternType)
	}
}

func TestPattern_ScNonPatternClearsState(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.Resources = ast.PdfDict{
		"Pattern": ast.PdfDict{
			"P1": ast.PdfStream{
				Dict: ast.PdfDict{
					"PatternType": ast.PdfInt(1),
				},
			},
		},
	}

	// Set Pattern then set numeric color values — should clear pattern fields.
	ops := []content.Operation{
		{Operator: "cs", Operands: []ast.PdfObject{ast.PdfName("Pattern")}},
		{Operator: "scn", Operands: []ast.PdfObject{ast.PdfName("P1")}},
		{Operator: "cs", Operands: []ast.PdfObject{ast.PdfName("DeviceGray")}},
		{Operator: "sc", Operands: []ast.PdfObject{ast.PdfReal(0.5)}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	state := m.Current()
	if state.FillPatternName != "" {
		t.Errorf("FillPatternName should be empty, got %q", state.FillPatternName)
	}
	if state.FillPatternType != "" {
		t.Errorf("FillPatternType should be empty, got %q", state.FillPatternType)
	}
	if len(state.FillColor) != 1 || state.FillColor[0] != 0.5 {
		t.Errorf("FillColor = %v, want [0.5]", state.FillColor)
	}
}

func TestPattern_NoResources_FallsBackToPattern(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	// No resources set — pattern type should be "Pattern" (unknown).

	ops := []content.Operation{
		{Operator: "cs", Operands: []ast.PdfObject{ast.PdfName("Pattern")}},
		{Operator: "scn", Operands: []ast.PdfObject{ast.PdfName("UnknownPat")}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	state := m.Current()
	if state.FillPatternName != "UnknownPat" {
		t.Errorf("FillPatternName = %q, want 'UnknownPat'", state.FillPatternName)
	}
	if state.FillPatternType != "Pattern" {
		t.Errorf("FillPatternType = %q, want 'Pattern'", state.FillPatternType)
	}
}

func TestPattern_SaveRestore(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.Resources = ast.PdfDict{
		"Pattern": ast.PdfDict{
			"P1": ast.PdfStream{
				Dict: ast.PdfDict{
					"PatternType": ast.PdfInt(1),
				},
			},
		},
	}

	ops := []content.Operation{
		{Operator: "cs", Operands: []ast.PdfObject{ast.PdfName("Pattern")}},
		{Operator: "scn", Operands: []ast.PdfObject{ast.PdfName("P1")}},
		{Operator: "q"},
		{Operator: "cs", Operands: []ast.PdfObject{ast.PdfName("DeviceRGB")}},
		{Operator: "sc", Operands: []ast.PdfObject{ast.PdfReal(1), ast.PdfReal(0), ast.PdfReal(0)}},
		{Operator: "Q"},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	state := m.Current()
	// After Q, should restore to Pattern state.
	if state.FillPatternType != "Tiling" {
		t.Errorf("FillPatternType after restore = %q, want 'Tiling'", state.FillPatternType)
	}
	if state.FillPatternName != "P1" {
		t.Errorf("FillPatternName after restore = %q, want 'P1'", state.FillPatternName)
	}
}

func TestClassifyPattern_Type1(t *testing.T) {
	got := classifyPattern(nil, ast.PdfDict{"PatternType": ast.PdfInt(1)})
	if got != "Tiling" {
		t.Errorf("got %q, want 'Tiling'", got)
	}
}

func TestClassifyPattern_Type2(t *testing.T) {
	got := classifyPattern(nil, ast.PdfDict{"PatternType": ast.PdfInt(2)})
	if got != "Shading" {
		t.Errorf("got %q, want 'Shading'", got)
	}
}

func TestClassifyPattern_Unknown(t *testing.T) {
	got := classifyPattern(nil, ast.PdfDict{"PatternType": ast.PdfInt(99)})
	if got != "Pattern" {
		t.Errorf("got %q, want 'Pattern'", got)
	}
}

func TestClassifyPattern_NoPatternType(t *testing.T) {
	got := classifyPattern(nil, ast.PdfDict{})
	if got != "Pattern" {
		t.Errorf("got %q, want 'Pattern'", got)
	}
}
