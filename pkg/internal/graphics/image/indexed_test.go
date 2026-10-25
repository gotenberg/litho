package image

import (
	"fmt"
	"image/color"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// indexedImagePDF builds a one-page document whose page draws a 2x2 indexed
// image. samples are the palette indices, lookup the colour table, and base the
// colour space its entries are written in.
func indexedImagePDF(base, lookup string, samples []byte) []string {
	data := string(samples)
	return []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /XObject << /Im0 4 0 R >> >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 2 /Height 2"+
			" /ColorSpace [/Indexed %s 3 %s] /BitsPerComponent 8 /Length %d >>\nstream\n%s\nendstream",
			base, lookup, len(data), data),
	}
}

// TestDecodeIndexedImage pins that an image in an indexed colour space decodes.
//
// Per ISO 32000-1, 8.6.6.3 an indexed image holds one sample per pixel and that
// sample is an index into the colour table, not a colour. The decoder took the
// base space's component count instead, so it demanded three or four bytes for
// every pixel that holds one and every palette image failed outright with
// "decoded data too short". No palette image could be decoded at all, which is
// what OCR and redaction need.
//
// # Corpus evidence
//
//   - pdfjs 160F-2019.pdf, S2.pdf: among the palette images that failed with
//     "decoded data too short".
func TestDecodeIndexedImage(t *testing.T) {
	t.Parallel()

	// Four RGB entries: red, green, blue, white.
	const rgbLookup = "<FF0000 00FF00 0000FF FFFFFF>"
	ctx, doc, page := imagePDF(t, indexedImagePDF("/DeviceRGB", rgbLookup, []byte{0, 1, 2, 3}))

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	if got := img.Bounds(); got.Dx() != 2 || got.Dy() != 2 {
		t.Fatalf("bounds = %v, want 2x2", got)
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

// TestDecodeIndexedGrayImage pins the one-component base, where the palette
// entry is a single grey level rather than a triple.
func TestDecodeIndexedGrayImage(t *testing.T) {
	t.Parallel()

	ctx, doc, page := imagePDF(t, indexedImagePDF("/DeviceGray", "<00 40 80 FF>", []byte{0, 1, 2, 3}))

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	for i, tc := range []struct {
		x, y int
		want uint8
	}{{0, 0, 0x00}, {1, 0, 0x40}, {0, 1, 0x80}, {1, 1, 0xFF}} {
		r, g, b, _ := img.At(tc.x, tc.y).RGBA()
		if uint8(r>>8) != tc.want || uint8(g>>8) != tc.want || uint8(b>>8) != tc.want {
			t.Errorf("pixel %d = (%d,%d,%d), want grey %d", i, r>>8, g>>8, b>>8, tc.want)
		}
	}
}

// TestDecodeIndexedShortLookup pins that a colour table too short for the
// indices used reads as black rather than failing or reading past its end. The
// table comes from the file, so its length is not to be trusted.
func TestDecodeIndexedShortLookup(t *testing.T) {
	t.Parallel()

	// One entry for four indices.
	ctx, doc, page := imagePDF(t, indexedImagePDF("/DeviceRGB", "<FF0000>", []byte{0, 1, 2, 3}))

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	r, g, b, _ := img.At(0, 0).RGBA()
	if uint8(r>>8) != 255 || g != 0 || b != 0 {
		t.Errorf("index 0 = (%d,%d,%d), want red from the one entry present", r>>8, g>>8, b>>8)
	}
	for _, p := range [][2]int{{1, 0}, {0, 1}, {1, 1}} {
		r, g, b, a := img.At(p[0], p[1]).RGBA()
		if r != 0 || g != 0 || b != 0 || uint8(a>>8) != 255 {
			t.Errorf("index past the table at %v = (%d,%d,%d,%d), want opaque black", p, r>>8, g>>8, b>>8, a>>8)
		}
	}
}

// TestDecodeIndexedTruncatedSamples pins what a sample run shorter than the
// declared size decodes to: the indices it holds, a partial row's among them,
// and index 0 for every one it lacks, the table's first colour, as PDFium and
// MuPDF read it (see [padShortSamples]). One index is enough to decode.
func TestDecodeIndexedTruncatedSamples(t *testing.T) {
	t.Parallel()

	ctx, doc, page := imagePDF(t, indexedImagePDF("/DeviceRGB", "<FF0000 00FF00>", []byte{0, 1, 1}))

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	for _, tc := range []struct {
		x, y    int
		r, g, b uint32
	}{
		{0, 0, 255, 0, 0},
		{1, 0, 0, 255, 0},
		{0, 1, 0, 255, 0},
		{1, 1, 255, 0, 0},
	} {
		r, g, b, _ := img.At(tc.x, tc.y).RGBA()
		if r>>8 != tc.r || g>>8 != tc.g || b>>8 != tc.b {
			t.Errorf("pixel (%d,%d) = (%d,%d,%d), want (%d,%d,%d)", tc.x, tc.y, r>>8, g>>8, b>>8, tc.r, tc.g, tc.b)
		}
	}

	ctx, doc, page = imagePDF(t, indexedImagePDF("/DeviceRGB", "<FF0000 00FF00>", []byte{1}))
	img, err = DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage of one index: %v", err)
	}
	if r, g, _, _ := img.At(1, 1).RGBA(); r>>8 != 255 || g != 0 {
		t.Errorf("a missing index = (%d,%d), want red, index 0", r>>8, g>>8)
	}
}
