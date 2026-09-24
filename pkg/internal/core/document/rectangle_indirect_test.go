package document

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// buildIndirectBoxPDF assembles a one-page document from the given page
// dictionary body plus the four coordinate objects a rectangle may reference.
func buildIndirectBoxPDF(pageBody string, extra []string) []byte {
	objs := append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		pageBody,
	}, extra...)

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

func onlyPage(t *testing.T, data []byte) *Page {
	t.Helper()

	ctx := context.Background()
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("resolved %d pages, want 1", len(pages))
	}
	return pages[0]
}

// TestMediaBoxWithIndirectCoordinates pins that a rectangle whose coordinates
// are indirect references measures correctly.
//
// ISO 32000-1, 7.3.9 allows an indirect reference wherever a direct object may
// appear, including inside an array. Read verbatim, such coordinates are
// references rather than numbers, so the page measured 0 by 0.
//
// # Corpus evidence
//
//   - pdfjs issue7872.pdf: writes /MediaBox [4 0 R 5 0 R 6 0 R 7 0 R].
func TestMediaBoxWithIndirectCoordinates(t *testing.T) {
	t.Parallel()

	page := onlyPage(t, buildIndirectBoxPDF(
		"<< /Type /Page /Parent 2 0 R /MediaBox [4 0 R 5 0 R 6 0 R 7 0 R] >>",
		[]string{"0", "0", "250", "50"},
	))

	if len(page.MediaBox) != 4 {
		t.Fatalf("media box has %d entries, want 4", len(page.MediaBox))
	}
	llx, lly := Float(page.MediaBox[0]), Float(page.MediaBox[1])
	urx, ury := Float(page.MediaBox[2]), Float(page.MediaBox[3])
	if urx-llx != 250 || ury-lly != 50 {
		t.Errorf("media box measures %v x %v, want 250 x 50: the coordinates stayed unresolved",
			urx-llx, ury-lly)
	}
}

// TestCropBoxWithIndirectCoordinates pins the same for the crop box, which is
// what a viewer actually shows.
func TestCropBoxWithIndirectCoordinates(t *testing.T) {
	t.Parallel()

	page := onlyPage(t, buildIndirectBoxPDF(
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 250 50]"+
			" /CropBox [4 0 R 5 0 R 6 0 R 7 0 R] >>",
		[]string{"0", "0", "200", "40"},
	))

	if len(page.CropBox) != 4 {
		t.Fatalf("crop box has %d entries, want 4", len(page.CropBox))
	}
	urx, ury := Float(page.CropBox[2]), Float(page.CropBox[3])
	if urx != 200 || ury != 40 {
		t.Errorf("crop box upper right is (%v, %v), want (200, 40)", urx, ury)
	}
}

// TestTrimBoxWithIndirectCoordinates covers the non-inheritable boxes, which
// reach the page through a different helper.
func TestTrimBoxWithIndirectCoordinates(t *testing.T) {
	t.Parallel()

	page := onlyPage(t, buildIndirectBoxPDF(
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 250 50]"+
			" /TrimBox [4 0 R 5 0 R 6 0 R 7 0 R] >>",
		[]string{"5", "5", "245", "45"},
	))

	if len(page.TrimBox) != 4 {
		t.Fatalf("trim box has %d entries, want 4", len(page.TrimBox))
	}
	if got := Float(page.TrimBox[2]); got != 245 {
		t.Errorf("trim box urx = %v, want 245", got)
	}
}

// TestRotateIndirect pins that an indirect /Rotate is honoured rather than
// silently read as 0.
func TestRotateIndirect(t *testing.T) {
	t.Parallel()

	page := onlyPage(t, buildIndirectBoxPDF(
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 250 50] /Rotate 4 0 R >>",
		[]string{"90"},
	))

	if page.Rotate != 90 {
		t.Errorf("/Rotate = %d, want 90: the indirect entry was ignored", page.Rotate)
	}
}

// TestDirectRectangleUnchanged is the control: the ordinary rectangle must keep
// working, and keep its backing array.
func TestDirectRectangleUnchanged(t *testing.T) {
	t.Parallel()

	page := onlyPage(t, buildIndirectBoxPDF(
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Rotate 180 >>",
		nil,
	))

	urx, ury := Float(page.MediaBox[2]), Float(page.MediaBox[3])
	if urx != 612 || ury != 792 {
		t.Errorf("media box upper right is (%v, %v), want (612, 792)", urx, ury)
	}
	if page.Rotate != 180 {
		t.Errorf("/Rotate = %d, want 180", page.Rotate)
	}
}
