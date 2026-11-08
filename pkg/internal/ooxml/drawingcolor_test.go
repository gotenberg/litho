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
		// Impress fills the 40% tint of 4F81BD with D0D8E7: a tint in
		// linear RGB of gamma 2.3.
		{"tint linear", `<a:schemeClr val="accent1"><a:tint val="40000"/></a:schemeClr>`, [4]int{0xD0, 0xD8, 0xE7, 255}, true},
		{"tint 20", `<a:schemeClr val="accent1"><a:tint val="20000"/></a:schemeClr>`, [4]int{0xE9, 0xEC, 0xF3, 255}, true},
		{"shade", `<a:srgbClr val="FFFFFF"><a:shade val="50000"/></a:srgbClr>`, [4]int{189, 189, 189, 255}, true},
		{"lumMod lumOff", `<a:srgbClr val="FF0000"><a:lumMod val="50000"/><a:lumOff val="0"/></a:srgbClr>`, [4]int{128, 0, 0, 255}, true},
		{"alpha", `<a:srgbClr val="000000"><a:alpha val="50000"/></a:srgbClr>`, [4]int{0, 0, 0, 128}, true},
		{"sys default", `<a:sysClr val="window"/>`, [4]int{255, 255, 255, 255}, true},
		{"preset dk", `<a:prstClr val="dkBlue"/>`, [4]int{0, 0, 139, 255}, true},
		{"hsl", `<a:hslClr hue="0" sat="100000" lum="50000"/>`, [4]int{255, 0, 0, 255}, true},
		{"comp", `<a:srgbClr val="FF0000"><a:comp/></a:srgbClr>`, [4]int{0, 255, 255, 255}, true},
		{"gray", `<a:srgbClr val="FFFFFF"><a:gray/></a:srgbClr>`, [4]int{255, 255, 255, 255}, true},
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
			for i := range got {
				if d := got[i] - tc.want[i]; d < -1 || d > 1 {
					t.Fatalf("colour %v, want %v", got, tc.want)
				}
			}
		})
	}
}
