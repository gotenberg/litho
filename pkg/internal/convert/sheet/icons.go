package sheet

import (
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/typeset"
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
type iconColors struct{ fill, line typeset.Color }

// rgb returns the opaque colour of the 8-bit channels r, g, b.
func rgb(r, g, b uint8) typeset.Color {
	return typeset.Color{R: float64(r) / 255, G: float64(g) / 255, B: float64(b) / 255, A: 1}
}

// icon draws one icon in a 16 by 16 box whose top-left corner is (0, 0),
// y pointing down: the grid of Calc's 16-pixel icon bitmaps.
type icon func() []typeset.Fragment

// iconSets are the icons of each icon set, lowest band first, as Calc
// draws them (sc/source/core/data/colorscale.cxx, ScIconSetFormat's
// icon tables): drawn as vectors of the silhouettes and colours of the
// pinned LibreOffice's icon theme.
var iconSets = map[string][]icon{
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

// iconOf returns the icon of band i of the rule, nil for none: Excel's
// art when excel is set ([excelIconSets]), Calc's otherwise.
func iconOf(rule *Rule, i int, excel bool) icon {
	sets := iconSets
	if excel {
		sets = excelIconSets
	}
	set := sets[rule.Icons]
	if i < 0 || i >= len(set) {
		return nil
	}
	if rule.IconsReversed {
		i = len(set) - 1 - i
	}
	if i < len(rule.IconsCustom) && rule.IconsCustom[i] != "" {
		name, idx, _ := strings.Cut(rule.IconsCustom[i], ":")
		k, err := intconv.Atoi(idx)
		if other := sets[name]; err == nil && k >= 0 && k < len(other) {
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
func placeIcon(ic icon, x, y, s float64) typeset.Fragment {
	k := s / 16
	return &typeset.Group{Transform: typeset.Matrix{A: k, D: k, E: x, F: y}, Opacity: 1, Content: ic()}
}

// poly returns the closed polygon of the points xy, pairs of coordinates.
func poly(xy ...float64) []typeset.PathOp {
	var ops []typeset.PathOp
	for i := 0; i+1 < len(xy); i += 2 {
		k := typeset.LineTo
		if i == 0 {
			k = typeset.MoveTo
		}
		ops = append(ops, typeset.PathOp{Kind: k, P: [3]typeset.Point{{X: xy[i], Y: xy[i+1]}}})
	}
	return append(ops, typeset.PathOp{Kind: typeset.ClosePath})
}

// ellipse returns the path of the ellipse of centre (cx, cy) and radii rx
// and ry, as four cubic Béziers.
func ellipse(cx, cy, rx, ry float64) []typeset.PathOp {
	const k = 0.5522847498
	pt := func(x, y float64) typeset.Point { return typeset.Point{X: x, Y: y} }
	return []typeset.PathOp{
		{Kind: typeset.MoveTo, P: [3]typeset.Point{pt(cx+rx, cy)}},
		{Kind: typeset.CurveTo, P: [3]typeset.Point{pt(cx+rx, cy+float64(k*ry)), pt(cx+float64(k*rx), cy+ry), pt(cx, cy+ry)}},
		{Kind: typeset.CurveTo, P: [3]typeset.Point{pt(cx-float64(k*rx), cy+ry), pt(cx-rx, cy+float64(k*ry)), pt(cx-rx, cy)}},
		{Kind: typeset.CurveTo, P: [3]typeset.Point{pt(cx-rx, cy-float64(k*ry)), pt(cx-float64(k*rx), cy-ry), pt(cx, cy-ry)}},
		{Kind: typeset.CurveTo, P: [3]typeset.Point{pt(cx+float64(k*rx), cy-ry), pt(cx+rx, cy-float64(k*ry)), pt(cx+rx, cy)}},
		{Kind: typeset.ClosePath},
	}
}

// shapeOf returns ops filled with fill and outlined with line, 0.6 of a
// pixel wide, which draws as light as the theme's antialiased outlines;
// a transparent colour leaves its part out.
func shapeOf(ops []typeset.PathOp, fill, line typeset.Color) typeset.Fragment {
	p := &typeset.Path{Ops: ops}
	if fill.A > 0 {
		p.Fill = &typeset.Fill{Color: fill}
	}
	if line.A > 0 {
		p.Stroke = &typeset.Stroke{Color: line, Width: 0.6, Join: typeset.MiterJoin}
	}
	return p
}

// strokeOf returns the open path ops stroked in c, w pixels wide.
func strokeOf(ops []typeset.PathOp, c typeset.Color, w float64) typeset.Fragment {
	return &typeset.Path{Ops: ops, Stroke: &typeset.Stroke{Color: c, Width: w, Cap: typeset.RoundCap, Join: typeset.RoundJoin}}
}

// line returns the path from (x0, y0) to (x1, y1).
func line(x0, y0, x1, y1 float64) []typeset.PathOp {
	return []typeset.PathOp{
		{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: x0, Y: y0}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x1, Y: y1}}},
	}
}

// arrow returns an arrow pointing right turned clockwise by deg degrees
// about the box's centre: 90 points down, -90 up, a diagonal one shrunk
// to fit the box.
func arrow(deg float64, c iconColors) icon {
	return func() []typeset.Fragment {
		ops := poly(0.5, 4.5, 8.5, 4.5, 8.5, 0.5, 15.5, 8, 8.5, 15.5, 8.5, 11.5, 0.5, 11.5)
		rad := deg * math.Pi / 180
		sin, cos := detmath.Sincos(rad)
		k := 1.0
		if math.Abs(sin) > 0.1 && math.Abs(cos) > 0.1 {
			// A diagonal arrow runs from corner to corner.
			k = 1.3
		}
		m := typeset.Matrix{A: k * cos, B: k * sin, C: -k * sin, D: k * cos}
		m.E, m.F = 8-float64(m.A*8)-float64(m.C*8), 8-float64(m.B*8)-float64(m.D*8)
		return []typeset.Fragment{&typeset.Group{Transform: m, Opacity: 1, Content: []typeset.Fragment{shapeOf(ops, c.fill, c.line)}}}
	}
}

// flag returns a flag on a dark pole.
func flag(c iconColors) icon {
	return func() []typeset.Fragment {
		return []typeset.Fragment{
			shapeOf(poly(2, 0, 4, 0, 4, 16, 2, 16), iconBlack.fill, typeset.Color{}),
			shapeOf(poly(4.5, 1.5, 15.5, 1.5, 15.5, 8.5, 4.5, 8.5), c.fill, c.line),
		}
	}
}

// disc returns a disc filling the box.
func disc(c iconColors) icon {
	return func() []typeset.Fragment { return []typeset.Fragment{shapeOf(ellipse(8, 8, 7.5, 7.5), c.fill, c.line)} }
}

// smallDisc returns a disc of half the box.
func smallDisc(c iconColors) icon {
	return func() []typeset.Fragment { return []typeset.Fragment{shapeOf(ellipse(8, 8, 5, 5), c.fill, c.line)} }
}

// lamp returns a coloured disc in a dark square, a traffic light.
func lamp(c typeset.Color) icon {
	return func() []typeset.Fragment {
		return []typeset.Fragment{
			shapeOf(poly(0, 0, 16, 0, 16, 16, 0, 16), iconBlack.fill, typeset.Color{}),
			shapeOf(ellipse(8, 8, 6.5, 6.5), c, typeset.Color{}),
		}
	}
}

// diamond returns a diamond filling the box.
func diamond(c iconColors) icon {
	return func() []typeset.Fragment {
		return []typeset.Fragment{shapeOf(poly(8, 0.5, 15.5, 8, 8, 15.5, 0.5, 8), c.fill, c.line)}
	}
}

// triangle returns an upward triangle filling the box.
func triangle(c iconColors) icon {
	return func() []typeset.Fragment {
		return []typeset.Fragment{shapeOf(poly(8, 1, 15.5, 15.5, 0.5, 15.5), c.fill, c.line)}
	}
}

// pointer returns the small triangle of 3Triangles, pointing up or down.
func pointer(up bool, c iconColors) icon {
	return func() []typeset.Fragment {
		if up {
			return []typeset.Fragment{shapeOf(poly(8, 1.5, 15.5, 8.5, 0.5, 8.5), c.fill, c.line)}
		}
		return []typeset.Fragment{shapeOf(poly(0.5, 7.5, 15.5, 7.5, 8, 14.5), c.fill, c.line)}
	}
}

// dash returns the flat bar of 3Triangles' middle band.
func dash(c iconColors) icon {
	return func() []typeset.Fragment {
		return []typeset.Fragment{shapeOf(poly(0.5, 6.5, 15.5, 6.5, 15.5, 9.5, 0.5, 9.5), c.fill, c.line)}
	}
}

// symbol returns a disc with a white glyph: a cross 'x', an exclamation
// mark '!' or a check mark 'v'.
func symbol(c iconColors, glyph byte) icon {
	return func() []typeset.Fragment {
		out := []typeset.Fragment{shapeOf(ellipse(8, 8, 7.5, 7.5), c.fill, c.line)}
		switch glyph {
		case 'x':
			out = append(out, strokeOf(line(5, 5, 11, 11), iconWhite, 2), strokeOf(line(11, 5, 5, 11), iconWhite, 2))
		case '!':
			out = append(out, strokeOf(line(8, 3.5, 8, 9), iconWhite, 2), strokeOf(line(8, 12, 8, 12.5), iconWhite, 2))
		default:
			out = append(out, strokeOf(append(line(4.5, 8.5, 7, 11), typeset.PathOp{Kind: typeset.LineTo, P: [3]typeset.Point{{X: 11.5, Y: 5}}}), iconWhite, 2))
		}
		return out
	}
}

// face returns a disc with eyes and a mouth that frowns (mood -1), is flat
// (0) or smiles (1).
func face(c iconColors, mood float64) icon {
	return func() []typeset.Fragment {
		feature := c.line
		mouth := []typeset.PathOp{
			{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: 4.5, Y: 10.5}}},
			{Kind: typeset.CurveTo, P: [3]typeset.Point{{X: 6, Y: 10.5 + 2*mood}, {X: 10, Y: 10.5 + 2*mood}, {X: 11.5, Y: 10.5}}},
		}
		return []typeset.Fragment{
			shapeOf(ellipse(8, 8, 7.5, 7.5), c.fill, c.line),
			shapeOf(ellipse(5.5, 5.5, 1, 1), feature, typeset.Color{}),
			shapeOf(ellipse(10.5, 5.5, 1, 1), feature, typeset.Color{}),
			strokeOf(mouth, feature, 1),
		}
	}
}

// star returns a five-pointed star filled for the share full of its width
// from the left, the rest grey.
func star(full float64) icon {
	return func() []typeset.Fragment {
		xy := make([]float64, 0, 20)
		for i := range 10 {
			r := 7.5
			if i%2 == 1 {
				r = 3.2
			}
			a := -math.Pi/2 + float64(i)*math.Pi/5
			xy = append(xy, 8+float64(r*detmath.Cos(a)), 8.4+float64(r*detmath.Sin(a)))
		}
		ops := poly(xy...)
		switch {
		case full <= 0:
			return []typeset.Fragment{shapeOf(ops, iconLightGray.fill, iconBlack.line)}
		case full >= 1:
			return []typeset.Fragment{shapeOf(ops, iconYellow.fill, iconYellow.line)}
		}
		w := 16 * full
		return []typeset.Fragment{
			shapeOf(ops, iconOff.fill, typeset.Color{}),
			&typeset.Group{Clip: &typeset.Path{Ops: poly(0, 0, w, 0, w, 16, 0, 16), Fill: &typeset.Fill{}}, Opacity: 1, Content: []typeset.Fragment{shapeOf(ops, iconYellow.fill, typeset.Color{})}},
			shapeOf(ops, typeset.Color{}, iconYellow.line),
		}
	}
}

// bars returns four bars rising left to right, the first lit ones blue.
func bars(lit int) icon {
	return func() []typeset.Fragment {
		out := make([]typeset.Fragment, 0, 4)
		for i := range 4 {
			c := iconOff
			if i < lit {
				c = iconBlue
			}
			top := 10.5 - float64(3.5*float64(i))
			x := 0.5 + float64(4*float64(i))
			out = append(out, shapeOf(poly(x, top, x+2, top, x+2, 15.5, x, 15.5), c.fill, c.line))
		}
		return out
	}
}

// quarters returns a white disc filled grey by n quarters, clockwise from
// twelve o'clock.
func quarters(n int) icon {
	return func() []typeset.Fragment {
		out := []typeset.Fragment{shapeOf(ellipse(8, 8, 7.5, 7.5), iconWhite, iconBlack.line)}
		switch {
		case n >= 4:
			out = append(out, shapeOf(ellipse(8, 8, 7.5, 7.5), iconGray.fill, iconBlack.line))
		case n > 0:
			clip := map[int][]float64{1: {8, 0, 16, 0, 16, 8, 8, 8}, 2: {8, 0, 16, 0, 16, 16, 8, 16}, 3: {8, 0, 16, 0, 16, 16, 0, 16, 0, 8, 8, 8}}[n]
			pie := []typeset.Fragment{shapeOf(ellipse(8, 8, 7.5, 7.5), iconGray.fill, typeset.Color{})}
			out = append(out, &typeset.Group{Clip: &typeset.Path{Ops: poly(clip...), Fill: &typeset.Fill{}}, Opacity: 1, Content: pie})
		}
		return out
	}
}

// boxes returns four boxes in a square, the first lit ones blue: bottom
// left, bottom right, top left, then top right.
func boxes(lit int) icon {
	return func() []typeset.Fragment {
		at := [4][2]float64{{0.5, 9.5}, {9.5, 9.5}, {0.5, 0.5}, {9.5, 0.5}}
		out := make([]typeset.Fragment, 0, len(at))
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

// The colours of the icons Excel prints, sampled from Excel's prints:
// each a fill and an outline, flat.
var (
	excelRed      = iconColors{fill: rgb(0xd6, 0x55, 0x32), line: rgb(0x9e, 0x38, 0x1c)}
	excelAmber    = iconColors{fill: rgb(0xea, 0xc2, 0x82), line: rgb(0xa4, 0x80, 0x2b)}
	excelGreen    = iconColors{fill: rgb(0x68, 0xa4, 0x90), line: rgb(0x32, 0x6f, 0x5b)}
	excelGray     = iconColors{fill: rgb(0x80, 0x80, 0x80), line: rgb(0x64, 0x62, 0x62)}
	excelOff      = iconColors{fill: rgb(0xb3, 0xb3, 0xb3), line: rgb(0x72, 0x72, 0x72)}
	excelBlue     = iconColors{fill: rgb(0x4a, 0x7d, 0xb1), line: rgb(0x33, 0x5f, 0x8c)}
	excelDark     = iconColors{fill: rgb(0x50, 0x50, 0x50), line: rgb(0x30, 0x30, 0x30)}
	excelSilver   = iconColors{fill: rgb(0xb1, 0xb1, 0xb1), line: rgb(0x87, 0x87, 0x87)}
	excelPink     = iconColors{fill: rgb(0xed, 0xb2, 0xa3), line: rgb(0xc4, 0x92, 0x84)}
	excelBrick    = iconColors{fill: rgb(0xd8, 0x63, 0x44), line: rgb(0xb1, 0x46, 0x29)}
	excelPole     = rgb(0x72, 0x72, 0x72)
	excelCrossRed = iconColors{fill: rgb(0xd8, 0x63, 0x44), line: rgb(0xac, 0x42, 0x26)}
	excelBang     = iconColors{fill: rgb(0xed, 0xc8, 0x7e), line: rgb(0xa4, 0x80, 0x2b)}
	excelTick     = iconColors{fill: rgb(0x76, 0xa7, 0x97), line: rgb(0x3c, 0x77, 0x63)}
)

// excelIconSets are the icons of each icon set as Excel prints them,
// lowest band first: flat icons in Excel's colours, its arrows narrower,
// its flags pennants on a thin pole, its 3Symbols2 bare marks without a
// disc, its stars outlined, its rating a staircase of bars, drawn as
// vectors of the silhouettes of its prints.
//
// # Corpus evidence
//
//   - litho/xlsx: cf_icon_sets prints its 20 icon sets in these colours
//     and shapes, as 24 by 24 pixel pictures of 11 points.
var excelIconSets = map[string][]icon{
	"3Arrows":         {webArrow(90, excelRed), webArrow(0, excelAmber), webArrow(-90, excelGreen)},
	"3ArrowsGray":     {webArrow(90, excelGray), webArrow(0, excelGray), webArrow(-90, excelGray)},
	"3Flags":          {pennant(excelRed), pennant(excelAmber), pennant(excelGreen)},
	"3TrafficLights1": {disc(excelRed), disc(excelAmber), disc(excelGreen)},
	"3TrafficLights2": {roundLamp(excelRed), roundLamp(excelAmber), roundLamp(excelGreen)},
	"3Signs":          {diamond(excelRed), webTriangle(excelAmber), disc(excelGreen)},
	"3Symbols":        {symbol(excelRed, 'x'), symbol(excelAmber, '!'), symbol(excelGreen, 'v')},
	"3Symbols2":       {mark(excelCrossRed, 'x'), mark(excelBang, '!'), mark(excelTick, 'v')},
	"3Smilies":        {face(excelAmber, -1), face(excelAmber, 0), face(excelAmber, 1)},
	"3ColorSmilies":   {face(excelRed, -1), face(excelAmber, 0), face(excelGreen, 1)},
	"3Stars":          {webStar(0), webStar(0.5), webStar(1)},
	"3Triangles":      {webPointer(false, excelRed), webDash(excelAmber), webPointer(true, excelGreen)},
	"4Arrows":         {webArrow(90, excelRed), webArrow(45, excelAmber), webArrow(-45, excelAmber), webArrow(-90, excelGreen)},
	"4ArrowsGray":     {webArrow(90, excelGray), webArrow(45, excelGray), webArrow(-45, excelGray), webArrow(-90, excelGray)},
	"4RedToBlack":     {disc(excelDark), disc(excelSilver), disc(excelPink), disc(excelBrick)},
	"4Rating":         {stairs(1), stairs(2), stairs(3), stairs(4)},
	"4TrafficLights":  {disc(excelDark), disc(excelRed), disc(excelAmber), disc(excelGreen)},
	"5Arrows":         {webArrow(90, excelRed), webArrow(45, excelAmber), webArrow(0, excelAmber), webArrow(-45, excelAmber), webArrow(-90, excelGreen)},
	"5ArrowsGray":     {webArrow(90, excelGray), webArrow(45, excelGray), webArrow(0, excelGray), webArrow(-45, excelGray), webArrow(-90, excelGray)},
	"5Rating":         {stairs(0), stairs(1), stairs(2), stairs(3), stairs(4)},
	"5Quarters":       {webQuarters(0), webQuarters(1), webQuarters(2), webQuarters(3), webQuarters(4)},
	"5Boxes":          {webBoxes(0), webBoxes(1), webBoxes(2), webBoxes(3), webBoxes(4)},
}

// webArrow returns Excel's arrow pointing right turned clockwise by deg
// degrees about the box's centre: a shaft a third of the box wide and a
// head of 12 units.
func webArrow(deg float64, c iconColors) icon {
	return func() []typeset.Fragment {
		ops := poly(0.7, 5.3, 8.7, 5.3, 8.7, 2, 15.3, 8, 8.7, 14, 8.7, 10.7, 0.7, 10.7)
		rad := deg * math.Pi / 180
		sin, cos := detmath.Sincos(rad)
		m := typeset.Matrix{A: cos, B: sin, C: -sin, D: cos}
		m.E, m.F = 8-float64(m.A*8)-float64(m.C*8), 8-float64(m.B*8)-float64(m.D*8)
		return []typeset.Fragment{&typeset.Group{Transform: m, Opacity: 1, Content: []typeset.Fragment{shapeOf(ops, c.fill, c.line)}}}
	}
}

// pennant returns a triangular flag at the top of a thin grey pole.
func pennant(c iconColors) icon {
	return func() []typeset.Fragment {
		return []typeset.Fragment{
			shapeOf(poly(3.3, 0.7, 4.7, 0.7, 4.7, 15.3, 3.3, 15.3), excelPole, typeset.Color{}),
			shapeOf(poly(5, 1.5, 13.3, 4.7, 5, 8), c.fill, c.line),
		}
	}
}

// roundLamp returns a coloured disc in a dark square, Excel's traffic
// light.
func roundLamp(c iconColors) icon {
	return func() []typeset.Fragment {
		return []typeset.Fragment{
			shapeOf(poly(0.7, 0.7, 15.3, 0.7, 15.3, 15.3, 0.7, 15.3), excelDark.fill, excelDark.line),
			shapeOf(ellipse(8, 8, 4.6, 4.6), c.fill, typeset.Color{}),
		}
	}
}

// webTriangle returns Excel's upward warning triangle.
func webTriangle(c iconColors) icon {
	return func() []typeset.Fragment {
		return []typeset.Fragment{shapeOf(poly(8, 1.3, 15.3, 14, 0.7, 14), c.fill, c.line)}
	}
}

// mark returns a bare cross 'x', exclamation mark '!' or check mark 'v'
// without a disc, 3Symbols2's icons as Excel prints them.
func mark(c iconColors, glyph byte) icon {
	return func() []typeset.Fragment {
		switch glyph {
		case 'x':
			return []typeset.Fragment{
				shapeOf(poly(0.7, 3, 3, 0.7, 8, 5.7, 13, 0.7, 15.3, 3, 10.3, 8, 15.3, 13, 13, 15.3, 8, 10.3, 3, 15.3, 0.7, 13, 5.7, 8), c.fill, c.line),
			}
		case '!':
			return []typeset.Fragment{
				shapeOf(poly(6.3, 0.7, 9.7, 0.7, 9, 10.7, 7, 10.7), c.fill, c.line),
				shapeOf(ellipse(8, 13.5, 1.8, 1.8), c.fill, c.line),
			}
		}
		return []typeset.Fragment{shapeOf(poly(0.7, 8.3, 3, 6, 6, 9, 13, 2, 15.3, 4.3, 6, 13.7), c.fill, c.line)}
	}
}

// webStar returns Excel's five-pointed star, white for none, its left
// half amber for a half, amber whole.
func webStar(full float64) icon {
	return func() []typeset.Fragment {
		xy := make([]float64, 0, 20)
		for i := range 10 {
			r := 7.3
			if i%2 == 1 {
				r = 3.3
			}
			a := -math.Pi/2 + float64(i)*math.Pi/5
			xy = append(xy, 8+float64(r*detmath.Cos(a)), 8.4+float64(r*detmath.Sin(a)))
		}
		ops := poly(xy...)
		white := rgb(0xff, 0xff, 0xff)
		switch {
		case full <= 0:
			return []typeset.Fragment{shapeOf(ops, white, excelPole)}
		case full >= 1:
			return []typeset.Fragment{shapeOf(ops, excelAmber.fill, excelAmber.line)}
		}
		w := 16 * full
		return []typeset.Fragment{
			shapeOf(ops, white, typeset.Color{}),
			&typeset.Group{Clip: &typeset.Path{Ops: poly(0, 0, w, 0, w, 16, 0, 16), Fill: &typeset.Fill{}}, Opacity: 1, Content: []typeset.Fragment{shapeOf(ops, excelAmber.fill, typeset.Color{})}},
			shapeOf(ops, typeset.Color{}, excelAmber.line),
		}
	}
}

// webPointer returns 3Triangles' wide triangle as Excel prints it,
// pointing up or down.
func webPointer(up bool, c iconColors) icon {
	return func() []typeset.Fragment {
		if up {
			return []typeset.Fragment{shapeOf(poly(8, 4, 15.3, 12.7, 0.7, 12.7), c.fill, c.line)}
		}
		return []typeset.Fragment{shapeOf(poly(0.7, 3.3, 15.3, 3.3, 8, 12), c.fill, c.line)}
	}
}

// webDash returns the bar of 3Triangles' middle band as Excel prints it.
func webDash(c iconColors) icon {
	return func() []typeset.Fragment {
		return []typeset.Fragment{shapeOf(poly(0.7, 5.3, 15.3, 5.3, 15.3, 10.7, 0.7, 10.7), c.fill, c.line)}
	}
}

// stairs returns Excel's rating: four bars side by side rising left to
// right, the first lit ones blue, the rest grey.
func stairs(lit int) icon {
	return func() []typeset.Fragment {
		out := make([]typeset.Fragment, 0, 4)
		for i := range 4 {
			c := excelOff
			if i < lit {
				c = excelBlue
			}
			x := 0.7 + float64(3.6*float64(i))
			top := 11.3 - float64(3.3*float64(i))
			out = append(out, shapeOf(poly(x, top, x+3.6, top, x+3.6, 15.3, x, 15.3), c.fill, c.line))
		}
		return out
	}
}

// webQuarters returns a white disc filled dark by n quarters, clockwise
// from twelve o'clock, as Excel prints it.
func webQuarters(n int) icon {
	return func() []typeset.Fragment {
		out := []typeset.Fragment{shapeOf(ellipse(8, 8, 7.3, 7.3), rgb(0xff, 0xff, 0xff), excelDark.fill)}
		switch {
		case n >= 4:
			out = append(out, shapeOf(ellipse(8, 8, 7.3, 7.3), excelDark.fill, excelDark.fill))
		case n > 0:
			clip := map[int][]float64{1: {8, 0, 16, 0, 16, 8, 8, 8}, 2: {8, 0, 16, 0, 16, 16, 8, 16}, 3: {8, 0, 16, 0, 16, 16, 0, 16, 0, 8, 8, 8}}[n]
			pie := []typeset.Fragment{shapeOf(ellipse(8, 8, 7.3, 7.3), excelDark.fill, typeset.Color{})}
			out = append(out, &typeset.Group{Clip: &typeset.Path{Ops: poly(clip...), Fill: &typeset.Fill{}}, Opacity: 1, Content: pie})
		}
		return out
	}
}

// webBoxes returns four boxes in a square as Excel prints them, the first
// lit ones blue: bottom left, bottom right, top left, then top right.
func webBoxes(lit int) icon {
	return func() []typeset.Fragment {
		at := [4][2]float64{{0, 8.3}, {8.3, 8.3}, {0, 0.7}, {8.3, 0.7}}
		out := make([]typeset.Fragment, 0, len(at))
		for i, p := range at {
			c := excelOff
			if i < lit {
				c = excelBlue
			}
			out = append(out, shapeOf(poly(p[0], p[1], p[0]+7.3, p[1], p[0]+7.3, p[1]+7.3, p[0], p[1]+7.3), c.fill, c.line))
		}
		return out
	}
}
