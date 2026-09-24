package compliance

import (
	"context"
	"fmt"
	"testing"
)

// TestMaskImageInterpolateJudged pins that an image's /SMask and /Mask are
// image dictionaries in their own right, so the image rules bind to them.
//
// ISO 19005-2 §6.2.8 t3 forbids /Interpolate true on an image dictionary, and
// veraPDF builds a PDSMaskImage for /SMask and states the rule against it.
// The XObject walk descended into Form XObjects but never into an image's
// masks. Fixtures confirm veraPDF states it for /Mask as well.
//
// # Corpus evidence
//
//   - govdocs thread_400_400019.pdf: carries 72 images with /Interpolate
//     true, of which 15 are reached only as a /SMask. veraPDF reported 72 and
//     litho 57 before the walk descended into masks.
func TestMaskImageInterpolateJudged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// build lays out one image XObject whose mask entry is the caller's.
	build := func(t *testing.T, maskEntry string) []Violation {
		t.Helper()

		const content = "q 100 0 0 100 10 10 cm /Im0 Do Q"
		doc, cat := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R"+
				" /Resources << /XObject << /Im0 5 0 R >> >> >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
			"<< /Type /XObject /Subtype /Image /Width 2 /Height 2 /BitsPerComponent 8"+
				" /ColorSpace /DeviceGray"+maskEntry+" /Length 4 >>\nstream\nabcd\nendstream",
			// Object 6: the mask image, interpolated.
			"<< /Type /XObject /Subtype /Image /Width 2 /Height 2 /BitsPerComponent 8"+
				" /ColorSpace /DeviceGray /Interpolate true /Length 4 >>\nstream\nabcd\nendstream",
		)

		pages := pageCtxsFor(ctx, doc, cat)
		pages[0].content = []byte(content)
		return checkXObjectGroup(ctx, &validationCtx{
			doc: doc, catalog: cat, profile: "PDF/A-2b", part: 2, pages: pages,
		})
	}

	count := func(vs []Violation) int {
		n := 0
		for _, v := range vs {
			if v.Rule == Rule_19005_2_6_2_8_t03 {
				n++
			}
		}
		return n
	}

	t.Run("an interpolated /SMask reports", func(t *testing.T) {
		t.Parallel()
		if n := count(build(t, " /SMask 6 0 R")); n != 1 {
			t.Errorf("an interpolated soft mask must report §6.2.8 t3 once, got %d", n)
		}
	})

	t.Run("an interpolated /Mask reports", func(t *testing.T) {
		t.Parallel()
		if n := count(build(t, " /Mask 6 0 R")); n != 1 {
			t.Errorf("an interpolated stencil mask must report §6.2.8 t3 once, got %d", n)
		}
	})

	t.Run("a colour-key /Mask array is no image", func(t *testing.T) {
		t.Parallel()
		// ISO 32000-1 §8.9.6.4 lets /Mask be a range array, which resolves to
		// no stream and must simply be passed over.
		if n := count(build(t, " /Mask [0 1]")); n != 0 {
			t.Errorf("a colour-key /Mask array must report nothing, got %d", n)
		}
	})

	t.Run("no mask reports nothing", func(t *testing.T) {
		t.Parallel()
		if n := count(build(t, "")); n != 0 {
			t.Errorf("an image with no mask must report nothing, got %d", n)
		}
	})
}
