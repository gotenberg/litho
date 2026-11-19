package pdf

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestWithStreamCompression pins that a full rewrite given the option
// compresses a plain content stream, which draws the same, and leaves the
// XMP metadata plain (ISO 19005-1, 6.7.2).
func TestWithStreamCompression(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	content := strings.Repeat("BT /F1 12 Tf 72 700 Td (A line of text) Tj ET\n", 40)
	xmp := `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?><x:xmpmeta xmlns:x="adobe:ns:meta/"/><?xpacket end="w"?>`
	data := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /Metadata 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(xmp), xmp),
	})
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	var out bytes.Buffer
	if err := NewWriter(r).WriteTo(ctx, &out, WithFullRewrite(), WithStreamCompression()); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if out.Len() >= len(data) {
		t.Errorf("the output weighs %d bytes, the source %d", out.Len(), len(data))
	}
	if !bytes.Contains(out.Bytes(), []byte(xmp)) {
		t.Error("the XMP metadata is no longer plain")
	}
	if bytes.Contains(out.Bytes(), []byte("(A line of text) Tj")) {
		t.Error("the content stream is still plain")
	}
	if got := pageContent(ctx, t, out.Bytes(), 1); strings.TrimSpace(got) != strings.TrimSpace(content) {
		t.Error("the compressed content decodes to other operators")
	}
}
