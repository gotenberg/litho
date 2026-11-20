package pdf

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	xbmp "golang.org/x/image/bmp"
	xtiff "golang.org/x/image/tiff"

	"github.com/gotenberg/litho/pkg/models"
)

// convertImageFile converts the image file data with opts and opens the PDF.
func convertImageFile(t *testing.T, data []byte, opts ...ImageOption) (*Reader, []byte, *models.ConvertReport) {
	t.Helper()
	ctx := context.Background()
	var out bytes.Buffer
	opts = append([]ImageOption{WithConvertFontDirs(convertFontDir(t))}, opts...)
	report, err := FromImage(bytes.NewReader(data), opts...).WriteTo(ctx, &out)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return r, out.Bytes(), report
}

// testPicture returns a picture of w by h pixels whose colours differ along
// both axes and between neighbours, so that a turned, mirrored or shifted
// drawing does not match it.
func testPicture(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 255 / max(w-1, 1)), G: uint8(y * 255 / max(h-1, 1)), B: uint8((x*7 + y*13) % 256), A: 0xFF})
		}
	}
	return img
}

// overWhite returns img composed over white, as a page shows it.
func overWhite(img image.Image) *image.NRGBA {
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Over)
	return out
}

// renderedPage renders page n of r at dpi.
func renderedPage(t *testing.T, r *Reader, n int, dpi float64) image.Image {
	t.Helper()
	img, err := r.RenderPage(context.Background(), n, WithRenderDPI(dpi))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// samePixels compares got and want pixel by pixel, each channel within tol
// levels, and reports the first pixel that differs.
func samePixels(t *testing.T, got, want image.Image, tol int) {
	t.Helper()
	gb, wb := got.Bounds(), want.Bounds()
	if gb.Dx() != wb.Dx() || gb.Dy() != wb.Dy() {
		t.Fatalf("drawn %d by %d pixels, want %d by %d", gb.Dx(), gb.Dy(), wb.Dx(), wb.Dy())
	}
	bad := 0
	for y := range wb.Dy() {
		for x := range wb.Dx() {
			g := color.NRGBAModel.Convert(got.At(gb.Min.X+x, gb.Min.Y+y)).(color.NRGBA)
			w := color.NRGBAModel.Convert(want.At(wb.Min.X+x, wb.Min.Y+y)).(color.NRGBA)
			if levelDiff(g.R, w.R) > tol || levelDiff(g.G, w.G) > tol || levelDiff(g.B, w.B) > tol {
				if bad == 0 {
					t.Errorf("pixel %d,%d drawn %v, want %v", x, y, g, w)
				}
				bad++
			}
		}
	}
	if bad > 0 {
		t.Errorf("%d of %d pixels differ", bad, wb.Dx()*wb.Dy())
	}
}

func levelDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestFromImage_DrawsThePicturesPixels converts a picture of each lossless
// format built here, renders its page at the picture's own resolution and
// compares every pixel with the decoded picture over white: equal, since
// the samples are the file's.
func TestFromImage_DrawsThePicturesPixels(t *testing.T) {
	t.Parallel()
	pic := testPicture(37, 23)
	translucent := testPicture(29, 17)
	for i := 3; i < len(translucent.Pix); i += 4 {
		translucent.Pix[i] = uint8(i * 5 % 256)
	}
	gray := image.NewGray(image.Rect(0, 0, 31, 19))
	for i := range gray.Pix {
		gray.Pix[i] = uint8(i * 11 % 256)
	}
	bilevel := image.NewGray(image.Rect(0, 0, 33, 9))
	for i := range bilevel.Pix {
		if i%3 == 0 || i%7 == 1 {
			bilevel.Pix[i] = 0xFF
		}
	}
	paletted := image.NewPaletted(image.Rect(0, 0, 21, 15), color.Palette{color.NRGBA{255, 0, 0, 255}, color.NRGBA{0, 0, 255, 255}, color.NRGBA{0, 128, 0, 255}, color.NRGBA{10, 20, 30, 0}, color.NRGBA{200, 200, 0, 128}})
	for i := range paletted.Pix {
		paletted.Pix[i] = uint8(i % 5)
	}
	// GIF keeps one fully transparent entry, BMP none.
	gifPaletted := image.NewPaletted(paletted.Rect, color.Palette{color.NRGBA{255, 0, 0, 255}, color.NRGBA{0, 0, 255, 255}, color.NRGBA{0, 128, 0, 255}, color.NRGBA{}, color.NRGBA{200, 200, 0, 255}})
	copy(gifPaletted.Pix, paletted.Pix)
	opaquePaletted := image.NewPaletted(paletted.Rect, gifPaletted.Palette[:3])
	for i := range opaquePaletted.Pix {
		opaquePaletted.Pix[i] = uint8(i % 3)
	}
	deep := image.NewNRGBA64(image.Rect(0, 0, 17, 11))
	for y := range 11 {
		for x := range 17 {
			deep.SetNRGBA64(x, y, color.NRGBA64{R: uint16(x * 3851), G: uint16(y * 5957), B: uint16(x*y*397 + 1), A: 0xFFFF})
		}
	}
	tiffOf := func(img image.Image, c xtiff.CompressionType) []byte {
		var b bytes.Buffer
		if err := xtiff.Encode(&b, img, &xtiff.Options{Compression: c}); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	bmpOf := func(img image.Image) []byte {
		var b bytes.Buffer
		if err := xbmp.Encode(&b, img); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	gifOf := func(img *image.Paletted) []byte {
		var b bytes.Buffer
		if err := gif.Encode(&b, img, nil); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	for _, tc := range []struct {
		name string
		data []byte
		want image.Image
		// dpi is the resolution the file states, 96 when it states none.
		dpi float64
	}{
		{"png rgb", encodePNG(t, pic), pic, 96},
		{"png alpha", encodePNG(t, translucent), translucent, 96},
		{"png gray", encodePNG(t, gray), gray, 96},
		{"png one bit", encodePNG(t, bilevel), bilevel, 96},
		{"png indexed", encodePNG(t, paletted), paletted, 96},
		{"png 16 bit", encodePNG(t, deep), deep, 96},
		{"gif", gifOf(gifPaletted), gifPaletted, 96},
		{"bmp rgb", bmpOf(pic), pic, 96},
		{"bmp indexed", bmpOf(opaquePaletted), opaquePaletted, 96},
		// golang.org/x/image/tiff states 72 pixels an inch.
		{"tiff deflate", tiffOf(pic, xtiff.Deflate), pic, 72},
		{"tiff gray", tiffOf(gray, xtiff.Uncompressed), gray, 72},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, _, _ := convertImageFile(t, tc.data)
			samePixels(t, renderedPage(t, r, 1, tc.dpi), overWhite(tc.want), 1)
		})
	}
}

// TestFromImage_WritesTheJPEGAsItIs checks that a JPEG's bytes are the
// image stream's, and that its page draws as the JPEG decodes.
func TestFromImage_WritesTheJPEGAsItIs(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, testPicture(40, 24), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	r, data, _ := convertImageFile(t, b.Bytes())
	if !bytes.Contains(data, b.Bytes()) {
		t.Error("the PDF does not hold the JPEG's bytes")
	}
	want, err := jpeg.Decode(bytes.NewReader(b.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	samePixels(t, renderedPage(t, r, 1, 96), want, 3)
}

// TestFromImage_RefusesWhatItDoesNotRead checks that a source that is no
// image of the nine formats fails as a malformed document does, naming the
// format it can tell.
func TestFromImage_RefusesWhatItDoesNotRead(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, data, says string
	}{
		{"text", "hello", "not an image"},
		{"heif", "\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic", "HEIF"},
		{"avif", "\x00\x00\x00\x18ftypavif\x00\x00\x00\x00mif1avif", "AVIF"},
		{"jpeg xl", "\xff\x0a\x00\x00", "JPEG XL"},
	} {
		_, err := FromImage(strings.NewReader(tc.data)).WriteTo(context.Background(), &bytes.Buffer{})
		if !errors.Is(err, ErrConvertMalformed) || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("%s: %v, want ErrConvertMalformed saying %q", tc.name, err, tc.says)
		}
	}
}
