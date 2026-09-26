package mutations

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// contentsRefToArrayPDF builds a one-page document whose /Contents is an
// INDIRECT REFERENCE naming an ARRAY of two streams.
//
// That is the spelling stampSourcePDF cannot express: it writes /Contents as a
// reference to a single stream, and the array case in [collectPageContents] was
// only ever reached by a DIRECT array. ISO 32000-1, 7.7.3.3 allows /Contents to
// be a stream or an array of streams, and 7.3.10 allows any object to be
// written indirectly, so `/Contents 4 0 R` naming an array is ordinary.
func contentsRefToArrayPDF(t *testing.T) []byte {
	t.Helper()

	first := "BT /F1 24 Tf 50 400 Td (FIRSTHALF) Tj ET\n"
	second := "BT /F1 24 Tf 50 300 Td (SECONDHALF) Tj ET\n"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /Font << /F1 7 0 R >> >> >>",
		"[5 0 R 6 0 R]",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(first), first),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(second), second),
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

// TestCollectPageContents_ContentsRefNamingAnArray pins the construct that
// refused a legal page.
//
// [collectPageContents] switched on the UNRESOLVED /Contents value, so every
// `/Contents N 0 R` took the stream branch and a reference naming an array died
// in ResolveStream as "expected PdfStream, got ast.PdfArray". Because all three
// callers share this function, redaction, [ExcisePageSpans] and stamp-pdf each
// refused such a page.
//
// Both halves must arrive: a fix that resolved the reference but then read only
// the first stream would leave the page half-assembled, and the byte spans
// redaction computes against this buffer would address the wrong content.
//
// # Corpus evidence
//
//   - govdocs: 26 of 1091 documents carry a /Contents reference naming an
//     array, every one of them a file litho otherwise reads.
func TestCollectPageContents_ContentsRefNamingAnArray(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := contentsRefToArrayPDF(t)

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
		t.Fatalf("collectPageContents refused a /Contents reference naming an array: %v", err)
	}

	if !bytes.Contains(got, []byte("(FIRSTHALF) Tj")) {
		t.Errorf("the first stream is missing from the assembled content: %q", got)
	}
	if !bytes.Contains(got, []byte("(SECONDHALF) Tj")) {
		t.Errorf("the second stream is missing from the assembled content: %q", got)
	}

	// The separator is load-bearing: ExcisePageSpans and RedactPageRegions
	// address byte offsets in exactly this buffer, so the streams must be
	// joined the way the array branch has always joined them.
	if !bytes.Contains(got, []byte("ET\n\nBT")) && !bytes.Contains(got, []byte("ET\nBT")) {
		t.Errorf("the two streams are not newline-separated, so recorded byte offsets shift: %q", got)
	}
}
