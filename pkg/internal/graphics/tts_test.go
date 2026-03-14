package graphics

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

func TestBDC_LangTag(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})

	ops := []content.Operation{
		{Operator: "BDC", Operands: []ast.PdfObject{
			ast.PdfName("Span"),
			ast.PdfDict{
				"Lang": ast.PdfString("fr-FR"),
			},
		}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	if len(m.MarkedContent) != 1 {
		t.Fatalf("expected 1 marked content entry, got %d", len(m.MarkedContent))
	}
	if m.MarkedContent[0].Language != "fr-FR" {
		t.Errorf("expected Language='fr-FR', got %q", m.MarkedContent[0].Language)
	}
}

func TestBDC_ExpansionTag(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})

	ops := []content.Operation{
		{Operator: "BDC", Operands: []ast.PdfObject{
			ast.PdfName("Span"),
			ast.PdfDict{
				"E": ast.PdfString("World Wide Web"),
			},
		}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	if len(m.MarkedContent) != 1 {
		t.Fatalf("expected 1 marked content entry, got %d", len(m.MarkedContent))
	}
	if m.MarkedContent[0].ExpansionText != "World Wide Web" {
		t.Errorf("expected ExpansionText='World Wide Web', got %q", m.MarkedContent[0].ExpansionText)
	}
}

func TestBDC_LangAndExpansion(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})

	ops := []content.Operation{
		{Operator: "BDC", Operands: []ast.PdfObject{
			ast.PdfName("Span"),
			ast.PdfDict{
				"Lang": ast.PdfString("en-US"),
				"E":    ast.PdfString("National Aeronautics and Space Administration"),
				"MCID": ast.PdfInt(42),
			},
		}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	mc := m.MarkedContent[0]
	if mc.Language != "en-US" {
		t.Errorf("expected Language='en-US', got %q", mc.Language)
	}
	if mc.ExpansionText != "National Aeronautics and Space Administration" {
		t.Errorf("expected ExpansionText, got %q", mc.ExpansionText)
	}
	if mc.MCID != 42 {
		t.Errorf("expected MCID=42, got %d", mc.MCID)
	}
}

func TestBDC_NestedLangInheritance(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})

	ops := []content.Operation{
		// Outer: set language to German
		{Operator: "BDC", Operands: []ast.PdfObject{
			ast.PdfName("Span"),
			ast.PdfDict{
				"Lang": ast.PdfString("de-DE"),
			},
		}},
		// Inner: set expansion but no language — should inherit outer Lang
		{Operator: "BDC", Operands: []ast.PdfObject{
			ast.PdfName("Span"),
			ast.PdfDict{
				"E": ast.PdfString("Bundesrepublik Deutschland"),
			},
		}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	if len(m.MarkedContent) != 2 {
		t.Fatalf("expected 2 marked content entries, got %d", len(m.MarkedContent))
	}

	// Inner entry has no language of its own
	if m.MarkedContent[1].Language != "" {
		t.Errorf("inner entry should have empty Language, got %q", m.MarkedContent[1].Language)
	}
	// But the outer entry has it — the text extraction walk-back should find it
	if m.MarkedContent[0].Language != "de-DE" {
		t.Errorf("outer entry Language should be 'de-DE', got %q", m.MarkedContent[0].Language)
	}
}

func TestBDC_EMCClearsLang(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})

	ops := []content.Operation{
		{Operator: "BDC", Operands: []ast.PdfObject{
			ast.PdfName("Span"),
			ast.PdfDict{
				"Lang": ast.PdfString("ja-JP"),
			},
		}},
		{Operator: "EMC"},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	if len(m.MarkedContent) != 0 {
		t.Errorf("expected empty MarkedContent after EMC, got %d entries", len(m.MarkedContent))
	}
}

func TestBDC_NoLangOrExpansion(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})

	ops := []content.Operation{
		{Operator: "BDC", Operands: []ast.PdfObject{
			ast.PdfName("Span"),
			ast.PdfDict{
				"MCID": ast.PdfInt(7),
			},
		}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	mc := m.MarkedContent[0]
	if mc.Language != "" {
		t.Errorf("expected empty Language, got %q", mc.Language)
	}
	if mc.ExpansionText != "" {
		t.Errorf("expected empty ExpansionText, got %q", mc.ExpansionText)
	}
}

func TestBDC_NestedLangOverride(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})

	ops := []content.Operation{
		{Operator: "BDC", Operands: []ast.PdfObject{
			ast.PdfName("Span"),
			ast.PdfDict{
				"Lang": ast.PdfString("en-US"),
			},
		}},
		{Operator: "BDC", Operands: []ast.PdfObject{
			ast.PdfName("Span"),
			ast.PdfDict{
				"Lang": ast.PdfString("es-ES"),
			},
		}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	// Inner has its own lang
	if m.MarkedContent[1].Language != "es-ES" {
		t.Errorf("expected inner Language='es-ES', got %q", m.MarkedContent[1].Language)
	}
	// Outer still has its lang
	if m.MarkedContent[0].Language != "en-US" {
		t.Errorf("expected outer Language='en-US', got %q", m.MarkedContent[0].Language)
	}
}
