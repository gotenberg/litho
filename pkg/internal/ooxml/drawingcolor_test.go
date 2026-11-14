package ooxml

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestDrawingColor_Transforms(t *testing.T) {
	t.Parallel()
	scheme := func(name string) (css.Color, bool) {
		if name == "accent1" {
			return css.Color{R: 0x4F / 255.0, G: 0x81 / 255.0, B: 0xBD / 255.0, A: 1}, true
		}
		return css.Color{}, false
	}
	for _, tc := range []struct {
		name, xml string
		want      [4]int
		ok        bool
	}{
		{"srgb", `<a:srgbClr val="FF8000"/>`, [4]int{255, 128, 0, 255}, true},
		{"scheme", `<a:schemeClr val="accent1"/>`, [4]int{0x4F, 0x81, 0xBD, 255}, true},
		// A tint and a shade work in linear light on the sRGB curve.
		{"tint linear", `<a:schemeClr val="accent1"><a:tint val="40000"/></a:schemeClr>`, [4]int{208, 216, 232, 255}, true},
		{"tint 20", `<a:schemeClr val="accent1"><a:tint val="20000"/></a:schemeClr>`, [4]int{233, 237, 244, 255}, true},
		{"shade", `<a:srgbClr val="FFFFFF"><a:shade val="50000"/></a:srgbClr>`, [4]int{188, 188, 188, 255}, true},
		// PowerPoint's colours of litho/pptx fill-solid.
		{"shade 50", `<a:srgbClr val="4472C4"><a:shade val="50000"/></a:srgbClr>`, [4]int{47, 82, 143, 255}, true},
		{"scrgb", `<a:scrgbClr r="100000" g="50000" b="0"/>`, [4]int{255, 188, 0, 255}, true},
		{"blueOff", `<a:srgbClr val="404040"><a:blueOff val="50000"/></a:srgbClr>`, [4]int{64, 64, 196, 255}, true},
		{"greenMod", `<a:srgbClr val="404040"><a:greenMod val="200000"/></a:srgbClr>`, [4]int{64, 90, 64, 255}, true},
		{"redMod", `<a:srgbClr val="808080"><a:redMod val="0"/></a:srgbClr>`, [4]int{0, 128, 128, 255}, true},
		{"lumMod 50", `<a:srgbClr val="4472C4"><a:lumMod val="50000"/></a:srgbClr>`, [4]int{32, 56, 100, 255}, true},
		{"lumMod lumOff 20 80", `<a:srgbClr val="4472C4"><a:lumMod val="20000"/><a:lumOff val="80000"/></a:srgbClr>`, [4]int{218, 227, 243, 255}, true},
		{"hueOff", `<a:srgbClr val="FF0000"><a:hueOff val="7200000"/></a:srgbClr>`, [4]int{0, 255, 0, 255}, true},
		{"hueMod", `<a:srgbClr val="00FF00"><a:hueMod val="50000"/></a:srgbClr>`, [4]int{255, 255, 0, 255}, true},
		{"lum", `<a:srgbClr val="0000FF"><a:lum val="80000"/></a:srgbClr>`, [4]int{153, 153, 255, 255}, true},
		{"lumMod lumOff", `<a:srgbClr val="FF0000"><a:lumMod val="50000"/><a:lumOff val="0"/></a:srgbClr>`, [4]int{128, 0, 0, 255}, true},
		{"alpha", `<a:srgbClr val="000000"><a:alpha val="50000"/></a:srgbClr>`, [4]int{0, 0, 0, 128}, true},
		{"sys default", `<a:sysClr val="window"/>`, [4]int{255, 255, 255, 255}, true},
		// LibreOffice measured: a windowText line saved red prints black.
		{"sys over lastClr", `<a:sysClr val="windowText" lastClr="FF0000"/>`, [4]int{0, 0, 0, 255}, true},
		{"unknown sys keeps lastClr", `<a:sysClr val="hotLight" lastClr="FF0000"/>`, [4]int{255, 0, 0, 255}, true},
		{"preset dk", `<a:prstClr val="dkBlue"/>`, [4]int{0, 0, 139, 255}, true},
		{"hsl", `<a:hslClr hue="0" sat="100000" lum="50000"/>`, [4]int{255, 0, 0, 255}, true},
		{"comp", `<a:srgbClr val="FF0000"><a:comp/></a:srgbClr>`, [4]int{0, 255, 255, 255}, true},
		{"inv", `<a:srgbClr val="4472C4"><a:inv/></a:srgbClr>`, [4]int{248, 235, 179, 255}, true},
		{"gamma", `<a:srgbClr val="4472C4"><a:gamma/></a:srgbClr>`, [4]int{141, 178, 227, 255}, true},
		{"invGamma", `<a:srgbClr val="4472C4"><a:invGamma/></a:srgbClr>`, [4]int{15, 43, 141, 255}, true},
		{"comp 4472C4", `<a:srgbClr val="4472C4"><a:comp/></a:srgbClr>`, [4]int{196, 150, 68, 255}, true},
		// PowerPoint's colours: a saturation scaled past 1 is not clamped,
		// its components are cut to the gamut.
		{"satMod past 1", `<a:srgbClr val="F79646"><a:satMod val="180000"/></a:srgbClr>`, [4]int{255, 143, 0, 255}, true},
		{"tint satMod past 1", `<a:schemeClr val="accent1"><a:tint val="52000"/><a:satMod val="300000"/></a:schemeClr>`, [4]int{158, 193, 255, 255}, true},
		{"shade satMod past 1", `<a:schemeClr val="accent1"><a:shade val="20000"/><a:satMod val="300000"/></a:schemeClr>`, [4]int{0, 54, 146, 255}, true},
		{"tint satMod 250", `<a:schemeClr val="accent1"><a:tint val="40000"/><a:satMod val="250000"/></a:schemeClr>`, [4]int{190, 211, 249, 255}, true},
		{"satMod within 1", `<a:schemeClr val="accent1"><a:satMod val="110000"/></a:schemeClr>`, [4]int{74, 128, 195, 255}, true},
		{"gray", `<a:srgbClr val="FFFFFF"><a:gray/></a:srgbClr>`, [4]int{255, 255, 255, 255}, true},
		{"gray ED7D31", `<a:srgbClr val="ED7D31"><a:gray/></a:srgbClr>`, [4]int{143, 143, 143, 255}, true},
		{"unknown scheme", `<a:schemeClr val="accent9"/>`, [4]int{}, false},
		{"not a colour", `<a:noFill/>`, [4]int{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, err := Parse(context.Background(), []byte(`<a:solidFill xmlns:a="`+NSA+`">`+tc.xml+`</a:solidFill>`), nil)
			if err != nil {
				t.Fatal(err)
			}
			c, ok := DrawingColor(root, scheme)
			if ok != tc.ok {
				t.Fatalf("ok %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			got := [4]int{int(math.Round(c.R * 255)), int(math.Round(c.G * 255)), int(math.Round(c.B * 255)), int(math.Round(c.A * 255))}
			if got != tc.want {
				t.Fatalf("colour %v, want %v", got, tc.want)
			}
		})
	}
}
