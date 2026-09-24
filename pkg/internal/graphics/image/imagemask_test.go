package image

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// maskImagePDF builds a one-page document whose /Im0 is a stencil mask. It
// writes no /ColorSpace at all, which is what packedImagePDF cannot express and
// what ISO 32000-1, 8.9.6.2 requires of an image mask.
func maskImagePDF(width, height int, extra string, data []byte) []string {
	return []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /XObject << /Im0 4 0 R >> >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d"+
			" /ImageMask true /BitsPerComponent 1 %s/Length %d >>\nstream\n%s\nendstream",
			width, height, extra, len(data), string(data)),
	}
}

// TestDecodeImage_StencilMaskUsesOneSamplePerPixel is the stencil twin of
// [TestDecodeImage_FourBitIndexedUsesOneSamplePerPixel], and the same mistake
// one axis over.
//
// An image mask holds ONE sample per pixel and carries no /ColorSpace, but
// resolveColorSpace answers "DeviceRGB" for a missing entry, which counts three.
// Striding a 1-bit stencil by three makes a 16-pixel row 6 bytes instead of 2
// and demands 12 bytes for this image, so a COMPLETE mask failed outright with
// "decoded data too short".
//
// The control is the load-bearing half: the same bytes and the same geometry
// declared /DeviceGray decode fine, so only the stencil-ness differs and
// neither the filter nor the bit depth can explain the failure.
//
// # Corpus evidence
//
//   - pdfcpu Wonderwall.pdf: before the fix, a 2312x3307 mask carries 955,723
//     bytes and was asked for 2,867,169, exactly three times as many.
func TestDecodeImage_StencilMaskUsesOneSamplePerPixel(t *testing.T) {
	t.Parallel()

	// 16x2 at 1 bpc, one component: (16*1+7)/8 = 2 bytes a row, 2 rows = 4.
	const w, h = 16, 2
	data := []byte{0xF0, 0x0F, 0xAA, 0x55}

	t.Run("control: the same bytes declared /DeviceGray", func(t *testing.T) {
		t.Parallel()

		ctx, doc, page := imagePDF(t, packedImagePDF(w, h, 1, "/DeviceGray", "", data))
		if _, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0")); err != nil {
			t.Fatalf("the control failed, so the stencil case below proves nothing: %v", err)
		}
	})

	t.Run("the stencil", func(t *testing.T) {
		t.Parallel()

		ctx, doc, page := imagePDF(t, maskImagePDF(w, h, "", data))
		img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
		if err != nil {
			t.Fatalf("a complete 1-bpc stencil was refused although every sample is present: %v", err)
		}
		if got := img.Bounds().Dx(); got != w {
			t.Errorf("width = %d, want %d", got, w)
		}
		if got := img.Bounds().Dy(); got != h {
			t.Errorf("height = %d, want %d", got, h)
		}

		// A 16-pixel row at one bit a sample is TWO bytes, so the rows read:
		//
		//	row 0   0xF0 0x0F   1111 0000  0000 1111
		//	row 1   0xAA 0x55   1010 1010  0101 0101
		//
		// Under the default /Decode [0 1] a 1 sample leaves the pixel unpainted
		// and a 0 paints it. Asserting pixels rather than only the bounds is
		// what shows the samples were strided one per pixel: strided by three
		// the row would end after five and a third pixels.
		for _, tc := range []struct {
			x, y int
			want uint8
			why  string
		}{
			{0, 0, 255, "0xF0 bit 7 is 1, unpainted"},
			{4, 0, 0, "0xF0 bit 3 is 0, painted"},
			{15, 0, 255, "0x0F bit 0 is 1, unpainted"},
			{0, 1, 255, "0xAA bit 7 is 1, unpainted"},
			{1, 1, 0, "0xAA bit 6 is 0, painted"},
		} {
			if v := grey(t, img, tc.x, tc.y); v != tc.want {
				t.Errorf("pixel (%d,%d) = %d, want %d: %s", tc.x, tc.y, v, tc.want, tc.why)
			}
		}
	})
}

// TestDecodeImage_StencilMaskIgnoresAColorSpace pins that a /ColorSpace written
// on a mask does not change the sample count.
//
// ISO 32000-1, Table 89 forbids the entry on an image mask, and litho's
// compliance side already ignores it there (TestColorScope_ImageMaskColorSpaceIgnored).
// The decoder has to agree, or a malformed file would stride by three again.
func TestDecodeImage_StencilMaskIgnoresAColorSpace(t *testing.T) {
	t.Parallel()

	const w, h = 16, 2
	data := []byte{0xF0, 0x0F, 0xAA, 0x55}

	ctx, doc, page := imagePDF(t, maskImagePDF(w, h, "/ColorSpace /DeviceRGB ", data))
	if _, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0")); err != nil {
		t.Fatalf("a stencil carrying a forbidden /ColorSpace was refused: %v", err)
	}
}
