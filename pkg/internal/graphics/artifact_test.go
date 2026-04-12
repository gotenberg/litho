package graphics

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

func TestArtifactBMC_Stripped(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.StripArtifacts = true

	ops := []content.Operation{
		{Operator: "BMC", Operands: []ast.PdfObject{ast.PdfName("Artifact")}},
	}
	if err := m.Execute(context.Background(), ops); err != nil {
		t.Fatal(err)
	}

	state := m.Current()
	if !state.SkipExtraction {
		t.Error("expected SkipExtraction=true inside /Artifact BMC when StripArtifacts enabled")
	}
	if len(m.MarkedContent) != 1 || !m.MarkedContent[0].IsArtifact {
		t.Error("expected MarkedContent[0].IsArtifact=true")
	}

	// EMC should restore SkipExtraction to false.
	ops = []content.Operation{
		{Operator: "EMC"},
	}
	if err := m.Execute(context.Background(), ops); err != nil {
		t.Fatal(err)
	}
	state = m.Current()
	if state.SkipExtraction {
		t.Error("expected SkipExtraction=false after EMC")
	}
}

func TestArtifactBDC_Stripped(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.StripArtifacts = true

	ops := []content.Operation{
		{Operator: "BDC", Operands: []ast.PdfObject{
			ast.PdfName("Artifact"),
			ast.PdfDict{"Type": ast.PdfName("Pagination")},
		}},
	}
	if err := m.Execute(context.Background(), ops); err != nil {
		t.Fatal(err)
	}

	state := m.Current()
	if !state.SkipExtraction {
		t.Error("expected SkipExtraction=true inside /Artifact BDC when StripArtifacts enabled")
	}
}

func TestArtifactBMC_NotStrippedWhenDisabled(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.StripArtifacts = false

	ops := []content.Operation{
		{Operator: "BMC", Operands: []ast.PdfObject{ast.PdfName("Artifact")}},
	}
	if err := m.Execute(context.Background(), ops); err != nil {
		t.Fatal(err)
	}

	state := m.Current()
	if state.SkipExtraction {
		t.Error("expected SkipExtraction=false when StripArtifacts is disabled")
	}
	if !m.MarkedContent[0].IsArtifact {
		t.Error("expected IsArtifact=true regardless of StripArtifacts setting")
	}
}

func TestArtifactNested_RestoresCorrectly(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.StripArtifacts = true

	// Start a non-artifact block, then nest an artifact inside.
	ops := []content.Operation{
		{Operator: "BMC", Operands: []ast.PdfObject{ast.PdfName("P")}},
		{Operator: "BMC", Operands: []ast.PdfObject{ast.PdfName("Artifact")}},
	}
	if err := m.Execute(context.Background(), ops); err != nil {
		t.Fatal(err)
	}
	state := m.Current()
	if !state.SkipExtraction {
		t.Error("expected SkipExtraction=true inside nested Artifact")
	}

	// Pop the artifact block.
	ops = []content.Operation{
		{Operator: "EMC"},
	}
	if err := m.Execute(context.Background(), ops); err != nil {
		t.Fatal(err)
	}
	state = m.Current()
	if state.SkipExtraction {
		t.Error("expected SkipExtraction=false after popping Artifact, non-artifact parent remains")
	}

	// Pop the non-artifact block.
	ops = []content.Operation{
		{Operator: "EMC"},
	}
	if err := m.Execute(context.Background(), ops); err != nil {
		t.Fatal(err)
	}
	if len(m.MarkedContent) != 0 {
		t.Errorf("expected empty MarkedContent stack, got %d", len(m.MarkedContent))
	}
}

func TestNonArtifactTag_NotSkipped(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.StripArtifacts = true

	ops := []content.Operation{
		{Operator: "BMC", Operands: []ast.PdfObject{ast.PdfName("P")}},
	}
	if err := m.Execute(context.Background(), ops); err != nil {
		t.Fatal(err)
	}

	state := m.Current()
	if state.SkipExtraction {
		t.Error("expected SkipExtraction=false for non-Artifact tag")
	}
}
