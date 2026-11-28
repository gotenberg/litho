package chart

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// maxSurfaceBands caps the bands a surface is cut into. Anti-DoS guard,
// not spec-mandated: the bands are the major intervals of the value axis,
// which an automatic scale keeps to ten and a stated unit could raise to
// thousands; Office's surfaces show a handful.
const maxSurfaceBands = 64

// surfaceLevels returns the bounds of the bands of a surface drawn against
// y: its major ticks from the axis's minimum to its maximum, at most
// maxSurfaceBands bands; nil for an axis without a linear scale of two
// ticks.
func surfaceLevels(y *dim) []float64 {
	if y == nil || y.sc.log > 0 || !(y.sc.major > 0) {
		return nil
	}
	ts := y.sc.ticks()
	if len(ts) < 2 {
		return nil
	}
	if len(ts) > maxSurfaceBands+1 {
		ts = ts[:maxSurfaceBands+1]
	}
	return ts
}

// bandFill returns the fill of band k of the surface group g, of n: the
// band's format; else the first series' fill, which Office gives every
// band; else the automatic colour of the k-th series.
//
// # Corpus evidence
//
//   - litho/pptx: chart-surface's 3-D surface and contour, whose first
//     series states accent1: PowerPoint keys their three bands #4472C4.
//   - litho/xlsx: charts_radar_stock_surface's 3-D surface, whose series
//     state no fill: Excel fills its two bands accent1 and accent2.
func (rd *renderer) bandFill(g *Group, k, n int) *Paint {
	if k < len(g.Bands) && g.Bands[k].Fill != nil {
		return g.Bands[k].Fill
	}
	if len(g.Series) > 0 && g.Series[0].Fill != nil {
		return g.Series[0].Fill
	}
	return &Paint{Color: rd.c.AutoColor(k, n)}
}

// bandLine returns the line of band k of the wireframe surface g, of n:
// the band's format, else the automatic colour of the k-th series, 0.75
// point wide.
//
// # Corpus evidence
//
//   - litho/pptx: chart-surface's wireframes, whose series state an empty
//     line: PowerPoint draws their three bands' lines accent1, accent2
//     and accent3.
func (rd *renderer) bandLine(g *Group, k, n int) *Stroke {
	if k < len(g.Bands) && g.Bands[k].Border != nil {
		return g.Bands[k].Border
	}
	return &Stroke{Color: rd.c.AutoColor(k, n), Width: 0.75}
}

// gridPoint is a point of a surface's grid: its category position u, its
// row w, and its value.
type gridPoint struct {
	u, w, v float64
}

// mix returns the point a share t of the way from a to b.
func mix(a, b gridPoint, t float64) gridPoint {
	return gridPoint{
		u: a.u + float64(t*(b.u-a.u)),
		w: a.w + float64(t*(b.w-a.w)),
		v: a.v + float64(t*(b.v-a.v)),
	}
}

// clipLevel returns the part of the convex polygon pts whose value is at
// least lo when above is set, at most lo otherwise (Sutherland-Hodgman
// against one level, values interpolated along the edges).
func clipLevel(pts []gridPoint, lo float64, above bool) []gridPoint {
	in := func(p gridPoint) bool {
		if above {
			return p.v >= lo
		}
		return p.v <= lo
	}
	var out []gridPoint
	for i, a := range pts {
		b := pts[(i+1)%len(pts)]
		ia, ib := in(a), in(b)
		if ia {
			out = append(out, a)
		}
		if ia != ib {
			out = append(out, mix(a, b, (lo-a.v)/(b.v-a.v)))
		}
	}
	return out
}

// surfaceCell returns the four triangles of the cell whose corners are
// c (front left, front right, back right, back left), around its centre
// at the corners' mean value.
func surfaceCell(c [4]gridPoint) [4][3]gridPoint {
	m := gridPoint{
		u: (c[0].u + c[2].u) / 2,
		w: (c[0].w + c[2].w) / 2,
		v: (c[0].v + c[1].v + c[2].v + c[3].v) / 4,
	}
	return [4][3]gridPoint{{c[0], c[1], m}, {c[1], c[2], m}, {c[2], c[3], m}, {c[3], c[0], m}}
}

// surface3D lays out a surface group: per cell of its grid, the series i
// and i+1 by the categories j and j+1, the parts of the cell's four
// triangles in each band, filled with the band's fill; a wireframe draws
// the grid's lines and the lines between the bands instead, in the band's
// colour, a line between two bands in the lower one's. Each value is
// clamped to the axis. Office lights a surface; litho fills each band
// with one colour.
//
// # Corpus evidence
//
//   - litho/pptx: chart-surface: PowerPoint draws the 3-D surface and the
//     contour of six categories and five series over a grid whose points
//     stand on the category ticks and on the depth axis's five ticks, Y0
//     in front; the wireframe contour's lines between the bands are its
//     blue loop at 2 and its orange corners at 4.
func (rd *renderer) surface3D(p *projection, g *Group, x, y *dim) []solid {
	levels := rd.levels
	if levels == nil {
		levels = surfaceLevels(y)
	}
	rd.surfaceY = y
	if len(levels) < 2 || len(g.Series) < 2 {
		return nil
	}
	n := len(levels) - 1
	vals := make([][]float64, len(g.Series))
	for i, s := range g.Series {
		d := rd.data(s.Values)
		row := make([]float64, x.n)
		for j := range row {
			v, ok := d.number(j)
			if !ok || !numeric.Finite(v) {
				v = math.NaN()
			} else {
				v = y.sc.clamp(v)
			}
			row[j] = v
		}
		vals[i] = row
	}
	at := func(q gridPoint) typeset.Point {
		z, _ := p.row(0)
		z += float64(q.w * p.rowDepth)
		return p.at(x.valuePixel(q.u), y.valuePixel(q.v), z)
	}
	var ss []solid
	for i := 0; i+1 < len(vals); i++ {
		for j := 0; j+1 < x.n; j++ {
			c := [4]gridPoint{
				{float64(j), float64(i), vals[i][j]},
				{float64(j + 1), float64(i), vals[i][j+1]},
				{float64(j + 1), float64(i + 1), vals[i+1][j+1]},
				{float64(j), float64(i + 1), vals[i+1][j]},
			}
			if math.IsNaN(c[0].v) || math.IsNaN(c[1].v) || math.IsNaN(c[2].v) || math.IsNaN(c[3].v) {
				continue
			}
			z, _ := p.row(0)
			z += float64((float64(i) + 0.5) * p.rowDepth)
			sl := solid{z: z, cat: x.valuePixel(float64(j) + 0.5), val: y.valuePixel((c[0].v + c[1].v + c[2].v + c[3].v) / 4)}
			if g.Wireframe {
				sl.draw = func() { rd.wireCell(g, c, levels, n, at, i+2 == len(vals), j+2 == x.n) }
			} else {
				sl.draw = func() { rd.fillCell(g, c, levels, n, at) }
			}
			ss = append(ss, sl)
		}
	}
	return ss
}

// fillCell fills the parts of cell c in each band, a band's parts of the
// cell's four triangles as one path, so no seam shows between them.
func (rd *renderer) fillCell(g *Group, c [4]gridPoint, levels []float64, n int, at func(gridPoint) typeset.Point) {
	tris := surfaceCell(c)
	for k := range n {
		var ops []typeset.PathOp
		for _, tri := range tris {
			part := tri[:]
			if k > 0 {
				part = clipLevel(part, levels[k], true)
			}
			if k < n-1 {
				part = clipLevel(part, levels[k+1], false)
			}
			if len(part) < 3 {
				continue
			}
			pts := make([]typeset.Point, len(part))
			for m, q := range part {
				pts[m] = at(q)
			}
			ops = append(ops, polygon(pts...)...)
		}
		if len(ops) > 0 {
			rd.paint(ops, rd.bandFill(g, k, n), nil)
		}
	}
}

// wireCell draws the lines of cell c: its front and left edges, its back
// and right edges where it ends the grid, each cut into its bands, and
// the lines between the bands across its triangles.
func (rd *renderer) wireCell(g *Group, c [4]gridPoint, levels []float64, n int, at func(gridPoint) typeset.Point, back, right bool) {
	edges := [][2]gridPoint{{c[0], c[1]}, {c[3], c[0]}}
	if back {
		edges = append(edges, [2]gridPoint{c[2], c[3]})
	}
	if right {
		edges = append(edges, [2]gridPoint{c[1], c[2]})
	}
	for _, e := range edges {
		for k := range n {
			a, b, ok := segmentIn(e[0], e[1], levels[k], levels[k+1], k == 0, k == n-1)
			if !ok {
				continue
			}
			pa, pb := at(a), at(b)
			rd.add(line(pa.X, pa.Y, pb.X, pb.Y, rd.bandLine(g, k, n)))
		}
	}
	for _, tri := range surfaceCell(c) {
		for k := 1; k < n; k++ {
			if a, b, ok := isoSegment(tri, levels[k]); ok {
				pa, pb := at(a), at(b)
				rd.add(line(pa.X, pa.Y, pb.X, pb.Y, rd.bandLine(g, k-1, n)))
			}
		}
	}
}

// segmentIn returns the part of the segment from a to b whose value lies
// between lo and hi, the open ends taking all below lo or above hi, and
// whether it is longer than a point.
func segmentIn(a, b gridPoint, lo, hi float64, openLo, openHi bool) (gridPoint, gridPoint, bool) {
	t0, t1 := 0.0, 1.0
	dv := b.v - a.v
	bound := func(level float64, keepAbove bool) {
		if dv == 0 {
			if keepAbove && a.v < level || !keepAbove && a.v > level {
				t0, t1 = 1, 0
			}
			return
		}
		t := (level - a.v) / dv
		if keepAbove == (dv > 0) {
			t0 = math.Max(t0, t)
		} else {
			t1 = math.Min(t1, t)
		}
	}
	if !openLo {
		bound(lo, true)
	}
	if !openHi {
		bound(hi, false)
	}
	if t1-t0 <= 1e-9 {
		return a, b, false
	}
	return mix(a, b, t0), mix(a, b, t1), true
}

// isoSegment returns the segment of triangle tri along which its value is
// level, and whether the level crosses it.
func isoSegment(tri [3]gridPoint, level float64) (gridPoint, gridPoint, bool) {
	var pts []gridPoint
	for i := range 3 {
		a, b := tri[i], tri[(i+1)%3]
		if (a.v < level) != (b.v < level) {
			pts = append(pts, mix(a, b, (level-a.v)/(b.v-a.v)))
		}
	}
	if len(pts) != 2 {
		return gridPoint{}, gridPoint{}, false
	}
	return pts[0], pts[1], true
}

// surfaceBands returns the bounds of the bands of the surface group g and
// the value dimension that formats them. The legend lists the bands before
// the plot is laid out, and the plot's value axis depends on the room the
// legend leaves: a dry run of the layout without the legend finds the
// bands the legend first lists, a second one with that legend the axis
// the plot then takes, whose bands the legend lists and the plot cuts.
func (rd *renderer) surfaceBands(g *Group) ([]float64, *dim) {
	if rd.levels != nil {
		return rd.levels, rd.surfaceY
	}
	if len(rd.c.Groups) == 0 || rd.c.Groups[0] != g {
		return nil, nil
	}
	c := *rd.c
	c.Legend = nil
	first := &renderer{ctx: rd.ctx, fonts: rd.fonts, c: &c, page: rd.page, points: rd.points}
	first.render(rd.page)
	if first.surfaceY == nil {
		return nil, nil
	}
	second := &renderer{ctx: rd.ctx, fonts: rd.fonts, c: rd.c, page: rd.page, points: rd.points}
	second.levels, second.surfaceY = surfaceLevels(first.surfaceY), first.surfaceY
	second.render(rd.page)
	rd.levels, rd.surfaceY = surfaceLevels(second.surfaceY), second.surfaceY
	return rd.levels, rd.surfaceY
}
