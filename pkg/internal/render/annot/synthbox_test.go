package annot

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// synthBox returns SynthesizedBox for the single annotation of a page.
func synthBox(t *testing.T, dict string, objs ...string) (geom.Rect, geom.Rect, bool) {
	t.Helper()
	ctx, doc, page := firstPage(t, onePage("", "[5 0 R]", append([]string{dict}, objs...)...))
	annots, err := document.ResolveArray(ctx, doc, page.Dict["Annots"])
	if err != nil || len(annots) != 1 {
		t.Fatalf("page /Annots: %v, %v", annots, err)
	}
	d, err := document.ResolveDict(ctx, doc, annots[0])
	if err != nil {
		t.Fatal(err)
	}
	return SynthesizedBox(ctx, doc, d)
}

func rectNear(a geom.Rect, b [4]float64) bool {
	return approx(a.Min.X, b[0]) && approx(a.Min.Y, b[1]) && approx(a.Max.X, b[2]) && approx(a.Max.Y, b[3])
}

func TestSynthesizedBox(t *testing.T) {
	t.Parallel()

	// A line of one segment reaches half its width across itself, wherever
	// /Rect sits: here nowhere.
	ink, path, ok := synthBox(t, "<< /Type /Annot /Subtype /Line /Rect [0 0 0 0] /L [20 100 180 100] /BS << /W 2 >> >>")
	if !ok || !rectNear(path, [4]float64{20, 100, 180, 100}) || !rectNear(ink, [4]float64{19, 99, 181, 101}) {
		t.Errorf("line: ink %v path %v ok %v, want ink [19 99 181 101], path [20 100 180 100]", ink, path, ok)
	}

	// A polyline's joins take the miter's full reach.
	ink, _, ok = synthBox(t, "<< /Type /Annot /Subtype /PolyLine /Rect [0 0 1 1] /Vertices [20 20 60 40 100 20] /BS << /W 2 >> >>")
	if !ok || !rectNear(ink, [4]float64{10, 10, 110, 50}) {
		t.Errorf("polyline: ink %v ok %v, want [10 10 110 50]", ink, ok)
	}

	// A square is drawn inside /Rect and clipped to it.
	ink, _, ok = synthBox(t, "<< /Type /Annot /Subtype /Square /Rect [20 20 80 80] /BS << /W 5 >> >>")
	if !ok || !rectNear(ink, [4]float64{20, 20, 80, 80}) {
		t.Errorf("square: ink %v ok %v, want /Rect [20 20 80 80]", ink, ok)
	}

	// An annotation with an appearance stream draws it, inside /Rect.
	if _, _, ok := synthBox(t, annot("Line", 4, "<< /N 6 0 R >>", "/L [0 0 500 500]"), form("/BBox [0 0 10 10]")); ok {
		t.Error("an annotation with /AP reported a synthesised box")
	}

	// A subtype no appearance is synthesised for has none.
	if _, _, ok := synthBox(t, "<< /Type /Annot /Subtype /Text /Rect [20 20 40 40] >>"); ok {
		t.Error("a Text annotation reported a synthesised box")
	}
}
