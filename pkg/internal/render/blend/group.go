package blend

import (
	"context"
	"fmt"
	"image"

	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// Attributes are a transparency group's /I and /K entries (ISO 32000-1,
// Table 147).
type Attributes struct {
	// Isolated composites the group's elements over a transparent initial
	// backdrop rather than over the group's backdrop (11.4.5).
	Isolated bool
	// Knockout composites each element with the group's initial backdrop
	// rather than with the elements painted before it (11.4.6).
	Knockout bool
}

// Backdrop is what a non-isolated group starts from: the stack of its parent
// group, or a constant colour. The zero value is a transparent backdrop.
type Backdrop struct {
	parent *Group
	color  [3]float32
	alpha  float32
}

// Opaque returns a fully opaque backdrop of colour c, the one a luminosity
// soft mask composites its group with (11.5.3).
func Opaque(c [3]float32) Backdrop { return Backdrop{color: c, alpha: 1} }

// Backdrop returns the backdrop a non-isolated child of g starts from: g's
// stack as painted so far, or g's own initial backdrop when g is a knockout
// group. Per ISO 32000-1, 11.4.6, NOTE 6, a non-isolated group nested in a
// knockout group shares the knockout group's initial backdrop.
func (g *Group) Backdrop() Backdrop { return Backdrop{parent: g} }

// Group is the stack of one transparency group: for every pixel of its
// bounds, the accumulated colour and alpha including the initial backdrop
// (Ci and αi), the group shape fgi and the group alpha αgi, which exclude it,
// and the initial backdrop itself (C0 and α0), the variables of ISO 32000-1,
// 11.4.8.
//
// A Group is not safe for concurrent writes.
type Group struct {
	bounds image.Rectangle
	attrs  Attributes
	// color holds αi·Ci and αi.
	color *surface.Canvas
	// shape holds fgi.
	shape *surface.Mask
	// alpha holds αgi. It is nil for an isolated group, whose α0 is 0, so
	// αi = Union(0, αgi) = αgi and color's alpha already holds it.
	alpha *surface.Mask
	// initial holds α0·C0 and α0. It is nil for an isolated group, whose
	// initial backdrop is transparent.
	initial *surface.Canvas
}

// NewPage returns the page group of a width × height page: isolated and
// non-knockout over a transparent backdrop (ISO 32000-1, 11.4.7). Its
// [Group.Canvas] is then composited over the medium's colour.
func NewPage(width, height int, maxPixels int64) (*Group, error) {
	return NewGroup(image.Rect(0, 0, width, height), Backdrop{}, Attributes{Isolated: true}, maxPixels)
}

// NewGroup returns an empty group covering bounds, in device pixels, with
// attributes attrs. A non-isolated group copies its initial backdrop from
// backdrop now, so painting the parent afterwards does not reach it; an
// isolated group ignores backdrop. Each of the group's planes is refused
// beyond maxPixels.
func NewGroup(bounds image.Rectangle, backdrop Backdrop, attrs Attributes, maxPixels int64) (*Group, error) {
	w, h := bounds.Dx(), bounds.Dy()
	color, err := surface.NewCanvas(w, h, maxPixels)
	if err != nil {
		return nil, fmt.Errorf("allocating group colour: %w", err)
	}
	shape, err := surface.NewMask(w, h, maxPixels)
	if err != nil {
		return nil, fmt.Errorf("allocating group shape: %w", err)
	}
	g := &Group{bounds: bounds, attrs: attrs, color: color, shape: shape}
	if attrs.Isolated {
		return g, nil
	}

	if g.alpha, err = surface.NewMask(w, h, maxPixels); err != nil {
		return nil, fmt.Errorf("allocating group alpha: %w", err)
	}
	if g.initial, err = surface.NewCanvas(w, h, maxPixels); err != nil {
		return nil, fmt.Errorf("allocating group backdrop: %w", err)
	}
	g.copyBackdrop(backdrop)
	// Per 11.4.4, the stack starts at C0 and α0: the first element is
	// composited over the backdrop itself.
	copy(g.color.Pix, g.initial.Pix)
	return g, nil
}

// copyBackdrop fills g.initial from backdrop over g's bounds.
func (g *Group) copyBackdrop(backdrop Backdrop) {
	p := backdrop.parent
	if p == nil {
		c, a := backdrop.color, backdrop.alpha
		g.initial.Fill(c[0], c[1], c[2], a)
		return
	}
	src := p.color
	if p.attrs.Knockout {
		src = p.initial
	}
	if src == nil {
		return
	}
	area := g.bounds.Intersect(p.bounds)
	w := g.bounds.Dx()
	pw := p.bounds.Dx()
	for y := area.Min.Y; y < area.Max.Y; y++ {
		dst := 4 * ((y-g.bounds.Min.Y)*w + area.Min.X - g.bounds.Min.X)
		from := 4 * ((y-p.bounds.Min.Y)*pw + area.Min.X - p.bounds.Min.X)
		copy(g.initial.Pix[dst:dst+4*area.Dx()], src.Pix[from:from+4*area.Dx()])
	}
}

// Bounds returns the device pixels the group covers.
func (g *Group) Bounds() image.Rectangle { return g.bounds }

// Canvas returns the group's accumulated colour αi·Ci and alpha αi, the
// group's origin at [Group.Bounds].Min. For the page group it is the page
// before the medium's colour shows through (11.4.7). Painting changes it.
func (g *Group) Canvas() *surface.Canvas { return g.color }

// Paint composites one element's contribution at device pixel (x, y): colour
// cs, an unpremultiplied sRGB triple, with source shape fs and source alpha
// as (ISO 32000-1, 11.4.8, where fs is the product of the object, mask and
// constant shapes and as the product of the object alpha and the mask and
// constant alphas). [Params.Source] computes both. Anti-aliased coverage is
// object shape (11.3.7.2, NOTE 1).
//
// In a knockout group, an element covers each pixel at most once: a second
// call for the same element replaces the first rather than adding to it, as
// a second element would. Pixels outside the group are ignored.
func (g *Group) Paint(x, y int, mode Mode, cs [3]float32, fs, as float32) {
	if x < g.bounds.Min.X || y < g.bounds.Min.Y || x >= g.bounds.Max.X || y >= g.bounds.Max.Y {
		return
	}
	fs = clamp01(fs)
	if fs <= 0 {
		// A zero source shape leaves every variable of 11.4.8 unchanged.
		return
	}
	// An alpha cannot exceed its shape: alpha is shape times opacity.
	as = min(clamp01(as), fs)
	i := (y-g.bounds.Min.Y)*g.bounds.Dx() + x - g.bounds.Min.X
	p := g.color.Pix[4*i : 4*i+4 : 4*i+4]

	// Per 11.4.8, b is 0 in a knockout group and i-1 otherwise: the element
	// composites with the initial backdrop or with the stack so far.
	var b [4]float32
	switch {
	case !g.attrs.Knockout:
		b = [4]float32(p)
	case g.initial != nil:
		b = [4]float32(g.initial.Pix[4*i : 4*i+4])
	}

	agPrev := p[3]
	if g.alpha != nil {
		agPrev = g.alpha.A[i]
	}
	// αg0 is 0, so a knockout group's backdrop term αgb vanishes.
	var agb float32
	if !g.attrs.Knockout {
		agb = agPrev
	}
	ag := (1-fs)*agPrev + (fs-as)*agb + as
	var a0 float32
	if g.initial != nil {
		a0 = g.initial.Pix[4*i+3]
	}

	blended := cs
	if mode != Normal && b[3] > 0 {
		cb := [3]float32{b[0] / b[3], b[1] / b[3], b[2] / b[3]}
		blended = mode.Blend(cb, cs)
	}
	// Ct = (fs - as)·αb·Cb + as·((1 - αb)·Cs + αb·B(Cb, Cs)), and
	// αi·Ci = (1 - fs)·αi-1·Ci-1 + Ct, both premultiplied.
	for k := range 3 {
		ct := (fs-as)*b[k] + as*((1-b[3])*cs[k]+b[3]*blended[k])
		p[k] = (1-fs)*p[k] + ct
	}
	p[3] = union(a0, ag)
	g.shape.A[i] = union(g.shape.A[i], fs)
	if g.alpha != nil {
		g.alpha.A[i] = ag
	}
}

// result returns the group compositing function's results at local index i
// (11.4.8, Result): the colour C with the backdrop factored out, the shape f
// and the alpha α.
func (g *Group) result(i int) (c [3]float32, f, a float32) {
	p := g.color.Pix[4*i : 4*i+4]
	f = g.shape.A[i]
	a = p[3]
	if g.alpha != nil {
		a = g.alpha.A[i]
	}
	if a <= 0 {
		return c, f, a
	}
	// αn = Union(α0, αgn) >= αgn > 0.
	an := p[3]
	c = [3]float32{p[0] / an, p[1] / an, p[2] / an}
	if g.initial != nil {
		// C = Cn + (Cn - C0)·(α0/αgn - α0): remove the backdrop's
		// contribution so compositing the group with it counts it once.
		if a0 := g.initial.Pix[4*i+3]; a0 > 0 {
			k := a0/a - a0
			for j := range 3 {
				c0 := g.initial.Pix[4*i+j] / a0
				c[j] = clamp01(c[j] + (c[j]-c0)*k)
			}
		}
	}
	return c, f, a
}

// Into composites the finished group into parent as one element (ISO
// 32000-1, 11.4.4): the group's colour, shape and alpha become the
// element's colour, object shape and object alpha, combined with params'
// mask and constant inputs and blended with params' mode.
func (g *Group) Into(ctx context.Context, parent *Group, params Params) error {
	area := g.bounds.Intersect(parent.bounds)
	w := g.bounds.Dx()
	for y := area.Min.Y; y < area.Max.Y; y++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("compositing group: %w", err)
		}
		row := (y - g.bounds.Min.Y) * w
		for x := area.Min.X; x < area.Max.X; x++ {
			c, f, a := g.result(row + x - g.bounds.Min.X)
			if f <= 0 {
				continue
			}
			fs, as := params.Source(x, y, f, a)
			parent.Paint(x, y, params.Mode, c, fs, as)
		}
	}
	return nil
}

// Params are the compositing inputs the graphics state contributes to an
// element: its blend mode, constant alpha and soft mask (ISO 32000-1,
// 11.6.4).
type Params struct {
	Mode Mode
	// Opacity is the constant alpha, CA or ca. Zero paints nothing: set it
	// to 1 for an opaque element.
	Opacity float32
	// SoftMask holds the soft mask's values over the device, nil for /None.
	SoftMask *surface.Mask
	// AlphaIsShape makes the soft mask and the constant alpha shape rather
	// than opacity (the AIS entry, 11.6.4.3).
	AlphaIsShape bool
}

// Source returns the source shape and source alpha at device pixel (x, y) of
// an element with the given object shape and object alpha (11.3.7.2, 11.4.8): fs = fj·fm·fk and αs = αj·(fm·qm)·(fk·qk). For an
// elementary object both are its coverage.
func (p Params) Source(x, y int, shape, alpha float32) (fs, as float32) {
	m := float32(1)
	if p.SoftMask != nil {
		m = p.SoftMask.At(x, y)
	}
	k := m * clamp01(p.Opacity)
	if p.AlphaIsShape {
		return shape * k, alpha * k
	}
	return shape, alpha * k
}

func union(b, s float32) float32 { return b + s - b*s }

func clamp01(v float32) float32 { return min(max(v, 0), 1) }
