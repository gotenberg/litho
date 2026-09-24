package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// annotFlagsInlinePDF builds a one-page document whose /Annots array is an
// INDIRECT object holding an inline annotation that needs an /F flag.
//
// Both halves are required to reach the write under test. [installInlineAnnots]
// only runs when something was repaired INLINE, which needs an annotation that
// is not an indirect object, and it writes the array at its own object number
// only when /Annots is a reference. An all-indirect fixture takes the
// per-annotation write in [fixAnnotFlags] instead and never touches the array
// at all.
//
// The inline /Link declares no /F, so currentF is 0, the already-correct
// shortcut is skipped, and it is collected into inlineFixed. The
// indirect /FileAttachment already carries /F 4 (Print set, nothing forbidden)
// so it is skipped outright, keeping the array write the only thing under test.
func annotFlagsInlinePDF() []byte {
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots 4 0 R >>",
		"[<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] >> 5 0 R]",
		"<< /Type /Annot /Subtype /FileAttachment /Rect [0 0 10 10] /F 4 >>",
	})
}

// TestFixAnnotFlags_KeepsAnEarlierFixersPendingAnnotsArray pins that repairing
// an inline annotation does not resurrect annotations an earlier fixer removed.
//
// [fixAnnotFlags] hands the page's /Annots array to [installInlineAnnots],
// which rebuilds it wholesale with copy(newArr, annots) and writes it back at
// the array's own object number, or into the page when /Annots is inline. It
// used to read that array with document.ResolveArray, consulting no
// alloc-aware helper, so an indirect array was rebuilt from the parsed file
// rather than from an earlier fixer's pending copy.
//
// THE PEER IS annotations, WHICH RUNS FIRST. fixAnnotArray nils forbidden
// annotation slots and rebuildAnnotsArray compacts them, then
// fixPageAnnotations writes the result at that same array object, and
// annotations precedes annot-flags in the registry. So the stale rebuild
// restored annotations that the annotations fixer had deliberately removed,
// which is an output correctness regression rather than a wasted object.
//
// The matched helper is liveArray, for an ARRAY, and fixAnnotFlags now reads
// the array through it. The sibling fixer did exactly that already:
// fixAnnotationAppearances reads liveArray(ctx, alloc, doc, annotsObj) before
// installInlineAppearances makes the same kind of write.
//
// The marker is the array's LENGTH, because that is what removal changes and
// what a stale rebuild restores: newArr is sized from the stale array, so a
// peer's shortened array comes back at full length.
func TestFixAnnotFlags_KeepsAnEarlierFixersPendingAnnotsArray(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, annotFlagsInlinePDF())

	const annotsObj = 4

	onDisk, err := document.ResolveArray(ctx, doc, ast.PdfIndirectRef{ObjectNum: annotsObj})
	if err != nil || len(onDisk) != 2 {
		t.Fatalf("fixture /Annots array at object %d did not resolve to 2 entries: %v (%v)",
			annotsObj, err, onDisk)
	}
	inline, isDict := onDisk[0].(ast.PdfDict)
	if !isDict {
		t.Fatalf("precondition: /Annots[0] must be an INLINE dict or the inline path is "+
			"never reached; got %T", onDisk[0])
	}
	if _, hasF := inline["F"]; hasF {
		t.Fatalf("precondition: the inline annotation must declare no /F, else it is "+
			"skipped and nothing is repaired; got %v", inline["F"])
	}
	if _, isRef := onDisk[1].(ast.PdfIndirectRef); !isRef {
		t.Fatalf("precondition: /Annots[1] must be an indirect annotation for its "+
			"removal to be meaningful; got %T", onDisk[1])
	}

	// Stand in for the annotations fixer: the forbidden annotation has been removed
	// and the array compacted, exactly what rebuildAnnotsArray produces.
	peer := ast.PdfArray{onDisk[0]}
	if !alloc.AllocAt(annotsObj, peer) {
		t.Fatalf("AllocAt(%d) refused the earlier fixer's pending write", annotsObj)
	}

	if _, err := fixAnnotFlags(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixAnnotFlags: %v", err)
	}

	got, ok := alloc.Pending()[annotsObj].(ast.PdfArray)
	if !ok {
		t.Fatalf("object %d is no longer a pending array: %T", annotsObj, alloc.Pending()[annotsObj])
	}

	// Self-diagnosing, keyed on the SEEDED object: the seed carried no /F, so
	// finding one proves the repair rewrote object 4 rather than the test
	// reading back its own seed.
	if len(got) == 0 {
		t.Fatalf("object %d came back empty; the repair did not run as expected", annotsObj)
	}
	repaired, isDict := got[0].(ast.PdfDict)
	if !isDict {
		t.Fatalf("/Annots[0] is %T after the repair, want the inline dict", got[0])
	}
	if f, _ := repaired["F"].(ast.PdfInt); f != 4 {
		t.Fatalf("the inline annotation carries /F %v, want 4: the repair never ran, so "+
			"this test proves nothing about clobbering", repaired["F"])
	}

	if len(got) != 1 {
		t.Errorf("/Annots has %d entries, want 1: fixAnnotFlags rebuilt the array from "+
			"the on-disk copy, so the annotation that annotations @121 removed is "+
			"back; array = %v", len(got), got)
	}
}
