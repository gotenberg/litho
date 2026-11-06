package layout

import (
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// kappa places the control points of a cubic Bézier that approximates a
// quarter ellipse.
const kappa = 0.5522847498

// rectOps returns the path of rectangle r.
func rectOps(r compose.Rect) []compose.PathOp {
	return []compose.PathOp{
		{Kind: compose.MoveTo, P: [3]compose.Point{{X: r.X, Y: r.Y}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: r.X + r.Width, Y: r.Y}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: r.X + r.Width, Y: r.Y + r.Height}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: r.X, Y: r.Y + r.Height}}},
		{Kind: compose.ClosePath},
	}
}

// radiiOf returns the used border radii of b's border box of size w by h,
// horizontal then vertical per corner, scaled down so adjacent radii fit
// their side (CSS Backgrounds 3, 5.5).
func radiiOf(b *box, w, h float64) [4][2]float64 {
	var r [4][2]float64
	for c := range 4 {
		br := b.st.BorderRadius[c]
		r[c] = [2]float64{max(resolve(br.H, w), 0), max(resolve(br.V, h), 0)}
		if r[c][0] <= 0 || r[c][1] <= 0 {
			r[c] = [2]float64{}
		}
	}
	f := 1.0
	ratio := func(side, sum float64) {
		if sum > 0 {
			f = min(f, side/sum)
		}
	}
	ratio(w, r[css.TopLeft][0]+r[css.TopRight][0])
	ratio(w, r[css.BottomLeft][0]+r[css.BottomRight][0])
	ratio(h, r[css.TopLeft][1]+r[css.BottomLeft][1])
	ratio(h, r[css.TopRight][1]+r[css.BottomRight][1])
	if f < 1 {
		for c := range r {
			r[c][0] *= f
			r[c][1] *= f
		}
	}
	return r
}

// insetRect returns r shrunk by d per side and the radii of its corners,
// reduced by the inset (CSS Backgrounds 3, 5.2).
func insetRect(r compose.Rect, radii [4][2]float64, d [4]float64) (compose.Rect, [4][2]float64) {
	out := compose.Rect{
		X: r.X + d[css.Left], Y: r.Y + d[css.Top],
		Width:  max(r.Width-d[css.Left]-d[css.Right], 0),
		Height: max(r.Height-d[css.Top]-d[css.Bottom], 0),
	}
	in := radii
	sub := func(c css.Corner, h, v float64) {
		in[c] = [2]float64{max(radii[c][0]-h, 0), max(radii[c][1]-v, 0)}
		if in[c][0] == 0 || in[c][1] == 0 {
			in[c] = [2]float64{}
		}
	}
	sub(css.TopLeft, d[css.Left], d[css.Top])
	sub(css.TopRight, d[css.Right], d[css.Top])
	sub(css.BottomRight, d[css.Right], d[css.Bottom])
	sub(css.BottomLeft, d[css.Left], d[css.Bottom])
	return out, in
}

// roundedRect returns the path of r with elliptical corners of radii,
// each a cubic Bézier.
func roundedRect(r compose.Rect, radii [4][2]float64) []compose.PathOp {
	if radii == [4][2]float64{} {
		return rectOps(r)
	}
	x0, y0, x1, y1 := r.X, r.Y, r.X+r.Width, r.Y+r.Height
	tl, tr, br, bl := radii[css.TopLeft], radii[css.TopRight], radii[css.BottomRight], radii[css.BottomLeft]
	pt := func(x, y float64) compose.Point { return compose.Point{X: x, Y: y} }
	ops := []compose.PathOp{{Kind: compose.MoveTo, P: [3]compose.Point{pt(x0+tl[0], y0)}}}
	line := func(x, y float64) {
		ops = append(ops, compose.PathOp{Kind: compose.LineTo, P: [3]compose.Point{pt(x, y)}})
	}
	curve := func(c1, c2, to compose.Point) {
		ops = append(ops, compose.PathOp{Kind: compose.CurveTo, P: [3]compose.Point{c1, c2, to}})
	}
	line(x1-tr[0], y0)
	if tr[0] > 0 {
		curve(pt(x1-tr[0]+kappa*tr[0], y0), pt(x1, y0+tr[1]-kappa*tr[1]), pt(x1, y0+tr[1]))
	}
	line(x1, y1-br[1])
	if br[0] > 0 {
		curve(pt(x1, y1-br[1]+kappa*br[1]), pt(x1-br[0]+kappa*br[0], y1), pt(x1-br[0], y1))
	}
	line(x0+bl[0], y1)
	if bl[0] > 0 {
		curve(pt(x0+bl[0]-kappa*bl[0], y1), pt(x0, y1-bl[1]+kappa*bl[1]), pt(x0, y1-bl[1]))
	}
	line(x0, y0+tl[1])
	if tl[0] > 0 {
		curve(pt(x0, y0+tl[1]-kappa*tl[1]), pt(x0+tl[0]-kappa*tl[0], y0), pt(x0+tl[0], y0))
	}
	return append(ops, compose.PathOp{Kind: compose.ClosePath})
}

// ring returns the even-odd path between the rounded rectangles outer and
// inner.
func ring(outer compose.Rect, oradii [4][2]float64, inner compose.Rect, iradii [4][2]float64) []compose.PathOp {
	ops := roundedRect(outer, oradii)
	if inner.Width > 0 && inner.Height > 0 {
		ops = append(ops, roundedRect(inner, iradii)...)
	}
	return ops
}

// background paints the background of box b of style st on the border box
// r with outer corner radii: the colour over the background-clip area, then
// the image (CSS Backgrounds 3, 3).
func (p *painter) background(b *box, st *css.Style, r compose.Rect, radii [4][2]float64, bd, pd [4]float64) {
	clipR, clipRadii := r, radii
	switch st.BackgroundClip {
	case css.BorderEdge:
	case css.PaddingEdge:
		clipR, clipRadii = insetRect(r, radii, bd)
	case css.ContentEdge:
		clipR, clipRadii = insetRect(r, radii, addSides(bd, pd))
	}
	if clipR.Width <= 0 || clipR.Height <= 0 {
		return
	}
	if c, ok := colorOf(st.BackgroundColor); ok {
		p.emit(b, false, &compose.Path{Ops: roundedRect(clipR, clipRadii), Fill: &compose.Fill{Color: c}}, clipR.Y, clipR.Y+clipR.Height, -1)
	}
	if st.BackgroundImage == "" {
		return
	}
	pic := p.e.picture(st.BackgroundImage, r.Y)
	if pic == nil {
		return
	}
	area := r
	switch st.BackgroundOrigin {
	case css.BorderEdge:
	case css.PaddingEdge:
		area, _ = insetRect(r, radii, bd)
	case css.ContentEdge:
		area, _ = insetRect(r, radii, addSides(bd, pd))
	}
	tiles := p.e.tiles(st, pic, area, clipR, r.Y)
	if len(tiles) == 0 {
		return
	}
	g := &ditem{group: true, opacity: 1, clip: &compose.Path{Ops: roundedRect(clipR, clipRadii), Fill: &compose.Fill{}}}
	for _, t := range tiles {
		g.kids = append(g.kids, &ditem{frag: &compose.Image{Rect: t, Source: pic.Source}, y0: t.Y, y1: t.Y + t.Height, page: -1})
	}
	c := p.sync(p.chain(b, false))
	if p.mono >= 0 {
		for _, k := range g.kids {
			k.page = p.mono
		}
	}
	c.kids = append(c.kids, g)
}

// addSides returns a + b per side.
func addSides(a, b [4]float64) [4]float64 {
	for i := range a {
		a[i] += b[i]
	}
	return a
}

// tiles returns the placements of the background image pic over the
// positioning area, within the painting area clip: its size (CSS
// Backgrounds 3, 3.9), position (3.6) and repetition (3.4).
func (e *engine) tiles(st *css.Style, pic *Picture, area, clip compose.Rect, y float64) []compose.Rect {
	iw, ih := pic.Width, pic.Height
	w, h := iw, ih
	bs := st.BackgroundSize
	switch bs.Kind {
	case css.SizeCover, css.SizeContain:
		k := max(area.Width/iw, area.Height/ih)
		if bs.Kind == css.SizeContain {
			k = min(area.Width/iw, area.Height/ih)
		}
		w, h = iw*k, ih*k
	case css.SizeExplicit:
		ws, wOK := sizeOf(bs.Width, area.Width)
		hs, hOK := sizeOf(bs.Height, area.Height)
		switch {
		case wOK && hOK:
			w, h = ws, hs
		case wOK:
			w, h = ws, ws*ih/iw
		case hOK:
			w, h = hs*iw/ih, hs
		}
	}
	rep := st.BackgroundRepeat
	if rep[0] == css.RepeatRound && w > 0 {
		w = area.Width / max(math.Round(area.Width/w), 1)
	}
	if rep[1] == css.RepeatRound && h > 0 {
		h = area.Height / max(math.Round(area.Height/h), 1)
	}
	if !(w > 0) || !(h > 0) || math.IsInf(w, 0) || math.IsInf(h, 0) {
		return nil
	}
	x0 := area.X + resolve(st.BackgroundPosition[0], area.Width-w)
	y0 := area.Y + resolve(st.BackgroundPosition[1], area.Height-h)
	xs := positions(rep[0], x0, w, area.X, area.Width, clip.X, clip.Width)
	ys := positions(rep[1], y0, h, area.Y, area.Height, clip.Y, clip.Height)
	if len(xs)*len(ys) > maxBackgroundTiles || len(xs) > maxBackgroundTiles || len(ys) > maxBackgroundTiles {
		e.warn(codeBackgroundTiles, fmt.Sprintf("a background repeated more than %d times was drawn once", maxBackgroundTiles), y)
		return []compose.Rect{{X: x0, Y: y0, Width: w, Height: h}}
	}
	var out []compose.Rect
	for _, ty := range ys {
		for _, tx := range xs {
			out = append(out, compose.Rect{X: tx, Y: ty, Width: w, Height: h})
		}
	}
	return out
}

// sizeOf resolves one background-size dimension against ref, false for
// auto.
func sizeOf(d css.Dimension, ref float64) (float64, bool) {
	if d.Kind != css.Points && d.Kind != css.Percent {
		return 0, false
	}
	v := resolve(d, ref)
	return v, v > 0
}

// positions returns the tile offsets along one axis: one at start for
// no-repeat, every step from start covering the clip span for repeat and
// round, and spread evenly over the area for space (CSS Backgrounds 3,
// 3.4).
func positions(rep css.Repeat, start, size, areaStart, areaSize, clipStart, clipSize float64) []float64 {
	limit := float64(maxBackgroundTiles + 1)
	switch rep {
	case css.RepeatNoRepeat:
		return []float64{start}
	case css.RepeatSpace:
		n := math.Floor(areaSize / size)
		if n < 2 || n > limit {
			if n > limit {
				return make([]float64, maxBackgroundTiles+1)
			}
			return []float64{start}
		}
		gap := (areaSize - n*size) / (n - 1)
		out := make([]float64, 0, int(n)) // fits int: at most limit, checked above
		for i := 0.0; i < n; i++ {
			out = append(out, areaStart+i*(size+gap))
		}
		return out
	case css.RepeatRepeat, css.RepeatRound:
	}
	first := start - math.Ceil((start-clipStart)/size)*size
	n := math.Ceil((clipStart + clipSize - first) / size)
	if n > limit {
		return make([]float64, maxBackgroundTiles+1)
	}
	out := make([]float64, 0, max(int(n), 0)) // fits int: at most limit, checked above
	for i := 0.0; i < n; i++ {
		out = append(out, first+i*size)
	}
	return out
}

// shades returns the darker and lighter shade of c a two-tone border style
// draws with (CSS Backgrounds 3, 4.2 leaves them to the user agent): half
// toward black, and for a colour already near black, the colour itself
// and a lighter grey.
func shades(c compose.Color) (compose.Color, compose.Color) {
	dark := compose.Color{R: c.R / 2, G: c.G / 2, B: c.B / 2, A: c.A}
	light := c
	if max(c.R, c.G, c.B) < 0.2 {
		dark = c
		light = compose.Color{R: c.R + (1-c.R)*0.4, G: c.G + (1-c.G)*0.4, B: c.B + (1-c.B)*0.4, A: c.A}
	}
	return dark, light
}

// border paints the borders of box b of style st on the border box r with
// outer corner radii and used widths bd, the sides in draw (CSS 2.1, 8.5;
// CSS Backgrounds 3, 4).
func (p *painter) border(b *box, st *css.Style, r compose.Rect, radii [4][2]float64, bd [4]float64, draw [4]bool) {
	var visible [4]bool
	shown := false
	for s := range 4 {
		bs := st.Border[s].Style
		_, ok := colorOf(st.Border[s].Color)
		visible[s] = draw[s] && bd[s] > 0 && ok && bs != css.BorderNone && bs != css.BorderHidden
		shown = shown || visible[s]
	}
	if !shown {
		return
	}
	inner, iradii := insetRect(r, radii, bd)
	uniform := true
	for s := 1; s < 4; s++ {
		if !visible[s] || !visible[0] || st.Border[s] != st.Border[0] || bd[s] != bd[0] {
			uniform = false
		}
	}
	if uniform && (st.Border[0].Style == css.Solid || st.Border[0].Style == css.Double) {
		// One colour all round needs no cut at the corners.
		g := &ditem{}
		p.side(g, css.Top, st.Border[0], r, radii, inner, iradii, bd)
		for _, k := range g.kids {
			p.emit(b, false, k.frag, r.Y, r.Y+r.Height, -1)
		}
		return
	}
	for s := range 4 {
		if !visible[s] {
			continue
		}
		g := &ditem{group: true, opacity: 1, clip: &compose.Path{Ops: sideClip(css.Side(s), r, inner), Fill: &compose.Fill{}}}
		if bs := st.Border[s].Style; bs == css.Dashed || bs == css.Dotted {
			// A stroke along the side stays inside it: no clip.
			g = &ditem{group: true, opacity: 1}
		}
		p.side(g, css.Side(s), st.Border[s], r, radii, inner, iradii, bd)
		if len(g.kids) == 0 {
			continue
		}
		for _, k := range g.kids {
			k.y0, k.y1, k.page = r.Y, r.Y+r.Height, -1
			if p.mono >= 0 {
				k.page = p.mono
			}
		}
		c := p.sync(p.chain(b, false))
		c.kids = append(c.kids, g)
	}
}

// sideClip returns the trapezoid of side s between the outer box r and the
// inner box in, its ends cut on the diagonals from the outer to the inner
// corners.
func sideClip(s css.Side, r, in compose.Rect) []compose.PathOp {
	pt := func(x, y float64) compose.Point { return compose.Point{X: x, Y: y} }
	x0, y0, x1, y1 := r.X, r.Y, r.X+r.Width, r.Y+r.Height
	ix0, iy0, ix1, iy1 := in.X, in.Y, in.X+in.Width, in.Y+in.Height
	var q [4]compose.Point
	switch s {
	case css.Top:
		q = [4]compose.Point{pt(x0, y0), pt(x1, y0), pt(ix1, iy0), pt(ix0, iy0)}
	case css.Right:
		q = [4]compose.Point{pt(x1, y0), pt(x1, y1), pt(ix1, iy1), pt(ix1, iy0)}
	case css.Bottom:
		q = [4]compose.Point{pt(x1, y1), pt(x0, y1), pt(ix0, iy1), pt(ix1, iy1)}
	case css.Left:
		q = [4]compose.Point{pt(x0, y1), pt(x0, y0), pt(ix0, iy0), pt(ix0, iy1)}
	}
	ops := []compose.PathOp{{Kind: compose.MoveTo, P: [3]compose.Point{q[0]}}}
	for _, v := range q[1:] {
		ops = append(ops, compose.PathOp{Kind: compose.LineTo, P: [3]compose.Point{v}})
	}
	return append(ops, compose.PathOp{Kind: compose.ClosePath})
}

// side draws one border side of style bdr into g, which clips it to the
// side's trapezoid.
func (p *painter) side(g *ditem, s css.Side, bdr css.Border, r compose.Rect, radii [4][2]float64, inner compose.Rect, iradii [4][2]float64, bd [4]float64) {
	c, _ := colorOf(bdr.Color)
	fill := func(ops []compose.PathOp, col compose.Color) {
		g.kids = append(g.kids, &ditem{frag: &compose.Path{Ops: ops, Fill: &compose.Fill{Color: col, Rule: compose.EvenOdd}}})
	}
	frac := func(f float64) (compose.Rect, [4][2]float64) {
		var d [4]float64
		for i := range d {
			d[i] = bd[i] * f
		}
		return insetRect(r, radii, d)
	}
	dark, light := shades(c)
	topLeft := s == css.Top || s == css.Left
	switch bdr.Style {
	case css.Double:
		o1, o1r := frac(1.0 / 3)
		i1, i1r := frac(2.0 / 3)
		fill(ring(r, radii, o1, o1r), c)
		fill(ring(i1, i1r, inner, iradii), c)
	case css.Groove, css.Ridge:
		mid, midr := frac(0.5)
		outer, in := dark, light
		if !topLeft {
			outer, in = light, dark
		}
		if bdr.Style == css.Ridge {
			outer, in = in, outer
		}
		fill(ring(r, radii, mid, midr), outer)
		fill(ring(mid, midr, inner, iradii), in)
	case css.Inset, css.Outset:
		col := light
		if (bdr.Style == css.Inset) == topLeft {
			col = dark
		}
		fill(ring(r, radii, inner, iradii), col)
	case css.Dashed, css.Dotted:
		w := bd[s]
		var a, z compose.Point
		switch s {
		case css.Top:
			a, z = compose.Point{X: r.X, Y: r.Y + w/2}, compose.Point{X: r.X + r.Width, Y: r.Y + w/2}
		case css.Right:
			a, z = compose.Point{X: r.X + r.Width - w/2, Y: r.Y}, compose.Point{X: r.X + r.Width - w/2, Y: r.Y + r.Height}
		case css.Bottom:
			a, z = compose.Point{X: r.X + r.Width, Y: r.Y + r.Height - w/2}, compose.Point{X: r.X, Y: r.Y + r.Height - w/2}
		case css.Left:
			a, z = compose.Point{X: r.X + w/2, Y: r.Y + r.Height}, compose.Point{X: r.X + w/2, Y: r.Y}
		}
		// CSS Backgrounds 3, 4.2 leaves the dash pattern to the user
		// agent: dashes three widths long, dots as squares of the width.
		stroke := &compose.Stroke{Color: c, Width: w, Dash: []float64{3 * w, 3 * w}}
		if bdr.Style == css.Dotted {
			stroke.Dash = []float64{w, w}
		}
		g.kids = append(g.kids, &ditem{frag: &compose.Path{Ops: []compose.PathOp{
			{Kind: compose.MoveTo, P: [3]compose.Point{a}},
			{Kind: compose.LineTo, P: [3]compose.Point{z}},
		}, Stroke: stroke}})
	case css.Solid, css.BorderNone, css.BorderHidden:
		fill(ring(r, radii, inner, iradii), c)
	}
}
