package sheet

import (
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// The colours of the icons Calc paints, sampled from its icon theme as the
// pinned LibreOffice prints them: each icon a fill and an outline.
var (
	iconRed       = iconColors{fill: rgb(0xff, 0x91, 0x98), line: rgb(0xd4, 0x23, 0x14)}
	iconYellow    = iconColors{fill: rgb(0xf8, 0xdb, 0x8f), line: rgb(0xed, 0x87, 0x33)}
	iconGreen     = iconColors{fill: rgb(0xa1, 0xdd, 0xaa), line: rgb(0x30, 0x90, 0x48)}
	iconGray      = iconColors{fill: rgb(0x79, 0x77, 0x74), line: rgb(0x3a, 0x3a, 0x38)}
	iconLightGray = iconColors{fill: rgb(0xc8, 0xc6, 0xc4), line: rgb(0x79, 0x77, 0x74)}
	iconBlack     = iconColors{fill: rgb(0x3a, 0x3a, 0x38), line: rgb(0x3a, 0x3a, 0x38)}
	iconDarkRed   = iconColors{fill: rgb(0xd4, 0x23, 0x14), line: rgb(0xd4, 0x23, 0x14)}
	iconBlue      = iconColors{fill: rgb(0x83, 0xbd, 0xeb), line: rgb(0x00, 0x63, 0xb1)}
	iconOff       = iconColors{fill: rgb(0x7a, 0x78, 0x74), line: rgb(0x3a, 0x3a, 0x38)}
	iconLight     = iconColors{fill: rgb(0xed, 0x3d, 0x3b), line: rgb(0xd4, 0x23, 0x14)}
	iconWhite     = rgb(0xfa, 0xfa, 0xfa)
)

// iconColors is an icon's fill and outline colours.
type iconColors struct{ fill, line compose.Color }

// rgb returns the opaque colour of the 8-bit channels r, g, b.
func rgb(r, g, b uint8) compose.Color {
	return compose.Color{R: float64(r) / 255, G: float64(g) / 255, B: float64(b) / 255, A: 1}
}

// icon draws one icon in a 16 by 16 box whose top-left corner is (0, 0),
// y pointing down: the grid of Calc's 16-pixel icon bitmaps.
type icon func() []compose.Fragment

// iconSets are the icons of each icon set, lowest band first, as Calc
// draws them (sc/source/core/data/colorscale.cxx, ScIconSetFormat's
// icon tables): drawn as vectors of the silhouettes and colours of the
// pinned LibreOffice's icon theme.
var iconSets = map[string][]icon{ //nolint:gochecknoglobals // a read-only table of the icon sets.
	"3Arrows":         {arrow(90, iconRed), arrow(0, iconYellow), arrow(-90, iconGreen)},
	"3ArrowsGray":     {arrow(90, iconGray), arrow(0, iconGray), arrow(-90, iconGray)},
	"3Flags":          {flag(iconRed), flag(iconYellow), flag(iconGreen)},
	"3TrafficLights1": {disc(iconLight), disc(iconYellow), disc(iconGreen)},
	"3TrafficLights2": {lamp(rgb(0xed, 0x3d, 0x3b)), lamp(rgb(0xfb, 0x98, 0x3b)), lamp(rgb(0x18, 0xab, 0x50))},
	"3Signs":          {diamond(iconRed), triangle(iconYellow), smallDisc(iconGreen)},
	"3Symbols":        {symbol(iconRed, 'x'), symbol(iconYellow, '!'), symbol(iconGreen, 'v')},
	"3Symbols2":       {symbol(iconRed, 'x'), symbol(iconYellow, '!'), symbol(iconGreen, 'v')},
	"3Smilies":        {face(iconYellow, -1), face(iconYellow, 0), face(iconYellow, 1)},
	"3ColorSmilies":   {face(iconRed, -1), face(iconYellow, 0), face(iconGreen, 1)},
	"3Stars":          {star(0), star(0.5), star(1)},
	"3Triangles":      {pointer(false, iconRed), dash(iconYellow), pointer(true, iconGreen)},
	"4Arrows":         {arrow(90, iconRed), arrow(45, iconYellow), arrow(-45, iconYellow), arrow(-90, iconGreen)},
	"4ArrowsGray":     {arrow(90, iconGray), arrow(45, iconGray), arrow(-45, iconGray), arrow(-90, iconGray)},
	"4RedToBlack":     {disc(iconBlack), disc(iconLightGray), disc(iconRed), disc(iconDarkRed)},
	"4Rating":         {bars(1), bars(2), bars(3), bars(4)},
	"4TrafficLights":  {disc(iconGray), disc(iconLight), disc(iconYellow), disc(iconGreen)},
	"5Arrows":         {arrow(90, iconRed), arrow(45, iconYellow), arrow(0, iconYellow), arrow(-45, iconYellow), arrow(-90, iconGreen)},
	"5ArrowsGray":     {arrow(90, iconGray), arrow(45, iconGray), arrow(0, iconGray), arrow(-45, iconGray), arrow(-90, iconGray)},
	"5Rating":         {bars(0), bars(1), bars(2), bars(3), bars(4)},
	"5Quarters":       {quarters(0), quarters(1), quarters(2), quarters(3), quarters(4)},
	"5Boxes":          {boxes(0), boxes(1), boxes(2), boxes(3), boxes(4)},
}

// iconOf returns the icon of band i of the rule, nil for none.
func iconOf(rule *Rule, i int) icon {
	set := iconSets[rule.Icons]
	if i < 0 || i >= len(set) {
		return nil
	}
	if rule.IconsReversed {
		i = len(set) - 1 - i
	}
	if i < len(rule.IconsCustom) && rule.IconsCustom[i] != "" {
		name, idx, _ := strings.Cut(rule.IconsCustom[i], ":")
		k, err := strconv.Atoi(idx)
		if other := iconSets[name]; err == nil && k >= 0 && k < len(other) {
			return other[k]
		}
		return nil
	}
	return set[i]
}

// iconSide returns the side of the icon of a cell whose font is size
// points: Calc sizes it from the font's height in 1/100 mm read as
// points' worth of device units, 72/254 of the size (measured on the
// pinned LibreOffice: 2.835 points at 10 points, 3.118 at 11, 6.803 at
// 24).
func iconSide(size float64) float64 { return size * 72 / 254 }

// The icon's offsets from the cell's bottom-left corner, in points
// (measured on the pinned LibreOffice: 0.227 and 0.255 whatever the
// alignment).
const (
	iconInsetX = 0.227
	iconInsetY = 0.255
)

// placeIcon returns ic's fragments in the box of side s whose top-left
// corner is (x, y).
func placeIcon(ic icon, x, y, s float64) compose.Fragment {
	k := s / 16
	return &compose.Group{Transform: compose.Matrix{A: k, D: k, E: x, F: y}, Opacity: 1, Content: ic()}
}

// poly returns the closed polygon of the points xy, pairs of coordinates.
func poly(xy ...float64) []compose.PathOp {
	var ops []compose.PathOp
	for i := 0; i+1 < len(xy); i += 2 {
		k := compose.LineTo
		if i == 0 {
			k = compose.MoveTo
		}
		ops = append(ops, compose.PathOp{Kind: k, P: [3]compose.Point{{X: xy[i], Y: xy[i+1]}}})
	}
	return append(ops, compose.PathOp{Kind: compose.ClosePath})
}

// ellipse returns the path of the ellipse of centre (cx, cy) and radii rx
// and ry, as four cubic Béziers.
func ellipse(cx, cy, rx, ry float64) []compose.PathOp {
	const k = 0.5522847498
	pt := func(x, y float64) compose.Point { return compose.Point{X: x, Y: y} }
	return []compose.PathOp{
		{Kind: compose.MoveTo, P: [3]compose.Point{pt(cx+rx, cy)}},
		{Kind: compose.CurveTo, P: [3]compose.Point{pt(cx+rx, cy+k*ry), pt(cx+k*rx, cy+ry), pt(cx, cy+ry)}},
		{Kind: compose.CurveTo, P: [3]compose.Point{pt(cx-k*rx, cy+ry), pt(cx-rx, cy+k*ry), pt(cx-rx, cy)}},
		{Kind: compose.CurveTo, P: [3]compose.Point{pt(cx-rx, cy-k*ry), pt(cx-k*rx, cy-ry), pt(cx, cy-ry)}},
		{Kind: compose.CurveTo, P: [3]compose.Point{pt(cx+k*rx, cy-ry), pt(cx+rx, cy-k*ry), pt(cx+rx, cy)}},
		{Kind: compose.ClosePath},
	}
}

// shapeOf returns ops filled with fill and outlined with line, 0.6 of a
// pixel wide, which draws as light as the theme's antialiased outlines;
// a transparent colour leaves its part out.
func shapeOf(ops []compose.PathOp, fill, line compose.Color) compose.Fragment {
	p := &compose.Path{Ops: ops}
	if fill.A > 0 {
		p.Fill = &compose.Fill{Color: fill}
	}
	if line.A > 0 {
		p.Stroke = &compose.Stroke{Color: line, Width: 0.6, Join: compose.MiterJoin}
	}
	return p
}

// strokeOf returns the open path ops stroked in c, w pixels wide.
func strokeOf(ops []compose.PathOp, c compose.Color, w float64) compose.Fragment {
	return &compose.Path{Ops: ops, Stroke: &compose.Stroke{Color: c, Width: w, Cap: compose.RoundCap, Join: compose.RoundJoin}}
}

// line returns the path from (x0, y0) to (x1, y1).
func line(x0, y0, x1, y1 float64) []compose.PathOp {
	return []compose.PathOp{
		{Kind: compose.MoveTo, P: [3]compose.Point{{X: x0, Y: y0}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: x1, Y: y1}}},
	}
}

// arrow returns an arrow pointing right turned clockwise by deg degrees
// about the box's centre: 90 points down, -90 up, a diagonal one shrunk
// to fit the box.
func arrow(deg float64, c iconColors) icon {
	return func() []compose.Fragment {
		ops := poly(0.5, 4.5, 8.5, 4.5, 8.5, 0.5, 15.5, 8, 8.5, 15.5, 8.5, 11.5, 0.5, 11.5)
		rad := deg * math.Pi / 180
		sin, cos := math.Sincos(rad)
		k := 1.0
		if math.Abs(sin) > 0.1 && math.Abs(cos) > 0.1 {
			// A diagonal arrow runs from corner to corner.
			k = 1.3
		}
		m := compose.Matrix{A: k * cos, B: k * sin, C: -k * sin, D: k * cos}
		m.E, m.F = 8-m.A*8-m.C*8, 8-m.B*8-m.D*8
		return []compose.Fragment{&compose.Group{Transform: m, Opacity: 1, Content: []compose.Fragment{shapeOf(ops, c.fill, c.line)}}}
	}
}

// flag returns a flag on a dark pole.
func flag(c iconColors) icon {
	return func() []compose.Fragment {
		return []compose.Fragment{
			shapeOf(poly(2, 0, 4, 0, 4, 16, 2, 16), iconBlack.fill, compose.Color{}),
			shapeOf(poly(4.5, 1.5, 15.5, 1.5, 15.5, 8.5, 4.5, 8.5), c.fill, c.line),
		}
	}
}

// disc returns a disc filling the box.
func disc(c iconColors) icon {
	return func() []compose.Fragment { return []compose.Fragment{shapeOf(ellipse(8, 8, 7.5, 7.5), c.fill, c.line)} }
}

// smallDisc returns a disc of half the box.
func smallDisc(c iconColors) icon {
	return func() []compose.Fragment { return []compose.Fragment{shapeOf(ellipse(8, 8, 5, 5), c.fill, c.line)} }
}

// lamp returns a coloured disc in a dark square, a traffic light.
func lamp(c compose.Color) icon {
	return func() []compose.Fragment {
		return []compose.Fragment{
			shapeOf(poly(0, 0, 16, 0, 16, 16, 0, 16), iconBlack.fill, compose.Color{}),
			shapeOf(ellipse(8, 8, 6.5, 6.5), c, compose.Color{}),
		}
	}
}

// diamond returns a diamond filling the box.
func diamond(c iconColors) icon {
	return func() []compose.Fragment {
		return []compose.Fragment{shapeOf(poly(8, 0.5, 15.5, 8, 8, 15.5, 0.5, 8), c.fill, c.line)}
	}
}

// triangle returns an upward triangle filling the box.
func triangle(c iconColors) icon {
	return func() []compose.Fragment {
		return []compose.Fragment{shapeOf(poly(8, 1, 15.5, 15.5, 0.5, 15.5), c.fill, c.line)}
	}
}

// pointer returns the small triangle of 3Triangles, pointing up or down.
func pointer(up bool, c iconColors) icon {
	return func() []compose.Fragment {
		if up {
			return []compose.Fragment{shapeOf(poly(8, 1.5, 15.5, 8.5, 0.5, 8.5), c.fill, c.line)}
		}
		return []compose.Fragment{shapeOf(poly(0.5, 7.5, 15.5, 7.5, 8, 14.5), c.fill, c.line)}
	}
}

// dash returns the flat bar of 3Triangles' middle band.
func dash(c iconColors) icon {
	return func() []compose.Fragment {
		return []compose.Fragment{shapeOf(poly(0.5, 6.5, 15.5, 6.5, 15.5, 9.5, 0.5, 9.5), c.fill, c.line)}
	}
}

// symbol returns a disc with a white glyph: a cross 'x', an exclamation
// mark '!' or a check mark 'v'.
func symbol(c iconColors, glyph byte) icon {
	return func() []compose.Fragment {
		out := []compose.Fragment{shapeOf(ellipse(8, 8, 7.5, 7.5), c.fill, c.line)}
		switch glyph {
		case 'x':
			out = append(out, strokeOf(line(5, 5, 11, 11), iconWhite, 2), strokeOf(line(11, 5, 5, 11), iconWhite, 2))
		case '!':
			out = append(out, strokeOf(line(8, 3.5, 8, 9), iconWhite, 2), strokeOf(line(8, 12, 8, 12.5), iconWhite, 2))
		default:
			out = append(out, strokeOf(append(line(4.5, 8.5, 7, 11), compose.PathOp{Kind: compose.LineTo, P: [3]compose.Point{{X: 11.5, Y: 5}}}), iconWhite, 2))
		}
		return out
	}
}

// face returns a disc with eyes and a mouth that frowns (mood -1), is flat
// (0) or smiles (1).
func face(c iconColors, mood float64) icon {
	return func() []compose.Fragment {
		feature := c.line
		mouth := []compose.PathOp{
			{Kind: compose.MoveTo, P: [3]compose.Point{{X: 4.5, Y: 10.5}}},
			{Kind: compose.CurveTo, P: [3]compose.Point{{X: 6, Y: 10.5 + 2*mood}, {X: 10, Y: 10.5 + 2*mood}, {X: 11.5, Y: 10.5}}},
		}
		return []compose.Fragment{
			shapeOf(ellipse(8, 8, 7.5, 7.5), c.fill, c.line),
			shapeOf(ellipse(5.5, 5.5, 1, 1), feature, compose.Color{}),
			shapeOf(ellipse(10.5, 5.5, 1, 1), feature, compose.Color{}),
			strokeOf(mouth, feature, 1),
		}
	}
}

// star returns a five-pointed star filled for the share full of its width
// from the left, the rest grey.
func star(full float64) icon {
	return func() []compose.Fragment {
		xy := make([]float64, 0, 20)
		for i := range 10 {
			r := 7.5
			if i%2 == 1 {
				r = 3.2
			}
			a := -math.Pi/2 + float64(i)*math.Pi/5
			xy = append(xy, 8+r*math.Cos(a), 8.4+r*math.Sin(a))
		}
		ops := poly(xy...)
		switch {
		case full <= 0:
			return []compose.Fragment{shapeOf(ops, iconLightGray.fill, iconBlack.line)}
		case full >= 1:
			return []compose.Fragment{shapeOf(ops, iconYellow.fill, iconYellow.line)}
		}
		w := 16 * full
		return []compose.Fragment{
			shapeOf(ops, iconOff.fill, compose.Color{}),
			&compose.Group{Clip: &compose.Path{Ops: poly(0, 0, w, 0, w, 16, 0, 16), Fill: &compose.Fill{}}, Opacity: 1, Content: []compose.Fragment{shapeOf(ops, iconYellow.fill, compose.Color{})}},
			shapeOf(ops, compose.Color{}, iconYellow.line),
		}
	}
}

// bars returns four bars rising left to right, the first lit ones blue.
func bars(lit int) icon {
	return func() []compose.Fragment {
		out := make([]compose.Fragment, 0, 4)
		for i := range 4 {
			c := iconOff
			if i < lit {
				c = iconBlue
			}
			top := 10.5 - 3.5*float64(i)
			x := 0.5 + 4*float64(i)
			out = append(out, shapeOf(poly(x, top, x+2, top, x+2, 15.5, x, 15.5), c.fill, c.line))
		}
		return out
	}
}

// quarters returns a white disc filled grey by n quarters, clockwise from
// twelve o'clock.
func quarters(n int) icon {
	return func() []compose.Fragment {
		out := []compose.Fragment{shapeOf(ellipse(8, 8, 7.5, 7.5), iconWhite, iconBlack.line)}
		switch {
		case n >= 4:
			out = append(out, shapeOf(ellipse(8, 8, 7.5, 7.5), iconGray.fill, iconBlack.line))
		case n > 0:
			clip := map[int][]float64{1: {8, 0, 16, 0, 16, 8, 8, 8}, 2: {8, 0, 16, 0, 16, 16, 8, 16}, 3: {8, 0, 16, 0, 16, 16, 0, 16, 0, 8, 8, 8}}[n]
			pie := []compose.Fragment{shapeOf(ellipse(8, 8, 7.5, 7.5), iconGray.fill, compose.Color{})}
			out = append(out, &compose.Group{Clip: &compose.Path{Ops: poly(clip...), Fill: &compose.Fill{}}, Opacity: 1, Content: pie})
		}
		return out
	}
}

// boxes returns four boxes in a square, the first lit ones blue: bottom
// left, bottom right, top left, then top right.
func boxes(lit int) icon {
	return func() []compose.Fragment {
		at := [4][2]float64{{0.5, 9.5}, {9.5, 9.5}, {0.5, 0.5}, {9.5, 0.5}}
		out := make([]compose.Fragment, 0, len(at))
		for i, p := range at {
			c := iconOff
			if i < lit {
				c = iconBlue
			}
			out = append(out, shapeOf(poly(p[0], p[1], p[0]+6, p[1], p[0]+6, p[1]+6, p[0], p[1]+6), c.fill, c.line))
		}
		return out
	}
}
