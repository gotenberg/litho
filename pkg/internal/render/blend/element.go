package blend

import (
	"context"
	"fmt"
	"image"

	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// rowsPerContextCheck is how many rows of an element raster are composited
// between two checks of the context.
const rowsPerContextCheck = 64

// Direct returns the group's colour plane when an element painted onto it
// with the Normal blend mode, source over, no soft mask and its constant
// alpha folded into its coverage, ends as [Group.Paint] would leave it, and
// nil otherwise. That holds in an isolated, non-knockout group blending in sRGB, whose
// plane is a canvas: per ISO
// 32000-1, 11.4.8, with b = i-1 and Normal's B(Cb, Cs) = Cs, the colour
// becomes (1 - αs)·Ci-1 + αs·Cs whatever the shape, and αi = αgi needs no
// plane of its own. Only the shape plane is not kept: the group then takes
// each pixel's shape as at least its alpha, which differs from the exact
// shape only for an element whose alpha is below its shape (a constant alpha
// or a soft mask), and only where the group is composited into a knockout
// group (11.4.6, the one place shape and alpha part).
//
// The plane's origin is at [Group.Bounds].Min. A caller that paints on it
// does so before [Group.Into], as it would call Paint.
func (g *Group) Direct() *surface.Canvas {
	if !g.attrs.Isolated || g.attrs.Knockout || g.rgb == nil {
		return nil
	}
	g.direct = true
	return g.rgb
}

// PaintCanvas composites src, the raster of one element (ISO 32000-1,
// 11.4.4), into g: src's pixel (0, 0) is device pixel origin, its colour
// premultiplied, and its alpha the element's object shape and object alpha
// both, as anti-aliased coverage, an image's masks and the clip leave them
// (11.3.7.2). Its colour is sRGB, converted into the group's blending colour
// space (11.3.4). params adds the graphics state's mode, constant alpha and
// soft mask. An element that holds partial shape apart from partial alpha, which
// matters only in a knockout group, is not represented: its shape is taken
// as its alpha.
func (g *Group) PaintCanvas(ctx context.Context, src *surface.Canvas, origin image.Point, params Params) error {
	area := image.Rect(origin.X, origin.Y, origin.X+src.Width, origin.Y+src.Height).Intersect(g.bounds)
	// An element repeats its colours pixel after pixel, an image scaled up
	// most of all, so the last conversion is kept, as [Group.RGB] keeps it.
	var (
		last   [3]float32
		lastCS Components
		seen   bool
	)
	for y := area.Min.Y; y < area.Max.Y; y++ {
		if (y-area.Min.Y)%rowsPerContextCheck == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("compositing an element: %w", err)
			}
		}
		row := src.Pix[4*(y-origin.Y)*src.Width:]
		for x := area.Min.X; x < area.Max.X; x++ {
			p := row[4*(x-origin.X) : 4*(x-origin.X)+4]
			a := p[3]
			if a <= 0 {
				continue
			}
			rgb := [3]float32{clamp01(p[0] / a), clamp01(p[1] / a), clamp01(p[2] / a)}
			if !seen || rgb != last {
				lastCS, last, seen = g.space.fromRGB(rgb), rgb, true
			}
			fs, as := params.Source(x, y, a, a)
			g.paint(x, y, params.Mode, lastCS, fs, as, params.Keep&AllSpots, Components{})
		}
	}
	return nil
}

// PaintPlanes composites the raster of one element whose colour is already
// in the group's blending colour space into g, as [Group.PaintCanvas] does
// for one in sRGB: planes hold the element's components three to a plane,
// in the red, green and blue channels, premultiplied, the first plane's
// alpha its object shape and object alpha. Every plane is the element drawn
// with the same coverage, so they share the alpha; pixel (0, 0) of each is
// device pixel origin. A colour in the group's own space needs no
// conversion (ISO 32000-1, 11.3.4), and none is made: a round trip through
// sRGB would move a CMYK colour by what the sRGB gamut and the profile's
// tables lose.
func (g *Group) PaintPlanes(ctx context.Context, planes []*surface.Canvas, origin image.Point, params Params) error {
	n := g.space.channels()
	if len(planes) == 0 || len(planes) < (n+2)/3 {
		return fmt.Errorf("%w: %d planes for %d components", ErrPlanes, len(planes), n)
	}
	src := planes[0]
	for _, p := range planes[1:] {
		if p.Width != src.Width || p.Height != src.Height {
			return fmt.Errorf("%w: a %dx%d plane beside a %dx%d one", ErrPlanes, p.Width, p.Height, src.Width, src.Height)
		}
	}
	area := image.Rect(origin.X, origin.Y, origin.X+src.Width, origin.Y+src.Height).Intersect(g.bounds)
	for y := area.Min.Y; y < area.Max.Y; y++ {
		if (y-area.Min.Y)%rowsPerContextCheck == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("compositing an element: %w", err)
			}
		}
		base := 4 * (y - origin.Y) * src.Width
		for x := area.Min.X; x < area.Max.X; x++ {
			i := base + 4*(x-origin.X)
			a := src.Pix[i+3]
			if a <= 0 {
				continue
			}
			var c Components
			for k := range n {
				c[k] = clamp01(planes[k/3].Pix[i+k%3] / a)
			}
			fs, as := params.Source(x, y, a, a)
			g.paint(x, y, params.Mode, g.space.store(c), fs, as, params.Keep&AllSpots, Components{})
		}
	}
	return nil
}

// PaintCoverage composites one element of the single colour cs, components
// of the group's blending colour space, into g: coverage holds its shape and alpha
// over the device rectangle rect, row by row, as a filled or stroked path's
// anti-aliased coverage under the clip is both (ISO 32000-1, 11.3.7.2).
// params adds the graphics state's mode, constant alpha and soft mask.
func (g *Group) PaintCoverage(ctx context.Context, rect image.Rectangle, coverage []float32, cs Components, params Params) error {
	stored := g.space.store(cs)
	if len(coverage) < rect.Dx()*rect.Dy() {
		return fmt.Errorf("%w: %d values for a %dx%d coverage", ErrCoverage, len(coverage), rect.Dx(), rect.Dy())
	}
	area := rect.Intersect(g.bounds)
	w := rect.Dx()
	for y := area.Min.Y; y < area.Max.Y; y++ {
		if (y-area.Min.Y)%rowsPerContextCheck == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("compositing an element: %w", err)
			}
		}
		row := coverage[(y-rect.Min.Y)*w:]
		for x := area.Min.X; x < area.Max.X; x++ {
			c := row[x-rect.Min.X]
			if c <= 0 {
				continue
			}
			fs, as := params.Source(x, y, c, c)
			g.paint(x, y, params.Mode, stored, fs, as, params.Keep, params.Spots)
		}
	}
	return nil
}
