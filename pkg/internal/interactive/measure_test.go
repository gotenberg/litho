package interactive

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// makeNumberFormat builds a Number Format dictionary with /U, /C, and /D.
func makeNumberFormat(unit string, conversion float64, decimals int) ast.PdfDict {
	return ast.PdfDict{
		"U": ast.PdfString(unit),
		"C": ast.PdfReal(conversion),
		"D": ast.PdfInt(decimals),
	}
}

// makeRLMeasure builds a /Measure dict with /Subtype /RL and X/Y axes.
func makeRLMeasure(xUnit string, xConv float64, yUnit string, yConv float64) ast.PdfDict {
	m := ast.PdfDict{
		"Subtype": ast.PdfName("RL"),
		"X":       ast.PdfArray{makeNumberFormat(xUnit, xConv, 2)},
	}
	if yUnit != "" {
		m["Y"] = ast.PdfArray{makeNumberFormat(yUnit, yConv, 2)}
	}
	return m
}

func TestExtractMeasurements_ViewportRL(t *testing.T) {
	page := &document.Page{
		Dict: ast.PdfDict{
			"Type": ast.PdfName("Page"),
			"VP": ast.PdfArray{
				ast.PdfDict{
					"Type":    ast.PdfName("Viewport"),
					"BBox":    ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(612), ast.PdfInt(792)},
					"Name":    ast.PdfString("Floor Plan"),
					"Measure": makeRLMeasure("ft", 0.0138889, "ft", 0.0138889),
				},
			},
		},
	}

	results, err := ExtractMeasurements(context.Background(), nil, page)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 measurement, got %d", len(results))
	}

	m := results[0]
	if m.Source != "Viewport" {
		t.Errorf("Source = %q, want 'Viewport'", m.Source)
	}
	if m.Name != "Floor Plan" {
		t.Errorf("Name = %q, want 'Floor Plan'", m.Name)
	}
	if m.BBox != [4]float64{0, 0, 612, 792} {
		t.Errorf("BBox = %v, want [0 0 612 792]", m.BBox)
	}
	assertAxis(t, "ScaleX", m.ScaleX, "ft", 0.0138889, 2)
	assertAxis(t, "ScaleY", m.ScaleY, "ft", 0.0138889, 2)
}

func TestExtractMeasurements_ViewportYDefaultsToX(t *testing.T) {
	page := &document.Page{
		Dict: ast.PdfDict{
			"Type": ast.PdfName("Page"),
			"VP": ast.PdfArray{
				ast.PdfDict{
					"BBox":    ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(100), ast.PdfInt(100)},
					"Measure": makeRLMeasure("m", 0.000352778, "", 0),
				},
			},
		},
	}

	results, err := ExtractMeasurements(context.Background(), nil, page)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 measurement, got %d", len(results))
	}

	m := results[0]
	// Y should default to X when not specified.
	assertAxis(t, "ScaleX", m.ScaleX, "m", 0.000352778, 2)
	assertAxis(t, "ScaleY", m.ScaleY, "m", 0.000352778, 2)
}

func TestExtractMeasurements_LineAnnotation(t *testing.T) {
	page := &document.Page{
		Dict: ast.PdfDict{
			"Type": ast.PdfName("Page"),
			"Annots": ast.PdfArray{
				ast.PdfDict{
					"Subtype": ast.PdfName("Line"),
					"NM":      ast.PdfString("dim-1"),
					"Rect":    ast.PdfArray{ast.PdfReal(100), ast.PdfReal(200), ast.PdfReal(300), ast.PdfReal(400)},
					"Measure": makeRLMeasure("mm", 0.352778, "mm", 0.352778),
				},
			},
		},
	}

	results, err := ExtractMeasurements(context.Background(), nil, page)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 measurement, got %d", len(results))
	}

	m := results[0]
	if m.Source != "Line" {
		t.Errorf("Source = %q, want 'Line'", m.Source)
	}
	if m.Name != "dim-1" {
		t.Errorf("Name = %q, want 'dim-1'", m.Name)
	}
	if m.BBox != [4]float64{100, 200, 300, 400} {
		t.Errorf("BBox = %v, want [100 200 300 400]", m.BBox)
	}
	assertAxis(t, "ScaleX", m.ScaleX, "mm", 0.352778, 2)
}

func TestExtractMeasurements_PolyLineAnnotation(t *testing.T) {
	page := &document.Page{
		Dict: ast.PdfDict{
			"Type": ast.PdfName("Page"),
			"Annots": ast.PdfArray{
				ast.PdfDict{
					"Subtype": ast.PdfName("PolyLine"),
					"Rect":    ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(500), ast.PdfInt(500)},
					"Measure": makeRLMeasure("in", 0.0138889, "in", 0.0138889),
				},
			},
		},
	}

	results, err := ExtractMeasurements(context.Background(), nil, page)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 measurement, got %d", len(results))
	}
	if results[0].Source != "PolyLine" {
		t.Errorf("Source = %q, want 'PolyLine'", results[0].Source)
	}
}

func TestExtractMeasurements_SkipsNonRLSubtype(t *testing.T) {
	page := &document.Page{
		Dict: ast.PdfDict{
			"Type": ast.PdfName("Page"),
			"VP": ast.PdfArray{
				ast.PdfDict{
					"BBox": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(100), ast.PdfInt(100)},
					"Measure": ast.PdfDict{
						"Subtype": ast.PdfName("GEO"),
					},
				},
			},
		},
	}

	results, err := ExtractMeasurements(context.Background(), nil, page)
	if err != nil {
		t.Fatal(err)
	}
	if results != nil {
		t.Errorf("expected nil for non-RL measure, got %d results", len(results))
	}
}

func TestExtractMeasurements_SkipsNonMeasuredAnnotation(t *testing.T) {
	page := &document.Page{
		Dict: ast.PdfDict{
			"Type": ast.PdfName("Page"),
			"Annots": ast.PdfArray{
				ast.PdfDict{
					"Subtype": ast.PdfName("Line"),
					"Rect":    ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(100), ast.PdfInt(100)},
					// No /Measure
				},
				ast.PdfDict{
					"Subtype": ast.PdfName("Text"),
					"Rect":    ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(50), ast.PdfInt(50)},
					"Measure": makeRLMeasure("ft", 1.0, "ft", 1.0), // Text annots ignored
				},
			},
		},
	}

	results, err := ExtractMeasurements(context.Background(), nil, page)
	if err != nil {
		t.Fatal(err)
	}
	if results != nil {
		t.Errorf("expected nil, got %d results", len(results))
	}
}

func TestExtractMeasurements_NoVPOrAnnots(t *testing.T) {
	page := &document.Page{
		Dict: ast.PdfDict{
			"Type": ast.PdfName("Page"),
		},
	}

	results, err := ExtractMeasurements(context.Background(), nil, page)
	if err != nil {
		t.Fatal(err)
	}
	if results != nil {
		t.Errorf("expected nil for page with no VP or annotations, got %v", results)
	}
}

func TestExtractMeasurements_ViewportAndAnnotation(t *testing.T) {
	page := &document.Page{
		Dict: ast.PdfDict{
			"Type": ast.PdfName("Page"),
			"VP": ast.PdfArray{
				ast.PdfDict{
					"BBox":    ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(612), ast.PdfInt(792)},
					"Measure": makeRLMeasure("ft", 0.5, "ft", 0.5),
				},
			},
			"Annots": ast.PdfArray{
				ast.PdfDict{
					"Subtype": ast.PdfName("Line"),
					"Rect":    ast.PdfArray{ast.PdfInt(10), ast.PdfInt(20), ast.PdfInt(30), ast.PdfInt(40)},
					"Measure": makeRLMeasure("m", 0.001, "m", 0.001),
				},
			},
		},
	}

	results, err := ExtractMeasurements(context.Background(), nil, page)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 measurements, got %d", len(results))
	}
	if results[0].Source != "Viewport" {
		t.Errorf("first result Source = %q, want 'Viewport'", results[0].Source)
	}
	if results[1].Source != "Line" {
		t.Errorf("second result Source = %q, want 'Line'", results[1].Source)
	}
}

func TestExtractMeasurements_AreaUnit(t *testing.T) {
	measure := makeRLMeasure("ft", 0.0138889, "ft", 0.0138889)
	measure["A"] = ast.PdfArray{makeNumberFormat("sq ft", 0.000192901, 2)}

	page := &document.Page{
		Dict: ast.PdfDict{
			"Type": ast.PdfName("Page"),
			"VP": ast.PdfArray{
				ast.PdfDict{
					"BBox":    ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(612), ast.PdfInt(792)},
					"Measure": measure,
				},
			},
		},
	}

	results, err := ExtractMeasurements(context.Background(), nil, page)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 measurement, got %d", len(results))
	}
	if results[0].AreaUnit != "sq ft" {
		t.Errorf("AreaUnit = %q, want 'sq ft'", results[0].AreaUnit)
	}
}

func TestParseNumberFormatArray_Empty(t *testing.T) {
	axis := parseNumberFormatArray(context.Background(), nil, ast.PdfArray{})
	if axis.Unit != "" || axis.ConversionFactor != 0 {
		t.Errorf("expected zero axis for empty array, got %+v", axis)
	}
}

func TestParseNumberFormatArray_NonArray(t *testing.T) {
	axis := parseNumberFormatArray(context.Background(), nil, ast.PdfName("invalid"))
	if axis.Unit != "" {
		t.Errorf("expected zero axis for non-array, got %+v", axis)
	}
}

func assertAxis(t *testing.T, name string, got models.ScaleAxis, wantUnit string, wantConv float64, wantDec int) {
	t.Helper()
	if got.Unit != wantUnit {
		t.Errorf("%s.Unit = %q, want %q", name, got.Unit, wantUnit)
	}
	// Use a small epsilon for float comparison.
	diff := got.ConversionFactor - wantConv
	if diff < 0 {
		diff = -diff
	}
	if diff > 1e-9 {
		t.Errorf("%s.ConversionFactor = %g, want %g", name, got.ConversionFactor, wantConv)
	}
	if got.Decimals != wantDec {
		t.Errorf("%s.Decimals = %d, want %d", name, got.Decimals, wantDec)
	}
}
