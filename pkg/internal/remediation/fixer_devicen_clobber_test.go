package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// TestPatchPageDeviceN_KeepsAnEarlierFixersPendingColourSpaceWrite pins that
// the DeviceN /Colorants repair keeps an earlier fixer's pending write to the
// same colour-space object.
//
// The /Colorants repair ([deviceNWalk]) hands the DeviceN colour-space array to
// [buildPatchedDeviceN] and writes the result back at the array's own object
// number. It used to read that array with document.ResolveArray, consulting no
// alloc-aware helper, so an earlier fixer's edit to the same colour-space
// object was discarded. It now reads the array with liveArray.
//
// Peer and ordering are measured, not assumed. Colour-space objects are written
// earlier by output-intent and output-intent-cleanup through
// injectDefaultColorSpace, patchResourceColorSpace and recurseColorSpacePatch,
// and later-but-still-earlier by graphics, transparency and group-cs. devicen
// follows all of them in the registry.
//
// The READ decides the bytes: buildPatchedDeviceN takes the array as a
// PARAMETER, so passing it alloc would change nothing. Only reading the array
// alloc-first keeps the peer's edit.
//
// The marker is the ALTERNATE SPACE, element 2 of the array, because that is
// what a colour-space fixer would realistically rewrite, and because the
// fixture declares /DeviceCMYK there so any other value can only have come from
// the pending copy. deviceNPDF stores the array INDIRECTLY at object 5
// (/CSN 5 0 R), which is the only shape in which AllocAt fires on it: the
// package's own helper already asserts alloc.Pending()[5].
func TestPatchPageDeviceN_KeepsAnEarlierFixersPendingColourSpaceWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, deviceNPDF(true))

	const csObj = 5

	onDisk, err := document.ResolveArray(ctx, doc, ast.PdfIndirectRef{ObjectNum: csObj})
	if err != nil || len(onDisk) < 3 {
		t.Fatalf("fixture DeviceN array at object %d did not resolve: %v (%v)", csObj, err, onDisk)
	}
	if name, _ := onDisk[0].(ast.PdfName); name != "DeviceN" {
		t.Fatalf("precondition: object %d must be a /DeviceN array, got %v", csObj, onDisk[0])
	}
	if alt, _ := onDisk[2].(ast.PdfName); alt != "DeviceCMYK" {
		t.Fatalf("precondition: the fixture's alternate space must be /DeviceCMYK, "+
			"else the marker proves nothing; got %v", onDisk[2])
	}
	if len(onDisk) > 4 {
		t.Fatalf("precondition: the fixture must NOT already carry /Attributes, "+
			"or the guard below proves nothing; array = %v", onDisk)
	}

	// Stand in for an earlier colour-space fixer: carry the array across and
	// change only the alternate space, the shape output-intent's patches use.
	peer := make(ast.PdfArray, len(onDisk))
	copy(peer, onDisk)
	peer[2] = ast.PdfName("DeviceRGB")
	if !alloc.AllocAt(csObj, peer) {
		t.Fatalf("AllocAt(%d) refused the earlier fixer's pending write", csObj)
	}

	if _, err := fixDeviceNColorants(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixDeviceNColorants: %v", err)
	}

	got, ok := alloc.Pending()[csObj].(ast.PdfArray)
	if !ok {
		t.Fatalf("object %d is no longer a pending array: %T", csObj, alloc.Pending()[csObj])
	}

	// Self-diagnosing, keyed on the SEEDED object: the seed carried no
	// /Attributes, so its presence proves object 5 itself was rewritten by the
	// repair. Without this the test could pass by reading back its own seed.
	if len(got) < 5 {
		t.Fatalf("object %d has %d elements and no /Attributes: the repair never "+
			"rewrote it, so this test proves nothing about clobbering; array = %v",
			csObj, len(got), got)
	}
	attrs, ok := got[4].(ast.PdfDict)
	if !ok {
		t.Fatalf("/Attributes is %T, want a dict; the repair did not run as expected", got[4])
	}
	if _, has := attrs["Colorants"]; !has {
		t.Fatalf("/Attributes carries no /Colorants: the repair did not run, so this "+
			"test proves nothing about clobbering; attrs = %v", attrs)
	}

	if alt, _ := got[2].(ast.PdfName); alt != "DeviceRGB" {
		t.Errorf("the alternate space is %v, want /DeviceRGB: the /Colorants repair rebuilt "+
			"the DeviceN array from the on-disk copy and discarded the earlier "+
			"fixer's write; array = %v", got[2], got)
	}
}

// injectTestDefaultRGB runs output-intent's /DefaultRGB injection, the real
// earlier writer of page /Resources dictionaries, and returns how many pages it
// patched. output-intent sits ahead of devicen, sep-canon and icc-dedup in the
// registry, and patchResourceColorSpace writes an indirect /Resources
// dictionary back at its own object number with the /ColorSpace dictionary
// inlined into it, which is what makes it the peer these tests need.
func injectTestDefaultRGB(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document) int {
	t.Helper()

	ctx := context.Background()
	catalog := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 1})
	if catalog == nil {
		t.Fatal("fixture catalog at object 1 did not resolve")
	}
	var iccRef ast.PdfIndirectRef
	csArr := func() ast.PdfArray {
		if iccRef.ObjectNum == 0 {
			iccRef = alloc.Alloc(ast.NewStream(ast.PdfDict{"N": ast.PdfInt(3)}, sRGBICCProfile))
		}
		return ast.PdfArray{ast.PdfName("ICCBased"), iccRef}
	}
	n, err := injectDefaultColorSpace(ctx, alloc, doc, catalog, "DefaultRGB", csArr)
	if err != nil {
		t.Fatalf("injectDefaultColorSpace: %v", err)
	}
	return n
}

// liveResourceColourSpace returns the /ColorSpace dictionary the output will
// carry for the /Resources dictionary at resObj: the allocator's copy of each
// object when one is pending, the document's otherwise, following the
// /ColorSpace value whether it is inline or indirect.
func liveResourceColourSpace(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, resObj int) ast.PdfDict {
	t.Helper()

	ctx := context.Background()
	res := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: resObj})
	if res == nil {
		t.Fatalf("/Resources at object %d did not resolve", resObj)
	}
	switch cs := res["ColorSpace"].(type) {
	case ast.PdfDict:
		return cs
	case ast.PdfIndirectRef:
		d := resolveFromAllocOrDoc(ctx, alloc, doc, cs)
		if d == nil {
			t.Fatalf("/ColorSpace %v of /Resources %d did not resolve", cs, resObj)
		}
		return d
	default:
		t.Fatalf("/Resources %d carries /ColorSpace %T, want a dict or a reference", resObj, res["ColorSpace"])
		return nil
	}
}

// hasColorants reports whether a colour-space value is a DeviceN array that
// carries an /Attributes dictionary with a /Colorants entry, the mark
// buildPatchedDeviceN leaves.
func hasColorants(obj ast.PdfObject) bool {
	arr, ok := obj.(ast.PdfArray)
	if !ok || len(arr) < 5 {
		return false
	}
	attrs, ok := arr[4].(ast.PdfDict)
	if !ok {
		return false
	}
	_, has := attrs["Colorants"]
	return has
}

// separationColorant returns the colorant name of a /Separation array.
func separationColorant(t *testing.T, obj ast.PdfObject) ast.PdfName {
	t.Helper()

	arr, ok := obj.(ast.PdfArray)
	if !ok || len(arr) < 2 {
		t.Fatalf("want a /Separation array, got %T %v", obj, obj)
	}
	name, ok := arr[1].(ast.PdfName)
	if !ok {
		t.Fatalf("/Separation colorant is %T, want a name", arr[1])
	}
	return name
}

// deviceNIndirectColourSpaceDictPDF stores the page's /ColorSpace DICTIONARY
// indirectly at object 4 while both colour spaces inside it are INLINE, so
// the /Colorants repair rebuilds the whole dictionary at its own number.
// /CSX names its colorant /Sp#E9t, byte 0xE9 on its own being
// invalid UTF-8, so name-utf8 has a reason to rewrite object 4 first.
func deviceNIndirectColourSpaceDictPDF() []byte {
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /ColorSpace 4 0 R >> >>",
		"<< /CSN [/DeviceN [/Cyan /SPOT] /DeviceCMYK 5 0 R] /CSX [/Separation /Sp#E9t /DeviceCMYK 6 0 R] >>",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [1 1 0 0] /N 1 >>",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0.1 0 0 0] /N 1 >>",
	})
}

// TestPatchPageDeviceN_KeepsAnEarlierFixersPendingColourSpaceDictWrite pins
// that the /Colorants repair rebuilds an indirect /ColorSpace dictionary from
// the copy an earlier fixer queued, not from the document.
//
// The peer is real: name-utf8 runs ahead of devicen and rewrites any indirect
// object carrying a name that is not valid UTF-8 (ISO 19005-2, 6.1.8), here the
// /Sp#E9t colorant inside object 4. A /CSX colorant name back at the raw byte
// after devicen means the dictionary was rebuilt from the document and the
// sanitised copy was discarded.
func TestPatchPageDeviceN_KeepsAnEarlierFixersPendingColourSpaceDictWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, deviceNIndirectColourSpaceDictPDF())

	const csDictObj = 4

	if _, err := fixNameUTF8(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixNameUTF8: %v", err)
	}
	seeded, ok := alloc.Pending()[csDictObj].(ast.PdfDict)
	if !ok {
		t.Fatalf("control: name-utf8 did not rewrite object %d, so there is no earlier "+
			"edit to lose and this test proves nothing; pending = %v", csDictObj, alloc.Pending())
	}
	sanitised := separationColorant(t, seeded["CSX"])
	if sanitised == "Sp\xe9t" {
		t.Fatalf("control: name-utf8 rewrote object %d but left /CSX's colorant unsanitised", csDictObj)
	}

	if _, err := fixDeviceNColorants(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixDeviceNColorants: %v", err)
	}

	got, ok := alloc.Pending()[csDictObj].(ast.PdfDict)
	if !ok {
		t.Fatalf("object %d is no longer a pending dict: %T", csDictObj, alloc.Pending()[csDictObj])
	}
	if !hasColorants(got["CSN"]) {
		t.Fatalf("/CSN in object %d carries no /Colorants: the repair did not rewrite the "+
			"dictionary, so this test proves nothing about clobbering; /CSN = %v", csDictObj, got["CSN"])
	}
	if name := separationColorant(t, got["CSX"]); name != sanitised {
		t.Errorf("/CSX names colorant %q, want name-utf8's %q: the /Colorants repair rebuilt the "+
			"/ColorSpace dictionary from the document and discarded the earlier fixer's write",
			name, sanitised)
	}
}

// deviceNIndirectResourcesPDF stores the page's /Resources dictionary
// indirectly at object 4 with an INLINE DeviceN missing /Colorants. With
// csIndirect false the /ColorSpace dictionary is inline in object 4, so the
// repair writes object 4 itself. With csIndirect true it sits at object 6, and
// output-intent's peer write inlines a copy of it into object 4, which leaves
// object 6 unreferenced by the output.
func deviceNIndirectResourcesPDF(csIndirect bool) []byte {
	const cs = "<< /CSN [/DeviceN [/Cyan /SPOT] /DeviceCMYK 5 0 R] >>"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 4 0 R >>",
		"<< /ColorSpace " + cs + " >>",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [1 1 0 0] /N 1 >>",
	}
	if csIndirect {
		objs[3] = "<< /ColorSpace 6 0 R >>"
		objs = append(objs, cs)
	}
	return assemblePDF(objs)
}

// TestPatchPageDeviceN_KeepsAnEarlierFixersPendingResourcesWrite pins that the
// /Colorants repair rebuilds an indirect /Resources dictionary from the copy
// output-intent queued, and lands where the output reads it.
//
// Two contracts, reported independently:
//
//	PEER PRESERVED  the /DefaultRGB output-intent injected must survive. Reading
//	                /Resources from the document and writing it back at its own
//	                object number drops it.
//	REPAIR LANDED   the /Colorants repair must be visible through the
//	                /Resources dictionary the output carries. Following the
//	                document's /ColorSpace reference to object 6 and writing
//	                there patches an object the pending /Resources no longer
//	                references.
func TestPatchPageDeviceN_KeepsAnEarlierFixersPendingResourcesWrite(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		csIndirect bool
	}{
		{name: "inline colour space dict", csIndirect: false},
		{name: "indirect colour space dict", csIndirect: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, deviceNIndirectResourcesPDF(tc.csIndirect))

			const resObj = 4

			if n := injectTestDefaultRGB(t, alloc, doc); n == 0 {
				t.Fatal("control: output-intent patched no page, so there is no earlier edit to lose")
			}
			if _, has := liveResourceColourSpace(t, alloc, doc, resObj)["DefaultRGB"]; !has {
				t.Fatalf("control: output-intent did not queue /DefaultRGB for /Resources %d; pending = %v",
					resObj, alloc.Pending())
			}

			if _, err := fixDeviceNColorants(ctx, alloc, doc, Options{}, nil, ""); err != nil {
				t.Fatalf("fixDeviceNColorants: %v", err)
			}

			cs := liveResourceColourSpace(t, alloc, doc, resObj)
			if _, has := cs["DefaultRGB"]; !has {
				t.Errorf("/DefaultRGB is gone: the /Colorants repair rebuilt /Resources %d from the "+
					"document and discarded output-intent's write; /ColorSpace = %v", resObj, cs)
			}
			if !hasColorants(cs["CSN"]) {
				t.Errorf("/CSN carries no /Colorants in the /ColorSpace the output reads: the "+
					"repair was written where the pending /Resources no longer points; /CSN = %v",
					cs["CSN"])
			}
		})
	}
}
