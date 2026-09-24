package image

import "testing"

// nestedResourcePDF builds a one-page document whose page resources hold the
// given entry, alongside object 4, the image the nested resources show.
func nestedResourcePDF(pageResources string) []string {
	return []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources " + pageResources + " >>",
		"<< /Type /XObject /Subtype /Image /Width 8 /Height 4" +
			" /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 0 >>\nstream\n\nendstream",
		// 5: a form XObject whose resources show the image.
		"<< /Type /XObject /Subtype /Form /BBox [0 0 10 10]" +
			" /Resources << /XObject << /Im0 4 0 R >> >> /Length 0 >>\nstream\n\nendstream",
		// 6: a tiling pattern whose resources show the image.
		"<< /Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10]" +
			" /XStep 10 /YStep 10 /Resources << /XObject << /Im0 4 0 R >> >> /Length 0 >>\nstream\n\nendstream",
	}
}

// TestCollectImagesFollowsPatternsAndSoftMasks pins that an image painted
// through a tiling pattern or a soft-mask group is reported.
//
// A tiling pattern is a content stream with its own resources (ISO 32000-1,
// 8.7.3.1) and a soft mask names a transparency group XObject in /G that is
// painted to derive the mask (11.6.5.2, Table 144). Neither puts the image in
// the page's /XObject dictionary, so walking that alone reported nothing.
//
// # Corpus evidence
//
//   - pdfjs issue18956.pdf: draws a 728x90 image through an /ExtGState soft
//     mask and nothing else.
//   - pdfjs issue16395.pdf: paints its images through a pattern.
func TestCollectImagesFollowsPatternsAndSoftMasks(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		resources string
		want      int
	}{
		{
			"soft-mask group",
			"<< /ExtGState << /GS0 << /Type /ExtGState /SMask << /S /Luminosity /G 5 0 R >> >> >> >>",
			1,
		},
		{
			"tiling pattern",
			"<< /Pattern << /P0 6 0 R >> >>",
			1,
		},
		{
			"a graphics state with no mask",
			"<< /ExtGState << /GS0 << /Type /ExtGState /SMask /None >> >> >>",
			0,
		},
		{
			"a graphics state with no /SMask entry",
			"<< /ExtGState << /GS0 << /Type /ExtGState /CA 0.5 >> >> >>",
			0,
		},
		{
			"the same group used by two graphics states",
			"<< /ExtGState << /GS0 << /SMask << /G 5 0 R >> >> /GS1 << /SMask << /G 5 0 R >> >> >> >>",
			1,
		},
		{
			"a pattern and the page's own XObject",
			"<< /Pattern << /P0 6 0 R >> /XObject << /ImDirect 4 0 R >> >>",
			2,
		},
	} {
		ctx, doc, page := imagePDF(t, nestedResourcePDF(tc.resources))
		got := ExtractImagesRecursive(ctx, doc, page)
		if len(got) != tc.want {
			t.Errorf("%s: reported %d images, want %d", tc.name, len(got), tc.want)
			continue
		}
		for _, m := range got {
			if m.Width != 8 || m.Height != 4 {
				t.Errorf("%s: got %dx%d, want 8x4", tc.name, m.Width, m.Height)
			}
		}
	}
}

// TestCollectImagesSoftMaskCycleTerminates pins that a soft-mask group whose
// own resources name it again is walked once. The group comes from the file, so
// nothing stops a producer, or an attacker, from writing that loop.
func TestCollectImagesSoftMaskCycleTerminates(t *testing.T) {
	t.Parallel()

	ctx, doc, page := imagePDF(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /ExtGState << /GS0 << /SMask << /G 5 0 R >> >> >> >> >>",
		"<< /Type /XObject /Subtype /Image /Width 8 /Height 4" +
			" /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 0 >>\nstream\n\nendstream",
		// Its own resources point the same soft mask back at itself.
		"<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources" +
			" << /XObject << /Im0 4 0 R >> /ExtGState << /GS0 << /SMask << /G 5 0 R >> >> >> >>" +
			" /Length 0 >>\nstream\n\nendstream",
	})

	if got := ExtractImagesRecursive(ctx, doc, page); len(got) != 1 {
		t.Errorf("reported %d images for a self-referential soft mask, want 1", len(got))
	}
}
