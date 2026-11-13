package effect

import (
	"context"
	"fmt"
	"math"
	"math/bits"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// FillRule decides which points a filled path covers (ISO 32000-1,
// 8.5.3.3).
type FillRule uint8

const (
	// NonZero covers where the winding number is not zero.
	NonZero FillRule = iota
	// EvenOdd covers where the winding number is odd.
	EvenOdd
)

// Path is a path to cover: its construction steps in path units, filled
// by Rule when Width is 0, else stroked with that width in path units,
// with round joins and caps.
type Path struct {
	Ops   []typeset.PathOp
	Width float64
	Rule  FillRule
}

const (
	// subBits is the fractional bits of a fixed-point coordinate: 65,536
	// steps a pixel, so the rounding of a coordinate moves an area by far
	// less than a level.
	subBits = 16
	one     = 1 << subBits
	// fullCover is twice the area of a pixel in squared steps, the sum a
	// fully covered pixel accumulates.
	fullCover = 2 * one * one
)

// maxPixelCoordinate bounds a mapped coordinate, in pixels. Anti-DoS
// guard, not spec-mandated: sixteen times the longest side a surface can
// have covers every path that reaches a surface and keeps every
// coordinate within 37 bits in steps.
const maxPixelCoordinate = 1 << 20

// maxSide caps a surface's side for coverage, in pixels. Anti-DoS guard,
// not spec-mandated: 65,536 pixels are 18 feet at 300 pixels an inch,
// longer than any page, and keep the coverage arithmetic in range.
const maxSide = 1 << 16

// maxSegments caps the line segments one call covers, curves flattened.
// Anti-DoS guard, not spec-mandated: a million segments outline any shape
// a document draws hundreds of times over; past the cap the remaining
// path is left out, so a hostile path costs bounded time.
const maxSegments = 1 << 20

// maxCurveDepth caps the halvings of one cubic: at most 1024 segments a
// curve. Anti-DoS guard, not spec-mandated: 1024 segments keep a quarter
// circle of 4,000 pixels' radius within flatness of the curve and one of
// 65,536 pixels within a twentieth of a pixel.
const maxCurveDepth = 10

// flatUp is the extra bits a cubic is halved at, so its midpoints keep
// their fractions.
const flatUp = 6

// flatness is the largest second difference of a cubic's control
// polygon, in steps of 2^-22 pixel, under which it is drawn as its chord:
// 1/512 pixel, the curve then within 1/680 pixel of the chord, so the
// area a convex curve loses to its chords stays under a quarter level a
// pixel of its length.
const flatness = (one << flatUp) / 512

// maxStrokeVertices caps the polyline vertices of one stroked path.
// Anti-DoS guard, not spec-mandated: 65,536 vertices outline any stroke a
// document draws; past the cap the rest of the path is left out.
const maxStrokeVertices = 1 << 16

// bandCells is the cells of the accumulation band: rows are covered a
// band at a time so the work memory stays at 1 MiB whatever the surface.
const bandCells = 1 << 16

// maxArcSides caps the sides of the polygon a round join or cap is drawn
// on. Anti-DoS guard, not spec-mandated: 4096 sides keep the polygon
// within 1/512 pixel of a circle of 850 pixels' radius, wider than any
// stroke a page holds.
const maxArcSides = 4096

// Fill covers the coverage surface s with paths mapped by tr from path
// units to pixels, each path over the coverage before it (a union: a + b
// - ab). A filled path covers by its rule, the subpaths of one path
// summed by direction; a stroke's width scales by the square root of
// tr's determinant. A path with a coordinate that is not finite or maps
// past 2^20 pixels is skipped.
//
// The coverage of a filled pixel is the area of the path within it,
// accumulated in integers on a grid of 1/65,536 pixel: within a level of
// the exact area, and the same on every machine. Curves are cut into
// chords within 1/680 pixel. A stroke is filled as its outline, its sides
// offset from the path and joined and capped by arcs: its edges are as
// exact, but for a pixel or two at a sharp inner corner, where the two
// offsets overlap and their coverages add.
func Fill(ctx context.Context, s *Surface, paths []Path, tr typeset.Matrix) error {
	if err := want("fill", s, Coverage); err != nil {
		return err
	}
	if s.W > maxSide || s.H > maxSide {
		return fmt.Errorf("%w: %d by %d pixels to cover", ErrTooLarge, s.W, s.H)
	}
	z := &rasterizer{ctx: ctx, w: s.W, h: s.H}
	defer z.sc.release()
	if err := z.sc.take(ctx, 2*8*bandCells); err != nil {
		return err
	}
	z.cover = make([]int64, bandCells)
	z.area = make([]int64, bandCells)
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		z.edges = z.edges[:0]
		rule := p.Rule
		if p.Width > 0 {
			hw, ok := halfWidth(p.Width, tr)
			if !ok {
				continue
			}
			lines, ok := z.polylines(p.Ops, tr)
			if !ok {
				continue
			}
			z.outline(lines, hw)
			rule = NonZero
		} else if !z.fill(p.Ops, tr) {
			continue
		}
		if z.err != nil {
			return z.err
		}
		if err := z.render(rule, s.Pix); err != nil {
			return err
		}
	}
	return nil
}

// fx is a point in fixed point, 1/65,536 pixel a step.
type fx struct{ x, y int64 }

// edge is a line segment going down, y0 < y1, dir +1 when the path went
// down along it and -1 when it went up.
type edge struct {
	x0, y0, x1, y1, dir int64
}

// rasterizer accumulates the signed area of edges into cells, a band of
// rows at a time. It lives for one call of [Fill] and holds that call's
// context, which it does not outlive.
type rasterizer struct {
	ctx   context.Context
	err   error
	w, h  int
	edges []edge
	cover []int64
	area  []int64
	// lo and hi are the first and last cell each row of the band touched.
	lo, hi []int
	// band is the rows of a band.
	band  int
	total int
	sc    scratch
}

// halfWidth returns half the stroke width w maps to through tr, in steps.
func halfWidth(w float64, tr typeset.Matrix) (int64, bool) {
	det := math.Abs(float64(tr.A*tr.D) - float64(tr.B*tr.C))
	hw := float64(w*math.Sqrt(det)) / 2
	if !numeric.PositiveFinite(hw) || hw > maxPixelCoordinate {
		return 0, false
	}
	return int64(math.Round(hw * one)), true // fits int64: at most 2^36, checked above
}

// mapPoint returns p mapped by tr, in steps; false when it is not finite
// or lands past maxPixelCoordinate.
func mapPoint(tr typeset.Matrix, p typeset.Point) (fx, bool) {
	x := float64(float64(tr.A*p.X)+float64(tr.C*p.Y)) + tr.E
	y := float64(float64(tr.B*p.X)+float64(tr.D*p.Y)) + tr.F
	if !(math.Abs(x) <= maxPixelCoordinate && math.Abs(y) <= maxPixelCoordinate) {
		return fx{}, false
	}
	return fx{int64(math.Round(x * one)), int64(math.Round(y * one))}, true // fits int64: at most 2^36, checked above
}

// mapAll maps every point of ops; false when one fails.
func mapAll(ops []typeset.PathOp, tr typeset.Matrix) ([][3]fx, bool) {
	out := make([][3]fx, len(ops))
	for i, op := range ops {
		n := 0
		switch op.Kind {
		case typeset.MoveTo, typeset.LineTo:
			n = 1
		case typeset.CurveTo:
			n = 3
		case typeset.ClosePath:
		}
		for j := range n {
			q, ok := mapPoint(tr, op.P[j])
			if !ok {
				return nil, false
			}
			out[i][j] = q
		}
	}
	return out, true
}

// fill adds the edges of ops' subpaths, each closed; false when a point
// does not map. A LineTo or CurveTo before any MoveTo is skipped.
func (z *rasterizer) fill(ops []typeset.PathOp, tr typeset.Matrix) bool {
	pts, ok := mapAll(ops, tr)
	if !ok {
		return false
	}
	var start, pen fx
	open := false
	for i, op := range ops {
		switch op.Kind {
		case typeset.MoveTo:
			if open {
				z.line(pen, start)
			}
			start, pen, open = pts[i][0], pts[i][0], true
		case typeset.LineTo:
			if open {
				z.line(pen, pts[i][0])
				pen = pts[i][0]
			}
		case typeset.CurveTo:
			if open {
				z.cubic(pen, pts[i][0], pts[i][1], pts[i][2], func(a, b fx) { z.line(a, b) })
				pen = pts[i][2]
			}
		case typeset.ClosePath:
			if open {
				z.line(pen, start)
				open = false
			}
		}
	}
	if open {
		z.line(pen, start)
	}
	return true
}

// edgeBytes is what one edge costs the budget.
const edgeBytes = 40

// line adds the segment from a to b, unless horizontal, past the segment
// cap or refused by the budget, the refusal kept in z.err.
func (z *rasterizer) line(a, b fx) {
	if a.y == b.y || z.total >= maxSegments || z.err != nil {
		return
	}
	if len(z.edges) == cap(z.edges) {
		n := max(2*cap(z.edges), 256)
		if err := z.sc.take(z.ctx, int64(n-cap(z.edges))*edgeBytes); err != nil {
			z.err = err
			return
		}
		z.edges = append(make([]edge, 0, n), z.edges...)
	}
	z.total++
	if a.y < b.y {
		z.edges = append(z.edges, edge{a.x, a.y, b.x, b.y, 1})
	} else {
		z.edges = append(z.edges, edge{b.x, b.y, a.x, a.y, -1})
	}
}

// cubic flattens the cubic from p0 through p1 and p2 to p3 into chords,
// passing each to emit, by halving it until its control polygon is flat
// (de Casteljau, in integers at 64 times the step's precision).
func (z *rasterizer) cubic(p0, p1, p2, p3 fx, emit func(a, b fx)) {
	sh := func(p fx) fx { return fx{p.x << flatUp, p.y << flatUp} }
	down := func(p fx) fx {
		return fx{(p.x + 1<<(flatUp-1)) >> flatUp, (p.y + 1<<(flatUp-1)) >> flatUp}
	}
	prev := p0
	var rec func(a, b, c, d fx, depth int)
	rec = func(a, b, c, d fx, depth int) {
		if depth >= maxCurveDepth || flat(a, b, c, d) {
			q := down(d)
			emit(prev, q)
			prev = q
			return
		}
		mid := func(u, v fx) fx { return fx{(u.x + v.x) >> 1, (u.y + v.y) >> 1} }
		ab, bc, cd := mid(a, b), mid(b, c), mid(c, d)
		abc, bcd := mid(ab, bc), mid(bc, cd)
		m := mid(abc, bcd)
		rec(a, ab, abc, m, depth+1)
		rec(m, bcd, cd, d, depth+1)
	}
	rec(sh(p0), sh(p1), sh(p2), sh(p3), 0)
	if prev != p3 {
		emit(prev, p3)
	}
}

// flat reports whether the second differences of a cubic's control
// polygon are all within flatness.
func flat(a, b, c, d fx) bool {
	return abs64(a.x-2*b.x+c.x) <= flatness && abs64(a.y-2*b.y+c.y) <= flatness &&
		abs64(b.x-2*c.x+d.x) <= flatness && abs64(b.y-2*c.y+d.y) <= flatness
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// polyline is a flattened subpath; closed joins its last point to its
// first.
type polyline struct {
	pts    []fx
	closed bool
}

// polylines flattens ops mapped by tr into polylines of at most
// maxStrokeVertices points together; false when a point does not map.
func (z *rasterizer) polylines(ops []typeset.PathOp, tr typeset.Matrix) ([]polyline, bool) {
	pts, ok := mapAll(ops, tr)
	if !ok {
		return nil, false
	}
	var out []polyline
	cur := -1
	total := 0
	add := func(p fx) {
		if cur >= 0 && total < maxStrokeVertices {
			out[cur].pts = append(out[cur].pts, p)
			total++
		}
	}
	for i, op := range ops {
		switch op.Kind {
		case typeset.MoveTo:
			out = append(out, polyline{})
			cur = len(out) - 1
			add(pts[i][0])
		case typeset.LineTo:
			add(pts[i][0])
		case typeset.CurveTo:
			if cur < 0 || len(out[cur].pts) == 0 {
				continue
			}
			p0 := out[cur].pts[len(out[cur].pts)-1]
			z.cubic(p0, pts[i][0], pts[i][1], pts[i][2], func(_, b fx) { add(b) })
		case typeset.ClosePath:
			if cur >= 0 {
				out[cur].closed = true
				cur = -1
			}
		}
	}
	return out, true
}

// outline adds the outline of the stroke of lines, hw steps each side,
// with round joins and caps, as closed polygons whose nonzero fill is the
// stroke: each side's offset segments, joined on the outer side of a turn
// by an arc about the vertex and on the inner side at the point where
// the two offsets cross. An open polyline is one polygon closed by its
// two caps, a closed one two polygons, one a side; a polyline of one
// point is a disc.
func (z *rasterizer) outline(lines []polyline, hw int64) {
	disc := discPoints(hw)
	var ring []fx
	for _, l := range lines {
		pts := dedupe(l.pts)
		n := len(pts)
		if n == 0 {
			continue
		}
		if n == 1 {
			ring = ring[:0]
			for _, o := range disc {
				ring = append(ring, fx{pts[0].x + o.x, pts[0].y + o.y})
			}
			z.closed(ring)
			continue
		}
		closed := l.closed && n > 2
		if closed && pts[0] == pts[n-1] {
			pts = pts[:n-1]
		}
		back := slices.Clone(pts)
		slices.Reverse(back)
		if closed {
			ring = side(ring[:0], pts, disc, hw, true)
			z.closed(ring)
			ring = side(ring[:0], back, disc, hw, true)
			z.closed(ring)
			continue
		}
		ring = side(ring[:0], pts, disc, hw, false)
		ring = side(ring, back, disc, hw, false)
		z.closed(ring)
	}
}

// normal returns the vector (dx, dy) turned a quarter toward increasing
// angle and scaled to length hw, in floating point whose every product is
// rounded on its own, so it is the same on every machine.
func normal(dx, dy, hw int64) fx {
	fdx, fdy := float64(dx), float64(dy)
	d := math.Sqrt(float64(fdx*fdx) + float64(fdy*fdy))
	k := float64(hw) / d
	return fx{int64(math.Round(float64(-fdy * k))), int64(math.Round(float64(fdx * k)))} // fits int64: at most hw
}

// side appends the left side of the polyline pts, offset hw steps along
// each segment's normal, walking forward, with its joins, and for an
// open polyline the cap at its end: arcs drawn through the points of
// disc.
func side(ring, pts, disc []fx, hw int64, closed bool) []fx {
	n := len(pts)
	segs := n - 1
	if closed {
		segs = n
	}
	d := make([]fx, segs)
	nrm := make([]fx, segs)
	s := make([]fx, segs)
	e := make([]fx, segs)
	for i := range segs {
		a, b := pts[i], pts[(i+1)%n]
		d[i] = fx{b.x - a.x, b.y - a.y}
		nrm[i] = normal(d[i].x, d[i].y, hw)
		s[i] = fx{a.x + nrm[i].x, a.y + nrm[i].y}
		e[i] = fx{b.x + nrm[i].x, b.y + nrm[i].y}
	}
	// The offsets turn toward increasing angle where the walk turns
	// toward their side, and there the two segments' offsets overlap: the
	// inner side, joined where they cross, or through the vertex when they
	// cross past the end of a segment. Elsewhere they part, and an arc
	// turning against the walk's turn closes the gap.
	pivot := make([]bool, segs)
	joins := segs - 1
	if closed {
		joins = segs
	}
	for i := range joins {
		j := (i + 1) % segs
		u, w := nrm[i], nrm[j]
		if u == w || cross(u, w) <= 0 {
			continue
		}
		if x, ok := miter(pts[j], u, w, d[i], d[j], hw); ok {
			e[i], s[j] = x, x
		} else {
			pivot[j] = true
		}
	}
	for i := range segs {
		if i > 0 || closed {
			k := (i + segs - 1) % segs
			switch {
			case pivot[i]:
				ring = append(ring, pts[i])
			case nrm[k] != nrm[i] && cross(nrm[k], nrm[i]) <= 0:
				ring = arc(ring, pts[i], nrm[k], nrm[i], -1, disc)
			}
		}
		ring = append(ring, s[i], e[i])
	}
	if !closed {
		last := nrm[segs-1]
		ring = arc(ring, pts[n-1], last, fx{-last.x, -last.y}, -1, disc)
	}
	return ring
}

// miter returns the point where the offsets u and w of the segments
// arriving at v (along a) and leaving it (along b) cross, both of length
// hw on the inner side of the turn; false when it lies past the end of
// either segment, the turn too sharp for their length.
func miter(v, u, w, a, b fx, hw int64) (fx, bool) {
	h2 := float64(float64(hw) * float64(hw))
	dot := float64(float64(u.x)*float64(w.x)) + float64(float64(u.y)*float64(w.y))
	if h2+dot <= 0 {
		return fx{}, false
	}
	f := h2 / (h2 + dot)
	mx, my := float64(float64(u.x+w.x)*f), float64(float64(u.y+w.y)*f)
	within := func(d fx) bool {
		dx, dy := float64(d.x), float64(d.y)
		return math.Abs(float64(mx*dx)+float64(my*dy)) <= float64(dx*dx)+float64(dy*dy)
	}
	if !within(a) || !within(b) {
		return fx{}, false
	}
	return fx{v.x + int64(math.Round(mx)), v.y + int64(math.Round(my))}, true // fits int64: within the segments' reach
}

// cross returns the sign of the cross product of a and b, in floating
// point rounded product by product, the same on every machine.
func cross(a, b fx) float64 {
	return float64(float64(a.x)*float64(b.y)) - float64(float64(a.y)*float64(b.x))
}

// arc appends the points of the circle about c from c+u to c+w, turning
// by dir (+1 toward increasing angle, -1 the other way), u and w of the
// disc's radius: c+u, the disc's points strictly between, c+w.
func arc(ring []fx, c, u, w fx, dir int64, disc []fx) []fx {
	ring = append(ring, fx{c.x + u.x, c.y + u.y})
	n := len(disc)
	nearest := func(v fx) int {
		k, top := 0, math.Inf(-1)
		for i, d := range disc {
			if p := float64(float64(d.x)*float64(v.x)) + float64(float64(d.y)*float64(v.y)); p > top {
				k, top = i, p
			}
		}
		return k
	}
	fd := float64(dir)
	k, kw := nearest(u), nearest(w)
	for range n + 1 {
		d := disc[k]
		if fd*cross(u, d) > 0 && fd*cross(d, w) > 0 {
			ring = append(ring, fx{c.x + d.x, c.y + d.y})
		}
		if k == kw {
			break
		}
		k = (k + int(dir) + n) % n // fits int: dir is 1 or -1
	}
	return append(ring, fx{c.x + w.x, c.y + w.y})
}

// dedupe returns pts without the points equal to the one before them.
func dedupe(pts []fx) []fx {
	out := pts[:0:0]
	for i, p := range pts {
		if i == 0 || p != pts[i-1] {
			out = append(out, p)
		}
	}
	return out
}

// closed adds the closed polygon q's edges as they wind.
func (z *rasterizer) closed(q []fx) {
	for i := range q {
		z.line(q[i], q[(i+1)%len(q)])
	}
}

// discPoints returns the points of a regular polygon inscribed in the
// circle of radius r steps about the origin, its sides close enough that
// the polygon stays within 1/512 pixel of the circle: n sides with
// 1 - cos(pi/n) <= 1/(512 r), at least 8 and at most maxArcSides. The
// sines come from [Sincos], the same on every machine.
func discPoints(r int64) []fx {
	rp := float64(r) / one
	n := int(min(max(math.Ceil(float64(math.Pi*math.Sqrt(float64(256*rp)))), 8), maxArcSides)) // fits int: within [8, maxArcSides]
	out := make([]fx, n)
	fr := float64(r)
	for k := range n {
		s, c := Sincos(float64(360*k) / float64(n))
		out[k] = fx{int64(math.Round(float64(fr * c))), int64(math.Round(float64(fr * s)))} // fits int64: at most r
	}
	return out
}

// render accumulates the edges over the surface band by band, turns the
// cells into coverage by rule and puts it over pix as a union.
func (z *rasterizer) render(rule FillRule, pix []uint8) error {
	slices.SortFunc(z.edges, func(a, b edge) int {
		switch {
		case a.y0 < b.y0:
			return -1
		case a.y0 > b.y0:
			return 1
		}
		return 0
	})
	z.band = max(1, bandCells/z.w)
	if len(z.lo) < z.band {
		z.lo, z.hi = make([]int, z.band), make([]int, z.band)
	}
	for i := range z.band {
		z.lo[i], z.hi[i] = z.w, -1
	}
	next := 0
	var active []int
	for top := 0; top < z.h; top += z.band {
		if err := z.ctx.Err(); err != nil {
			return err
		}
		bot := min(top+z.band, z.h)
		ytop, ybot := int64(top)*one, int64(bot)*one
		for next < len(z.edges) && z.edges[next].y0 < ybot {
			active = append(active, next)
			next++
		}
		kept := active[:0]
		for _, i := range active {
			e := &z.edges[i]
			if e.y1 <= ytop {
				continue
			}
			kept = append(kept, i)
			z.walk(e, ytop, ybot, top)
		}
		active = kept
		z.sweep(pix, top, bot, rule)
	}
	return nil
}

// walk adds the part of e between ytop and ybot to the band starting at
// row top, row by row.
func (z *rasterizer) walk(e *edge, ytop, ybot int64, top int) {
	ys, ye := max(e.y0, ytop), min(e.y1, ybot)
	for ys < ye {
		r := ys >> subBits
		yn := min(ye, (r+1)*one)
		z.piece(int(r)-top, e.xAt(ys), ys-r*one, e.xAt(yn), yn-r*one, e.dir) // fits int: a row of the surface
		ys = yn
	}
}

// xAt returns the x of e at y, y0 <= y <= y1, to the nearest step.
func (e *edge) xAt(y int64) int64 {
	return e.x0 + mulDiv(y-e.y0, e.x1-e.x0, e.y1-e.y0)
}

// mulDiv returns a*b/c to the nearest integer, halves away from zero,
// for c > 0 and |a| <= c, in 128-bit arithmetic: the product of two
// coordinates passes 64 bits.
func mulDiv(a, b, c int64) int64 {
	neg := (a < 0) != (b < 0)
	hi, lo := bits.Mul64(uint64(abs64(a)), uint64(abs64(b))) //nolint:gosec // magnitudes of coordinates within 2^38
	uc := uint64(c)                                          //nolint:gosec // c is positive
	var carry uint64
	lo, carry = bits.Add64(lo, uc/2, 0)
	hi += carry
	if hi >= uc {
		// Unreachable when |a| <= c: the quotient fits 64 bits.
		return 0
	}
	q, _ := bits.Div64(hi, lo, uc)
	if neg {
		return -int64(q) //nolint:gosec // at most |b|, within 2^38
	}
	return int64(q) //nolint:gosec // at most |b|, within 2^38
}

// piece adds a segment within one row, from (xa, ya) to (xb, yb), ya
// and yb in steps from the row's top, to the row's cells: each cell it
// crosses gains its height as cover and its height times the sum of its
// two x offsets in the cell as area (twice the trapezoid it leaves to
// its left). A part left of the surface counts in cell 0 at offset 0; a
// part right of it is dropped.
func (z *rasterizer) piece(row int, xa, ya, xb, yb, dir int64) {
	if xa > xb {
		xa, ya, xb, yb = xb, yb, xa, ya
	}
	width := int64(z.w) * one
	if xa >= width {
		return
	}
	cells := z.cover[row*z.w : (row+1)*z.w]
	areas := z.area[row*z.w : (row+1)*z.w]
	touch := func(c int64) {
		z.lo[row], z.hi[row] = min(z.lo[row], int(c)), max(z.hi[row], int(c)) // fits int: a cell of the row
	}
	if xb <= 0 {
		cells[0] += dir * abs64(yb-ya)
		touch(0)
		return
	}
	// The y of the segment at x, from its two ends, which stay fixed
	// while the segment is cut so every cut lands on the one line.
	x0, y0, dx, dy := xa, ya, xb-xa, yb-ya
	yAt := func(x int64) int64 { return y0 + mulDiv(x-x0, dy, dx) }
	if xa < 0 {
		y := yAt(0)
		cells[0] += dir * abs64(y-ya)
		touch(0)
		xa, ya = 0, y
	}
	if xb > width {
		xb, yb = width, yAt(width)
	}
	if xa == xb {
		c := xa >> subBits
		if c >= int64(z.w) {
			return
		}
		h := dir * abs64(yb-ya)
		cells[c] += h
		areas[c] += h * 2 * (xa - c*one)
		touch(c)
		return
	}
	px, py := xa, ya
	for px < xb {
		c := px >> subBits
		nx := min((c+1)*one, xb)
		ny := yb
		if nx < xb {
			ny = yAt(nx)
		}
		h := dir * abs64(ny-py)
		cells[c] += h
		areas[c] += h * (px - c*one + nx - c*one)
		touch(c)
		px, py = nx, ny
	}
}

// sweep turns the band's cells, rows top to bot, into coverage by rule,
// puts it over pix as a union, and clears the cells.
func (z *rasterizer) sweep(pix []uint8, top, bot int, rule FillRule) {
	for r := top; r < bot; r++ {
		i := r - top
		lo, hi := z.lo[i], z.hi[i]
		z.lo[i], z.hi[i] = z.w, -1
		if hi < 0 {
			continue
		}
		cells := z.cover[i*z.w : (i+1)*z.w]
		areas := z.area[i*z.w : (i+1)*z.w]
		dst := pix[r*z.w : (r+1)*z.w]
		var acc int64
		for x := lo; x <= hi; x++ {
			v := 2*one*(acc+cells[x]) - areas[x]
			acc += cells[x]
			cells[x], areas[x] = 0, 0
			if c := level(v, rule); c != 0 {
				dst[x] = union(dst[x], c)
			}
		}
		if c := level(2*one*acc, rule); c != 0 {
			for x := hi + 1; x < z.w; x++ {
				dst[x] = union(dst[x], c)
			}
		}
	}
}

// level returns the coverage byte of the twice-area sum v by rule.
func level(v int64, rule FillRule) uint8 {
	if v < 0 {
		v = -v
	}
	if rule == EvenOdd {
		v %= 2 * fullCover
		if v > fullCover {
			v = 2*fullCover - v
		}
	} else if v > fullCover {
		v = fullCover
	}
	return uint8((v*255 + fullCover/2) / fullCover) //nolint:gosec // at most 255: v is at most fullCover
}

// union returns a + b - ab, in bytes, rounded.
func union(a, b uint8) uint8 {
	x, y := uint32(a), uint32(b)
	return uint8(x + y - (x*y+127)/255) //nolint:gosec // a union of two bytes is a byte
}
