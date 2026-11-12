package css

import "math"

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
	circle    bool
	size      RadialSize
	radius    [2]length
	center    [2]length
	stops     []stopSpec
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
}{
	"linear-gradient":           {LinearGradient, false},
	"repeating-linear-gradient": {LinearGradient, true},
	"radial-gradient":           {RadialGradient, false},
	"repeating-radial-gradient": {RadialGradient, true},
}

// bgImageValue parses one background <image> or none: a gradient function
// litho paints, else what [imageValue] takes.
func bgImageValue(t token, c *declCtx, prop string) (imageSpec, bool) {
	if t.typ == tFunction {
		if f, ok := gradientFunctions[lower(t.value)]; ok {
			g, ok := parseGradient(significant(t.children), f.kind, f.repeating)
			if !ok {
				return imageSpec{}, false
			}
			return imageSpec{grad: g}, true
		}
	}
	u, ok := imageValue(t, c, prop)
	return imageSpec{url: u}, ok
}

// parseGradient parses the arguments of a linear or radial gradient
// function (CSS Images 3, 3.1.1 and 3.2.1): an optional first argument
// for the direction or the shape, then the colour stop list.
func parseGradient(args []token, kind GradientKind, repeating bool) (*gradientSpec, bool) {
	parts := splitCommas(args)
	if len(parts) == 0 {
		return nil, false
	}
	g := &gradientSpec{kind: kind, repeating: repeating, angle: 180}
	first := parts[0]
	if kind == LinearGradient {
		if ok, consumed := g.linearDirection(first); consumed {
			if !ok {
				return nil, false
			}
			parts = parts[1:]
		}
	} else {
		g.center = [2]length{pctLength(50), pctLength(50)}
		if ok, consumed := g.radialShape(first); consumed {
			if !ok {
				return nil, false
			}
			parts = parts[1:]
		}
	}
	stops, ok := parseStops(parts)
	if !ok {
		return nil, false
	}
	g.stops = stops
	return g, true
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

// parseStops parses a <color-stop-list> (CSS Images 3, 3.4) with the
// transition hints of CSS Images 4, 3.5.3: stops of a colour and up to two
// positions, a hint a lone position between two stops. One stop is
// enough, as CSS Images 4 allows.
func parseStops(parts [][]token) ([]stopSpec, bool) {
	if len(parts) == 0 || len(parts) > maxGradientStops {
		return nil, false
	}
	var out []stopSpec
	for i, p := range parts {
		if len(p) == 0 {
			return nil, false
		}
		if len(p) == 1 {
			if l, ok := parseLength(p[0], allowPct); ok {
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
			l, ok := parseLength(t, allowPct)
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
		Kind: g.kind, Repeating: g.repeating, Angle: g.angle, Corner: g.corner,
		Circle: g.circle, Size: g.size,
		Stops: make([]GradientStop, len(g.stops)),
	}
	if g.kind == RadialGradient {
		out.Center = [2]Dimension{g.center[0].dim(lc, false), g.center[1].dim(lc, false)}
	}
	if g.size == RadialExplicit {
		out.Radius = [2]Dimension{g.radius[0].dim(lc, true), g.radius[1].dim(lc, true)}
	}
	for i, s := range g.stops {
		st := GradientStop{Hint: s.hint, Position: Dimension{Kind: Auto}}
		if s.hasPos {
			st.Position = s.pos.dim(lc, false)
		}
		if !s.hint {
			st.Color = s.color.c
			if s.color.current {
				st.Color = color
			}
		}
		out.Stops[i] = st
	}
	return out
}
