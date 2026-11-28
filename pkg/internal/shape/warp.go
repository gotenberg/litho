package shape

//go:generate go run ../../../cmd/gen-text-warps -out textwarps_gen.go

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TextWarp is the shape a body's text is warped to: a preset text warp of
// DrawingML by name with its adjustments (ECMA-376 Part 1, 20.1.9.19
// prstTxWarp, 20.1.10.76 ST_TextShapeType), or a custom geometry whose
// paths the text follows, as a VML text path or an OpenDocument Fontwork
// shape gives it.
type TextWarp struct {
	// Preset is the ST_TextShapeType name ("textArchUp", "textInflate",
	// ...); "textNoShape" and a name the table lacks warp nothing.
	Preset string
	// Adjust are the adjustment values the file sets ("adj", "adj1",
	// "adj2"), in the preset's units.
	Adjust []Adjust
	// Custom, when not nil, is the geometry whose paths the text follows,
	// which overrides Preset.
	Custom *Custom
}

// WarpLane is where one line of a warped text goes: between two paths,
// or along one.
type WarpLane struct {
	// Top and Bottom are the paths the line's ink box is stretched
	// between, its top edge onto Top and its bottom edge onto Bottom.
	// Bottom is empty for a lane of one path, which the line's glyphs
	// follow upright on its side ([WarpCurve.Empty]).
	Top, Bottom WarpCurve
}

// Envelope reports whether the lane stretches a line between two paths.
func (l *WarpLane) Envelope() bool { return !l.Bottom.Empty() }

// Map returns where the point at the fractions u across and v down a
// line's box goes in an envelope lane: v of the way from the point u of
// the way along Top to the point u of the way along Bottom, each path
// measured by its own length (ECMA-376 Part 1, 20.1.9.19, steps 3 to 11).
func (l *WarpLane) Map(u, v float64) typeset.Point {
	t, _ := l.Top.At(u)
	b, _ := l.Bottom.At(u)
	w := 1 - v
	return typeset.Point{X: float64(t.X*w) + float64(b.X*v), Y: float64(t.Y*w) + float64(b.Y*v)}
}

// WarpCurve is a path of a text warp flattened into a polyline measured
// along its length.
type WarpCurve struct {
	pts []typeset.Point
	// cum[i] is the length of the polyline from its start to pts[i].
	cum []float64
}

// Empty reports whether the curve has no length to follow.
func (c *WarpCurve) Empty() bool { return len(c.pts) < 2 || !numeric.Positive(c.Length()) }

// Length returns the curve's length in points.
func (c *WarpCurve) Length() float64 {
	if len(c.cum) == 0 {
		return 0
	}
	return c.cum[len(c.cum)-1]
}

// At returns the point at the fraction u of the curve's length, u clamped
// to [0, 1], and the curve's unit direction there.
func (c *WarpCurve) At(u float64) (typeset.Point, typeset.Point) {
	if len(c.pts) == 0 {
		return typeset.Point{}, typeset.Point{X: 1}
	}
	if len(c.pts) == 1 || !numeric.Finite(u) {
		return c.pts[0], typeset.Point{X: 1}
	}
	s := float64(min(max(u, 0), 1) * c.Length())
	// The first vertex whose distance reaches s ends the segment s lies on.
	lo, hi := 1, len(c.cum)-1
	for lo < hi {
		mid := (lo + hi) / 2
		if c.cum[mid] < s {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	a, b := c.pts[lo-1], c.pts[lo]
	seg := c.cum[lo] - c.cum[lo-1]
	t := 0.0
	if seg > 0 {
		t = (s - c.cum[lo-1]) / seg
	}
	dx, dy := b.X-a.X, b.Y-a.Y
	p := typeset.Point{X: a.X + float64(dx*t), Y: a.Y + float64(dy*t)}
	if seg <= 0 {
		return p, typeset.Point{X: 1}
	}
	return p, typeset.Point{X: dx / seg, Y: dy / seg}
}

// The bounds of a text warp's paths. A warp evaluates no more.
const (
	// maxWarpLanes caps the lanes of a warp. Anti-DoS guard, not
	// spec-mandated: the presets have at most three, a Fontwork shape a
	// few.
	maxWarpLanes = 64
	// curveSteps is the number of chords a Bézier segment of a warp path
	// is flattened into: at the size of a slide a chord's sagitta stays
	// below a hundredth of a point on the presets' quarter arcs.
	curveSteps = 32
	// MaxWarpCurvePoints caps the vertices of the flattened paths of one
	// warp together, 24 bytes each, 384 KiB in all. Anti-DoS guard, not
	// spec-mandated: a preset's paths flatten to at most 6 × 257
	// vertices, a traced Fontwork path a few thousand; the rest of a longer
	// warp is left out.
	MaxWarpCurvePoints = 1 << 14
)

// WarpLanes returns the lanes of the warp w fitted to the box r, in the
// order a body's lines take them, nil when w warps nothing. Each subpath
// of the geometry's paths is a path of the warp, as a VML text path and a
// Fontwork shape draw theirs in one path of several subpaths. A geometry
// whose path count is even pairs its paths, a top and a bottom path a
// lane, as the presets that fill an envelope are written (20.1.9.19: the
// first path of a pair is the top line, the second the base line); one
// whose path count is odd makes a lane of each path, which a line
// follows.
//
// A preset is evaluated as PowerPoint evaluates it: on the square as
// tall as the box, then widened to the box. Its guides read the height
// for the shorter side (ss), whatever the box's shape, and an arc's
// angles (20.1.9.3 arcTo) fall where they fall on a circle, not where
// they point on the box's ellipse.
//
// # Corpus evidence
//
//   - litho/pptx text-warp-wordart: PowerPoint prints one line of
//     "WordArt" in each of the forty presets in a 116.9 by 138.2 point
//     box. In textDeflate and textDeflateBottom, whose definitions read ss,
//     the glyph points fit the definitions within 0.6 point with ss read
//     as the height and miss them by 3.9 and 10.4 points with ss read as
//     the width, the shorter side; the presets of two paths fill the box
//     between them, the line of textButton follows its first path, that of
//     textButtonPour, textDeflateInflate and textDeflateInflateDeflate
//     fills their first pair.
//   - libreoffice/pptx oox/tdf139618_WordArtBitmapFill: PowerPoint fills
//     the two pairs of textDeflateInflate with the two lines of
//     "Happy" and "Birthday", one a pair, ink from the box's top to its
//     bottom.
//   - libreoffice/pptx sd/tdf116350-texteffects: twenty lines of
//     "FontEffectArcUp" on textArchUp, textArchDown, textCircle and
//     textButton adjusted from 15 to 359 degrees in 135 by 71 point boxes
//     start and end within 2.5 points of where the arcs evaluated on the
//     square put them, at sizes within 0.06 point; on the box's ellipse
//     the two shortest arcs are 1.75 and 1.6 times shorter than the ones
//     PowerPoint fits its lines to.
func WarpLanes(w *TextWarp, r typeset.Rect) []WarpLane {
	def, heightShort := warpDefinition(w)
	if def == nil || len(def.Paths) == 0 {
		return nil
	}
	var g *Geometry
	if w.Custom == nil {
		g = &Geometry{Adjust: w.Adjust}
	}
	ev := newEvaluatorShort(def, g, squareBox(r, heightShort), heightShort)
	var curves []WarpCurve
	budget := MaxCommands
	points := MaxWarpCurvePoints
	for i := range def.Paths {
		if i >= MaxPaths || budget <= 0 || len(curves) >= 2*maxWarpLanes {
			break
		}
		pd := &def.Paths[i]
		n := min(len(pd.Cmds), budget)
		budget -= n
		ops := stretchToBox(ev.path(pd, pd.Cmds[:n]), r, heightShort)
		for start := 0; start < len(ops) && len(curves) < 2*maxWarpLanes && points > 0; {
			end := start + 1
			for end < len(ops) && ops[end].Kind != typeset.MoveTo {
				end++
			}
			if c := flatten(ops[start:end], points); len(c.pts) > 0 {
				curves = append(curves, c)
				points -= len(c.pts)
			}
			start = end
		}
	}
	var lanes []WarpLane
	if len(curves)%2 == 0 {
		for i := 0; i+1 < len(curves) && len(lanes) < maxWarpLanes; i += 2 {
			lanes = append(lanes, WarpLane{Top: curves[i], Bottom: curves[i+1]})
		}
	} else {
		for i := 0; i < len(curves) && len(lanes) < maxWarpLanes; i++ {
			lanes = append(lanes, WarpLane{Top: curves[i]})
		}
	}
	for _, l := range lanes {
		if !l.Top.Empty() {
			return lanes
		}
	}
	return nil
}

// warpDefinition returns the geometry of w and whether its guides read
// the height for the shorter side: a preset's do.
func warpDefinition(w *TextWarp) (*Custom, bool) {
	if w == nil {
		return nil, false
	}
	if w.Custom != nil {
		return w.Custom, false
	}
	return textWarpTable[w.Preset], true
}

// flatten returns the first subpath of ops as a measured polyline, each
// Bézier segment cut into curveSteps chords, at most limit vertices.
func flatten(ops []typeset.PathOp, limit int) WarpCurve {
	var c WarpCurve
	add := func(p typeset.Point) bool {
		if len(c.pts) >= limit || !numeric.Finite(p.X) || !numeric.Finite(p.Y) {
			return false
		}
		d := 0.0
		if n := len(c.pts); n > 0 {
			dx, dy := p.X-c.pts[n-1].X, p.Y-c.pts[n-1].Y
			d = c.cum[n-1] + math.Sqrt(float64(dx*dx)+float64(dy*dy))
		}
		c.pts = append(c.pts, p)
		c.cum = append(c.cum, d)
		return true
	}
	var cur typeset.Point
	for i, op := range ops {
		switch op.Kind {
		case typeset.MoveTo:
			if i > 0 {
				return c
			}
			cur = op.P[0]
			if !add(cur) {
				return c
			}
		case typeset.LineTo:
			cur = op.P[0]
			if !add(cur) {
				return c
			}
		case typeset.CurveTo:
			p0, p1, p2, p3 := cur, op.P[0], op.P[1], op.P[2]
			for k := 1; k <= curveSteps; k++ {
				if !add(cubicAt(p0, p1, p2, p3, float64(k)/curveSteps)) {
					return c
				}
			}
			cur = p3
		case typeset.ClosePath:
			if len(c.pts) > 0 {
				add(c.pts[0])
			}
			return c
		}
	}
	return c
}

// cubicAt returns the point at parameter t of the cubic Bézier p0..p3.
func cubicAt(p0, p1, p2, p3 typeset.Point, t float64) typeset.Point {
	s := 1 - t
	a, b, c, d := float64(s*s)*s, float64(3*s*s)*t, float64(3*s*t)*t, float64(t*t)*t
	return typeset.Point{
		X: float64(a*p0.X) + float64(b*p1.X) + float64(c*p2.X) + float64(d*p3.X),
		Y: float64(a*p0.Y) + float64(b*p1.Y) + float64(c*p2.Y) + float64(d*p3.Y),
	}
}

// squareBox returns the box a warp's geometry is evaluated in: for a
// preset (square true), the square as tall as r at r's origin, which
// stretchToBox widens to r afterwards.
func squareBox(r typeset.Rect, square bool) typeset.Rect {
	if !square || !numeric.Positive(r.Height) || !numeric.Positive(r.Width) {
		return r
	}
	return typeset.Rect{X: r.X, Y: r.Y, Width: r.Height, Height: r.Height}
}

// stretchToBox maps ops evaluated in squareBox(r, square) onto r,
// scaling x about r's left edge.
func stretchToBox(ops []typeset.PathOp, r typeset.Rect, square bool) []typeset.PathOp {
	if !square || !numeric.Positive(r.Height) || !numeric.Positive(r.Width) {
		return ops
	}
	k := r.Width / r.Height
	for i := range ops {
		for j := range ops[i].P {
			ops[i].P[j].X = r.X + float64((ops[i].P[j].X-r.X)*k)
		}
	}
	return ops
}
