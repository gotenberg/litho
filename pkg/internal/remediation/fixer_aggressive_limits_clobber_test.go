package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// deviceNManyColorantsPDF builds a one-page document whose /ColorSpace holds a
// DeviceN array with NINE colorants, stored INDIRECTLY at object 5.
//
// Nine is not decoration. [trimDeviceNColorants] skips any array whose name
// list is `<= pdfADeviceNComponents`, so the existing deviceNPDF fixture, which
// declares two, never reaches the write at all. A fixture that cannot reach the
// write proves nothing, which is the dead-fixture failure this suite has hit
// twice.
//
// The array is indirect because trimDeviceNColorants writes the array at its
// own object number only under `isRef && ref.ObjectNum > 0`. An inline array
// takes the other branch, which writes the parent /ColorSpace dictionary
// instead and so exercises a different object (see
// deviceNInlineColourSpacePDF).
func deviceNManyColorantsPDF() []byte {
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /ColorSpace << /CSN 5 0 R >> >> >>",
		"<< /Type /Nothing >>",
		"[/DeviceN [/Cyan /Magenta /Yellow /Black /Spot1 /Spot2 /Spot3 /Spot4 /Spot5]" +
			" /DeviceCMYK 6 0 R]",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0.1 0 0 0] /N 1 >>",
	})
}

// TestTrimDeviceNColorants_KeepsAnEarlierFixersPendingColourSpaceWrite pins
// that the 8-component trim preserves a peer fixer's pending edit to the same
// colour-space object.
//
// [trimDeviceNColorants] rebuilds the DeviceN array wholesale with
// copy(newArr, arr) and writes the result back at that array's own object
// number via alloc.AllocAt. It used to read the array with
// document.ResolveArray, consulting no alloc-aware helper, so an earlier
// fixer's edit to the same object was discarded. The catalog read was already
// alloc-aware (resolveFromAllocOrDoc), which made the omission on the array
// easy to miss: the function was half defended.
//
// THE HELPER MUST MATCH THE OBJECT KIND. resolveFromAllocOrDoc returns a dict
// and cannot make an array read live. The array read now goes through
// liveArray, as in the three peers that read the same `entry` value drawn from
// a /ColorSpace dictionary: patchPageDeviceN (devicen),
// eachColorSpaceArrayMutable (sep-canon) and dedupICCInResources (icc-dedup).
//
// Peer and ordering are measured, not assumed. aggressive-limits is the LAST
// entry of the ordered registry slice in fixer.go, so every colour-space
// writer precedes it: output-intent and output-intent-cleanup, graphics,
// transparency, group-cs, and above all devicen, which rewrites DeviceN arrays
// at the very object numbers this function then rebuilds.
//
// The marker is the ALTERNATE SPACE, element 2, because that is what a
// colour-space fixer realistically rewrites and the fixture declares
// /DeviceCMYK there, so any other value can only have come from the pending
// copy.
func TestTrimDeviceNColorants_KeepsAnEarlierFixersPendingColourSpaceWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, deviceNManyColorantsPDF())

	const csObj = 5

	onDisk, err := document.ResolveArray(ctx, doc, ast.PdfIndirectRef{ObjectNum: csObj})
	if err != nil || len(onDisk) < 3 {
		t.Fatalf("fixture DeviceN array at object %d did not resolve: %v (%v)", csObj, err, onDisk)
	}
	if name, _ := onDisk[0].(ast.PdfName); name != "DeviceN" {
		t.Fatalf("precondition: object %d must be a /DeviceN array, got %v", csObj, onDisk[0])
	}
	names, err := document.ResolveArray(ctx, doc, onDisk[1])
	if err != nil || len(names) <= pdfADeviceNComponents {
		t.Fatalf("precondition: the fixture must declare MORE than %d colorants or the "+
			"trim never runs and this test proves nothing; got %d (%v)",
			pdfADeviceNComponents, len(names), err)
	}
	if alt, _ := onDisk[2].(ast.PdfName); alt != "DeviceCMYK" {
		t.Fatalf("precondition: the fixture's alternate space must be /DeviceCMYK, "+
			"else the marker proves nothing; got %v", onDisk[2])
	}

	// Stand in for devicen or an output-intent patch: carry the array across
	// and change only the alternate space.
	peer := make(ast.PdfArray, len(onDisk))
	copy(peer, onDisk)
	peer[2] = ast.PdfName("DeviceRGB")
	if !alloc.AllocAt(csObj, peer) {
		t.Fatalf("AllocAt(%d) refused the earlier fixer's pending write", csObj)
	}

	if _, err := fixAggressiveLimits(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixAggressiveLimits: %v", err)
	}

	got, ok := alloc.Pending()[csObj].(ast.PdfArray)
	if !ok {
		t.Fatalf("object %d is no longer a pending array: %T", csObj, alloc.Pending()[csObj])
	}

	// Self-diagnosing, keyed on the SEEDED object: the seed carried NINE names,
	// so a list of exactly eight can only have been written by the repair. That
	// rules out the test passing by reading back its own seed.
	gotNames, ok := got[1].(ast.PdfArray)
	if !ok {
		t.Fatalf("element 1 is %T, want the colorant name array; the repair did not "+
			"run as expected", got[1])
	}
	if len(gotNames) != pdfADeviceNComponents {
		t.Fatalf("object %d carries %d colorants, want %d: the repair never trimmed it, "+
			"so this test proves nothing about clobbering; array = %v",
			csObj, len(gotNames), pdfADeviceNComponents, got)
	}

	if alt, _ := got[2].(ast.PdfName); alt != "DeviceRGB" {
		t.Errorf("the alternate space is %v, want /DeviceRGB: trimDeviceNColorants "+
			"rebuilt the DeviceN array from the on-disk copy and discarded the "+
			"earlier fixer's write; array = %v", got[2], got)
	}
}
