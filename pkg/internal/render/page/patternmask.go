package page

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/render/imgpaint"
	"github.com/gotenberg/litho/pkg/internal/render/raster"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// patternStencil paints an image mask whose fill colour is a pattern (ISO
// 32000-1, 8.9.6.2: the mask's samples mark where the current fill colour,
// a pattern included, is painted). The stencil is drawn opaque into a
// raster of the page, under the current clip, and its coverage then paints
// the pattern as a filled path's would, in the fill alpha.
func (r *renderer) patternStencil(s *scope, mask *imgpaint.Alpha, p imgpaint.Params, st *graphics.State) error {
	w, h := r.width, r.height
	release, err := r.reserve(int64(w) * int64(h))
	if err != nil {
		return err
	}
	defer release()
	scratch, err := surface.NewCanvas(w, h, r.budget)
	if err != nil {
		return fmt.Errorf("%w: image mask raster: %w", ErrTooLarge, err)
	}
	p.Opacity = surface.Opaque
	if err := imgpaint.DrawStencil(r.ctx, scratch, mask, 1, 1, 1, p); err != nil {
		return fmt.Errorf("drawing image mask: %w", err)
	}
	cov := coverageOf(scratch)
	if cov.Empty() {
		return nil
	}
	return r.paintCoverage(s, cov, st.Fill, st.FillAlpha)
}

// coverageOf returns the alpha of c as a coverage over the pixels it
// touches.
func coverageOf(c *surface.Canvas) *raster.Coverage {
	x0, y0, x1, y1 := c.Width, c.Height, -1, -1
	for y := range c.Height {
		for x := range c.Width {
			if c.Pix[4*(y*c.Width+x)+3] > 0 {
				x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
			}
		}
	}
	if x1 < x0 {
		return &raster.Coverage{}
	}
	cov := &raster.Coverage{X: x0, Y: y0, Width: x1 - x0 + 1, Height: y1 - y0 + 1}
	cov.A = make([]float32, cov.Width*cov.Height)
	for y := range cov.Height {
		for x := range cov.Width {
			cov.A[y*cov.Width+x] = c.Pix[4*((y0+y)*c.Width+x0+x)+3]
		}
	}
	return cov
}
