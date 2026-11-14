package layout

import (
	"math"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/svg"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// clipPathOf returns the clips of b's clip-path in flow coordinates, the
// content showing inside every one, none for no clip-path, and false when
// the clip-path clips everything away (CSS Masking 1, 5.1). A clip is a
// clipping path (ISO 32000-1, 8.5.4) and needs no group of its own.
//
// # Corpus evidence
//
//   - probe, html: Chromium writes a clip-path of each basic shape, of a
//     box alone and of a url() to a clipPath element as a clipping path
//     (W n) around the element's painting, with no transparency group,
//     and draws an element whose url() names no element unclipped.
func (p *painter) clipPathOf(b *box) ([]*typeset.Path, bool) {
	c := b.st.ClipPath
	if c.None() {
		return nil, true
	}
	if c.URL != "" {
		return p.clipURL(b, c.URL)
	}
	r, radii := p.referenceBox(b, c.Box)
	if c.Shape == nil {
		if r.Width <= 0 || r.Height <= 0 {
			return nil, false
		}
		return []*typeset.Path{{Ops: roundedRect(r, radii), Fill: &typeset.Fill{}}}, true
	}
	clip, ok := shapePath(c.Shape, r)
	if !ok {
		return nil, false
	}
	return []*typeset.Path{clip}, true
}

// referenceBox returns b's reference box g in flow coordinates, its
// relative offset in force, and the radii of its corners (CSS Masking 1,
// 5.1: a box alone clips with its corner shaping).
func (p *painter) referenceBox(b *box, g css.GeometryBox) (typeset.Rect, [4][2]float64) {
	if b.kind == kindInline {
		r, radii, _ := p.inlineReference(b, g) // an element on no line has an empty reference box
		return r, radii
	}
	r := p.borderRect(b)
	radii := radiiOf(b, r.Width, r.Height)
	switch g {
	case css.GeoBorder:
	case css.GeoPadding:
		r, radii = insetRect(r, radii, b.bd)
	case css.GeoContent:
		r, radii = insetRect(r, radii, addSides(b.bd, b.pd))
	case css.GeoMargin:
		m := shapeMargins(b)
		r = typeset.Rect{X: r.X - m[css.Left], Y: r.Y - m[css.Top], Width: r.Width + m[css.Left] + m[css.Right], Height: r.Height + m[css.Top] + m[css.Bottom]}
		// CSS Masking 1, 5.1 takes the margin box of CSS Shapes 1, 5.
		radii = marginRadii(r, radii, m)
	}
	return r, radii
}

// shapeMargins returns the margins of b's margin box as a reference box:
// its used margins, except that the horizontal margins of a block in
// normal flow are the specified ones, not the one CSS 2.1, 10.3.3 stretches
// to fill an over-constrained line, as Chromium measures the margin box.
//
// # Corpus evidence
//
//   - wpt: css-masking/clip-path/clip-path-polygon-008: Chromium 156's
//     screenshot places polygon() percentages of the margin-box of a 200px
//     wide margin box in an 800px window against 200px, not against the
//     stretched right margin.
func shapeMargins(b *box) [4]float64 {
	m := b.m
	st := b.st
	if b.parent == nil || b.inline || b.float != css.FloatNone || b.abs || b.isItem() || b.tpart != partNone ||
		st.Margin[css.Left].Kind == css.Auto || st.Margin[css.Right].Kind == css.Auto {
		return m
	}
	// The used margins and border box fill the containing block.
	cbw := b.borderW() + m[css.Left] + m[css.Right]
	m[css.Left], m[css.Right] = resolve(st.Margin[css.Left], cbw), resolve(st.Margin[css.Right], cbw)
	return m
}

// shapePath returns the clipping path of the basic shape s in the
// reference box r (CSS Shapes 1, 3.1), false when it encloses nothing.
func shapePath(s *css.Shape, r typeset.Rect) (*typeset.Path, bool) {
	w, h := r.Width, r.Height
	fill := &typeset.Fill{}
	if s.EvenOdd {
		fill.Rule = typeset.EvenOdd
	}
	var ops []typeset.PathOp
	switch s.Kind {
	case css.ShapeInset:
		in := typeset.Rect{X: r.X + resolve(s.Inset[css.Left], w), Y: r.Y + resolve(s.Inset[css.Top], h)}
		in.Width = r.X + w - resolve(s.Inset[css.Right], w) - in.X
		in.Height = r.Y + h - resolve(s.Inset[css.Bottom], h) - in.Y
		if in.Width <= 0 || in.Height <= 0 {
			return nil, false
		}
		var radii [4][2]float64
		for c, rd := range s.Radii {
			// The radii's percentages are of the reference box, not of
			// the inset rectangle (CSS Shapes 1, 3.1, inset()).
			radii[c] = [2]float64{max(resolve(rd.H, w), 0), max(resolve(rd.V, h), 0)}
			if radii[c][0] == 0 || radii[c][1] == 0 {
				radii[c] = [2]float64{}
			}
		}
		ops = roundedRect(in, fitRadii(in, radii))
	case css.ShapeCircle, css.ShapeEllipse:
		cx, cy := resolve(s.Center[0], w), resolve(s.Center[1], h)
		dx0, dx1, dy0, dy1 := math.Abs(cx), math.Abs(w-cx), math.Abs(cy), math.Abs(h-cy)
		side := func(sr css.ShapeRadius, near, far, ref float64) float64 {
			switch sr.Side {
			case css.RadiusFarthest:
				return far
			case css.RadiusLength:
				return max(resolve(sr.Length, ref), 0)
			case css.RadiusClosestCorner:
				return detmath.Hypot(min(dx0, dx1), min(dy0, dy1))
			case css.RadiusFarthestCorner:
				return detmath.Hypot(max(dx0, dx1), max(dy0, dy1))
			case css.RadiusClosest:
			}
			return near
		}
		var rx, ry float64
		if s.Kind == css.ShapeCircle {
			// A circle's percentage is of the reference box's diagonal
			// over the square root of 2 (CSS Shapes 1, 3.1).
			rx = side(s.Radius[0], min(dx0, dx1, dy0, dy1), max(dx0, dx1, dy0, dy1), detmath.Hypot(w, h)/math.Sqrt2)
			ry = rx
		} else {
			rx = side(s.Radius[0], min(dx0, dx1), max(dx0, dx1), w)
			ry = side(s.Radius[1], min(dy0, dy1), max(dy0, dy1), h)
		}
		if rx <= 0 || ry <= 0 {
			return nil, false
		}
		e := typeset.Rect{X: r.X + cx - rx, Y: r.Y + cy - ry, Width: 2 * rx, Height: 2 * ry}
		ops = roundedRect(e, [4][2]float64{{rx, ry}, {rx, ry}, {rx, ry}, {rx, ry}})
	case css.ShapePolygon:
		pts := make([]typeset.Point, len(s.Points))
		for i, pt := range s.Points {
			pts[i] = typeset.Point{X: r.X + resolve(pt[0], w), Y: r.Y + resolve(pt[1], h)}
		}
		ops = roundedPolygon(pts, s.Round)
	case css.ShapePath, css.ShapeShape:
		// Path data is in CSS pixels from the reference box's top left.
		data, _ := svg.PathData(shapePathData(s, w, h)) // a path cut short by an error clips with what it drew, as SVG 1.1, F.2 asks
		for _, op := range data {
			for i := range op.P {
				op.P[i] = typeset.Point{X: r.X + float64(op.P[i].X*css.Pt), Y: r.Y + float64(op.P[i].Y*css.Pt)}
			}
			ops = append(ops, op)
		}
	}
	if len(ops) < 3 {
		return nil, false
	}
	return &typeset.Path{Ops: ops, Fill: fill}, true
}

// roundedPolygon returns the closed path through pts, each vertex
// rounded by a circle of radius round tangent to both its sides, centred
// on the bisector of its angle (CSS Shapes 1, 3.1, polygon()). The radius
// of each vertex is clamped so that its rounding takes at most half of
// either side: tan(angle / 2) times half the side. The arc is drawn as
// one cubic Bézier curve.
func roundedPolygon(pts []typeset.Point, round float64) []typeset.PathOp {
	ops := make([]typeset.PathOp, 0, 2*len(pts)+1)
	n := len(pts)
	for i, p := range pts {
		k := typeset.LineTo
		if i == 0 {
			k = typeset.MoveTo
		}
		if !(round > 0) || n < 3 {
			ops = append(ops, typeset.PathOp{Kind: k, P: [3]typeset.Point{p}})
			continue
		}
		a, b := pts[(i+n-1)%n], pts[(i+1)%n]
		la, lb := detmath.Hypot(a.X-p.X, a.Y-p.Y), detmath.Hypot(b.X-p.X, b.Y-p.Y)
		if la <= 0 || lb <= 0 {
			ops = append(ops, typeset.PathOp{Kind: k, P: [3]typeset.Point{p}})
			continue
		}
		ua := typeset.Point{X: (a.X - p.X) / la, Y: (a.Y - p.Y) / la}
		ub := typeset.Point{X: (b.X - p.X) / lb, Y: (b.Y - p.Y) / lb}
		angle := detmath.Acos(min(max(float64(ua.X*ub.X)+float64(ua.Y*ub.Y), -1), 1))
		half := detmath.Tan(angle / 2)
		if !(half > 0) || angle >= math.Pi {
			ops = append(ops, typeset.PathOp{Kind: k, P: [3]typeset.Point{p}})
			continue
		}
		r := min(round, half*la/2, half*lb/2)
		d := r / half
		t1 := typeset.Point{X: p.X + float64(ua.X*d), Y: p.Y + float64(ua.Y*d)}
		t2 := typeset.Point{X: p.X + float64(ub.X*d), Y: p.Y + float64(ub.Y*d)}
		// The arc turns by pi less the angle; its control points lie on
		// the tangents, toward the vertex, 4/3 tan(turn / 4) r away.
		c := 4.0 / 3 * detmath.Tan((math.Pi-angle)/4) * r
		ops = append(ops,
			typeset.PathOp{Kind: k, P: [3]typeset.Point{t1}},
			typeset.PathOp{Kind: typeset.CurveTo, P: [3]typeset.Point{
				{X: t1.X - float64(ua.X*c), Y: t1.Y - float64(ua.Y*c)}, {X: t2.X - float64(ub.X*c), Y: t2.Y - float64(ub.Y*c)}, t2,
			}})
	}
	return append(ops, typeset.PathOp{Kind: typeset.ClosePath})
}

// shapePathData returns the SVG path data of a path() or a shape() in a
// reference box w by h points, in CSS pixels: a shape()'s commands are
// the path commands of the same name, its lengths resolved against the
// box (CSS Shapes 1, 3.2.1).
func shapePathData(s *css.Shape, w, h float64) string {
	if s.Kind == css.ShapePath {
		return s.Path
	}
	var b []byte
	for _, c := range s.Commands {
		b = append(b, c.Op)
		for i, a := range c.Args {
			ref := w
			switch {
			case c.OneRadius && i < 2:
				ref = detmath.Hypot(w, h) / math.Sqrt2
			case i < len(c.Vertical) && c.Vertical[i]:
				ref = h
			}
			if c.Op|0x20 == 'a' && i == 2 {
				b = append(b, ' ')
				b = strconv.AppendFloat(b, c.Rotate, 'g', -1, 64)
				b = append(b, ' ', boolDigit(c.Large), ' ', boolDigit(c.Sweep))
			}
			b = append(b, ' ')
			b = strconv.AppendFloat(b, resolve(a, ref)/css.Pt, 'g', -1, 64)
		}
		b = append(b, ' ')
	}
	return string(b)
}

// boolDigit returns the flag digit of an arc, '1' for set.
func boolDigit(v bool) byte {
	if v {
		return '1'
	}
	return '0'
}

// clipURL returns the clips of the clipPath element url names, for b;
// none when url names no clipPath element, which leaves b unclipped (CSS
// Masking 1, 5.1). A clip path of several regions, which one clipping
// path cannot unite, clips with its first.
func (p *painter) clipURL(b *box, url string) ([]*typeset.Path, bool) {
	if p.e.opts.ClipSource == nil {
		return nil, true
	}
	r, _ := p.referenceBox(b, css.GeoBorder)
	regions, found := p.e.opts.ClipSource(p.e.ctx, url, r)
	if !found {
		return nil, true
	}
	if len(regions) == 0 || len(regions[0]) == 0 {
		return nil, false
	}
	if len(regions) > 1 {
		p.e.warnOnce(warn.ContentUnsupported, warn.Approximated, "a clip path whose shapes differ in clip rule or carry clip paths of their own clips with its first region alone", r.Y)
	}
	return regions[0], true
}

// clipsRect reports an absolutely positioned box that clip clips: clip
// applies to absolutely positioned elements alone (CSS 2.2, 11.1.2).
func clipsRect(b *box) bool {
	return b.abs && b.st.Clip.Set && b.parent != nil
}

// clipRectOf returns the rectangle clip cuts from b's border box r, each
// offset from r's top left, auto the box's own edge (CSS 2.2, 11.1.2).
func clipRectOf(b *box, r typeset.Rect) typeset.Rect {
	e := b.st.Clip.Edges
	edge := func(d css.Dimension, auto float64) float64 {
		if d.Kind == css.Auto {
			return auto
		}
		return resolve(d, 0)
	}
	top, right, bottom, left := edge(e[css.Top], 0), edge(e[css.Right], r.Width), edge(e[css.Bottom], r.Height), edge(e[css.Left], 0)
	return typeset.Rect{X: r.X + left, Y: r.Y + top, Width: right - left, Height: bottom - top}
}
