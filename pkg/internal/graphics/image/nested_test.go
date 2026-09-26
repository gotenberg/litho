package image

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// imagePDF builds a one-page document from the given objects and returns its
// single page.
func imagePDF(t *testing.T, objs []string) (context.Context, *document.Document, *document.Page) {
	t.Helper()

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

	data := buf.Bytes()
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
	if err != nil || len(pages) != 1 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	return ctx, doc, pages[0]
}

const imageObj = "<< /Type /XObject /Subtype /Image /Width 8 /Height 8" +
	" /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 0 >>\nstream\n\nendstream"

// TestExtractImagesRecursiveFindsNestedImages pins that an image drawn through
// a form XObject is reported.
//
// ISO 32000-1, 8.10.1 lets a page draw its content through a form XObject, and
// producers put images there routinely. Reporting only the page's own resources
// missed them, down to pages whose every image sat inside a form.
//
// # Corpus evidence
//
//   - All corpora: across the pdfjs and govdocs corpora, reporting only the
//     page's own resources under-reported 583 pages, including one where every
//     one of 580 images sat inside a form.
func TestExtractImagesRecursiveFindsNestedImages(t *testing.T) {
	t.Parallel()

	ctx, doc, page := imagePDF(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200]" +
			" /Resources << /XObject << /Fm0 4 0 R >> >> >>",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 100 100] /Length 0" +
			" /Resources << /XObject << /Im0 5 0 R >> >> >>\nstream\n\nendstream",
		imageObj,
	})

	if got := ExtractImages(ctx, doc, page); len(got) != 0 {
		t.Errorf("the shallow scan reported %d images, want 0: it must stay page-level for OCR triage", len(got))
	}
	got := ExtractImagesRecursive(ctx, doc, page)
	if len(got) != 1 {
		t.Fatalf("reported %d images, want 1: the image is drawn through a form", len(got))
	}
	if got[0].Width != 8 || got[0].Height != 8 {
		t.Errorf("image measures %dx%d, want 8x8", got[0].Width, got[0].Height)
	}
}

// TestExtractImagesRecursiveKeepsBothNames pins that an image named twice in
// one resource dictionary stays two entries, which is what a listing keyed by
// resource name has always reported.
//
// # Corpus evidence
//
//   - pdfjs 160F-2019.pdf: names object 394 as both /Im0 and /Im1.
func TestExtractImagesRecursiveKeepsBothNames(t *testing.T) {
	t.Parallel()

	ctx, doc, page := imagePDF(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200]" +
			" /Resources << /XObject << /Im0 4 0 R /Im1 4 0 R >> >> >>",
		imageObj,
	})

	got := ExtractImagesRecursive(ctx, doc, page)
	if len(got) != 2 {
		t.Fatalf("reported %d images, want 2: both names are placements", len(got))
	}
	if got[0].Filename == got[1].Filename {
		t.Errorf("both entries report the same name %q", got[0].Filename)
	}
}

// TestExtractImagesRecursiveWalksAFormOnce pins that a form invoked under two
// names contributes its images once, so the count does not double.
func TestExtractImagesRecursiveWalksAFormOnce(t *testing.T) {
	t.Parallel()

	ctx, doc, page := imagePDF(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200]" +
			" /Resources << /XObject << /Fm0 4 0 R /Fm1 4 0 R >> >> >>",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 100 100] /Length 0" +
			" /Resources << /XObject << /Im0 5 0 R >> >> >>\nstream\n\nendstream",
		imageObj,
	})

	if got := ExtractImagesRecursive(ctx, doc, page); len(got) != 1 {
		t.Errorf("reported %d images, want 1: the same form was walked twice", len(got))
	}
}

// TestExtractImagesRecursiveSurvivesASelfReferencingForm pins that a form
// naming itself terminates rather than recursing without end.
func TestExtractImagesRecursiveSurvivesASelfReferencingForm(t *testing.T) {
	t.Parallel()

	ctx, doc, page := imagePDF(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200]" +
			" /Resources << /XObject << /Fm0 4 0 R >> >> >>",
		// The form lists itself alongside the image.
		"<< /Type /XObject /Subtype /Form /BBox [0 0 100 100] /Length 0" +
			" /Resources << /XObject << /Fm0 4 0 R /Im0 5 0 R >> >> >>\nstream\n\nendstream",
		imageObj,
	})

	done := make(chan int, 1)
	go func() { done <- len(ExtractImagesRecursive(ctx, doc, page)) }()
	if n := <-done; n != 1 {
		t.Errorf("reported %d images, want 1", n)
	}
}

// TestExtractImagesRecursiveSkipsAnImageWithNoSize is the control: a malformed
// image states no usable dimensions and is not reportable.
//
// # Corpus evidence
//
//   - pdfjs issue4575.pdf: writes /Width /Height with no height at all.
func TestExtractImagesRecursiveSkipsAnImageWithNoSize(t *testing.T) {
	t.Parallel()

	ctx, doc, page := imagePDF(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200]" +
			" /Resources << /XObject << /I1 4 0 R >> >> >>",
		"<< /Type /XObject /Subtype /Image /Width /Height /ColorSpace /DeviceGray" +
			" /BitsPerComponent 8 /Length 0 >>\nstream\n\nendstream",
	})

	if got := ExtractImagesRecursive(ctx, doc, page); len(got) != 0 {
		t.Errorf("reported %d images, want 0: the image states no height", len(got))
	}
}
