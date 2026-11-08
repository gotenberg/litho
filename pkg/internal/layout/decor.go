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
	case css.BorderEdge, css.TextEdge:
	case css.PaddingEdge:
		clipR, clipRadii = insetRect(r, radii, bd)
	case css.ContentEdge:
		clipR, clipRadii = insetRect(r, radii, addSides(bd, pd))
	}
	if clipR.Width <= 0 || clipR.Height <= 0 {
		return
	}
	if clipToText(st) {
		// CSS Backgrounds 4, 2.4: the background paints under the glyphs
		// alone, within the border box.
		tc := p.textClip(b)
		if tc == nil || !hasBackground(st) {
			return
		}
		g := &ditem{group: true, opacity: 1, clip: tc}
		c := p.sync(p.chain(b, false))
		c.kids = append(c.kids, g)
		p.backgroundInto(g, b, st, r, radii, bd, pd, clipR, clipRadii)
		return
	}
	p.backgroundInto(nil, b, st, r, radii, bd, pd, clipR, clipRadii)
}

// backgroundInto paints the colour and the image of the background of b
// clipped to clipR with clipRadii, into the group into, or into the
// display list when into is nil.
func (p *painter) backgroundInto(into *ditem, b *box, st *css.Style, r compose.Rect, radii [4][2]float64, bd, pd [4]float64, clipR compose.Rect, clipRadii [4][2]float64) {
	if c, ok := colorOf(st.BackgroundColor); ok {
		path := &compose.Path{Ops: roundedRect(clipR, clipRadii), Fill: &compose.Fill{Color: c}}
		if into == nil {
			p.emit(b, false, path, clipR.Y, clipR.Y+clipR.Height, -1)
		} else {
			page := -1
			if p.mono >= 0 {
				page = p.mono
			}
			into.kids = append(into.kids, &ditem{frag: path, y0: clipR.Y, y1: clipR.Y + clipR.Height, page: page})
		}
	}
	if st.BackgroundImage == "" && st.BackgroundGradient == nil {
		return
	}
	area := r
	switch st.BackgroundOrigin {
	case css.BorderEdge, css.TextEdge:
	case css.PaddingEdge:
		area, _ = insetRect(r, radii, bd)
	case css.ContentEdge:
		area, _ = insetRect(r, radii, addSides(bd, pd))
	}
	if st.BackgroundAttachment == css.AttachLocal {
		area = p.localArea(b, area, r, pd)
	}
	g := &ditem{group: true, opacity: 1, clip: &compose.Path{Ops: roundedRect(clipR, clipRadii), Fill: &compose.Fill{}}}
	if st.BackgroundGradient != nil {
		for _, pa := range p.e.gradientTiles(st, area, clipR, r.Y) {
			t := pa.Ops[0].P[0]
			g.kids = append(g.kids, &ditem{frag: pa, y0: t.Y, y1: pa.Ops[2].P[0].Y, page: -1})
		}
	} else {
		pic := p.e.picture(st.BackgroundImage, r.Y)
		if pic == nil {
			return
		}
		pic = naturalPicture(pic, area.Width, area.Height)
		tiles := p.e.tiles(st, pic, area, clipR, r.Y)
		for i, t := range tiles {
			f := p.e.pictureFrag(pic, t, nil)
			if f == nil {
				break
			}
			if i == 0 && pic.Vector != nil {
				tiles = p.e.vectorTiles(tiles, fragNodes(f), r.Y)
			}
			g.kids = append(g.kids, &ditem{frag: f, y0: t.Y, y1: t.Y + t.Height, page: -1})
		}
	}
	if len(g.kids) == 0 {
		return
	}
	if p.mono >= 0 {
		for _, k := range g.kids {
			k.page = p.mono
		}
	}
	if into != nil {
		into.kids = append(into.kids, g)
		return
	}
	c := p.sync(p.chain(b, false))
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
	out, over := tileRects(st, pic, area, clip)
	if over {
		e.warn(codeBackgroundTiles, fmt.Sprintf("a background repeated more than %d times was drawn once", maxBackgroundTiles), y)
	}
	return out
}

// tileRects returns what [engine.tiles] does, and true with the first
// tile alone when more than maxBackgroundTiles would cover the clip.
func tileRects(st *css.Style, pic *Picture, area, clip compose.Rect) ([]compose.Rect, bool) {
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
	w0, h0 := w, h
	if rep[0] == css.RepeatRound && w > 0 {
		w = area.Width / max(math.Round(area.Width/w), 1)
	}
	if rep[1] == css.RepeatRound && h > 0 {
		h = area.Height / max(math.Round(area.Height/h), 1)
	}
	// CSS Backgrounds 3, 3.4: round on one axis only, with an auto size
	// on the other, scales the other to keep the aspect ratio.
	autoW := bs.Kind == css.SizeExplicit && bs.Width.Kind == css.Auto
	autoH := bs.Kind == css.SizeExplicit && bs.Height.Kind == css.Auto
	switch {
	case rep[0] == css.RepeatRound && rep[1] != css.RepeatRound && autoH && w0 > 0:
		h = h0 * w / w0
	case rep[1] == css.RepeatRound && rep[0] != css.RepeatRound && autoW && h0 > 0:
		w = w0 * h / h0
	}
	if !(w > 0) || !(h > 0) || math.IsInf(w, 0) || math.IsInf(h, 0) {
		return nil, false
	}
	x0 := area.X + resolve(st.BackgroundPosition[0], area.Width-w)
	y0 := area.Y + resolve(st.BackgroundPosition[1], area.Height-h)
	xs := positions(rep[0], x0, w, area.X, area.Width, clip.X, clip.Width)
	ys := positions(rep[1], y0, h, area.Y, area.Height, clip.Y, clip.Height)
	if len(xs)*len(ys) > maxBackgroundTiles || len(xs) > maxBackgroundTiles || len(ys) > maxBackgroundTiles {
		return []compose.Rect{{X: x0, Y: y0, Width: w, Height: h}}, true
	}
	var out []compose.Rect
	for _, ty := range ys {
		for _, tx := range xs {
			out = append(out, compose.Rect{X: tx, Y: ty, Width: w, Height: h})
		}
	}
	return out, false
}

// sizeOf resolves one background-size dimension against ref, false for
// auto.
func sizeOf(d css.Dimension, ref float64) (float64, bool) {
	if !d.IsLength() {
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
// draws with. CSS Backgrounds 3, 4.2 leaves them to the user agent; these
// are Blink's (Color::Dark and Color::Light), which the Chromium
// references draw: the brightest channel moved down or up by 0.33 of the
// range, the others scaled with it, and black lightened to #545454. A
// colour of a relative luminance at most #202020's is too dark to darken:
// its shades are its lighter one and that one's lighter, as Chromium
// 9.0.0's print measures (#000 draws #545454 and #a8a8a8, #210000 (117, 0,
// 0) and (201, 0, 0), #003000 black and (0, 132, 0)).
func shades(c compose.Color) (compose.Color, compose.Color) {
	if veryDark(c) {
		l := lighter(c)
		return l, lighter(l)
	}
	v := max(c.R, c.G, c.B)
	if v <= 0 {
		return compose.Color{A: c.A}, lighter(c)
	}
	return blinkScale(c, max(v-blinkShift, 0)/v), lighter(c)
}

// blinkShift is the step of Blink's Color::Dark and Color::Light, 0.33 of
// the range rather than a third: Chromium draws #210000's lighter shade
// as 117, which a third would round to 118.
const blinkShift = 0.33

// lighter is Blink's Color::Light: the brightest channel up by 0.33 of the
// range, the others scaled with it, black to #545454.
func lighter(c compose.Color) compose.Color {
	v := max(c.R, c.G, c.B)
	if v <= 0 {
		return compose.Color{R: 0x54 / 255.0, G: 0x54 / 255.0, B: 0x54 / 255.0, A: c.A}
	}
	return blinkScale(c, min(v+blinkShift, 1)/v)
}

// blinkScale scales the channels of c by k and truncates them to 8 bits
// as Blink does (the channel times just under 256, rounded down).
func blinkScale(c compose.Color, k float64) compose.Color {
	ch := func(v float64) float64 { return min(math.Floor(min(max(v*k, 0), 1)*255.99998), 255) / 255 }
	return compose.Color{R: ch(c.R), G: ch(c.G), B: ch(c.B), A: c.A}
}

// veryDark reports a colour whose relative luminance (WCAG 2, sRGB) is at
// most #202020's, the bound Chromium's measured shades switch at: #202020
// and #300000 are very dark, #212121 and #003000 are not.
func veryDark(c compose.Color) bool {
	lin := func(v float64) float64 {
		v = min(max(v, 0), 1)
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	l := 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
	return l <= lin(0x20/255.0)+1e-9
}

// currentShades are the dark and light shades of a two-tone border whose
// colour is currentColor: Blink paints #eeeeee and its darker #9a9a9a
// whatever the color property, a legacy quirk Chromium 9.0.0's print
// measures on every color tried (black, #800000).
func currentShades(a float64) (compose.Color, compose.Color) {
	g := func(v float64) compose.Color { return compose.Color{R: v / 255, G: v / 255, B: v / 255, A: a} }
	return g(0x9a), g(0xee)
}

// border paints the borders of box b of style st on the border box r with
// outer corner radii and used widths bd, the sides in draw (CSS 2.1, 8.5;
// CSS Backgrounds 3, 4), or its border image in their place (CSS
// Backgrounds 3, 6).
func (p *painter) border(b *box, st *css.Style, r compose.Rect, radii [4][2]float64, bd [4]float64, draw [4]bool) {
	if p.borderImage(b, st, r, bd, draw) {
		return
	}
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
	if st.StrokeBorders && radii == ([4][2]float64{}) {
		shown = false
		for s := range 4 {
			if visible[s] && st.Border[s].Style == css.Solid {
				c, _ := colorOf(st.Border[s].Color)
				p.emit(b, false, strokedSide(css.Side(s), r, bd[s], c), r.Y, r.Y+r.Height, -1)
				visible[s] = false
			}
			shown = shown || visible[s]
		}
		if !shown {
			return
		}
	}
	inner, iradii := insetRect(r, radii, bd)
	ref := 0
	for !visible[ref] {
		ref++
	}
	// A side cut away, not drawn and of no width, as the slice of a box a
	// page break cuts has, does not break the uniformity of the others.
	uniform := true
	for s := range 4 {
		if !visible[s] && !draw[s] && bd[s] == 0 {
			continue
		}
		if !visible[s] || st.Border[s] != st.Border[ref] || bd[s] != bd[ref] {
			uniform = false
		}
	}
	if uniform && (st.Border[ref].Style == css.Solid || st.Border[ref].Style == css.Double) {
		// One colour all round needs no cut at the corners.
		g := &ditem{}
		p.side(g, css.Side(ref), st.Border[ref], st.BorderColorIsCurrent(css.Side(ref)), r, radii, inner, iradii, bd)
		for _, k := range g.kids {
			p.emit(b, false, k.frag, r.Y, r.Y+r.Height, -1)
		}
		return
	}
	if bs := st.Border[0].Style; uniform && ref == 0 && visible[2] && (bs == css.Dashed || bs == css.Dotted) && radii != ([4][2]float64{}) {
		// A rounded dashed or dotted border runs its pattern along the
		// whole rounded outline, as Blink strokes it.
		c, _ := colorOf(st.Border[0].Color)
		mid, midRadii := insetRect(r, radii, [4]float64{bd[0] / 2, bd[0] / 2, bd[0] / 2, bd[0] / 2})
		ops := roundedRect(mid, midRadii)
		stroke := dashStroke(bs, c, bd[0], roundedLength(mid, midRadii), true)
		p.emit(b, false, &compose.Path{Ops: ops, Stroke: stroke}, r.Y, r.Y+r.Height, -1)
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
		p.side(g, css.Side(s), st.Border[s], st.BorderColorIsCurrent(css.Side(s)), r, radii, inner, iradii, bd)
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

// dashStroke returns the stroke of a dashed or dotted line of width w
// along a path of length l (CSS Backgrounds 3, 4.2 leaves the pattern to
// the user agent; this is Blink's). Dashes are twice the width, three times
// under 3px, dots round and as wide as the line, and the gaps, nominally
// the width, stretch or shrink so the pattern starts and ends on a dash or
// a dot: on the corners of a side. closed reports a closed path, whose
// pattern repeats a whole number of times around it.
func dashStroke(bs css.BorderStyle, c compose.Color, w, l float64, closed bool) *compose.Stroke {
	st := &compose.Stroke{Color: c, Width: w}
	dash := 2 * w
	if w < 3*css.Pt {
		dash = 3 * w
	}
	round := bs == css.Dotted && !squareDots(w)
	if bs == css.Dotted {
		dash = w
		if round {
			st.Cap = compose.RoundCap
		}
	}
	gap := w
	if closed {
		n := max(math.Round(l/(dash+gap)), 1)
		gap = max(l/n-dash, 0)
	} else {
		span := l
		if round {
			// Round caps reach half a width past each end.
			span = l + w
		}
		gap = bestGap(span, dash, gap)
	}
	if round {
		st.Dash = []float64{0, dash + gap}
		return st
	}
	st.Dash = []float64{dash, gap}
	return st
}

// squareDots reports a dotted line too thin for round dots, w points
// wide: under 3 pixels Blink draws its dots as squares, as WeasyPrint does.
func squareDots(w float64) bool { return w < 3*css.Pt }

// bestGap returns the gap closest to gap that fits a whole number of dashes
// of length dash, gaps between them, in length l, dashes on both ends
// (Blink's StrokeData::SelectBestDashGap).
func bestGap(l, dash, gap float64) float64 {
	if !(l > 0) || !(dash+gap > 0) {
		return gap
	}
	lo := math.Floor((l + gap) / (dash + gap))
	if lo > float64(maxBackgroundTiles) {
		return gap
	}
	hi := lo + 1
	loGap, hiGap := math.Inf(1), (l-hi*dash)/(hi-1)
	if lo > 1 {
		loGap = (l - lo*dash) / (lo - 1)
	}
	if hiGap <= 0 || math.Abs(loGap-gap) < math.Abs(hiGap-gap) {
		if math.IsInf(loGap, 0) {
			return gap
		}
		return max(loGap, 0)
	}
	return hiGap
}

// roundedLength returns the length of the outline of r with elliptical
// corners of radii, each quarter ellipse measured with Ramanujan's
// approximation.
func roundedLength(r compose.Rect, radii [4][2]float64) float64 {
	l := 2 * (r.Width + r.Height)
	for _, c := range radii {
		a, b := c[0], c[1]
		if a <= 0 || b <= 0 {
			continue
		}
		h := (a - b) * (a - b) / ((a + b) * (a + b))
		quarter := math.Pi * (a + b) * (1 + 3*h/(10+math.Sqrt(4-3*h))) / 4
		l += quarter - a - b
	}
	return l
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
// side's trapezoid; current reports a border colour of currentColor,
// which the two-tone styles paint as [currentShades] say.
func (p *painter) side(g *ditem, s css.Side, bdr css.Border, current bool, r compose.Rect, radii [4][2]float64, inner compose.Rect, iradii [4][2]float64, bd [4]float64) {
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
	if current {
		dark, light = currentShades(c.A)
	}
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
		l := math.Hypot(z.X-a.X, z.Y-a.Y)
		if bdr.Style == css.Dotted && !squareDots(w) {
			// The dots sit on the line, the first and the last centred
			// half a width in from the corners.
			if l <= w {
				return
			}
			ux, uy := (z.X-a.X)/l, (z.Y-a.Y)/l
			a = compose.Point{X: a.X + ux*w/2, Y: a.Y + uy*w/2}
			z = compose.Point{X: z.X - ux*w/2, Y: z.Y - uy*w/2}
			l -= w
		}
		g.kids = append(g.kids, &ditem{frag: &compose.Path{Ops: []compose.PathOp{
			{Kind: compose.MoveTo, P: [3]compose.Point{a}},
			{Kind: compose.LineTo, P: [3]compose.Point{z}},
		}, Stroke: dashStroke(bdr.Style, c, w, l, false)}})
	case css.Solid, css.BorderNone, css.BorderHidden:
		fill(ring(r, radii, inner, iradii), c)
	}
}

// strokedSide returns the side s of the border box r, w points wide, as
// Writer prints a solid border (css.Office.StrokeBorders): a line of colour
// c stroked along the middle of the side, from one outer corner to the
// other, at least css.MinBorderStroke wide, so that a raster draws a
// border thinner than a pixel a pixel wide where a filled band would
// average to grey.
func strokedSide(s css.Side, r compose.Rect, w float64, c compose.Color) *compose.Path {
	var a, z compose.Point
	switch s {
	case css.Top:
		a, z = compose.Point{X: r.X, Y: r.Y + w/2}, compose.Point{X: r.X + r.Width, Y: r.Y + w/2}
	case css.Bottom:
		a, z = compose.Point{X: r.X, Y: r.Y + r.Height - w/2}, compose.Point{X: r.X + r.Width, Y: r.Y + r.Height - w/2}
	case css.Left:
		a, z = compose.Point{X: r.X + w/2, Y: r.Y}, compose.Point{X: r.X + w/2, Y: r.Y + r.Height}
	case css.Right:
		a, z = compose.Point{X: r.X + r.Width - w/2, Y: r.Y}, compose.Point{X: r.X + r.Width - w/2, Y: r.Y + r.Height}
	}
	return strokedLine(a, z, w, c)
}

// strokedLine returns the line from a to z stroked in c, w points wide and
// at least css.MinBorderStroke, with butt caps.
//
// # Corpus evidence
//
//   - litho: a flat ODT whose paragraphs have solid borders of 0.02 pt to
//     1 pt prints, through the pinned LibreOffice, strokes of 0.1 pt for
//     every border of one or two twips, 0.15, 0.2, 0.25, 0.3, 0.5, 0.75
//     and 1 pt at the widths above, and no border for 0.01 pt.
func strokedLine(a, z compose.Point, w float64, c compose.Color) *compose.Path {
	return &compose.Path{Ops: []compose.PathOp{
		{Kind: compose.MoveTo, P: [3]compose.Point{a}},
		{Kind: compose.LineTo, P: [3]compose.Point{z}},
	}, Stroke: &compose.Stroke{Color: c, Width: max(w, css.MinBorderStroke)}}
}
