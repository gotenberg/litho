package compliance

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// flateStream returns a content stream object holding n bytes of compressible
// filler. The point is a small file whose decoded content is large: that is
// what makes the accumulated buffer worth bounding, and it keeps the fixture
// tiny (a megabyte of spaces deflates to about a kilobyte).
func flateStream(t *testing.T, n int) string {
	t.Helper()

	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write([]byte(strings.Repeat(" ", n))); err != nil {
		t.Fatalf("deflating filler: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("closing deflate writer: %v", err)
	}
	return fmt.Sprintf("<< /Length %d /Filter /FlateDecode >>\nstream\n%s\nendstream",
		buf.Len(), buf.String())
}

// TestPageContentBytesBoundsTheAccumulatedBuffer asserts a page cannot make
// [resolvePageContentBytes] accumulate without limit.
//
// Per ISO 32000-1, 7.7.3.3 a page's content may be split across an array of
// streams. The per-stream decompression cap bounds each part but says nothing
// about their sum, so before [content.MaxPageContentSize] applied here, a page
// naming enough parts grew this buffer with the file's object count rather than
// with anything it could legitimately draw. Every page-scoped check reads this
// buffer, so the growth happened on the ordinary validation path.
func TestPageContentBytesBoundsTheAccumulatedBuffer(t *testing.T) {
	t.Parallel()

	// Parts of 4 MB each, enough of them to pass the cap.
	const part = 4 << 20
	parts := content.MaxPageContentSize/part + 2

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"", // placeholder for the page, filled in below once refs are known
	}
	var refs []string
	for i := range parts {
		refs = append(refs, fmt.Sprintf("%d 0 R", 4+i))
	}
	objs[2] = fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents [%s] >>",
		strings.Join(refs, " "))
	for range parts {
		objs = append(objs, flateStream(t, part))
	}

	doc, cat := objsDocPDF(t, objs...)
	ctx := context.Background()
	page, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil || page == nil {
		t.Fatalf("resolving page: %v", err)
	}
	if cat == nil {
		t.Fatal("no catalog")
	}

	data, err := resolvePageContentBytes(ctx, doc, page)
	if err == nil {
		t.Fatalf("a page declaring %d MB of content was accumulated in full (%d bytes); want the limit to stop it",
			parts*part>>20, len(data))
	}
	if len(data) != 0 {
		t.Errorf("rejected page still returned %d bytes", len(data))
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error %q does not report the limit", err)
	}
}

// TestPageContentBytesKeepsALargeLegitimatePage guards the other side: the
// limit must sit well clear of the heaviest real pages rather than turning a
// legitimate document into one litho cannot read.
//
// # Corpus evidence
//
//   - All corpora: the heaviest real page holds 27.5 MB of decoded content,
//     which the 28 MB fixture just passes.
func TestPageContentBytesKeepsALargeLegitimatePage(t *testing.T) {
	t.Parallel()

	const part = 4 << 20
	const parts = 7 // 28 MB, just past the heaviest real page

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"",
	}
	var refs []string
	for i := range parts {
		refs = append(refs, fmt.Sprintf("%d 0 R", 4+i))
	}
	objs[2] = fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents [%s] >>",
		strings.Join(refs, " "))
	for range parts {
		objs = append(objs, flateStream(t, part))
	}

	doc, _ := objsDocPDF(t, objs...)
	ctx := context.Background()
	page, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if err != nil || page == nil {
		t.Fatalf("resolving page: %v", err)
	}

	data, err := resolvePageContentBytes(ctx, doc, page)
	if err != nil {
		t.Fatalf("a %d MB page was rejected: %v", parts*part>>20, err)
	}
	if len(data) < parts*part {
		t.Errorf("accumulated %d bytes, want at least %d", len(data), parts*part)
	}
}
