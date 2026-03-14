package graphics

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

func TestOCMD_AnyOn_Visible(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.ActiveOCs["Layer1"] = false
	m.ActiveOCs["Layer2"] = true

	ops := []content.Operation{
		{Operator: "BDC", Operands: []ast.PdfObject{
			ast.PdfName("OC"),
			ast.PdfDict{
				"Type": ast.PdfName("OCMD"),
				"OCGs": ast.PdfArray{ast.PdfName("Layer1"), ast.PdfName("Layer2")},
				"P":    ast.PdfName("AnyOn"),
			},
		}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	state := m.Current()
	if state.SkipExtraction {
		t.Error("expected visible: at least one layer is on (AnyOn)")
	}
}

func TestOCMD_AnyOn_Hidden(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.ActiveOCs["Layer1"] = false
	m.ActiveOCs["Layer2"] = false

	ops := []content.Operation{
		{Operator: "BDC", Operands: []ast.PdfObject{
			ast.PdfName("OC"),
			ast.PdfDict{
				"Type": ast.PdfName("OCMD"),
				"OCGs": ast.PdfArray{ast.PdfName("Layer1"), ast.PdfName("Layer2")},
			},
		}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	state := m.Current()
	if !state.SkipExtraction {
		t.Error("expected hidden: no layers are on (AnyOn default)")
	}
	if !m.MarkedContent[0].IsHiddenOC {
		t.Error("expected IsHiddenOC=true")
	}
}

func TestOCMD_AllOn_Visible(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.ActiveOCs["Layer1"] = true
	m.ActiveOCs["Layer2"] = true

	ops := []content.Operation{
		{Operator: "BDC", Operands: []ast.PdfObject{
			ast.PdfName("OC"),
			ast.PdfDict{
				"OCGs": ast.PdfArray{ast.PdfName("Layer1"), ast.PdfName("Layer2")},
				"P":    ast.PdfName("AllOn"),
			},
		}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	if m.Current().SkipExtraction {
		t.Error("expected visible: all layers are on (AllOn)")
	}
}

func TestOCMD_AllOn_Hidden(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.ActiveOCs["Layer1"] = true
	m.ActiveOCs["Layer2"] = false

	ops := []content.Operation{
		{Operator: "BDC", Operands: []ast.PdfObject{
			ast.PdfName("OC"),
			ast.PdfDict{
				"OCGs": ast.PdfArray{ast.PdfName("Layer1"), ast.PdfName("Layer2")},
				"P":    ast.PdfName("AllOn"),
			},
		}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	if !m.Current().SkipExtraction {
		t.Error("expected hidden: not all layers are on (AllOn)")
	}
}

func TestOCMD_AnyOff_Visible(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.ActiveOCs["Layer1"] = true
	m.ActiveOCs["Layer2"] = false

	ops := []content.Operation{
		{Operator: "BDC", Operands: []ast.PdfObject{
			ast.PdfName("OC"),
			ast.PdfDict{
				"OCGs": ast.PdfArray{ast.PdfName("Layer1"), ast.PdfName("Layer2")},
				"P":    ast.PdfName("AnyOff"),
			},
		}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	if m.Current().SkipExtraction {
		t.Error("expected visible: at least one layer is off (AnyOff)")
	}
}

func TestOCMD_AnyOff_Hidden(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.ActiveOCs["Layer1"] = true
	m.ActiveOCs["Layer2"] = true

	ops := []content.Operation{
		{Operator: "BDC", Operands: []ast.PdfObject{
			ast.PdfName("OC"),
			ast.PdfDict{
				"OCGs": ast.PdfArray{ast.PdfName("Layer1"), ast.PdfName("Layer2")},
				"P":    ast.PdfName("AnyOff"),
			},
		}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	if !m.Current().SkipExtraction {
		t.Error("expected hidden: no layers are off (AnyOff)")
	}
}

func TestOCMD_AllOff_Visible(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.ActiveOCs["Layer1"] = false
	m.ActiveOCs["Layer2"] = false

	ops := []content.Operation{
		{Operator: "BDC", Operands: []ast.PdfObject{
			ast.PdfName("OC"),
			ast.PdfDict{
				"OCGs": ast.PdfArray{ast.PdfName("Layer1"), ast.PdfName("Layer2")},
				"P":    ast.PdfName("AllOff"),
			},
		}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	if m.Current().SkipExtraction {
		t.Error("expected visible: all layers are off (AllOff)")
	}
}

func TestOCMD_AllOff_Hidden(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.ActiveOCs["Layer1"] = false
	m.ActiveOCs["Layer2"] = true

	ops := []content.Operation{
		{Operator: "BDC", Operands: []ast.PdfObject{
			ast.PdfName("OC"),
			ast.PdfDict{
				"OCGs": ast.PdfArray{ast.PdfName("Layer1"), ast.PdfName("Layer2")},
				"P":    ast.PdfName("AllOff"),
			},
		}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	if !m.Current().SkipExtraction {
		t.Error("expected hidden: not all layers are off (AllOff)")
	}
}

func TestOCMD_DefaultPolicy_AnyOn(t *testing.T) {
	// No /P key → defaults to AnyOn.
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.ActiveOCs["Layer1"] = true

	ops := []content.Operation{
		{Operator: "BDC", Operands: []ast.PdfObject{
			ast.PdfName("OC"),
			ast.PdfDict{
				"OCGs": ast.PdfArray{ast.PdfName("Layer1")},
			},
		}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	if m.Current().SkipExtraction {
		t.Error("expected visible: default AnyOn with one visible layer")
	}
}

func TestOCMD_SingleName(t *testing.T) {
	// /OCGs as a single PdfName rather than an array.
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.ActiveOCs["Layer1"] = false

	ops := []content.Operation{
		{Operator: "BDC", Operands: []ast.PdfObject{
			ast.PdfName("OC"),
			ast.PdfDict{
				"OCGs": ast.PdfName("Layer1"),
				"P":    ast.PdfName("AnyOn"),
			},
		}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	if !m.Current().SkipExtraction {
		t.Error("expected hidden: single layer is off (AnyOn)")
	}
}

func TestOCMD_UnknownLayers_DefaultVisible(t *testing.T) {
	// Layers not in ActiveOCs are skipped → defaults to visible.
	m := NewMachine([4]float64{0, 0, 612, 792})

	ops := []content.Operation{
		{Operator: "BDC", Operands: []ast.PdfObject{
			ast.PdfName("OC"),
			ast.PdfDict{
				"OCGs": ast.PdfArray{ast.PdfName("UnknownLayer")},
				"P":    ast.PdfName("AllOn"),
			},
		}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	if m.Current().SkipExtraction {
		t.Error("expected visible: unknown layers default to visible")
	}
}

func TestOCMD_MalformedDict_DefaultVisible(t *testing.T) {
	// No /OCGs key → default visible.
	m := NewMachine([4]float64{0, 0, 612, 792})

	ops := []content.Operation{
		{Operator: "BDC", Operands: []ast.PdfObject{
			ast.PdfName("OC"),
			ast.PdfDict{
				"P": ast.PdfName("AllOff"),
			},
		}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}

	if m.Current().SkipExtraction {
		t.Error("expected visible: malformed OCMD without /OCGs defaults to visible")
	}
}

func TestOCMD_EMC_RestoresVisibility(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.ActiveOCs["Layer1"] = false

	ops := []content.Operation{
		{Operator: "BDC", Operands: []ast.PdfObject{
			ast.PdfName("OC"),
			ast.PdfDict{
				"OCGs": ast.PdfName("Layer1"),
			},
		}},
	}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}
	if !m.Current().SkipExtraction {
		t.Fatal("expected hidden before EMC")
	}

	ops = []content.Operation{{Operator: "EMC"}}
	if err := m.Execute(ops); err != nil {
		t.Fatal(err)
	}
	if m.Current().SkipExtraction {
		t.Error("expected SkipExtraction=false after EMC")
	}
}
