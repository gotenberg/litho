package layout

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"slices"

	"golang.org/x/image/vector"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// shadowScale is the resolution of a blurred shadow's raster: 2 pixels per
// point, 192 dpi, twice the 96 dpi a reader draws a page at by default.
const shadowScale = 2.0

// maxShadowPixels bounds the raster of one blurred shadow: 4 Mpx, a shadow
// of 1000 by 1000 points at full resolution. A larger one is drawn at a
// lower resolution, which a blur hides. Anti-DoS guard, not spec-mandated.
const maxShadowPixels = 4 << 20

// maxShadowBudget bounds the raster pixels of every blurred shadow of one
// layout: 64 Mpx. Past it a blurred shadow is drawn sharp, with a warning.
// Anti-DoS guard, not spec-mandated: each raster is kept in the output.
const maxShadowBudget = 64 << 20

// codeShadowBlur is the warning code of a blur left out.
const codeShadowBlur = "layout-shadow-blur"

// growRect returns r grown by d on every side, shrunk when d is negative,
// and its corner radii adjusted the way CSS Backgrounds 3, 7.2 adjusts a
// shadow's: a growing radius r becomes r + d, scaled by 1 + (r/d - 1)^3
// when r is under d so a sharp corner stays sharp; a shrinking one r + d,
// floored at 0.
func growRect(r compose.Rect, radii [4][2]float64, d float64) (compose.Rect, [4][2]float64) {
	out := compose.Rect{X: r.X - d, Y: r.Y - d, Width: max(r.Width+2*d, 0), Height: max(r.Height+2*d, 0)}
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
func fitRadii(r compose.Rect, radii [4][2]float64) [4][2]float64 {
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
func (p *painter) shadows(b *box, st *css.Style, r compose.Rect, radii [4][2]float64, bd [4]float64, inset bool) {
	for _, sh := range slices.Backward(st.BoxShadow) {
		if sh.Inset != inset {
			continue
		}
		col, ok := colorOf(sh.Color)
		x, y, blur, spread := clamp(sh.X), clamp(sh.Y), max(clamp(sh.Blur), 0), clamp(sh.Spread)
		if !ok || r.Width <= 0 && r.Height <= 0 {
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
func (p *painter) outerShadow(b *box, r compose.Rect, radii [4][2]float64, x, y, blur, spread float64, col compose.Color) {
	shape, sradii := growRect(r, radii, spread)
	shape.X += x
	shape.Y += y
	if shape.Width <= 0 || shape.Height <= 0 {
		return
	}
	reach := 1.5 * blur
	bounds := union(r, compose.Rect{X: shape.X - reach, Y: shape.Y - reach, Width: shape.Width + 2*reach, Height: shape.Height + 2*reach})
	bounds = compose.Rect{X: bounds.X - 1, Y: bounds.Y - 1, Width: bounds.Width + 2, Height: bounds.Height + 2}
	// The shadow never shows under the box (CSS Backgrounds 3, 7.1).
	clip := &compose.Path{Ops: append(rectOps(bounds), roundedRect(r, radii)...), Fill: &compose.Fill{Rule: compose.EvenOdd}}
	g := &ditem{group: true, opacity: 1, clip: clip}
	if frag := p.blurred(roundedRect(shape, sradii), shape, blur, false, col, r.Y); frag != nil {
		g.kids = append(g.kids, &ditem{frag: frag, y0: bounds.Y, y1: bounds.Y + bounds.Height, page: -1})
	} else {
		g.kids = append(g.kids, &ditem{frag: &compose.Path{Ops: roundedRect(shape, sradii), Fill: &compose.Fill{Color: col}}, y0: shape.Y, y1: shape.Y + shape.Height, page: -1})
	}
	p.emitGroup(b, g)
}

// innerShadow paints one inner shadow in the padding box of the border box
// r with border widths bd.
func (p *painter) innerShadow(b *box, r compose.Rect, radii [4][2]float64, bd [4]float64, x, y, blur, spread float64, col compose.Color) {
	pr, pradii := insetRect(r, radii, bd)
	if pr.Width <= 0 || pr.Height <= 0 {
		return
	}
	hole, hradii := growRect(pr, pradii, -spread)
	hole.X += x
	hole.Y += y
	clip := &compose.Path{Ops: roundedRect(pr, pradii), Fill: &compose.Fill{}}
	g := &ditem{group: true, opacity: 1, clip: clip}
	y0, y1 := pr.Y, pr.Y+pr.Height
	if frag := p.blurred(roundedRect(hole, hradii), pr, blur, true, col, r.Y); frag != nil {
		g.kids = append(g.kids, &ditem{frag: frag, y0: y0, y1: y1, page: -1})
	} else {
		ops := rectOps(pr)
		if hole.Width > 0 && hole.Height > 0 {
			ops = append(ops, roundedRect(hole, hradii)...)
		}
		g.kids = append(g.kids, &ditem{frag: &compose.Path{Ops: ops, Fill: &compose.Fill{Color: col, Rule: compose.EvenOdd}}, y0: y0, y1: y1, page: -1})
	}
	p.emitGroup(b, g)
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

// blurred returns the shadow of shape blurred by a Gaussian of standard
// deviation blur/2 (CSS Backgrounds 3, 7.3) as a raster image in colour
// col, or nil for no blur, or when the layout's raster budget is spent.
// For an outer shadow the shadow is shape, whose bounding box is area; for
// an inner one (inner true) it is everything outside shape, drawn over
// area, the padding box.
func (p *painter) blurred(shape []compose.PathOp, area compose.Rect, blur float64, inner bool, col compose.Color, y float64) compose.Fragment {
	if blur <= 0 {
		return nil
	}
	reach := 1.5 * blur
	bounds := compose.Rect{X: area.X - reach, Y: area.Y - reach, Width: area.Width + 2*reach, Height: area.Height + 2*reach}
	k := shadowScale
	if px := bounds.Width * bounds.Height * k * k; px > maxShadowPixels {
		k *= math.Sqrt(maxShadowPixels / px)
	}
	w, h := int(math.Ceil(bounds.Width*k)), int(math.Ceil(bounds.Height*k)) // fit int: at most maxShadowPixels
	if w <= 0 || h <= 0 || w*h > 2*maxShadowPixels {
		return nil
	}
	if p.shadowPixels+w*h > maxShadowBudget {
		p.e.warnOnce(codeShadowBlur, fmt.Sprintf("blurred shadows past %d raster pixels drawn sharp", maxShadowBudget), y)
		return nil
	}
	p.shadowPixels += w * h
	mask := rasterize(shape, bounds, k, w, h)
	if inner {
		for i, v := range mask.Pix {
			mask.Pix[i] = 255 - v
		}
	}
	blurAlpha(mask, blur/2*k, inner)
	if err := compose.ChargeImage(p.e.ctx, w, h, color.NRGBAModel); err != nil {
		// The refusal stops the conversion, which reports it.
		return nil
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	r8, g8, b8 := uint8(col.R*255+0.5), uint8(col.G*255+0.5), uint8(col.B*255+0.5)
	for i, v := range mask.Pix {
		img.Pix[4*i] = r8
		img.Pix[4*i+1] = g8
		img.Pix[4*i+2] = b8
		img.Pix[4*i+3] = uint8(float64(v)*col.A + 0.5)
	}
	src, err := compose.EncodeImage(p.e.ctx, img)
	if err != nil {
		return nil
	}
	rect := compose.Rect{X: bounds.X, Y: bounds.Y, Width: float64(w) / k, Height: float64(h) / k}
	return &compose.Image{Rect: rect, Source: src}
}

// rasterize draws the path ops, in flow coordinates, into a w by h alpha
// mask of bounds at k pixels per point.
func rasterize(ops []compose.PathOp, bounds compose.Rect, k float64, w, h int) *image.Alpha {
	z := vector.NewRasterizer(w, h)
	pt := func(q compose.Point) (float32, float32) {
		return float32((q.X - bounds.X) * k), float32((q.Y - bounds.Y) * k)
	}
	open := false
	for _, op := range ops {
		switch op.Kind {
		case compose.MoveTo:
			if open {
				z.ClosePath()
			}
			z.MoveTo(pt(op.P[0]))
			open = true
		case compose.LineTo:
			z.LineTo(pt(op.P[0]))
		case compose.CurveTo:
			ax, ay := pt(op.P[0])
			bx, by := pt(op.P[1])
			cx, cy := pt(op.P[2])
			z.CubeTo(ax, ay, bx, by, cx, cy)
		case compose.ClosePath:
			z.ClosePath()
			open = false
		}
	}
	if open {
		z.ClosePath()
	}
	dst := image.NewAlpha(image.Rect(0, 0, w, h))
	z.Draw(dst, dst.Bounds(), image.NewUniform(color.Alpha{A: 255}), image.Point{})
	return dst
}

// blurAlpha blurs m in place by three box blurs approximating a Gaussian
// of standard deviation sigma pixels. Pixels past the edge read as the
// edge's when extend is set, else as transparent.
func blurAlpha(m *image.Alpha, sigma float64, extend bool) {
	if !numeric.Positive(sigma) {
		return
	}
	w, h := m.Rect.Dx(), m.Rect.Dy()
	buf := make([]float64, w*h)
	for i, v := range m.Pix {
		buf[i] = float64(v)
	}
	// Three box blurs of widths from sigma (W. Jarosz, "Fast Image
	// Convolutions", 2001): the ideal width sqrt(12 sigma^2 / n + 1).
	ideal := math.Sqrt(12*sigma*sigma/3 + 1)
	lo := int(math.Floor(ideal)) // fits int: sigma is bounded by the raster size
	if lo%2 == 0 {
		lo--
	}
	hi := lo + 2
	mIdeal := (12*sigma*sigma - 3*float64(lo*lo) - 12*float64(lo) - 9) / (-4*float64(lo) - 4)
	mm := int(math.Round(mIdeal))
	tmp := make([]float64, max(w, h))
	for pass := range 3 {
		size := hi
		if pass < mm {
			size = lo
		}
		r := (size - 1) / 2
		if r <= 0 {
			continue
		}
		for y := range h {
			boxLine(buf[y*w:(y+1)*w], 1, w, r, extend, tmp)
		}
		for x := range w {
			boxLine(buf[x:], w, h, r, extend, tmp)
		}
	}
	for i, v := range buf {
		m.Pix[i] = uint8(min(max(v, 0), 255) + 0.5)
	}
}

// boxLine averages the n values of line, stride apart, over a window of
// radius r, in place, tmp holding at least n values.
func boxLine(line []float64, stride, n, r int, extend bool, tmp []float64) {
	get := func(i int) float64 {
		switch {
		case i < 0:
			if !extend {
				return 0
			}
			i = 0
		case i >= n:
			if !extend {
				return 0
			}
			i = n - 1
		}
		return line[i*stride]
	}
	sum := 0.0
	for i := -r - 1; i < r; i++ {
		sum += get(i)
	}
	win := float64(2*r + 1)
	for i := range n {
		sum += get(i+r) - get(i-r-1)
		tmp[i] = sum / win
	}
	for i := range n {
		line[i*stride] = tmp[i]
	}
}

// outline paints the outline of b outside its border box, offset by
// outline-offset, following its border radius (CSS UI 3, 5).
func (p *painter) outline(b *box) {
	o := b.st.Outline
	w := max(clamp(o.Width), 0)
	if b.hidden || b.kind == kindInline || b.kind == kindText || b.kind == kindBreak || w <= 0 || o.Style == css.BorderNone || o.Style == css.BorderHidden {
		return
	}
	if _, ok := colorOf(o.Color); !ok {
		return
	}
	r := p.borderRect(b)
	radii := radiiOf(b, r.Width, r.Height)
	outer, oradii := growRect(r, radii, clamp(b.st.OutlineOffset)+w)
	if outer.Width <= 0 || outer.Height <= 0 {
		return
	}
	st := &css.Style{}
	for s := range st.Border {
		st.Border[s] = o
	}
	p.border(b, st, outer, oradii, [4]float64{w, w, w, w}, [4]bool{true, true, true, true})
}
