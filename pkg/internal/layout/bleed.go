package layout

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// markLine is the width of a page mark's line: one CSS pixel, as
// WeasyPrint strokes its marks.
const markLine = css.Pt

// trim moves what the page pg paints from index from on into the page box
// of pb, inside the bleed, and records the page's trim and bleed boxes
// (CSS Page 3, 7.2; ISO 32000-1, 14.11.2). A page without a bleed is left
// as it is.
func trim(pg *compose.Page, pb PageBox, from int) {
	b := pb.Bleed
	if b <= 0 {
		return
	}
	for i := range pg.Links {
		pg.Links[i].Rect.X += b
		pg.Links[i].Rect.Y += b
	}
	if from < len(pg.Content) {
		inner := append([]compose.Fragment(nil), pg.Content[from:]...)
		pg.Content = append(pg.Content[:from], &compose.Group{Transform: compose.Matrix{A: 1, D: 1, E: b, F: b}, Opacity: 1, Content: inner})
	}
	pg.TrimBox = &compose.Rect{X: b, Y: b, Width: pb.Width, Height: pb.Height}
	pg.BleedBox = &compose.Rect{Width: pg.Width, Height: pg.Height}
}

// paintMarks paints the page marks of pb on the sheet pg, over the page
// background and under the canvas (CSS Page 3, 7.1), where WeasyPrint
// draws them, Chromium drawing none: a crop mark at each corner, two
// strokes on the trim lines reaching half the bleed out from the sheet's
// edge, and a registration cross at the middle of each side, a circle
// and a crosshair half the bleed long, centred a quarter of the bleed in
// from the edge. The straight strokes are filled rectangles covering the
// CSS pixels the stroke touches, so that a line one pixel wide on a
// fractional position prints whole rather than as two half-tone halves.
func (e *engine) paintMarks(pg *compose.Page, pb PageBox) {
	b := pb.Bleed
	if b <= 0 || pb.Marks == 0 {
		return
	}
	w, h := pg.Width, pg.Height
	hb := b / 2
	var rects []compose.Rect
	// line adds the stroke of width lw from (x0, y0) to (x1, y1), one of
	// the two coordinates the same.
	line := func(x0, y0, x1, y1, lw float64) {
		r := compose.Rect{X: min(x0, x1), Y: min(y0, y1), Width: math.Abs(x1 - x0), Height: math.Abs(y1 - y0)}
		if r.Width == 0 {
			r.X, r.Width = r.X-lw/2, lw
		} else {
			r.Y, r.Height = r.Y-lw/2, lw
		}
		rects = append(rects, snapOut(r))
	}
	var circles []compose.Fragment
	circle := func(cx, cy, r, lw float64) {
		circles = append(circles, &compose.Path{Ops: circleOps(cx, cy, r), Stroke: &compose.Stroke{Color: compose.Black, Width: lw}})
	}
	if pb.Marks&css.MarksCrop != 0 {
		for _, c := range [4][2]float64{{0, 0}, {w, 0}, {0, h}, {w, h}} {
			// Mirror the top-left mark into each corner.
			sx, sy := 1.0, 1.0
			if c[0] > 0 {
				sx = -1
			}
			if c[1] > 0 {
				sy = -1
			}
			line(c[0], c[1]+sy*b, c[0]+sx*hb, c[1]+sy*b, markLine)
			line(c[0]+sx*b, c[1], c[0]+sx*b, c[1]+sy*hb, markLine)
		}
	}
	if pb.Marks&css.MarksCross != 0 {
		q := b / 4
		// Top and bottom: a crosshair centred at (w/2, q) from the edge.
		for _, y := range [2][2]float64{{0, 1}, {h, -1}} {
			cy := y[0] + y[1]*q
			line(w/2-q, cy, w/2+q, cy, markLine/2)
			line(w/2, y[0], w/2, y[0]+y[1]*hb, markLine/2)
			circle(w/2, cy, b/8, markLine/4)
		}
		for _, x := range [2][2]float64{{0, 1}, {w, -1}} {
			cx := x[0] + x[1]*q
			line(cx, h/2-q, cx, h/2+q, markLine/2)
			line(x[0], h/2, x[0]+x[1]*hb, h/2, markLine/2)
			circle(cx, h/2, b/8, markLine/4)
		}
	}
	for _, r := range rects {
		pg.Content = append(pg.Content, &compose.Path{Ops: rectOps(r), Fill: &compose.Fill{Color: compose.Black}})
	}
	pg.Content = append(pg.Content, circles...)
}

// snapOut grows r to the CSS pixel grid around it.
func snapOut(r compose.Rect) compose.Rect {
	const slack = 1e-6
	x0 := math.Floor(r.X/css.Pt+slack) * css.Pt
	y0 := math.Floor(r.Y/css.Pt+slack) * css.Pt
	x1 := math.Ceil((r.X+r.Width)/css.Pt-slack) * css.Pt
	y1 := math.Ceil((r.Y+r.Height)/css.Pt-slack) * css.Pt
	return compose.Rect{X: x0, Y: y0, Width: max(x1-x0, css.Pt), Height: max(y1-y0, css.Pt)}
}

// circleOps returns the path of the circle of radius r centred on (cx,
// cy), four cubic Béziers.
func circleOps(cx, cy, r float64) []compose.PathOp {
	k := kappa * r
	pt := func(x, y float64) compose.Point { return compose.Point{X: cx + x, Y: cy + y} }
	curve := func(a, b, c compose.Point) compose.PathOp {
		return compose.PathOp{Kind: compose.CurveTo, P: [3]compose.Point{a, b, c}}
	}
	return []compose.PathOp{
		{Kind: compose.MoveTo, P: [3]compose.Point{pt(r, 0)}},
		curve(pt(r, k), pt(k, r), pt(0, r)),
		curve(pt(-k, r), pt(-r, k), pt(-r, 0)),
		curve(pt(-r, -k), pt(-k, -r), pt(0, -r)),
		curve(pt(k, -r), pt(r, -k), pt(r, 0)),
		{Kind: compose.ClosePath},
	}
}
