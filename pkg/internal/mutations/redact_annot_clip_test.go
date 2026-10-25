package mutations

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// clipRedact redacts theTextRegion of a page carrying annots and objects,
// with or without [RedactOptions.ClipAnnotations].
func clipRedact(t *testing.T, annots, objects string, clip bool) (RedactReport, *writer.ObjAllocator, error) {
	t.Helper()
	doc, page := annotPageFixture(t, annots, objects)
	alloc := writer.NewObjAllocator(20)
	report, err := RedactPageRegionsWith(context.Background(), alloc, doc, page, theTextRegion, RedactOptions{ClipAnnotations: clip})
	return report, alloc, err
}

// clipPending returns the dictionary alloc queued at objNum.
func clipPending(t *testing.T, alloc *writer.ObjAllocator, objNum int) ast.PdfDict {
	t.Helper()
	d, ok := alloc.Pending()[objNum].(ast.PdfDict)
	if !ok {
		t.Fatalf("object %d was not rewritten: %T", objNum, alloc.Pending()[objNum])
	}
	return d
}

// numbers reads arr as numbers.
func numbers(t *testing.T, obj ast.PdfObject) []float64 {
	t.Helper()
	arr, ok := obj.(ast.PdfArray)
	if !ok {
		t.Fatalf("%v is not an array", obj)
	}
	out := make([]float64, len(arr))
	for i, e := range arr {
		v, ok := document.Number(e)
		if !ok {
			t.Fatalf("%v is not a number", e)
		}
		out[i] = v
	}
	return out
}

// borderlessLink is a link drawing nothing, its border of width 0.
const borderlessLink = "/Type /Annot /Subtype /Link /Border [0 0 0] /A << /S /URI /URI (http://example.test/) >>"

func TestRedactClip_CutsALinkToThePartOutside(t *testing.T) {
	// The region ends at x 300, the link at x 500: the part left of it goes,
	// the rest stays a link, and nothing it paints changes, since it paints
	// nothing.
	objects := "8 0 obj\n<< " + borderlessLink + " /Rect [100 695 500 720] >>\nendobj\n"

	if _, _, err := clipRedact(t, "[8 0 R]", objects, false); !errors.Is(err, ErrAnnotationStraddlesRegion) {
		t.Fatalf("without the option: err = %v, want ErrAnnotationStraddlesRegion", err)
	}

	report, alloc, err := clipRedact(t, "[8 0 R]", objects, true)
	if err != nil {
		t.Fatalf("RedactPageRegionsWith: %v", err)
	}
	if !slices.Equal(report.ClippedAnnotations, []int{8}) || len(report.RemovedAnnotations) != 0 {
		t.Errorf("clipped %v removed %v, want object 8 clipped and none removed", report.ClippedAnnotations, report.RemovedAnnotations)
	}
	link := clipPending(t, alloc, 8)
	if got := numbers(t, link["Rect"]); !slices.Equal(got, []float64{300, 695, 500, 720}) {
		t.Errorf("/Rect = %v, want [300 695 500 720]", got)
	}
	if _, has := link["QuadPoints"]; has {
		t.Errorf("/QuadPoints = %v, want none for a single piece", link["QuadPoints"])
	}
	if _, has := link["A"]; !has {
		t.Error("the link lost its action")
	}
}

func TestRedactClip_LeavesSeveralPiecesAsQuadPoints(t *testing.T) {
	// The region lies across the middle of a link taller and wider than it,
	// so four pieces are left around it.
	objects := "8 0 obj\n<< " + borderlessLink + " /Rect [50 600 400 800] >>\nendobj\n"
	_, alloc, err := clipRedact(t, "[8 0 R]", objects, true)
	if err != nil {
		t.Fatalf("RedactPageRegionsWith: %v", err)
	}
	link := clipPending(t, alloc, 8)
	if got := numbers(t, link["Rect"]); !slices.Equal(got, []float64{50, 600, 400, 800}) {
		t.Errorf("/Rect = %v, want the pieces' bounding box [50 600 400 800]", got)
	}
	quads := numbers(t, link["QuadPoints"])
	if len(quads) != 4*quadPointsPerQuad {
		t.Fatalf("/QuadPoints = %v, want 4 quadrilaterals", quads)
	}
	region := theTextRegion[0]
	for q := 0; q < len(quads); q += quadPointsPerQuad {
		x0, y0, x1, y1 := quads[q], quads[q+1], quads[q+4], quads[q+5]
		if x0 < region[2] && x1 > region[0] && y0 < region[3] && y1 > region[1] {
			t.Errorf("quadrilateral %v reaches into the region %v", quads[q:q+quadPointsPerQuad], region)
		}
	}
}

func TestRedactClip_CutsALinksQuadPoints(t *testing.T) {
	// Two lines of a link: the one inside the region goes, the other stays,
	// and the link's area is that one line.
	objects := "8 0 obj\n<< " + borderlessLink + " /Rect [100 600 500 720]" +
		" /QuadPoints [100 700 200 700 200 715 100 715 100 600 500 600 500 620 100 620] >>\nendobj\n"
	_, alloc, err := clipRedact(t, "[8 0 R]", objects, true)
	if err != nil {
		t.Fatalf("RedactPageRegionsWith: %v", err)
	}
	link := clipPending(t, alloc, 8)
	if got := numbers(t, link["QuadPoints"]); !slices.Equal(got, []float64{100, 600, 500, 600, 500, 620, 100, 620}) {
		t.Errorf("/QuadPoints = %v, want the second line alone", got)
	}
	if got := numbers(t, link["Rect"]); !slices.Equal(got, []float64{100, 600, 500, 620}) {
		t.Errorf("/Rect = %v, want [100 600 500 620]", got)
	}
}

func TestRedactClip_RemovesALinkWhoseQuadPointsLieInside(t *testing.T) {
	// /Rect reaches past the region, but the link is only activated over its
	// quadrilateral, which the region covers: nothing of it is left.
	objects := "8 0 obj\n<< " + borderlessLink + " /Rect [100 600 500 720]" +
		" /QuadPoints [100 700 200 700 200 715 100 715] >>\nendobj\n"
	report, _, err := clipRedact(t, "[8 0 R]", objects, true)
	if err != nil {
		t.Fatalf("RedactPageRegionsWith: %v", err)
	}
	if !slices.Equal(report.RemovedAnnotations, []int{8}) || len(report.ClippedAnnotations) != 0 {
		t.Errorf("removed %v clipped %v, want object 8 removed", report.RemovedAnnotations, report.ClippedAnnotations)
	}
}

func TestRedactClip_StillRefuses(t *testing.T) {
	cases := map[string]string{
		// Its default border is 1 wide (ISO 32000-1, Table 173), so it paints.
		"bordered link": "8 0 obj\n<< /Type /Annot /Subtype /Link /Rect [100 695 500 720] >>\nendobj\n",
		"link with an appearance": "8 0 obj\n<< " + borderlessLink + " /Rect [100 695 500 720] /AP << /N 9 0 R >> >>\nendobj\n" +
			"9 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 400 25] /Length 0 >>\nstream\n\nendstream\nendobj\n",
		"turned quadrilateral": "8 0 obj\n<< " + borderlessLink + " /Rect [100 600 500 720]" +
			" /QuadPoints [100 650 300 600 350 700 150 720] >>\nendobj\n",
		"square": "8 0 obj\n<< /Type /Annot /Subtype /Square /Rect [100 695 500 720] /Border [0 0 0] >>\nendobj\n",
	}
	for name, objects := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := clipRedact(t, "[8 0 R]", objects, true); !errors.Is(err, ErrAnnotationStraddlesRegion) {
				t.Errorf("err = %v, want ErrAnnotationStraddlesRegion", err)
			}
		})
	}
}

func TestRedactClip_KeepsAClosedPopupWhoseParentStays(t *testing.T) {
	// The pop-up straddles the region; its parent sits clear of it.
	objects := "8 0 obj\n<< /Type /Annot /Subtype /Square /Rect [10 10 50 50] /Popup 9 0 R >>\nendobj\n" +
		"9 0 obj\n<< /Type /Annot /Subtype /Popup /Rect [100 695 500 720] /Parent 8 0 R >>\nendobj\n"
	if _, _, err := clipRedact(t, "[8 0 R 9 0 R]", objects, false); !errors.Is(err, ErrAnnotationStraddlesRegion) {
		t.Fatalf("without the option: err = %v, want ErrAnnotationStraddlesRegion", err)
	}
	report, _, err := clipRedact(t, "[8 0 R 9 0 R]", objects, true)
	if err != nil {
		t.Fatalf("RedactPageRegionsWith: %v", err)
	}
	if len(report.RemovedAnnotations)+len(report.ClippedAnnotations) != 0 {
		t.Errorf("removed %v clipped %v, want both kept as they are", report.RemovedAnnotations, report.ClippedAnnotations)
	}

	open := "8 0 obj\n<< /Type /Annot /Subtype /Square /Rect [10 10 50 50] /Popup 9 0 R >>\nendobj\n" +
		"9 0 obj\n<< /Type /Annot /Subtype /Popup /Rect [100 695 500 720] /Parent 8 0 R /Open true >>\nendobj\n"
	if _, _, err := clipRedact(t, "[8 0 R 9 0 R]", open, true); !errors.Is(err, ErrAnnotationStraddlesRegion) {
		t.Errorf("open pop-up: err = %v, want ErrAnnotationStraddlesRegion: it draws its window", err)
	}
}

func TestRedactRegions_RemovesThePopupOfARemovedAnnotation(t *testing.T) {
	// The leak: the pop-up, clear of the region, kept its /Parent, and the
	// rewrite wrote the removed Ink annotation through it, its /InkList
	// drawing what lay inside the region. Each way a pop-up and its parent
	// name each other takes the pop-up with it. A reply loses /IRT.
	cases := map[string]string{
		"by /Parent": "8 0 obj\n<< /Type /Annot /Subtype /Ink /Rect [100 695 200 720] /InkList [[110 700 190 715]] >>\nendobj\n" +
			"9 0 obj\n<< /Type /Annot /Subtype /Popup /Rect [400 400 500 500] /Parent 8 0 R >>\nendobj\n",
		"by /Popup": "8 0 obj\n<< /Type /Annot /Subtype /Ink /Rect [100 695 200 720] /InkList [[110 700 190 715]] /Popup 9 0 R >>\nendobj\n" +
			"9 0 obj\n<< /Type /Annot /Subtype /Popup /Rect [100 695 500 720] >>\nendobj\n",
	}
	for name, objects := range cases {
		t.Run(name, func(t *testing.T) {
			objects += "10 0 obj\n<< /Type /Annot /Subtype /Text /Rect [10 10 30 30] /IRT 8 0 R /RT /R /AP << /N 11 0 R >> >>\nendobj\n" +
				"11 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 20 20] /Length 0 >>\nstream\n\nendstream\nendobj\n"
			report, alloc, err := clipRedact(t, "[8 0 R 9 0 R 10 0 R]", objects, false)
			if err != nil {
				t.Fatalf("RedactPageRegionsWith: %v", err)
			}
			if !slices.Equal(report.RemovedAnnotations, []int{8, 9}) {
				t.Errorf("RemovedAnnotations = %v, want [8 9]", report.RemovedAnnotations)
			}
			if arr, _ := clipPending(t, alloc, 3)["Annots"].(ast.PdfArray); len(arr) != 1 {
				t.Errorf("/Annots = %v, want the reply alone", arr)
			}
			reply := clipPending(t, alloc, 10)
			if _, has := reply["IRT"]; has {
				t.Errorf("the reply still names the removed annotation: %v", reply)
			}
			if _, has := reply["RT"]; has {
				t.Errorf("the reply keeps /RT: %v", reply)
			}
		})
	}
}
