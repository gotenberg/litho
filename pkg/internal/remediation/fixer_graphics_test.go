package remediation

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestMatchGraphics_PostScriptXObject covers the PDF/A-1 §6.2.7 clause that the
// matcher omitted, leaving PostScript XObjects unfixable on part 1.
func TestMatchGraphics_PostScriptXObject(t *testing.T) {
	t.Parallel()
	if !matchGraphics(compliance.Violation{Rule: compliance.Rule_19005_1_6_2_7_t01}) {
		t.Error("matchGraphics must match PDF/A-1 §6.2.7 (PostScript XObject)")
	}
}

// pdfWithImageIntent builds a single-page PDF whose page references an image
// XObject carrying the given /Intent and raw stream data. A non-empty filter is
// emitted as /Filter so the test can confirm the fixer preserves the raw
// (encoded) bytes untouched while it edits the dict.
func pdfWithImageIntent(intent, filter string, data []byte) []byte {
	filterEntry := ""
	if filter != "" {
		filterEntry = " /Filter /" + filter
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.6\n")
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /Im0 4 0 R >> >> >>",
	}
	offsets := make([]int, 5)
	for i, body := range objs {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	offsets[4] = buf.Len()
	fmt.Fprintf(&buf, "4 0 obj\n<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceRGB%s /Intent /%s /Length %d >>\nstream\n", filterEntry, intent, len(data))
	buf.Write(data)
	buf.WriteString("\nendstream\nendobj\n")
	xrefStart := buf.Len()
	buf.WriteString("xref\n0 5\n0000000000 65535 f \r\n")
	for i := 1; i <= 4; i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size 5 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefStart)
	return buf.Bytes()
}

// TestFixGraphics_RepairsImageRenderingIntent checks the new image /Intent
// repair AND that the repair preserves the image's stream bytes (the modified
// write-back used to re-emit the stream with nil data, blanking the image).
func TestFixGraphics_RepairsImageRenderingIntent(t *testing.T) {
	doc, alloc := newTestDoc(t, pdfWithImageIntent("Foo", "", []byte{0x7f}))

	if _, err := fixGraphics(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixGraphics: %v", err)
	}

	stream, ok := alloc.Pending()[4].(ast.PdfStream)
	if !ok {
		t.Fatalf("expected image XObject (obj 4) rewritten as a stream, got %T", alloc.Pending()[4])
	}
	if stream.Dict["Intent"] != ast.PdfName("RelativeColorimetric") {
		t.Errorf("image /Intent: got %v, want RelativeColorimetric", stream.Dict["Intent"])
	}
	data, err := stream.Bytes(context.Background())
	if err != nil {
		t.Fatalf("reading repaired image stream: %v", err)
	}
	if !bytes.Equal(data, []byte{0x7f}) {
		t.Errorf("image stream data not preserved: got %v, want [127]", data)
	}
}

// TestFixGraphics_PreservesFilteredImageData is the realistic case: the image
// data is FlateDecode-compressed. The fixer must carry the raw (still-encoded)
// bytes across byte-for-byte while it only edits the dict.
func TestFixGraphics_PreservesFilteredImageData(t *testing.T) {
	var zbuf bytes.Buffer
	zw := zlib.NewWriter(&zbuf)
	if _, err := zw.Write([]byte{0x10, 0x20, 0x30, 0x40}); err != nil {
		t.Fatalf("zlib write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}
	compressed := zbuf.Bytes()

	doc, alloc := newTestDoc(t, pdfWithImageIntent("Foo", "FlateDecode", compressed))

	if _, err := fixGraphics(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixGraphics: %v", err)
	}

	stream, ok := alloc.Pending()[4].(ast.PdfStream)
	if !ok {
		t.Fatalf("expected image XObject (obj 4) rewritten as a stream, got %T", alloc.Pending()[4])
	}
	if stream.Dict["Intent"] != ast.PdfName("RelativeColorimetric") {
		t.Errorf("image /Intent: got %v, want RelativeColorimetric", stream.Dict["Intent"])
	}
	if stream.Dict["Filter"] != ast.PdfName("FlateDecode") {
		t.Errorf("image /Filter: got %v, want FlateDecode", stream.Dict["Filter"])
	}
	data, err := stream.Bytes(context.Background())
	if err != nil {
		t.Fatalf("reading repaired image stream: %v", err)
	}
	if !bytes.Equal(data, compressed) {
		t.Errorf("filtered stream bytes not preserved: got %d bytes, want %d", len(data), len(compressed))
	}
}
