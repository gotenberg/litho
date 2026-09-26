package compliance

import (
	"context"
	"fmt"
	"testing"
)

// TestCheckAnnotationTransparency_AppearanceResourceTree pins that an
// appearance's transparency is found wherever the resource tree carries it,
// not only in an ExtGState on the appearance form itself. An appearance
// routinely paints a soft-masked image or draws a transparency-group form, and
// veraPDF counts either toward the page's containsTransparency for ISO 19005-2
// §6.2.10 t2.
//
// # Corpus evidence
//
//   - pdfjs annotation-as.pdf: carries image /SMask entries inside annotation
//     appearances.
//   - pdfjs stamps.pdf: carries a /Group /S /Transparency inside annotation
//     appearances.
func TestCheckAnnotationTransparency_AppearanceResourceTree(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// build wraps an annotation whose /AP /N form declares apRes as its
	// resources and paints draws, then reports whether the page is seen as
	// transparent. The form paints what it declares: a resource an appearance
	// never invokes is not rendered content, so it carries no transparency.
	build := func(t *testing.T, apRes, draws string, extraObjs ...string) bool {
		t.Helper()
		objs := []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
			"<< /Type /Annot /Subtype /Stamp /Rect [0 0 10 10] /AP << /N 5 0 R >> >>",
			fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources %s /Length %d >>"+
				"\nstream\n%s\nendstream", apRes, len(draws), draws),
		}
		objs = append(objs, extraObjs...)
		doc, cat := objsDocPDF(t, objs...)
		return pagePaintsTransparency(ctx, doc, collectPages(ctx, doc, cat)[0], 2)
	}

	image := func(smask string) string {
		return "<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 " +
			"/ColorSpace /DeviceGray " + smask + " /Length 0 >>\nstream\nendstream"
	}

	t.Run("soft-masked image in the appearance resources", func(t *testing.T) {
		t.Parallel()
		if !build(t, "<< /XObject << /Im 6 0 R >> >>", "/Im Do", image("/SMask 7 0 R"), image("")) {
			t.Error("an /SMask image painted by an appearance is transparency")
		}
	})

	t.Run("transparency group form in the appearance resources", func(t *testing.T) {
		t.Parallel()
		if !build(t, "<< /XObject << /Fm 6 0 R >> >>", "/Fm Do",
			"<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Group << /S /Transparency >>"+
				" /Length 0 >>\nstream\nendstream") {
			t.Error("a transparency-group form drawn by an appearance is transparency")
		}
	})

	t.Run("transparency nested one form deeper", func(t *testing.T) {
		t.Parallel()
		if !build(t, "<< /XObject << /Fm 6 0 R >> >>", "/Fm Do",
			"<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << /XObject << /Im 7 0 R >> >>"+
				" /Length 6 >>\nstream\n/Im Do\nendstream",
			image("/SMask 8 0 R"), image("")) {
			t.Error("transparency nested below the appearance form must still be found")
		}
	})

	t.Run("opaque appearance resources stay clean", func(t *testing.T) {
		t.Parallel()
		if build(t, "<< /XObject << /Im 6 0 R >> >>", "/Im Do", image("")) {
			t.Error("an appearance painting a plain image uses no transparency")
		}
	})

	t.Run("fully opaque ExtGState stays clean", func(t *testing.T) {
		t.Parallel()
		if build(t, "<< /ExtGState << /GS0 << /CA 1 /ca 1 /BM /Normal /SMask /None >> >> >>", "/GS0 gs") {
			t.Error("an explicitly opaque ExtGState is not transparency")
		}
	})
}
