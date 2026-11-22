package layout

import (
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// MeterLevel is the region a meter's value lies in against its optimum
// (HTML Standard, 4.10.14): what colour its widget fills its bar with.
type MeterLevel uint8

// The meter regions.
const (
	// MeterOptimum is the optimum region, or a meter with no low or high
	// boundary.
	MeterOptimum MeterLevel = iota
	// MeterSuboptimal is the region next to the optimum one.
	MeterSuboptimal
	// MeterEvenLessGood is the region past the suboptimal one.
	MeterEvenLessGood
)

// widgetTheme holds the colours of Chromium's form control widgets under
// one colour scheme, opaque, each stroke composited over the fill it is
// drawn on as Chromium's PDF draws it.
type widgetTheme struct {
	// field and fieldEdge fill and edge a text field, a textarea and a
	// drop-down box, listEdge a list box.
	field, fieldEdge, listEdge typeset.Color
	// button and buttonEdge fill and edge a button and a colour well.
	button, buttonEdge typeset.Color
	// box and boxEdge fill and edge a check box and a radio button that
	// are not checked.
	box, boxEdge typeset.Color
	// track and trackEdge fill and edge the track of a range and a
	// progress bar, meterEdge a meter's.
	track, trackEdge, meterEdge typeset.Color
	// accent fills a checked check box, a radio button's ring and dot, a
	// range's value and thumb and a progress bar's value; tick draws the
	// check of a checked box.
	accent, tick typeset.Color
	// meter fills a meter's value in each region.
	meter [3]typeset.Color
	// selected marks the selected rows of a list box.
	selected typeset.Color
}

// hexColor returns the opaque colour 0xRRGGBB.
func hexColor(v uint32) typeset.Color {
	return typeset.Color{R: float64(v>>16&0xff) / 255, G: float64(v>>8&0xff) / 255, B: float64(v&0xff) / 255, A: 1}
}

// over returns c at opacity a composited over the opaque base.
func over(c, base typeset.Color, a float64) typeset.Color {
	mix := func(x, y float64) float64 { return float64(x*a) + float64(y*(1-a)) }
	return typeset.Color{R: mix(c.R, base.R), G: mix(c.G, base.G), B: mix(c.B, base.B), A: 1}
}

// Chromium's widget colours under the light and the dark colour schemes.
//
// A text field's and a drop-down box's edge is a 1 pixel line of #767676
// (#858585 dark) Chromium's PDF writes as a hairline at 75 per cent; a
// track's edge #767676 at 50 per cent, a meter's at 30 per cent.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints, light:
//     fields #ffffff edged #767676 at 0.75, buttons and colour wells
//     #efefef edged #767676, check boxes and radio buttons #ffffff edged
//     #767676, checked #0075ff with a #ffffff tick, range and progress
//     tracks #efefef edged #767676 at 0.5 and filled #0075ff, meter
//     tracks #efefef edged #767676 at 0.3 and filled #107c10, #ffb900 or
//     #d83b01, a list box's selected rows #cecece; dark: fields #3b3b3b
//     edged #858585 at 0.75, buttons #6b6b6b edged #6b6b6b, boxes
//     #3b3b3b edged #858585, checked #99c8ff with a #3b3b3b tick, tracks
//     #3b3b3b edged #858585 and filled #99c8ff, the meter's optimum
//     #74b374, a list box's selected rows #545454.
var (
	lightWidgets = widgetTheme{
		field: hexColor(0xffffff), fieldEdge: over(hexColor(0x767676), hexColor(0xffffff), 0.75), listEdge: hexColor(0x767676),
		button: hexColor(0xefefef), buttonEdge: hexColor(0x767676),
		box: hexColor(0xffffff), boxEdge: hexColor(0x767676),
		track: hexColor(0xefefef), trackEdge: over(hexColor(0x767676), hexColor(0xefefef), 0.5),
		meterEdge: over(hexColor(0x767676), hexColor(0xefefef), 0.3),
		accent:    hexColor(0x0075ff), tick: hexColor(0xffffff),
		meter:    [3]typeset.Color{hexColor(0x107c10), hexColor(0xffb900), hexColor(0xd83b01)},
		selected: hexColor(0xcecece),
	}
	darkWidgets = widgetTheme{
		field: hexColor(0x3b3b3b), fieldEdge: over(hexColor(0x858585), hexColor(0x3b3b3b), 0.75), listEdge: hexColor(0x858585),
		button: hexColor(0x6b6b6b), buttonEdge: hexColor(0x6b6b6b),
		box: hexColor(0x3b3b3b), boxEdge: hexColor(0x858585),
		track: hexColor(0x3b3b3b), trackEdge: hexColor(0x858585), meterEdge: hexColor(0x858585),
		accent: hexColor(0x99c8ff), tick: hexColor(0x3b3b3b),
		meter:    [3]typeset.Color{hexColor(0x74b374), hexColor(0xffb900), hexColor(0xd83b01)},
		selected: hexColor(0x545454),
	}
)

// themeOf returns the widget colours of a control styled st: the dark
// ones under a dark used colour scheme, the light ones else, and its
// accent-color in place of the default accent. An accent whose contrast
// with white is under 3 (WCAG 2, 1.4.11's ratio for the parts of a
// control) takes the dark colours around it, as Chromium draws it, so
// that it stands out from its box.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws a checked
//     box with a #ffffff tick on its accent from gray #949494 (contrast
//     3.03) down, and on #3b3b3b with a #3b3b3b tick from #9c9c9c (2.75)
//     up; #ff8800, #00cccc, #ff7f7f, #00ff00 and #ffff00 on the dark
//     colours, #ff0000, #0000ff, #cc00cc, #888800, #008888 and #7f7fff on
//     the light ones.
func themeOf(st *css.Style) widgetTheme {
	t := lightWidgets
	if st.DarkScheme {
		t = darkWidgets
	}
	if st.AccentColor == nil {
		return t
	}
	accent, ok := colorOf(*st.AccentColor)
	if !ok {
		return t
	}
	accent = over(accent, t.field, accent.A)
	if contrastWithWhite(accent) < 3 {
		t = darkWidgets
	} else {
		t.tick = hexColor(0xffffff)
	}
	t.accent = accent
	return t
}

// contrastWithWhite returns the contrast ratio of the opaque colour c
// against white (WCAG 2, relative luminance and contrast ratio).
func contrastWithWhite(c typeset.Color) float64 {
	lin := func(v float64) float64 {
		if v <= 0.04045 {
			return v / 12.92
		}
		return detmath.Pow((v+0.055)/1.055, 2.4)
	}
	l := float64(0.2126*lin(c.R)) + float64(0.7152*lin(c.G)) + float64(0.0722*lin(c.B))
	return 1.05 / (l + 0.05)
}
