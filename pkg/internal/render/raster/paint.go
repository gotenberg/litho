package raster

import "github.com/gotenberg/litho/pkg/internal/render/surface"

// PaintSolid composites the unpremultiplied colour r, g, b at constant alpha
// a onto dst through cov, source-over (the Normal blend mode, ISO 32000-1,
// 11.3.5): each pixel takes alpha a times its coverage, the shape and
// opacity terms of 11.3.7 multiplied. Pixels of cov outside dst are ignored.
func PaintSolid(dst *surface.Canvas, cov *Coverage, r, g, b, a float32) {
	if cov == nil || a <= 0 {
		return
	}
	a = min(a, 1)
	y0, y1 := max(cov.Y, 0), min(cov.Y+cov.Height, dst.Height)
	x0, x1 := max(cov.X, 0), min(cov.X+cov.Width, dst.Width)
	for y := y0; y < y1; y++ {
		row := cov.A[(y-cov.Y)*cov.Width:]
		for x := x0; x < x1; x++ {
			if c := row[x-cov.X]; c > 0 {
				dst.BlendOver(x, y, r, g, b, a*c)
			}
		}
	}
}
