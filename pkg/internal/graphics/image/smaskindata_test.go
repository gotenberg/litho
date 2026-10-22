package image

import (
	stdimage "image"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// readSMaskInDataJPX reads a 96 x 64 JPEG 2000 image from pkg/pdf's test
// data: rgba8.jp2 holds straight colour and an opacity channel,
// rgba8_premul.jp2 the same colour premultiplied by a premultiplied-opacity
// channel (T.800, Table I.16); see [dataMaskPixel].
func readSMaskInDataJPX(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../../../pdf/testdata/jpx/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// dataMaskPixel is the straight colour and the opacity of pixel (x, y) of the
// readSMaskInDataJPX images: gradients, and an opacity of 0 left of column
// 24, 255 from column 72, and a ramp between.
func dataMaskPixel(x, y int) (r, g, b, a int) {
	r, g, b = x*255/95, y*255/63, (x+y)*37%256
	switch {
	case x < 24:
		a = 0
	case x >= 72:
		a = 255
	default:
		a = (x - 24) * 255 / 47
	}
	return r, g, b, a
}

// TestDecodeImage_SMaskInData pins that a JPXDecode image carrying its soft
// mask in its data (ISO 32000-1, Table 89 /SMaskInData 1 or 2) decodes to
// straight colour with that opacity as alpha, premultiplied colour undone
// (T.800, Table I.16), whether the image names its colour space or takes it
// from the data (7.4.9).
func TestDecodeImage_SMaskInData(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, file, entries string
	}{
		{"straight, own colour space", "rgba8.jp2", "/ColorSpace /DeviceRGB /BitsPerComponent 8 /SMaskInData 1"},
		{"straight, colour of the data", "rgba8.jp2", "/SMaskInData 1"},
		{"premultiplied, own colour space", "rgba8_premul.jp2", "/ColorSpace /DeviceRGB /BitsPerComponent 8 /SMaskInData 2"},
		{"premultiplied, colour of the data", "rgba8_premul.jp2", "/SMaskInData 2"},
		{"premultiplied channel under value 1", "rgba8_premul.jp2", "/SMaskInData 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, doc, page := imagePDF(t, jpxPDF(96, 64, tc.entries, readSMaskInDataJPX(t, tc.file)))
			img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
			if err != nil {
				t.Fatal(err)
			}
			n, ok := img.(*stdimage.NRGBA)
			if !ok {
				t.Fatalf("decoded a %T, want *image.NRGBA", img)
			}
			for y := range 64 {
				for x := range 96 {
					r, g, b, a := dataMaskPixel(x, y)
					c := n.NRGBAAt(x, y)
					if int(c.A) != a {
						t.Fatalf("opacity at (%d, %d) = %d, want %d", x, y, c.A, a)
					}
					if a == 0 {
						continue
					}
					// Undoing an 8-bit premultiplication loses up to 255/a
					// levels; the straight colour is exact.
					tol := 1 + 255/a
					for i, want := range [3]int{r, g, b} {
						got := int([3]uint8{c.R, c.G, c.B}[i])
						if d := got - want; d < -tol || d > tol {
							t.Fatalf("channel %d at (%d, %d) = %d, want %d within %d", i, x, y, got, want, tol)
						}
					}
				}
			}
		})
	}
}

// TestDecodeImage_SMaskInDataLeftOut pins when the data's opacity is not
// applied: /SMaskInData 0 ignores it (Table 89), an explicit /SMask takes
// precedence, and a value Table 89 does not define leaves the image opaque
// rather than failing its decode.
func TestDecodeImage_SMaskInDataLeftOut(t *testing.T) {
	t.Parallel()
	smask := "<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n\xff\nendstream"
	cases := []struct {
		name, entries string
	}{
		{"value 0", "/SMaskInData 0"},
		{"explicit /SMask", "/SMaskInData 1 /SMask 5 0 R"},
		{"undefined value", "/SMaskInData 3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, doc, page := imagePDF(t, jpxPDF(96, 64, tc.entries, readSMaskInDataJPX(t, "rgba8.jp2"), smask))
			img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := img.(*stdimage.NRGBA); ok {
				t.Fatal("decoded with the data's opacity, want it left out")
			}
			if _, _, _, a := img.At(0, 0).RGBA(); a != 0xffff {
				t.Errorf("pixel (0, 0) alpha = %d, want opaque", a)
			}
		})
	}
}

func TestUnpremultiply(t *testing.T) {
	t.Parallel()
	samples := []byte{50, 100, 7, 9, 200, 255}
	Unpremultiply(samples, []byte{100, 0, 255}, 3, 2)
	want := []byte{128, 255, 0, 0, 200, 255}
	if string(samples) != string(want) {
		t.Errorf("got %v, want %v", samples, want)
	}
	// Fewer samples than pixels stop at the last whole pixel.
	short := []byte{50, 100, 7}
	Unpremultiply(short, []byte{100, 100}, 2, 2)
	if short[2] != 7 {
		t.Errorf("a partial pixel was rescaled: %v", short)
	}
}
