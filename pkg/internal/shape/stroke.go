package shape

import (
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// ArrowKind is an arrowhead kind (ECMA-376 Part 1, 20.1.10.33
// ST_LineEndType).
type ArrowKind uint8

// The arrowhead kinds.
const (
	ArrowNone ArrowKind = iota
	// ArrowTriangle is a filled triangle (triangle).
	ArrowTriangle
	// ArrowStealth is a triangle notched at its base (stealth).
	ArrowStealth
	// ArrowDiamond is a diamond centred on the line's end (diamond).
	ArrowDiamond
	// ArrowOval is an ellipse centred on the line's end (oval).
	ArrowOval
	// ArrowOpen is an open V of the line's width (arrow).
	ArrowOpen
)

// ArrowSize is an arrowhead's width or length (20.1.10.32
// ST_LineEndWidth, 20.1.10.34 ST_LineEndLength).
type ArrowSize uint8

// The arrowhead sizes; the zero value is medium, the default.
const (
	ArrowMedium ArrowSize = iota
	ArrowSmall
	ArrowLarge
)

// Arrowhead is the arrowhead at one end of a line.
type Arrowhead struct {
	Kind          ArrowKind
	Width, Length ArrowSize
}

// Line is a stroke as plain values, for [Stroke].
type Line struct {
	// Width is the line width in points; Color its colour.
	Width float64
	Color compose.Color
	// Dash is the dash array in points, nil for a solid line ([PresetDash]
	// resolves a preset).
	Dash []float64
	Cap  compose.LineCap
	Join compose.LineJoin
	// Head and Tail are the arrowheads at the start of the first subpath
	// and at the end of the last one (20.1.8.38 headEnd, 20.1.8.57
	// tailEnd).
	Head, Tail Arrowhead
}

// presetDashes are the dash and gap lengths of the preset dashes of
// ECMA-376 Part 1, 20.1.10.48 ST_PresetLineDashVal, in line widths, as
// LibreOffice reads them (oox lineproperties.cxx, lclConvertPresetDash).
var presetDashes = map[string][]float64{ //nolint:gochecknoglobals // a read-only spec table, ECMA-376 Part 1, 20.1.10.48.
	"dot": {1, 3}, "dash": {4, 3}, "dashDot": {4, 3, 1, 3},
	"lgDash": {8, 3}, "lgDashDot": {8, 3, 1, 3}, "lgDashDotDot": {8, 3, 1, 3, 1, 3},
	"sysDot": {1, 1}, "sysDash": {3, 1}, "sysDashDot": {3, 1, 1, 1}, "sysDashDotDot": {3, 1, 1, 1, 1, 1},
}

// PresetDash returns the dash array in points of the preset dash name
// (ECMA-376 Part 1, 20.1.10.48) for a line width wide with the cap c, as
// LibreOffice draws it: each length a multiple of the width, and with a
// round or square cap each dash 0.99 width shorter and each gap as much
// longer, since the caps lengthen the dashes. It returns nil for "solid"
// and an unknown name; a zero width dashes at 1 point.
func PresetDash(name string, width float64, c compose.LineCap) []float64 {
	rel, ok := presetDashes[name]
	if !ok {
		return nil
	}
	if width <= 0 || !finite(width) {
		width = 1
	}
	out := make([]float64, len(rel))
	for i, v := range rel {
		if c != compose.ButtCap {
			if i%2 == 0 {
				v -= 0.99
			} else {
				v += 0.99
			}
		}
		out[i] = v * width
	}
	return out
}

// minArrowBase is the line width an arrowhead is sized from at least,
// 0.7 mm, as LibreOffice sizes one (oox lineproperties.cxx,
// lclPushMarkerProperties: 70 hundredths of a millimetre).
const minArrowBase = 70.0 / hundredthMMPerPoint

// arrowFactor returns the multiple of the base width an arrowhead size
// makes, the open arrow's a half larger.
func arrowFactor(s ArrowSize, open bool) float64 {
	f := 3.0
	switch s {
	case ArrowSmall:
		f = 2
	case ArrowLarge:
		f = 5
	case ArrowMedium:
	}
	if open {
		f += 0.5
	}
	return f
}

// Stroke returns the fragments that stroke the outline ops with l: the
// line with its width, colour, dash, cap and join, and its arrowheads as
// filled outlines, as LibreOffice draws a DrawingML line (oox
// lineproperties.cxx): an arrowhead is as wide as 2, 3 or 5 times the
// line width (2.5, 3.5, 5.5 for the open arrow), at least 0.7 mm, and as
// long by the same factors; the triangle, stealth and open arrow have
// their tip at the line's end and the line stops halfway into them; the
// diamond and the oval are centred on the end. A closed subpath has no
// arrowheads. A zero width strokes nothing.
func Stroke(ops []compose.PathOp, l Line) []compose.Fragment {
	if len(ops) == 0 || l.Width <= 0 || !finite(l.Width) {
		return nil
	}
	// A copy, so shortening leaves the caller's outline alone.
	line := slices.Clone(ops)
	var heads []compose.Fragment
	base := math.Max(l.Width, minArrowBase)
	if l.Head.Kind != ArrowNone {
		if tip, dir, ok := startTangent(line); ok {
			frag, back := arrowhead(l.Head, l.Width, base, tip, dir, l.Color)
			heads = append(heads, frag)
			shortenStart(line, back)
		}
	}
	if l.Tail.Kind != ArrowNone {
		if tip, dir, ok := endTangent(line); ok {
			frag, back := arrowhead(l.Tail, l.Width, base, tip, dir, l.Color)
			heads = append(heads, frag)
			shortenEnd(line, back)
		}
	}
	p := &compose.Path{Ops: line, Stroke: &compose.Stroke{
		Color: l.Color, Width: l.Width, Cap: l.Cap, Join: l.Join, Dash: l.Dash,
	}}
	return append([]compose.Fragment{p}, heads...)
}

// startTangent returns the start of the first subpath and the unit
// direction pointing out of the line there, when the subpath is open.
func startTangent(ops []compose.PathOp) (compose.Point, compose.Point, bool) {
	if len(ops) < 2 || ops[0].Kind != compose.MoveTo {
		return compose.Point{}, compose.Point{}, false
	}
	for _, op := range ops[1:] {
		if op.Kind == compose.MoveTo {
			break
		}
		if op.Kind == compose.ClosePath {
			return compose.Point{}, compose.Point{}, false
		}
	}
	p0 := ops[0].P[0]
	for _, q := range nextPoints(ops[1]) {
		if d, ok := unit(p0.X-q.X, p0.Y-q.Y); ok {
			return p0, d, true
		}
	}
	return compose.Point{}, compose.Point{}, false
}

// nextPoints returns the points an operation heads through, in order.
func nextPoints(op compose.PathOp) []compose.Point {
	switch op.Kind {
	case compose.LineTo:
		return op.P[:1]
	case compose.CurveTo:
		return op.P[:]
	case compose.MoveTo, compose.ClosePath:
	}
	return nil
}

// endTangent returns the end of the last subpath and the unit direction
// pointing out of the line there, when the subpath is open.
func endTangent(ops []compose.PathOp) (compose.Point, compose.Point, bool) {
	last := ops[len(ops)-1]
	if last.Kind == compose.ClosePath || last.Kind == compose.MoveTo || len(ops) < 2 {
		return compose.Point{}, compose.Point{}, false
	}
	var pts []compose.Point
	prev := ops[len(ops)-2]
	switch prev.Kind {
	case compose.MoveTo, compose.LineTo:
		pts = append(pts, prev.P[0])
	case compose.CurveTo:
		pts = append(pts, prev.P[2])
	case compose.ClosePath:
	}
	end := last.P[0]
	if last.Kind == compose.CurveTo {
		end = last.P[2]
		pts = append([]compose.Point{last.P[1], last.P[0]}, pts...)
	}
	for _, q := range pts {
		if d, ok := unit(end.X-q.X, end.Y-q.Y); ok {
			return end, d, true
		}
	}
	return compose.Point{}, compose.Point{}, false
}

// unit returns (x, y) scaled to length 1, false for a null vector.
func unit(x, y float64) (compose.Point, bool) {
	n := math.Hypot(x, y)
	if n < 1e-9 || !finite(n) {
		return compose.Point{}, false
	}
	return compose.Point{X: x / n, Y: y / n}, true
}

// shortenStart moves the first point back along the line by d.
func shortenStart(ops []compose.PathOp, d float64) {
	if d <= 0 {
		return
	}
	p0 := ops[0].P[0]
	q := nextPoints(ops[1])
	if len(q) == 0 {
		return
	}
	dir, ok := unit(q[0].X-p0.X, q[0].Y-p0.Y)
	if !ok {
		return
	}
	d = math.Min(d, math.Hypot(q[len(q)-1].X-p0.X, q[len(q)-1].Y-p0.Y))
	ops[0].P[0] = compose.Point{X: p0.X + dir.X*d, Y: p0.Y + dir.Y*d}
	if ops[1].Kind == compose.CurveTo {
		ops[1].P[0] = compose.Point{X: ops[1].P[0].X + dir.X*d, Y: ops[1].P[0].Y + dir.Y*d}
	}
}

// shortenEnd moves the last point back along the line by d.
func shortenEnd(ops []compose.PathOp, d float64) {
	if d <= 0 {
		return
	}
	i := len(ops) - 1
	_, dir, ok := endTangent(ops)
	if !ok {
		return
	}
	if ops[i].Kind == compose.CurveTo {
		ops[i].P[2] = compose.Point{X: ops[i].P[2].X - dir.X*d, Y: ops[i].P[2].Y - dir.Y*d}
		ops[i].P[1] = compose.Point{X: ops[i].P[1].X - dir.X*d, Y: ops[i].P[1].Y - dir.Y*d}
		return
	}
	ops[i].P[0] = compose.Point{X: ops[i].P[0].X - dir.X*d, Y: ops[i].P[0].Y - dir.Y*d}
}

// arrowPolygons are LibreOffice's arrowhead outlines in a 100 by 100
// space, the tip at (50, 0) and the base at y 100 (oox lineproperties.cxx,
// lclPushMarkerProperties); the open arrow's depends on the line width
// and is built by openArrow.
var arrowPolygons = map[ArrowKind][][2]float64{ //nolint:gochecknoglobals // a read-only table of LibreOffice's marker outlines.
	ArrowTriangle: {{50, 0}, {100, 100}, {0, 100}},
	ArrowStealth:  {{50, 0}, {100, 100}, {50, 60}, {0, 100}},
	ArrowDiamond:  {{50, 0}, {100, 50}, {50, 100}, {0, 50}},
	ArrowOval: {
		{50, 0},
		{75, 7},
		{93, 25},
		{100, 50},
		{93, 75},
		{75, 93},
		{50, 100},
		{25, 93},
		{7, 75},
		{0, 50},
		{7, 25},
		{25, 7},
	},
}

// openArrow returns the open arrow's outline for a line w wide, its
// arrowhead aw wide (oox lineproperties.cxx, XML_arrow).
func openArrow(w, aw float64) [][2]float64 {
	h := math.Max(100*0.5*w/aw, 1)
	return [][2]float64{
		{50, 0},
		{100, 100 - h*1.5},
		{100 - h*1.5, 100},
		{50 + h, 5.5 * h},
		{50 + h, 100},
		{50 - h, 100},
		{50 - h, 5.5 * h},
		{h * 1.5, 100},
		{0, 100 - h*1.5},
	}
}

// arrowhead returns the filled arrowhead a at tip pointing along dir, and
// how far the line is shortened under it.
func arrowhead(a Arrowhead, w, base float64, tip, dir compose.Point, c compose.Color) (compose.Fragment, float64) {
	open := a.Kind == ArrowOpen
	aw := arrowFactor(a.Width, open) * base
	al := arrowFactor(a.Length, open) * base
	poly := arrowPolygons[a.Kind]
	if open {
		poly = openArrow(w, aw)
	}
	centred := a.Kind == ArrowDiamond || a.Kind == ArrowOval
	// The outline's y runs back from the tip along -dir; x across it.
	nx, ny := -dir.Y, dir.X
	shift := 0.0
	if centred {
		shift = al / 2
	}
	ops := make([]compose.PathOp, 0, len(poly)+1)
	for i, p := range poly {
		across := (p[0] - 50) / 100 * aw
		back := p[1]/100*al - shift
		pt := compose.Point{X: tip.X - dir.X*back + nx*across, Y: tip.Y - dir.Y*back + ny*across}
		k := compose.LineTo
		if i == 0 {
			k = compose.MoveTo
		}
		ops = append(ops, compose.PathOp{Kind: k, P: [3]compose.Point{pt}})
	}
	ops = append(ops, compose.PathOp{Kind: compose.ClosePath})
	back := al / 2
	if centred {
		back = 0
	}
	return &compose.Path{Ops: ops, Fill: &compose.Fill{Color: c}}, back
}
