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

// Inline colour spaces for the sep-canon fixtures below. /CSA is seen first
// and becomes canonical, so /CSB is the one canonicalised to /DeviceCMYK and
// tint transform 5 0 R. /CSN is a DeviceN missing /Colorants, for devicen.
const (
	sepCanonCSA = "/CSA [/Separation /SPOT /DeviceCMYK 5 0 R]"
	sepCanonCSB = "/CSB [/Separation /SPOT /DeviceRGB 6 0 R]"
	sepCanonCSN = "/CSN [/DeviceN [/Cyan /SPOT] /DeviceCMYK 7 0 R]"
)

// sepCanonPDF builds a one-page document whose /Resources value is resources,
// with the tint transforms of sepCanonCSA, sepCanonCSB and sepCanonCSN at
// objects 5, 6 and 7, and extra appended from object 8.
func sepCanonPDF(resources, obj4 string, extra ...string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources " + resources + " >>",
		obj4,
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0 0 0 1] /N 1 >>",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0] /C1 [1 1 1] /N 1 >>",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [1 1 0 0] /N 1 >>",
	}
	return assemblePDF(append(objs, extra...))
}

// canonicalisedCSB fails the test unless /CSB carries the canonical alternate
// space, the mark that sep-canon rewrote the dictionary holding it.
func canonicalisedCSB(t *testing.T, cs ast.PdfDict) {
	t.Helper()

	arr, ok := cs["CSB"].(ast.PdfArray)
	if !ok || len(arr) < 4 {
		t.Fatalf("/CSB is %T %v, want a /Separation array", cs["CSB"], cs["CSB"])
	}
	if alt, _ := arr[2].(ast.PdfName); alt != "DeviceCMYK" {
		t.Fatalf("/CSB still carries alternate space %v: the canonicalisation did not land "+
			"where the output reads it, so this test proves nothing about clobbering", arr[2])
	}
}

// TestEachColorSpaceArrayMutable_KeepsAnEarlierFixersPendingColourSpaceDictWrite
// pins that sep-canon rebuilds an indirect /ColorSpace dictionary from the copy
// an earlier fixer queued, not from the document.
//
// The peer is real and adjacent: devicen runs one entry ahead of sep-canon and
// writes the /Colorants repair of an inline DeviceN into the same dictionary at
// its own object number, 4 here. The /Separation canonicalised is inline in
// that dictionary, so sep-canon rebuilds the whole dictionary too.
func TestEachColorSpaceArrayMutable_KeepsAnEarlierFixersPendingColourSpaceDictWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, sepCanonPDF("<< /ColorSpace 4 0 R >>",
		"<< "+sepCanonCSA+" "+sepCanonCSB+" "+sepCanonCSN+" >>"))

	const csDictObj = 4

	if _, err := fixDeviceNColorants(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixDeviceNColorants: %v", err)
	}
	seeded, ok := alloc.Pending()[csDictObj].(ast.PdfDict)
	if !ok || !hasColorants(seeded["CSN"]) {
		t.Fatalf("control: devicen did not queue the /Colorants repair at object %d, so there "+
			"is no earlier edit to lose; pending = %v", csDictObj, alloc.Pending())
	}

	if _, err := fixSepCanon(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixSepCanon: %v", err)
	}

	got, ok := alloc.Pending()[csDictObj].(ast.PdfDict)
	if !ok {
		t.Fatalf("object %d is no longer a pending dict: %T", csDictObj, alloc.Pending()[csDictObj])
	}
	canonicalisedCSB(t, got)
	if !hasColorants(got["CSN"]) {
		t.Errorf("/CSN lost its /Colorants: eachColorSpaceArrayMutable rebuilt the /ColorSpace "+
			"dictionary from the document and discarded devicen's write; /CSN = %v", got["CSN"])
	}
}

// TestEachColorSpaceArrayMutable_KeepsAnEarlierFixersPendingResourcesWrite pins
// that sep-canon rebuilds an indirect /Resources dictionary from the copy
// output-intent queued, and lands where the output reads it.
//
// The /Resources dictionary is object 4. With the /ColorSpace dictionary inline
// in it, reading object 4 from the document drops the /DefaultRGB output-intent
// injected. With the /ColorSpace dictionary at object 8, output-intent inlines
// a copy of it into object 4, so following the document's reference to object 8
// writes the canonical /Separation where the output no longer looks.
func TestEachColorSpaceArrayMutable_KeepsAnEarlierFixersPendingResourcesWrite(t *testing.T) {
	t.Parallel()

	const cs = "<< " + sepCanonCSA + " " + sepCanonCSB + " >>"
	for _, tc := range []struct {
		name  string
		obj4  string
		extra []string
	}{
		{name: "inline colour space dict", obj4: "<< /ColorSpace " + cs + " >>"},
		{name: "indirect colour space dict", obj4: "<< /ColorSpace 8 0 R >>", extra: []string{cs}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, sepCanonPDF("4 0 R", tc.obj4, tc.extra...))

			const resObj = 4

			if n := injectTestDefaultRGB(t, alloc, doc); n == 0 {
				t.Fatal("control: output-intent patched no page, so there is no earlier edit to lose")
			}
			if _, has := liveResourceColourSpace(t, alloc, doc, resObj)["DefaultRGB"]; !has {
				t.Fatalf("control: output-intent did not queue /DefaultRGB for /Resources %d; pending = %v",
					resObj, alloc.Pending())
			}

			if _, err := fixSepCanon(ctx, alloc, doc, Options{}, nil, ""); err != nil {
				t.Fatalf("fixSepCanon: %v", err)
			}

			got := liveResourceColourSpace(t, alloc, doc, resObj)
			if _, has := got["DefaultRGB"]; !has {
				t.Errorf("/DefaultRGB is gone: eachColorSpaceArrayMutable rebuilt /Resources %d "+
					"from the document and discarded output-intent's write; /ColorSpace = %v", resObj, got)
			}
			canonicalisedCSB(t, got)
		})
	}
}
