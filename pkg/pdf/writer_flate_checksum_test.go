package pdf

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"io"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// zlibEndingIn returns zlib data (RFC 1950) whose last byte, the low byte of
// its Adler-32 checksum, is last. The payload varies until the checksum
// cooperates, which takes a few hundred tries at most.
func zlibEndingIn(t *testing.T, last byte) []byte {
	t.Helper()
	for i := range 1 << 16 {
		var buf bytes.Buffer
		zw := zlib.NewWriter(&buf)
		if _, err := fmt.Fprintf(zw, "0 0 1 rg %d 10 50 50 re f\n", i); err != nil {
			t.Fatalf("compressing: %v", err)
		}
		if err := zw.Close(); err != nil {
			t.Fatalf("closing: %v", err)
		}
		if b := buf.Bytes(); b[len(b)-1] == last {
			return b
		}
	}
	t.Fatalf("no payload yields zlib data ending in %#x", last)
	return nil
}

// TestWriter_FullRewrite_KeepsFlateChecksum pins that a full rewrite copies a
// FlateDecode stream whole when its zlib checksum ends in CR or LF and no
// end-of-line marker follows it before "endstream".
//
// The reader took that byte for the marker ISO 32000-1, 7.3.8.1 places there
// and dropped it, so every copy carried a checksum a byte short: qpdf warns
// "input stream is complete but output may still be valid" on it. Both ways of
// finding the data's end are covered, a direct /Length and the scan an
// indirect /Length falls back to.
//
// # Corpus evidence
//
//   - pdfcpu golang.pdf: its content stream 20 and font program 88 end their
//     checksum in CR under an indirect /Length, and qpdf warned on both in the
//     output of its PDF/A-1b remediation.
func TestWriter_FullRewrite_KeepsFlateChecksum(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	scanned := zlibEndingIn(t, '\r')
	counted := zlibEndingIn(t, '\n')
	data := buildTestPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents [4 0 R 6 0 R] >>",
		"<< /Length 5 0 R /Filter /FlateDecode >>\nstream\n" + string(scanned) + "endstream",
		strconv.Itoa(len(scanned)),
		fmt.Sprintf("<< /Length %d /Filter /FlateDecode >>\nstream\n%sendstream", len(counted), counted),
	}, "")

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	defer w.Close()
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out, WithFullRewrite()); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if bytes.HasPrefix(out.Bytes(), data) {
		t.Fatal("the output extends the source, so no stream was copied and this proves nothing")
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	defer r2.Close()

	streams := pageContentStreams(ctx, t, r2.doc)
	if len(streams) != 2 {
		t.Fatalf("the output page has %d content streams, want 2", len(streams))
	}
	for i, s := range streams {
		raw, err := s.Bytes(ctx)
		if err != nil {
			t.Fatalf("content stream %d: %v", i, err)
		}
		if err := wholeZlib(raw); err != nil {
			t.Errorf("content stream %d of the output is no longer a whole zlib stream "+
				"(%d bytes): %v", i, len(raw), err)
		}
	}
}

// pageContentStreams returns the /Contents streams of the document's first page.
func pageContentStreams(ctx context.Context, t *testing.T, doc *document.Document) []ast.PdfStream {
	t.Helper()
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolveDict(ctx, doc, catalog.Dict["Pages"])
	if err != nil {
		t.Fatalf("resolving /Pages: %v", err)
	}
	kids, err := document.ResolveArray(ctx, doc, pages["Kids"])
	if err != nil || len(kids) == 0 {
		t.Fatalf("resolving /Kids: %v", err)
	}
	page, err := document.ResolveDict(ctx, doc, kids[0])
	if err != nil {
		t.Fatalf("resolving the page: %v", err)
	}
	contents, err := document.ResolveArray(ctx, doc, page["Contents"])
	if err != nil {
		t.Fatalf("resolving /Contents: %v", err)
	}
	streams := make([]ast.PdfStream, 0, len(contents))
	for _, c := range contents {
		s, err := document.ResolveStream(ctx, doc, c)
		if err != nil {
			t.Fatalf("resolving a content stream: %v", err)
		}
		streams = append(streams, s)
	}
	return streams
}

// wholeZlib reports whether data holds a whole zlib stream, its checksum
// included: compress/zlib verifies the checksum once it reaches the end of the
// deflate data, and fails with io.ErrUnexpectedEOF when bytes of it are
// missing.
func wholeZlib(data []byte) error {
	zr, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	if _, err := io.Copy(io.Discard, zr); err != nil {
		return err
	}
	return zr.Close()
}
