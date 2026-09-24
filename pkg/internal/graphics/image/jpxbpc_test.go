package image

import "testing"

// jpxImagePDF builds a one-page document showing one image with the given
// filter and /BitsPerComponent entry.
func jpxImagePDF(filter, bpcEntry string) []string {
	return []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /XObject << /Im0 4 0 R >> >> >>",
		"<< /Type /XObject /Subtype /Image /Width 8 /Height 4 /ColorSpace /DeviceRGB" +
			" /Filter " + filter + " " + bpcEntry + " /Length 0 >>\nstream\n\nendstream",
	}
}

// TestImageMetaIgnoresBitsPerComponentForJPX pins that a JPEG 2000 image does
// not report the /BitsPerComponent its dictionary states.
//
// Per ISO 32000-1, Table 89 the entry is optional for a JPXDecode image and
// shall be ignored if present, because the depth belongs to the codestream.
// Reporting the stated value hands the caller a number the spec says means
// nothing.
//
// # Corpus evidence
//
//   - pdfjs issue12213.pdf: states 4 for an image poppler reads as 8.
func TestImageMetaIgnoresBitsPerComponentForJPX(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		filter string
		bpc    string
		want   int
	}{
		{"JPXDecode with a stated depth", "/JPXDecode", "/BitsPerComponent 4", 8},
		{"JPXDecode with no stated depth", "/JPXDecode", "", 8},
		{"JPXDecode last in a filter chain", "[/FlateDecode /JPXDecode]", "/BitsPerComponent 4", 8},
		{"FlateDecode keeps the stated depth", "/FlateDecode", "/BitsPerComponent 4", 4},
		{"DCTDecode keeps the stated depth", "/DCTDecode", "/BitsPerComponent 8", 8},
		{"a filter array without JPX keeps it", "[/ASCII85Decode /FlateDecode]", "/BitsPerComponent 1", 1},
	} {
		ctx, doc, page := imagePDF(t, jpxImagePDF(tc.filter, tc.bpc))
		got := ExtractImagesRecursive(ctx, doc, page)
		if len(got) != 1 {
			t.Errorf("%s: reported %d images, want 1", tc.name, len(got))
			continue
		}
		if got[0].BitsPerComponent != tc.want {
			t.Errorf("%s: bitsPerComponent = %d, want %d",
				tc.name, got[0].BitsPerComponent, tc.want)
		}
	}
}
