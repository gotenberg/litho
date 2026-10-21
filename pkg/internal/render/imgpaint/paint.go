package imgpaint

import (
	"context"
	"fmt"
	"image"
	"image/draw"
	"math"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// rowsPerContextCheck is how many device rows run between two checks of the
// context. A row costs at most the canvas width times (maxBoxTaps+1)² reads, so
// 32 rows keeps cancellation prompt without a check per pixel.
const rowsPerContextCheck = 32

// Params says where and how an image is painted.
type Params struct {
	// Matrix maps the image's unit square to device space: the current
	// transformation matrix composed with the page's device matrix
	// (ISO 32000-1, 8.9.4).
	Matrix geom.Matrix
	// Interpolate is the image's /Interpolate (ISO 32000-1, 8.9.5.3, Table
	// 89). For [DrawStencil] it is the mask's own.
	Interpolate bool
	// Opacity is the constant alpha (ISO 32000-1, 11.6.4.4, /ca). The zero
	// value paints opaque.
	Opacity surface.Opacity
	// Clip is the current clip as coverage, the size of the canvas; nil
	// clips nothing.
	Clip *surface.Mask
}

// Masks are the transparency an image's dictionary adds to its colour.
type Masks struct {
	// SMask is the /SMask plane ([SoftMask]), or nil.
	SMask *Alpha
	// Matte is the soft mask's /Matte converted to sRGB on 0 to 1, or nil.
	// Set it only when the colour still holds the preblended samples
	// (ISO 32000-1, 11.6.5.3): the decoder's DecodeImage already undoes the
	// preblending, DecodeImageSamples does not. It needs SMask.
	Matte []float32
	// Mask is the /Mask plane, a stencil ([StencilMask]) or a colour key
	// ([ColorKey]), or nil.
	Mask *Alpha
}

// source reads the colour of a decoded image, sample by sample.
type source struct {
	pix        []uint8
	stride, ch int
}

// newSource wraps img for sampling. A Gray or RGBA image, which is what the
// decoder returns, is read in place; any other is copied once into RGBA. Only
// colour is read: transparency comes from [Masks].
func newSource(img image.Image) source {
	switch m := img.(type) {
	case *image.Gray:
		return source{pix: m.Pix, stride: m.Stride, ch: 1}
	case *image.RGBA:
		return source{pix: m.Pix, stride: m.Stride, ch: 4}
	case *image.NRGBA:
		return source{pix: m.Pix, stride: m.Stride, ch: 4}
	}
	b := img.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	return source{pix: rgba.Pix, stride: rgba.Stride, ch: 4}
}

// rgb returns the colour of sample (i, j) on 0 to 1.
func (s *source) rgb(i, j int) (float32, float32, float32) {
	o := j*s.stride + i*s.ch
	if s.ch == 1 {
		v := float32(s.pix[o]) * (1.0 / 255)
		return v, v, v
	}
	return float32(s.pix[o]) * (1.0 / 255), float32(s.pix[o+1]) * (1.0 / 255), float32(s.pix[o+2]) * (1.0 / 255)
}

// Draw paints img onto dst under p, made transparent by masks. img is the
// decoder's reading of an image XObject (ISO 32000-1, 8.9.5). A matrix that
// maps the unit square to a line or a point paints nothing.
func Draw(ctx context.Context, dst *surface.Canvas, img image.Image, masks Masks, p Params) error {
	b := img.Bounds()
	if b.Empty() {
		return nil
	}
	if masks.Matte != nil && (len(masks.Matte) != 3 || masks.SMask == nil) {
		return fmt.Errorf("%w: a /Matte of %d components needs an sRGB triple and a soft mask", ErrBadMask, len(masks.Matte))
	}
	src := newSource(img)
	job := job{src: &src, w: b.Dx(), h: b.Dy(), interpolate: p.Interpolate}
	if masks.Matte != nil {
		job.matte = [3]float32{masks.Matte[0], masks.Matte[1], masks.Matte[2]}
		job.matted = true
	}
	for _, plane := range []*Alpha{masks.Mask, masks.SMask} {
		if plane == nil {
			continue
		}
		if err := plane.valid(); err != nil {
			return err
		}
		// A matted soft mask is never folded: the preblended colour is
		// already premultiplied toward the matte, and the preblending is
		// undone with the mask's own value after filtering, below.
		if job.matted && plane == masks.SMask {
			continue
		}
		// A plane on the image's own grid is folded into the colour taps, so
		// the colour is filtered premultiplied.
		if plane.Width == job.w && plane.Height == job.h {
			job.folded = append(job.folded, plane)
			continue
		}
		job.separate = append(job.separate, plane)
	}
	if job.matted {
		job.matteIndex = len(job.separate)
		job.separate = append(job.separate, masks.SMask)
	}
	return job.run(ctx, dst, p)
}

// DrawStencil paints an image mask, given as its plane, in the fill colour r, g, b (sRGB on
// 0 to 1) under p (ISO 32000-1, 8.9.6.2): the mask's opaque samples take the
// colour, its transparent ones leave dst as it is. p.Interpolate is the
// mask's /Interpolate.
func DrawStencil(ctx context.Context, dst *surface.Canvas, mask *Alpha, r, g, b float32, p Params) error {
	if err := mask.valid(); err != nil {
		return err
	}
	job := job{
		colour:      [3]float32{r, g, b},
		w:           mask.Width,
		h:           mask.Height,
		interpolate: p.Interpolate,
		folded:      []*Alpha{mask},
	}
	return job.run(ctx, dst, p)
}

// job is one image painting: the colour (a source, or one colour when src is
// nil), its grid, and the planes that make it transparent.
type job struct {
	src         *source
	colour      [3]float32
	w, h        int
	interpolate bool
	folded      []*Alpha
	separate    []*Alpha
	matted      bool
	matte       [3]float32
	matteIndex  int
}

func (j *job) run(ctx context.Context, dst *surface.Canvas, p Params) error {
	if p.Clip != nil && (p.Clip.Width != dst.Width || p.Clip.Height != dst.Height) {
		return fmt.Errorf("%w: %dx%d clip on a %dx%d canvas", ErrBadClip, p.Clip.Width, p.Clip.Height, dst.Width, dst.Height)
	}
	constAlpha := p.Opacity.Alpha()
	if !(constAlpha > 0) {
		return nil
	}
	inv, ok := p.Matrix.Invert()
	if !ok {
		// A singular matrix maps the image to a line or a point, which covers
		// no pixel centre.
		return nil
	}
	x0, y0, x1, y1 := deviceBounds(p.Matrix, dst.Width, dst.Height)
	if x0 >= x1 || y0 >= y1 {
		return nil
	}

	eux, euy := math.Abs(inv[0])+math.Abs(inv[2]), math.Abs(inv[1])+math.Abs(inv[3])
	img := newGrid(j.w, j.h, eux, euy, j.interpolate)
	sep := make([]grid, len(j.separate))
	for k, plane := range j.separate {
		sep[k] = newGrid(plane.Width, plane.Height, eux, euy, plane.Interpolate)
	}

	var xt, yt axisTaps
	for y := y0; y < y1; y++ {
		if (y-y0)%rowsPerContextCheck == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("painting an image: %w", err)
			}
		}
		cy := float64(y) + 0.5
		for x := x0; x < x1; x++ {
			cx := float64(x) + 0.5
			u := inv[0]*cx + inv[2]*cy + inv[4]
			v := inv[1]*cx + inv[3]*cy + inv[5]
			// Image space covers [0, w) × [0, h) samples, so the unit square
			// is half open: u in [0, 1), v in (0, 1].
			if !(u >= 0 && u < 1 && v > 0 && v <= 1) {
				continue
			}
			clip := float32(1)
			if p.Clip != nil {
				if clip = p.Clip.A[y*p.Clip.Width+x]; clip <= 0 {
					continue
				}
			}

			img.taps(u, v, &xt, &yt)
			var pr, pg, pb, a float32
			for _, ty := range yt.t[:yt.n] {
				for _, tx := range xt.t[:xt.n] {
					w := ty.w * tx.w
					for _, plane := range j.folded {
						w *= plane.at(tx.i, ty.i)
					}
					if w == 0 {
						continue
					}
					a += w
					if j.src != nil {
						r, g, b := j.src.rgb(tx.i, ty.i)
						pr += w * r
						pg += w * g
						pb += w * b
					}
				}
			}
			if a <= 0 {
				continue
			}
			var r, g, b float32
			if j.src != nil {
				r, g, b = pr/a, pg/a, pb/a
			} else {
				r, g, b = j.colour[0], j.colour[1], j.colour[2]
			}

			total := a * constAlpha * clip
			for k := range sep {
				s := sampleAlpha(&sep[k], j.separate[k], u, v, &xt, &yt)
				if j.matted && k == j.matteIndex && s > 0 {
					// ISO 32000-1, 11.6.5.3: c' = m + α(c − m), so c = m + (c' − m)/α.
					// Filtering c' and α separately and dividing after keeps the
					// filter weighted by α, as c' − m is premultiplied by it.
					r = j.matte[0] + (r-j.matte[0])/s
					g = j.matte[1] + (g-j.matte[1])/s
					b = j.matte[2] + (b-j.matte[2])/s
				}
				total *= s
			}
			if total <= 0 {
				continue
			}
			total = min(total, 1)
			i := 4 * (y*dst.Width + x)
			keep := 1 - total
			dst.Pix[i] = clamp01(r)*total + dst.Pix[i]*keep
			dst.Pix[i+1] = clamp01(g)*total + dst.Pix[i+1]*keep
			dst.Pix[i+2] = clamp01(b)*total + dst.Pix[i+2]*keep
			dst.Pix[i+3] = total + dst.Pix[i+3]*keep
		}
	}
	return nil
}

// sampleAlpha filters plane, laid on g, at (u, v).
func sampleAlpha(g *grid, plane *Alpha, u, v float64, xt, yt *axisTaps) float32 {
	g.taps(u, v, xt, yt)
	var s float32
	for _, ty := range yt.t[:yt.n] {
		for _, tx := range xt.t[:xt.n] {
			s += ty.w * tx.w * plane.at(tx.i, ty.i)
		}
	}
	return s
}

// deviceBounds returns the pixel rectangle [x0, x1) × [y0, y1) holding the
// image of the unit square under m, intersected with a width × height canvas.
func deviceBounds(m geom.Matrix, width, height int) (x0, y0, x1, y1 int) {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, c := range [4]geom.Point{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 0, Y: 1}, {X: 1, Y: 1}} {
		d := m.Apply(c)
		minX, maxX = min(minX, d.X), max(maxX, d.X)
		minY, maxY = min(minY, d.Y), max(maxY, d.Y)
	}
	// Clamped in floating point first: a corner far off the canvas would
	// overflow the conversion to int.
	clampTo := func(v float64, hi int) int {
		return int(min(max(v, 0), float64(hi)))
	}
	return clampTo(math.Floor(minX), width), clampTo(math.Floor(minY), height),
		clampTo(math.Ceil(maxX), width), clampTo(math.Ceil(maxY), height)
}

func clamp01(v float32) float32 {
	return min(max(v, 0), 1)
}
