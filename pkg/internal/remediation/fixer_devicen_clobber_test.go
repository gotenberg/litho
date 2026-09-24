package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestPatchPageDeviceN_KeepsAnEarlierFixersPendingColourSpaceWrite pins that
// the DeviceN /Colorants repair keeps an earlier fixer's pending write to the
// same colour-space object.
//
// [patchPageDeviceN] reads the DeviceN colour-space array with
// document.ResolveArray (fixer_devicen.go), hands that value to
// [buildPatchedDeviceN], and writes the result back at the array's own object
// number. It consults no alloc-aware helper, so an earlier fixer's edit to the
// same colour-space object is discarded.
//
// Peer and ordering are measured, not assumed. Colour-space objects are written
// earlier by output-intent (fixer.go:68) and output-intent-cleanup (:70)
// through injectDefaultColorSpace, patchResourceColorSpace and
// recurseColorSpacePatch, and later-but-still-earlier by graphics (:106),
// transparency (:107) and group-cs (:108). devicen runs at :115, after all of
// them.
//
// The value that decides the bytes is the STALE one: buildPatchedDeviceN takes
// the array as a PARAMETER, so passing it alloc changes nothing.
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
		t.Errorf("the alternate space is %v, want /DeviceRGB: patchPageDeviceN rebuilt "+
			"the DeviceN array from the on-disk copy and discarded the earlier "+
			"fixer's write; array = %v", got[2], got)
	}
}
