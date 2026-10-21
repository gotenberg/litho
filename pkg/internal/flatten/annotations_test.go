package flatten

import (
	"context"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// annotationsFixture holds annotations at objects 7 to 13 over an opaque
// appearance (object 14) or one painting with /GS1 (object 15):
//
//   - 7, a square with /CA 0.5, baked;
//   - 8, a square whose appearance paints with transparency, baked;
//   - 9, an opaque printed square apart from the others, left;
//   - 10, a text field widget with /CA 0.5, left;
//   - 11, a hidden square with /CA 0.5, emptied;
//   - 12, an open pop-up with /CA 0.5, left;
//   - 13, a square with /CA 0.5 under optional content, left.
func annotationsFixture() fixture {
	square := func(rect, extra string, ap int) string {
		return "<< /Type /Annot /Subtype /Square /Rect [" + rect + "] " + extra + " /AP << /N " + strconv.Itoa(ap) + " 0 R >> >>"
	}
	return fixture{
		page:    "/Annots [7 0 R 8 0 R 9 0 R 10 0 R 11 0 R 12 0 R 13 0 R]",
		content: "0 0 1 rg 0 0 100 100 re f",
		objs: []string{
			square("10 10 20 20", "/F 4 /CA 0.5", 14),
			square("30 10 40 20", "/F 4", 15),
			square("80 80 90 90", "/F 4", 14),
			"<< /Type /Annot /Subtype /Widget /FT /Tx /T (f) /Rect [50 10 60 20] /F 4 /CA 0.5 /AP << /N 14 0 R >> >>",
			square("10 30 20 40", "/F 6 /CA 0.5", 14),
			"<< /Type /Annot /Subtype /Popup /Open true /Rect [30 30 40 40] /F 4 /CA 0.5 /AP << /N 14 0 R >> >>",
			square("50 30 60 40", "/F 4 /CA 0.5 /OC << /Type /OCG /Name (x) >>", 14),
			stream("/Type /XObject /Subtype /Form /BBox [0 0 10 10]", "1 0 0 rg 0 0 10 10 re f"),
			stream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << /ExtGState << /GS1 6 0 R >> >>", "/GS1 gs 1 0 0 rg 0 0 10 10 re f"),
		},
	}
}

// bakeAnnotations runs [Annotations] over data's first page and returns the
// report and the annotations it rewrote.
func bakeAnnotations(t *testing.T, data []byte, opts AnnotationOptions) (*AnnotationReport, map[int]bool) {
	t.Helper()
	ctx := context.Background()
	doc, pg := open(t, data)
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	report, err := Annotations(ctx, alloc, doc, []*document.Page{pg}, opts)
	if err != nil {
		t.Fatalf("Annotations: %v", err)
	}
	rewritten := make(map[int]bool)
	for num, obj := range alloc.Pending() {
		if d, ok := obj.(ast.PdfDict); ok && d["Type"] == ast.PdfName("Annot") {
			rewritten[num] = true
		}
	}
	return report, rewritten
}

func TestAnnotationsBakesTransparentOnes(t *testing.T) {
	t.Parallel()
	report, rewritten := bakeAnnotations(t, annotationsFixture().bytes(), AnnotationOptions{})
	if report.Baked[0] != 2 || report.Emptied[0] != 1 || report.Signatures {
		t.Errorf("report %+v, want 2 baked, 1 emptied", report)
	}
	for _, num := range []int{7, 8, 11} {
		if !rewritten[num] {
			t.Errorf("annotation %d left as it was", num)
		}
	}
	for _, num := range []int{9, 10, 12, 13} {
		if rewritten[num] {
			t.Errorf("annotation %d rewritten", num)
		}
	}
}

func TestAnnotationsBakesPrintedOnes(t *testing.T) {
	t.Parallel()
	report, rewritten := bakeAnnotations(t, annotationsFixture().bytes(), AnnotationOptions{Printed: true})
	if report.Baked[0] != 3 {
		t.Errorf("baked %d, want 3", report.Baked[0])
	}
	if !rewritten[9] {
		t.Error("the opaque printed square is left with WithPrinted")
	}
	if rewritten[10] {
		t.Error("the widget is baked")
	}
}

// TestAnnotationsKeepsTheDrawingOrder pins that an annotation drawn under
// one that is baked is baked with it: drawn into the content, the later one
// would otherwise go under the earlier one, which a viewer drew below it.
func TestAnnotationsKeepsTheDrawingOrder(t *testing.T) {
	t.Parallel()
	in := fixture{
		page: "/Annots [7 0 R 8 0 R 9 0 R]",
		objs: []string{
			"<< /Type /Annot /Subtype /Square /Rect [10 10 50 50] /F 4 /AP << /N 10 0 R >> >>",
			"<< /Type /Annot /Subtype /Square /Rect [60 60 90 90] /F 4 /AP << /N 10 0 R >> >>",
			"<< /Type /Annot /Subtype /Square /Rect [30 30 70 70] /F 4 /CA 0.5 /AP << /N 10 0 R >> >>",
			stream("/Type /XObject /Subtype /Form /BBox [0 0 10 10]", "1 0 0 rg 0 0 10 10 re f"),
		},
	}
	report, rewritten := bakeAnnotations(t, in.bytes(), AnnotationOptions{})
	if report.Baked[0] != 3 || !rewritten[7] || !rewritten[8] || !rewritten[9] {
		t.Errorf("report %+v, rewritten %v: want all three baked", report, rewritten)
	}

	apart := fixture{
		page: "/Annots [7 0 R 8 0 R]",
		objs: []string{
			"<< /Type /Annot /Subtype /Square /Rect [10 10 20 20] /F 4 /AP << /N 9 0 R >> >>",
			"<< /Type /Annot /Subtype /Square /Rect [60 60 90 90] /F 4 /CA 0.5 /AP << /N 9 0 R >> >>",
			stream("/Type /XObject /Subtype /Form /BBox [0 0 10 10]", "1 0 0 rg 0 0 10 10 re f"),
		},
	}
	if report, rewritten := bakeAnnotations(t, apart.bytes(), AnnotationOptions{}); report.Baked[0] != 1 || rewritten[7] {
		t.Errorf("report %+v, rewritten %v: want only the transparent square baked", report, rewritten)
	}
}

// TestAnnotationsEmptiesOneWithNoRect pins that a transparent annotation
// with no /Rect, which a viewer has nowhere to draw (ISO 32000-1, Table 164),
// loses its appearance with nothing drawn.
func TestAnnotationsEmptiesOneWithNoRect(t *testing.T) {
	t.Parallel()
	in := fixture{
		page: "/Annots [7 0 R]",
		objs: []string{
			"<< /Type /Annot /Subtype /Ink /F 4 /CA 0.7 /AP << /N 8 0 R >> >>",
			stream("/Type /XObject /Subtype /Form /BBox [0 0 0 0]", ""),
		},
	}
	report, rewritten := bakeAnnotations(t, in.bytes(), AnnotationOptions{})
	if report.Emptied[0] != 1 || report.Baked[0] != 0 || !rewritten[7] {
		t.Errorf("report %+v, rewritten %v: want the ink emptied", report, rewritten)
	}
}
