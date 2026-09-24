package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestFixType3FontTransparency_ReachesAFontDictStoredInline pins the direct arm
// of the font lookup.
//
// [fixType3FontTransparency] resolves each /Font entry through a ref/direct
// split: an indirect entry is read through the allocator, a directly stored
// dictionary through the document. Every other fixture in this package reaches
// its Type 3 font through an indirect reference (/F1 10 0 R), so without this
// test the direct arm is never executed.
//
// The split is load-bearing rather than cosmetic: collapsing it to a single
// alloc-aware read would hand the helper a ZERO object number for an inline
// font, resolve nothing, and silently skip the repair on every such file.
//
// An inline font dictionary has no object number, so the write at the end of
// the loop cannot fire on the font itself. The observable proof that the direct
// arm ran is one level down: the /ExtGState it reaches through /Resources is
// indirect, so it is repaired at its own object number. A broken direct arm
// leaves fd nil, the loop continues, and nothing is written at all.
//
// # Corpus evidence
//
//   - All corpora: the gate cannot cover the direct arm either unless a carrier
//     happens to store the font inline.
func TestFixType3FontTransparency_ReachesAFontDictStoredInline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, inlineFontDictPDF())

	const gsObj = 4

	if _, present := alloc.Pending()[gsObj]; present {
		t.Fatalf("precondition: object %d must not already be pending", gsObj)
	}

	// Stored DIRECTLY, with no object number: only the direct arm can see it.
	pageRes := ast.PdfDict{"Font": ast.PdfDict{"F1": ast.PdfDict{
		"Type":    ast.PdfName("Font"),
		"Subtype": ast.PdfName("Type3"),
		"Resources": ast.PdfDict{
			"ExtGState": ast.PdfDict{"GS0": ast.PdfIndirectRef{ObjectNum: gsObj}},
		},
	}}}

	if n := fixType3FontTransparency(ctx, doc, alloc, pageRes, make(map[int]bool), 0); n == 0 {
		t.Fatal("a Type 3 font dictionary stored inline must still be walked, repaired 0: " +
			"the direct arm resolved nothing")
	}

	gs := pendingDict(t, alloc, gsObj)
	if got := document.Float(gs["ca"]); got != 1.0 {
		t.Errorf("/ca = %v, want 1: the direct arm did not reach the font's own "+
			"/ExtGState; gs = %v", got, gs)
	}
}

// inlineFontDictPDF carries only the indirect /ExtGState the inline font
// dictionary points at. The font itself is supplied directly by the caller, so
// the file deliberately declares no /Font resource of its own.
func inlineFontDictPDF() []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /ExtGState /ca 0.5 >>",
	)
}
