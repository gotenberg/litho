package annotation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// buildLinkPDF assembles a two-page document from object bodies (1-indexed).
func buildLinkPDF(objs ...string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", o)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// annotsOfFirstPage extracts the annotations of page 1 of data.
func annotsOfFirstPage(t *testing.T, data []byte) []struct {
	Subtype string
	Dest    int
} {
	t.Helper()

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}
	if len(pages) == 0 {
		t.Fatal("document has no pages")
	}
	got, err := ExtractAnnotations(ctx, doc, pages[0], document.BuildPageMap(pages))
	if err != nil {
		t.Fatalf("ExtractAnnotations: %v", err)
	}
	out := make([]struct {
		Subtype string
		Dest    int
	}, len(got))
	for i, a := range got {
		out[i].Subtype = a.Subtype
		out[i].Dest = a.DestPage
	}
	return out
}

// TestLinkAnnotationResolvesNamedDestinations pins that a link to a named
// destination reports the page it points at.
//
// The outline walk resolved named destinations and the annotation walk did not,
// so the same document answered the question one way for its bookmarks and
// another for its links. Eleven pdfjs files land here, including
// link-annotation-border.pdf whose 26 links all reported no page, and
// issue14864.pdf whose destination name carries non-ASCII text.
func TestLinkAnnotationResolvesNamedDestinations(t *testing.T) {
	t.Parallel()

	data := buildLinkPDF(
		"<< /Type /Catalog /Pages 2 0 R /Names 6 0 R /Dests 7 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [8 0 R 9 0 R 10 0 R 11 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Dests << /Names [(target) [4 0 R /Fit]] >> >>",
		"<< /Legacy [4 0 R /Fit] >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /Dest (target) >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /A << /S /GoTo /D (target) >> >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /Dest /Legacy >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /A << /S /GoTo /D [4 0 R /Fit] >> >>",
	)

	got := annotsOfFirstPage(t, data)
	if len(got) != 4 {
		t.Fatalf("extracted %d annotations, want 4", len(got))
	}
	for i, a := range got {
		if a.Subtype != "Link" {
			t.Errorf("annotation %d: subtype %q, want Link", i, a.Subtype)
		}
		if a.Dest != 2 {
			t.Errorf("annotation %d: destination page %d, want 2", i, a.Dest)
		}
	}
}

// TestAnnotsNullReadsAsNoAnnotations pins that /Annots null is the same as no
// /Annots at all. Per ISO 32000-1, 7.3.7 a dictionary entry whose value is null
// is equivalent to the entry being absent; reporting an error made litho the
// only reader that could not list annotations for pdfjs issue11287.pdf.
func TestAnnotsNullReadsAsNoAnnotations(t *testing.T) {
	t.Parallel()

	data := buildLinkPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots null >>",
	)

	if got := annotsOfFirstPage(t, data); len(got) != 0 {
		t.Errorf("got %d annotations from /Annots null, want 0", len(got))
	}
}
