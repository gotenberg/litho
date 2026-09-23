package image

import (
	"image/color"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// labBase is the base colour space of the two pdfjs documents that exposed the
// refusal: /Range and /WhitePoint are theirs verbatim. The white point is D50
// (Z < 1), which selects the D50 matrix in [labEntryToRGB].
const labBase = "[/Lab << /Range [-128.0 127.0 -128.0 127.0]" +
	" /WhitePoint [0.964203 1.0 0.824905] /BlackPoint [0.0 0.0 0.0] >>]"

// TestDecodeIndexedLabImage pins that an indexed image over a /Lab base decodes.
//
// Before this it could not: imageComponents knows no count for Lab, so the
// palette went unresolved, resolveColorSpace collapsed /Indexed to that base and
// defaulted it to DeviceRGB, and the decoder demanded three bytes for every
// pixel that holds one index. Measured on 4 images in 2 pdfjs documents
// (issue10339_reduced.pdf, issue2761.pdf); no other corpus holds one.
//
// The expected values are computed from ISO 32000-1, 8.6.6.3 and 8.6.5.4 with
// the white point above, not eyeballed. Byte 128 maps a* and b* to exactly 0
// under /Range [-128 127], so the grey entries are exact anchors. Note white
// comes back 255,254,254 rather than 255,255,255: the D50 matrix and the
// square-root transfer leave green and blue one short, and asserting pure white
// would be asserting an intuition over the arithmetic.
func TestDecodeIndexedLabImage(t *testing.T) {
	t.Parallel()

	// L* 100 / 0 / 50.2 grey, then a saturated a* = +127.
	lookup := "<FF8080 008080 808080 80FF80>"
	ctx, doc, page := imagePDF(t, indexedImagePDF(labBase, lookup, []byte{0, 1, 2, 3}))

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage refused an indexed image over a /Lab base: %v", err)
	}
	if got := img.Bounds(); got.Dx() != 2 || got.Dy() != 2 {
		t.Fatalf("bounds = %v, want 2x2", got)
	}

	for _, tc := range []struct {
		x, y int
		want color.RGBA
		note string
	}{
		{0, 0, color.RGBA{R: 255, G: 254, B: 254, A: 255}, "L*=100 a*=0 b*=0"},
		{1, 0, color.RGBA{R: 0, G: 0, B: 0, A: 255}, "L*=0 a*=0 b*=0"},
		{0, 1, color.RGBA{R: 109, G: 109, B: 109, A: 255}, "L*=50.196 a*=0 b*=0"},
		{1, 1, color.RGBA{R: 255, G: 0, B: 117, A: 255}, "L*=50.196 a*=127 b*=0"},
	} {
		r, g, b, a := img.At(tc.x, tc.y).RGBA()
		got := color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
		if got != tc.want {
			t.Errorf("pixel (%d,%d) [%s] = %v, want %v", tc.x, tc.y, tc.note, got, tc.want)
		}
	}
}

// TestDecodeIndexedLabClampsOutOfGamut pins the clamp in both directions.
//
// The Lab to sRGB matrix leaves [0, 1] in both directions for colours outside
// the sRGB gamut, and real palettes hold them. The two entries here are the two
// halves, and only one of them is observable:
//
//   - a* = +127 puts red at 1.3189. Without the upper clamp math.Sqrt gives
//     1.1484, times 255 is 292.8, and the conversion wraps to 36: the brightest
//     channel in the image comes out nearly black. This is what the assertion
//     below actually catches.
//   - a* = -128 with b* = +127 drives red and blue negative. math.Sqrt of a
//     negative is NaN, and converting NaN to uint8 currently yields 0 here,
//     which is the same answer the clamp gives. That agreement is incidental:
//     Go leaves an out-of-range float to integer conversion
//     implementation-defined, so the clamp is what makes the result 0 by
//     contract rather than by luck.
//
// An earlier version of this test asserted only the negative entry and passed
// with the clamp removed, which is why both are pinned now.
func TestDecodeIndexedLabClampsOutOfGamut(t *testing.T) {
	t.Parallel()

	// Entry 0 overshoots above 1, entry 1 goes negative.
	ctx, doc, page := imagePDF(t,
		indexedImagePDF(labBase, "<80FF80 8000FF>", []byte{0, 1, 0, 1}))

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}

	for _, tc := range []struct {
		x, y int
		want color.RGBA
		note string
	}{
		{0, 0, color.RGBA{R: 255, G: 0, B: 117, A: 255}, "red 1.3189 clamped down to 1"},
		{1, 0, color.RGBA{R: 0, G: 145, B: 0, A: 255}, "red and blue negative, clamped up to 0"},
	} {
		r, g, b, a := img.At(tc.x, tc.y).RGBA()
		got := color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
		if got != tc.want {
			t.Errorf("pixel (%d,%d) [%s] = %v, want %v: the clamp did not hold",
				tc.x, tc.y, tc.note, got, tc.want)
		}
	}
}

// TestDecodeIndexedLabWithoutWhitePointIsRefused pins that a /Lab base missing
// its required /WhitePoint is refused rather than decoded against a guess.
//
// ISO 32000-1, Table 65 makes /WhitePoint required. Substituting a default
// illuminant would render every pixel in a colour the file never specified,
// which is worse than the refusal commit aea54967 added.
func TestDecodeIndexedLabWithoutWhitePointIsRefused(t *testing.T) {
	t.Parallel()

	ctx, doc, page := imagePDF(t,
		indexedImagePDF("[/Lab << /Range [-100 100 -100 100] >>]", "<FF8080>", []byte{0, 0, 0, 0}))

	_, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err == nil {
		t.Fatal("DecodeImage accepted a /Lab base with no /WhitePoint")
	}
	if strings.Contains(err.Error(), "too short") {
		t.Errorf("refusal blames the data length rather than the colour space: %v", err)
	}
	if !strings.Contains(err.Error(), "Lab") {
		t.Errorf("refusal does not name the base colour space: %v", err)
	}
}

// TestDecodeIndexedLabRangeIsHonoured pins that /Range is read rather than
// assumed to be the [-100 100] default.
//
// Both documents that exposed this declare [-128 127 -128 127]. A decoder that
// hardcoded the default would scale a* and b* by 200/255 instead of 255/255,
// shifting every chromatic pixel with no error anywhere.
func TestDecodeIndexedLabRangeIsHonoured(t *testing.T) {
	t.Parallel()

	const narrow = "[/Lab << /Range [-10 10 -10 10]" +
		" /WhitePoint [0.964203 1.0 0.824905] >>]"

	// Same bytes, two ranges: byte 0xFF is a* = 127 under the wide range and
	// a* = 10 under the narrow one, so the results must differ.
	ctx, doc, page := imagePDF(t, indexedImagePDF(labBase, "<80FF80>", []byte{0, 0, 0, 0}))
	wide, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage (wide range): %v", err)
	}
	ctx, doc, page = imagePDF(t, indexedImagePDF(narrow, "<80FF80>", []byte{0, 0, 0, 0}))
	got, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage (narrow range): %v", err)
	}

	wr, wg, wb, _ := wide.At(0, 0).RGBA()
	nr, ng, nb, _ := got.At(0, 0).RGBA()
	if wr == nr && wg == ng && wb == nb {
		t.Errorf("/Range made no difference: both (%d,%d,%d), so the array is not being read",
			wr>>8, wg>>8, wb>>8)
	}
}
