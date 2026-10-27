package recolor

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// TestConvertImages_RenderAsBefore converts images in each colour space an
// image may have to DeviceRGB through the decoder's own sRGB reading, and
// renders the page with litho before and after. The renderings agree within
// renderTolerance on every channel of every pixel: the conversion reads the
// samples, their /Decode, masks and mattes as the renderer does.
//
// renderTolerance is 2 levels of 255: the renderer reads a sample through its
// 8-bit tables where the conversion computes it, and a 16-bit sample is
// rounded to 8 bits on both paths by different arithmetic. A spot kept with
// its tint transform rewritten as a sampled function of 17 points is held to
// spotTolerance, the error of interpolating a smooth transform between them.
//
// The matted case is in DeviceGray. The conversion undoes a /Matte in the
// image's own space, where ISO 32000-1, 11.6.5.3 preblends the samples, and
// the renderer undoes it after reading the samples as sRGB; the two agree
// where that reading is linear, as DeviceGray's is, and drift apart through a
// CMYK profile, where the conversion is the one the clause describes.
func TestConvertImages_RenderAsBefore(t *testing.T) {
	const (
		renderTolerance = 2
		spotTolerance   = 4
		side            = 8
	)
	gradient := func(comps, bytesPer int) []byte {
		out := make([]byte, 0, side*side*comps*bytesPer)
		for i := range side * side {
			for c := range comps {
				v := byte((i*37 + c*71) % 256)
				out = append(out, v)
				if bytesPer == 2 {
					out = append(out, byte(i*c))
				}
			}
		}
		return out
	}
	tint := ast.PdfDict{
		"FunctionType": ast.PdfInt(2), "Domain": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(1)},
		"C0": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0)},
		"C1": ast.PdfArray{ast.PdfReal(0.1), ast.PdfReal(0.9), ast.PdfReal(0.2), ast.PdfReal(0.05)}, "N": ast.PdfInt(1),
	}
	separation := ast.PdfArray{familySeparation, ast.PdfName("Spot"), familyCMYK, tint}
	lab := ast.PdfArray{familyLab, ast.PdfDict{"WhitePoint": ast.PdfArray{ast.PdfReal(0.9505), ast.PdfInt(1), ast.PdfReal(1.089)}}}
	iccCMYK := ast.PdfArray{familyICCBased, ast.PdfIndirectRef{ObjectNum: 9}}
	labTable := make([]byte, 0, 4*3)
	for _, e := range [][3]byte{{255, 128, 128}, {128, 200, 60}, {40, 90, 210}, {0, 128, 128}} {
		labTable = append(labTable, e[0], e[1], e[2])
	}

	cases := []struct {
		name      string
		img       ast.PdfStream
		spots     bool
		tolerance int
	}{
		{"DeviceCMYK", imageStream(side, side, familyCMYK, 8, gradient(4, 1), nil), false, renderTolerance},
		{"DeviceCMYK 16-bit", imageStream(side, side, familyCMYK, 16, gradient(4, 2), nil), false, renderTolerance},
		{"DeviceGray 1-bit inverted", imageStream(side, side, familyGray, 1, gradient(1, 1)[:side], ast.PdfDict{"Decode": ast.PdfArray{ast.PdfInt(1), ast.PdfInt(0)}}), false, renderTolerance},
		{"Lab", imageStream(side, side, lab, 8, gradient(3, 1), nil), false, renderTolerance},
		{"ICCBased CMYK", imageStream(side, side, iccCMYK, 8, gradient(4, 1), nil), false, renderTolerance},
		{"Indexed over Lab", imageStream(side, side, ast.PdfArray{familyIndexed, lab, ast.PdfInt(3), ast.PdfString(labTable)}, 2, gradient(1, 1)[:side*2], nil), false, renderTolerance},
		{"Separation to process", imageStream(side, side, separation, 8, gradient(1, 1), nil), true, renderTolerance},
		{"Separation kept", imageStream(side, side, separation, 8, gradient(1, 1), nil), false, spotTolerance},
		{"DeviceGray under a matted soft mask", imageStream(side, side, familyGray, 8, gradient(1, 1), ast.PdfDict{"SMask": ast.PdfIndirectRef{ObjectNum: 8}}), false, renderTolerance},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestPage(side, side, tc.img)
			p.objs[8] = imageStream(side, side, familyGray, 8, gradient(1, 1), ast.PdfDict{
				"Matte": ast.PdfArray{ast.PdfReal(0.3)},
			})
			p.objs[9] = ast.NewStream(ast.PdfDict{"N": ast.PdfInt(4)}, icc.CMYKPrinterProfile())
			before := p.render(t)
			cfg := deviceTarget(familyRGB)
			cfg.ConvertSpots = tc.spots
			alloc, _, outcome, err := p.convert(t, cfg, newRenderedColors(3))
			if err != nil || outcome == ImageUnchanged {
				t.Fatalf("outcome %v, %v; want the image converted", outcome, err)
			}
			after := p.with(alloc).render(t)
			if d := maxSampleDiff(t, before, after); d > tc.tolerance {
				t.Errorf("renderings differ by %d levels, want at most %d", d, tc.tolerance)
			}
		})
	}
}
