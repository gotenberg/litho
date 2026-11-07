package chart

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// kappa places the control points of a cubic Bézier quarter circle.
const kappa = 0.5522847498

// polygon returns the closed outline through pts.
func polygon(pts ...compose.Point) []compose.PathOp {
	if len(pts) == 0 {
		return nil
	}
	ops := make([]compose.PathOp, 0, len(pts)+1)
	ops = append(ops, compose.PathOp{Kind: compose.MoveTo, P: [3]compose.Point{pts[0]}})
	for _, p := range pts[1:] {
		ops = append(ops, compose.PathOp{Kind: compose.LineTo, P: [3]compose.Point{p}})
	}
	return append(ops, compose.PathOp{Kind: compose.ClosePath})
}

// ellipse returns the outline of the ellipse of centre (cx, cy) and radii
// rx, ry.
func ellipse(cx, cy, rx, ry float64) []compose.PathOp {
	kx, ky := rx*kappa, ry*kappa
	return []compose.PathOp{
		{Kind: compose.MoveTo, P: [3]compose.Point{{X: cx + rx, Y: cy}}},
		{Kind: compose.CurveTo, P: [3]compose.Point{{X: cx + rx, Y: cy + ky}, {X: cx + kx, Y: cy + ry}, {X: cx, Y: cy + ry}}},
		{Kind: compose.CurveTo, P: [3]compose.Point{{X: cx - kx, Y: cy + ry}, {X: cx - rx, Y: cy + ky}, {X: cx - rx, Y: cy}}},
		{Kind: compose.CurveTo, P: [3]compose.Point{{X: cx - rx, Y: cy - ky}, {X: cx - kx, Y: cy - ry}, {X: cx, Y: cy - ry}}},
		{Kind: compose.CurveTo, P: [3]compose.Point{{X: cx + kx, Y: cy - ry}, {X: cx + rx, Y: cy - ky}, {X: cx + rx, Y: cy}}},
		{Kind: compose.ClosePath},
	}
}

// markerPath returns the outline of symbol sym of size s centred on (x,
// y), as LibreOffice's ShapeFactory draws its standard symbols.
func markerPath(sym MarkerSymbol, x, y, s float64) []compose.PathOp {
	h := s / 2
	pt := func(dx, dy float64) compose.Point { return compose.Point{X: x + dx*h, Y: y + dy*h} }
	switch sym {
	case MarkerDiamond:
		return polygon(pt(0, -1), pt(1, 0), pt(0, 1), pt(-1, 0))
	case MarkerArrowDown:
		return polygon(pt(-1, -1), pt(1, -1), pt(0, 1))
	case MarkerArrowUp:
		return polygon(pt(-1, 1), pt(0, -1), pt(1, 1))
	case MarkerArrowRight:
		return polygon(pt(-1, -1), pt(1, 0), pt(-1, 1))
	case MarkerArrowLeft:
		return polygon(pt(1, -1), pt(1, 1), pt(-1, 0))
	case MarkerBowTie:
		return polygon(pt(-1, -1), pt(1, 1), pt(1, -1), pt(-1, 1))
	case MarkerSandglass:
		return polygon(pt(-1, -1), pt(1, -1), pt(-1, 1), pt(1, 1))
	case MarkerCircle:
		return ellipse(x, y, h, h)
	case MarkerStar:
		var pts []compose.Point
		for i := range 8 {
			a := float64(i)*math.Pi/4 - math.Pi/2
			r := 1.0
			if i%2 == 1 {
				r = 0.35
			}
			pts = append(pts, pt(r*math.Cos(a), r*math.Sin(a)))
		}
		return polygon(pts...)
	case MarkerX:
		t := 0.25
		return polygon(pt(-1, -1), pt(-1+t, -1), pt(0, -t), pt(1-t, -1), pt(1, -1), pt(1, -1+t), pt(t, 0), pt(1, 1-t), pt(1, 1),
			pt(1-t, 1), pt(0, t), pt(-1+t, 1), pt(-1, 1), pt(-1, 1-t), pt(-t, 0), pt(-1, -1+t))
	case MarkerPlus:
		t := 0.2
		return polygon(pt(-t, -1), pt(t, -1), pt(t, -t), pt(1, -t), pt(1, t), pt(t, t), pt(t, 1), pt(-t, 1), pt(-t, t), pt(-1, t), pt(-1, -t), pt(-t, -t))
	case MarkerAsterisk:
		t := 0.15
		var pts []compose.Point
		for i := range 6 {
			a := float64(i) * math.Pi / 3
			c, s := math.Cos(a), math.Sin(a)
			pts = append(pts, pt(c-t*s, s+t*c), pt(c+t*s, s-t*c))
		}
		return polygon(pts...)
	case MarkerHorizontalBar:
		return polygon(pt(-1, -0.3), pt(1, -0.3), pt(1, 0.3), pt(-1, 0.3))
	case MarkerVerticalBar:
		return polygon(pt(-0.3, -1), pt(0.3, -1), pt(0.3, 1), pt(-0.3, 1))
	}
	return polygon(pt(-1, -1), pt(1, -1), pt(1, 1), pt(-1, 1))
}

// marker draws m centred on (x, y).
func (rd *renderer) marker(m Marker, x, y float64) {
	if m.Symbol == MarkerNone || m.Symbol == MarkerAuto || !(m.Size > 0) {
		return
	}
	border := m.Border
	if border != nil && !border.None {
		b := *border
		b.Width = math.Min(b.Width, m.Size/4)
		border = &b
	}
	rd.add(paintPath(markerPath(m.Symbol, x, y, m.Size), m.Fill, border))
}
