package recolor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// srgbEngine converts to sRGB as the renderer reads colour: through each
// source profile's transform to sRGB ([icc.Profile.NewTransform]), so a page
// recoloured to DeviceRGB through it draws as its source drew whatever its
// spaces. It only converts to an RGB endpoint.
type srgbEngine struct{}

func (srgbEngine) Transform(src, dst Endpoint, opts Options) (Converter, error) {
	if src.same(dst) {
		return identity{}, nil
	}
	if dst.Space != SpaceRGB {
		return nil, fmt.Errorf("srgbEngine converts to RGB, not %q", dst.Space)
	}
	if src.Profile == nil {
		return naiveConverter{src: src.Space, dst: SpaceRGB}, nil
	}
	p, err := icc.Parse(src.Profile)
	if err != nil {
		return nil, err
	}
	tr, err := p.NewTransform(opts.Intent)
	if err != nil {
		return nil, err
	}
	return iccConverter{tr: tr}, nil
}

type iccConverter struct {
	tr *icc.Transform
}

func (c iccConverter) Convert(in, out []float64) {
	b := make([]byte, c.tr.Components())
	for i := range b {
		b[i] = byte(clamp01(in[i])*255 + 0.5)
	}
	r, g, bl := c.tr.Convert(b)
	out[0], out[1], out[2] = float64(r)/255, float64(g)/255, float64(bl)/255
}

// TestRenderMatchesFromCMYK converts CMYK and ICC-based colour to DeviceRGB
// through the renderer's own reading of each space, so the page must draw
// as it drew, within renderTolerance.
func TestRenderMatchesFromCMYK(t *testing.T) {
	tint := "<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0.1 0.8 0.3 0.05] /N 1 >>"
	tests := []struct {
		name string
		f    fixture
	}{
		{"device", onePage("", "0.1 0.6 0.9 0.05 k 0 0 20 40 re f 0.8 0.1 0.1 0.3 k 20 0 20 40 re f")},
		{"spot", onePage("/Resources << /ColorSpace << /S [/Separation /Spot /DeviceCMYK 5 0 R] >> >>",
			"/S cs 0.6 sc 0 0 40 40 re f", tint)},
		{"indexed", onePage("/Resources << /ColorSpace << /I [/Indexed /DeviceCMYK 1 <10E06000 80101040>] >> >>",
			"/I cs 0 sc 0 0 20 40 re f 1 sc 20 0 20 40 re f")},
		{"icc", onePage("/Resources << /ColorSpace << /C [/ICCBased 5 0 R] >> >>",
			"/C cs 0.2 0.7 0.1 0 sc 0 0 40 40 re f", stream("/N 4", string(builtinCMYK)))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, pages := open(t, tc.f.bytes())
			before := render(t, doc, pages[0])
			r := recolor(t, tc.f, rgbTarget, Options{Engine: srgbEngine{}})
			after := render(t, r.doc, r.pages[0])
			d := maxChannelDiff(t, before, after)
			t.Logf("largest channel difference: %d", d)
			if d > renderTolerance {
				t.Errorf("renderings differ by %d levels\n%s", d, strings.TrimSpace(r.text(0)))
			}
		})
	}
}

// renderTolerance is the largest difference of one 8-bit channel a
// recoloured page may draw from its source: 1 level. The naive engine's grey
// to RGB is the renderer's own reading of DeviceGray, so device colour, spot
// alternates, palettes, shadings and meshes in grey draw the same but for the
// rounding of a component to 1/10000 and of a sampled function to 16 bits,
// and every such case draws 0 levels apart. Lab and CalGray go through the
// CIE formulas, which the renderer evaluates on its own path, and draw 1
// level apart.
const renderTolerance = 1

// TestRenderMatches renders crafted pages before and after their
// conversion to DeviceRGB and compares them within renderTolerance. The
// colorimetric agreement of a conversion is the ICC engine's to test; this
// checks that every construct is rewritten into what draws the same colour.
func TestRenderMatches(t *testing.T) {
	gray := "<< /FunctionType 2 /Domain [0 1] /C0 [1] /C1 [0.2] /N 1 >>"
	tests := []struct {
		name string
		f    fixture
	}{
		{"device", onePage("", "0.3 g 0 0 20 20 re f 0.7 G 4 w 20 20 m 40 40 l S")},
		{"spot", onePage("/Resources << /ColorSpace << /S [/Separation /Spot /DeviceGray 5 0 R] >> >>",
			"/S cs 0.6 sc 0 0 40 40 re f", gray)},
		{"indexed", onePage("/Resources << /ColorSpace << /I [/Indexed /DeviceGray 2 <20A0F0>] >> >>",
			"/I cs 2 sc 0 0 20 40 re f 1 sc 20 0 20 40 re f")},
		{"axial", onePage("/Resources << /Shading << /Sh 5 0 R >> >>", "/Sh sh",
			"<< /ShadingType 2 /ColorSpace /DeviceGray /Coords [0 0 40 0] /Function << /FunctionType 3 /Domain [0 1] /Bounds [0.5] /Encode [0 1 0 1] /Functions [6 0 R 6 0 R] >> >>",
			gray)},
		{"mesh", onePage("/Resources << /Shading << /Sh 5 0 R >> >>", "/Sh sh",
			stream("/ShadingType 4 /ColorSpace /DeviceGray /BitsPerCoordinate 8 /BitsPerComponent 8 /BitsPerFlag 8 /Decode [0 40 0 40 0 1]",
				string([]byte{0, 0, 0, 0x10, 0, 0x28, 0, 0x90, 0, 0, 0x28, 0xf0})))},
		{"pattern", onePage("/Resources << /ColorSpace << /P [/Pattern /DeviceGray] >> /Pattern << /T 5 0 R >> >>",
			"/P cs 0.4 /T scn 0 0 40 40 re f",
			stream("/PatternType 1 /PaintType 2 /TilingType 1 /BBox [0 0 8 8] /XStep 8 /YStep 8 /Resources << >>", "0 0 4 4 re f"))},
		{"form", onePage("/Resources << /XObject << /F 5 0 R >> >>", "/F Do",
			stream("/Subtype /Form /BBox [0 0 40 40] /Resources << >>", "0.25 g 5 5 30 30 re f"))},
		{"cie", onePage("/Resources << /ColorSpace << /L [/Lab << /WhitePoint [0.9505 1 1.089] >>] /G [/CalGray << /WhitePoint [0.9505 1 1.089] /Gamma 2.2 >>] >> >>",
			"/L cs 60 0 0 sc 0 0 20 40 re f /G cs 0.5 sc 20 0 20 40 re f")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, pages := open(t, tc.f.bytes())
			before := render(t, doc, pages[0])
			r := recolor(t, tc.f, rgbTarget, naive(Options{}))
			if r.report.Operators+r.report.Spaces+r.report.Shadings == 0 {
				t.Fatalf("nothing converted: %+v", r.report)
			}
			after := render(t, r.doc, r.pages[0])
			d := maxChannelDiff(t, before, after)
			t.Logf("largest channel difference: %d", d)
			if d > renderTolerance {
				t.Errorf("renderings differ by %d levels\n%s", d, strings.TrimSpace(r.text(0)))
			}
		})
	}
}
