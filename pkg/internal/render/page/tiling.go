package page

import (
	"errors"
	"fmt"
	"image"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/render/blend"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/raster"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// The paint types of a tiling pattern (ISO 32000-1, 8.7.3.1, Table 75): a
// coloured pattern's cell sets its own colours, an uncoloured one's is a
// stencil painted in the colour scn gives (8.7.3.3).
const (
	paintTypeColoured   = 1
	paintTypeUncoloured = 2
)

// maxPatternCells bounds the cells one painting with a tiling pattern draws:
// those that cover the painted area, or one tile. Anti-DoS, not
// spec-mandated: Table 75 allows any non-zero /XStep and /YStep, so a step of
// 0.001 over a page runs the cell's content billions of times. 1<<14 cells
// is a step of under 5 pixels over a letter page at 72 dpi; past it a
// pattern draws in a tile of one step, whose cells the cap counts only where
// a /BBox far larger than the step overlaps many of them.
const maxPatternCells = 1 << 14

// tileCacheShare bounds the tiles one render keeps for reuse: their pixels
// together may reach 1/tileCacheShare of the render pixel budget, past which
// a tile is drawn for one painting and dropped. Anti-DoS, not spec-mandated:
// a page reuses few patterns, each at one scale, and a quarter of the budget
// holds hundreds of tiles of real hatchings and dot screens.
const tileCacheShare = 4

// directCellLimit is how many cells a pattern whose matrix rotates or skews
// its cells against the device grid draws one by one over the painted area,
// exactly; past it the cells draw once into a tile resampled per pixel, as a
// viewer tiles them. Not spec-mandated: 8.7.3.1 lets a viewer place cells to
// device precision, and 64 cells keep the exact path cheap.
const directCellLimit = 64

// axisTolerance is the share of a step's length its cross-axis component may
// reach and still count as axis-aligned in device space.
const axisTolerance = 1e-9

// tilingPattern is a compiled tiling pattern (ISO 32000-1, 8.7.3.1, Table
// 75).
type tilingPattern struct {
	ref          ast.PdfIndirectRef
	paintType    int
	bbox         geom.Rect
	xStep, yStep float64
	matrix       geom.Matrix
	// resources are the pattern's /Resources, nil to use those of the stream
	// that paints with it.
	resources ast.PdfDict
	ops       []content.Operation
}

// tilingState is what one render shares across the renderers of its pattern
// cells: compiled patterns, cached tiles and the pixels they hold.
type tilingState struct {
	compiled map[ast.PdfIndirectRef]*tilingPattern
	tiles    map[tileKey]*surface.Canvas
	// cached counts the pixels of tiles.
	cached int64
}

// tileKey names a tile: the pattern, how its space maps to the tile's
// pixels, and the tile's size.
type tileKey struct {
	ref           ast.PdfIndirectRef
	toTile        geom.Matrix
	width, height int
}

func (r *renderer) tilingCaches() *tilingState {
	if r.tiling == nil {
		r.tiling = &tilingState{
			compiled: make(map[ast.PdfIndirectRef]*tilingPattern),
			tiles:    make(map[tileKey]*surface.Canvas),
		}
	}
	return r.tiling
}

// compileTiling reads a tiling pattern stream, once per indirect object a
// render paints with.
func (r *renderer) compileTiling(obj ast.PdfObject) (*tilingPattern, error) {
	ts := r.tilingCaches()
	ref, isRef := obj.(ast.PdfIndirectRef)
	if isRef {
		if tp, ok := ts.compiled[ref]; ok {
			return tp, nil
		}
	}
	stream, err := document.ResolveStream(r.ctx, r.doc, obj)
	if err != nil || stream.Dict == nil {
		// Table 75: a tiling pattern is a stream.
		return nil, fmt.Errorf("tiling pattern is not a stream: %w", err)
	}
	dict := stream.Dict
	tp := &tilingPattern{ref: ref, matrix: geom.Identity}
	paint, err := document.ResolveInt(r.ctx, r.doc, dict["PaintType"])
	if err != nil || (paint != paintTypeColoured && paint != paintTypeUncoloured) {
		return nil, fmt.Errorf("tiling pattern /PaintType %d is neither 1 nor 2: %w", paint, err)
	}
	tp.paintType = int(paint)
	box, ok := r.numberArray(dict["BBox"], 4)
	if !ok {
		return nil, errors.New("tiling pattern has no usable /BBox")
	}
	tp.bbox = geom.Rect{
		Min: geom.Point{X: min(box[0], box[2]), Y: min(box[1], box[3])},
		Max: geom.Point{X: max(box[0], box[2]), Y: max(box[1], box[3])},
	}
	steps := [2]float64{}
	for i, key := range []ast.PdfName{"XStep", "YStep"} {
		v, err := document.ResolveObject(r.ctx, r.doc, dict[key])
		f, isNum := document.Number(v)
		// Table 75: /XStep and /YStep shall not be zero; they may be negative.
		if err != nil || !isNum || f == 0 || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("tiling pattern /%s is no non-zero number", key)
		}
		steps[i] = f
	}
	tp.xStep, tp.yStep = steps[0], steps[1]
	if m, ok := r.numberArray(dict["Matrix"], 6); ok {
		tp.matrix = geom.Matrix(m)
	}
	if res, err := document.ResolveDict(r.ctx, r.doc, dict["Resources"]); err == nil && res != nil {
		tp.resources = res
	}
	tp.ops, err = content.ParsePageContent(r.ctx, r.doc, stream, content.SplitGluedOperators(), content.InlineImageData(), content.ViewerFilterChains())
	if err != nil {
		return nil, fmt.Errorf("parsing tiling pattern content: %w", err)
	}
	if isRef {
		ts.compiled[ref] = tp
	}
	return tp, nil
}

// numberArray reads an array of n numbers.
func (r *renderer) numberArray(obj ast.PdfObject, n int) ([6]float64, bool) {
	var out [6]float64
	arr, err := document.ResolveArray(r.ctx, r.doc, obj)
	if err != nil || len(arr) != n {
		return out, false
	}
	for i, e := range arr {
		f, ok := document.Number(e)
		if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
			return out, false
		}
		out[i] = f
	}
	return out, true
}

// tilingFill paints cov with the tiling pattern obj (ISO 32000-1, 8.7.3) at
// constant alpha a. The pattern matrix maps pattern space into s.base, the
// default space of the stream the pattern is used in (8.7.2), not the CTM at
// the painting. Each cell is the pattern's content clipped to its /BBox and
// repeated every /XStep and /YStep; an uncoloured pattern's cells are a
// stencil painted in c's components in the base space of the Pattern space
// (8.7.3.3).
//
// /TilingType is not read: every type draws as type 1 (Table 75, constant
// spacing). When the matrix keeps the cells aligned with the device grid,
// each step is rounded to whole device pixels and the cell scaled to it, the
// slight distortion type 1 permits, and one tile repeats exactly. Type 3
// permits the same. Deliberate leniency toward type 2, which asks for an
// undistorted cell and lets the spacing vary by a pixel instead: cells placed
// a fraction of a pixel apart leave anti-aliased seams where they abut,
// where the scaled cell leaves none, and the distortion stays under one
// device pixel a cell. A matrix that turns the cells against the device grid
// draws them where they fall, or through a tile resampled per pixel when
// they are many (directCellLimit).
func (r *renderer) tilingFill(s *scope, cov *raster.Coverage, c graphics.Color, a float64, obj ast.PdfObject) error {
	tp, err := r.compileTiling(obj)
	if err != nil {
		if fatal(err) {
			return err
		}
		r.skip("tiling pattern", err)
		return nil
	}
	if lim := r.doc.Limits().WithDefaults().MaxFormDepth; s.depth+1 > lim {
		r.skip("tiling pattern", fmt.Errorf("nested deeper than %d forms and patterns", lim))
		return nil
	}
	if tp.ref.ObjectNum != 0 && slices.Contains(r.chain, tp.ref.ObjectNum) {
		r.skip("tiling pattern", fmt.Errorf("pattern %d paints with itself", tp.ref.ObjectNum))
		return nil
	}
	var stencil [3]float32
	if tp.paintType == paintTypeUncoloured {
		// 8.7.3.3: an uncoloured pattern is used in a Pattern space with an
		// underlying space, whose components scn gives with the name.
		if c.Space.Base == nil {
			r.skip("tiling pattern", errors.New("uncoloured pattern in a Pattern space with no underlying space"))
			return nil
		}
		conv, paints := r.colorFor(s, *c.Space.Base)
		if !paints {
			return nil
		}
		stencil[0], stencil[1], stencil[2] = conv.RGB(c.Components)
	}
	resources := tp.resources
	if resources == nil {
		resources = s.resources
	}
	src, err := r.tilingSource(s, tp, resources, cov)
	if err != nil {
		if fatal(err) {
			return err
		}
		r.skip("tiling pattern", err)
		return nil
	}
	if src == nil {
		return nil
	}
	defer src.release()
	rect := image.Rect(cov.X, cov.Y, cov.X+cov.Width, cov.Y+cov.Height)
	return r.paint(rect, r.effectsOf(r.m.Current(), a), func(v view, op surface.Opacity) error {
		composite(v, cov, op.Alpha(), src, tp.paintType == paintTypeUncoloured, stencil)
		return nil
	})
}

// patternSource is where the pixels of a tiling pattern come from: a raster
// of the painted area, or a tile repeated over it.
type patternSource struct {
	canvas *surface.Canvas
	// release returns the raster's pixels to the live budget.
	release func()
	// direct marks a raster over the painted area, (ox, oy) its device
	// origin. Otherwise canvas is a tile of one period.
	direct bool
	ox, oy int
	// exact marks a tile on the device grid, (ox, oy) the device pixel of
	// its (0, 0); otherwise fromDevice maps a device point into the tile,
	// sampled with wrapping.
	exact      bool
	fromDevice geom.Matrix
}

// tilingSource draws the pixels pattern tp paints over cov.
func (r *renderer) tilingSource(s *scope, tp *tilingPattern, resources ast.PdfDict, cov *raster.Coverage) (*patternSource, error) {
	p := tp.matrix.Mul(s.base)
	u := p.ApplyVector(geom.Point{X: tp.xStep})
	v := p.ApplyVector(geom.Point{Y: tp.yStep})
	if !(math.Abs(u.X*v.Y-u.Y*v.X) > 0) || !finitePoint(u) || !finitePoint(v) {
		// A singular pattern matrix squeezes the cells to nothing.
		return nil, nil //nolint:nilnil // nothing to paint is no error
	}
	area := int64(cov.Width) * int64(cov.Height)
	cacheCap := r.budget / tileCacheShare

	if w, h, ok := axisPeriods(u, v); ok {
		// Round each period to whole device pixels and scale the cell to it
		// about the pattern's origin: the constant spacing of /TilingType 1.
		wi, hi := max(1, math.Round(w)), max(1, math.Round(h))
		o := p.Apply(geom.Point{})
		p = p.Mul(geom.Matrix{1, 0, 0, 1, -o.X, -o.Y}).Mul(geom.Matrix{wi / w, 0, 0, hi / h, o.X, o.Y})
		tw, th := int64(wi), int64(hi)
		if tw*th < area && tw*th <= cacheCap {
			ox, oy := math.Floor(o.X), math.Floor(o.Y)
			toTile := p.Mul(geom.Matrix{1, 0, 0, 1, -ox, -oy})
			tile, release, err := r.tile(s, tp, resources, toTile, int(tw), int(th))
			if err != nil || tile == nil {
				return nil, err
			}
			return &patternSource{canvas: tile, release: release, exact: true, ox: int(ox), oy: int(oy)}, nil
		}
		return r.directSource(s, tp, resources, p, cov)
	}

	nu, nv := math.Ceil(math.Hypot(u.X, u.Y)), math.Ceil(math.Hypot(v.X, v.Y))
	if nu*nv < float64(area) && nu*nv <= float64(cacheCap) && r.cellCount(tp, p, cov) > directCellLimit {
		inv, ok := p.Invert()
		if !ok {
			return nil, nil //nolint:nilnil // a matrix that does not invert paints nothing
		}
		toTile := geom.Matrix{nu / tp.xStep, 0, 0, nv / tp.yStep, 0, 0}
		tile, release, err := r.tile(s, tp, resources, toTile, int(nu), int(nv))
		if err != nil || tile == nil {
			return nil, err
		}
		return &patternSource{canvas: tile, release: release, fromDevice: inv.Mul(toTile)}, nil
	}
	return r.directSource(s, tp, resources, p, cov)
}

func finitePoint(p geom.Point) bool {
	return !math.IsNaN(p.X) && !math.IsInf(p.X, 0) && !math.IsNaN(p.Y) && !math.IsInf(p.Y, 0)
}

// axisPeriods returns the horizontal and vertical device periods of the
// lattice the steps u and v span, when both lie on the device axes.
func axisPeriods(u, v geom.Point) (w, h float64, ok bool) {
	onX := func(p geom.Point) bool { return math.Abs(p.Y) <= axisTolerance*math.Abs(p.X) }
	onY := func(p geom.Point) bool { return math.Abs(p.X) <= axisTolerance*math.Abs(p.Y) }
	switch {
	case onX(u) && onY(v):
		return math.Abs(u.X), math.Abs(v.Y), true
	case onY(u) && onX(v):
		return math.Abs(v.X), math.Abs(u.Y), true
	}
	return 0, 0, false
}

// cellCount returns how many cells of tp, placed through p, the area of cov
// touches, or more than maxPatternCells.
func (r *renderer) cellCount(tp *tilingPattern, p geom.Matrix, cov *raster.Coverage) float64 {
	inv, ok := p.Invert()
	if !ok {
		return 0
	}
	i0, i1, j0, j1 := cellRange(tp, regionIn(inv, float64(cov.X), float64(cov.Y), float64(cov.Width), float64(cov.Height)))
	return (i1 - i0 + 1) * (j1 - j0 + 1)
}

// regionIn returns the bounds, through m, of the rectangle at (x, y) of size
// w by h.
func regionIn(m geom.Matrix, x, y, w, h float64) geom.Rect {
	var path geom.Path
	path.MoveTo(m.Apply(geom.Point{X: x, Y: y}))
	path.LineTo(m.Apply(geom.Point{X: x + w, Y: y}))
	path.LineTo(m.Apply(geom.Point{X: x + w, Y: y + h}))
	path.LineTo(m.Apply(geom.Point{X: x, Y: y + h}))
	b, _ := path.Bounds()
	return b
}

// cellRange returns the cells (i, j), each tp's /BBox moved by i /XStep and j
// /YStep, that may overlap region, a rectangle of pattern space.
func cellRange(tp *tilingPattern, region geom.Rect) (i0, i1, j0, j1 float64) {
	span := func(lo, hi, boxLo, boxHi, step float64) (float64, float64) {
		a, b := (lo-boxHi)/step, (hi-boxLo)/step
		return math.Floor(min(a, b)), math.Ceil(max(a, b))
	}
	i0, i1 = span(region.Min.X, region.Max.X, tp.bbox.Min.X, tp.bbox.Max.X, tp.xStep)
	j0, j1 = span(region.Min.Y, region.Max.Y, tp.bbox.Min.Y, tp.bbox.Max.Y, tp.yStep)
	return i0, i1, j0, j1
}

// directSource draws the cells of tp that cover cov into a raster of cov's
// size, each where it falls, p mapping pattern space to device space.
func (r *renderer) directSource(s *scope, tp *tilingPattern, resources ast.PdfDict, p geom.Matrix, cov *raster.Coverage) (*patternSource, error) {
	release, err := r.reserve(int64(cov.Width) * int64(cov.Height))
	if err != nil {
		return nil, err
	}
	dst, err := blend.NewPage(cov.Width, cov.Height, r.budget)
	if err != nil {
		release()
		return nil, fmt.Errorf("%w: pattern raster: %w", ErrTooLarge, err)
	}
	toDst := p.Mul(geom.Matrix{1, 0, 0, 1, -float64(cov.X), -float64(cov.Y)})
	if err := r.drawCells(s, tp, resources, dst, toDst); err != nil {
		release()
		return nil, err
	}
	return &patternSource{canvas: dst.Canvas(), release: release, direct: true, ox: cov.X, oy: cov.Y}, nil
}

// tile returns the tile of tp of w by h pixels, toTile mapping pattern space
// to its pixels, drawn once and cached while the cache has room.
func (r *renderer) tile(s *scope, tp *tilingPattern, resources ast.PdfDict, toTile geom.Matrix, w, h int) (*surface.Canvas, func(), error) {
	ts := r.tilingCaches()
	// A pattern drawn with the resources of the stream it is used in cannot
	// be cached under its own name.
	cacheable := tp.ref.ObjectNum != 0 && tp.resources != nil
	key := tileKey{ref: tp.ref, toTile: toTile, width: w, height: h}
	if cacheable {
		if t, ok := ts.tiles[key]; ok {
			return t, func() {}, nil
		}
	}
	pixels := int64(w) * int64(h)
	release, err := r.reserve(pixels)
	if err != nil {
		return nil, nil, err
	}
	g, err := blend.NewPage(w, h, r.budget)
	if err != nil {
		release()
		return nil, nil, fmt.Errorf("%w: pattern tile: %w", ErrTooLarge, err)
	}
	t := g.Canvas()
	if err := r.drawCells(s, tp, resources, g, toTile); err != nil {
		release()
		return nil, nil, err
	}
	if cacheable && ts.cached+pixels <= r.budget/tileCacheShare {
		release()
		ts.cached += pixels
		ts.tiles[key] = t
		return t, func() {}, nil
	}
	return t, release, nil
}

// drawCells runs tp's content once per cell that overlaps dst, an isolated
// group of the tile's or the painted area's size, toDst mapping pattern space
// to its pixels. A tile's edges cut cells that the tile's
// neighbours continue, so a tile drawn this way repeats seamlessly.
//
// The cells run in a renderer of their own, sharing the render's budgets and
// caches, from the initial graphics state (ISO 32000-1, 8.7.3.1: the cell is
// painted in its own space, independent of the state where the pattern is
// used) with the CTM of the cell's place, into dst as the group the cells'
// transparency composites in. Each cell is its own copy: a
// pattern used inside the cell maps into the moved cell's space.
func (r *renderer) drawCells(s *scope, tp *tilingPattern, resources ast.PdfDict, dst *blend.Group, toDst geom.Matrix) error {
	inv, ok := toDst.Invert()
	if !ok {
		return nil
	}
	w, h := dst.Bounds().Dx(), dst.Bounds().Dy()
	i0, i1, j0, j1 := cellRange(tp, regionIn(inv, 0, 0, float64(w), float64(h)))
	if n := (i1 - i0 + 1) * (j1 - j0 + 1); !(n <= maxPatternCells) {
		return fmt.Errorf("pattern of %.0f cells exceeds %d", n, maxPatternCells)
	}
	sub := r.sub(w, h, geom.Identity, dst)
	sub.chain = append(sub.chain, tp.ref.ObjectNum)
	defer func() { r.ops = sub.ops }()
	for j := j0; j <= j1; j++ {
		for i := i0; i <= i1; i++ {
			cell := geom.Matrix{1, 0, 0, 1, i * tp.xStep, j * tp.yStep}.Mul(toDst)
			if err := sub.drawCell(s, tp, resources, cell); err != nil {
				return err
			}
		}
	}
	return nil
}

// drawCell runs tp's content once with cell as its CTM, clipped to its
// /BBox (Table 75).
func (r *renderer) drawCell(s *scope, tp *tilingPattern, resources ast.PdfDict, cell geom.Matrix) error {
	saved, savedMarked := len(r.m.Stack), len(r.m.MarkedContent)
	if err := r.m.ExecuteOne(r.ctx, content.Operation{Operator: "q"}); err != nil {
		return fmt.Errorf("saving state for a pattern cell: %w", err)
	}
	r.syncClips()
	defer func() {
		r.m.Stack = r.m.Stack[:saved]
		if len(r.m.MarkedContent) > savedMarked {
			r.m.MarkedContent = r.m.MarkedContent[:savedMarked]
		}
		r.syncClips()
	}()
	r.m.Current().CTM = [6]float64(cell)
	b := tp.bbox
	var bp geom.Path
	bp.MoveTo(cell.Apply(b.Min))
	bp.LineTo(cell.Apply(geom.Point{X: b.Max.X, Y: b.Min.Y}))
	bp.LineTo(cell.Apply(b.Max))
	bp.LineTo(cell.Apply(geom.Point{X: b.Min.X, Y: b.Max.Y}))
	bp.Close()
	c, err := r.clip().withPath(bp, raster.NonZero, r.width, r.height, r.budget)
	if err != nil {
		if fatal(err) {
			return err
		}
		r.skip("tiling pattern cell", err)
		return nil
	}
	if c.empty() {
		return nil
	}
	if err := r.setClip(c); err != nil {
		return err
	}
	return r.run(tp.ops, resources, cell, s.depth+1)
}

// composite paints src through cov, device pixels, at constant alpha a into
// v, source-over. An uncoloured pattern paints its raster's coverage in the
// colour stencil.
func composite(v view, cov *raster.Coverage, a float32, src *patternSource, uncoloured bool, stencil [3]float32) {
	if a <= 0 {
		return
	}
	t, dst := src.canvas, v.canvas
	for y := range cov.Height {
		dy := cov.Y + y
		if dy < v.y || dy >= v.y+dst.Height {
			continue
		}
		for x := range cov.Width {
			k := cov.A[y*cov.Width+x] * a
			dx := cov.X + x
			if k <= 0 || dx < v.x || dx >= v.x+dst.Width {
				continue
			}
			var pr, pg, pb, pa float32
			switch {
			case src.direct:
				pr, pg, pb, pa = t.At(dx-src.ox, dy-src.oy)
			case src.exact:
				pr, pg, pb, pa = t.At(wrap(dx-src.ox, t.Width), wrap(dy-src.oy, t.Height))
			default:
				pr, pg, pb, pa = sampleWrapped(t, src.fromDevice.Apply(geom.Point{X: float64(dx) + 0.5, Y: float64(dy) + 0.5}))
			}
			if pa <= 0 {
				continue
			}
			if uncoloured {
				pr, pg, pb = stencil[0]*pa, stencil[1]*pa, stencil[2]*pa
			}
			i := 4 * ((dy-v.y)*dst.Width + dx - v.x)
			keep := 1 - min(1, pa*k)
			dst.Pix[i] = pr*k + dst.Pix[i]*keep
			dst.Pix[i+1] = pg*k + dst.Pix[i+1]*keep
			dst.Pix[i+2] = pb*k + dst.Pix[i+2]*keep
			dst.Pix[i+3] = pa*k + dst.Pix[i+3]*keep
		}
	}
}

// wrap returns v modulo n, in [0, n).
func wrap(v, n int) int {
	v %= n
	if v < 0 {
		v += n
	}
	return v
}

// sampleWrapped reads tile t at point p, its pixel (i, j) centred at
// (i+0.5, j+0.5), interpolating the four nearest pixels bilinearly and
// wrapping at the edges, as the tile repeats.
func sampleWrapped(t *surface.Canvas, p geom.Point) (r, g, b, a float32) {
	fx, fy := p.X-0.5, p.Y-0.5
	if math.IsNaN(fx) || math.IsNaN(fy) || math.IsInf(fx, 0) || math.IsInf(fy, 0) {
		return 0, 0, 0, 0
	}
	x0, y0 := math.Floor(fx), math.Floor(fy)
	wx, wy := float32(fx-x0), float32(fy-y0)
	ix := wrap(int(math.Mod(x0, float64(t.Width))), t.Width)
	iy := wrap(int(math.Mod(y0, float64(t.Height))), t.Height)
	ix1, iy1 := (ix+1)%t.Width, (iy+1)%t.Height
	var out [4]float32
	for _, tap := range [4]struct {
		x, y int
		w    float32
	}{{ix, iy, (1 - wx) * (1 - wy)}, {ix1, iy, wx * (1 - wy)}, {ix, iy1, (1 - wx) * wy}, {ix1, iy1, wx * wy}} {
		i := 4 * (tap.y*t.Width + tap.x)
		for c := range 4 {
			out[c] += t.Pix[i+c] * tap.w
		}
	}
	return out[0], out[1], out[2], out[3]
}
