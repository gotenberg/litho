package layout

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// growRect returns r grown by d on every side, shrunk when d is negative,
// and its corner radii adjusted the way CSS Backgrounds 3, 7.2 adjusts a
// shadow's: a growing radius r becomes r + d, scaled by 1 + (r/d - 1)^3
// when r is under d so a sharp corner stays sharp; a shrinking one r + d,
// floored at 0.
func growRect(r typeset.Rect, radii [4][2]float64, d float64) (typeset.Rect, [4][2]float64) {
	out := typeset.Rect{X: r.X - d, Y: r.Y - d, Width: max(r.Width+2*d, 0), Height: max(r.Height+2*d, 0)}
	if out.Width == 0 {
		out.X = r.X + r.Width/2
	}
	if out.Height == 0 {
		out.Y = r.Y + r.Height/2
	}
	var rr [4][2]float64
	for c := range radii {
		for a := range 2 {
			v := radii[c][a]
			switch {
			case v <= 0:
			case d >= 0 && v < d:
				q := v/d - 1
				rr[c][a] = v + d*(1+q*q*q)
			default:
				rr[c][a] = max(v+d, 0)
			}
		}
		if rr[c][0] <= 0 || rr[c][1] <= 0 {
			rr[c] = [2]float64{}
		}
	}
	return out, fitRadii(out, rr)
}

// fitRadii scales radii down so adjacent ones fit their side (CSS
// Backgrounds 3, 5.5).
func fitRadii(r typeset.Rect, radii [4][2]float64) [4][2]float64 {
	f := 1.0
	ratio := func(side, sum float64) {
		if sum > 0 {
			f = min(f, side/sum)
		}
	}
	ratio(r.Width, radii[css.TopLeft][0]+radii[css.TopRight][0])
	ratio(r.Width, radii[css.BottomLeft][0]+radii[css.BottomRight][0])
	ratio(r.Height, radii[css.TopLeft][1]+radii[css.BottomLeft][1])
	ratio(r.Height, radii[css.TopRight][1]+radii[css.BottomRight][1])
	if f < 1 {
		for c := range radii {
			radii[c][0] *= f
			radii[c][1] *= f
		}
	}
	return radii
}

// shadows paints the outer shadows (inset false) or the inner ones (inset
// true) of box b of style st on the border box r with outer corner radii
// and border widths bd (CSS Backgrounds 3, 7.1): outer shadows under the
// background, clipped out of the border box, inner ones over it, clipped
// to the padding box. The first shadow paints on top.
func (p *painter) shadows(b *box, st *css.Style, r typeset.Rect, radii [4][2]float64, bd [4]float64, inset bool) {
	for _, sh := range slices.Backward(st.BoxShadow) {
		if sh.Inset != inset {
			continue
		}
		col, ok := colorOf(sh.Color)
		x, y, blur, spread := clamp(sh.X), clamp(sh.Y), max(clamp(sh.Blur), 0), clamp(sh.Spread)
		if !ok {
			continue
		}
		if inset {
			p.innerShadow(b, r, radii, bd, x, y, blur, spread, col)
		} else {
			p.outerShadow(b, r, radii, x, y, blur, spread, col)
		}
	}
}

// outerShadow paints one outer shadow of the border box r.
func (p *painter) outerShadow(b *box, r typeset.Rect, radii [4][2]float64, x, y, blur, spread float64, col typeset.Color) {
	shape, sradii := growRect(r, radii, spread)
	shape.X += x
	shape.Y += y
	if shape.Width <= 0 || shape.Height <= 0 {
		return
	}
	reach := 1.5 * blur
	bounds := union(r, typeset.Rect{X: shape.X - reach, Y: shape.Y - reach, Width: shape.Width + 2*reach, Height: shape.Height + 2*reach})
	bounds = typeset.Rect{X: bounds.X - 1, Y: bounds.Y - 1, Width: bounds.Width + 2, Height: bounds.Height + 2}
	// The shadow never shows under the box (CSS Backgrounds 3, 7.1).
	clip := &typeset.Path{Ops: append(rectOps(bounds), roundedRect(r, radii)...), Fill: &typeset.Fill{Rule: typeset.EvenOdd}}
	g := &ditem{group: true, fx: &groupFx{opacity: 1, clip: clip}}
	if frag := p.blurred([]effect.Path{{Ops: roundedRect(shape, sradii)}}, nil, shape, blur, col, r.Y); frag != nil {
		g.kids = append(g.kids, &ditem{frag: frag, y0: bounds.Y, y1: bounds.Y + bounds.Height, page: -1})
	} else {
		g.kids = append(g.kids, &ditem{frag: &typeset.Path{Ops: roundedRect(shape, sradii), Fill: &typeset.Fill{Color: col}}, y0: shape.Y, y1: shape.Y + shape.Height, page: -1})
	}
	p.emitGroup(b, g)
}

// innerShadow paints one inner shadow in the padding box of the border box
// r with border widths bd.
func (p *painter) innerShadow(b *box, r typeset.Rect, radii [4][2]float64, bd [4]float64, x, y, blur, spread float64, col typeset.Color) {
	pr, pradii := insetRect(r, radii, bd)
	if pr.Width <= 0 || pr.Height <= 0 {
		return
	}
	hole, hradii := growRect(pr, pradii, -spread)
	hole.X += x
	hole.Y += y
	clip := &typeset.Path{Ops: roundedRect(pr, pradii), Fill: &typeset.Fill{}}
	g := &ditem{group: true, fx: &groupFx{opacity: 1, clip: clip}}
	y0, y1 := pr.Y, pr.Y+pr.Height
	// The shadow is the padding box, grown as Chromium grows it, less the
	// hole (see innerReach).
	ring, rradii := growRect(pr, pradii, innerReach(blur))
	if frag := p.blurred([]effect.Path{{Ops: roundedRect(ring, rradii)}}, holePaths(hole, hradii), ring, blur, col, r.Y); frag != nil {
		g.kids = append(g.kids, &ditem{frag: frag, y0: y0, y1: y1, page: -1})
	} else {
		ops := rectOps(pr)
		if hole.Width > 0 && hole.Height > 0 {
			ops = append(ops, roundedRect(hole, hradii)...)
		}
		g.kids = append(g.kids, &ditem{frag: &typeset.Path{Ops: ops, Fill: &typeset.Fill{Color: col, Rule: typeset.EvenOdd}}, y0: y0, y1: y1, page: -1})
	}
	p.emitGroup(b, g)
}

// holePaths returns the hole of an inner shadow, the rectangle r with
// corner radii, as paths to cover: none when it is empty.
func holePaths(r typeset.Rect, radii [4][2]float64) []effect.Path {
	if r.Width <= 0 || r.Height <= 0 {
		return nil
	}
	return []effect.Path{{Ops: roundedRect(r, radii)}}
}

// innerReach returns how far past the padding box Chromium covers an
// inner shadow of blur radius blur points before it blurs it: half the
// radius less a pixel. CSS Backgrounds 3, 7.1 casts the shadow as if
// everything outside the padding box were opaque; Chromium leaves it
// transparent past that reach, so its blurred inner shadows fade towards
// the padding edge, and litho draws them as Chromium does.
//
// # Corpus evidence
//
//   - probe, html: Chromium's masks of inner shadows blurred 12, 20 and
//     40px fit a coverage that ends 5, 9 and 18.5px past the padding box,
//     whatever the spread and the offset, blurred by its deviation
//     (root mean square 0.9 to 1.7 levels).
func innerReach(blur float64) float64 {
	return max(blur/2-css.Pt, 0)
}

// emitGroup adds the group g of box b's decoration to the display list.
func (p *painter) emitGroup(b *box, g *ditem) {
	if p.mono >= 0 {
		for _, k := range g.kids {
			k.page = p.mono
		}
	}
	c := p.sync(p.chain(b, false))
	c.kids = append(c.kids, g)
}

// chromiumScale is the pixels a point Chromium draws a blurred shadow's
// mask at: 300 pixels an inch.
const chromiumScale = 300.0 / 72

// boxFactor is 3 sqrt(2 pi) / 4, the width of a box blur a deviation
// spans in Filter Effects 1, 9.14.
const boxFactor = 1.8799712059732503

// shadowSigma returns the standard deviation in points of the blur
// Chromium draws a shadow of blur radius blur points with, 0 when it
// draws none. CSS Backgrounds 3, 7.3 asks for a Gaussian of half the
// radius; Chromium draws three box blurs of the width Filter Effects 1,
// 9.14 derives from that deviation at 300 pixels an inch, whose deviation
// is a little smaller. The shadow takes the boxes' deviation, so the
// falloff spreads as Chromium's at any resolution.
//
// # Corpus evidence
//
//   - probe, html: Chromium's masks of outer box shadows blurred 10, 20
//     and 40px, one row across each, fit three boxes of that width within
//     0.99, 0.78 and 0.80 levels (root mean square, its JPEG's noise
//     included) and a Gaussian of half the radius within 1.71, 1.53 and
//     1.74; 4px fits both within 1.3.
func shadowSigma(blur float64) float64 {
	return chromiumSigma(blur / 2)
}

// skiaBoxWidth returns the width of the three box blurs Filter Effects 1,
// 9.14 approximates a Gaussian of deviation sigma pixels with:
// floor(sigma 3 sqrt(2 pi) / 4 + 0.5).
func skiaBoxWidth(sigma float64) int {
	return int(math.Floor(float64(sigma*boxFactor) + 0.5)) // fits int: sigma is at most maxLength points at chromiumScale
}

// boxVariance returns the variance in squared pixels of three box blurs
// of width d as Filter Effects 1, 9.14 runs them: three of width d for an
// odd d, two of width d and one of d+1 for an even one. A box of n
// pixels spreads by (n^2 - 1) / 12.
func boxVariance(d int) float64 {
	n := float64(d)
	if d%2 == 1 {
		return float64(3*float64(n*n-1)) / 12
	}
	return (float64(2*float64(n*n-1)) + float64((n+1)*(n+1)) - 1) / 12
}

// boxWidth returns the width of three box blurs whose deviation is
// nearest sigma pixels: below 2 for one too small to blur. Three odd
// boxes of width d spread by (d^2 - 1) / 4, so d is near
// sqrt(4 sigma^2 + 1).
func boxWidth(sigma float64) int {
	want := float64(sigma * sigma)
	near := int(math.Round(math.Sqrt(float64(4*want) + 1))) // fits int: sigma is at most maxLength points at MaxScale
	best, gap := 0, math.Inf(1)
	for d := max(near-1, 2); d <= near+1; d++ {
		if g := math.Abs(boxVariance(d) - want); g < gap {
			best, gap = d, g
		}
	}
	return best
}

// blurred returns the shadow of paths less cut, in page points, blurred
// as Chromium blurs a shadow of blur radius blur (see shadowSigma), as an
// image in colour col drawn by package effect; nil for no blur, or when
// the page's effect pixels are spent. area bounds what paths cover, and
// nothing past them is covered. y is the flow position a warning names.
func (p *painter) blurred(paths, cut []effect.Path, area typeset.Rect, blur float64, col typeset.Color, y float64) *typeset.Image {
	sigma := shadowSigma(blur)
	if sigma <= 0 {
		return nil
	}
	// Three deviations reach past the boxes' half width; a point more
	// keeps the image's edge clear of the falloff.
	reach := float64(3*sigma) + 1
	bounds := typeset.Rect{X: area.X - reach, Y: area.Y - reach, Width: area.Width + 2*reach, Height: area.Height + 2*reach}
	res, ok := effect.Plan(sigma, bounds.Width, bounds.Height)
	if !ok {
		return nil
	}
	if !p.effectPixels.Take(res) {
		p.e.warnOnce(warn.LimitReached, warn.Approximated, fmt.Sprintf("blurred shadows past %d pixels a page are drawn sharp", effect.MaxPagePixels), y)
		return nil
	}
	if res.Reduced {
		p.e.warnOnce(warn.LimitReached, warn.Approximated, fmt.Sprintf("a blurred shadow past %d pixels is drawn at a lower resolution", effect.MaxPixels), y)
	}
	k := res.Scale
	ctx := p.e.ctx
	s, err := effect.New(ctx, effect.Coverage, res.W, res.H, effect.Grid{X: bounds.X, Y: bounds.Y, Scale: k})
	if err != nil {
		p.effectFailed(err, y)
		return nil
	}
	defer s.Release()
	tr := typeset.Matrix{A: k, D: k, E: float64(-bounds.X * k), F: float64(-bounds.Y * k)}
	if err := effect.Fill(ctx, s, paths, tr); err != nil {
		p.effectFailed(err, y)
		return nil
	}
	if len(cut) > 0 {
		h, err := effect.New(ctx, effect.Coverage, res.W, res.H, s.Grid)
		if err != nil {
			p.effectFailed(err, y)
			return nil
		}
		err = effect.Fill(ctx, h, cut, tr)
		if err == nil {
			for i, v := range h.Pix {
				s.Pix[i] = uint8((uint32(s.Pix[i])*uint32(255-v) + 127) / 255) //nolint:gosec // at most 255
			}
		}
		h.Release()
		if err != nil {
			p.effectFailed(err, y)
			return nil
		}
	}
	kern := effect.BoxKernel(boxWidth(float64(sigma*k)), 3)
	if err := effect.Blur(ctx, s, kern, kern, effect.EdgeNone); err != nil {
		p.effectFailed(err, y)
		return nil
	}
	if err := typeset.ChargeImage(ctx, s.W, s.H, color.NRGBAModel); err != nil {
		p.effectFailed(err, y)
		return nil
	}
	img := image.NewNRGBA(image.Rect(0, 0, s.W, s.H))
	r8, g8, b8 := channel(col.R), channel(col.G), channel(col.B)
	var alpha [256]uint8
	for v := range alpha {
		alpha[v] = uint8(float64(float64(v)*min(max(col.A, 0), 1)) + 0.5) //nolint:gosec // at most 255
	}
	for i, v := range s.Pix {
		q := img.Pix[4*i : 4*i+4 : 4*i+4]
		q[0], q[1], q[2], q[3] = r8, g8, b8, alpha[v]
	}
	src, err := typeset.EncodeImage(ctx, img)
	if err != nil {
		p.effectFailed(err, y)
		return nil
	}
	return &typeset.Image{Rect: typeset.Rect{X: bounds.X, Y: bounds.Y, Width: float64(s.W) / k, Height: float64(s.H) / k}, Source: src}
}

// channel returns the colour channel v, in [0, 1], as a byte.
func channel(v float64) uint8 {
	return uint8(float64(min(max(v, 0), 1)*255) + 0.5) //nolint:gosec // at most 255
}

// effectFailed settles an error of drawing a blurred shadow at flow
// position y: one that ends the conversion (see limits.Stops) fails the
// layout, any other leaves the shadow sharp with a warning.
func (p *painter) effectFailed(err error, y float64) {
	if limits.Stops(err) {
		p.e.fail(fmt.Errorf("drawing a blurred shadow: %w", err))
		return
	}
	p.e.warnOnce(warn.ContentUnsupported, warn.Approximated, fmt.Sprintf("a blurred shadow is drawn sharp: %v", err), y)
}

// outlineRect returns the border box r grown by d, the outline offset
// plus its width, and the outline's outer radii: each rounded corner of
// the box grows by d, so the outline's inner edge follows the border edge
// at offset 0 (CSS UI 4, 3.1: the outline "should follow the
// border-radius curve"); a sharp corner stays sharp.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) draws the
//     outline of a 50px box of border-radius 10px, 30px and 5px wide, up
//     to the rounded border edge, and its outer corner at radius 40px and
//     15px; the cubic rule of shadows would give about 31px.
func outlineRect(r typeset.Rect, radii [4][2]float64, d float64) (typeset.Rect, [4][2]float64) {
	out := typeset.Rect{X: r.X - d, Y: r.Y - d, Width: max(r.Width+2*d, 0), Height: max(r.Height+2*d, 0)}
	for c := range radii {
		if radii[c][0] <= 0 || radii[c][1] <= 0 {
			radii[c] = [2]float64{}
			continue
		}
		radii[c] = [2]float64{max(radii[c][0]+d, 0), max(radii[c][1]+d, 0)}
		if radii[c][0] == 0 || radii[c][1] == 0 {
			radii[c] = [2]float64{}
		}
	}
	return out, fitRadii(out, radii)
}

// outline paints the outline of b outside its border box, offset by
// outline-offset, following its border radius (CSS UI 3, 5), around each
// fragment a box that clones its decorations paints (CSS Fragmentation 3,
// 5.4), as Chromium draws it.
func (p *painter) outline(b *box) {
	o := b.st.Outline
	w := max(clamp(o.Width), 0)
	if b.hidden || b.kind == kindInline || b.kind == kindText || b.kind == kindBreak || w <= 0 || o.Style == css.BorderNone || o.Style == css.BorderHidden {
		return
	}
	if _, ok := colorOf(o.Color); !ok {
		return
	}
	st := &css.Style{}
	for s := range st.Border {
		st.Border[s] = o
	}
	frags, columns := p.fragments(b)
	for _, r := range frags {
		outer, oradii := outlineRect(r, radiiOf(b, r.Width, r.Height), clamp(b.st.OutlineOffset)+w)
		if outer.Width <= 0 || outer.Height <= 0 {
			continue
		}
		paint := func() { p.border(b, st, outer, oradii, [4]float64{w, w, w, w}, [4]bool{true, true, true, true}) }
		if columns {
			p.paintWhole(b, paint)
		} else {
			paint()
		}
	}
}
