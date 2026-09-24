package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// twoSeparationsPDF declares the SAME colorant name twice with different
// (alternate space, tint transform) pairs, which is what sep-canon exists to
// reconcile: ISO 19005 requires Separations of one name to agree.
//
// collectSeparations iterates sortedDictKeys, so /CSA is seen first and becomes
// canonical; /CSB then differs at indices 2 and 3 and is the one rewritten.
// /CSB is stored INDIRECTLY (object 6) because that is the only shape in which
// eachColorSpaceArrayMutable takes its isRef branch and calls
// alloc.AllocAt(ref.ObjectNum, newArr). Stored inline it would splice into the
// /ColorSpace dict instead and the seeded object would never be written at all.
func twoSeparationsPDF() []byte {
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /ColorSpace << /CSA 5 0 R /CSB 6 0 R >> >> >>",
		"<< /Type /Nothing >>",
		"[/Separation /SPOT /DeviceCMYK 7 0 R]",
		"[/Separation /SPOT /DeviceRGB 8 0 R]",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0 0 0 1] /N 1 >>",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0] /C1 [1 1 1] /N 1 >>",
	})
}

// TestEachColorSpaceArrayMutable_KeepsAnEarlierFixersPendingSeparationWrite
// pins that sep-canon reads a colour-space array through the allocator.
//
// [eachColorSpaceArrayMutable] used to read each colour-space array with
// document.ResolveArray, hand it to the caller's patch closure, and write the
// result back at the array's own object number. It consulted no alloc-aware
// helper, so an earlier fixer's edit to that array was discarded.
//
// Peer and ordering are measured: colour-space objects are written earlier by
// output-intent (fixer.go:68) and output-intent-cleanup (:70), and by graphics
// (:106), transparency (:107) and group-cs (:108). sep-canon runs at :116.
//
// COVERAGE NOTE, measured 2026-09-20: NOTHING in this package drives
// fixSepCanon, eachColorSpaceArrayMutable, patchColorants or matchSepCanon. The
// only test in fixer_sepcanon_test.go covers objectsEqual's panic fix. Registry
// entry 116 carries four AllocAt sites and was entirely unpinned.
//
// The marker is a FIFTH array element. rewriteSeparations builds its
// replacement with `newArr := make(ast.PdfArray, len(arr))` followed by
// `copy(newArr, arr)`, sizing the result from the array it READ, so a fifth
// element survives an alloc-aware read and vanishes on a stale one. It is inert
// to the logic: extractSeparation inspects only indices 0 and 1, and
// arraysEqual compares only 2 and 3.
func TestEachColorSpaceArrayMutable_KeepsAnEarlierFixersPendingSeparationWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, twoSeparationsPDF())

	const sepObj = 6
	const marker = ast.PdfName("clobberMarker")

	// Stand in for an earlier colour-space fixer: carry the array across
	// untouched and append one inert element.
	peer := ast.PdfArray{
		ast.PdfName("Separation"), ast.PdfName("SPOT"),
		ast.PdfName("DeviceRGB"),
		ast.PdfIndirectRef{ObjectNum: 8},
		marker,
	}
	if !alloc.AllocAt(sepObj, peer) {
		t.Fatalf("AllocAt(%d) refused the earlier fixer's pending write", sepObj)
	}

	if _, err := fixSepCanon(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixSepCanon: %v", err)
	}

	got, ok := alloc.Pending()[sepObj].(ast.PdfArray)
	if !ok {
		t.Fatalf("object %d is no longer a pending array: %T", sepObj, alloc.Pending()[sepObj])
	}

	// Self-diagnosing, keyed on the SEEDED object: the seed carried
	// /DeviceRGB, so seeing the canonical /DeviceCMYK proves object 6 itself
	// was rewritten by the repair. Without this the test could pass by reading
	// back its own seed untouched.
	if alt, _ := got[2].(ast.PdfName); alt != "DeviceCMYK" {
		t.Fatalf("object %d still carries alternate space %v: the repair never "+
			"canonicalised it, so this test proves nothing about clobbering; array = %v",
			sepObj, got[2], got)
	}

	if len(got) < 5 || got[4] != marker {
		t.Errorf("the marker element is gone: eachColorSpaceArrayMutable rebuilt the "+
			"Separation array from the on-disk copy and discarded the earlier "+
			"fixer's write; array = %v", got)
	}
}
