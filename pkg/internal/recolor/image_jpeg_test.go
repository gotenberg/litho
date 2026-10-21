package recolor

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter/dct"
)

// photoJPEG returns a w x h JPEG of smooth colour gradients, a photograph to
// an encoder, written by the standard library.
func photoJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			fx, fy := float64(x)/float64(w), float64(y)/float64(h)
			src.Set(x, y, color.RGBA{
				R: uint8(255 * fx),
				G: uint8(127 + 127*math.Sin(6*fy)),
				B: uint8(255 * (1 - fx*fy)),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// meanSampleDiff returns the mean difference between two renderings,
// channel by channel.
func meanSampleDiff(t *testing.T, a, b []uint8) float64 {
	t.Helper()
	if len(a) != len(b) {
		t.Fatalf("renderings of %d and %d values", len(a), len(b))
	}
	sum := 0.0
	for i := range a {
		sum += math.Abs(float64(a[i]) - float64(b[i]))
	}
	return sum / float64(len(a))
}

// TestConvertPixels_JPEGQuality pins that a DCTDecode image re-encodes as a
// JPEG under JPEGQuality, smaller than the lossless encoding and drawing
// what it draws within the loss of the quality, for every target family.
func TestConvertPixels_JPEGQuality(t *testing.T) {
	const w, h = 64, 48
	rgb := photoJPEG(t, w, h)
	// A CMYK source for the RGB target: the RGB photo's samples
	// complemented, written by the encoder under test.
	samples, _, _, _, err := dct.Decode(context.Background(), rgb, -1, h, dct.DefaultMaxSamples)
	if err != nil {
		t.Fatal(err)
	}
	cmyk := make([]byte, 0, w*h*4)
	for i := 0; i < len(samples); i += 3 {
		cmyk = append(cmyk, 255-samples[i], 255-samples[i+1], 255-samples[i+2], 0)
	}
	cmykJPEG, err := dct.Encode(context.Background(), cmyk, w, h, 4, dct.EncodeOptions{Quality: 95})
	if err != nil {
		t.Fatal(err)
	}
	for target, source := range map[ast.PdfName]struct {
		space ast.PdfName
		data  []byte
	}{familyGray: {familyRGB, rgb}, familyRGB: {familyCMYK, cmykJPEG}, familyCMYK: {familyRGB, rgb}} {
		t.Run(string(target), func(t *testing.T) {
			p := newTestPage(w, h, imageStream(w, h, source.space, 8, source.data, ast.PdfDict{"Filter": ast.PdfName("DCTDecode")}))
			colors := fixedColors(deviceColors{tc: deviceTarget(target).Components})
			lossless, im0, _, err := p.convert(t, deviceTarget(target), colors)
			if err != nil {
				t.Fatal(err)
			}
			cfg := deviceTarget(target)
			cfg.JPEGQuality = 85
			lossy, im, outcome, err := p.convert(t, cfg, colors)
			if err != nil || outcome != ImageConverted {
				t.Fatalf("outcome %v, %v", outcome, err)
			}
			out := pending(t, lossy, 7)
			if out.Dict["Filter"] != ast.PdfName("DCTDecode") {
				t.Fatalf("Filter = %v, want DCTDecode", out.Dict["Filter"])
			}
			if _, ok := out.Dict["DecodeParms"]; ok {
				t.Errorf("DecodeParms = %v, want none", out.Dict["DecodeParms"])
			}
			if out.Dict["ColorSpace"] != target || out.Dict["BitsPerComponent"] != ast.PdfInt(8) {
				t.Errorf("ColorSpace %v, BitsPerComponent %v", out.Dict["ColorSpace"], out.Dict["BitsPerComponent"])
			}
			raw, err := out.Bytes(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			_, n, dw, dh, err := dct.Decode(context.Background(), raw, -1, h, dct.DefaultMaxSamples)
			if err != nil || n != cfg.Components || dw != w || dh != h {
				t.Fatalf("the JPEG decodes to %dx%d of %d components: %v", dw, dh, n, err)
			}
			before, after := im0.Report().Encoded["DCTDecode"], im.Report().Encoded["DCTDecode"]
			if after.After >= before.After {
				t.Errorf("JPEG %d bytes, lossless %d", after.After, before.After)
			}
			if d := meanSampleDiff(t, p.with(lossless).render(t), p.with(lossy).render(t)); d > 2 {
				t.Errorf("mean difference %.2f from the lossless conversion", d)
			} else {
				t.Logf("mean %.3f, %d vs %d bytes", d, after.After, before.After)
			}
		})
	}
}

// TestConvertPixels_JPEGQualityKeepsSmallerLossless pins that a flat image,
// which FlateDecode holds in fewer bytes than any JPEG, stays lossless.
func TestConvertPixels_JPEGQualityKeepsSmallerLossless(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 32, 32))
	for i := range src.Pix {
		src.Pix[i] = 90
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, nil); err != nil {
		t.Fatal(err)
	}
	p := newTestPage(32, 32, imageStream(32, 32, familyGray, 8, buf.Bytes(), ast.PdfDict{"Filter": ast.PdfName("DCTDecode")}))
	cfg := deviceTarget(familyCMYK)
	cfg.JPEGQuality = 90
	alloc, _, _, err := p.convert(t, cfg, fixedColors(deviceColors{tc: 4}))
	if err != nil {
		t.Fatal(err)
	}
	if f := pending(t, alloc, 7).Dict["Filter"]; f != ast.PdfName("FlateDecode") {
		t.Errorf("Filter = %v, want FlateDecode", f)
	}
}

// TestConvertPixels_JPEGQualityOnlyForJPEG pins that a source of another
// filter stays lossless whatever the quality.
func TestConvertPixels_JPEGQualityOnlyForJPEG(t *testing.T) {
	const w, h = 16, 16
	samples := make([]byte, w*h*3)
	for i := range samples {
		samples[i] = byte(i * 7)
	}
	p := newTestPage(w, h, imageStream(w, h, familyRGB, 8, samples, nil))
	cfg := deviceTarget(familyCMYK)
	cfg.JPEGQuality = 50
	alloc, _, _, err := p.convert(t, cfg, fixedColors(deviceColors{tc: 4}))
	if err != nil {
		t.Fatal(err)
	}
	if f := pending(t, alloc, 7).Dict["Filter"]; f != ast.PdfName("FlateDecode") {
		t.Errorf("Filter = %v, want FlateDecode", f)
	}
}

// TestConvertPixels_JPEGQualityKeepsSMask pins that the soft mask of a
// re-encoded JPEG is kept as it was, the same object.
func TestConvertPixels_JPEGQualityKeepsSMask(t *testing.T) {
	const w, h = 64, 48
	p := newTestPage(w, h, imageStream(w, h, familyRGB, 8, photoJPEG(t, w, h), ast.PdfDict{
		"Filter": ast.PdfName("DCTDecode"), "SMask": ast.PdfIndirectRef{ObjectNum: 8},
	}))
	p.objs[8] = imageStream(1, 1, familyGray, 8, []byte{128}, nil)
	cfg := deviceTarget(familyCMYK)
	cfg.JPEGQuality = 75
	alloc, _, _, err := p.convert(t, cfg, fixedColors(deviceColors{tc: 4}))
	if err != nil {
		t.Fatal(err)
	}
	out := pending(t, alloc, 7)
	if out.Dict["Filter"] != ast.PdfName("DCTDecode") || out.Dict["SMask"] != (ast.PdfIndirectRef{ObjectNum: 8}) {
		t.Errorf("Filter %v, SMask %v, want DCTDecode over the same soft mask", out.Dict["Filter"], out.Dict["SMask"])
	}
	if _, queued := alloc.Pending()[8]; queued {
		t.Error("the soft mask was rewritten")
	}
}

func TestNewImages_RefusesJPEGQualityOutOfRange(t *testing.T) {
	p := newTestPage(1, 1, imageStream(1, 1, familyRGB, 8, []byte{1, 2, 3}, nil))
	doc, _ := p.open(t)
	for _, q := range []int{-1, 101} {
		cfg := deviceTarget(familyGray)
		cfg.Colors, cfg.JPEGQuality = deviceColors{tc: 1}, q
		if _, err := NewImages(doc, nil, cfg); err == nil {
			t.Errorf("quality %d accepted", q)
		}
	}
}
