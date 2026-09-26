package mutations

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// stampSourcePDF builds a one-page document that draws a single word, with its
// content stream compressed when asked.
func stampSourcePDF(t *testing.T, compress bool) []byte {
	t.Helper()

	body := []byte("BT /F1 24 Tf 50 400 Td (STAMPWORD) Tj ET\n")
	entry := "<< /Length %d >>\nstream\n%s\nendstream"
	if compress {
		var deflated bytes.Buffer
		zw := zlib.NewWriter(&deflated)
		if _, err := zw.Write(body); err != nil {
			t.Fatalf("deflating content: %v", err)
		}
		if err := zw.Close(); err != nil {
			t.Fatalf("closing deflate writer: %v", err)
		}
		body = deflated.Bytes()
		entry = "<< /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream"
	}

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf(entry, len(body), body),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
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

func collectedStampContents(t *testing.T, compress bool) []byte {
	t.Helper()

	ctx := context.Background()
	data := stampSourcePDF(t, compress)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	got, err := collectPageContents(ctx, doc, pages[0])
	if err != nil {
		t.Fatalf("collectPageContents: %v", err)
	}
	return got
}

// TestCollectPageContentsDecodesTheStampPage pins that the stamp source's
// content arrives decoded.
//
// The collected bytes go into a form XObject that declares no /Filter, so they
// must be plain content. [ast.PdfStream.Bytes] returns the body before filter
// decoding, so a stamp PDF with a compressed page, which is nearly every real
// one, was embedded as deflate output inside an unfiltered stream and drew
// nothing: stamp-pdf silently produced an unstamped document.
func TestCollectPageContentsDecodesTheStampPage(t *testing.T) {
	t.Parallel()

	got := collectedStampContents(t, true)
	if !bytes.Contains(got, []byte("(STAMPWORD) Tj")) {
		t.Errorf("the stamp content was not decoded, so the stamp draws nothing: %q", got)
	}
}

// TestCollectPageContentsReadsAPlainStampPage is the control: the same page
// stored uncompressed, which pins that compression is the only difference.
func TestCollectPageContentsReadsAPlainStampPage(t *testing.T) {
	t.Parallel()

	got := collectedStampContents(t, false)
	if !bytes.Contains(got, []byte("(STAMPWORD) Tj")) {
		t.Errorf("the uncompressed stamp content did not survive collection: %q", got)
	}
}
