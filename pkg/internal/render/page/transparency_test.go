package page

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

func init() {
	maps.Copy(popplerFixtures, transparencyFixtures)
}

// blendModeNames are the sixteen standard blend modes (ISO 32000-1, 11.3.5,
// Tables 136 and 137).
var blendModeNames = []string{
	"Normal", "Multiply", "Screen", "Overlay", "Darken", "Lighten", "ColorDodge", "ColorBurn",
	"HardLight", "SoftLight", "Difference", "Exclusion", "Hue", "Saturation", "Color", "Luminosity",
}

// blendModesFixture draws every blend mode in a 40-unit cell of a 4 × 4
// grid: a source square over a backdrop of two colours, its top part over
// the bare page, which the page group's transparent backdrop leaves the
// source's own colour (11.4.7).
func blendModesFixture() fixture {
	var states, content strings.Builder
	for i, name := range blendModeNames {
		fmt.Fprintf(&states, "/B%d << /BM /%s >> ", i, name)
		x, y := float64(i%4)*40, float64(3-i/4)*40
		fmt.Fprintf(&content, "0.9 0.5 0.1 rg %g %g 20 26 re f 0.2 0.4 0.8 rg %g %g 20 26 re f ", x, y, x+20, y)
		fmt.Fprintf(&content, "q /B%d gs 0.3 0.8 0.5 rg %g %g 30 32 re f Q ", i, x+5, y+5)
	}
	return fixture{
		page:    "/MediaBox [0 0 160 160] /Resources << /ExtGState << " + states.String() + ">> >>",
		content: content.String(),
	}
}

// groupForm is a form XObject whose content paints two overlapping squares
// at ca 0.6 with the Multiply blend mode, as a transparency group of the
// attributes given.
func groupForm(attrs string) string {
	return stream("/Type /XObject /Subtype /Form /BBox [0 0 50 50] /Group << /S /Transparency "+attrs+" >> "+
		"/Resources << /ExtGState << /G << /ca 0.6 /BM /Multiply >> >> >>",
		"/G gs 1 0 0 rg 5 5 30 30 re f 0 0 1 rg 15 15 30 30 re f")
}

var transparencyFixtures = map[string]fixture{
	"blend_modes": blendModesFixture(),
	// Four groups over a yellow band, composited at ca 0.8: non-isolated,
	// isolated, non-isolated knockout and isolated knockout (11.4.5,
	// 11.4.6).
	"groups": {
		page: "/MediaBox [0 0 200 60] /Resources << /XObject << /N 5 0 R /I 6 0 R /K 7 0 R /IK 8 0 R >> " +
			"/ExtGState << /A << /ca 0.8 >> >> >>",
		content: "1 1 0 rg 0 0 200 25 re f /A gs q 1 0 0 1 0 5 cm /N Do Q q 1 0 0 1 50 5 cm /I Do Q " +
			"q 1 0 0 1 100 5 cm /K Do Q q 1 0 0 1 150 5 cm /IK Do Q",
		objs: []string{groupForm(""), groupForm("/I true"), groupForm("/K true"), groupForm("/I true /K true")},
	},
	// A luminosity soft mask whose group is a gray ramp over part of the
	// page, its /BC gray 0.5 outside it; then the same with a /TR that
	// inverts it (11.5.3, 11.6.5.2).
	"smask_luminosity": {
		page: "/Resources << /ExtGState << /L << /SMask << /S /Luminosity /G 5 0 R /BC [0.5] >> >> " +
			"/T << /SMask << /S /Luminosity /G 5 0 R /BC [0.5] /TR 7 0 R >> >> >> >>",
		content: "0 0 1 rg 0 0 100 100 re f q /L gs 1 0 0 rg 0 50 100 50 re f Q q /T gs 0 1 0 rg 0 0 100 50 re f Q",
		objs: []string{
			stream("/Type /XObject /Subtype /Form /BBox [20 0 100 100] /Group << /S /Transparency /CS /DeviceGray >> "+
				"/Resources << /Shading << /Sh 6 0 R >> >>", "/Sh sh"),
			"<< /ShadingType 2 /ColorSpace /DeviceGray /Coords [20 0 100 0] " +
				"/Function << /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >> >>",
			"<< /FunctionType 2 /Domain [0 1] /C0 [1] /C1 [0] /N 1 >>",
		},
	},
	// An alpha soft mask whose group holds an opaque square and a square at
	// ca 0.4 (11.5.2): blue shows through both, the rest of the page masked.
	"smask_alpha": {
		page:    "/Resources << /ExtGState << /M << /SMask << /S /Alpha /G 5 0 R >> >> >> >>",
		content: "1 1 0 rg 0 0 100 100 re f /M gs 0 0 1 rg 0 0 100 100 re f",
		objs: []string{
			stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Group << /S /Transparency >> "+
				"/Resources << /ExtGState << /H << /ca 0.4 >> >> >>",
				"0 g 10 10 40 80 re f /H gs 50 10 40 80 re f"),
		},
	},
	// An isolated group holding a non-isolated knockout group of Difference
	// squares, the outer group composited with Screen (11.4).
	"nested_groups": {
		page:    "/Resources << /XObject << /O 5 0 R >> /ExtGState << /S << /BM /Screen /ca 0.9 >> >> >>",
		content: "0.2 0.3 0.6 rg 0 0 100 100 re f 0.9 0.8 0.1 rg 0 0 50 100 re f /S gs /O Do",
		objs: []string{
			stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Group << /S /Transparency /I true >> "+
				"/Resources << /XObject << /K 6 0 R >> >>", "0.5 g 10 10 80 30 re f /K Do"),
			stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Group << /S /Transparency /K true >> "+
				"/Resources << /ExtGState << /D << /BM /Difference /ca 0.7 >> >> >>",
				"/D gs 1 0 0 rg 20 20 40 60 re f 0 1 1 rg 40 30 40 60 re f"),
		},
	},
	// An image, an image mask and a shading under a luminosity soft mask of
	// the graphics state (11.6.5.2): each fades where the mask is dark.
	"smask_image": {
		page: "/Resources << /XObject << /Im 5 0 R /Mk 6 0 R >> /Shading << /Sh 8 0 R >> " +
			"/ExtGState << /M << /SMask << /S /Luminosity /G 7 0 R >> >> >> >>",
		content: "0 1 0 rg 0 0 100 100 re f /M gs q 50 0 0 100 0 0 cm /Im Do Q 0 0 1 rg q 50 0 0 50 50 50 cm /Mk Do Q " +
			"q 50 0 50 50 re W n /Sh sh Q",
		objs: []string{
			stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8", "\xff\x00\x00"),
			stream("/Type /XObject /Subtype /Image /Width 8 /Height 8 /ImageMask true", "\x0f\x0f\x0f\x0f\xf0\xf0\xf0\xf0"),
			stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Group << /S /Transparency /CS /DeviceGray >>",
				"1 g 0 0 100 30 re f 0.5 g 0 30 100 40 re f"),
			"<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [50 0 100 0] " +
				"/Function << /FunctionType 2 /Domain [0 1] /C0 [1 0 0] /C1 [0 0 1] /N 1 >> >>",
		},
	},
	// Constant alphas in a knockout group: the later square at ca 0.5
	// replaces the earlier one where they overlap, rather than compositing
	// over it (11.4.6).
	"knockout_alpha": {
		page:    "/Resources << /XObject << /A 5 0 R >> >>",
		content: "q 1 0 0 1 0 25 cm /A Do Q",
		objs:    []string{knockoutForm("")},
	},
}

// knockoutForm is a knockout group holding a red square, a blue square at
// ca 0.5 over it and a black line at CA 0.5, the alpha constants set with
// the extra entries given.
func knockoutForm(extra string) string {
	return stream("/Type /XObject /Subtype /Form /BBox [0 0 100 50] /Group << /S /Transparency /K true >> "+
		"/Resources << /ExtGState << /H << /ca 0.5 /CA 0.5 "+extra+" >> >> >>",
		"1 0 0 rg 10 10 50 30 re f /H gs 0 0 1 rg 40 5 50 30 re f 0 G 4 w 5 25 m 95 25 l S")
}

// TestSoftMaskedImageUnderSoftMask checks that an image's /SMask and the
// graphics state's soft mask multiply (ISO 32000-1, 11.6.5.3). poppler
// 26.09.0 draws such an image with its own mask alone, so the expected
// values are the specification's.
func TestSoftMaskedImageUnderSoftMask(t *testing.T) {
	t.Parallel()
	img, _ := draw(t, fixture{
		page:    "/Resources << /XObject << /Im 5 0 R >> /ExtGState << /M << /SMask << /S /Luminosity /G 7 0 R >> >> >> >>",
		content: "0 1 0 rg 0 0 100 100 re f /M gs q 100 0 0 100 0 0 cm /Im Do Q",
		objs: []string{
			stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8 /SMask 6 0 R",
				"\xff\x00\x00"),
			stream("/Type /XObject /Subtype /Image /Width 2 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", "\x40\xff"),
			stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Group << /S /Transparency /CS /DeviceGray >>",
				"1 g 0 0 100 50 re f 0.5 g 0 50 100 50 re f"),
		},
	})
	// Red at alpha a over green: the image's mask is 0.25 on the left and 1
	// on the right, the graphics state's 0.5 on the top and 1 below.
	over := func(a float64) rgb {
		return rgb{uint8(255*a + 0.5), uint8(255*(1-a) + 0.5), 0}
	}
	expect(t, img, 3, map[[2]int]rgb{
		{25, 25}: over(0.25 * 0.5), {75, 25}: over(0.5), {25, 75}: over(0.25), {75, 75}: over(1),
	})
}

// TestAlphaIsShape checks AIS in a knockout group (ISO 32000-1, 11.6.4.3):
// the constant alpha becomes shape, so the later square covers half of each
// pixel and leaves half of the earlier one, where without AIS it replaces
// it. poppler 26.09.0 ignores AIS, so the expected values are the
// specification's.
func TestAlphaIsShape(t *testing.T) {
	t.Parallel()
	img, _ := draw(t, fixture{
		page:    "/Resources << /XObject << /A 5 0 R /S 6 0 R >> >>",
		content: "q 1 0 0 1 0 50 cm /A Do Q /S Do",
		objs:    []string{knockoutForm(""), knockoutForm("/AIS true")},
	})
	expect(t, img, 2, map[[2]int]rgb{
		{50, 35}: {128, 128, 255}, // blue at 0.5 over the knocked-out red: over the page
		{50, 85}: {128, 0, 128},   // half red, half blue
		{20, 85}: red,
	})
}

// TestTextKnockout checks the text knockout parameter (ISO 32000-1, 9.3.8):
// with TK true, the default, the glyphs of a text object are one element,
// so two overlapping glyphs at ca 0.5 show one alpha where they overlap;
// with TK false each glyph composites over the one before. poppler 26.09.0
// does not implement text knockout, so the expected values are the
// specification's.
func TestTextKnockout(t *testing.T) {
	t.Parallel()
	got, _ := draw(t, fixture{
		page:    "/Resources << /Font << /F 5 0 R >> /ExtGState << /A << /ca 0.5 >> /K << /TK false >> >> >>",
		content: "/A gs 1 0 0 rg BT /F 40 Tf 5 60 Td [(A) 300 (A)] TJ ET /K gs BT /F 40 Tf 5 10 Td [(A) 300 (A)] TJ ET",
		objs:    trueTypeObjs(),
	})
	// The glyphs are 24 by 20 pixels; the second starts 12 pixels left of
	// where the first ends, so they overlap over x 17 to 29.
	half := rgb{255, 128, 128}
	expect(t, got, 2, map[[2]int]rgb{
		{10, 30}: half, {23, 30}: half, {35, 30}: half,
		{10, 80}: half, {23, 80}: {255, 64, 64}, {35, 80}: half,
	})
}

// TestBlendEdition checks that ColorDodge and ColorBurn follow the edition
// of ISO 32000 the document's version names: on a black backdrop under a
// white source ColorDodge is white in ISO 32000-1 and black in ISO 32000-2,
// and on a white backdrop under a black source ColorBurn is black and white.
func TestBlendEdition(t *testing.T) {
	t.Parallel()
	f := fixture{
		page: "/Resources << /ExtGState << /D << /BM /ColorDodge >> /B << /BM /ColorBurn >> >> >>",
		content: "0 g 0 0 50 100 re f 1 g 50 0 50 100 re f " +
			"q /D gs 1 g 0 50 100 50 re f Q q /B gs 0 g 0 0 100 50 re f Q",
	}
	v1, _ := draw(t, f)
	expect(t, v1, 1, map[[2]int]rgb{{25, 25}: paper, {75, 75}: black})
	f.catalog = "/Version /2.0"
	v2, _ := draw(t, f)
	expect(t, v2, 1, map[[2]int]rgb{{25, 25}: black, {75, 75}: paper})
}

// TestGroupIsolation checks that a group composited with Normal, opaque
// and unmasked still blends its content within itself when it is isolated
// (ISO 32000-1, 11.4.5), the Multiply set in its resources or in those of a
// form it paints, and blends it with the page when it is not.
func TestGroupIsolation(t *testing.T) {
	t.Parallel()
	const multiply = "/Resources << /ExtGState << /M << /BM /Multiply >> >> >>"
	img, report := draw(t, fixture{
		page:    "/Resources << /XObject << /I 5 0 R /N 6 0 R /J 7 0 R >> >>",
		content: "0 0 1 rg 0 0 100 100 re f /I Do /N Do /J Do",
		objs: []string{
			stream("/Type /XObject /Subtype /Form /BBox [0 0 50 50] /Group << /S /Transparency /I true >> "+multiply,
				"/M gs 1 0 0 rg 0 0 50 50 re f"),
			stream("/Type /XObject /Subtype /Form /BBox [50 0 100 50] /Group << /S /Transparency >> "+multiply,
				"/M gs 1 0 0 rg 50 0 50 50 re f"),
			stream("/Type /XObject /Subtype /Form /BBox [0 50 50 100] /Group << /S /Transparency /I true >> "+
				"/Resources << /XObject << /F 8 0 R >> >>", "/F Do"),
			stream("/Type /XObject /Subtype /Form /BBox [0 50 50 100] "+multiply, "/M gs 1 0 0 rg 0 50 50 50 re f"),
		},
	})
	expect(t, img, 1, map[[2]int]rgb{{25, 75}: red, {75, 75}: black, {25, 25}: red, {75, 25}: blue})
	if len(report.Unsupported) != 0 {
		t.Errorf("report %v, want nothing left out", report.Unsupported)
	}
}

// TestTransparencyOverBudget checks that a group whose rasters would pass
// the render pixel budget paints its content into the page and is counted.
func TestTransparencyOverBudget(t *testing.T) {
	t.Parallel()
	f := fixture{
		page:    "/Resources << /XObject << /I 5 0 R >> >>",
		content: "0 0 1 rg 0 0 100 100 re f /I Do",
		objs: []string{stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Group << /S /Transparency /I true >> "+
			"/Resources << /ExtGState << /M << /BM /Multiply >> >> >>", "/M gs 1 0 0 rg 0 0 100 100 re f")},
	}
	doc, pg := open(t, f.bytes(), limits.Limits{MaxRenderPixels: 12_000})
	c, report, err := Render(context.Background(), doc, pg, Options{DPI: 72, Background: white})
	if err != nil {
		t.Fatal(err)
	}
	if report.Unsupported[TransparencyOverBudget] != 1 {
		t.Errorf("report %v, want one group over budget", report.Unsupported)
	}
	// Painted into the page, the Multiply blends with the blue below.
	expect(t, c.Image(), 1, map[[2]int]rgb{{50, 50}: black})
}
