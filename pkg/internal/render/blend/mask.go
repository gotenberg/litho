package blend

import (
	"context"
	"fmt"
	"image"

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

// AlphaMask derives a soft mask of /S /Alpha from the rendered group g (ISO
// 32000-1, 11.5.2): the group alpha αgn, the backdrop factored out, through
// the transfer function tr (nil for /Identity). Outside the group's bounds,
// the mask is tr(0) (11.6.5.2). The mask covers width × height device
// pixels and is refused beyond maxPixels.
func AlphaMask(ctx context.Context, g *Group, tr *function.Function, width, height int, maxPixels int64) (*surface.Mask, error) {
	t, err := newTransfer(tr)
	if err != nil {
		return nil, err
	}
	return deriveMask(ctx, g, width, height, maxPixels, t.apply(0), func(i int) float32 {
		_, _, a := g.result(i)
		return t.apply(a)
	})
}

// LuminosityMask derives a soft mask of /S /Luminosity from the rendered
// group g (ISO 32000-1, 11.5.3): the group composited over a fully opaque
// backdrop of colour bc, the /BC entry converted to sRGB, then reduced to
// luminosity and passed through the transfer function tr (nil for
// /Identity). Outside the group's bounds the mask is tr(Y(bc)) (11.6.5.2).
// The mask covers width × height device pixels and is refused beyond
// maxPixels.
//
// A non-isolated g is built over [Opaque](bc), so its elements blend with
// the backdrop the mask defines. The luminosity is the DeviceRGB formula of
// 11.5.3, Y = 0.30 R + 0.59 G + 0.11 B, applied to the sRGB blending space.
func LuminosityMask(ctx context.Context, g *Group, bc [3]float32, tr *function.Function, width, height int, maxPixels int64) (*surface.Mask, error) {
	t, err := newTransfer(tr)
	if err != nil {
		return nil, err
	}
	return deriveMask(ctx, g, width, height, maxPixels, t.apply(lum(bc)), func(i int) float32 {
		c, _, a := g.result(i)
		// 11.5.3: C = (1 - αg)·C0 + αg·Cg.
		var mixed [3]float32
		for k := range 3 {
			mixed[k] = (1-a)*bc[k] + a*c[k]
		}
		return t.apply(lum(mixed))
	})
}

// deriveMask fills a width × height mask with outside, then with value(i)
// at every pixel the group covers, i the group-local index.
func deriveMask(ctx context.Context, g *Group, width, height int, maxPixels int64, outside float32, value func(i int) float32) (*surface.Mask, error) {
	m, err := surface.NewMask(width, height, maxPixels)
	if err != nil {
		return nil, fmt.Errorf("allocating soft mask: %w", err)
	}
	if outside != 0 {
		for i := range m.A {
			m.A[i] = outside
		}
	}
	area := g.bounds.Intersect(image.Rect(0, 0, width, height))
	w := g.bounds.Dx()
	for y := area.Min.Y; y < area.Max.Y; y++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("deriving soft mask: %w", err)
		}
		row := (y - g.bounds.Min.Y) * w
		for x := area.Min.X; x < area.Max.X; x++ {
			m.A[y*width+x] = value(row + x - g.bounds.Min.X)
		}
	}
	return m, nil
}
