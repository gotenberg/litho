package shade

import (
	"context"
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// Bounds on the work of subdividing patches. A patch is flattened into a
// grid of cells, two triangles each, sized from its device extent.
//
// patchCellPixels is the target cell side: four pixels keeps the chord of a
// curved edge within a small fraction of a pixel of the curve for any patch
// whose edges bend less than a semicircle per cell row.
//
// maxPatchGrid caps the cells per patch side, and maxPatchCells the cells
// over the whole mesh: a file can declare a patch spanning the page or
// hundreds of thousands of patches, and the grid of each grows with the
// square of its size. Anti-DoS, not spec-mandated: ISO 32000-1, 8.7.4.5.7
// leaves the subdivision to the renderer. 64 cells per side is 16 pixels
// per cell on a 1024-pixel patch; 1<<22 cells is 8 million triangles.
//
// # Corpus evidence
//
//   - All corpora: the largest patch mesh holds 1,455 patches, which the
//     cell budget still grants a 53-cell side each.
const (
	patchCellPixels = 4
	maxPatchGrid    = 64
	maxPatchCells   = 1 << 22
)

// checkCtxPixels is how many pixels a mesh rasterises between context
// checks. Mesh rasterisation costs the sum of its triangles' areas, which
// a file can make large with overlapping triangles: the deadline the caller
// sets through ctx is what bounds it.
const checkCtxPixels = 1 << 16

// Bounds on the pixels a mesh may visit while it is rasterised. A triangle
// costs the pixels of its bounding box inside the paint area, so a file can
// make one paint cost the area many times over with overlapping or sliver
// triangles, and a deadline is the only other bound. The budget is
// meshOverdraw times the pixels of the area the mesh reaches, and never less
// than minMeshWork; past it painting fails with [ErrTooLarge]. Anti-DoS, not
// spec-mandated: ISO 32000-1, 8.7.4.5.5 to 8.7.4.5.8 put no bound on the
// overlap of a mesh's primitives.
//
// A mesh that tiles its area visits each pixel about twice, the bounding box
// of a triangle being twice its area, and a patch mesh's cells, four pixels
// a side, about the same. 256 leaves room for meshes that overlap themselves
// a hundred times; minMeshWork, 4 million pixels, lets a mesh over a small
// area draw slivers freely.
const (
	meshOverdraw = 256
	minMeshWork  = 1 << 22
)

// insideTolerance widens the barycentric inside test so that a pixel centre
// on an edge two triangles share is never missed by both through rounding.
const insideTolerance = 1e-9

// Converter converts colour components in a shading's colour space to sRGB
// channels in [0, 1]. The integrator implements it over the colour space
// [Shading.ColorSpace] names; comps holds [Shading.Components] values and is
// reused across calls.
type Converter interface {
	RGB(comps []float64) (r, g, b float32)
}

// Target is where a shading is painted.
type Target struct {
	// Canvas receives the shading, composited with the Normal blend mode.
	Canvas *surface.Canvas
	// Clip, when not nil, has the canvas's size and scales the paint pixel
	// by pixel: the clip for the sh operator, the shape's coverage times
	// the clip for a pattern fill.
	Clip *surface.Mask
	// Opacity is the constant alpha (ISO 32000-1, 11.6.4.4). The zero value
	// paints opaque.
	Opacity surface.Opacity
	// Convert maps the shading's colour components to sRGB.
	Convert Converter
}

// Paint paints the shading as the sh operator does (ISO 32000-1, 8.7.4.2):
// over the whole clip, through toDevice, the matrix from shading space to
// device space. /Background is ignored, as Table 78 requires of sh; /BBox
// clips.
//
// A singular toDevice paints nothing.
func (s *Shading) Paint(ctx context.Context, t Target, toDevice geom.Matrix) error {
	return s.paint(ctx, t, toDevice, false)
}

// region is a half-open pixel rectangle.
type region struct{ x0, y0, x1, y1 int }

func (r region) empty() bool { return r.x0 >= r.x1 || r.y0 >= r.y1 }

// painter carries one paint call.
type painter struct {
	s        *Shading
	t        Target
	toDevice geom.Matrix
	inv      geom.Matrix // device to shading space
	area     region

	rgbRamp []float32 // rampSize RGB triples
	hasBG   bool
	bg      [3]float32
	comps   []float64 // scratch, ncomp values
}

func (s *Shading) paint(ctx context.Context, t Target, toDevice geom.Matrix, withBackground bool) error {
	if t.Canvas == nil || t.Convert == nil {
		return fmt.Errorf("%w: a canvas and a converter are required", ErrTarget)
	}
	if t.Clip != nil && (t.Clip.Width != t.Canvas.Width || t.Clip.Height != t.Canvas.Height) {
		return fmt.Errorf("%w: clip is %dx%d, canvas %dx%d", ErrTarget,
			t.Clip.Width, t.Clip.Height, t.Canvas.Width, t.Canvas.Height)
	}
	if t.Opacity.Transparent() {
		return nil
	}
	inv, invertible := toDevice.Invert()
	if !invertible {
		return nil
	}
	p := &painter{s: s, t: t, toDevice: toDevice, inv: inv, area: clipBounds(t), comps: make([]float64, s.ncomp)}
	if p.area.empty() {
		return nil
	}
	if withBackground && s.background != nil {
		p.hasBG = true
		copy(p.comps, s.background)
		p.bg[0], p.bg[1], p.bg[2] = t.Convert.RGB(p.comps)
	}
	if s.ramp != nil {
		p.rgbRamp = make([]float32, 3*rampSize)
		for i := range rampSize {
			copy(p.comps, s.ramp[i*s.ncomp:(i+1)*s.ncomp])
			p.rgbRamp[3*i], p.rgbRamp[3*i+1], p.rgbRamp[3*i+2] = t.Convert.RGB(p.comps)
		}
	}

	switch s.kind {
	case 1:
		return p.paintFunctionBased(ctx)
	case 2:
		return p.paintPerPixel(ctx, p.axial)
	case 3:
		return p.paintPerPixel(ctx, p.radial)
	}
	return p.paintMesh(ctx)
}

// clipBounds returns the canvas rectangle holding every pixel the clip lets
// through, the whole canvas without a clip.
func clipBounds(t Target) region {
	if t.Clip == nil {
		return region{0, 0, t.Canvas.Width, t.Canvas.Height}
	}
	r := region{t.Clip.Width, t.Clip.Height, 0, 0}
	for y := range t.Clip.Height {
		row := t.Clip.A[y*t.Clip.Width : (y+1)*t.Clip.Width]
		for x, v := range row {
			if v > 0 {
				r.x0, r.x1 = min(r.x0, x), max(r.x1, x+1)
				r.y0, r.y1 = min(r.y0, y), max(r.y1, y+1)
			}
		}
	}
	return r
}

// coverage returns the paint's alpha at pixel (x, y).
func (p *painter) coverage(x, y int) float32 {
	a := p.t.Opacity.Alpha()
	if p.t.Clip != nil {
		a *= p.t.Clip.A[y*p.t.Clip.Width+x]
	}
	return a
}

// inBBox reports whether the shading-space point q lies inside /BBox, or
// true when there is none.
func (p *painter) inBBox(q geom.Point) bool {
	if !p.s.hasBBox {
		return true
	}
	b := p.s.bbox
	return q.X >= b.Min.X && q.X <= b.Max.X && q.Y >= b.Min.Y && q.Y <= b.Max.Y
}

// sampler returns the colour at the shading-space point q, and false where
// the shading paints nothing.
type sampler func(q geom.Point) (r, g, b float32, ok bool)

// paintPerPixel samples every pixel centre of the area in shading space.
// Cost: one inverse transform and one sample per pixel.
func (p *painter) paintPerPixel(ctx context.Context, sample sampler) error {
	for y := p.area.y0; y < p.area.y1; y++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("painting shading: %w", err)
		}
		for x := p.area.x0; x < p.area.x1; x++ {
			cov := p.coverage(x, y)
			if cov <= 0 {
				continue
			}
			q := p.inv.Apply(geom.Point{X: float64(x) + 0.5, Y: float64(y) + 0.5})
			r, g, b, ok := float32(0), float32(0), float32(0), false
			if p.inBBox(q) {
				r, g, b, ok = sample(q)
			}
			if !ok {
				if !p.hasBG {
					continue
				}
				r, g, b = p.bg[0], p.bg[1], p.bg[2]
			}
			p.t.Canvas.BlendOver(x, y, r, g, b, cov)
		}
	}
	return nil
}

// paintFunctionBased paints a type 1 shading (8.7.4.5.2): /Matrix maps the
// /Domain rectangle into shading space, and the function is evaluated at
// every pixel's preimage inside it. Nothing is painted outside /Domain.
// The two-input function cannot be tabulated in one dimension, so each
// pixel costs a function evaluation and a conversion.
func (p *painter) paintFunctionBased(ctx context.Context) error {
	toDomain, invertible := p.s.matrix.Invert()
	if !invertible {
		if !p.hasBG {
			return nil
		}
		return p.paintPerPixel(ctx, func(geom.Point) (float32, float32, float32, bool) { return 0, 0, 0, false })
	}
	d := p.s.domain
	xlo, xhi := min(d[0], d[1]), max(d[0], d[1])
	ylo, yhi := min(d[2], d[3]), max(d[2], d[3])
	in := make([]float64, 2)
	var evalErr error
	err := p.paintPerPixel(ctx, func(q geom.Point) (float32, float32, float32, bool) {
		u := toDomain.Apply(q)
		if evalErr != nil || u.X < xlo || u.X > xhi || u.Y < ylo || u.Y > yhi {
			return 0, 0, 0, false
		}
		in[0], in[1] = u.X, u.Y
		if evalErr = p.s.evalFuncs(in, p.comps); evalErr != nil {
			return 0, 0, 0, false
		}
		r, g, b := p.t.Convert.RGB(p.comps)
		return r, g, b, true
	})
	if err != nil {
		return err
	}
	if evalErr != nil {
		return fmt.Errorf("painting function-based shading: %w", evalErr)
	}
	return nil
}

// axial samples a type 2 shading (8.7.4.5.3, Table 80): the parameter s of
// the projection of q onto the axis, clamped to [0, 1] where /Extend
// extends that end and unpainted beyond an end it does not extend, maps to
// t = t0 + s(t1 - t0). An axis of zero length paints nothing.
func (p *painter) axial(q geom.Point) (r, g, b float32, ok bool) {
	c := p.s.coords
	dx, dy := c[2]-c[0], c[3]-c[1]
	den := dx*dx + dy*dy
	if den == 0 {
		return 0, 0, 0, false
	}
	s := ((q.X-c[0])*dx + (q.Y-c[1])*dy) / den
	switch {
	case s < 0:
		if !p.s.extend[0] {
			return 0, 0, 0, false
		}
		s = 0
	case s > 1:
		if !p.s.extend[1] {
			return 0, 0, 0, false
		}
		s = 1
	}
	r, g, b = p.rampAt(p.s.t0 + s*(p.s.t1-p.s.t0))
	return r, g, b, true
}

// radial samples a type 3 shading (8.7.4.5.4, Table 81). The shading is the
// family of circles with centre c(s) = c0 + s(c1 - c0) and radius
// r(s) = r0 + s(r1 - r0), each painted in the colour of t = t0 + s(t1 - t0),
// and a circle with a larger s paints over one with a smaller s. So q takes
// the colour of the largest s whose circle passes through it with a
// non-negative radius, s limited to [0, 1] except at an extended end, where
// the circles beyond keep the end's colour.
//
// |q - c(s)| = r(s) is the quadratic a s² - 2b s + c = 0 with
// a = |c1 - c0|² - (r1 - r0)², b = (q - c0)·(c1 - c0) + r0 (r1 - r0) and
// c = |q - c0|² - r0², solved for its larger root first.
func (p *painter) radial(q geom.Point) (r, g, b float32, ok bool) {
	c := p.s.coords
	cdx, cdy, dr := c[3]-c[0], c[4]-c[1], c[5]-c[2]
	pdx, pdy := q.X-c[0], q.Y-c[1]
	qa := cdx*cdx + cdy*cdy - dr*dr
	qb := pdx*cdx + pdy*cdy + c[2]*dr
	qc := pdx*pdx + pdy*pdy - c[2]*c[2]

	valid := func(s float64) bool {
		if math.IsNaN(s) || c[2]+s*dr < 0 {
			return false
		}
		return (s >= 0 || p.s.extend[0]) && (s <= 1 || p.s.extend[1])
	}
	s := math.NaN()
	if qa == 0 {
		// The radius grows exactly as fast as the centre moves: one root.
		if qb != 0 && valid(qc/(2*qb)) {
			s = qc / (2 * qb)
		}
	} else {
		disc := qb*qb - qa*qc
		if disc < 0 {
			return 0, 0, 0, false
		}
		sq := math.Sqrt(disc)
		hi, lo := (qb+sq)/qa, (qb-sq)/qa
		if hi < lo {
			hi, lo = lo, hi
		}
		switch {
		case valid(hi):
			s = hi
		case valid(lo):
			s = lo
		}
	}
	if math.IsNaN(s) {
		return 0, 0, 0, false
	}
	s = min(max(s, 0), 1)
	r, g, b = p.rampAt(p.s.t0 + s*(p.s.t1-p.s.t0))
	return r, g, b, true
}

// rampAt interpolates the converted ramp at parameter t, clamped to the
// ramp's range as the function clamps its input to /Domain (7.10.1).
func (p *painter) rampAt(t float64) (r, g, b float32) {
	lo, hi := p.s.rampLo, p.s.rampHi
	x := 0.0
	if hi != lo {
		x = (t - lo) / (hi - lo) * (rampSize - 1)
	}
	if math.IsNaN(x) || x < 0 {
		x = 0
	}
	x = min(x, rampSize-1)
	i := min(int(x), rampSize-2)
	f := float32(x - float64(i))
	a, c := p.rgbRamp[3*i:3*i+3], p.rgbRamp[3*i+3:3*i+6]
	return a[0] + f*(c[0]-a[0]), a[1] + f*(c[1]-a[1]), a[2] + f*(c[2]-a[2])
}

// meshBuffer records, per pixel of the paint area, which triangle or patch
// covers its centre last and where: barycentric weights (a, b) of a
// triangle's second and third vertices, or the (u, v) of a patch. Colours
// are resolved once per pixel after every primitive is drawn, so a pixel
// covered twice is converted and composited once.
type meshBuffer struct {
	area    region
	w       int
	prim    []int32
	a, b    []float32
	visited int
	// work is what is left of the pixels the mesh may visit (meshOverdraw).
	work int64
}

// paintMesh rasterises a type 4 to 7 shading. Triangles are drawn in stream
// order and patch cells in increasing v, then u, so where the geometry
// overlaps itself the later primitive wins, which 8.7.4.5.5 and 8.7.4.5.7
// require.
func (p *painter) paintMesh(ctx context.Context) error {
	area := p.area
	// A background covers the whole area; without one only the mesh's
	// reach needs a buffer.
	if !p.hasBG {
		mb, ok := p.meshDeviceBounds()
		if !ok {
			return nil
		}
		area = region{max(area.x0, mb.x0), max(area.y0, mb.y0), min(area.x1, mb.x1), min(area.y1, mb.y1)}
		if area.empty() {
			return nil
		}
	}
	w, h := area.x1-area.x0, area.y1-area.y0
	buf := &meshBuffer{
		area: area, w: w, prim: make([]int32, w*h), a: make([]float32, w*h), b: make([]float32, w*h),
		work: max(meshOverdraw*int64(w)*int64(h), minMeshWork),
	}
	for i := range buf.prim {
		buf.prim[i] = -1
	}

	if p.s.patches != nil {
		if err := p.drawPatches(ctx, buf); err != nil {
			return err
		}
	} else {
		ident := [3][2]float32{{0, 0}, {1, 0}, {0, 1}}
		for i, tri := range p.s.tris {
			d := [3]geom.Point{
				p.toDevice.Apply(p.s.points[tri[0]]),
				p.toDevice.Apply(p.s.points[tri[1]]),
				p.toDevice.Apply(p.s.points[tri[2]]),
			}
			if err := buf.triangle(ctx, d, ident, int32(i)); err != nil { //nolint:gosec // len(tris) is bounded by maxMeshValues, which fits int32
				return err
			}
		}
	}
	return p.resolveMesh(ctx, buf)
}

// meshDeviceBounds returns the device pixels the mesh's geometry can reach.
func (p *painter) meshDeviceBounds() (region, bool) {
	var path geom.Path
	for _, pt := range p.s.points {
		path.Pts = append(path.Pts, p.toDevice.Apply(pt))
	}
	for i := range p.s.patches {
		for _, pt := range p.s.patches[i].p {
			path.Pts = append(path.Pts, p.toDevice.Apply(pt))
		}
	}
	r, ok := path.Bounds()
	if !ok {
		return region{}, false
	}
	return pixelSpan(r, p.area), true
}

// pixelSpan returns the pixels of within whose centres r can contain.
func pixelSpan(r geom.Rect, within region) region {
	clampTo := func(v float64, lo, hi int) int {
		switch {
		case math.IsNaN(v) || v < float64(lo):
			return lo
		case v > float64(hi):
			return hi
		}
		return int(v)
	}
	return region{
		x0: clampTo(math.Ceil(r.Min.X-0.5), within.x0, within.x1),
		y0: clampTo(math.Ceil(r.Min.Y-0.5), within.y0, within.y1),
		x1: clampTo(math.Floor(r.Max.X-0.5)+1, within.x0, within.x1),
		y1: clampTo(math.Floor(r.Max.Y-0.5)+1, within.y0, within.y1),
	}
}

// triangle records prim at every pixel centre inside the device triangle d,
// with the attributes attr interpolated barycentrically. Cost: the pixels
// of the triangle's bounding box inside the area.
func (mb *meshBuffer) triangle(ctx context.Context, d [3]geom.Point, attr [3][2]float32, prim int32) error {
	area := (d[1].X-d[0].X)*(d[2].Y-d[0].Y) - (d[2].X-d[0].X)*(d[1].Y-d[0].Y)
	if area == 0 || math.IsNaN(area) || math.IsInf(area, 0) {
		return nil
	}
	var path geom.Path
	path.Pts = d[:]
	box, _ := path.Bounds()
	span := pixelSpan(box, mb.area)
	for y := span.y0; y < span.y1; y++ {
		mb.work -= int64(span.x1 - span.x0)
		if mb.work < 0 {
			return fmt.Errorf("%w: the mesh visits more than %d times the pixels it paints", ErrTooLarge, meshOverdraw)
		}
		mb.visited += span.x1 - span.x0
		if mb.visited >= checkCtxPixels {
			mb.visited = 0
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("rasterising shading mesh: %w", err)
			}
		}
		py := float64(y) + 0.5
		for x := span.x0; x < span.x1; x++ {
			px := float64(x) + 0.5
			w1 := ((px-d[0].X)*(d[2].Y-d[0].Y) - (d[2].X-d[0].X)*(py-d[0].Y)) / area
			w2 := ((d[1].X-d[0].X)*(py-d[0].Y) - (px-d[0].X)*(d[1].Y-d[0].Y)) / area
			w0 := 1 - w1 - w2
			if w0 < -insideTolerance || w1 < -insideTolerance || w2 < -insideTolerance {
				continue
			}
			i := (y-mb.area.y0)*mb.w + (x - mb.area.x0)
			mb.prim[i] = prim
			mb.a[i] = float32(w0)*attr[0][0] + float32(w1)*attr[1][0] + float32(w2)*attr[2][0]
			mb.b[i] = float32(w0)*attr[0][1] + float32(w1)*attr[1][1] + float32(w2)*attr[2][1]
		}
	}
	return nil
}

// drawPatches flattens each patch into a grid of cells and rasterises them.
// The surface is the bicubic tensor product S(u, v) = Σ p_ij B_i(u) B_j(v)
// of 8.7.4.5.8, a Coons patch having had its interior points derived; an
// affine map commutes with it, so the control points are mapped to device
// space first.
func (p *painter) drawPatches(ctx context.Context, mb *meshBuffer) error {
	perPatch := max(1, int(math.Sqrt(float64(maxPatchCells)/float64(len(p.s.patches)))))
	gridCap := min(maxPatchGrid, perPatch)
	var grid []geom.Point
	var uv []float32
	for pi := range p.s.patches {
		var dp [16]geom.Point
		var path geom.Path
		for k, pt := range p.s.patches[pi].p {
			dp[k] = p.toDevice.Apply(pt)
		}
		path.Pts = dp[:]
		box, _ := path.Bounds()
		extent := max(box.Max.X-box.Min.X, box.Max.Y-box.Min.Y)
		n := gridCap
		if cells := math.Ceil(extent / patchCellPixels); cells < float64(n) {
			n = max(1, int(cells))
		}
		grid = grid[:0]
		uv = uv[:0]
		for jv := range n + 1 {
			v := float64(jv) / float64(n)
			bv := bernstein(v)
			for iu := range n + 1 {
				u := float64(iu) / float64(n)
				bu := bernstein(u)
				var pt geom.Point
				for i := range 4 {
					for j := range 4 {
						w := bu[i] * bv[j]
						pt.X += w * dp[4*i+j].X
						pt.Y += w * dp[4*i+j].Y
					}
				}
				grid = append(grid, pt)
				uv = append(uv, float32(u), float32(v))
			}
		}
		row := n + 1
		at := func(iu, jv int) (geom.Point, [2]float32) {
			k := jv*row + iu
			return grid[k], [2]float32{uv[2*k], uv[2*k+1]}
		}
		for jv := range n {
			for iu := range n {
				p00, a00 := at(iu, jv)
				p10, a10 := at(iu+1, jv)
				p11, a11 := at(iu+1, jv+1)
				p01, a01 := at(iu, jv+1)
				prim := int32(pi) //nolint:gosec // len(patches) is bounded by maxMeshValues, which fits int32
				if err := mb.triangle(ctx, [3]geom.Point{p00, p10, p11}, [3][2]float32{a00, a10, a11}, prim); err != nil {
					return err
				}
				if err := mb.triangle(ctx, [3]geom.Point{p00, p11, p01}, [3][2]float32{a00, a11, a01}, prim); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// bernstein returns the cubic Bernstein polynomials B0 to B3 at t.
func bernstein(t float64) [4]float64 {
	s := 1 - t
	return [4]float64{s * s * s, 3 * t * s * s, 3 * t * t * s, t * t * t}
}

// resolveMesh turns the buffer into colours and composites them: a
// triangle's values interpolate linearly between its vertices (8.7.4.5.5)
// and a patch's bilinearly between its corners in (u, v) (8.7.4.5.7), then
// a /Function maps the interpolated t to colour.
func (p *painter) resolveMesh(ctx context.Context, mb *meshBuffer) error {
	s := p.s
	vals := make([]float64, s.nvals)
	for y := mb.area.y0; y < mb.area.y1; y++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("painting shading mesh: %w", err)
		}
		for x := mb.area.x0; x < mb.area.x1; x++ {
			cov := p.coverage(x, y)
			if cov <= 0 {
				continue
			}
			i := (y-mb.area.y0)*mb.w + (x - mb.area.x0)
			prim := mb.prim[i]
			if prim >= 0 && s.hasBBox && !p.inBBox(p.inv.Apply(geom.Point{X: float64(x) + 0.5, Y: float64(y) + 0.5})) {
				prim = -1
			}
			var r, g, b float32
			switch {
			case prim >= 0:
				p.meshValues(prim, float64(mb.a[i]), float64(mb.b[i]), vals)
				if s.funcs != nil {
					r, g, b = p.rampAt(vals[0])
				} else {
					copy(p.comps, vals)
					r, g, b = p.t.Convert.RGB(p.comps)
				}
			case p.hasBG:
				r, g, b = p.bg[0], p.bg[1], p.bg[2]
			default:
				continue
			}
			p.t.Canvas.BlendOver(x, y, r, g, b, cov)
		}
	}
	return nil
}

// meshValues interpolates the colour values of primitive prim at (a, b)
// into out.
func (p *painter) meshValues(prim int32, a, b float64, out []float64) {
	s := p.s
	n := s.nvals
	if s.patches != nil {
		c := s.patches[prim].c
		u, v := a, b
		w := [4]float64{(1 - u) * (1 - v), (1 - u) * v, u * v, u * (1 - v)}
		for k := range out {
			out[k] = w[0]*s.colors[int(c[0])+k] + w[1]*s.colors[int(c[1])+k] +
				w[2]*s.colors[int(c[2])+k] + w[3]*s.colors[int(c[3])+k]
		}
		return
	}
	tri := s.tris[prim]
	w0 := 1 - a - b
	for k := range out {
		out[k] = w0*s.colors[int(tri[0])*n+k] + a*s.colors[int(tri[1])*n+k] + b*s.colors[int(tri[2])*n+k]
	}
}
