package page

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/render/blend"
)

// cmykMultiplyForm is an isolated transparency group in the blending colour
// space cs whose content paints a cyan square, then a magenta one over its
// right half with the Multiply blend mode.
func cmykMultiplyForm(cs string) string {
	return stream("/Type /XObject /Subtype /Form /BBox [0 0 100 50] /Group << /S /Transparency /I true /CS "+cs+" >> "+
		"/Resources << /ExtGState << /M << /BM /Multiply >> >> >>",
		"1 0 0 0 k 0 0 60 50 re f /M gs 0 1 0 0 k 40 0 60 50 re f")
}

// TestMultiplyInCMYKGroup checks ISO 32000-1, 11.3.4 and 11.3.5: a group
// whose /CS is DeviceCMYK blends in CMYK, where Multiply over the complements
// lays magenta ink on cyan ink, so the overlap shows as the colour 1 1 0 0 k
// paints; the same group blending in RGB multiplies the two colours as they
// look, another colour.
func TestMultiplyInCMYKGroup(t *testing.T) {
	t.Parallel()
	img, _ := draw(t, fixture{
		page:    "/Resources << /XObject << /C 5 0 R /R 6 0 R >> >>",
		content: "q 1 0 0 1 0 50 cm /C Do Q /R Do",
		objs:    []string{cmykMultiplyForm("/DeviceCMYK"), cmykMultiplyForm("/DeviceRGB")},
	})
	ref, _ := draw(t, fixture{content: "1 1 0 0 k 0 50 100 50 re f 1 0 0 0 k 0 0 100 50 re f"})
	blue, cyan := at(t, ref, 50, 25), at(t, ref, 50, 75)
	expect(t, img, 2, map[[2]int]rgb{{50, 25}: blue, {20, 25}: cyan})
	if rgbOverlap := at(t, img, 50, 75); close8(rgbOverlap, blue, 16) {
		t.Errorf("Multiply blended in RGB = %v, the CMYK result %v: the spaces do not differ", rgbOverlap, blue)
	}
}

// TestKnockoutCMYKGroup checks a knockout group blending in CMYK (ISO
// 32000-1, 11.4.6): where its second square overlaps the first, the second
// composites with the group's initial backdrop alone, so the overlap shows
// the second square as it shows where it is alone.
func TestKnockoutCMYKGroup(t *testing.T) {
	t.Parallel()
	img, _ := draw(t, fixture{
		page:    "/Resources << /XObject << /K 5 0 R >> >>",
		content: "0 0 1 0 k 0 0 100 100 re f /K Do",
		objs: []string{stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Group << /S /Transparency /I true /K true /CS /DeviceCMYK >> "+
			"/Resources << /ExtGState << /M << /BM /Multiply /ca 0.7 >> >> >>",
			"/M gs 1 0 0 0 k 0 0 60 100 re f 0 1 0 0 k 40 0 60 100 re f")},
	})
	if a, b := at(t, img, 50, 50), at(t, img, 80, 50); !close8(a, b, 1) {
		t.Errorf("overlap %v, second square alone %v: the knockout let the first show through", a, b)
	}
}

// TestGreyLuminositySoftMask checks a luminosity soft mask whose group's /CS
// is DeviceGray (ISO 32000-1, 11.5.3): the group blends in grey, where red
// and green Multiply to 0.3 × 0.59 rather than to the black they make in
// RGB, and the mask is that grey.
func TestGreyLuminositySoftMask(t *testing.T) {
	t.Parallel()
	maskGroup := func(cs string) string {
		return stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Group << /S /Transparency /CS "+cs+" >> "+
			"/Resources << /ExtGState << /M << /BM /Multiply >> >> >>",
			"1 0 0 rg 0 0 100 100 re f /M gs 0 1 0 rg 0 0 100 100 re f")
	}
	img, _ := draw(t, fixture{
		page: "/Resources << /ExtGState << /G << /SMask << /S /Luminosity /G 5 0 R >> >> " +
			"/R << /SMask << /S /Luminosity /G 6 0 R >> >> >> >>",
		content: "q /G gs 0 g 0 0 50 100 re f Q q /R gs 0 g 50 0 50 100 re f Q",
		objs:    []string{maskGroup("/DeviceGray"), maskGroup("/DeviceRGB")},
	})
	// Black at alpha 0.177 over white, and at alpha 0.
	expect(t, img, 2, map[[2]int]rgb{{25, 50}: {210, 210, 210}, {75, 50}: paper})
}

// TestCMYKPageGroupKeepsInk checks that a page group blending in DeviceCMYK
// keeps a black-only colour as black ink: a half-transparent K-only fill over
// paper is K 0.5 and no colour ink, which a trip through RGB would turn into
// four inks.
func TestCMYKPageGroupKeepsInk(t *testing.T) {
	t.Parallel()
	f := fixture{
		page:    "/Group << /S /Transparency /CS /DeviceCMYK >> /Resources << /ExtGState << /A << /ca 0.5 >> >> >>",
		content: "/A gs 0 0 0 1 k 0 0 50 100 re f 0 0 1 rg 50 0 50 100 re f",
	}
	doc, pg := open(t, f.bytes(), limits.Limits{})
	res, _, err := RenderGroup(context.Background(), doc, pg, Options{DPI: 72, Background: white, GroupSpace: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Space == nil || res.Group.Space().Kind() != blend.CMYK {
		t.Fatalf("page group space %v, want DeviceCMYK", res.Space)
	}
	c, a := res.Group.Components(25, 50)
	if a != 1 || c[0] != 0 || c[1] != 0 || c[2] != 0 || c[3] < 0.49 || c[3] > 0.51 {
		t.Errorf("half black = %v at alpha %g, want K 0.5 alone", c, a)
	}
	if c, _ := res.Group.Components(75, 50); c[2] > 0.1 || c[0] < 0.3 {
		t.Errorf("half RGB blue = %v, want cyan and magenta ink", c)
	}

	// Unforced, a page whose content paints opaque, with Normal, stays in
	// sRGB.
	opaque := fixture{page: "/Group << /S /Transparency /CS /DeviceCMYK >>", content: "0 0 1 rg 0 0 50 100 re f"}
	doc, pg = open(t, opaque.bytes(), limits.Limits{})
	res, _, err = RenderGroup(context.Background(), doc, pg, Options{DPI: 72, Background: white})
	if err != nil {
		t.Fatal(err)
	}
	if res.Space != nil || res.Group.Space() != nil {
		t.Errorf("unforced page group blends in %v, want sRGB", res.Space)
	}
}

// TestNormalGroupKeepsParentSpace checks the leniency of
// [renderer.formSpace]: a CMYK knockout group, which gets a raster of its
// own, whose content paints opaque with Normal renders as its content does
// without a group, so an RGB colour inside it keeps its gamut.
func TestNormalGroupKeepsParentSpace(t *testing.T) {
	t.Parallel()
	grouped, _ := draw(t, fixture{
		page:    "/Resources << /XObject << /G 5 0 R >> >>",
		content: "/G Do",
		objs: []string{stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Group << /S /Transparency /I true /K true /CS /DeviceCMYK >> "+
			"/Resources << >>", "0 0 1 rg 0 0 100 100 re f")},
	})
	plain, _ := draw(t, fixture{content: "0 0 1 rg 0 0 100 100 re f"})
	expect(t, grouped, 0, map[[2]int]rgb{{50, 50}: at(t, plain, 50, 50)})
}
