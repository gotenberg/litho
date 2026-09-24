package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// deviceNPDF builds a one-page document whose /ColorSpace holds a DeviceN with
// no /Colorants alongside a Separation that already defines the same colorant.
//
// Object 5 is the DeviceN array, 6 the existing Separation, 7 its tint
// transform. When defineSpot is false the Separation is omitted, so the fixer
// has nothing to reuse.
func deviceNPDF(defineSpot bool) []byte {
	cs := "/CSN 5 0 R"
	if defineSpot {
		cs += " /CSS 6 0 R"
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /ColorSpace << " + cs + " >> >> >>",
		"<< /Type /Nothing >>",
		"[/DeviceN [/Cyan /SPOT] /DeviceCMYK 8 0 R]",
		"[/Separation /SPOT /DeviceCMYK 7 0 R]",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0.1000000015 0 0 0] /N 1 >>",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0] /C1 [1 1] /N 1 >>",
	}
	return assemblePDF(objs)
}

// populatedColorants runs the fixer and returns the /Colorants dict it wrote.
func populatedColorants(t *testing.T, data []byte) ast.PdfDict {
	t.Helper()

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())
	if _, err := fixDeviceNColorants(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixDeviceNColorants: %v", err)
	}
	arr, ok := alloc.Pending()[5].(ast.PdfArray)
	if !ok {
		t.Fatalf("the DeviceN array was not rewritten: %T", alloc.Pending()[5])
	}
	if len(arr) < 5 {
		t.Fatalf("the rewritten DeviceN has %d elements, want /Attributes", len(arr))
	}
	attrs, ok := arr[4].(ast.PdfDict)
	if !ok {
		t.Fatalf("/Attributes is %T, want a dict", arr[4])
	}
	colorants, ok := attrs["Colorants"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/Colorants is %T, want a dict", attrs["Colorants"])
	}
	return colorants
}

// TestFixDeviceNColorantsReusesTheDocumentDefinition pins that a colorant the
// document already defines is reused rather than reinvented.
//
// ISO 19005-2, 6.2.4.4 test 2 requires every Separation of the same name to
// share an alternate space and a tint transform. The fixer synthesised a
// degenerate transform for each missing colorant, so populating /Colorants to
// satisfy test 1 made files that already defined the colorant newly fail test
// 2.
//
// # Corpus evidence
//
//   - All corpora: the synthesised transform made four corpus files newly fail
//     test 2.
func TestFixDeviceNColorantsReusesTheDocumentDefinition(t *testing.T) {
	t.Parallel()

	colorants := populatedColorants(t, deviceNPDF(true))
	spot, ok := colorants["SPOT"].(ast.PdfArray)
	if !ok {
		t.Fatalf("/Colorants /SPOT is %T, want a Separation array", colorants["SPOT"])
	}
	if len(spot) < 4 {
		t.Fatalf("the Separation has %d elements, want 4", len(spot))
	}
	// The tint transform must be the very object the existing Separation uses.
	ref, ok := spot[3].(ast.PdfIndirectRef)
	if !ok || ref.ObjectNum != 7 {
		t.Errorf("tint transform = %v, want the document's own object 7", spot[3])
	}
	if name, _ := spot[2].(ast.PdfName); name != "DeviceCMYK" {
		t.Errorf("alternate space = %v, want /DeviceCMYK", spot[2])
	}
}

// TestFixDeviceNColorantsStillSynthesisesAnUndefinedColorant is the control:
// with nothing to reuse, the fixer must still populate /Colorants so
// 6.2.4.4 test 1 is satisfied.
func TestFixDeviceNColorantsStillSynthesisesAnUndefinedColorant(t *testing.T) {
	t.Parallel()

	colorants := populatedColorants(t, deviceNPDF(false))
	spot, ok := colorants["SPOT"].(ast.PdfArray)
	if !ok {
		t.Fatalf("/Colorants /SPOT is %T, want a Separation array", colorants["SPOT"])
	}
	if sep, _ := spot[0].(ast.PdfName); sep != "Separation" {
		t.Errorf("/Colorants /SPOT = %v, want a /Separation array", spot)
	}
	// A synthesised entry points at a freshly allocated tint transform, which
	// cannot be one of the objects the source file already numbered.
	ref, ok := spot[3].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("tint transform = %T, want an indirect reference", spot[3])
	}
	if ref.ObjectNum <= 8 {
		t.Errorf("tint transform = object %d, want a newly allocated one", ref.ObjectNum)
	}
}

// TestFixDeviceNColorantsSkipsProcessColorants is the control for the process
// inks: /Cyan is a device colorant, not a spot ink, so it takes no entry.
func TestFixDeviceNColorantsSkipsProcessColorants(t *testing.T) {
	t.Parallel()

	if colorants := populatedColorants(t, deviceNPDF(true)); colorants["Cyan"] != nil {
		t.Errorf("/Colorants gained a /Cyan entry: %v", colorants["Cyan"])
	}
}

// assertDeviceNUnchanged is a helper kept close to the fixture so a future
// case can assert the no-op path without rebuilding the document.
func assertDeviceNUnchanged(t *testing.T, data []byte) {
	t.Helper()

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())
	fixes, err := fixDeviceNColorants(ctx, alloc, doc, Options{}, nil, "")
	if err != nil {
		t.Fatalf("fixDeviceNColorants: %v", err)
	}
	if len(fixes) != 0 {
		t.Errorf("a document with nothing to populate reported %v", fixes)
	}
}

// TestFixDeviceNColorantsLeavesACompleteSpaceAlone is the control against the
// fixer rewriting a DeviceN that already carries every colorant.
func TestFixDeviceNColorantsLeavesACompleteSpaceAlone(t *testing.T) {
	t.Parallel()

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /ColorSpace << /CSN 5 0 R >> >> >>",
		"<< /Type /Nothing >>",
		fmt.Sprintf("[/DeviceN [/SPOT] /DeviceCMYK 8 0 R << /Colorants << /SPOT %s >> >>]",
			"[/Separation /SPOT /DeviceCMYK 7 0 R]"),
		"<< /Type /Nothing >>",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [1 0 0 0] /N 1 >>",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >>",
	}
	assertDeviceNUnchanged(t, assemblePDF(objs))
}
