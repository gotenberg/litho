package blend

import (
	"context"
	"fmt"
	"image"

	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// Attributes are a transparency group's /I and /K entries (ISO 32000-1,
// Table 147), and the blending colour space its /CS names.
type Attributes struct {
	// Isolated composites the group's elements over a transparent initial
	// backdrop rather than over the group's backdrop (11.4.5).
	Isolated bool
	// Knockout composites each element with the group's initial backdrop
	// rather than with the elements painted before it (11.4.6).
	Knockout bool
	// Space is the blending colour space (11.3.4), nil for sRGB. A
	// non-isolated group whose backdrop is its parent's stack composites
	// with that stack (11.4.5), so it blends in the parent's space and
	// Space is ignored.
	Space *Space
}

// Backdrop is what a non-isolated group starts from: the stack of its parent
// group, or a constant colour. The zero value is a transparent backdrop.
type Backdrop struct {
	parent *Group
	color  Components
	alpha  float32
}

// Opaque returns a fully opaque backdrop of colour c, components of the
// group's blending colour space, the one a luminosity soft mask composites
// its group with (11.5.3).
func Opaque(c Components) Backdrop { return Backdrop{color: c, alpha: 1} }

// Backdrop returns the backdrop a non-isolated child of g starts from: g's
// stack as painted so far, or g's own initial backdrop when g is a knockout
// group. Per ISO 32000-1, 11.4.6, NOTE 6, a non-isolated group nested in a
// knockout group shares the knockout group's initial backdrop.
func (g *Group) Backdrop() Backdrop { return Backdrop{parent: g} }

// maxStride is the most values one pixel of a group's colour plane holds:
// the colour channels of the widest space, then the alpha.
const maxStride = surface.MaxPlaneChannels + 1

// Group is the stack of one transparency group: for every pixel of its
// bounds, the accumulated colour and alpha including the initial backdrop
// (Ci and αi), the group shape fgi and the group alpha αgi, which exclude it,
// and the initial backdrop itself (C0 and α0), the variables of ISO 32000-1,
// 11.4.8. Colours are held in the group's blending colour space, additive
// ([Space.store]).
//
// A Group is not safe for concurrent writes.
type Group struct {
	bounds image.Rectangle
	attrs  Attributes
	space  *Space
	// n is the number of colour channels, stride the values of a pixel.
	n, stride int
	// color holds αi·Ci and αi.
	color *surface.Plane
	// rgb views color as a canvas when the group blends in sRGB, else nil.
	rgb *surface.Canvas
	// shape holds fgi.
	shape *surface.Mask
	// alpha holds αgi. It is nil for an isolated group, whose α0 is 0, so
	// αi = Union(0, αgi) = αgi and color's alpha already holds it.
	alpha *surface.Mask
	// initial holds α0·C0 and α0, laid out as color. It is nil for an
	// isolated group, whose initial backdrop is transparent.
	initial []float32
	// direct marks a group [Group.Direct] handed its colour plane to, whose
	// shape plane misses what was painted there.
	direct bool
	// spots holds the ink of each spot colorant plane premultiplied by the
	// alpha, [MaxSpots] of them a pixel, nil until [Group.EnableSpots].
	spots *surface.Plane
}

// NewPage returns the page group of a width × height page: isolated and
// non-knockout over a transparent backdrop (ISO 32000-1, 11.4.7), blending in
// sRGB. Its [Group.Canvas] is then composited over the medium's colour.
//
// A page group keeps no shape plane: it is composited with the medium, never
// into another group, so nothing reads its shape, which it takes as its
// alpha where [Group.Into] or a soft mask asks.
func NewPage(width, height int, maxPixels int64) (*Group, error) {
	return NewPageIn(width, height, nil, maxPixels)
}

// NewPageIn returns the page group of [NewPage] blending in space, nil for
// sRGB: the space the page's /Group /CS names (11.4.7).
func NewPageIn(width, height int, space *Space, maxPixels int64) (*Group, error) {
	return NewPageOn(nil, width, height, space, maxPixels)
}

// NewPageOn returns the page group of [NewPageIn], its colour plane taken
// from pool ([surface.Pool]); a nil pool allocates it.
func NewPageOn(pool *surface.Pool, width, height int, space *Space, maxPixels int64) (*Group, error) {
	g := &Group{bounds: image.Rect(0, 0, width, height), attrs: Attributes{Isolated: true, Space: space}}
	if err := g.allocColor(pool, space, maxPixels); err != nil {
		return nil, fmt.Errorf("allocating page group colour: %w", err)
	}
	return g, nil
}

// allocColor sets g's space and allocates its colour plane, on pool when it
// is not nil.
func (g *Group) allocColor(pool *surface.Pool, space *Space, maxPixels int64) error {
	g.space, g.n = space, space.channels()
	g.stride = g.n + 1
	color, err := pool.NewPlane(g.bounds.Dx(), g.bounds.Dy(), g.n, maxPixels)
	if err != nil {
		return err
	}
	g.color, g.rgb = color, color.Canvas()
	return nil
}

// NewGroup returns an empty group covering bounds, in device pixels, with
// attributes attrs. A non-isolated group copies its initial backdrop from
// backdrop now, so painting the parent afterwards does not reach it; an
// isolated group ignores backdrop. Each of the group's planes is refused
// beyond maxPixels.
func NewGroup(bounds image.Rectangle, backdrop Backdrop, attrs Attributes, maxPixels int64) (*Group, error) {
	w, h := bounds.Dx(), bounds.Dy()
	space := attrs.Space
	if !attrs.Isolated && backdrop.parent != nil {
		space = backdrop.parent.space
	}
	g := &Group{bounds: bounds, attrs: attrs}
	if err := g.allocColor(nil, space, maxPixels); err != nil {
		return nil, fmt.Errorf("allocating group colour: %w", err)
	}
	shape, err := surface.NewMask(w, h, maxPixels)
	if err != nil {
		return nil, fmt.Errorf("allocating group shape: %w", err)
	}
	g.shape = shape
	if attrs.Isolated {
		return g, nil
	}

	if g.alpha, err = surface.NewMask(w, h, maxPixels); err != nil {
		return nil, fmt.Errorf("allocating group alpha: %w", err)
	}
	g.initial = make([]float32, len(g.color.Pix))
	g.copyBackdrop(backdrop)
	// Per 11.4.4, the stack starts at C0 and α0: the first element is
	// composited over the backdrop itself.
	copy(g.color.Pix, g.initial)
	return g, nil
}

// copyBackdrop fills g.initial from backdrop over g's bounds.
func (g *Group) copyBackdrop(backdrop Backdrop) {
	p := backdrop.parent
	if p == nil {
		c, a := g.space.store(backdrop.color), backdrop.alpha
		for i := 0; i < len(g.initial); i += g.stride {
			for k := range g.n {
				g.initial[i+k] = c[k] * a
			}
			g.initial[i+g.n] = a
		}
		return
	}
	src := p.color.Pix
	if p.attrs.Knockout {
		src = p.initial
	}
	if src == nil {
		return
	}
	area := g.bounds.Intersect(p.bounds)
	w := g.bounds.Dx()
	pw := p.bounds.Dx()
	s := g.stride
	for y := area.Min.Y; y < area.Max.Y; y++ {
		dst := s * ((y-g.bounds.Min.Y)*w + area.Min.X - g.bounds.Min.X)
		from := s * ((y-p.bounds.Min.Y)*pw + area.Min.X - p.bounds.Min.X)
		copy(g.initial[dst:dst+s*area.Dx()], src[from:from+s*area.Dx()])
	}
}

// Bounds returns the device pixels the group covers.
func (g *Group) Bounds() image.Rectangle { return g.bounds }

// Space returns the group's blending colour space, nil for sRGB.
func (g *Group) Space() *Space { return g.space }

// Canvas returns the group's accumulated colour αi·Ci and alpha αi, the
// group's origin at [Group.Bounds].Min, when the group blends in sRGB, and
// nil otherwise: [Group.RGB] converts any group. For the page group it is the
// page before the medium's colour shows through (11.4.7). Painting changes
// it.
func (g *Group) Canvas() *surface.Canvas { return g.rgb }

// Plane returns the group's accumulated colour and alpha in its blending
// colour space, held additive: a CMYK colour as its complement
// ([Group.Components] reads a pixel back as components). Painting changes
// it.
func (g *Group) Plane() *surface.Plane { return g.color }

// Components returns the colour and alpha of pixel (x, y) of the group's
// plane, its origin at [Group.Bounds].Min: the colour as components of the
// blending colour space, unpremultiplied, and zero where the alpha is.
func (g *Group) Components(x, y int) (Components, float32) {
	if x < 0 || y < 0 || x >= g.bounds.Dx() || y >= g.bounds.Dy() {
		return Components{}, 0
	}
	p := g.color.Pix[g.stride*(y*g.bounds.Dx()+x):]
	a := p[g.n]
	if a <= 0 {
		return Components{}, 0
	}
	var c Components
	for k := range g.n {
		c[k] = clamp01(p[k] / a)
	}
	return g.space.load(c), a
}

// ComponentsRow sets out[i] to the colour [Group.Components] returns for
// pixel (x0+i, y), for x0 ≤ x0+i < x0+len(out): a caller reading a row
// pays one call for it rather than one a pixel.
func (g *Group) ComponentsRow(x0, y int, out []Components) {
	w := g.bounds.Dx()
	cmyk := g.space.Kind() == CMYK
	for i := range out {
		x := x0 + i
		if x < 0 || y < 0 || x >= w || y >= g.bounds.Dy() {
			out[i] = Components{}
			continue
		}
		p := g.color.Pix[g.stride*(y*w+x):]
		a := p[g.n]
		if a <= 0 {
			out[i] = Components{}
			continue
		}
		var c Components
		for k := range g.n {
			c[k] = clamp01(p[k] / a)
		}
		if cmyk {
			// [Space.load], inlined.
			c = Components{1 - c[0], 1 - c[1], 1 - c[2], 1 - c[3]}
		}
		out[i] = c
	}
}

// RGB returns the group's accumulated colour and alpha as an sRGB canvas:
// [Group.Canvas] itself when the group blends in sRGB, otherwise each pixel
// converted through the space's [Converter] into a new canvas, refused
// beyond maxPixels.
func (g *Group) RGB(ctx context.Context, maxPixels int64) (*surface.Canvas, error) {
	if g.rgb != nil {
		return g.rgb, nil
	}
	w, h := g.bounds.Dx(), g.bounds.Dy()
	out, err := surface.NewCanvas(w, h, maxPixels)
	if err != nil {
		return nil, fmt.Errorf("allocating the group's sRGB canvas: %w", err)
	}
	var (
		last    Components
		lastRGB [3]float32
		seen    bool
	)
	for y := range h {
		if y%rowsPerContextCheck == 0 {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("converting a group to sRGB: %w", err)
			}
		}
		for x := range w {
			i := y*w + x
			p := g.color.Pix[g.stride*i:]
			a := p[g.n]
			if a <= 0 {
				continue
			}
			// A page repeats its colours pixel after pixel, so the last
			// conversion is kept.
			var c Components
			for k := range g.n {
				c[k] = clamp01(p[k] / a)
			}
			if !seen || c != last {
				lastRGB, last, seen = g.space.toRGB(c), c, true
			}
			rgb := lastRGB
			o := out.Pix[4*i : 4*i+4]
			o[0], o[1], o[2], o[3] = rgb[0]*a, rgb[1]*a, rgb[2]*a, a
		}
	}
	return out, nil
}

// RGBReader reads pixels of a group's sRGB canvas, as [Group.RGB] makes it,
// without making it: a caller that needs a few pixels of a page group in
// another space converts those alone. It keeps the last conversion, as RGB
// does, so it is not safe for concurrent use.
type RGBReader struct {
	g       *Group
	last    Components
	lastRGB [3]float32
	seen    bool
}

// RGBReader returns a reader of the group's sRGB canvas.
func (g *Group) RGBReader() *RGBReader {
	return &RGBReader{g: g}
}

// At returns pixel (x, y) of the canvas [Group.RGB] makes, its origin at
// [Group.Bounds].Min, bit for bit: the colour premultiplied, and zero
// outside the group.
func (r *RGBReader) At(x, y int) (red, green, blue, alpha float32) {
	g := r.g
	if g.rgb != nil {
		return g.rgb.At(x, y)
	}
	if x < 0 || y < 0 || x >= g.bounds.Dx() || y >= g.bounds.Dy() {
		return 0, 0, 0, 0
	}
	p := g.color.Pix[g.stride*(y*g.bounds.Dx()+x):]
	a := p[g.n]
	if a <= 0 {
		return 0, 0, 0, 0
	}
	var c Components
	for k := range g.n {
		c[k] = clamp01(p[k] / a)
	}
	if !r.seen || c != r.last {
		r.lastRGB, r.last, r.seen = g.space.toRGB(c), c, true
	}
	rgb := r.lastRGB
	return rgb[0] * a, rgb[1] * a, rgb[2] * a, a
}

// Alpha returns the alpha of pixel (x, y), as [RGBReader.At] does, without
// converting its colour.
func (r *RGBReader) Alpha(x, y int) float32 {
	g := r.g
	if g.rgb != nil {
		_, _, _, a := g.rgb.At(x, y)
		return a
	}
	if x < 0 || y < 0 || x >= g.bounds.Dx() || y >= g.bounds.Dy() {
		return 0
	}
	a := g.color.Pix[g.stride*(y*g.bounds.Dx()+x)+g.n]
	if a <= 0 {
		return 0
	}
	return a
}

// Paint composites one element's contribution at device pixel (x, y): colour
// cs, components of the group's blending colour space, unpremultiplied, with
// source shape fs and source alpha as (ISO 32000-1, 11.4.8, where fs is the
// product of the object, mask and constant shapes and as the product of the
// object alpha and the mask and constant alphas). [Params.Source] computes
// both. Anti-aliased coverage is object shape (11.3.7.2, NOTE 1).
//
// In a knockout group, an element covers each pixel at most once: a second
// call for the same element replaces the first rather than adding to it, as
// a second element would. Pixels outside the group are ignored.
func (g *Group) Paint(x, y int, mode Mode, cs Components, fs, as float32) {
	g.paint(x, y, mode, g.space.store(cs), fs, as, 0, Components{})
}

// paint is [Group.Paint] for a colour already stored as the group holds it,
// leaving the components keep has bits for as the backdrop holds them
// ([Params.Keep]), and painting spot the ink of each spot colorant plane
// ([Params.Spots]) when the group keeps them.
func (g *Group) paint(x, y int, mode Mode, cs Components, fs, as float32, keep uint8, spot Components) {
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
	if g.rgb != nil {
		g.paintRGB(x, y, mode, [3]float32{cs[0], cs[1], cs[2]}, fs, as)
		return
	}
	n, s := g.n, g.stride
	i := (y-g.bounds.Min.Y)*g.bounds.Dx() + x - g.bounds.Min.X
	p := g.color.Pix[s*i : s*i+s : s*i+s]

	// Per 11.4.8, b is 0 in a knockout group and i-1 otherwise: the element
	// composites with the initial backdrop or with the stack so far.
	var b [maxStride]float32
	switch {
	case !g.attrs.Knockout:
		copy(b[:], p)
	case g.initial != nil:
		copy(b[:], g.initial[s*i:s*i+s])
	}
	ab := b[n]

	agPrev := p[n]
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
		a0 = g.initial[s*i+n]
	}

	blended := cs
	if mode != Normal && ab > 0 {
		var cb Components
		for k := range n {
			cb[k] = b[k] / ab
		}
		blended = mode.blendIn(g.space.Kind(), cb, cs)
	}
	if keep != 0 && ab > 0 && g.space.Kind() == CMYK {
		// ISO 32000-1, 11.7.4.3: an overprinted component blends as
		// B(cb, cs) = cb, which leaves it as the backdrop holds it where the
		// element is opaque. Over no backdrop it takes the source's, no ink.
		for k := range n {
			if keep&(1<<k) != 0 {
				blended[k] = b[k] / ab
			}
		}
	}
	// Ct = (fs - as)·αb·Cb + as·((1 - αb)·Cs + αb·B(Cb, Cs)), and
	// αi·Ci = (1 - fs)·αi-1·Ci-1 + Ct, both premultiplied.
	for k := range n {
		ct := (fs-as)*b[k] + as*((1-ab)*cs[k]+ab*blended[k])
		p[k] = (1-fs)*p[k] + ct
	}
	if g.spots != nil {
		g.paintSpots(i, mode, spot, fs, as, ab, keep)
	}
	p[n] = union(a0, ag)
	if g.shape != nil {
		g.shape.A[i] = union(g.shape.A[i], fs)
	}
	if g.alpha != nil {
		g.alpha.A[i] = ag
	}
}

// paintSpots composites the spot colorant inks spot of an element into the
// spot planes at local index i, by the formulas [Group.paint] applies to the
// process colorants, ab the alpha of the backdrop. A spot plane holds ink,
// not its complement, as a group holds CMYK ([Space.store]): 11.4's weighted
// sums give the same result either way, and a plane allocated after painting
// began then holds no ink at once. ISO 32000-1, 11.7.3: a separable blend
// mode blends each spot colorant on its own, as the subtractive rule of
// 11.3.5 blends a CMYK component, and a non-separable one, defined on the
// process colour as a whole, blends spot colorants as Normal. A component
// keep has the bit for overprints (11.7.4.3), as in [Group.paint]. Spot
// planes belong to an isolated group alone ([Group.EnableSpots]): a
// knockout group's backdrop holds no spot ink.
func (g *Group) paintSpots(i int, mode Mode, spot Components, fs, as, ab float32, keep uint8) {
	const s = MaxSpots + 1
	q := g.spots.Pix[s*i : s*i+MaxSpots : s*i+MaxSpots]
	for j := range MaxSpots {
		var bs float32
		if !g.attrs.Knockout {
			bs = q[j]
		}
		blended := spot[j]
		switch {
		case ab <= 0:
		case keep&SpotBit(j) != 0:
			blended = bs / ab
		case mode != Normal && mode.Separable():
			blended = 1 - mode.channel(1-bs/ab, 1-spot[j])
		}
		ct := (fs-as)*bs + as*((1-ab)*spot[j]+ab*blended)
		q[j] = (1-fs)*q[j] + ct
	}
}

// paintRGB is [Group.paint] for a group blending in sRGB, its colour plane a
// canvas: the same arithmetic over three fixed channels, which the compiler
// keeps in registers, so opaque and blended painting cost what they did
// before blending spaces existed. fs and as arrive clamped.
func (g *Group) paintRGB(x, y int, mode Mode, cs [3]float32, fs, as float32) {
	i := (y-g.bounds.Min.Y)*g.bounds.Dx() + x - g.bounds.Min.X
	p := g.rgb.Pix[4*i : 4*i+4 : 4*i+4]

	var b [4]float32
	switch {
	case !g.attrs.Knockout:
		b = [4]float32(p)
	case g.initial != nil:
		b = [4]float32(g.initial[4*i : 4*i+4])
	}

	agPrev := p[3]
	if g.alpha != nil {
		agPrev = g.alpha.A[i]
	}
	var agb float32
	if !g.attrs.Knockout {
		agb = agPrev
	}
	ag := (1-fs)*agPrev + (fs-as)*agb + as
	var a0 float32
	if g.initial != nil {
		a0 = g.initial[4*i+3]
	}

	blended := cs
	if mode != Normal && b[3] > 0 {
		cb := [3]float32{b[0] / b[3], b[1] / b[3], b[2] / b[3]}
		blended = mode.Blend(cb, cs)
	}
	for k := range 3 {
		ct := (fs-as)*b[k] + as*((1-b[3])*cs[k]+b[3]*blended[k])
		p[k] = (1-fs)*p[k] + ct
	}
	p[3] = union(a0, ag)
	if g.shape != nil {
		g.shape.A[i] = union(g.shape.A[i], fs)
	}
	if g.alpha != nil {
		g.alpha.A[i] = ag
	}
}

// result returns the group compositing function's results at local index i
// (11.4.8, Result): the colour C with the backdrop factored out, stored as
// the group holds it, the shape f and the alpha α.
func (g *Group) result(i int) (c Components, f, a float32) {
	n, s := g.n, g.stride
	p := g.color.Pix[s*i : s*i+s]
	a = p[n]
	if g.shape != nil {
		f = g.shape.A[i]
	}
	if g.alpha != nil {
		a = g.alpha.A[i]
	}
	if g.direct || g.shape == nil {
		// What was painted straight onto the colour plane covered at least
		// its alpha.
		f = max(f, a)
	}
	if a <= 0 {
		return c, f, a
	}
	// αn = Union(α0, αgn) >= αgn > 0.
	an := p[n]
	for j := range n {
		c[j] = p[j] / an
	}
	if g.initial != nil {
		// C = Cn + (Cn - C0)·(α0/αgn - α0): remove the backdrop's
		// contribution so compositing the group with it counts it once.
		if a0 := g.initial[s*i+n]; a0 > 0 {
			k := a0/a - a0
			for j := range n {
				c0 := g.initial[s*i+j] / a0
				c[j] = clamp01(c[j] + (c[j]-c0)*k)
			}
		}
	}
	return c, f, a
}

// Into composites the finished group into parent as one element (ISO
// 32000-1, 11.4.4): the group's colour, shape and alpha become the
// element's colour, object shape and object alpha, combined with params'
// mask and constant inputs and blended with params' mode. A group whose
// blending colour space is not its parent's has its colour converted to the
// parent's (Table 147, /CS: the group's space is the colour space of the
// group as a whole when it is painted onto its backdrop).
func (g *Group) Into(ctx context.Context, parent *Group, params Params) error {
	area := g.bounds.Intersect(parent.bounds)
	w := g.bounds.Dx()
	same := g.space.Same(parent.space)
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
			if !same {
				c = convert(c, g.space, parent.space)
			}
			fs, as := params.Source(x, y, f, a)
			parent.paint(x, y, params.Mode, c, fs, as, params.Keep&AllSpots, Components{})
		}
	}
	return nil
}

// Params are the compositing inputs the graphics state contributes to an
// element: its blend mode, constant alpha and soft mask (ISO 32000-1,
// 11.6.4).
type Params struct {
	Mode Mode
	// Opacity is the constant alpha, CA or ca. The zero value is opaque.
	Opacity surface.Opacity
	// SoftMask is the soft mask, nil for /None.
	SoftMask *Mask
	// AlphaIsShape makes the soft mask and the constant alpha shape rather
	// than opacity (the AIS entry, 11.6.4.3).
	AlphaIsShape bool
	// Keep has bit k set for each component k of a CMYK group the element
	// overprints (ISO 32000-1, 8.6.7): the element leaves it as the
	// backdrop holds it, by the compatible overprint blend function of
	// 11.7.4.3. It applies to a CMYK group alone, where the element's
	// components arrive as the group's own ([Group.PaintCoverage]). Bits 4
	// to 7 ([SpotBit]) name the spot colorant planes of a group that keeps
	// them ([Group.EnableSpots]), which any element may leave.
	Keep uint8
	// Spots is the ink the element paints on each spot colorant plane of
	// a group that keeps them, in [0, 1], as Keep leaves them.
	Spots Components
}

// Source returns the source shape and source alpha at device pixel (x, y) of
// an element with the given object shape and object alpha (11.3.7.2, 11.4.8): fs = fj·fm·fk and αs = αj·(fm·qm)·(fk·qk). For an
// elementary object both are its coverage.
func (p Params) Source(x, y int, shape, alpha float32) (fs, as float32) {
	m := float32(1)
	if p.SoftMask != nil {
		m = p.SoftMask.At(x, y)
	}
	k := m * p.Opacity.Alpha()
	if p.AlphaIsShape {
		return shape * k, alpha * k
	}
	return shape, alpha * k
}

func union(b, s float32) float32 { return b + s - b*s }

func clamp01(v float32) float32 { return min(max(v, 0), 1) }

// Underlay composites the group over the opaque or partly transparent
// colour bg, components of its blending colour space at alpha a: the medium
// the page group is composited with (ISO 32000-1, 11.4.7).
func (g *Group) Underlay(bg Components, a float32) {
	a = clamp01(a)
	c := g.space.store(bg)
	n, s := g.n, g.stride
	for i := 0; i < len(g.color.Pix); i += s {
		p := g.color.Pix[i : i+s]
		keep := 1 - p[n]
		switch {
		case keep <= 0:
			continue
		case keep >= 1:
			for k := range n {
				p[k] = c[k] * a
			}
			p[n] = a
			continue
		}
		for k := range n {
			p[k] += c[k] * a * keep
		}
		p[n] += a * keep
	}
}
