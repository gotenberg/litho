package image

import "testing"

// annotImageObjs builds a one-page document whose page draws nothing itself and
// carries the given annotation as object 5. Object 4 is the image the
// annotation's appearance stream shows.
func annotImageObjs(annot string) []string {
	return []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [5 0 R] >>",
		"<< /Type /XObject /Subtype /Image /Width 8 /Height 4" +
			" /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 0 >>\nstream\n\nendstream",
		annot,
		"<< /Type /XObject /Subtype /Form /BBox [0 0 10 10]" +
			" /Resources << /XObject << /Im0 4 0 R >> >> /Length 0 >>\nstream\n\nendstream",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 10 10]" +
			" /Resources << /XObject << /ImOff 4 0 R >> >> /Length 0 >>\nstream\n\nendstream",
	}
}

// TestExtractImagesRecursiveFindsAnnotationImages pins that an image an
// annotation draws is reported.
//
// An annotation's normal appearance is a form XObject the page draws
// (ISO 32000-1, 12.5.5) and its resources are its own, not the page's. Walking
// only the page resources missed them entirely, so a page showing an image only
// through an annotation was reported empty.
//
// # Corpus evidence
//
//   - pdfjs firefox_logo.pdf: shows a 512x543 image through a /Stamp
//     annotation and nothing else, so `litho images` reported an empty page
//     while poppler drew one.
func TestExtractImagesRecursiveFindsAnnotationImages(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		annot string
		want  int
	}{
		{
			"appearance is a stream",
			"<< /Type /Annot /Subtype /Stamp /Rect [0 0 10 10] /AP << /N 6 0 R >> >>",
			1,
		},
		{
			"appearance state selected by /AS",
			"<< /Type /Annot /Subtype /Widget /Rect [0 0 10 10] /AS /On" +
				" /AP << /N << /On 6 0 R /Off 7 0 R >> >> >>",
			1,
		},
		{
			"a single state with no /AS",
			"<< /Type /Annot /Subtype /Widget /Rect [0 0 10 10] /AP << /N << /On 6 0 R >> >> >>",
			1,
		},
		{
			"several states and no /AS is ambiguous",
			"<< /Type /Annot /Subtype /Widget /Rect [0 0 10 10]" +
				" /AP << /N << /On 6 0 R /Off 7 0 R >> >> >>",
			0,
		},
		{
			"hidden annotations are not drawn",
			"<< /Type /Annot /Subtype /Stamp /Rect [0 0 10 10] /F 2 /AP << /N 6 0 R >> >>",
			0,
		},
		{
			"NoView annotations are not drawn",
			"<< /Type /Annot /Subtype /Stamp /Rect [0 0 10 10] /F 32 /AP << /N 6 0 R >> >>",
			0,
		},
		{
			"a printable annotation is drawn",
			"<< /Type /Annot /Subtype /Stamp /Rect [0 0 10 10] /F 4 /AP << /N 6 0 R >> >>",
			1,
		},
		{
			"only the down appearance carries the image",
			"<< /Type /Annot /Subtype /Stamp /Rect [0 0 10 10] /AP << /D 6 0 R >> >>",
			0,
		},
		{
			"no appearance at all",
			"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] >>",
			0,
		},
	} {
		ctx, doc, page := imagePDF(t, annotImageObjs(tc.annot))
		got := ExtractImagesRecursive(ctx, doc, page)
		if len(got) != tc.want {
			t.Errorf("%s: reported %d images, want %d", tc.name, len(got), tc.want)
			continue
		}
		if tc.want == 1 && (got[0].Width != 8 || got[0].Height != 4) {
			t.Errorf("%s: got %dx%d, want 8x4", tc.name, got[0].Width, got[0].Height)
		}
	}
}

// TestExtractImagesRecursiveReportsAnnotationImageOnce pins that an appearance
// form invoked by two annotations is reported once, the same rule the page's
// own form XObjects follow.
func TestExtractImagesRecursiveReportsAnnotationImageOnce(t *testing.T) {
	t.Parallel()

	ctx, doc, page := imagePDF(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [5 0 R 7 0 R] >>",
		"<< /Type /XObject /Subtype /Image /Width 8 /Height 4" +
			" /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 0 >>\nstream\n\nendstream",
		"<< /Type /Annot /Subtype /Stamp /Rect [0 0 10 10] /AP << /N 6 0 R >> >>",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 10 10]" +
			" /Resources << /XObject << /Im0 4 0 R >> >> /Length 0 >>\nstream\n\nendstream",
		"<< /Type /Annot /Subtype /Stamp /Rect [0 0 20 20] /AP << /N 6 0 R >> >>",
	})

	if got := ExtractImagesRecursive(ctx, doc, page); len(got) != 1 {
		t.Errorf("reported %d images for one appearance form shared by two annotations, want 1", len(got))
	}
}

// TestExtractImagesShallowIgnoresAnnotations pins that the shallow reader keeps
// its own contract: the OCR triage heuristic asks whether the page itself is a
// scanned image, and an annotation's stamp is not that.
func TestExtractImagesShallowIgnoresAnnotations(t *testing.T) {
	t.Parallel()

	ctx, doc, page := imagePDF(t, annotImageObjs(
		"<< /Type /Annot /Subtype /Stamp /Rect [0 0 10 10] /AP << /N 6 0 R >> >>"))
	if got := ExtractImages(ctx, doc, page); len(got) != 0 {
		t.Errorf("the shallow reader reported %d annotation images, want 0", len(got))
	}
}
