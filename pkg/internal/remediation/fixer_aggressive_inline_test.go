package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// buildContentDoc assembles a minimal single-page PDF whose page /Contents is the
// given content stream, computing xref offsets so [newTestDoc] can parse it.
func buildContentDoc(t *testing.T, content string) []byte {
	t.Helper()
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// TestFixContentStreams_PreservesInlineImage guards against corrupting an inline
// image. The content parser drops an inline image's binary data off-token, so
// re-serialising a content stream that contains one would emit invalid
// "<dict> BI" syntax and lose the image. Such a stream must be left untouched
// even when it also carries a fixable defect (here an invalid rendering intent).
func TestFixContentStreams_PreservesInlineImage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	content := "/Foo ri\nBI /W 2 /H 2 /BPC 8 /CS /G ID \x00\xff\xff\x00 EI\n"
	doc, alloc := newTestDoc(t, buildContentDoc(t, content))

	if _, err := fixContentStreams(ctx, alloc, doc, true, true); err != nil {
		t.Fatalf("fixContentStreams: %v", err)
	}
	if _, ok := alloc.Pending()[4]; ok {
		t.Error("a content stream containing an inline image must not be re-serialized")
	}
}
