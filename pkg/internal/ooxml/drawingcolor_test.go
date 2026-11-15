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
		// PowerPoint's colours of a probe: a half level rounds down.
		{"lumMod lumOff", `<a:srgbClr val="FF0000"><a:lumMod val="50000"/><a:lumOff val="0"/></a:srgbClr>`, [4]int{127, 0, 0, 255}, true},
		{"sat 0 tie", `<a:srgbClr val="FF8000"><a:sat val="0"/></a:srgbClr>`, [4]int{127, 127, 127, 255}, true},
		{"lumMod white tie", `<a:srgbClr val="FFFFFF"><a:lumMod val="50000"/></a:srgbClr>`, [4]int{127, 127, 127, 255}, true},
		{"satOff", `<a:srgbClr val="4472C4"><a:satOff val="-30000"/></a:srgbClr>`, [4]int{105, 124, 159, 255}, true},
		{"tint then lumMod", `<a:srgbClr val="4472C4"><a:tint val="50000"/><a:lumMod val="80000"/></a:srgbClr>`, [4]int{134, 150, 203, 255}, true},
		{"lumMod then tint", `<a:srgbClr val="4472C4"><a:lumMod val="80000"/><a:tint val="50000"/></a:srgbClr>`, [4]int{190, 196, 215, 255}, true},
		{"shade then inv", `<a:srgbClr val="4472C4"><a:shade val="50000"/><a:inv/></a:srgbClr>`, [4]int{252, 245, 221, 255}, true},
		{"gray then tint", `<a:srgbClr val="ED7D31"><a:gray/><a:tint val="50000"/></a:srgbClr>`, [4]int{209, 209, 209, 255}, true},
		{"gray 70AD47", `<a:srgbClr val="70AD47"><a:gray/></a:srgbClr>`, [4]int{153, 153, 153, 255}, true},
		{"tint black", `<a:srgbClr val="000000"><a:tint val="50000"/></a:srgbClr>`, [4]int{188, 188, 188, 255}, true},
		{"alpha", `<a:srgbClr val="000000"><a:alpha val="50000"/></a:srgbClr>`, [4]int{0, 0, 0, 128}, true},
		{"sys default", `<a:sysClr val="window"/>`, [4]int{255, 255, 255, 255}, true},
		// LibreOffice measured: a windowText line saved red prints black.
		{"sys over lastClr", `<a:sysClr val="windowText" lastClr="FF0000"/>`, [4]int{0, 0, 0, 255}, true},
		{"unknown sys keeps lastClr", `<a:sysClr val="gradientActiveCaption" lastClr="FF0000"/>`, [4]int{255, 0, 0, 255}, true},
		// PowerPoint's colours of a probe: the system's, not lastClr.
		{"sys highlight", `<a:sysClr val="highlight" lastClr="0078D7"/>`, [4]int{0x33, 0x99, 0xFF, 255}, true},
		{"sys hotLight", `<a:sysClr val="hotLight" lastClr="FF0000"/>`, [4]int{0, 0x66, 0xCC, 255}, true},
		{"sys btnFace", `<a:sysClr val="btnFace" lastClr="FF0000"/>`, [4]int{0xF0, 0xF0, 0xF0, 255}, true},
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
		// HSL gives 73.5 red and 194.5 blue: a half level rounds down.
		{"satMod within 1", `<a:schemeClr val="accent1"><a:satMod val="110000"/></a:schemeClr>`, [4]int{73, 128, 194, 255}, true},
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

func TestSchemeColor_AppliesTheDrawingTransforms(t *testing.T) {
	t.Parallel()
	theme := &Theme{Colors: map[string]css.Color{"accent1": {R: 0x44 / 255.0, G: 0x72 / 255.0, B: 0xC4 / 255.0, A: 1}}}
	for _, tc := range []struct {
		name, xml string
		want      [4]int
	}{
		// The transforms DrawingColorChoice applies, in Word's and Excel's
		// shapes and charts too.
		{"shade", `<a:schemeClr val="accent1"><a:shade val="50000"/></a:schemeClr>`, [4]int{47, 82, 143, 255}},
		{"tint", `<a:schemeClr val="accent1"><a:tint val="40000"/></a:schemeClr>`, [4]int{207, 213, 234, 255}},
		{"lumMod lumOff", `<a:schemeClr val="accent1"><a:lumMod val="20000"/><a:lumOff val="80000"/></a:schemeClr>`, [4]int{218, 227, 243, 255}},
		{"inv", `<a:schemeClr val="accent1"><a:inv/></a:schemeClr>`, [4]int{248, 235, 179, 255}},
		{"alpha", `<a:schemeClr val="accent1"><a:alpha val="50000"/></a:schemeClr>`, [4]int{68, 114, 196, 128}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, err := Parse(context.Background(), []byte(`<a:solidFill xmlns:a="`+NSA+`">`+tc.xml+`</a:solidFill>`), nil)
			if err != nil {
				t.Fatal(err)
			}
			c, ok := SchemeColor(root, theme)
			if !ok {
				t.Fatal("no colour")
			}
			got := [4]int{int(math.Round(c.R * 255)), int(math.Round(c.G * 255)), int(math.Round(c.B * 255)), int(math.Round(c.A * 255))}
			if got != tc.want {
				t.Fatalf("colour %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDrawingColor_HoldsWholeLevels(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, xml string
		want      float64
	}{
		// HSL gives 127.5 exactly: PowerPoint paints 127.
		{"tie", `<a:srgbClr val="FFFFFF"><a:lumMod val="50000"/></a:srgbClr>`, 127},
		// 187.52 on the sRGB curve: PowerPoint paints 188.
		{"nearest", `<a:srgbClr val="000000"><a:tint val="50000"/></a:srgbClr>`, 188},
		{"scrgb", `<a:scrgbClr r="50000" g="50000" b="50000"/>`, 188},
		// HSL gives 22.5 in exact arithmetic, 22.500000000000004 in
		// floating point: Word paints 22.
		{"tie in floating point", `<a:srgbClr val="1EC020"><a:lumMod val="75000"/></a:srgbClr>`, 22},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, err := Parse(context.Background(), []byte(`<a:solidFill xmlns:a="`+NSA+`">`+tc.xml+`</a:solidFill>`), nil)
			if err != nil {
				t.Fatal(err)
			}
			c, ok := DrawingColor(root, nil)
			if !ok {
				t.Fatal("no colour")
			}
			if c.R != tc.want/255 {
				t.Fatalf("red %v (%v levels), want %v levels exactly", c.R, c.R*255, tc.want)
			}
		})
	}
}
