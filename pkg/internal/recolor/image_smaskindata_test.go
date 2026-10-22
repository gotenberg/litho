package recolor

import (
	"errors"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// smaskInDataImage returns the 96 x 64 JPEG 2000 image testdata/jpx/name of
// pkg/pdf under extra entries. rgba8.jp2 holds straight colour and an
// opacity channel, rgba8_premul.jp2 the same colour premultiplied by a
// premultiplied-opacity channel (T.800, Table I.16); see [smaskInDataPixel].
func smaskInDataImage(t *testing.T, name string, extra ast.PdfDict) ast.PdfStream {
	t.Helper()
	data, err := os.ReadFile("../../pdf/testdata/jpx/" + name)
	if err != nil {
		t.Fatal(err)
	}
	d := ast.PdfDict{"Filter": ast.PdfName("JPXDecode")}
	for k, v := range extra {
		d[k] = v
	}
	img := imageStream(96, 64, nil, 8, data, d)
	if _, ok := extra["BitsPerComponent"]; !ok {
		delete(img.Dict, "BitsPerComponent")
	}
	return img
}

// smaskInDataPixel is the straight colour and the opacity of pixel (x, y) of
// the smaskInDataImage fixtures: gradients, and an opacity of 0 left of
// column 24, 255 from column 72, and a ramp between.
func smaskInDataPixel(x, y int) (r, g, b, a int) {
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

func TestConvertSMaskInData_ExtractsTheMask(t *testing.T) {
	cases := []struct {
		name  string
		file  string
		extra ast.PdfDict
	}{
		{"straight, own colour space", "rgba8.jp2", ast.PdfDict{"ColorSpace": familyRGB, "BitsPerComponent": ast.PdfInt(8), "SMaskInData": ast.PdfInt(1)}},
		{"straight, colour of the data", "rgba8.jp2", ast.PdfDict{"SMaskInData": ast.PdfInt(1)}},
		{"premultiplied, own colour space", "rgba8_premul.jp2", ast.PdfDict{"ColorSpace": familyRGB, "BitsPerComponent": ast.PdfInt(8), "SMaskInData": ast.PdfInt(2)}},
		{"premultiplied, colour of the data", "rgba8_premul.jp2", ast.PdfDict{"SMaskInData": ast.PdfInt(2)}},
		{"premultiplied channel under value 1", "rgba8_premul.jp2", ast.PdfDict{"SMaskInData": ast.PdfInt(1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestPage(96, 64, smaskInDataImage(t, tc.file, tc.extra))
			for _, quality := range []int{0, 85} {
				cfg := deviceTarget(familyGray)
				cfg.JPEGQuality = quality
				alloc, _, outcome, err := p.convert(t, cfg, fixedColors(&funcColors{f: mean}))
				if err != nil || outcome != ImageConverted {
					t.Fatalf("quality %d: outcome %v, %v; want converted", quality, outcome, err)
				}
				out := pending(t, alloc, 7)
				if _, ok := out.Dict["SMaskInData"]; ok {
					t.Errorf("quality %d: the converted image keeps /SMaskInData", quality)
				}
				ref, ok := out.Dict["SMask"].(ast.PdfIndirectRef)
				if !ok {
					t.Fatalf("quality %d: /SMask = %v, want a reference", quality, out.Dict["SMask"])
				}
				mask := pending(t, alloc, ref.ObjectNum)
				if cs := mask.Dict["ColorSpace"]; cs != familyGray {
					t.Errorf("quality %d: soft mask ColorSpace = %v, want DeviceGray", quality, cs)
				}
				doc, _ := p.with(alloc).open(t)
				alpha := inflate(t, doc, mask)
				if len(alpha) != 96*64 {
					t.Fatalf("quality %d: soft mask of %d samples, want %d", quality, len(alpha), 96*64)
				}
				for y := range 64 {
					for x := range 96 {
						if _, _, _, a := smaskInDataPixel(x, y); int(alpha[y*96+x]) != a {
							t.Fatalf("quality %d: opacity at (%d, %d) = %d, want %d", quality, x, y, alpha[y*96+x], a)
						}
					}
				}
				if quality != 0 {
					continue
				}
				grey := inflate(t, doc, out)
				for y := range 64 {
					for x := range 96 {
						r, g, b, a := smaskInDataPixel(x, y)
						if a == 0 {
							continue
						}
						want := (r + g + b) / 3
						// Undoing an 8-bit premultiplication loses up to 255/a
						// levels; the straight colour is exact.
						tol := 1 + 255/a
						if d := int(grey[y*96+x]) - want; d < -tol || d > tol {
							t.Fatalf("grey at (%d, %d) = %d, want %d within %d", x, y, grey[y*96+x], want, tol)
						}
					}
				}
			}
		})
	}
}

func TestConvertSMaskInData_Refusals(t *testing.T) {
	maskRef := ast.PdfIndirectRef{ObjectNum: 8}
	cases := []struct {
		name  string
		extra ast.PdfDict
	}{
		{"value Table 89 does not define", ast.PdfDict{"SMaskInData": ast.PdfInt(3)}},
		{"explicit /SMask beside it", ast.PdfDict{"SMaskInData": ast.PdfInt(1), "SMask": maskRef}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestPage(96, 64, smaskInDataImage(t, "rgba8.jp2", tc.extra))
			p.objs[8] = imageStream(1, 1, familyGray, 8, []byte{255}, nil)
			_, _, outcome, err := p.convert(t, deviceTarget(familyGray), fixedColors(&funcColors{f: mean}))
			if outcome != ImageUnchanged || !errors.Is(err, ErrImageNotConverted) {
				t.Errorf("outcome %v, %v; want ErrImageNotConverted", outcome, err)
			}
		})
	}
}
