package raster

import (
	"cmp"
	"fmt"
	"image"
	"math"
	"slices"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// FillRule selects how the winding number of a point decides whether it is
// inside a path (ISO 32000-1, 8.5.3.3).
type FillRule uint8

const (
	// NonZero paints a point whose winding number is not zero (8.5.3.3.2):
	// f, F, B, b and W.
	NonZero FillRule = iota
	// EvenOdd paints a point crossed an odd number of times (8.5.3.3.3): f*,
	// B*, b* and W*.
	EvenOdd
)

// FillTolerance is the distance, in device pixels, a flattened curve may
// stray from the true one when filled. A tenth of a pixel keeps the area
// error under the 8-bit quantum on any curve a page draws.
const FillTolerance = 0.1

// subBands is the number of uniform bands each pixel row is cut into, on top
// of the cuts at vertices. Bands between vertices are exact for edges that do
// not cross (see the package doc); this bounds the error where two edges cross
// inside a row to a quarter of a pixel in height.
const subBands = 4

// maxRowVertexCuts caps the vertex cuts added to one pixel row. Anti-DoS
// guard, not spec-mandated: each cut is a band that scans every active edge,
// so thousands of vertices in one row (a crafted zigzag) would cost their
// square. Past the cap the row falls back to the uniform bands, an
// approximation of under a quarter pixel. A flattened circle puts at most a
// few vertices in a row; only glyph-scale detail at low resolution nears 64.
const maxRowVertexCuts = 64

// Coverage is the anti-aliased coverage of a filled path over the pixels
// X ≤ x < X+Width, Y ≤ y < Y+Height of a raster, rows top to bottom; pixels
// outside that box have none. Values are in [0, 1].
type Coverage struct {
	X, Y, Width, Height int
	A                   []float32
}

// At returns the coverage of raster pixel (x, y), zero outside the box.
func (c *Coverage) At(x, y int) float32 {
	x -= c.X
	y -= c.Y
	if x < 0 || y < 0 || x >= c.Width || y >= c.Height {
		return 0
	}
	return c.A[y*c.Width+x]
}

// Empty reports whether no pixel is covered.
func (c *Coverage) Empty() bool {
	return c.Width == 0 || c.Height == 0
}

// edge is one straight piece of a path inside the raster, oriented top to
// bottom (y0 < y1); dir is +1 for a piece the path walks downwards.
type edge struct {
	x0, y0, x1, y1 float64
	dir            int
}

type crossing struct {
	x   float64
	dir int
}

// Fill computes the coverage of path, in device space, over a raster of
// width × height pixels whose pixel (0, 0) spans [0, 1) × [0, 1), under rule.
// Every subpath is closed implicitly (ISO 32000-1, 8.5.3.1). When clip is not
// nil the coverage is multiplied by it, and it must be width × height.
//
// width and height are the size of a surface the caller allocated under its
// pixel budget: the coverage holds at most that many values. The work is
// O(E·R) for E edges each spanning R rows; parts of the path outside the
// raster are clipped away before any row is scanned, so coordinates far off
// the page cost nothing and cannot overflow.
func Fill(path geom.Path, rule FillRule, width, height int, clip *surface.Mask) (*Coverage, error) {
	if width <= 0 || height <= 0 || width > math.MaxInt32 || height > math.MaxInt32 {
		return nil, fmt.Errorf("%w: raster %dx%d", ErrSizeMismatch, width, height)
	}
	if clip != nil && (clip.Width != width || clip.Height != height) {
		return nil, fmt.Errorf("%w: %dx%d clip on a %dx%d raster", ErrSizeMismatch, clip.Width, clip.Height, width, height)
	}
	s, err := flattenShape(path, rule, width, height)
	if err != nil {
		return nil, err
	}
	edges := s.edgesIn(image.Rect(0, 0, width, height))
	if len(edges) == 0 {
		return &Coverage{}, nil
	}
	return scan(edges, rule, clip), nil
}

// flattenShape flattens path into a [Shape] whose edges are not computed
// yet.
func flattenShape(path geom.Path, rule FillRule, width, height int) (*Shape, error) {
	if width <= 0 || height <= 0 || width > math.MaxInt32 || height > math.MaxInt32 {
		return nil, fmt.Errorf("%w: raster %dx%d", ErrSizeMismatch, width, height)
	}
	if rule != NonZero && rule != EvenOdd {
		return nil, fmt.Errorf("%w: unknown fill rule %d", ErrInvalidPath, rule)
	}
	view := geom.Rect{Max: geom.Point{X: float64(width), Y: float64(height)}}
	f := flattener{tol: FillTolerance, cull: &view}
	if err := f.run(path); err != nil {
		return nil, fmt.Errorf("flattening the fill path: %w", err)
	}
	return &Shape{polys: f.out, rule: rule, width: width, height: height}, nil
}

// Shape is a path flattened once, in device space, over a raster of width ×
// height pixels, whose coverage under its fill rule [Shape.CoverageIn]
// computes over any part of the raster. A clipping path that holds back a few
// small elements of a large page costs their rows rather than the page's.
//
// A Shape is immutable and safe for concurrent use.
type Shape struct {
	polys         []Polyline
	rule          FillRule
	width, height int
	// sorted holds the edges inside the raster in the order [scan] sorts
	// them, and bounds the box scan computes the coverage over.
	sorted []edge
	bounds image.Rectangle
	// samples holds the segments [Shape.SampleIn] reads, made on its first
	// call.
	samples     []sampleSeg
	samplesOnce sync.Once
}

// NewShape flattens path for coverage under rule over a raster of width ×
// height pixels, as [Fill] flattens it.
func NewShape(path geom.Path, rule FillRule, width, height int) (*Shape, error) {
	s, err := flattenShape(path, rule, width, height)
	if err != nil {
		return nil, err
	}
	if edges := s.edgesIn(image.Rect(0, 0, width, height)); len(edges) > 0 {
		sortEdges(edges)
		s.sorted = edges
		s.bounds = edgeBounds(edges)
	}
	return s, nil
}

// Bounds returns the pixels the shape's coverage can be above zero at: the
// box [Fill] computes the coverage over.
func (s *Shape) Bounds() image.Rectangle {
	return s.bounds
}

// CoverageIn returns the coverage of the shape over box, a part of the
// raster, row by row, each value multiplied by the matching value of clip
// when clip is not nil, as [Fill] multiplies by its clip mask; clip holds a
// value per pixel of box. The values are those [Fill] computes over the whole
// raster, bit for bit: each row of box is scanned as Fill scans it, with the
// same edges, bands and sums, and only the pixels of box are kept. The work
// is that of box's rows, not the raster's.
func (s *Shape) CoverageIn(box image.Rectangle, clip []float32) []float32 {
	box = box.Intersect(image.Rect(0, 0, s.width, s.height))
	out := make([]float32, box.Dx()*box.Dy())
	part := box.Intersect(s.bounds)
	if part.Empty() {
		return out
	}
	edges := s.sorted
	bx0, bx1 := s.bounds.Min.X, s.bounds.Max.X
	acc := make([]float64, bx1-bx0)
	diff := make([]float64, bx1-bx0+1)
	var (
		active []edge
		cuts   []float64
		cross  []crossing
		next   int
	)
	w := box.Dx()
	for y := part.Min.Y; y < part.Max.Y; y++ {
		top := float64(y)
		bot := top + 1
		// The edges active on the row, in the order scan holds them: the
		// sorted edges that start above its bottom and end below its top.
		kept := active[:0]
		for _, e := range active {
			if e.y1 > top {
				kept = append(kept, e)
			}
		}
		active = kept
		for next < len(edges) && edges[next].y0 < bot {
			if edges[next].y1 > top {
				active = append(active, edges[next])
			}
			next++
		}
		cuts, cross = scanRow(acc, diff, active, cuts, cross, s.rule, bx0, top, bot)
		var run float64
		at := (y-box.Min.Y)*w - box.Min.X
		for x := range part.Max.X - bx0 {
			run += diff[x]
			if x+bx0 < part.Min.X {
				continue
			}
			v := float32(min(max(acc[x]+run, 0), 1))
			if clip != nil {
				v *= clip[at+bx0+x]
			}
			out[at+bx0+x] = v
		}
		clear(acc)
		clear(diff)
	}
	return out
}

// sortEdges sorts edges top to bottom, as [scan] does.
func sortEdges(edges []edge) {
	slices.SortFunc(edges, func(p, q edge) int { return cmp.Compare(p.y0, q.y0) })
}

// edgesIn returns the edges of the shape inside box, the parts left or right
// of it pushed onto its sides (see [clipEdge]).
func (s *Shape) edgesIn(box image.Rectangle) []edge {
	view := geom.Rect{
		Min: geom.Point{X: float64(box.Min.X), Y: float64(box.Min.Y)},
		Max: geom.Point{X: float64(box.Max.X), Y: float64(box.Max.Y)},
	}
	var edges []edge
	for _, pl := range s.polys {
		n := len(pl.Pts)
		if n < 2 {
			continue
		}
		for i, a := range pl.Pts {
			edges = clipEdge(edges, a, pl.Pts[(i+1)%n], view)
		}
	}
	return edges
}

// edgeBounds returns the pixels edges span, the box [scan] computes the
// coverage over.
func edgeBounds(edges []edge) image.Rectangle {
	minX, maxX := edges[0].x0, edges[0].x0
	minY, maxY := edges[0].y0, edges[0].y1
	for _, e := range edges {
		minX = min(minX, e.x0, e.x1)
		maxX = max(maxX, e.x0, e.x1)
		minY = min(minY, e.y0)
		maxY = max(maxY, e.y1)
	}
	return image.Rect(int(math.Floor(minX)), int(math.Floor(minY)), int(math.Ceil(maxX)), int(math.Ceil(maxY)))
}

// clipEdge appends the part of segment a→b inside the raster's rows, with the
// parts left or right of the raster pushed onto its left or right side as
// vertical edges. A vertical edge at x = 0 crosses each row where the
// original did, so pixels right of it see the same winding; at x = width it
// changes nothing inside. Interpolations use the halved form so endpoints
// near the float64 limit stay finite.
func clipEdge(edges []edge, a, b geom.Point, view geom.Rect) []edge {
	if a.Y == b.Y {
		return edges
	}
	dir := 1
	if a.Y > b.Y {
		a, b, dir = b, a, -1
	}
	if b.Y <= view.Min.Y || a.Y >= view.Max.Y {
		return edges
	}
	if a.Y < view.Min.Y {
		a = geom.Point{X: lerp(a.X, b.X, param(a.Y, b.Y, view.Min.Y)), Y: view.Min.Y}
	}
	if b.Y > view.Max.Y {
		b = geom.Point{X: lerp(a.X, b.X, param(a.Y, b.Y, view.Max.Y)), Y: view.Max.Y}
	}
	cuts := [4]float64{0, 0, 0, 1}
	nc := 1
	for _, x := range [2]float64{view.Min.X, view.Max.X} {
		if (a.X < x) != (b.X < x) {
			cuts[nc] = param(a.X, b.X, x)
			nc++
		}
	}
	cuts[nc] = 1
	ts := cuts[:nc+1]
	slices.Sort(ts)
	prev := a
	for i := 1; i < len(ts); i++ {
		next := b
		if ts[i] < 1 {
			next = geom.Point{X: lerp(a.X, b.X, ts[i]), Y: lerp(a.Y, b.Y, ts[i])}
		}
		if next.Y > prev.Y {
			e := edge{x0: prev.X, y0: prev.Y, x1: next.X, y1: next.Y, dir: dir}
			if mid := prev.X/2 + next.X/2; mid <= view.Min.X {
				e.x0, e.x1 = view.Min.X, view.Min.X
			} else if mid >= view.Max.X {
				e.x0, e.x1 = view.Max.X, view.Max.X
			}
			e.x0 = min(max(e.x0, view.Min.X), view.Max.X)
			e.x1 = min(max(e.x1, view.Min.X), view.Max.X)
			edges = append(edges, e)
		}
		prev = next
	}
	return edges
}

// param returns t with lerp(a, b, t) = v, for a ≠ b.
func param(a, b, v float64) float64 {
	return min(max((v/2-a/2)/(b/2-a/2), 0), 1)
}

func lerp(a, b, t float64) float64 {
	return a*(1-t) + b*t
}

// scan converts the edges, all inside the raster, into coverage.
func scan(edges []edge, rule FillRule, clip *surface.Mask) *Coverage {
	sortEdges(edges)
	minX, maxX := edges[0].x0, edges[0].x0
	maxY := edges[0].y1
	for _, e := range edges {
		minX = min(minX, e.x0, e.x1)
		maxX = max(maxX, e.x0, e.x1)
		maxY = max(maxY, e.y1)
	}
	bx0, bx1 := int(math.Floor(minX)), int(math.Ceil(maxX))
	by0, by1 := int(math.Floor(edges[0].y0)), int(math.Ceil(maxY))
	if bx1 <= bx0 || by1 <= by0 {
		return &Coverage{}
	}
	cov := &Coverage{X: bx0, Y: by0, Width: bx1 - bx0, Height: by1 - by0}
	cov.A = make([]float32, cov.Width*cov.Height)

	acc := make([]float64, cov.Width)
	diff := make([]float64, cov.Width+1)
	var (
		active []edge
		cuts   []float64
		cross  []crossing
		next   int
	)
	for row := range cov.Height {
		top := float64(by0 + row)
		bot := top + 1
		kept := active[:0]
		for _, e := range active {
			if e.y1 > top {
				kept = append(kept, e)
			}
		}
		active = kept
		for next < len(edges) && edges[next].y0 < bot {
			if edges[next].y1 > top {
				active = append(active, edges[next])
			}
			next++
		}
		cuts, cross = scanRow(acc, diff, active, cuts, cross, rule, bx0, top, bot)
		var run float64
		out := cov.A[row*cov.Width : (row+1)*cov.Width]
		for x := range acc {
			run += diff[x]
			v := float32(min(max(acc[x]+run, 0), 1))
			if clip != nil {
				v *= clip.A[(by0+row)*clip.Width+bx0+x]
			}
			out[x] = v
			acc[x], diff[x] = 0, 0
		}
		diff[len(acc)] = 0
	}
	return cov
}

// scanRow adds the coverage the active edges give the row [top, bot) to acc
// and diff, whose index 0 is pixel bx0, band by band (see [rowCuts]), and
// returns the cut and crossing buffers for the next row.
func scanRow(acc, diff []float64, active []edge, cuts []float64, cross []crossing, rule FillRule, bx0 int, top, bot float64) ([]float64, []crossing) {
	cuts = rowCuts(cuts[:0], active, top, bot)
	for i := 1; i < len(cuts); i++ {
		b0, b1 := cuts[i-1], cuts[i]
		h := b1 - b0
		if h <= 0 {
			continue
		}
		ym := b0/2 + b1/2
		cross = cross[:0]
		for _, e := range active {
			if e.y0 <= ym && ym < e.y1 {
				x := lerp(e.x0, e.x1, (ym-e.y0)/(e.y1-e.y0))
				cross = append(cross, crossing{x: x - float64(bx0), dir: e.dir})
			}
		}
		slices.SortFunc(cross, func(p, q crossing) int { return cmp.Compare(p.x, q.x) })
		winding := 0
		for j := 0; j+1 < len(cross); j++ {
			winding += cross[j].dir
			if inside(rule, winding) {
				addSpan(acc, diff, cross[j].x, cross[j+1].x, h)
			}
		}
	}
	return cuts, cross
}

// rowCuts returns the band boundaries of the row [top, bot): the uniform
// cuts plus every edge endpoint strictly inside the row, up to
// maxRowVertexCuts of them, sorted and deduplicated.
func rowCuts(cuts []float64, active []edge, top, bot float64) []float64 {
	for k := range subBands + 1 {
		cuts = append(cuts, top+float64(k)/subBands)
	}
	extra := 0
	for _, e := range active {
		for _, y := range [2]float64{e.y0, e.y1} {
			if y > top && y < bot && extra < maxRowVertexCuts {
				cuts = append(cuts, y)
				extra++
			}
		}
	}
	if extra > 0 {
		slices.Sort(cuts)
		cuts = slices.Compact(cuts)
	}
	return cuts
}

func inside(rule FillRule, winding int) bool {
	if rule == EvenOdd {
		return winding&1 != 0
	}
	return winding != 0
}

// addSpan adds h times the horizontal coverage of [xa, xb) to the row:
// partial pixels at both ends go to acc, the run of whole pixels between them
// to the difference array diff, so a span costs O(1) whatever its length.
func addSpan(acc, diff []float64, xa, xb, h float64) {
	w := float64(len(acc))
	xa = min(max(xa, 0), w)
	xb = min(max(xb, 0), w)
	if xb <= xa {
		return
	}
	ia, ib := int(xa), int(xb)
	if ia == ib {
		acc[ia] += h * (xb - xa)
		return
	}
	acc[ia] += h * (float64(ia+1) - xa)
	diff[ia+1] += h
	diff[ib] -= h
	if ib < len(acc) {
		acc[ib] += h * (xb - float64(ib))
	}
}

// Clip intersects the clip cur with the inside of path under rule (W or W*,
// ISO 32000-1, 8.5.4) and returns the result as a new mask; cur is not
// modified. A nil cur stands for the whole raster, the initial clip
// (8.4.1, Table 52). The new mask counts against maxPixels.
func Clip(path geom.Path, rule FillRule, cur *surface.Mask, width, height int, maxPixels int64) (*surface.Mask, error) {
	cov, err := Fill(path, rule, width, height, cur)
	if err != nil {
		return nil, fmt.Errorf("computing the clip path: %w", err)
	}
	m, err := surface.NewMask(width, height, maxPixels)
	if err != nil {
		return nil, fmt.Errorf("allocating the clip mask: %w", err)
	}
	for y := range cov.Height {
		copy(m.A[(cov.Y+y)*width+cov.X:], cov.A[y*cov.Width:(y+1)*cov.Width])
	}
	return m, nil
}
