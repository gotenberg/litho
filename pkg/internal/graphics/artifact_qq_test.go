package graphics

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// TestArtifactSpan_UnbalancedSaveDoesNotOutliveEMC pins that a graphics-state
// frame saved inside an /Artifact span stops skipping extraction once EMC has
// closed that span, even though the frame outlives it.
//
// Marked-content nesting is independent of the q/Q stack (ISO 32000-1, 14.6),
// so a span may open in one frame and close in another. Carrying the skip flag
// in the restored frame silently dropped everything a later Q uncovered:
// issue13823 lost both of its form XObjects, and TAMReview-style pages lost
// whole trailing sections.
func TestArtifactSpan_UnbalancedSaveDoesNotOutliveEMC(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.StripArtifacts = true

	// An artifact span that saves the state and never restores it, exactly the
	// shape a tagger emits when it wraps a region whose q/Q pairs straddle it.
	ops := []content.Operation{
		{Operator: "BMC", Operands: []ast.PdfObject{ast.PdfName("Artifact")}},
		{Operator: "q"},
		{Operator: "EMC"},
	}
	if err := m.Execute(context.Background(), ops); err != nil {
		t.Fatal(err)
	}
	if m.Current().SkipExtraction {
		t.Fatal("expected SkipExtraction=false immediately after EMC")
	}

	// The Q uncovers the frame saved inside the span. No artifact is open, so
	// the content it reveals is extractable.
	if err := m.Execute(context.Background(), []content.Operation{{Operator: "Q"}}); err != nil {
		t.Fatal(err)
	}
	if m.Current().SkipExtraction {
		t.Error("Q restored a frame saved inside a closed /Artifact span and resumed skipping")
	}
}

// TestRestoreMarkedContent_RebuildsSpansAFormClosed pins that a nested stream
// carrying a stray EMC cannot close a span its caller still has open.
//
// Restoring only to the former depth cannot express this: the form leaves the
// stack shorter than it found it, so there is nothing to truncate and the
// page's own /Artifact would stay closed, leaking decoration into the text.
func TestRestoreMarkedContent_RebuildsSpansAFormClosed(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.StripArtifacts = true

	if err := m.Execute(context.Background(), []content.Operation{
		{Operator: "BMC", Operands: []ast.PdfObject{ast.PdfName("Artifact")}},
	}); err != nil {
		t.Fatal(err)
	}
	snapshot := m.SnapshotMarkedContent()

	// The form over-closes, popping the page's still-open span.
	if err := m.Execute(context.Background(), []content.Operation{{Operator: "EMC"}}); err != nil {
		t.Fatal(err)
	}
	if m.Current().SkipExtraction {
		t.Fatal("precondition: the stray EMC should have cancelled the span")
	}

	m.RestoreMarkedContent(snapshot)
	if len(m.MarkedContent) != 1 {
		t.Fatalf("marked-content stack has %d spans, want the page's 1 back", len(m.MarkedContent))
	}
	if !m.Current().SkipExtraction {
		t.Error("a form's stray EMC closed the page's /Artifact span for good")
	}
}

// TestArtifactSpan_RestoreDoesNotCancelOpenSpan pins the opposite direction: a
// Q landing inside an /Artifact span must not cancel it. Otherwise a frame
// saved before the span leaks the artifact's own content into extraction.
func TestArtifactSpan_RestoreDoesNotCancelOpenSpan(t *testing.T) {
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.StripArtifacts = true

	ops := []content.Operation{
		{Operator: "q"},
		{Operator: "BMC", Operands: []ast.PdfObject{ast.PdfName("Artifact")}},
		{Operator: "Q"},
	}
	if err := m.Execute(context.Background(), ops); err != nil {
		t.Fatal(err)
	}
	if !m.Current().SkipExtraction {
		t.Error("Q cancelled an /Artifact span that is still open")
	}
}
