package pdf

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

// jpegColorPDF draws one 96 x 64 RGB JPEG of colour gradients.
func jpegColorPDF(t *testing.T) []byte {
	t.Helper()
	const w, h = 96, 64
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x * 255 / w), G: uint8(y * 255 / h), B: uint8((x + y) % 256), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return colorFixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 96 64] /Contents 4 0 R /Resources << /XObject << /Im0 5 0 R >> >> >>",
		colorStream("", "q 96 0 0 64 0 0 cm /Im0 Do Q"),
		colorStream("/Type /XObject /Subtype /Image /Width 96 /Height 64 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode", buf.String()),
	})
}

// TestConvertColorsBuiltin_JPEGQuality pins that WithJPEGQuality writes a
// converted JPEG back as DCTDecode, smaller than the lossless default, which
// writes it under FlateDecode.
func TestConvertColorsBuiltin_JPEGQuality(t *testing.T) {
	t.Parallel()
	src := jpegColorPDF(t)
	_, lossless := convertBuiltin(t, src, CMYKColorTarget(), WithoutOutputIntent())
	report, lossy := convertBuiltin(t, src, CMYKColorTarget(), WithoutOutputIntent(), WithJPEGQuality(80))
	if report.ConvertedImages != 1 {
		t.Fatalf("report = %+v, want one converted image", report)
	}
	if bytes.Contains(lossless, []byte("/DCTDecode")) || !bytes.Contains(lossy, []byte("/DCTDecode")) {
		t.Errorf("DCTDecode in the lossless output %v, in the JPEG output %v", bytes.Contains(lossless, []byte("/DCTDecode")), bytes.Contains(lossy, []byte("/DCTDecode")))
	}
	if len(lossy) >= len(lossless) {
		t.Errorf("JPEG output %d bytes, lossless %d", len(lossy), len(lossless))
	}
	uses, err := openColorPDF(t, lossy).ExtractColorSpaces(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range uses {
		if u.Family != "DeviceCMYK" {
			t.Errorf("the output paints in %+v", u)
		}
	}
}

func TestConvertColors_RefusesJPEGQualityOutOfRange(t *testing.T) {
	t.Parallel()
	for _, q := range []int{0, -5, 101} {
		w := NewWriter(openColorPDF(t, recolorPDF("")))
		f := &fakeRecolor{}
		w.recolor = f.engine
		if _, err := w.ConvertColors(context.Background(), CMYKColorTarget(), WithJPEGQuality(q)); !errors.Is(err, ErrInvalidColorTarget) {
			t.Errorf("quality %d: %v, want ErrInvalidColorTarget", q, err)
		}
		if f.calls != 0 {
			t.Errorf("quality %d: the engine ran", q)
		}
		_ = w.Close()
	}
	f := &fakeRecolor{}
	convertAndWrite(t, openColorPDF(t, recolorPDF("")), f, CMYKColorTarget(), WithJPEGQuality(72))
	if f.req.jpegQuality != 72 {
		t.Errorf("request quality %d, want 72", f.req.jpegQuality)
	}
}
