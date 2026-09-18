package image

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// solidJPEG encodes a size x size JPEG of one colour. Solid rather than
// patterned so the lossy round-trip stays close to the input and the
// assertions can be tight.
func solidJPEG(t *testing.T, size int, c color.Color, gray bool) string {
	t.Helper()

	var src image.Image
	if gray {
		g := image.NewGray(image.Rect(0, 0, size, size))
		for y := range size {
			for x := range size {
				g.Set(x, y, c)
			}
		}
		src = g
	} else {
		rgb := image.NewRGBA(image.Rect(0, 0, size, size))
		for y := range size {
			for x := range size {
				rgb.Set(x, y, c)
			}
		}
		src = rgb
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return buf.String()
}

// jpegPDF builds a one-page document drawing a JPEG image that declares the
// given colour space, which need not match what the codestream holds.
func jpegPDF(colorSpace string, size int, data string) []string {
	return []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /XObject << /Im0 4 0 R >> >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d"+
			" /ColorSpace %s /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream",
			size, size, colorSpace, len(data), data),
	}
}

// near reports whether two 8-bit samples are within JPEG's lossy tolerance.
func near(got, want uint8) bool {
	d := int(got) - int(want)
	if d < 0 {
		d = -d
	}
	return d <= 12
}

// TestDecodeImage_JPEGDecodes is the first test in this package to decode a
// real JPEG, which is how the bug it pins survived.
//
// DecodeImage used to hand the stream to image/jpeg on the premise that the
// filter pipeline left DCTDecode untouched. It does not: the pipeline runs
// decodeDCT, so image/jpeg received already-decoded samples and every JPEG
// failed with "missing SOI marker". runOCR and blankImage both go through here,
// so OCR on a JPEG-scanned page and redaction of a JPEG image were both broken.
func TestDecodeImage_JPEGDecodes(t *testing.T) {
	t.Parallel()

	want := color.RGBA{R: 200, G: 60, B: 40, A: 255}
	ctx, doc, page := imagePDF(t, jpegPDF("/DeviceRGB", 8, solidJPEG(t, 8, want, false)))

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	if got := img.Bounds(); got.Dx() != 8 || got.Dy() != 8 {
		t.Fatalf("bounds = %v, want 8x8", got)
	}
	r, g, b, _ := img.At(4, 4).RGBA()
	if !near(uint8(r>>8), want.R) || !near(uint8(g>>8), want.G) || !near(uint8(b>>8), want.B) {
		t.Errorf("pixel = (%d,%d,%d), want about (%d,%d,%d)",
			r>>8, g>>8, b>>8, want.R, want.G, want.B)
	}
}

// TestDecodeImage_GrayscaleJPEGDecodes covers the one-component codestream,
// where the pixel loop must take one byte per pixel rather than three.
func TestDecodeImage_GrayscaleJPEGDecodes(t *testing.T) {
	t.Parallel()

	ctx, doc, page := imagePDF(t,
		jpegPDF("/DeviceGray", 8, solidJPEG(t, 8, color.Gray{Y: 90}, true)))

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	r, _, _, _ := img.At(4, 4).RGBA()
	if !near(uint8(r>>8), 90) {
		t.Errorf("grey = %d, want about 90", r>>8)
	}
}

// TestDecodeImage_JPEGShapeBeatsTheDeclaredColorSpace is the load-bearing case
// for measuring rather than believing.
//
// The codestream is grayscale while the dictionary says /DeviceRGB, which real
// files do. Taking three components from the declaration asks for 192 bytes
// where 64 arrived, so the decode fails with "decoded data too short" on a file
// every other reader opens. The shape of the samples decides.
func TestDecodeImage_JPEGShapeBeatsTheDeclaredColorSpace(t *testing.T) {
	t.Parallel()

	ctx, doc, page := imagePDF(t,
		jpegPDF("/DeviceRGB", 8, solidJPEG(t, 8, color.Gray{Y: 200}, true)))

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage: %v (the declared colour space was believed over the samples)", err)
	}
	r, g, b, _ := img.At(4, 4).RGBA()
	if !near(uint8(r>>8), 200) || !near(uint8(g>>8), 200) || !near(uint8(b>>8), 200) {
		t.Errorf("pixel = (%d,%d,%d), want about (200,200,200)", r>>8, g>>8, b>>8)
	}
}

// jpegPDFWithDecode builds the same page as jpegPDF with an added /Decode array.
func jpegPDFWithDecode(colorSpace, decode string, size int, data string) []string {
	objs := jpegPDF(colorSpace, size, data)
	objs[3] = fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d"+
		" /ColorSpace %s /BitsPerComponent 8 /Filter /DCTDecode /Decode %s /Length %d >>\nstream\n%s\nendstream",
		size, size, colorSpace, decode, len(data), data)
	return objs
}

// TestDecodeImage_JPEGHonoursDecodeArray pins that a JPEG's /Decode array now
// reaches its pixels.
//
// It did not before: the old DCT branch returned image/jpeg's output directly
// and never built the /Decode tables. Now a JPEG takes the ordinary sample path,
// where buildDecodeLUTs applies the array like any other image. That difference
// decides whether a caller re-encoding these pixels may drop the entry, which is
// what AppliesDecodeArray answers, so it is pinned rather than assumed.
func TestDecodeImage_JPEGHonoursDecodeArray(t *testing.T) {
	t.Parallel()

	// Grey 200 inverted by /Decode [1 0] is 55.
	ctx, doc, page := imagePDF(t,
		jpegPDFWithDecode("/DeviceGray", "[1 0]", 8, solidJPEG(t, 8, color.Gray{Y: 200}, true)))

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	r, _, _, _ := img.At(4, 4).RGBA()
	if !near(uint8(r>>8), 55) {
		t.Errorf("grey = %d, want about 55: /Decode [1 0] was not applied to the JPEG", r>>8)
	}
}

// TestDecodeImage_IndexedColourJPEGIsRefused pins a hazard that routing JPEGs
// through the ordinary sample path would otherwise have introduced.
//
// An indexed image holds one index per pixel, but a colour JPEG decodes to three
// bytes per pixel. decodeIndexed checks only that it has at least width*height
// bytes, which 3*width*height satisfies, so it would read colour components as
// palette indices and return a wrong picture with no error. Before JPEGs decoded
// at all this combination simply failed; trading that error for a silent
// mis-render would be the wrong direction.
func TestDecodeImage_IndexedColourJPEGIsRefused(t *testing.T) {
	t.Parallel()

	cs := "[/Indexed /DeviceRGB 1 <FF0000 00FF00>]"
	data := solidJPEG(t, 8, color.RGBA{R: 200, G: 60, B: 40, A: 255}, false)
	ctx, doc, page := imagePDF(t, jpegPDF(cs, 8, data))

	if _, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0")); err == nil {
		t.Error("a colour JPEG in an indexed colour space decoded, but its samples are not indices")
	}
}

// TestDecodeImage_IndexedGrayscaleJPEGDecodes is the other half: the guard above
// must not refuse the shape that can legitimately carry indices, one byte per
// pixel.
func TestDecodeImage_IndexedGrayscaleJPEGDecodes(t *testing.T) {
	t.Parallel()

	cs := "[/Indexed /DeviceRGB 1 <FF0000 00FF00>]"
	data := solidJPEG(t, 8, color.Gray{Y: 0}, true)
	ctx, doc, page := imagePDF(t, jpegPDF(cs, 8, data))

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage refused one index per pixel: %v", err)
	}
	if got := img.Bounds(); got.Dx() != 8 || got.Dy() != 8 {
		t.Errorf("bounds = %v, want 8x8", got)
	}
}

// TestJpegSampleSpace pins the deduction itself, including the lengths that
// correspond to no image this decoder can build.
func TestJpegSampleSpace(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		n, w, h       int
		want          string
		wantSupported bool
	}{
		{"one byte per pixel is grey", 64, 8, 8, "DeviceGray", true},
		{"three is RGB", 192, 8, 8, "DeviceRGB", true},
		{"four is CMYK", 256, 8, 8, "DeviceCMYK", true},
		{"two is nothing this builds", 128, 8, 8, "", false},
		{"a partial pixel is refused", 100, 8, 8, "", false},
		{"zero pixels cannot divide", 64, 0, 8, "", false},
	} {
		got, ok := jpegSampleSpace(tc.n, tc.w, tc.h)
		if ok != tc.wantSupported || got != tc.want {
			t.Errorf("%s: jpegSampleSpace(%d,%d,%d) = (%q,%v), want (%q,%v)",
				tc.name, tc.n, tc.w, tc.h, got, ok, tc.want, tc.wantSupported)
		}
	}
}
