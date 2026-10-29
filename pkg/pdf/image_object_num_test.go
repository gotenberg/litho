package pdf

import (
	"context"
	"testing"
)

// TestExtractImagesObjectNum pins that every placement of one image XObject
// reports the same object number, so a caller can tell a shared image from
// two copies.
func TestExtractImagesObjectNum(t *testing.T) {
	r := openBytes(t, buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /XObject << /Im0 5 0 R >> >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << /XObject << /Im1 5 0 R >> >> >>",
		"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n\x80\nendstream",
	}))

	for page := 1; page <= 2; page++ {
		images, err := r.ExtractImages(context.Background(), page)
		if err != nil {
			t.Fatalf("page %d: ExtractImages: %v", page, err)
		}
		if len(images) != 1 || images[0].ObjectNum != 5 {
			t.Fatalf("page %d: images = %+v, want one with object number 5", page, images)
		}
	}
}
