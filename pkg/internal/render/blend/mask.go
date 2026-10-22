package blend

import (
	"context"
	"fmt"
	"image"
	"math"

	"github.com/gotenberg/litho/pkg/internal/graphics/function"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// transferSteps is the number of intervals a /TR transfer function is
// tabulated over before a mask is derived. Evaluating the function once per
// pixel would allocate per pixel; a table read with linear interpolation
// costs one lookup. Not a resource limit: the step, 1/1024, is a quarter of
// an 8-bit output level, so a continuous function stays exact at the
// resolution the renderer emits.
const transferSteps = 1024

// transfer is a tabulated /TR transfer function. A nil table is the
// identity.
type transfer struct{ table []float32 }

// newTransfer tabulates fn, or returns the identity for nil (the /Identity
// default of ISO 32000-1, Table 144).
func newTransfer(fn *function.Function) (transfer, error) {
	if fn == nil {
		return transfer{}, nil
	}
	// Table 144: TR is a function of one input and one output.
	if fn.Inputs() != 1 || fn.Outputs() != 1 {
		return transfer{}, fmt.Errorf("%w: %d inputs, %d outputs", ErrTransfer, fn.Inputs(), fn.Outputs())
	}
	t := transfer{table: make([]float32, transferSteps+1)}
	in := make([]float64, 1)
	for i := range t.table {
		in[0] = float64(i) / transferSteps
		out, err := fn.Eval(in)
		if err != nil {
			return transfer{}, fmt.Errorf("%w: at %g: %w", ErrTransfer, in[0], err)
		}
		t.table[i] = clamp01(float32(out[0]))
	}
	return t, nil
}

// apply maps v in [0, 1] through the transfer function.
func (t transfer) apply(v float32) float32 {
	v = clamp01(v)
	if t.table == nil {
		return v
	}
	pos := v * transferSteps
	i := int(pos)
	if i >= transferSteps {
		return t.table[transferSteps]
	}
	frac := pos - float32(i)
	return t.table[i] + (t.table[i+1]-t.table[i])*frac
}

// Mask is a soft mask over the device (ISO 32000-1, 11.6.5.2): its values
// over a rectangle of device pixels, and one value everywhere else. A mask
// derived from a group holds values over the group's bounds alone, since
// outside them the group is transparent and the mask constant, so a small
// group makes a small mask whatever the page's size.
//
// A mask derived from a group rendered at a fraction of the device
// resolution ([Mask.Coarse]) holds one value for a square of device pixels
// and reads between them bilinearly.
//
// A Mask is immutable and safe for concurrent reads.
type Mask struct {
	values  *surface.Mask
	origin  image.Point
	outside float32
	// scale is how many device pixels one value spans along each axis, 1
	// at device resolution.
	scale int
}

// NewMask returns the mask holding values, whose pixel (0, 0) is device
// pixel origin, and outside everywhere else.
func NewMask(values *surface.Mask, origin image.Point, outside float32) *Mask {
	return &Mask{values: values, origin: origin, outside: clamp01(outside), scale: 1}
}

// ConstantMask returns the mask whose value is v at every device pixel. A
// mask of 0 hides everything painted through it.
func ConstantMask(v float32) *Mask {
	return &Mask{values: &surface.Mask{}, outside: clamp01(v), scale: 1}
}

// Coarse returns m read as a mask derived at 1/scale of the device
// resolution: its origin and its values count in squares of scale × scale
// device pixels, and the value at a device pixel interpolates bilinearly
// between the values around its centre. A scale below 2 returns m.
func (m *Mask) Coarse(scale int) *Mask {
	if scale < 2 {
		return m
	}
	c := *m
	c.scale = scale
	return &c
}

// At returns the mask's value at device pixel (x, y).
func (m *Mask) At(x, y int) float32 {
	if m.scale > 1 {
		return m.coarseAt(x, y)
	}
	x, y = x-m.origin.X, y-m.origin.Y
	if x < 0 || y < 0 || x >= m.values.Width || y >= m.values.Height {
		return m.outside
	}
	return m.values.A[y*m.values.Width+x]
}

// coarseAt returns the value of a coarse mask at device pixel (x, y): the
// bilinear interpolation of the four values nearest the pixel's centre, the
// nearest values along an edge, and outside past the values' extent.
func (m *Mask) coarseAt(x, y int) float32 {
	s := float64(m.scale)
	u := (float64(x)+0.5)/s - float64(m.origin.X)
	v := (float64(y)+0.5)/s - float64(m.origin.Y)
	w, h := m.values.Width, m.values.Height
	if u < 0 || v < 0 || u >= float64(w) || v >= float64(h) {
		return m.outside
	}
	// Each value sits at the centre of its square.
	u, v = u-0.5, v-0.5
	fx, fy := math.Floor(u), math.Floor(v)
	tx, ty := float32(u-fx), float32(v-fy)
	x0, y0 := int(fx), int(fy)
	x1, y1 := min(x0+1, w-1), min(y0+1, h-1)
	x0, y0 = max(x0, 0), max(y0, 0)
	a := m.values.A
	top := a[y0*w+x0] + (a[y0*w+x1]-a[y0*w+x0])*tx
	bottom := a[y1*w+x0] + (a[y1*w+x1]-a[y1*w+x0])*tx
	return top + (bottom-top)*ty
}

// Pixels returns how many pixels the mask holds values for.
func (m *Mask) Pixels() int64 {
	return int64(m.values.Width) * int64(m.values.Height)
}

// AlphaMask derives a soft mask of /S /Alpha from the rendered group g (ISO
// 32000-1, 11.5.2): the group alpha αgn, the backdrop factored out, through
// the transfer function tr (nil for /Identity). Outside the group's bounds,
// the mask is tr(0) (11.6.5.2). Its values cover the group's bounds and are
// refused beyond maxPixels.
func AlphaMask(ctx context.Context, g *Group, tr *function.Function, maxPixels int64) (*Mask, error) {
	t, err := newTransfer(tr)
	if err != nil {
		return nil, err
	}
	return deriveMask(ctx, g, maxPixels, t.apply(0), func(i int) float32 {
		_, _, a := g.result(i)
		return t.apply(a)
	})
}

// LuminosityMask derives a soft mask of /S /Luminosity from the rendered
// group g (ISO 32000-1, 11.5.3): the group composited over a fully opaque
// backdrop of colour bc, the /BC entry as components of the group's blending
// colour space, then reduced to luminosity and passed through the transfer
// function tr (nil for /Identity). Outside the group's bounds the mask is
// tr(Y(bc)) (11.6.5.2). Its values cover the group's bounds and are refused
// beyond maxPixels.
//
// A non-isolated g is built over [Opaque](bc), so its elements blend with
// the backdrop the mask defines. The luminosity of an RGB group is the
// DeviceRGB formula of 11.5.3, Y = 0.30 R + 0.59 G + 0.11 B, applied to the
// sRGB blending space; a grey group's is its grey, and a CMYK group's that
// of the colour's sRGB.
func LuminosityMask(ctx context.Context, g *Group, bc Components, tr *function.Function, maxPixels int64) (*Mask, error) {
	t, err := newTransfer(tr)
	if err != nil {
		return nil, err
	}
	b := g.space.store(bc)
	return deriveMask(ctx, g, maxPixels, t.apply(g.space.luminosity(b)), func(i int) float32 {
		c, _, a := g.result(i)
		// 11.5.3: C = (1 - αg)·C0 + αg·Cg, a weighted sum that holds in the
		// complemented form of a subtractive space too.
		var mixed Components
		for k := range g.n {
			mixed[k] = (1-a)*b[k] + a*c[k]
		}
		return t.apply(g.space.luminosity(mixed))
	})
}

// deriveMask returns the mask holding value(i) at every pixel of the
// group's bounds, i the group-local index, and outside elsewhere.
func deriveMask(ctx context.Context, g *Group, maxPixels int64, outside float32, value func(i int) float32) (*Mask, error) {
	w, h := g.bounds.Dx(), g.bounds.Dy()
	m, err := surface.NewMask(w, h, maxPixels)
	if err != nil {
		return nil, fmt.Errorf("allocating soft mask: %w", err)
	}
	for y := range h {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("deriving soft mask: %w", err)
		}
		for x := range w {
			m.A[y*w+x] = value(y*w + x)
		}
	}
	return NewMask(m, g.bounds.Min, outside), nil
}
