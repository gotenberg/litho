package image

import (
	"fmt"
	"image/color"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// packedImagePDF builds a one-page document drawing one image whose samples are
// packed at the given depth. extra carries any additional image entries and must
// end with a space when non-empty.
func packedImagePDF(width, height, bpc int, colorSpace, extra string, data []byte) []string {
	return []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /XObject << /Im0 4 0 R >> >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d"+
			" /ColorSpace %s /BitsPerComponent %d %s/Length %d >>\nstream\n%s\nendstream",
			width, height, colorSpace, bpc, extra, len(data), string(data)),
	}
}

// grey reads a pixel as a single 8-bit level, which is what a grayscale image
// decodes to on all three channels.
func grey(t *testing.T, img interface{ At(int, int) color.Color }, x, y int) uint8 {
	t.Helper()
	r, g, b, _ := img.At(x, y).RGBA()
	if r != g || g != b {
		t.Errorf("pixel (%d,%d) = (%d,%d,%d), want a grey", x, y, r>>8, g>>8, b>>8)
	}
	return uint8(r >> 8)
}

// TestDecodeImage_FourBitIndexedUsesOneSamplePerPixel is the load-bearing test
// for the sub-byte work.
//
// Per ISO 32000-1, 8.6.6.3 an indexed image holds ONE sample per pixel, but
// resolveColorSpace reports the BASE space of [/Indexed /DeviceRGB ...], which
// is "DeviceRGB" and counts three. Striding packed samples by three would make
// a 2-pixel row 3 bytes instead of 1 and demand 6 bytes for this image, so the
// decode fails outright with "decoded data too short" when the sample count is
// taken from the base. Every existing indexed test states /BitsPerComponent 8,
// where unpacking is a no-op, so none of them can see this.
func TestDecodeImage_FourBitIndexedUsesOneSamplePerPixel(t *testing.T) {
	t.Parallel()

	// Four RGB entries: red, green, blue, white. Two 4-bit indices per byte, so
	// one byte per 2-pixel row: 0x01 is indices 0 then 1, 0x23 is 2 then 3.
	const lookup = "<FF0000 00FF00 0000FF FFFFFF>"
	cs := "[/Indexed /DeviceRGB 3 " + lookup + "]"
	ctx, doc, page := imagePDF(t, packedImagePDF(2, 2, 4, cs, "", []byte{0x01, 0x23}))

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	for _, tc := range []struct {
		x, y int
		want color.RGBA
	}{
		{0, 0, color.RGBA{R: 255, A: 255}},
		{1, 0, color.RGBA{G: 255, A: 255}},
		{0, 1, color.RGBA{B: 255, A: 255}},
		{1, 1, color.RGBA{R: 255, G: 255, B: 255, A: 255}},
	} {
		r, g, b, a := img.At(tc.x, tc.y).RGBA()
		got := color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
		if got != tc.want {
			t.Errorf("pixel (%d,%d) = %v, want %v", tc.x, tc.y, got, tc.want)
		}
	}
}

// TestDecodeImage_OneBitGrayPadsEachRow drives row padding through the real
// decode path rather than through unpackSamples alone.
//
// Width 3 at 1 bpc fills three bits and pads five, so each row starts on a fresh
// byte (ISO 32000-1, 8.9.3). Reading the buffer as one bit run would take row 0's
// padding as row 1's samples and shift every row after the first.
func TestDecodeImage_OneBitGrayPadsEachRow(t *testing.T) {
	t.Parallel()

	// Row 0: 101xxxxx. Row 1: 010xxxxx.
	ctx, doc, page := imagePDF(t, packedImagePDF(3, 2, 1, "/DeviceGray", "", []byte{0xA0, 0x40}))

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	for _, tc := range []struct {
		x, y int
		want uint8
	}{
		{0, 0, 255}, {1, 0, 0}, {2, 0, 255},
		{0, 1, 0}, {1, 1, 255}, {2, 1, 0},
	} {
		if got := grey(t, img, tc.x, tc.y); got != tc.want {
			t.Errorf("pixel (%d,%d) = %d, want %d", tc.x, tc.y, got, tc.want)
		}
	}
}

// TestDecodeImage_OneBitHonoursAnInvertedDecodeArray proves the claim
// unpackSamples rests on, rather than leaving it as an assertion in a comment.
//
// A sample is stretched to the full 8-bit range BEFORE the /Decode tables are
// applied. Those tables are linear across 0..255, so stretching first and
// mapping after must give the same answer as mapping the raw [0, 1] range
// ISO 32000-1, 8.9.5.2 defines. /Decode [1 0] inverts, which is the common case
// on bilevel scans, and it is where a wrong composition would show.
func TestDecodeImage_OneBitHonoursAnInvertedDecodeArray(t *testing.T) {
	t.Parallel()

	ctx, doc, page := imagePDF(t,
		packedImagePDF(3, 2, 1, "/DeviceGray", "/Decode [1 0] ", []byte{0xA0, 0x40}))

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	// Exactly the inverse of the run above.
	for _, tc := range []struct {
		x, y int
		want uint8
	}{
		{0, 0, 0}, {1, 0, 255}, {2, 0, 0},
		{0, 1, 255}, {1, 1, 0}, {2, 1, 255},
	} {
		if got := grey(t, img, tc.x, tc.y); got != tc.want {
			t.Errorf("pixel (%d,%d) = %d, want %d under /Decode [1 0]", tc.x, tc.y, got, tc.want)
		}
	}
}

// TestDecodeImage_BitsPerComponentThreeIsRefused pins the corpus case: one file
// declares 3, which ISO 32000-1, 8.9.5.1 Table 89 does not permit and which has
// no defined packing, so it must be refused rather than strided as though it
// were 4.
func TestDecodeImage_BitsPerComponentThreeIsRefused(t *testing.T) {
	t.Parallel()

	ctx, doc, page := imagePDF(t, packedImagePDF(2, 2, 3, "/DeviceGray", "", []byte{0xFF, 0xFF}))

	if _, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0")); err == nil {
		t.Error("BitsPerComponent 3 decoded, but it has no defined sample packing")
	}
}
