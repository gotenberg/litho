package remediation

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// interpolatedImagePDF builds a one-page document whose only XObject is a
// FlateDecode image with /Interpolate true, the key ISO 19005-2, 6.2.4 requires
// to be false.
func interpolatedImagePDF(t *testing.T) []byte {
	t.Helper()

	var deflated bytes.Buffer
	zw := zlib.NewWriter(&deflated)
	if _, err := zw.Write([]byte{0, 255, 128, 64}); err != nil {
		t.Fatalf("deflating pixels: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("closing deflate writer: %v", err)
	}
	pixels := deflated.Bytes()

	const content = "q 100 0 0 100 50 600 cm /Im0 Do Q\n"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /XObject << /Im0 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceGray"+
			" /BitsPerComponent 8 /Interpolate true /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream",
			len(pixels), pixels),
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// TestFixImageIssuesKeepsThePixels pins that editing an image's dict does not
// drop its body.
//
// Only /Interpolate changes here, so the pixels have to survive. Queuing the
// replacement as a stream with a nil body serialises as /Length 0, which empties
// every image the fixer touches: fixResourcesGraphics carries the original bytes
// across for exactly this reason, and this fixer did not.
func TestFixImageIssuesKeepsThePixels(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := interpolatedImagePDF(t)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))

	notes, err := fixImageIssues(ctx, alloc, doc, Options{}, nil, "")
	if err != nil {
		t.Fatalf("fixImageIssues: %v", err)
	}
	if len(notes) == 0 {
		t.Fatal("the image carries /Interpolate true, so the fixer should report a change")
	}

	queued, ok := alloc.Pending()[5].(ast.PdfStream)
	if !ok {
		t.Fatalf("the image was not rewritten: %T", alloc.Pending()[5])
	}
	if interp, _ := queued.Dict["Interpolate"].(ast.PdfBool); bool(interp) {
		t.Error("/Interpolate was not cleared")
	}
	body, err := queued.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the rewritten image: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("the image body is empty: the fix replaced the pixels with nothing")
	}
	// The body stays raw, so it must still match the /Filter the dict keeps.
	if _, filtered := queued.Dict["Filter"]; !filtered {
		t.Error("the body is carried across raw, so /Filter has to stay")
	}
	decoded, err := doc.DecodeStream(ctx, queued)
	if err != nil {
		t.Fatalf("the rewritten image does not decode: %v", err)
	}
	if !bytes.Equal(decoded, []byte{0, 255, 128, 64}) {
		t.Errorf("pixels = %v, want the original four samples", decoded)
	}
}
