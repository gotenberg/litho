package annotation

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestWidgetFieldRoot_ResolvesANestedFieldTree pins that the ascent still
// climbs past every intermediate field to the outermost one.
//
// The widget's /Parent is a kid field that states only /T, and the field type
// lives on the root above it (ISO 32000-1, 12.7.3.1). A walk that stopped after
// one hop would answer with the kid, so this is the guard against mis-wiring
// the ascent rather than a statement about cycles.
func TestWidgetFieldRoot_ResolvesANestedFieldTree(t *testing.T) {
	t.Parallel()

	ctx, doc := widgetDoc(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [6 0 R] >>",
		"<< /FT /Tx /T (root) >>",
		"<< /T (kid) /Parent 4 0 R >>",
		"<< /Type /Annot /Subtype /Widget /Parent 5 0 R /Rect [0 0 10 10] >>",
	})

	got, ok := widgetFieldRoot(ctx, doc, ast.PdfIndirectRef{ObjectNum: 6})
	if !ok {
		t.Fatal("a widget whose root field states /FT was reported as decoration")
	}
	want := ast.PdfObject(ast.PdfIndirectRef{ObjectNum: 4})
	if got != want {
		t.Errorf("root field = %v, want %v (the outermost field, not the kid)", got, want)
	}
}

// TestWidgetFieldRoot_CycleTerminates pins termination only.
//
// On a /Parent cycle there is no correct root to name, and the guarded and
// unguarded walks happen to halt on the same node for a two-node loop, so this
// would not have failed before the visited set was added. It exists to keep a
// cyclic chain from ever becoming a hang.
func TestWidgetFieldRoot_CycleTerminates(t *testing.T) {
	t.Parallel()

	ctx, doc := widgetDoc(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [5 0 R] >>",
		"<< /FT /Tx /T (name) /Parent 5 0 R >>",
		"<< /Type /Annot /Subtype /Widget /Parent 4 0 R /Rect [0 0 10 10] >>",
	})

	// The assertion is that this returns at all; either verdict is acceptable
	// for a chain that names no root.
	if _, ok := widgetFieldRoot(ctx, doc, ast.PdfIndirectRef{ObjectNum: 5}); ok {
		t.Log("cyclic chain reported a root field, which is permitted here")
	}
}
