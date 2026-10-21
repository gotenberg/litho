package page

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/stroke"
)

// Stroke adjustment (ISO 32000-1, 10.6.5) snaps a thin line or a thin
// rectangle to whole device pixels, so a line one pixel wide that falls
// between two rows paints one row solid rather than two at half strength,
// and no line thinner than a pixel fades.
//
// Deliberate leniency: 10.6.5 lets /SA false turn the adjustment off, and
// litho adjusts whatever /SA says, as poppler's Splash does: its
// SplashOutputDev ignores the parameter because Acrobat hardwires it on.

// alignEpsilon is how far, in device pixels, the two ends of a segment may
// part on an axis and still count as one: such a segment runs along the
// other axis. It absorbs the rounding of a user-space coordinate times the
// page matrix.
const alignEpsilon = 1e-6

// edgeEpsilon is how far, relative to its magnitude, an outline coordinate
// may lie from a stroke edge and still move with it: the stroker computes an
// edge point as the centre line plus half the width, which rounds.
const edgeEpsilon = 1e-7

// maxAdjustedSegments bounds the segments a stroke may hold and still be
// adjusted. Anti-DoS guard, not spec-mandated: moving the outline costs its
// points times the segments' edges, and the strokes adjustment serves,
// rules, frames and table grids, hold a few dozen segments.
const maxAdjustedSegments = 256

// adjustOutline snaps outline, the device-space stroke of the device path p
// with style under the matrix m from user to device space, to whole device
// pixels, and returns it unchanged when p cannot be adjusted.
//
// As poppler's Splash adjusts a stroke, each edge of a horizontal or vertical
// segment moves to the nearest pixel boundary, the two edges of a segment at
// least a pixel apart, and so does the end of a butt or square cap across
// such a segment. The two edges round apart, so a line 1.6 pixels wide covers
// one row or two as its position falls, as in Splash. A path holding a curve
// or a slanted segment is left as it is, and so is a stroke under a matrix
// that does not map the axes onto the axes.
func adjustOutline(outline, p geom.Path, style stroke.Style, m geom.Matrix) geom.Path {
	thickX, thickY, ok := axisThickness(m, style.Width)
	if !ok {
		return outline
	}
	xs, ys, ok := axisEdges(p, style.Cap, thickX/2, thickY/2)
	if !ok {
		return outline
	}
	out := geom.Path{Ops: outline.Ops, Pts: make([]geom.Point, len(outline.Pts))}
	for i, pt := range outline.Pts {
		out.Pts[i] = geom.Point{X: xs.snap(pt.X), Y: ys.snap(pt.Y)}
	}
	return out
}

// axisThickness returns the device thickness of a stroke of width w across a
// vertical and across a horizontal device segment, false when m does not map
// the axes onto the axes. A width of 0 is one device pixel (8.4.3.2).
func axisThickness(m geom.Matrix, w float64) (thickX, thickY float64, ok bool) {
	if !(w >= 0) || math.IsInf(w, 0) {
		return 0, 0, false
	}
	switch {
	case m[1] == 0 && m[2] == 0:
		thickX, thickY = w*math.Abs(m[0]), w*math.Abs(m[3])
	case m[0] == 0 && m[3] == 0:
		// A turn by 90 degrees: a device-horizontal segment is user-vertical,
		// and its thickness is the image of the user x axis.
		thickX, thickY = w*math.Abs(m[2]), w*math.Abs(m[1])
	default:
		return 0, 0, false
	}
	if w == 0 {
		thickX, thickY = 1, 1
	}
	if math.IsInf(thickX, 0) || math.IsNaN(thickX) || math.IsInf(thickY, 0) || math.IsNaN(thickY) {
		return 0, 0, false
	}
	return thickX, thickY, true
}

// edgeMap maps the stroke edges on one axis to the pixel boundaries they
// move to.
type edgeMap []struct{ from, to float64 }

func (e *edgeMap) add(from, to float64) {
	*e = append(*e, struct{ from, to float64 }{from, to})
}

// snap returns the boundary v moves to, v itself when it lies on no edge.
func (e edgeMap) snap(v float64) float64 {
	for _, m := range e {
		if math.Abs(v-m.from) <= edgeEpsilon*max(1, math.Abs(v)) {
			return m.to
		}
	}
	return v
}

// axisEdges collects the edges of the segments of the device path p, a
// stroke of half thickness hx across vertical segments and hy across
// horizontal ones: xs holds the x edges of vertical segments and the cap
// ends of horizontal ones, ys the converse. It reports false when p holds a
// curve, a slanted segment, or more than [maxAdjustedSegments] segments.
func axisEdges(p geom.Path, lineCap stroke.Cap, hx, hy float64) (xs, ys edgeMap, ok bool) {
	segments := 0
	segment := func(a, b geom.Point) bool {
		segments++
		dx, dy := math.Abs(a.X-b.X), math.Abs(a.Y-b.Y)
		switch {
		case dx < alignEpsilon && dy < alignEpsilon:
		case dx < alignEpsilon:
			lo, hi := snapEdges(a.X-hx, a.X+hx)
			xs.add(a.X-hx, lo)
			xs.add(a.X+hx, hi)
		case dy < alignEpsilon:
			lo, hi := snapEdges(a.Y-hy, a.Y+hy)
			ys.add(a.Y-hy, lo)
			ys.add(a.Y+hy, hi)
		default:
			return false
		}
		return segments <= maxAdjustedSegments
	}
	// capEnd snaps the cap at the end of an open subpath, reached from from.
	// A butt cap ends at the end point, a square one half the line width
	// past it (8.4.3.3); a round cap is a curve and is left as it is.
	capEnd := func(end, from geom.Point) {
		var ext float64
		switch lineCap {
		case stroke.ButtCap:
		case stroke.SquareCap:
			ext = 1
		case stroke.RoundCap:
			return
		default:
			return
		}
		switch {
		case math.Abs(end.Y-from.Y) < alignEpsilon && math.Abs(end.X-from.X) >= alignEpsilon:
			v := end.X + math.Copysign(ext*hx, end.X-from.X)
			xs.add(v, math.Floor(v+0.5))
		case math.Abs(end.X-from.X) < alignEpsilon && math.Abs(end.Y-from.Y) >= alignEpsilon:
			v := end.Y + math.Copysign(ext*hy, end.Y-from.Y)
			ys.add(v, math.Floor(v+0.5))
		}
	}
	first, prev, pt := -1, -1, 0
	closed := false
	endSubpath := func() {
		if closed || first < 0 || prev <= first {
			return
		}
		capEnd(p.Pts[first], p.Pts[first+1])
		capEnd(p.Pts[prev], p.Pts[prev-1])
	}
	for _, op := range p.Ops {
		switch op {
		case geom.MoveTo:
			endSubpath()
			first, prev, closed = pt, pt, false
			pt++
		case geom.LineTo:
			if prev >= 0 && !segment(p.Pts[prev], p.Pts[pt]) {
				return nil, nil, false
			}
			prev = pt
			pt++
		case geom.Close:
			if first >= 0 && prev >= 0 && !segment(p.Pts[prev], p.Pts[first]) {
				return nil, nil, false
			}
			closed = true
		case geom.CubicTo:
			return nil, nil, false
		default:
			return nil, nil, false
		}
	}
	endSubpath()
	return xs, ys, true
}

// adjustedRect returns the device path p snapped to whole pixels when it is
// one axis-aligned rectangle: its edges round to the nearest pixel
// boundaries, and a side that would round to nothing keeps one pixel. Any
// other path returns false. poppler's Splash adjusts the fill of a path of
// one rectangular subpath the same way, as it judges Acrobat does.
func adjustedRect(p geom.Path) (geom.Path, bool) {
	for i, op := range p.Ops {
		switch {
		case i == 0 && op == geom.MoveTo, i > 0 && op == geom.LineTo:
		case op == geom.Close && i == len(p.Ops)-1:
		default:
			return geom.Path{}, false
		}
	}
	corners := p.Pts
	if len(corners) == 5 && math.Abs(corners[4].X-corners[0].X) < alignEpsilon && math.Abs(corners[4].Y-corners[0].Y) < alignEpsilon {
		corners = corners[:4]
	}
	if len(corners) != 4 {
		return geom.Path{}, false
	}
	// Each side runs along one axis, and the sides alternate.
	for i := range 4 {
		a, b := corners[i], corners[(i+1)%4]
		horizontal := math.Abs(a.Y-b.Y) < alignEpsilon
		if horizontal == (math.Abs(a.X-b.X) < alignEpsilon) {
			return geom.Path{}, false
		}
		if i > 0 && (math.Abs(corners[i-1].Y-a.Y) < alignEpsilon) == horizontal {
			return geom.Path{}, false
		}
	}
	x0, x1 := snapEdges(minMax(corners, func(q geom.Point) float64 { return q.X }))
	y0, y1 := snapEdges(minMax(corners, func(q geom.Point) float64 { return q.Y }))
	var out geom.Path
	out.MoveTo(geom.Point{X: x0, Y: y0})
	out.LineTo(geom.Point{X: x1, Y: y0})
	out.LineTo(geom.Point{X: x1, Y: y1})
	out.LineTo(geom.Point{X: x0, Y: y1})
	out.Close()
	return out, true
}

func minMax(pts []geom.Point, coord func(geom.Point) float64) (lo, hi float64) {
	lo, hi = coord(pts[0]), coord(pts[0])
	for _, q := range pts[1:] {
		lo, hi = min(lo, coord(q)), max(hi, coord(q))
	}
	return lo, hi
}

// snapEdges rounds the edges lo and hi of a side to the nearest pixel
// boundaries, at least one pixel apart.
func snapEdges(lo, hi float64) (float64, float64) {
	a, b := math.Floor(lo+0.5), math.Floor(hi+0.5)
	if b <= a {
		b = a + 1
	}
	return a, b
}
