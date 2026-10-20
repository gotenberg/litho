package page

import (
	"fmt"
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
// holds it: an axis-aligned device rectangle, intersected with a coverage
// mask when a clip of any other shape has been set. Most clips are
// rectangles, a page's margins or a form's bounding box, and a rectangle
// costs nothing to intersect, where a mask costs the page's pixels.
//
// A clip is immutable once a graphics state holds it: q shares it, and
// setting a clip makes a new one.
type clip struct {
	// mask is the coverage of the clips that are not rectangles, or nil.
	mask *surface.Mask
	// rect is the intersection of the rectangles, in device pixels.
	rect geom.Rect
	// inMask reports that mask already holds rect, which then only bounds
	// the work.
	inMask bool
	// full is mask with rect applied, made the first time an image or a
	// shading needs one mask, or nil.
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
	out := &clip{mask: c.mask, rect: geom.Rect{
		Min: geom.Point{X: max(c.rect.Min.X, r.Min.X), Y: max(c.rect.Min.Y, r.Min.Y)},
		Max: geom.Point{X: min(c.rect.Max.X, r.Max.X), Y: min(c.rect.Max.Y, r.Max.Y)},
	}}
	return out
}

// withPath returns c narrowed to the device-space path p filled with rule
// (ISO 32000-1, 8.5.4): a rectangle narrows the rectangle, any other shape
// makes a mask of the intersection.
func (c *clip) withPath(p geom.Path, rule raster.FillRule, width, height int, budget int64) (*clip, error) {
	if r, ok := axisRect(p); ok {
		return c.intersectRect(r), nil
	}
	cur, err := c.mask1(width, height, budget)
	if err != nil {
		return nil, err
	}
	m, err := raster.Clip(p, rule, cur, width, height, budget)
	if err != nil {
		return nil, fmt.Errorf("clipping: %w", err)
	}
	return &clip{mask: m, rect: c.rect, inMask: true}, nil
}

// mask1 returns the clip as one mask, rectangle applied, nil when it clips to
// the whole page. It is made once per clip.
func (c *clip) mask1(width, height int, budget int64) (*surface.Mask, error) {
	whole := c.rect.Min.X <= 0 && c.rect.Min.Y <= 0 && c.rect.Max.X >= float64(width) && c.rect.Max.Y >= float64(height)
	if whole || c.inMask {
		return c.mask, nil
	}
	if c.full != nil {
		return c.full, nil
	}
	m, err := surface.NewMask(width, height, budget)
	if err != nil {
		return nil, fmt.Errorf("%w: clip mask: %w", ErrTooLarge, err)
	}
	x0, x1, y0, y1 := c.span(width, height)
	for y := y0; y < y1; y++ {
		fy := overlap(float64(y), c.rect.Min.Y, c.rect.Max.Y)
		for x := x0; x < x1; x++ {
			v := fy * overlap(float64(x), c.rect.Min.X, c.rect.Max.X)
			if c.mask != nil {
				v *= c.mask.A[y*width+x]
			}
			m.A[y*width+x] = v
		}
	}
	c.full = m
	return m, nil
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

// owns reports the masks c holds that parent does not share, for the budget
// of the masks a render keeps alive.
func (c *clip) owns(parent *clip) int {
	n := 0
	if c.mask != nil && (parent == nil || c.mask != parent.mask) {
		n++
	}
	if c.full != nil && (parent == nil || c.full != parent.full) {
		n++
	}
	return n
}
