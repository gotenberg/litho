package page

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// TestGreyICCGroupTakesColoursThroughItsProfile checks ISO 32000-1, 11.3.4: a
// colour painted into a page group whose /CS is a grey ICCBased space enters
// that space through its profile (8.6.5.5). Under a tone curve of gamma 1.0
// the sRGB grey 0.66 is the component 0.393, its luminance, and it reads back
// as itself, as it draws outside the group; the device formula of 10.3 kept
// 0.66, which the profile read as 0.83. A DeviceGray group keeps the
// formula, which 10.3.3 defines for it.
func TestGreyICCGroupTakesColoursThroughItsProfile(t *testing.T) {
	t.Parallel()
	profile := stream("/N 1", string(linearGreyProfile()))
	const content = "0.66 0.66 0.66 rg 0 0 100 100 re f q /A gs 0 g 0 0 2 2 re f Q"
	const resources = " /Resources << /ExtGState << /A << /ca 0.5 >> >> >>"
	for _, c := range []struct {
		name string
		cs   string
		want float64
	}{
		{"ICCBased", "[/ICCBased 5 0 R]", 0.393},
		{"DeviceGray", "/DeviceGray", 0.66},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := fixture{
				page:    "/Group << /S /Transparency /CS " + c.cs + " >>" + resources,
				content: content,
				objs:    []string{profile},
			}
			doc, pg := open(t, f.bytes(), limits.Limits{})
			opts := f.options()
			opts.GroupSpace = true
			res, _, err := RenderGroup(context.Background(), doc, pg, opts)
			if err != nil {
				t.Fatal(err)
			}
			if got, _ := res.Group.Components(50, 50); math.Abs(float64(got[0])-c.want) > 0.01 {
				t.Errorf("sRGB grey 0.66 enters the group as %v, want %v", got[0], c.want)
			}
			img, _ := draw(t, f)
			if got, want := at(t, img, 50, 50), (rgb{168, 168, 168}); c.name == "ICCBased" && !close8(got, want, 1) {
				t.Errorf("sRGB grey 0.66 draws %v in the group, want %v as outside it", got, want)
			}
		})
	}
}

// TestGreyICCLuminositySoftMask checks a luminosity soft mask whose group's
// /CS is a grey ICCBased space of a linear tone curve (ISO 32000-1, 11.5.3):
// the group's colour enters it through the profile, and its luminosity is
// read as the colour looks, so the mask is the one the same group masks with
// in RGB, as poppler 26.09.0, PDFium and MuPDF 1.26.10 draw it, rather than
// the component, the colour's luminance.
func TestGreyICCLuminositySoftMask(t *testing.T) {
	t.Parallel()
	maskGroup := func(cs string) string {
		return stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Group << /S /Transparency /CS "+cs+" >>",
			"0.5 0.5 0.5 rg 0 0 100 100 re f")
	}
	img, _ := draw(t, fixture{
		page: "/Resources << /ExtGState << /G << /SMask << /S /Luminosity /G 6 0 R >> >> " +
			"/R << /SMask << /S /Luminosity /G 7 0 R >> >> >> >>",
		content: "q /G gs 0 g 0 0 50 100 re f Q q /R gs 0 g 50 0 50 100 re f Q",
		objs:    []string{stream("/N 1", string(linearGreyProfile())), maskGroup("[/ICCBased 5 0 R]"), maskGroup("/DeviceRGB")},
	})
	grey, rgbMask := at(t, img, 25, 50), at(t, img, 75, 50)
	if !close8(grey, rgbMask, 2) {
		t.Errorf("black through the grey ICC group's mask = %v, through the RGB group's %v", grey, rgbMask)
	}
	if rgbMask.r < 120 || rgbMask.r > 135 {
		t.Errorf("black through a mask of luminosity 0.5 = %v, want about 128", rgbMask)
	}
}
