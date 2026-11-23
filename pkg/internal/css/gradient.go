package css

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// maxGradientStops caps the comma-separated stops and hints of one
// gradient: 256, a stop with two positions counting once.
// Anti-DoS guard, not spec-mandated: CSS Images 3, 3.4 sets no bound, and
// real gradients hold two to a dozen stops.
const maxGradientStops = 256

// gradientSpec is a specified <gradient> (CSS Images 3, 3).
type gradientSpec struct {
	kind      GradientKind
	repeating bool
	angle     float64
	corner    [2]int8
	// diagonal runs a corner gradient from corner to corner, as a
	// prefixed legacy gradient does, where "to <corner>" makes its line
	// perpendicular to the other diagonal.
	diagonal bool
	circle   bool
	size     RadialSize
	radius   [2]length
	center   [2]length
	stops    []stopSpec
	// space and hue are the <color-interpolation-method>, method set
	// when one is written.
	space  colorSpace
	hue    hueMethod
	method bool
}

// stopSpec is a specified colour stop or transition hint.
type stopSpec struct {
	color  colorValue
	pos    length
	hasPos bool
	hint   bool
}

// imageSpec is a specified background <image>: a URL, "" for none, or a
// gradient.
type imageSpec struct {
	url  string
	grad *gradientSpec
}

// gradientFunctions maps the gradient functions litho paints to their kind
// and whether they repeat.
var gradientFunctions = map[string]struct {
	kind      GradientKind
	repeating bool
	legacy    bool
}{
	"linear-gradient":                   {LinearGradient, false, false},
	"repeating-linear-gradient":         {LinearGradient, true, false},
	"radial-gradient":                   {RadialGradient, false, false},
	"repeating-radial-gradient":         {RadialGradient, true, false},
	"conic-gradient":                    {ConicGradient, false, false},
	"repeating-conic-gradient":          {ConicGradient, true, false},
	"-webkit-linear-gradient":           {LinearGradient, false, true},
	"-webkit-repeating-linear-gradient": {LinearGradient, true, true},
	"-webkit-radial-gradient":           {RadialGradient, false, true},
	"-webkit-repeating-radial-gradient": {RadialGradient, true, true},
}

// bgImageValue parses one background <image> or none: a gradient function
// litho paints, else what [imageValue] takes.
func bgImageValue(t token, c *declCtx, prop string) (imageSpec, bool) {
	if isImageSet(t) {
		chosen, ok := imageSetChoice(t)
		if !ok {
			return imageSpec{}, false
		}
		if chosen.typ == tString {
			return imageSpec{url: resolveURL(c.baseURL, chosen.value)}, true
		}
		return bgImageValue(chosen, c, prop)
	}
	if t.typ == tFunction {
		if f, ok := gradientFunctions[lower(t.value)]; ok {
			parse := parseGradient
			if f.legacy {
				parse = parseLegacyGradient
			}
			g, ok := parse(significant(t.children), f.kind, f.repeating)
			if !ok {
				return imageSpec{}, false
			}
			return imageSpec{grad: g}, true
		}
		if g, ok := colorImage(t); ok {
			return imageSpec{grad: g}, true
		}
	}
	u, ok := imageValue(t, c, prop)
	return imageSpec{url: u}, ok
}

// colorImage parses image(<color>) (CSS Images 4, 2.5): a solid-colour
// image with no natural dimensions, which paints as a gradient whose two
// stops are that colour. image() naming a source is not painted: it falls
// to [imageValue], which warns.
func colorImage(t token) (*gradientSpec, bool) {
	if !eqFold(t.value, "image") {
		return nil, false
	}
	c, ok := single(significant(t.children))
	if !ok {
		return nil, false
	}
	if _, ok := parseColor(c); !ok {
		return nil, false
	}
	stops := []token{c, {typ: tComma}, c}
	return parseGradient(stops, LinearGradient, false)
}

// parseGradient parses the arguments of a linear, radial or conic
// gradient function (CSS Images 3, 3.1.1 and 3.2.1; CSS Images 4, 3.1,
// 3.2 and 3.3): an optional first argument for the direction, the shape
// or the from angle and centre, and the colour interpolation method, then
// the colour stop list.
func parseGradient(args []token, kind GradientKind, repeating bool) (*gradientSpec, bool) {
	parts := splitCommas(args)
	if len(parts) == 0 {
		return nil, false
	}
	g := &gradientSpec{kind: kind, repeating: repeating, angle: 180}
	first, method, ok := g.takeInterpolation(parts[0])
	if !ok {
		return nil, false
	}
	if method && len(first) == 0 {
		parts = parts[1:]
	}
	switch kind {
	case LinearGradient:
		if ok, consumed := g.linearDirection(first); consumed || method && len(first) > 0 {
			if !ok {
				return nil, false
			}
			parts = parts[1:]
		}
	case RadialGradient:
		g.center = [2]length{pctLength(50), pctLength(50)}
		if ok, consumed := g.radialShape(first); consumed || method && len(first) > 0 {
			if !ok {
				return nil, false
			}
			parts = parts[1:]
		}
	case ConicGradient:
		g.angle = 0
		g.center = [2]length{pctLength(50), pctLength(50)}
		if ok, consumed := g.conicStart(first); consumed || method && len(first) > 0 {
			if !ok {
				return nil, false
			}
			parts = parts[1:]
		}
	}
	stops, ok := parseStops(parts, kind == ConicGradient)
	if !ok {
		return nil, false
	}
	g.stops = stops
	return g, true
}

// parseLegacyGradient parses the arguments of a prefixed legacy linear or
// radial gradient, the syntax of the CSS Images 3 drafts before the "to"
// keyword and the "at" clause. No current standard defines it, and the
// Compatibility Standard lists no such function: the reading below is
// Chromium's, measured.
//
//   - -webkit-linear-gradient([<angle> | <side-or-corner>]?, <stops>):
//     the angle runs counterclockwise from the right, 90deg pointing up,
//     and a side or corner names where the line starts, without "to"; a
//     corner line runs from that corner to the opposite one.
//   - -webkit-radial-gradient([<position>,]? [<shape> || <size>,]?
//     <stops>): the centre first, without "at", then the shape and its
//     size, an extent keyword, contain for closest-side, cover for
//     farthest-corner, or two lengths.
//
// A colour interpolation method may lead the first argument, as Chromium
// reads it there.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 prints -webkit-linear-gradient(left top,
//     red, blue) on a 120 by 60 box as an axial shading from 0 0 to 120 60,
//     where linear-gradient(to right bottom, red, blue) runs from 36 -18 to
//     84 78, and -webkit-linear-gradient(30deg, red, blue) from 2.01 63.48
//     to 117.99 -3.48, the line of linear-gradient(60deg, red, blue). It
//     draws (20px 30px, red, blue) centred at 20px 30px, (center, 20px
//     30px, red, blue) as an ellipse of those radii, (left top, circle
//     contain, red, blue) all blue, and nothing for (to right, red, blue)
//     or (circle at center, red, blue).
func parseLegacyGradient(args []token, kind GradientKind, repeating bool) (*gradientSpec, bool) {
	parts := splitCommas(args)
	if len(parts) == 0 {
		return nil, false
	}
	g := &gradientSpec{kind: kind, repeating: repeating, angle: 180}
	first, method, ok := g.takeInterpolation(parts[0])
	if !ok {
		return nil, false
	}
	if method {
		if len(first) > 0 {
			return nil, false
		}
		parts = parts[1:]
		if len(parts) == 0 {
			return nil, false
		}
		first = parts[0]
	}
	switch kind { //nolint:exhaustive // no conic gradient has a prefixed form
	case LinearGradient:
		if ok, consumed := g.legacyDirection(first); consumed {
			if !ok {
				return nil, false
			}
			parts = parts[1:]
		}
	case RadialGradient:
		g.center = [2]length{pctLength(50), pctLength(50)}
		if len(parts) > 1 && legacyPosition(parts[0]) {
			p, ok := parsePosition(parts[0])
			if !ok {
				return nil, false
			}
			g.center = p
			parts = parts[1:]
		}
		if len(parts) > 1 {
			if ok, consumed := g.legacyShape(parts[0]); consumed {
				if !ok {
					return nil, false
				}
				parts = parts[1:]
			}
		}
	}
	stops, ok := parseStops(parts, false)
	if !ok {
		return nil, false
	}
	g.stops = stops
	return g, true
}

// legacyDirection parses the first argument of a prefixed legacy linear
// gradient: an angle counterclockwise from the right, or the side or
// corner the line starts at. consumed is false when v is not a direction,
// so that it is the first colour stop.
func (g *gradientSpec) legacyDirection(v []token) (ok, consumed bool) {
	if len(v) == 0 {
		return false, false
	}
	if a, isAngle := gradientAngle(v[0]); isAngle {
		if len(v) != 1 {
			return false, true
		}
		g.angle = math.Mod(450-a, 360)
		return true, true
	}
	var h, vert int8
	for i, t := range v {
		if t.typ != tIdent {
			if i == 0 {
				return false, false
			}
			return false, true
		}
		switch k := lower(t.value); k {
		case "left", "right":
			if h != 0 {
				return false, true
			}
			h = 1
			if k == "right" {
				h = -1
			}
		case "top", "bottom":
			if vert != 0 {
				return false, true
			}
			vert = 1
			if k == "bottom" {
				vert = -1
			}
		default:
			if i == 0 && k != "to" {
				return false, false
			}
			return false, true
		}
	}
	switch {
	case h != 0 && vert != 0:
		g.corner, g.diagonal = [2]int8{h, vert}, true
	case h > 0:
		g.angle = 90
	case h < 0:
		g.angle = 270
	case vert > 0:
		g.angle = 180
	default:
		g.angle = 0
	}
	return true, true
}

// legacyPosition reports whether the argument v of a prefixed legacy
// radial gradient is a centre: positions alone, no colour.
func legacyPosition(v []token) bool {
	if len(v) == 0 || len(v) > 4 {
		return false
	}
	for _, t := range v {
		if !isPositionToken(t) {
			return false
		}
	}
	return true
}

// legacyExtents are the size keywords of a prefixed legacy radial
// gradient: the extents, with contain and cover for closest-side and
// farthest-corner.
var legacyExtents = map[string]RadialSize{
	"closest-side": ClosestSide, "closest-corner": ClosestCorner,
	"farthest-side": FarthestSide, "farthest-corner": FarthestCorner,
	"contain": ClosestSide, "cover": FarthestCorner,
}

// legacyShape parses the shape and size argument of a prefixed legacy
// radial gradient: <shape> || <extent keyword>, or two lengths, an
// ellipse's radii. consumed is false when v is neither, so that it is a
// colour stop.
func (g *gradientSpec) legacyShape(v []token) (ok, consumed bool) {
	if len(v) == 2 && v[0].typ != tIdent && v[1].typ != tIdent {
		a, okA := parseLength(v[0], allowPct|nonNeg)
		b, okB := parseLength(v[1], allowPct|nonNeg)
		if !okA || !okB {
			return false, false
		}
		g.size, g.radius = RadialExplicit, [2]length{a, b}
		return true, true
	}
	shape, extent := "", false
	for _, t := range v {
		if t.typ != tIdent {
			return false, shape != "" || extent
		}
		k := lower(t.value)
		switch e, isExtent := legacyExtents[k]; {
		case k == "circle" || k == "ellipse":
			if shape != "" {
				return false, true
			}
			shape = k
		case isExtent:
			if extent {
				return false, true
			}
			extent, g.size = true, e
		default:
			return false, shape != "" || extent
		}
	}
	g.circle = shape == "circle"
	return len(v) > 0, len(v) > 0
}

// linearDirection parses <angle> or to <side-or-corner>. consumed is false
// when v is not a direction at all, so that it is the first colour stop.
func (g *gradientSpec) linearDirection(v []token) (ok, consumed bool) {
	if len(v) == 0 {
		return false, false
	}
	if a, isAngle := gradientAngle(v[0]); isAngle {
		if len(v) != 1 {
			return false, true
		}
		g.angle = a
		return true, true
	}
	if !v[0].isIdent("to") {
		return false, false
	}
	if len(v) < 2 || len(v) > 3 {
		return false, true
	}
	var h, vert int8
	for _, t := range v[1:] {
		if t.typ != tIdent {
			return false, true
		}
		switch lower(t.value) {
		case "left", "right":
			if h != 0 {
				return false, true
			}
			h = -1
			if lower(t.value) == "right" {
				h = 1
			}
		case "top", "bottom":
			if vert != 0 {
				return false, true
			}
			vert = -1
			if lower(t.value) == "bottom" {
				vert = 1
			}
		default:
			return false, true
		}
	}
	switch {
	case h != 0 && vert != 0:
		g.corner = [2]int8{h, vert}
	case h != 0:
		if h < 0 {
			g.angle = 270
		} else {
			g.angle = 90
		}
	case vert < 0:
		g.angle = 0
	default:
		g.angle = 180
	}
	return true, true
}

// gradientAngle parses a gradient's <angle> into degrees in [0, 360): an
// angle unit, or a bare zero (CSS Images 3, 3.1.1 keeps unitless zero for
// compatibility; CSS Values 4, 7.1).
func gradientAngle(t token) (float64, bool) {
	if t.typ == tNumber && t.num != 0 {
		return 0, false
	}
	deg, ok := parseAngle(t)
	if !ok {
		return 0, false
	}
	deg = math.Mod(deg, 360)
	if deg < 0 {
		deg += 360
	}
	return deg, true
}

// radialExtents are the <radial-extent> keywords (CSS Images 3, 3.2.1).
var radialExtents = map[string]RadialSize{
	"closest-side": ClosestSide, "closest-corner": ClosestCorner,
	"farthest-side": FarthestSide, "farthest-corner": FarthestCorner,
}

// radialShape parses [ <ending-shape> || <size> ]? [ at <position> ]?.
// consumed is false when v is not a shape clause, so that it is the first
// colour stop.
func (g *gradientSpec) radialShape(v []token) (ok, consumed bool) {
	at := -1
	for i, t := range v {
		if t.isIdent("at") {
			at = i
			break
		}
	}
	head := v
	if at >= 0 {
		head = v[:at]
		pos := v[at+1:]
		for _, t := range pos {
			if t.typ == tIdent && !isPositionToken(t) {
				return false, true
			}
		}
		p, ok := parsePosition(pos)
		if !ok {
			return false, true
		}
		g.center = p
	}
	var shape string
	var lens []length
	extent := false
	for _, t := range head {
		if t.typ == tIdent {
			k := lower(t.value)
			if k == "circle" || k == "ellipse" {
				if shape != "" {
					return false, true
				}
				shape = k
				continue
			}
			if e, isExtent := radialExtents[k]; isExtent {
				if extent || len(lens) > 0 {
					return false, true
				}
				extent, g.size = true, e
				continue
			}
			if at < 0 {
				return false, false
			}
			return false, true
		}
		l, isLen := parseLength(t, allowPct|nonNeg)
		if !isLen || extent || len(lens) == 2 {
			if at < 0 && shape == "" && !extent && len(lens) == 0 {
				return false, false
			}
			return false, true
		}
		lens = append(lens, l)
	}
	if at < 0 && shape == "" && !extent && len(lens) == 0 {
		return false, false
	}
	switch len(lens) {
	case 1:
		// CSS Images 3, 3.2.1: a single length is a circle's radius and
		// takes no percentage.
		if shape == "ellipse" || lens[0].hasPct {
			return false, true
		}
		shape = "circle"
		g.size, g.radius = RadialExplicit, [2]length{lens[0], lens[0]}
	case 2:
		if shape == "circle" {
			return false, true
		}
		shape = "ellipse"
		g.size, g.radius = RadialExplicit, [2]length{lens[0], lens[1]}
	}
	g.circle = shape == "circle"
	return true, true
}

// takeInterpolation removes a <color-interpolation-method> (CSS Color 4,
// 12.1) from the first argument of a gradient, where CSS Images 4, 3.1
// to 3.3 allow it before or after the rest, and records it in g. method
// reports one was there.
//
// The colours interpolate in that space, as CSS Images 4, 3.5.2 asks and
// Chromium paints on screen; Chromium's PDF writes every gradient
// interpolated in sRGB instead, a function of its PDF backend that
// litho does not copy.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 prints linear-gradient(in oklch, red,
//     blue), linear-gradient(in hsl longer hue, red, blue) and
//     linear-gradient(red, blue) as one type 2 shading from 1 0 0 to 0 0 1,
//     and linear-gradient(lab(50 80 60), lab(50 -40 -60)) from 0.938 0
//     0.0806 to 0 0.5476 0.8687, the two colours clipped to sRGB.
func (g *gradientSpec) takeInterpolation(v []token) (rest []token, method, ok bool) {
	for i, t := range v {
		if !t.isIdent("in") {
			continue
		}
		n := 2
		if i+3 < len(v) && v[i+3].isIdent("hue") {
			n = 4
		}
		if i+n > len(v) {
			return nil, false, false
		}
		space, hue, ok := parseInterpolation(v[i : i+n])
		if !ok {
			return nil, false, false
		}
		g.space, g.hue, g.method = space, hue, true
		rest = append(append(rest, v[:i]...), v[i+n:]...)
		return rest, true, true
	}
	return v, false, true
}

// conicStart parses [ from <angle> ]? [ at <position> ]? (CSS Images 4,
// 3.3.1). consumed is false when v is neither, so that it is the first
// colour stop.
func (g *gradientSpec) conicStart(v []token) (ok, consumed bool) {
	if len(v) == 0 || !v[0].isIdent("from") && !v[0].isIdent("at") {
		return false, false
	}
	if v[0].isIdent("from") {
		if len(v) < 2 {
			return false, true
		}
		a, ok := parseAngle(v[1])
		if !ok || v[1].typ == tNumber && v[1].num != 0 {
			return false, true
		}
		a = math.Mod(a, 360)
		if a < 0 {
			a += 360
		}
		g.angle = a
		v = v[2:]
	}
	if len(v) == 0 {
		return true, true
	}
	if !v[0].isIdent("at") {
		return false, true
	}
	pos := v[1:]
	for _, t := range pos {
		if t.typ == tIdent && !isPositionToken(t) {
			return false, true
		}
	}
	p, ok := parsePosition(pos)
	if !ok {
		return false, true
	}
	g.center = p
	return true, true
}

// stopPosition parses a stop or hint position: a <length-percentage>, or
// for a conic gradient an <angle-percentage> (CSS Images 4, 3.3.2), kept
// as the fraction of a turn it is, in percent.
func stopPosition(t token, conic bool) (length, bool) {
	if !conic {
		return parseLength(t, allowPct)
	}
	deg := 0.0
	switch t.typ { //nolint:exhaustive // the other token types are not positions
	case tPercentage:
		return pctLength(t.num), numeric.Finite(t.num)
	case tNumber:
		if t.num != 0 {
			return length{}, false
		}
	case tDimension:
		v, ok := parseAngle(t)
		if !ok {
			return length{}, false
		}
		deg = v
	case tFunction:
		if !isCalc(&t) {
			return length{}, false
		}
		v, ok := calcAnglePercent(&t)
		if !ok {
			return length{}, false
		}
		deg = v
	default:
		return length{}, false
	}
	return pctLength(deg / 360 * 100), true
}

// parseStops parses a <color-stop-list> (CSS Images 3, 3.4) with the
// transition hints of CSS Images 4, 3.5.3: stops of a colour and up to two
// positions, a hint a lone position between two stops. One stop is
// enough, as CSS Images 4 allows. conic takes angular positions.
func parseStops(parts [][]token, conic bool) ([]stopSpec, bool) {
	if len(parts) == 0 || len(parts) > maxGradientStops {
		return nil, false
	}
	var out []stopSpec
	for i, p := range parts {
		if len(p) == 0 {
			return nil, false
		}
		if len(p) == 1 {
			if l, ok := stopPosition(p[0], conic); ok {
				// A hint sits between two stops, never next to another.
				if i == 0 || i == len(parts)-1 || out[len(out)-1].hint {
					return nil, false
				}
				out = append(out, stopSpec{pos: l, hasPos: true, hint: true})
				continue
			}
		}
		col, ok := parseColor(p[0])
		if !ok || len(p) > 3 {
			return nil, false
		}
		if len(p) == 1 {
			out = append(out, stopSpec{color: col})
			continue
		}
		for _, t := range p[1:] {
			l, ok := stopPosition(t, conic)
			if !ok {
				return nil, false
			}
			out = append(out, stopSpec{color: col, pos: l, hasPos: true})
		}
	}
	return out, true
}

// compute returns the computed gradient: lengths in points, percentages
// kept, currentColor resolved to color.
func (g *gradientSpec) compute(lc lenCtx, color Color) *Gradient {
	out := &Gradient{
		Kind: g.kind, Repeating: g.repeating, Angle: g.angle, Corner: g.corner, Diagonal: g.diagonal,
		Circle: g.circle, Size: g.size,
		Stops: make([]GradientStop, len(g.stops)),
	}
	if g.kind != LinearGradient {
		out.Center = [2]Dimension{g.center[0].dim(lc, false), g.center[1].dim(lc, false)}
	}
	if g.size == RadialExplicit {
		out.Radius = [2]Dimension{g.radius[0].dim(lc, true), g.radius[1].dim(lc, true)}
	}
	// CSS Color 4, 13.3: without a method, legacy sRGB colours
	// interpolate in sRGB, any other in Oklab.
	out.space, out.hue = g.space, g.hue
	if !g.method {
		out.space = spaceSRGB
		for _, s := range g.stops {
			if !s.hint && !s.color.legacy {
				out.space = spaceOKLab
			}
		}
	}
	out.interpolated = out.space != spaceSRGB
	for i, s := range g.stops {
		st := GradientStop{Hint: s.hint, Position: Dimension{Kind: Auto}}
		if s.hasPos {
			st.Position = s.pos.dim(lc, false)
		}
		if !s.hint {
			st.Color = s.color.resolve(color, lc.dark)
			st.sc = s.color.space(color, lc.dark)
			if st.sc.none != [4]bool{} {
				out.interpolated = true
			}
		}
		out.Stops[i] = st
	}
	return out
}
