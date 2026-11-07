package mutations

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// boxedPagePDF builds a one-page document whose page states pageEntries
// (its /MediaBox, /CropBox and /Rotate) and draws one word.
func boxedPagePDF(t *testing.T, pageEntries string) []byte {
	t.Helper()

	body := "BT /F1 12 Tf 50 50 Td (BOXED) Tj ET\n"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R " + pageEntries + " /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
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

// stampFormOf returns the /BBox and /Matrix of the form [pageToFormXObject]
// builds from the one page of a document built by [boxedPagePDF].
func stampFormOf(t *testing.T, pageEntries string) (bbox, matrix [6]float64) {
	t.Helper()

	ctx := context.Background()
	data := boxedPagePDF(t, pageEntries)
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
	alloc := writer.NewObjAllocator(0)
	ref, err := pageToFormXObject(ctx, doc, pages[0], writer.NewCopier(doc, alloc), alloc)
	if err != nil {
		t.Fatalf("pageToFormXObject: %v", err)
	}
	form, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("the form is %T, not a stream", alloc.Pending()[ref.ObjectNum])
	}
	numbers := func(key ast.PdfName, want int) [6]float64 {
		arr, ok := form.Dict[key].(ast.PdfArray)
		if !ok || len(arr) != want {
			t.Fatalf("/%s = %v, want %d numbers", key, form.Dict[key], want)
		}
		var out [6]float64
		for i, v := range arr {
			out[i] = document.Float(v)
		}
		return out
	}
	return numbers("BBox", 4), numbers("Matrix", 6)
}

// transformedCorners maps the four corners of bbox through m and returns the
// rectangle they span.
func transformedCorners(bbox, m [6]float64) [4]float64 {
	lo := [2]float64{1e18, 1e18}
	hi := [2]float64{-1e18, -1e18}
	for _, c := range [][2]float64{{bbox[0], bbox[1]}, {bbox[2], bbox[1]}, {bbox[2], bbox[3]}, {bbox[0], bbox[3]}} {
		x := m[0]*c[0] + m[2]*c[1] + m[4]
		y := m[1]*c[0] + m[3]*c[1] + m[5]
		lo = [2]float64{min(lo[0], x), min(lo[1], y)}
		hi = [2]float64{max(hi[0], x), max(hi[1], y)}
	}
	return [4]float64{lo[0], lo[1], hi[0], hi[1]}
}

// TestPageToFormXObjectDrawsThePageAsViewed pins that the form a PDF stamp
// draws is the source page as a viewer shows it: clipped to its crop box,
// moved so that box starts at the origin, and turned by its /Rotate, so the
// form spans [0 0 W H] with W and H the page's displayed width and height.
// The form had the media box as /BBox and no /Matrix, so a box away from the
// origin moved the stamp by its origin, a rotated page was drawn sideways, and
// what a viewer crops away showed.
func TestPageToFormXObjectDrawsThePageAsViewed(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		entries string
		bbox    [4]float64
		matrix  [6]float64
		spans   [4]float64
	}{
		{
			name:    "an offset box",
			entries: "/MediaBox [20 30 320 530]",
			bbox:    [4]float64{20, 30, 320, 530},
			matrix:  [6]float64{1, 0, 0, 1, -20, -30},
			spans:   [4]float64{0, 0, 300, 500},
		},
		{
			name:    "a quarter turn",
			entries: "/MediaBox [20 30 320 530] /Rotate 90",
			bbox:    [4]float64{20, 30, 320, 530},
			matrix:  [6]float64{0, -1, 1, 0, -30, 320},
			spans:   [4]float64{0, 0, 500, 300},
		},
		{
			name:    "a half turn",
			entries: "/MediaBox [20 30 320 530] /Rotate 180",
			bbox:    [4]float64{20, 30, 320, 530},
			matrix:  [6]float64{-1, 0, 0, -1, 320, 530},
			spans:   [4]float64{0, 0, 300, 500},
		},
		{
			name:    "three quarter turns",
			entries: "/MediaBox [20 30 320 530] /Rotate 270",
			bbox:    [4]float64{20, 30, 320, 530},
			matrix:  [6]float64{0, 1, -1, 0, 530, -20},
			spans:   [4]float64{0, 0, 500, 300},
		},
		{
			name:    "a negative quarter turn, read as 270",
			entries: "/MediaBox [20 30 320 530] /Rotate -90",
			bbox:    [4]float64{20, 30, 320, 530},
			matrix:  [6]float64{0, 1, -1, 0, 530, -20},
			spans:   [4]float64{0, 0, 500, 300},
		},
		{
			name:    "a rotation no multiple of 90, read as 0",
			entries: "/MediaBox [20 30 320 530] /Rotate 45",
			bbox:    [4]float64{20, 30, 320, 530},
			matrix:  [6]float64{1, 0, 0, 1, -20, -30},
			spans:   [4]float64{0, 0, 300, 500},
		},
		{
			name:    "a crop box inside the media box",
			entries: "/MediaBox [0 0 612 792] /CropBox [100 200 400 600]",
			bbox:    [4]float64{100, 200, 400, 600},
			matrix:  [6]float64{1, 0, 0, 1, -100, -200},
			spans:   [4]float64{0, 0, 300, 400},
		},
		{
			name:    "a box naming its corners the other way",
			entries: "/MediaBox [320 530 20 30]",
			bbox:    [4]float64{20, 30, 320, 530},
			matrix:  [6]float64{1, 0, 0, 1, -20, -30},
			spans:   [4]float64{0, 0, 300, 500},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			bbox, matrix := stampFormOf(t, tc.entries)
			if got := [4]float64(bbox[:4]); got != tc.bbox {
				t.Errorf("/BBox = %v, want %v", got, tc.bbox)
			}
			if matrix != tc.matrix {
				t.Errorf("/Matrix = %v, want %v", matrix, tc.matrix)
			}
			if got := transformedCorners(bbox, matrix); got != tc.spans {
				t.Errorf("the form spans %v in user space, want %v", got, tc.spans)
			}
		})
	}
}
