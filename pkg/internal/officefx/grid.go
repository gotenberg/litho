package officefx

import (
	"context"
	"image"
	"image/color"
	"math"

	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// Grid is the pixel grid of an effect bitmap: the page area Box covers,
// W by H pixels, Unit pixels a point.
type Grid struct {
	Box  typeset.Rect
	W, H int
	Unit float64
}

// PlanGrid returns the grid the effect engine's rule gives an effect over
// the page area box whose blur has standard deviation sigma points
// ([effect.Plan]), the box widened to whole pixels; reduced reports that
// the engine's surface cap lowered its resolution. An effect that grows or
// shrinks its silhouette by morph points before the blur takes the
// largest resolution at most the rule's at which morph is a whole number
// of pixels: [effect.Dilate] and [effect.Erode] move by whole pixels, and
// a rounded distance would move the edge by up to half a pixel. It
// returns false for an area too small to draw.
func PlanGrid(box typeset.Rect, sigma, morph float64) (g Grid, reduced, ok bool) {
	if !usable(box) {
		return Grid{}, false, false
	}
	res, ok := effect.Plan(sigma, box.Width, box.Height)
	if !ok {
		return Grid{}, false, false
	}
	if n := math.Floor(morph * res.Scale); n >= 1 && numeric.Finite(n) {
		res.Scale = n / morph
		res.W, res.H = int(math.Ceil(box.Width*res.Scale)), int(math.Ceil(box.Height*res.Scale)) // fits int: within the sides Plan returned
	}
	box.Width, box.Height = float64(res.W)/res.Scale, float64(res.H)/res.Scale
	return Grid{Box: box, W: res.W, H: res.H, Unit: res.Scale}, res.Reduced, true
}

// usable reports a rectangle of finite coordinates and positive sides.
func usable(r typeset.Rect) bool {
	return numeric.Finite(r.X) && numeric.Finite(r.Y) && numeric.Finite(r.Width) && numeric.Finite(r.Height) && r.Width > 0 && r.Height > 0
}

// Span returns the length v points spans on the grid, in pixels.
func (g Grid) Span(v float64) float64 {
	return v * g.Unit
}

// Whole returns the length v points spans on the grid in whole pixels,
// rounded.
func (g Grid) Whole(v float64) int {
	return int(min(math.Round(max(g.Span(v), 0)), effect.MaxKernelRadius)) // fits int: at most MaxKernelRadius
}

// Pixels returns the transform from page points to the grid's pixels.
func (g Grid) Pixels() typeset.Matrix {
	sx, sy := float64(g.W)/g.Box.Width, float64(g.H)/g.Box.Height
	return typeset.Matrix{A: sx, D: sy, E: -g.Box.X * sx, F: -g.Box.Y * sy}
}

// Place returns where the grid's surfaces sit on the page.
func (g Grid) Place() effect.Grid {
	return effect.Grid{X: g.Box.X, Y: g.Box.Y, Scale: float64(g.W) / g.Box.Width}
}

// Surface returns a transparent surface of the grid's size in format f,
// charged to the conversion's budget.
func (g Grid) Surface(ctx context.Context, f effect.Format) (*effect.Surface, error) {
	return effect.New(ctx, f, g.W, g.H, g.Place())
}

// Image returns the bitmap img placed over the grid's area, encoded at
// once so the bitmap is dropped.
func (g Grid) Image(ctx context.Context, img image.Image) (typeset.Fragment, error) {
	src, err := typeset.EncodeImage(ctx, img)
	if err != nil {
		return nil, err
	}
	return &typeset.Image{Rect: g.Box, Source: src}, nil
}

// Coverage returns the coverage on g of paths, moved by tr in page
// coordinates first.
func (g Grid) Coverage(ctx context.Context, paths []effect.Path, tr typeset.Matrix) (*effect.Surface, error) {
	cov, err := g.Surface(ctx, effect.Coverage)
	if err != nil {
		return nil, err
	}
	if err := effect.Fill(ctx, cov, paths, mul(g.Pixels(), tr)); err != nil {
		cov.Release()
		return nil, err
	}
	return cov, nil
}

// BlurEdge blurs s in place by k along both axes, the pixels past the
// bitmap's edge repeating the edge's: a mask its caller clipped to the
// page keeps its coverage at the cut.
func BlurEdge(ctx context.Context, s *effect.Surface, k effect.Kernel) error {
	return effect.Blur(ctx, s, k, k, effect.EdgeDuplicate)
}

// Tinted returns the coverage cov in the one colour c as an image whose
// alpha is the coverage times c's alpha, colours straight, charged to the
// budget for [typeset.EncodeImage] to give back.
func Tinted(ctx context.Context, cov *effect.Surface, c typeset.Color) (*image.NRGBA, error) {
	if err := typeset.ChargeImage(ctx, cov.W, cov.H, color.NRGBAModel); err != nil {
		return nil, err
	}
	return Tint(cov.Pix, cov.W, cov.H, effect.ColorOf(c)), nil
}

// Tint returns the w by h alpha samples in the one colour c as an image,
// each alpha times c's.
func Tint(alpha []uint8, w, h int, c effect.Color) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i, v := range alpha[:w*h] {
		p := img.Pix[4*i : 4*i+4 : 4*i+4]
		p[0], p[1], p[2], p[3] = c.R, c.G, c.B, uint8((uint32(v)*uint32(c.A)+127)/255) //nolint:gosec // a byte times a fraction of 1
	}
	return img
}

// Straight returns the premultiplied RGBA surface s as an image of
// straight colours, charged to the budget for [typeset.EncodeImage] to
// give back.
func Straight(ctx context.Context, s *effect.Surface) (*image.NRGBA, error) {
	if err := typeset.ChargeImage(ctx, s.W, s.H, color.NRGBAModel); err != nil {
		return nil, err
	}
	img := image.NewNRGBA(image.Rect(0, 0, s.W, s.H))
	for i := 0; i < len(s.Pix); i += 4 {
		px, out := s.Pix[i:i+4:i+4], img.Pix[i:i+4:i+4]
		if a := px[3]; a != 0 {
			out[0], out[1], out[2], out[3] = Unpremultiply(px[0], a), Unpremultiply(px[1], a), Unpremultiply(px[2], a), a
		}
	}
	return img, nil
}

// Unpremultiply returns the colour channel c of a pixel premultiplied by
// its alpha a, not zero, as a straight channel, rounded.
func Unpremultiply(c, a uint8) uint8 {
	return uint8(min((uint32(c)*255+uint32(a)/2)/uint32(a), 255)) //nolint:gosec // clamped to a byte
}
