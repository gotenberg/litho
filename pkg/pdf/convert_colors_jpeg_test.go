package pdf

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
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

// jpxColorPDF draws the JPEG 2000 codestream testdata/jpx/name, 96 x 64
// RGB pixels of noisy gradients, under the image dictionary entries extra.
// The codestreams were written by OpenJPEG's opj_compress (-I -r 8), rgb8 at
// 8 bits a component and rgb16 at 16.
func jpxColorPDF(t *testing.T, name, extra string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/jpx/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return colorFixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 96 64] /Contents 4 0 R /Resources << /XObject << /Im0 5 0 R >> >> >>",
		colorStream("", "q 96 0 0 64 0 0 cm /Im0 Do Q"),
		colorStream("/Type /XObject /Subtype /Image /Width 96 /Height 64 /Filter /JPXDecode "+extra, string(data)),
	})
}

// TestConvertColorsBuiltin_JPEGQualityJPX pins that WithJPEGQuality writes a
// converted 8-bit JPEG 2000 image as DCTDecode, smaller than the lossless
// default, whether its dictionary names its colour space or leaves it to the
// JPEG 2000 data (ISO 32000-1, 7.4.9).
func TestConvertColorsBuiltin_JPEGQualityJPX(t *testing.T) {
	t.Parallel()
	for _, extra := range []string{"/ColorSpace /DeviceRGB /BitsPerComponent 8", ""} {
		src := jpxColorPDF(t, "rgb8.j2k", extra)
		_, lossless := convertBuiltin(t, src, CMYKColorTarget(), WithoutOutputIntent())
		report, lossy := convertBuiltin(t, src, CMYKColorTarget(), WithoutOutputIntent(), WithJPEGQuality(85))
		if report.ConvertedImages != 1 {
			t.Fatalf("%q: report = %+v, want one converted image", extra, report)
		}
		if bytes.Contains(lossless, []byte("/DCTDecode")) || bytes.Contains(lossless, []byte("/JPXDecode")) {
			t.Errorf("%q: the lossless output keeps a codec filter", extra)
		}
		if !bytes.Contains(lossy, []byte("/DCTDecode")) || bytes.Contains(lossy, []byte("/JPXDecode")) {
			t.Errorf("%q: the JPEG output is not DCTDecode alone", extra)
		}
		if len(lossy) >= len(lossless) {
			t.Errorf("%q: JPEG output %d bytes, lossless %d", extra, len(lossy), len(lossless))
		}
		uses, err := openColorPDF(t, lossy).ExtractColorSpaces(context.Background(), 1)
		if err != nil {
			t.Fatal(err)
		}
		for _, u := range uses {
			if u.Family != "DeviceCMYK" {
				t.Errorf("%q: the output paints in %+v", extra, u)
			}
		}
	}
}

// TestConvertColorsBuiltin_JPEGQualityKeeps16BitJPX pins that a JPEG 2000
// image deeper than 8 bits a component stays lossless under WithJPEGQuality:
// the output is the lossless default's, byte for byte.
func TestConvertColorsBuiltin_JPEGQualityKeeps16BitJPX(t *testing.T) {
	t.Parallel()
	for _, extra := range []string{"/ColorSpace /DeviceRGB /BitsPerComponent 16", ""} {
		src := jpxColorPDF(t, "rgb16.j2k", extra)
		_, lossless := convertBuiltin(t, src, CMYKColorTarget(), WithoutOutputIntent())
		report, kept := convertBuiltin(t, src, CMYKColorTarget(), WithoutOutputIntent(), WithJPEGQuality(85))
		if report.ConvertedImages != 1 {
			t.Fatalf("%q: report = %+v, want one converted image", extra, report)
		}
		if !bytes.Equal(kept, lossless) || bytes.Contains(kept, []byte("/DCTDecode")) {
			t.Errorf("%q: a 16-bit JPEG 2000 image was re-encoded as JPEG", extra)
		}
	}
	// The control: the same dictionary over the 8-bit codestream re-encodes.
	_, lossy := convertBuiltin(t, jpxColorPDF(t, "rgb8.j2k", ""), CMYKColorTarget(), WithoutOutputIntent(), WithJPEGQuality(85))
	if !bytes.Contains(lossy, []byte("/DCTDecode")) {
		t.Error("the 8-bit control was not re-encoded as JPEG")
	}
}

// TestConvertColors_SMaskInDataBecomesSMask pins that a JPEG 2000 image
// carrying its soft mask in its data (ISO 32000-1, 8.9.5, Table 89
// /SMaskInData) converts, by default and under WithJPEGQuality, with the
// mask extracted into an explicit /SMask: only JPXDecode data carries such a
// mask (7.4.9), so a re-encoding drops the entry. rgba8.jp2 and
// rgba8_premul.jp2 are 96 x 64 RGBA images written by OpenJPEG's
// opj_compress (lossless) from PNGs, the second with its colour
// premultiplied and its Channel Definition box retyping the opacity channel
// as premultiplied (T.800, Table I.16). rgb8.j2k holds no opacity channel,
// so the entry carries no mask and the image converts without one.
func TestConvertColors_SMaskInDataBecomesSMask(t *testing.T) {
	t.Parallel()
	cases := []struct {
		file, extra string
		masked      bool
	}{
		{"rgba8.jp2", "/ColorSpace /DeviceRGB /BitsPerComponent 8 /SMaskInData 1", true},
		{"rgba8_premul.jp2", "/SMaskInData 2", true},
		{"rgb8.j2k", "/ColorSpace /DeviceRGB /BitsPerComponent 8 /SMaskInData 1", false},
	}
	for _, tc := range cases {
		src := jpxColorPDF(t, tc.file, tc.extra)
		for _, opts := range [][]ColorOption{{WithoutOutputIntent()}, {WithoutOutputIntent(), WithJPEGQuality(85)}} {
			report, out := convertBuiltin(t, src, CMYKColorTarget(), opts...)
			if report.ConvertedImages != 1 {
				t.Errorf("%s, %d options: report = %+v, want one converted image", tc.file, len(opts), report)
			}
			if bytes.Contains(out, []byte("/SMaskInData")) {
				t.Errorf("%s, %d options: the output keeps /SMaskInData", tc.file, len(opts))
			}
			if got := bytes.Contains(out, []byte("/SMask ")); got != tc.masked {
				t.Errorf("%s, %d options: /SMask written = %v, want %v", tc.file, len(opts), got, tc.masked)
			}
		}
	}
}
