package image

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// tintedImagePDF builds a one-page document drawing a 4x1 image in a Separation
// or DeviceN colour space. cs is the colour space array, written inline.
func tintedImagePDF(cs string, samples []byte) []string {
	data := string(samples)
	return []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /XObject << /Im0 4 0 R >> >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 4 /Height 1"+
			" /ColorSpace %s /BitsPerComponent 8 /Length %d >>\nstream\n%s\nendstream",
			cs, len(data), data),
	}
}

// TestDecodeImage_DeviceNWithMoreColorantsThanThreeIsRefused pins the silent
// corruption this guard exists to stop.
//
// resolveColorSpace answers "DeviceRGB" for a DeviceN space, which assumes three
// components. A seven-colorant image holds seven bytes a pixel, so three bytes a
// pixel is SHORTER than the data: the length check passes, the decoder reads
// bytes 0,1,2 as the first pixel and 3,4,5 as the second, and colorants are
// scrambled across pixel boundaries while 4/7 of the stream is never read. No
// error is reported, so nothing downstream can tell.
//
// Measured on 135 images in thread_400_400932.pdf: /Im18 is 57x1 over seven
// colorants, its decoded stream is 399 bytes = 57*7 exactly, litho wanted 171,
// and 171 <= 399 so every one of them decoded "successfully". DecodeImage feeds
// mutations.blankImage, which re-encodes these pixels back into the document.
func TestDecodeImage_DeviceNWithMoreColorantsThanThreeIsRefused(t *testing.T) {
	t.Parallel()

	const cs = "[/DeviceN [/Cyan /Magenta /Yellow /Black /Spot1 /Spot2 /Spot3]" +
		" /DeviceRGB 5 0 R]"
	// 4 pixels x 7 colorants = 28 bytes, comfortably more than the 12 a
	// three-component read demands.
	samples := make([]byte, 4*7)
	for i := range samples {
		samples[i] = byte(i * 9)
	}

	objs := append(tintedImagePDF(cs, samples),
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0] /C1 [1 1 1] /N 1 >>")
	ctx, doc, page := imagePDF(t, objs)

	_, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err == nil {
		t.Fatal("DecodeImage silently read a 7-colorant DeviceN image as 3-component RGB")
	}
	if !strings.Contains(err.Error(), "DeviceN") {
		t.Errorf("refusal does not name the colour space family: %v", err)
	}
	if !strings.Contains(err.Error(), "7") {
		t.Errorf("refusal does not name the colorant count: %v", err)
	}
}

// TestDecodeImage_SeparationIsRefusedWithItsOwnReason pins that a Separation
// image is refused for the right reason rather than for a wrong length.
//
// A Separation space has exactly one component (ISO 32000-1, 8.6.6.4), so its
// stream is one byte a pixel. Read as three, complete data came back as "decoded
// data too short for RGB image", blaming the file. Measured at ratio exactly
// 3.0000 on thread_200_200192.pdf and thread_200_200196.pdf.
func TestDecodeImage_SeparationIsRefusedWithItsOwnReason(t *testing.T) {
	t.Parallel()

	const cs = "[/Separation /Spot1 /DeviceRGB 5 0 R]"
	objs := append(tintedImagePDF(cs, []byte{10, 20, 30, 40}),
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0] /C1 [1 1 1] /N 1 >>")
	ctx, doc, page := imagePDF(t, objs)

	_, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err == nil {
		t.Fatal("DecodeImage accepted a 1-colorant Separation image as 3-component RGB")
	}
	if strings.Contains(err.Error(), "too short") {
		t.Errorf("refusal still blames the data length: %v", err)
	}
	if !strings.Contains(err.Error(), "Separation") {
		t.Errorf("refusal does not name the colour space family: %v", err)
	}
}

// TestDecodeImage_DeviceNWithThreeColorantsStillDecodes pins that the guard is
// keyed on the COUNT, not on the family.
//
// Where a DeviceN space names exactly three colorants the substituted stride is
// right by accident, and treating the tints as RGB is the documented behaviour
// of resolveColorSpace. Refusing those would regress images that decode today:
// the pdfjs fixtures colorspace_atan.pdf, colorspace_cos.pdf and
// colorspace_sin.pdf are each a 256x256 FlateDecode image over
// /DeviceN [/X /Y /Z], and they must keep working.
func TestDecodeImage_DeviceNWithThreeColorantsStillDecodes(t *testing.T) {
	t.Parallel()

	const cs = "[/DeviceN [/X /Y /Z] /DeviceRGB 5 0 R]"
	samples := []byte{
		255, 0, 0,
		0, 255, 0,
		0, 0, 255,
		255, 255, 255,
	}
	objs := append(tintedImagePDF(cs, samples),
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0] /C1 [1 1 1] /N 1 >>")
	ctx, doc, page := imagePDF(t, objs)

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage refused a 3-colorant DeviceN image: %v", err)
	}
	if got := img.Bounds(); got.Dx() != 4 || got.Dy() != 1 {
		t.Fatalf("bounds = %v, want 4x1", got)
	}
	r, g, b, _ := img.At(0, 0).RGBA()
	if r>>8 != 255 || g>>8 != 0 || b>>8 != 0 {
		t.Errorf("first pixel = (%d,%d,%d), want the samples read straight through", r>>8, g>>8, b>>8)
	}
}

// TestTintedColorSpace pins the colorant count the guard reads.
func TestTintedColorSpace(t *testing.T) {
	t.Parallel()

	ctx, doc, _ := imagePDF(t, jpxImagePDF("/FlateDecode", "/BitsPerComponent 8"))

	for _, tc := range []struct {
		name       string
		dict       ast.PdfDict
		wantFamily string
		wantCount  int
		wantOK     bool
	}{
		{
			"a Separation has exactly one colorant",
			ast.PdfDict{"ColorSpace": ast.PdfArray{
				ast.PdfName("Separation"), ast.PdfName("Spot1"), ast.PdfName("DeviceRGB"),
			}},
			"Separation", 1, true,
		},
		{
			"a DeviceN has one per name",
			ast.PdfDict{"ColorSpace": ast.PdfArray{
				ast.PdfName("DeviceN"),
				ast.PdfArray{ast.PdfName("C"), ast.PdfName("M"), ast.PdfName("Y"), ast.PdfName("K")},
				ast.PdfName("DeviceRGB"),
			}},
			"DeviceN", 4, true,
		},
		{
			"a device space is not tinted",
			ast.PdfDict{"ColorSpace": ast.PdfName("DeviceRGB")},
			"", 0, false,
		},
		{
			"an indexed space is not tinted",
			ast.PdfDict{"ColorSpace": ast.PdfArray{
				ast.PdfName("Indexed"), ast.PdfName("DeviceRGB"), ast.PdfInt(1), ast.PdfString("\xff\x00\x00\x00\xff\x00"),
			}},
			"", 0, false,
		},
	} {
		family, count, ok := tintedColorSpace(ctx, doc, tc.dict)
		if ok != tc.wantOK || family != tc.wantFamily || count != tc.wantCount {
			t.Errorf("%s: got (%q, %d, %v), want (%q, %d, %v)",
				tc.name, family, count, ok, tc.wantFamily, tc.wantCount, tc.wantOK)
		}
	}
}
