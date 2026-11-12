package shape

import (
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/stroke"
	"github.com/gotenberg/litho/pkg/internal/typeset"
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
	// Kind is the arrowhead's outline, [ArrowNone] for none.
	Kind ArrowKind
	// Width and Length are its size across and along the line.
	Width, Length ArrowSize
}

// Line is a stroke as plain values, for [Stroke].
type Line struct {
	// Width is the line width in points; Color its colour.
	Width float64
	Color typeset.Color
	// Dash is the dash array in points, nil for a solid line ([PresetDash]
	// resolves a preset).
	Dash []float64
	// Cap and Join are the line's caps and joins.
	Cap  typeset.LineCap
	Join typeset.LineJoin
	// Head and Tail are the arrowheads at the start of the first subpath
	// and at the end of the last one (20.1.8.38 headEnd, 20.1.8.57
	// tailEnd).
	Head, Tail Arrowhead
}

// presetDashes are the dash and gap lengths of the preset dashes of
// ECMA-376 Part 1, 20.1.10.48 ST_PresetLineDashVal, in line widths, as
// LibreOffice reads them (oox lineproperties.cxx, lclConvertPresetDash).
var presetDashes = map[string][]float64{ // a read-only spec table, ECMA-376 Part 1, 20.1.10.48.
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
func PresetDash(name string, width float64, c typeset.LineCap) []float64 {
	rel, ok := presetDashes[name]
	if !ok {
		return nil
	}
	if !numeric.PositiveFinite(width) {
		width = 1
	}
	out := make([]float64, len(rel))
	for i, v := range rel {
		if c != typeset.ButtCap {
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
// their tip at the line's end, the diamond and the oval their centre, and
// the line stops just short of the arrowhead's base, notch or centre. A closed subpath has no
// arrowheads. A zero width strokes nothing.
//
// Stroke takes no context: it copies ops and reads them in passes linear in
// their length, and makes at most two arrowheads.
func Stroke(ops []typeset.PathOp, l Line) []typeset.Fragment {
	if len(ops) == 0 || !numeric.PositiveFinite(l.Width) {
		return nil
	}
	// A copy, so shortening leaves the caller's outline alone.
	line := slices.Clone(ops)
	var heads []typeset.Fragment
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
	p := &typeset.Path{Ops: line, Stroke: &typeset.Stroke{
		Color: l.Color, Width: l.Width, Cap: l.Cap, Join: l.Join, Dash: l.Dash,
	}}
	return append([]typeset.Fragment{p}, heads...)
}

// startTangent returns the start of the first subpath and the unit
// direction pointing out of the line there, when the subpath is open.
func startTangent(ops []typeset.PathOp) (typeset.Point, typeset.Point, bool) {
	if len(ops) < 2 || ops[0].Kind != typeset.MoveTo {
		return typeset.Point{}, typeset.Point{}, false
	}
	for _, op := range ops[1:] {
		if op.Kind == typeset.MoveTo {
			break
		}
		if op.Kind == typeset.ClosePath {
			return typeset.Point{}, typeset.Point{}, false
		}
	}
	p0 := ops[0].P[0]
	for _, q := range nextPoints(ops[1]) {
		if d, ok := unit(p0.X-q.X, p0.Y-q.Y); ok {
			return p0, d, true
		}
	}
	return typeset.Point{}, typeset.Point{}, false
}

// nextPoints returns the points an operation heads through, in order.
func nextPoints(op typeset.PathOp) []typeset.Point {
	switch op.Kind {
	case typeset.LineTo:
		return op.P[:1]
	case typeset.CurveTo:
		return op.P[:]
	case typeset.MoveTo, typeset.ClosePath:
	}
	return nil
}

// endTangent returns the end of the last subpath and the unit direction
// pointing out of the line there, when the subpath is open.
func endTangent(ops []typeset.PathOp) (typeset.Point, typeset.Point, bool) {
	last := ops[len(ops)-1]
	if last.Kind == typeset.ClosePath || last.Kind == typeset.MoveTo || len(ops) < 2 {
		return typeset.Point{}, typeset.Point{}, false
	}
	var pts []typeset.Point
	prev := ops[len(ops)-2]
	switch prev.Kind {
	case typeset.MoveTo, typeset.LineTo:
		pts = append(pts, prev.P[0])
	case typeset.CurveTo:
		pts = append(pts, prev.P[2])
	case typeset.ClosePath:
	}
	end := last.P[0]
	if last.Kind == typeset.CurveTo {
		end = last.P[2]
		pts = append([]typeset.Point{last.P[1], last.P[0]}, pts...)
	}
	for _, q := range pts {
		if d, ok := unit(end.X-q.X, end.Y-q.Y); ok {
			return end, d, true
		}
	}
	return typeset.Point{}, typeset.Point{}, false
}

// unit returns (x, y) scaled to length 1, false for a null vector.
func unit(x, y float64) (typeset.Point, bool) {
	n := math.Hypot(x, y)
	if n < 1e-9 || !numeric.Finite(n) {
		return typeset.Point{}, false
	}
	return typeset.Point{X: x / n, Y: y / n}, true
}

// shortenStart moves the first point back along the line by d.
func shortenStart(ops []typeset.PathOp, d float64) {
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
	ops[0].P[0] = typeset.Point{X: p0.X + dir.X*d, Y: p0.Y + dir.Y*d}
	if ops[1].Kind == typeset.CurveTo {
		ops[1].P[0] = typeset.Point{X: ops[1].P[0].X + dir.X*d, Y: ops[1].P[0].Y + dir.Y*d}
	}
}

// shortenEnd moves the last point back along the line by d.
func shortenEnd(ops []typeset.PathOp, d float64) {
	if d <= 0 {
		return
	}
	i := len(ops) - 1
	_, dir, ok := endTangent(ops)
	if !ok {
		return
	}
	if ops[i].Kind == typeset.CurveTo {
		ops[i].P[2] = typeset.Point{X: ops[i].P[2].X - dir.X*d, Y: ops[i].P[2].Y - dir.Y*d}
		ops[i].P[1] = typeset.Point{X: ops[i].P[1].X - dir.X*d, Y: ops[i].P[1].Y - dir.Y*d}
		return
	}
	ops[i].P[0] = typeset.Point{X: ops[i].P[0].X - dir.X*d, Y: ops[i].P[0].Y - dir.Y*d}
}

// arrowPolygons are LibreOffice's arrowhead outlines in a 100 by 100
// space, the tip at (50, 0) and the base at y 100 (oox lineproperties.cxx,
// lclPushMarkerProperties); the open arrow's depends on the line width
// and is built by openArrow.
var arrowPolygons = map[ArrowKind][][2]float64{ // a read-only table of LibreOffice's marker outlines.
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
func arrowhead(a Arrowhead, w, base float64, tip, dir typeset.Point, c typeset.Color) (typeset.Fragment, float64) {
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
	ops := make([]typeset.PathOp, 0, len(poly)+1)
	for i, p := range poly {
		across := (p[0] - 50) / 100 * aw
		back := p[1]/100*al - shift
		pt := typeset.Point{X: tip.X - dir.X*back + nx*across, Y: tip.Y - dir.Y*back + ny*across}
		k := typeset.LineTo
		if i == 0 {
			k = typeset.MoveTo
		}
		ops = append(ops, typeset.PathOp{Kind: k, P: [3]typeset.Point{pt}})
	}
	ops = append(ops, typeset.PathOp{Kind: typeset.ClosePath})
	return &typeset.Path{Ops: ops, Fill: &typeset.Fill{Color: c}}, max(0, arrowDock[a.Kind]*al-aw/15)
}

// arrowDock is where the line meets an arrowhead, as a share of its
// length from the line's end: the base of a triangle or an open arrow,
// the notch of a stealth arrow, the centre of a diamond or an oval, which
// sit centred on the end. LibreOffice stops the line a fifteenth of the
// arrowhead's width short of it.
//
// # Corpus evidence
//
//   - probe, pptx: in the pinned image's PDF, a 2.01 pt line under
//     triangles 6.04 and 4.03 pt long and 6.04 and 10.06 pt wide stops
//     5.64 and 3.35 pt from their tips, under stealth arrows 3.23 and
//     1.76 pt, and 2.61 and 1.33 pt short of the centres of diamonds and
//     ovals of the same sizes.
var arrowDock = map[ArrowKind]float64{ // a read-only table of LibreOffice's marker docking.
	ArrowTriangle: 1, ArrowOpen: 1, ArrowStealth: 0.6, ArrowDiamond: 0.5, ArrowOval: 0.5,
}

// strokeTolerance is the flattening tolerance of a stroke outline, as a
// share of the outline's diagonal: fine enough that the curve's facets do
// not show at print sizes.
const strokeTolerance = 1e-4

// GradientStroke returns the fragments that stroke the outline ops with
// the width, dash, cap and join of l, painted with p over box: the
// stroke's own outline filled with p, as a gradient line (ECMA-376 Part
// 1, 20.1.2.2.24 ln with a gradFill) paints. Arrowheads are left out. nil
// when the stroke paints nothing or its outline cannot be built.
func GradientStroke(ops []typeset.PathOp, l Line, p Paint, box typeset.Rect) []typeset.Fragment {
	if len(ops) == 0 || !numeric.PositiveFinite(l.Width) {
		return nil
	}
	var path geom.Path
	pt := func(q typeset.Point) geom.Point { return geom.Point{X: q.X, Y: q.Y} }
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, op := range ops {
		for _, q := range op.P {
			if op.Kind != typeset.ClosePath {
				x0, y0, x1, y1 = math.Min(x0, q.X), math.Min(y0, q.Y), math.Max(x1, q.X), math.Max(y1, q.Y)
			}
		}
		switch op.Kind {
		case typeset.MoveTo:
			path.MoveTo(pt(op.P[0]))
		case typeset.LineTo:
			path.LineTo(pt(op.P[0]))
		case typeset.CurveTo:
			path.CubicTo(pt(op.P[0]), pt(op.P[1]), pt(op.P[2]))
		case typeset.ClosePath:
			path.Close()
		}
	}
	diag := math.Hypot(x1-x0, y1-y0) + l.Width
	if !numeric.PositiveFinite(diag) {
		return nil
	}
	st := stroke.Style{Width: l.Width, Cap: stroke.Cap(l.Cap), Join: stroke.Join(l.Join), MiterLimit: stroke.DefaultMiterLimit, Dash: l.Dash}
	out, err := stroke.Outline(path, st, geom.Identity, diag*strokeTolerance)
	if err != nil {
		return nil
	}
	var res []typeset.PathOp
	i := 0
	next := func() typeset.Point {
		q := out.Pts[i]
		i++
		return typeset.Point{X: q.X, Y: q.Y}
	}
walk:
	for _, op := range out.Ops {
		switch op {
		case geom.MoveTo, geom.LineTo:
			if i >= len(out.Pts) {
				break walk
			}
			k := typeset.MoveTo
			if op == geom.LineTo {
				k = typeset.LineTo
			}
			res = append(res, typeset.PathOp{Kind: k, P: [3]typeset.Point{next()}})
		case geom.CubicTo:
			if i+3 > len(out.Pts) {
				break walk
			}
			a, b := next(), next()
			res = append(res, typeset.PathOp{Kind: typeset.CurveTo, P: [3]typeset.Point{a, b, next()}})
		case geom.Close:
			res = append(res, typeset.PathOp{Kind: typeset.ClosePath})
		}
	}
	if len(res) == 0 {
		return nil
	}
	if p.Kind != PaintGradient {
		return []typeset.Fragment{&typeset.Path{Ops: res, Fill: &typeset.Fill{Color: p.Color, Rule: typeset.NonZero}}}
	}
	stops := normalStops(p.Stops)
	sh := gradientShading(box, stops, p.Angle, p.Radial)
	if sh == nil {
		if len(stops) == 0 {
			return nil
		}
		return []typeset.Fragment{&typeset.Path{Ops: res, Fill: &typeset.Fill{Color: stops[0].Color, Rule: typeset.NonZero}}}
	}
	return []typeset.Fragment{&typeset.Path{Ops: res, Fill: &typeset.Fill{Rule: typeset.NonZero, Shading: sh}}}
}
