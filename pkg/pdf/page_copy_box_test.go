package pdf

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// runOnBoxPDF builds a two-page document whose boxes are no rectangles. Page 1
// states a /MediaBox that runs on into its content stream, [0 0 612 792 5 0 R].
// Page 2 inherits that shape from its /Pages node, with a /CropBox whose second
// entry is a name, [0 /Foo 300 400].
func runOnBoxPDF() []byte {
	stream := func(word string) string {
		content := fmt.Sprintf("BT /F1 12 Tf 50 300 Td (%s) Tj ET", word)
		return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content)
	}
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 /Resources << /Font << /F1 8 0 R >> >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792 5 0 R] /Contents 5 0 R >>",
		"<< /Type /Page /Parent 6 0 R /Contents 7 0 R >>",
		stream("Alpha"),
		"<< /Type /Pages /Parent 2 0 R /Kids [4 0 R] /Count 1 /MediaBox [0 0 612 792 7 0 R] /CropBox [0 /Foo 300 400] >>",
		stream("Bravo"),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	})
}

// pageBoxes returns, for each page of data, the /MediaBox and /CropBox its
// dictionary states, nil where it states none.
func pageBoxes(t *testing.T, data []byte) (media, crop []ast.PdfArray) {
	t.Helper()
	o := readOutputTree(t, data)
	for _, page := range o.pages {
		m, _ := document.ResolveArray(o.ctx, o.doc, page.Dict["MediaBox"])
		c, _ := document.ResolveArray(o.ctx, o.doc, page.Dict["CropBox"])
		media = append(media, m)
		crop = append(crop, c)
	}
	return media, crop
}

// isRectangle reports whether box is four numbers (ISO 32000-1, 7.9.5).
func isRectangle(box ast.PdfArray) bool {
	if len(box) != 4 {
		return false
	}
	for _, v := range box {
		if _, ok := document.Number(v); !ok {
			return false
		}
	}
	return true
}

// TestPageCopies_WriteRectangleBoxes pins that a page copied into a new page
// tree is written with page boundaries that are rectangles (ISO 32000-1,
// 7.9.5, Table 30). The copy materialised the region a page shows as a
// /CropBox whenever it differed from its /MediaBox, and a /MediaBox that is no
// rectangle never matched, so it was written whole as the /CropBox,
// [0 0 612 792 5 0 R] with a copy of the content stream it runs on into. Page
// 1 declares no crop box, and gets none. Page 2 inherits [0 /Foo 300 400],
// which pdf.js and poppler read as no box, and gets none either: each page
// shows US Letter, as a viewer shows the source, since neither /MediaBox is a
// box a viewer uses (see [document.UsableBox]).
func TestPageCopies_WriteRectangleBoxes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := runOnBoxPDF()

	var split, merged bytes.Buffer
	if err := Split(ctx, openBytes(t, src), []int{1, 2}, &split); err != nil {
		t.Fatalf("Split: %v", err)
	}
	// The other document is US Letter, so the pages with no media box a
	// viewer uses take the size a viewer shows them at (see
	// WithDefaultPageSize).
	if err := Merge(ctx, []*Reader{openBytes(t, letterPDF()), openBytes(t, src)}, &merged); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	inserted := writeLabelled(t, letterPDF(), func(w *Writer) error {
		return w.InsertPages(1, openBytes(t, src))
	})
	moved := writeLabelled(t, src, func(w *Writer) error {
		return w.DeletePages(1)
	})

	for _, tc := range []struct {
		name  string
		data  []byte
		first int
		crops [][]float64
	}{
		{"split", split.Bytes(), 0, [][]float64{nil, nil}},
		{"merge", merged.Bytes(), 1, [][]float64{nil, nil}},
		{"insert", inserted, 1, [][]float64{nil, nil}},
		{"delete", moved, 0, [][]float64{nil}},
	} {
		for i, size := range pageSizes(t, tc.data)[tc.first:] {
			if size != [2]float64{612, 792} {
				t.Errorf("%s: page %d measures %v, want US Letter, as the source page measures", tc.name, tc.first+i+1, size)
			}
		}
		media, crop := pageBoxes(t, tc.data)
		for i, box := range media {
			if !isRectangle(box) {
				t.Errorf("%s: page %d states /MediaBox %v, want a rectangle", tc.name, i+1, box)
			}
		}
		for i, want := range tc.crops {
			got := crop[tc.first+i]
			if want == nil {
				if got != nil {
					t.Errorf("%s: page %d states /CropBox %v, want none", tc.name, tc.first+i+1, got)
				}
				continue
			}
			values := make([]float64, 0, len(got))
			for _, v := range got {
				n, _ := document.Number(v)
				values = append(values, n)
			}
			if !isRectangle(got) || !slices.Equal(values, want) {
				t.Errorf("%s: page %d states /CropBox %v, want %v", tc.name, tc.first+i+1, got, want)
			}
		}
	}
}

// TestStampPDF_ClipsToThePageAViewerDraws pins that the form a PDF stamp builds
// from a source page is clipped to the page a viewer draws, its /BBox (ISO
// 32000-1, 8.10.1): the source's /MediaBox when a viewer uses it, and the US
// Letter page a viewer draws in its place when it does not (see
// [document.UsableBox]). The /BBox was the rectangle ISO 19005-2, 6.1.13
// measures the box as: [0 /Foo 200 300] clipped the stamp to 200 by 300,
// [0 0 0 0] clipped all of it away, and a page with no /MediaBox refused to
// stamp. It is four numbers either way (ISO 32000-1, Table 95): [0 0 200 6 0 R],
// naming a dictionary, was once written as it stood, a reference to an object
// of the source that named another object in the stamped document.
//
// # Corpus evidence
//
//   - pdfjs boundingBox_invalid.pdf: page 1 states /MediaBox [0 0 0 0], and
//     its stamp was clipped away whole.
//   - All corpora: StampPDF refused 10 pages in 7 files, whose media box is
//     none a viewer uses or absent, among them govdocs thread_400_400739.pdf,
//     pdfjs issue9105_other.pdf and litho pageboxes_2b.pdf.
func TestStampPDF_ClipsToThePageAViewerDraws(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const content = "BT /F1 12 Tf 10 10 Td (Stamp) Tj ET"
	src := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R 8 0 R 9 0 R 10 0 R 11 0 R] /Count 6 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 /Foo 200 300] /Contents 5 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 6 0 R] /Contents 5 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Kind /NotANumber >>",
		"300",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 0 0] /Contents 5 0 R >>",
		"<< /Type /Page /Parent 2 0 R /Contents 5 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 7 0 R] /Contents 5 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 300] /Contents 5 0 R >>",
	})
	letter := []float64{0, 0, 612, 792}
	for _, tc := range []struct {
		page int
		want []float64
	}{
		{1, letter},
		{2, letter},
		{3, letter},
		{4, letter},
		{5, []float64{0, 0, 200, 300}},
		{6, []float64{0, 0, 200, 300}},
	} {
		out := writeLabelled(t, labelledPDF(1, ""), func(w *Writer) error {
			return w.StampPDF(PDFStamp{Source: openBytes(t, src), Page: tc.page})
		})
		o := readOutputTree(t, out)
		xobjects := o.dict(o.dict(o.pages[0].Dict["Resources"])["XObject"])
		found := false
		for name, ref := range xobjects {
			form, err := document.ResolveStream(ctx, o.doc, ref)
			if err != nil {
				t.Fatalf("page %d: resolving %s: %v", tc.page, name, err)
			}
			bbox, _ := form.Dict["BBox"].(ast.PdfArray)
			values := make([]float64, 0, len(bbox))
			for _, v := range bbox {
				n, _ := document.Number(v)
				values = append(values, n)
			}
			if !isRectangle(bbox) || !slices.Equal(values, tc.want) {
				t.Errorf("page %d: the stamp's /BBox is %v, want %v", tc.page, form.Dict["BBox"], tc.want)
			}
			found = true
		}
		if !found {
			t.Errorf("page %d: the stamped page names no form", tc.page)
		}
	}
}
