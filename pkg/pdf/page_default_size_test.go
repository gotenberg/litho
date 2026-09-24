package pdf

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// pageBoxPDF builds a one-page document whose page dictionary carries the given
// entries, so a test can leave the media box out or make it degenerate.
func pageBoxPDF(pageEntries string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R " + pageEntries + " >>",
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

// TestPageWithoutMediaBoxReportsLetter pins the size reported for a page that
// declares no media box.
//
// ISO 32000-1, Table 30 requires /MediaBox, so such a file is invalid and the
// spec does not say what to report. Answering 0 by 0 describes a page no viewer
// would draw: poppler reports 612 x 792 for such a page, and pdf.js substitutes
// its LETTER_SIZE_MEDIABOX.
//
// # Corpus evidence
//
//   - govdocs thread_400_400739.pdf: has no /MediaBox anywhere, and poppler
//     reports 612 x 792 for it.
func TestPageWithoutMediaBoxReportsLetter(t *testing.T) {
	data := pageBoxPDF("")
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	p, err := r.Page(context.Background(), 1)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if !almostEqual(p.Width, 612, 0.01) || !almostEqual(p.Height, 792, 0.01) {
		t.Errorf("page measures %v x %v, want 612 x 792", p.Width, p.Height)
	}
	if p.MediaBox != ([4]float64{}) {
		t.Errorf("MediaBox = %v, want the zero rectangle: the fallback must not invent a declared box",
			p.MediaBox)
	}
}

// TestPageWithZeroAreaMediaBoxReportsLetter pins the same for a media box that
// is present but encloses no area.
//
// # Corpus evidence
//
//   - pdfjs boundingBox_invalid.pdf: its first page declares a media box that
//     is present but encloses no area, the shape this test builds.
func TestPageWithZeroAreaMediaBoxReportsLetter(t *testing.T) {
	data := pageBoxPDF("/MediaBox [0 0 0 0]")
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	p, err := r.Page(context.Background(), 1)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if !almostEqual(p.Width, 612, 0.01) || !almostEqual(p.Height, 792, 0.01) {
		t.Errorf("page measures %v x %v, want 612 x 792", p.Width, p.Height)
	}
	if p.MediaBox != ([4]float64{}) {
		t.Errorf("MediaBox = %v, want the declared zero rectangle: the fallback changes only "+
			"the effective dimensions", p.MediaBox)
	}
}

// TestPageWithZeroAreaCropBoxFallsBackToMediaBox pins that a usable media box
// still wins over a degenerate crop box, so the Letter fallback stays reserved
// for pages with no usable box at all.
//
// # Corpus evidence
//
//   - pdfjs boundingBox_invalid.pdf: page 2 declares /MediaBox [0 0 800 600]
//     with /CropBox [0 0 0 0], the shape this test builds, and poppler reports
//     800 x 600 for it.
func TestPageWithZeroAreaCropBoxFallsBackToMediaBox(t *testing.T) {
	data := pageBoxPDF("/MediaBox [0 0 800 600] /CropBox [0 0 0 0]")
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	p, err := r.Page(context.Background(), 1)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if !almostEqual(p.Width, 800, 0.01) || !almostEqual(p.Height, 600, 0.01) {
		t.Errorf("page measures %v x %v, want 800 x 600", p.Width, p.Height)
	}
	if p.MediaBox != ([4]float64{0, 0, 800, 600}) {
		t.Errorf("MediaBox = %v, want the declared [0 0 800 600]", p.MediaBox)
	}
}

// TestUsablePageSizeIsNotOverridden is the control: an ordinary page keeps its
// declared size, including one much smaller than US Letter.
func TestUsablePageSizeIsNotOverridden(t *testing.T) {
	data := pageBoxPDF("/MediaBox [0 0 250 50]")
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	p, err := r.Page(context.Background(), 1)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if !almostEqual(p.Width, 250, 0.01) || !almostEqual(p.Height, 50, 0.01) {
		t.Errorf("page measures %v x %v, want 250 x 50", p.Width, p.Height)
	}
}
