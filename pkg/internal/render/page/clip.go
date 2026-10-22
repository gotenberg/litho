package page

import (
	"fmt"
	"image"
	"math"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/raster"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// rectTolerance is how far, in device pixels, two coordinates of a clipping
// path may differ and still make an axis-aligned edge. It absorbs the
// rounding of a rectangle mapped through a matrix without rotation.
const rectTolerance = 1e-9

// clip is the current clipping path (ISO 32000-1, 8.5.4) as the renderer
// holds it: an axis-aligned device rectangle, intersected with the coverage
// of the clips of any other shape when one has been set. Most clips are
// rectangles, a page's margins or a form's bounding box, and a rectangle
// costs nothing to intersect. The other shapes are kept as paths
// ([clipNode]) and rasterised over the pixels painted under them, until they
// are rasterised over the whole page once.
//
// A clip is immutable once a graphics state holds it: q shares it, and
// setting a clip makes a new one.
type clip struct {
	// node is the coverage of the clips that are not rectangles, or nil.
	node *clipNode
	// rect is the intersection of the rectangles, in device pixels.
	rect geom.Rect
	// inMask reports that node already holds rect, which then only bounds
	// the work.
	inMask bool
	// full is node's coverage over the page with rect applied, made the
	// first time an element needs it over the whole page, or nil.
	full *surface.Mask
}

// newClip is the clip of a page with no clipping path set: the page itself.
func newClip(width, height int) *clip {
	return &clip{rect: geom.Rect{Max: geom.Point{X: float64(width), Y: float64(height)}}}
}

// empty reports whether the clip lets nothing through.
func (c *clip) empty() bool {
	return c.rect.Max.X <= c.rect.Min.X || c.rect.Max.Y <= c.rect.Min.Y
}

// axisRect returns the rectangle path p outlines when it is one closed
// subpath of four corners with axis-aligned edges, as re writes one under a
// matrix that does not rotate.
func axisRect(p geom.Path) (geom.Rect, bool) {
	ops := p.Ops
	if len(ops) == 6 && ops[4] == geom.LineTo && ops[5] == geom.Close {
		// A fifth point back on the first closes the same rectangle.
		if !near(p.Pts[4], p.Pts[0]) {
			return geom.Rect{}, false
		}
		ops = ops[:4]
	} else if len(ops) != 5 || ops[4] != geom.Close {
		return geom.Rect{}, false
	}
	if ops[0] != geom.MoveTo || ops[1] != geom.LineTo || ops[2] != geom.LineTo || ops[3] != geom.LineTo {
		return geom.Rect{}, false
	}
	q := p.Pts[:4]
	vertical := eq(q[0].X, q[1].X) && eq(q[1].Y, q[2].Y) && eq(q[2].X, q[3].X) && eq(q[3].Y, q[0].Y)
	horizontal := eq(q[0].Y, q[1].Y) && eq(q[1].X, q[2].X) && eq(q[2].Y, q[3].Y) && eq(q[3].X, q[0].X)
	if !vertical && !horizontal {
		return geom.Rect{}, false
	}
	r, ok := p.Bounds()
	return r, ok
}

func eq(a, b float64) bool { return math.Abs(a-b) <= rectTolerance }

func near(a, b geom.Point) bool { return eq(a.X, b.X) && eq(a.Y, b.Y) }

// intersectRect returns c narrowed to r.
func (c *clip) intersectRect(r geom.Rect) *clip {
	out := &clip{node: c.node, rect: geom.Rect{
		Min: geom.Point{X: max(c.rect.Min.X, r.Min.X), Y: max(c.rect.Min.Y, r.Min.Y)},
		Max: geom.Point{X: min(c.rect.Max.X, r.Max.X), Y: min(c.rect.Max.Y, r.Max.Y)},
	}}
	return out
}

// rowSnapTolerance is how far, in device pixels, a clipping rectangle's edge
// may lie past a row boundary and still round to it, so a rectangle that
// sits on whole rows but went through a matrix keeps them rather than
// gaining a row from the rounding error.
const rowSnapTolerance = 1e-6

// snapRows returns r with its top and bottom moved outward to whole device
// rows, as poppler clips a rectangle. ISO 32000-1, 8.5.4 makes the clipping
// path an exact region and leaves its rasterisation to the renderer; this is
// that convention, not a spec rule. Splash (SplashClip::clipToRect) keeps
// the rows floor(top) to ceil(bottom) whole and cuts columns at sub-pixel
// precision (SplashClip::clipAALine), and PDFium
// (CFX_AggDeviceDriver::SetClip_PathFill, CFX_FloatRect::GetOuterRect)
// rounds both outward to whole pixels. The two agree on the rows, so litho
// rounds them and keeps the columns exact, poppler's reading. A rectangle
// with no height keeps none: `0 0 0 0 re W n` clips everything away.
//
// # Corpus evidence
//
//   - pdfjs bug1978317, its /Annots removed so poppler draws it: 0.5-point
//     underlines under a clip half a pixel tall, which poppler paints solid
//     and exact coverage painted at half strength. 6951 pixels differed
//     from poppler, 0 do.
//   - govdocs, litho, pdfjs: 31 of 641 render-cell pages move. 20 come
//     closer to poppler (govdocs thread_200_200148 pages 1 and 2: 3951 and
//     3055 pixels differ, then 2037 and 1331), 7 keep their count, 4 move
//     away by 1 to 6 pixels; no page, flatten or redact-render verdict
//     changes.
func snapRows(r geom.Rect) geom.Rect {
	if !(r.Max.Y > r.Min.Y) {
		return r
	}
	r.Min.Y = math.Floor(r.Min.Y + rowSnapTolerance)
	r.Max.Y = math.Ceil(r.Max.Y - rowSnapTolerance)
	return r
}

// pixelRect returns r with its four edges moved outward to whole device
// pixels, within [rowSnapTolerance] of a pixel boundary kept on it. A
// rectangle with no width or height keeps none.
func pixelRect(r geom.Rect) geom.Rect {
	if !(r.Max.X > r.Min.X) || !(r.Max.Y > r.Min.Y) {
		return r
	}
	r.Min.X = math.Floor(r.Min.X + rowSnapTolerance)
	r.Max.X = math.Ceil(r.Max.X - rowSnapTolerance)
	return snapRows(r)
}

// wholePixels returns c with its rectangle widened to the whole device
// pixels it touches, in columns as in rows ([pixelRect]), the clip a
// transparency group composites under. ISO 32000-1, 11.4 composites a
// group's result as one object under the clip and leaves the edge pixels to
// the renderer; this is poppler's convention, not a spec rule. Splash paints
// a group's content unclipped into a bitmap of its own
// (SplashOutputDev::beginTransparencyGroup) and composites it pixel by pixel
// where SplashClip::test passes (Splash::composite), which keeps the columns
// floor(xMin) to ceil(xMax)-1 whole, where a fill or an image cuts them at a
// quarter pixel (SplashClip::clipAALine). A mask already holding the
// rectangle keeps the coverage it holds.
//
// # Corpus evidence
//
//   - govdocs, litho, pdfjs, pdfcpu: with glyphs ([clip.applyPixelRect])
//     and groups cut at whole pixels, no render-cell or redact-render page
//     moves; 25 flatten-cell page checks over 21 files move by a few
//     pixels (pdfjs issue14562: 4225 to 4346 differing pixels of 327260
//     inked), and none changes its verdict.
func (c *clip) wholePixels() *clip {
	if c.inMask {
		return c
	}
	// The whole-page coverage cached under the old rectangle does not hold
	// under the widened one, so the copy starts without it.
	return &clip{node: c.node, rect: pixelRect(c.rect)}
}

// withPath returns c narrowed to the device-space path p filled with rule
// (ISO 32000-1, 8.5.4): a rectangle narrows the rectangle, any other shape
// adds a [clipNode] of the intersection. With snap, a rectangle first takes
// whole rows ([snapRows]), as a viewer clips to a clipping path or a form's
// bounding box; a pattern cell keeps its exact box, so the cells of a tiling
// meet without overlapping.
func (c *clip) withPath(p geom.Path, rule raster.FillRule, snap bool, width, height int, budget int64) (*clip, error) {
	if r, ok := axisRect(p); ok {
		if snap {
			r = snapRows(r)
		}
		return c.intersectRect(r), nil
	}
	shape, err := raster.NewShape(p, rule, width, height)
	if err != nil {
		return nil, fmt.Errorf("clipping: computing the clip path: %w", err)
	}
	prev := c.node
	if prev != nil && prev.depth >= maxLazyClipDepth {
		if err := prev.materialize(); err != nil {
			return nil, err
		}
	}
	n := &clipNode{
		path:      p,
		rule:      rule,
		shape:     shape,
		prev:      prev,
		rect:      c.rect,
		applyRect: !c.whole(width, height) && !c.inMask,
		depth:     1,
		bounds:    shape.Bounds(),
		width:     width,
		height:    height,
		budget:    budget,
	}
	if prev != nil {
		n.bounds = n.bounds.Intersect(prev.bounds)
		n.depth += prev.depth
	}
	if n.applyRect {
		x0, x1, y0, y1 := c.span(width, height)
		n.bounds = n.bounds.Intersect(image.Rect(x0, y0, x1, y1))
	}
	return &clip{node: n, rect: c.rect, inMask: true}, nil
}

// whole reports whether the rectangle covers the page.
func (c *clip) whole(width, height int) bool {
	return c.rect.Min.X <= 0 && c.rect.Min.Y <= 0 && c.rect.Max.X >= float64(width) && c.rect.Max.Y >= float64(height)
}

// mask1 returns the clip as one mask, rectangle applied, nil when it clips to
// the whole page. It is made once per clip.
func (c *clip) mask1(width, height int, budget int64) (*surface.Mask, error) {
	var values *surface.Mask
	if c.node != nil {
		if err := c.node.materialize(); err != nil {
			return nil, err
		}
		values = c.node.values
	}
	if c.whole(width, height) || c.inMask {
		return values, nil
	}
	if c.full != nil {
		return c.full, nil
	}
	m, err := rectMask(c.rect, values, width, height, budget)
	if err != nil {
		return nil, err
	}
	c.full = m
	return m, nil
}

// rectMask returns the mask of rect over a raster of width × height pixels,
// multiplied by values when it is not nil.
func rectMask(rect geom.Rect, values *surface.Mask, width, height int, budget int64) (*surface.Mask, error) {
	m, err := surface.NewMask(width, height, budget)
	if err != nil {
		return nil, fmt.Errorf("%w: clip mask: %w", ErrTooLarge, err)
	}
	x0, x1, y0, y1 := (&clip{rect: rect}).span(width, height)
	for y := y0; y < y1; y++ {
		fy := overlap(float64(y), rect.Min.Y, rect.Max.Y)
		for x := x0; x < x1; x++ {
			v := fy * overlap(float64(x), rect.Min.X, rect.Max.X)
			if values != nil {
				v *= values.A[y*width+x]
			}
			m.A[y*width+x] = v
		}
	}
	return m, nil
}

// in returns the values [clip.mask1] gives over box, a part of the page, row
// by row, without making the mask when it is not made yet; nil when the clip
// clips nothing.
func (c *clip) in(box image.Rectangle, width, height int, budget int64) ([]float32, error) {
	if c.full != nil || (c.node != nil && c.node.values != nil) {
		m, err := c.mask1(width, height, budget)
		if err != nil || m == nil {
			return nil, err
		}
		return maskIn(m, box), nil
	}
	applyRect := !c.whole(width, height) && !c.inMask
	if c.node == nil && !applyRect {
		return nil, nil
	}
	var values []float32
	if c.node != nil {
		var err error
		if values, err = c.node.in(box); err != nil {
			return nil, err
		}
	}
	if !applyRect {
		return values, nil
	}
	return rectIn(c.rect, values, box, width, height), nil
}

// maskIn returns the values of m over box, row by row.
func maskIn(m *surface.Mask, box image.Rectangle) []float32 {
	out := make([]float32, box.Dx()*box.Dy())
	for y := box.Min.Y; y < box.Max.Y; y++ {
		at := (y - box.Min.Y) * box.Dx()
		copy(out[at:at+box.Dx()], m.A[y*m.Width+box.Min.X:y*m.Width+box.Max.X])
	}
	return out
}

// rectIn returns the values [rectMask] gives over box, row by row, values
// holding the values it multiplies by over box, or nil.
func rectIn(rect geom.Rect, values []float32, box image.Rectangle, width, height int) []float32 {
	out := make([]float32, box.Dx()*box.Dy())
	x0, x1, y0, y1 := (&clip{rect: rect}).span(width, height)
	span := image.Rect(x0, y0, x1, y1).Intersect(box)
	w := box.Dx()
	for y := span.Min.Y; y < span.Max.Y; y++ {
		fy := overlap(float64(y), rect.Min.Y, rect.Max.Y)
		for x := span.Min.X; x < span.Max.X; x++ {
			i := (y-box.Min.Y)*w + x - box.Min.X
			v := fy * overlap(float64(x), rect.Min.X, rect.Max.X)
			if values != nil {
				v *= values[i]
			}
			out[i] = v
		}
	}
	return out
}

// maxLazyClipDepth bounds the clipping paths kept as paths on top of one
// another: setting one more first rasterises the coverage under it over the
// page. Anti-DoS guard, not spec-mandated: every element painted under a
// chain of paths computes the coverage of each over its pixels, so a stream
// of nested q W n would make each fill cost as many coverages as it nests. 8
// holds the nesting of real pages, a form's bounding box and a few clips in
// it, and keeps a fill under it to 8 coverages of its own pixels.
const maxLazyClipDepth = 8

// clipNode is the coverage of a clipping path that is not a rectangle,
// intersected with the clip it was set under (ISO 32000-1, 8.5.4). It keeps
// the path and computes its coverage over the pixels an element paints
// ([clipNode.in]) until the coverage over the page is needed, or until the
// pixels computed in parts reach the page's: it then rasterises the page
// once ([clipNode.materialize]). Both give the values of the mask
// [raster.Clip] makes of the path over the mask of the clip under it,
// multiplied in the same order.
//
// The flattening composite of a page draws each element a composited region
// overlaps outside the region under the crop box with the region's box cut
// out, an even-odd clip as large as the page. A page whose elements were
// each clipped so rasterised that page for every element.
//
// # Corpus evidence
//
//   - pdfjs issue18765.pdf: the flattened page sets 2,296 such clips around
//     small paths; rasterising each over the 6975 x 2925 pixel page took 44
//     s of the 66 s of CPU the source and the flattened page took to render
//     at 72 dpi. The two renderings now take 5 s.
type clipNode struct {
	// values is the coverage over the page once made, nil before.
	values *surface.Mask
	// path is the clipping path, filled with rule, and shape the path
	// flattened.
	path  geom.Path
	rule  raster.FillRule
	shape *raster.Shape
	// prev is the coverage of the clip the path was set under, nil when it
	// had none; rect is that clip's rectangle, which multiplies prev when
	// applyRect is set, as [clip.mask1] applies it.
	prev      *clipNode
	rect      geom.Rect
	applyRect bool
	// depth counts the nodes down the chain not made yet, this one included.
	depth int
	// bounds holds every pixel the coverage can be above zero at.
	bounds image.Rectangle
	// work counts the pixels computed in parts so far.
	work int64

	width, height int
	budget        int64
}

// materialize computes the coverage over the page, once.
func (n *clipNode) materialize() error {
	if n.values != nil {
		return nil
	}
	var cur *surface.Mask
	if n.prev != nil {
		if err := n.prev.materialize(); err != nil {
			return err
		}
		cur = n.prev.values
	}
	if n.applyRect {
		m, err := rectMask(n.rect, cur, n.width, n.height, n.budget)
		if err != nil {
			return err
		}
		cur = m
	}
	m, err := raster.Clip(n.path, n.rule, cur, n.width, n.height, n.budget)
	if err != nil {
		return fmt.Errorf("%w: clip mask: %w", ErrTooLarge, err)
	}
	n.values, n.prev, n.shape, n.depth = m, nil, nil, 0
	return nil
}

// in returns the coverage over box, a part of the page, row by row.
func (n *clipNode) in(box image.Rectangle) ([]float32, error) {
	if n.values != nil {
		return maskIn(n.values, box), nil
	}
	out := make([]float32, box.Dx()*box.Dy())
	part := box.Intersect(n.bounds)
	if part.Empty() {
		return out, nil
	}
	n.work += int64(part.Dx()) * int64(part.Dy())
	if n.work > int64(n.width)*int64(n.height) {
		// The parts have cost the page: the page's coverage serves every
		// element painted under the clip from here on.
		if err := n.materialize(); err != nil {
			return nil, err
		}
		return maskIn(n.values, box), nil
	}
	var pre []float32
	if n.prev != nil {
		var err error
		if pre, err = n.prev.in(part); err != nil {
			return nil, err
		}
	}
	if n.applyRect {
		pre = rectIn(n.rect, pre, part, n.width, n.height)
	}
	values := n.shape.CoverageIn(part, pre)
	if part == box {
		return values, nil
	}
	w := part.Dx()
	for y := part.Min.Y; y < part.Max.Y; y++ {
		at := (y-box.Min.Y)*box.Dx() + part.Min.X - box.Min.X
		copy(out[at:at+w], values[(y-part.Min.Y)*w:(y-part.Min.Y+1)*w])
	}
	return out, nil
}

// span returns the pixels the rectangle touches, clamped to the page.
func (c *clip) span(width, height int) (x0, x1, y0, y1 int) {
	clampTo := func(v float64, hi int) int {
		switch {
		case math.IsNaN(v) || v < 0:
			return 0
		case v > float64(hi):
			return hi
		}
		return int(v)
	}
	return clampTo(math.Floor(c.rect.Min.X), width), clampTo(math.Ceil(c.rect.Max.X), width),
		clampTo(math.Floor(c.rect.Min.Y), height), clampTo(math.Ceil(c.rect.Max.Y), height)
}

// overlap returns the length of [p, p+1] inside [lo, hi].
func overlap(p, lo, hi float64) float32 {
	return float32(max(0, min(p+1, hi)-max(p, lo)))
}

// applyRect scales cov by the share of each pixel the rectangle covers. A
// shape and a rectangle that both cover part of an edge pixel are taken to
// overlap in proportion, which is exact where either covers the pixel whole.
func (c *clip) applyRect(cov *raster.Coverage) {
	if c.inMask {
		return
	}
	for y := range cov.Height {
		fy := overlap(float64(cov.Y+y), c.rect.Min.Y, c.rect.Max.Y)
		row := cov.A[y*cov.Width : (y+1)*cov.Width]
		for x := range row {
			if row[x] == 0 {
				continue
			}
			row[x] *= fy * overlap(float64(cov.X+x), c.rect.Min.X, c.rect.Max.X)
		}
	}
}

// applyPixelRect keeps cov in the whole device pixels the rectangle touches
// ([pixelRect]) and clears it elsewhere, as poppler clips a glyph: Splash
// draws a filled glyph's bitmap pixel by pixel where SplashClip::test passes
// (Splash::fillGlyph2), so a pixel the clipping rectangle touches keeps the
// glyph's whole coverage. ISO 32000-1, 8.5.4 leaves that rasterisation to
// the renderer; this is poppler's convention, not a spec rule.
func (c *clip) applyPixelRect(cov *raster.Coverage) {
	if c.inMask {
		return
	}
	r := pixelRect(c.rect)
	for y := range cov.Height {
		row := cov.A[y*cov.Width : (y+1)*cov.Width]
		if fy := float64(cov.Y + y); fy < r.Min.Y || fy+1 > r.Max.Y {
			clear(row)
			continue
		}
		for x := range row {
			if fx := float64(cov.X + x); fx < r.Min.X || fx+1 > r.Max.X {
				row[x] = 0
			}
		}
	}
}

// owns reports the masks c holds that parent does not share, for the budget
// of the masks a render keeps alive. A node not made yet counts as the mask
// it stands for.
func (c *clip) owns(parent *clip) int {
	n := 0
	if c.node != nil && (parent == nil || c.node != parent.node) {
		n++
	}
	if c.full != nil && (parent == nil || c.full != parent.full) {
		n++
	}
	return n
}
