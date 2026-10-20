package graphics

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

func TestPattern_CsSetsPatternColorSpace(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})

	ops := []content.Operation{
		{Operator: "cs", Operands: []ast.PdfObject{ast.PdfName("Pattern")}},
	}
	if err := m.Execute(context.Background(), ops); err != nil {
		t.Fatal(err)
	}

	state := m.Current()
	if state.Fill.Space.Family != FamilyPattern {
		t.Errorf("fill space = %q, want 'Pattern'", state.Fill.Space.Family)
	}
}

func TestPattern_ScnSetsTilingPattern(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.Resources = ast.PdfDict{
		"Pattern": ast.PdfDict{
			"P1": ast.NewStream(ast.PdfDict{
				"PatternType": ast.PdfInt(1),
				"PaintType":   ast.PdfInt(1),
				"TilingType":  ast.PdfInt(1),
				"BBox":        ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(10), ast.PdfInt(10)},
			}, nil),
		},
	}

	ops := []content.Operation{
		{Operator: "cs", Operands: []ast.PdfObject{ast.PdfName("Pattern")}},
		{Operator: "scn", Operands: []ast.PdfObject{ast.PdfName("P1")}},
	}
	if err := m.Execute(context.Background(), ops); err != nil {
		t.Fatal(err)
	}

	state := m.Current()
	if state.Fill.Pattern != "P1" {
		t.Errorf("Fill.Pattern = %q, want 'P1'", state.Fill.Pattern)
	}
	if state.Fill.PatternType != "Tiling" {
		t.Errorf("Fill.PatternType = %q, want 'Tiling'", state.Fill.PatternType)
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
	if err := m.Execute(context.Background(), ops); err != nil {
		t.Fatal(err)
	}

	state := m.Current()
	if state.Fill.Pattern != "P2" {
		t.Errorf("Fill.Pattern = %q, want 'P2'", state.Fill.Pattern)
	}
	if state.Fill.PatternType != "Shading" {
		t.Errorf("Fill.PatternType = %q, want 'Shading'", state.Fill.PatternType)
	}
}

func TestPattern_ScnWithUnderlyingColor(t *testing.T) {
	// An uncolored tiling pattern can carry underlying color components.
	// scn 0.5 0.3 0.8 /P1: three floats + pattern name.
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.Resources = ast.PdfDict{
		"Pattern": ast.PdfDict{
			"P1": ast.NewStream(ast.PdfDict{
				"PatternType": ast.PdfInt(1),
				"PaintType":   ast.PdfInt(2), // Uncolored
			}, nil),
		},
	}

	ops := []content.Operation{
		{Operator: "cs", Operands: []ast.PdfObject{ast.PdfName("Pattern")}},
		{Operator: "scn", Operands: []ast.PdfObject{
			ast.PdfReal(0.5), ast.PdfReal(0.3), ast.PdfReal(0.8),
			ast.PdfName("P1"),
		}},
	}
	if err := m.Execute(context.Background(), ops); err != nil {
		t.Fatal(err)
	}

	state := m.Current()
	if state.Fill.Pattern != "P1" {
		t.Errorf("Fill.Pattern = %q, want 'P1'", state.Fill.Pattern)
	}
	if state.Fill.PatternType != "Tiling" {
		t.Errorf("Fill.PatternType = %q, want 'Tiling'", state.Fill.PatternType)
	}
}

func TestPattern_CsNonPatternClearsState(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.Resources = ast.PdfDict{
		"Pattern": ast.PdfDict{
			"P1": ast.NewStream(ast.PdfDict{
				"PatternType": ast.PdfInt(1),
			}, nil),
		},
	}

	// First set Pattern color space and pattern.
	ops := []content.Operation{
		{Operator: "cs", Operands: []ast.PdfObject{ast.PdfName("Pattern")}},
		{Operator: "scn", Operands: []ast.PdfObject{ast.PdfName("P1")}},
	}
	if err := m.Execute(context.Background(), ops); err != nil {
		t.Fatal(err)
	}

	// Now switch to DeviceRGB. Should clear pattern fields.
	ops = []content.Operation{
		{Operator: "cs", Operands: []ast.PdfObject{ast.PdfName("DeviceRGB")}},
	}
	if err := m.Execute(context.Background(), ops); err != nil {
		t.Fatal(err)
	}

	state := m.Current()
	if state.Fill.Pattern != "" {
		t.Errorf("Fill.Pattern should be empty after switching to DeviceRGB, got %q", state.Fill.Pattern)
	}
	if state.Fill.PatternType != "" {
		t.Errorf("Fill.PatternType should be empty after switching to DeviceRGB, got %q", state.Fill.PatternType)
	}
}

func TestPattern_ScNonPatternClearsState(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.Resources = ast.PdfDict{
		"Pattern": ast.PdfDict{
			"P1": ast.NewStream(ast.PdfDict{
				"PatternType": ast.PdfInt(1),
			}, nil),
		},
	}

	// Set Pattern then set numeric color values. Should clear pattern fields.
	ops := []content.Operation{
		{Operator: "cs", Operands: []ast.PdfObject{ast.PdfName("Pattern")}},
		{Operator: "scn", Operands: []ast.PdfObject{ast.PdfName("P1")}},
		{Operator: "cs", Operands: []ast.PdfObject{ast.PdfName("DeviceGray")}},
		{Operator: "sc", Operands: []ast.PdfObject{ast.PdfReal(0.5)}},
	}
	if err := m.Execute(context.Background(), ops); err != nil {
		t.Fatal(err)
	}

	state := m.Current()
	if state.Fill.Pattern != "" {
		t.Errorf("Fill.Pattern should be empty, got %q", state.Fill.Pattern)
	}
	if state.Fill.PatternType != "" {
		t.Errorf("Fill.PatternType should be empty, got %q", state.Fill.PatternType)
	}
	if len(state.Fill.Components) != 1 || state.Fill.Components[0] != 0.5 {
		t.Errorf("fill components = %v, want [0.5]", state.Fill.Components)
	}
}

func TestPattern_NoResources_FallsBackToPattern(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	// No resources set. Pattern type should be "Pattern" (unknown).

	ops := []content.Operation{
		{Operator: "cs", Operands: []ast.PdfObject{ast.PdfName("Pattern")}},
		{Operator: "scn", Operands: []ast.PdfObject{ast.PdfName("UnknownPat")}},
	}
	if err := m.Execute(context.Background(), ops); err != nil {
		t.Fatal(err)
	}

	state := m.Current()
	if state.Fill.Pattern != "UnknownPat" {
		t.Errorf("Fill.Pattern = %q, want 'UnknownPat'", state.Fill.Pattern)
	}
	if state.Fill.PatternType != "Pattern" {
		t.Errorf("Fill.PatternType = %q, want 'Pattern'", state.Fill.PatternType)
	}
}

func TestPattern_SaveRestore(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.Resources = ast.PdfDict{
		"Pattern": ast.PdfDict{
			"P1": ast.NewStream(ast.PdfDict{
				"PatternType": ast.PdfInt(1),
			}, nil),
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
	if err := m.Execute(context.Background(), ops); err != nil {
		t.Fatal(err)
	}

	state := m.Current()
	// After Q, should restore to Pattern state.
	if state.Fill.PatternType != "Tiling" {
		t.Errorf("Fill.PatternType after restore = %q, want 'Tiling'", state.Fill.PatternType)
	}
	if state.Fill.Pattern != "P1" {
		t.Errorf("Fill.Pattern after restore = %q, want 'P1'", state.Fill.Pattern)
	}
}

func TestClassifyPattern_Type1(t *testing.T) {
	got := classifyPattern(context.Background(), nil, ast.PdfDict{"PatternType": ast.PdfInt(1)})
	if got != "Tiling" {
		t.Errorf("got %q, want 'Tiling'", got)
	}
}

func TestClassifyPattern_Type2(t *testing.T) {
	got := classifyPattern(context.Background(), nil, ast.PdfDict{"PatternType": ast.PdfInt(2)})
	if got != "Shading" {
		t.Errorf("got %q, want 'Shading'", got)
	}
}

func TestClassifyPattern_Unknown(t *testing.T) {
	got := classifyPattern(context.Background(), nil, ast.PdfDict{"PatternType": ast.PdfInt(99)})
	if got != "Pattern" {
		t.Errorf("got %q, want 'Pattern'", got)
	}
}

func TestClassifyPattern_NoPatternType(t *testing.T) {
	got := classifyPattern(context.Background(), nil, ast.PdfDict{})
	if got != "Pattern" {
		t.Errorf("got %q, want 'Pattern'", got)
	}
}

func TestPattern_DeviceOperatorClearsPattern(t *testing.T) {
	m := testMachine()
	m.Resources = ast.PdfDict{"Pattern": ast.PdfDict{
		"P1": ast.NewStream(ast.PdfDict{"PatternType": ast.PdfInt(1)}, nil),
	}}
	// rg selects DeviceRGB and the colour at once (ISO 32000-1, 8.6.8,
	// Table 74), so no pattern survives it.
	s := run(t, m,
		op("cs", ast.PdfName("Pattern")),
		op("scn", ast.PdfName("P1")),
		op("rg", num(1), num(1), num(1)),
	)
	if s.Fill.Pattern != "" || s.Fill.PatternType != "" || s.Fill.Space.Family != FamilyDeviceRGB {
		t.Errorf("fill after rg = %+v", s.Fill)
	}
}

func TestPattern_ResourceNamedPatternSpace(t *testing.T) {
	m := testMachine()
	m.Resources = ast.PdfDict{
		"ColorSpace": ast.PdfDict{"CS0": ast.PdfArray{ast.PdfName("Pattern"), ast.PdfName("DeviceRGB")}},
		"Pattern": ast.PdfDict{
			"P1": ast.NewStream(ast.PdfDict{"PatternType": ast.PdfInt(1), "PaintType": ast.PdfInt(2)}, nil),
		},
	}
	s := run(t, m,
		op("cs", ast.PdfName("CS0")),
		op("scn", num(0.1), num(0.2), num(0.3), ast.PdfName("P1")),
	)
	if s.Fill.Pattern != "P1" || s.Fill.PatternType != "Tiling" || len(s.Fill.Components) != 3 {
		t.Errorf("fill = %+v", s.Fill)
	}
}

func TestPattern_ReselectResolvesAgain(t *testing.T) {
	m := testMachine()
	m.Resources = ast.PdfDict{"Pattern": ast.PdfDict{
		"P1": ast.NewStream(ast.PdfDict{"PatternType": ast.PdfInt(1)}, nil),
	}}
	run(t, m, op("cs", ast.PdfName("Pattern")), op("scn", ast.PdfName("P1")))
	// The same name in other resources is another pattern.
	m.Resources = ast.PdfDict{"Pattern": ast.PdfDict{
		"P1": ast.PdfDict{"PatternType": ast.PdfInt(2)},
	}}
	s := run(t, m, op("scn", ast.PdfName("P1")))
	if s.Fill.PatternType != "Shading" {
		t.Errorf("PatternType = %q, want Shading", s.Fill.PatternType)
	}
	// Numbers alone in a Pattern space select nothing and keep the pattern.
	s = run(t, m, op("scn", num(0.5)))
	if s.Fill.Pattern != "P1" || s.Fill.PatternType != "Shading" {
		t.Errorf("fill after bare scn = %+v", s.Fill)
	}
}

func TestPattern_StrokeClassified(t *testing.T) {
	m := testMachine()
	m.Resources = ast.PdfDict{"Pattern": ast.PdfDict{
		"P2": ast.PdfDict{"PatternType": ast.PdfInt(2)},
	}}
	s := run(t, m, op("CS", ast.PdfName("Pattern")), op("SCN", ast.PdfName("P2")))
	if s.Stroke.Pattern != "P2" || s.Stroke.PatternType != "Shading" || s.Fill.Pattern != "" {
		t.Errorf("stroke = %+v fill = %+v", s.Stroke, s.Fill)
	}
}
