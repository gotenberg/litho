package layout

import (
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// kappa places the control points of a cubic Bézier that approximates a
// quarter ellipse.
const kappa = 0.5522847498

// rectOps returns the path of rectangle r.
func rectOps(r typeset.Rect) []typeset.PathOp {
	return []typeset.PathOp{
		{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: r.X, Y: r.Y}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: r.X + r.Width, Y: r.Y}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: r.X + r.Width, Y: r.Y + r.Height}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: r.X, Y: r.Y + r.Height}}},
		{Kind: typeset.ClosePath},
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
func insetRect(r typeset.Rect, radii [4][2]float64, d [4]float64) (typeset.Rect, [4][2]float64) {
	out := typeset.Rect{
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
func roundedRect(r typeset.Rect, radii [4][2]float64) []typeset.PathOp {
	if radii == [4][2]float64{} {
		return rectOps(r)
	}
	x0, y0, x1, y1 := r.X, r.Y, r.X+r.Width, r.Y+r.Height
	tl, tr, br, bl := radii[css.TopLeft], radii[css.TopRight], radii[css.BottomRight], radii[css.BottomLeft]
	pt := func(x, y float64) typeset.Point { return typeset.Point{X: x, Y: y} }
	ops := []typeset.PathOp{{Kind: typeset.MoveTo, P: [3]typeset.Point{pt(x0+tl[0], y0)}}}
	line := func(x, y float64) {
		ops = append(ops, typeset.PathOp{Kind: typeset.LineTo, P: [3]typeset.Point{pt(x, y)}})
	}
	curve := func(c1, c2, to typeset.Point) {
		ops = append(ops, typeset.PathOp{Kind: typeset.CurveTo, P: [3]typeset.Point{c1, c2, to}})
	}
	line(x1-tr[0], y0)
	if tr[0] > 0 {
		curve(pt(x1-tr[0]+float64(kappa*tr[0]), y0), pt(x1, y0+tr[1]-float64(kappa*tr[1])), pt(x1, y0+tr[1]))
	}
	line(x1, y1-br[1])
	if br[0] > 0 {
		curve(pt(x1, y1-br[1]+float64(kappa*br[1])), pt(x1-br[0]+float64(kappa*br[0]), y1), pt(x1-br[0], y1))
	}
	line(x0+bl[0], y1)
	if bl[0] > 0 {
		curve(pt(x0+bl[0]-float64(kappa*bl[0]), y1), pt(x0, y1-bl[1]+float64(kappa*bl[1])), pt(x0, y1-bl[1]))
	}
	line(x0, y0+tl[1])
	if tl[0] > 0 {
		curve(pt(x0, y0+tl[1]-float64(kappa*tl[1])), pt(x0+tl[0]-float64(kappa*tl[0]), y0), pt(x0+tl[0], y0))
	}
	return append(ops, typeset.PathOp{Kind: typeset.ClosePath})
}

// ring returns the even-odd path between the rounded rectangles outer and
// inner.
func ring(outer typeset.Rect, oradii [4][2]float64, inner typeset.Rect, iradii [4][2]float64) []typeset.PathOp {
	ops := roundedRect(outer, oradii)
	if inner.Width > 0 && inner.Height > 0 {
		ops = append(ops, roundedRect(inner, iradii)...)
	}
	return ops
}

// background paints the background of box b of style st on the border box
// r with outer corner radii: the colour over the background-clip area of
// the bottom layer, then the layers from the bottom one up, each over its
// own background-clip area (CSS Backgrounds 3, 2.2 and 3). A colour or a
// layer clipped to text paints under the glyphs alone, within the border
// box (CSS Backgrounds 4, 2.4): consecutive ones share one clip.
func (p *painter) background(b *box, st *css.Style, r typeset.Rect, radii [4][2]float64, bd, pd [4]float64) {
	var one [1]css.BackgroundLayer
	layers := one[:]
	if n := st.BackgroundLayerCount(); n > 1 {
		layers = make([]css.BackgroundLayer, n)
	}
	visible := false
	for i := range layers {
		layers[i] = st.BackgroundLayer(i)
		if c, _ := clipArea(layers[i].Clip, r, radii, bd, pd); c.Width > 0 && c.Height > 0 {
			visible = true
		}
	}
	colEdge := st.BackgroundColorClip()
	colR, colRadii := clipArea(colEdge, r, radii, bd, pd)
	if !visible && (colR.Width <= 0 || colR.Height <= 0) {
		return
	}
	_, hasColor := colorOf(st.BackgroundColor)
	allText, anyText := !hasColor || colEdge == css.TextEdge, hasColor && colEdge == css.TextEdge
	for i := range layers {
		if layers[i].HasImage() {
			text := layers[i].Clip == css.TextEdge
			allText, anyText = allText && text, anyText || text
		}
	}
	bg := bgPaint{p: p, b: b}
	if allText && anyText {
		tc := p.textClip(b)
		if tc == nil {
			return
		}
		bg.into = &ditem{group: true, fx: &groupFx{opacity: 1, clip: tc}}
		bg.addTo(nil, bg.into)
		bg.blended(st)
		bg.color(st, colR, colRadii)
		for i := len(layers) - 1; i >= 0; i-- {
			bg.layer(i, &layers[i], r, radii, bd, pd)
		}
		return
	}
	bg.blended(st)
	parent := bg.into
	var run *ditem
	use := func(text bool) {
		bg.into = parent
		if !text {
			run = nil
			return
		}
		if run == nil {
			bg.text(parent)
			run = bg.into
		}
		bg.into = run
	}
	use(hasColor && colEdge == css.TextEdge)
	bg.color(st, colR, colRadii)
	for i := len(layers) - 1; i >= 0; i-- {
		if l := &layers[i]; l.HasImage() {
			use(l.Clip == css.TextEdge)
			bg.layer(i, l, r, radii, bd, pd)
		}
	}
}

// bgPaint paints the colour and the layers of one background of the box
// b into the group into, or into the display list when into is nil. It
// does not outlive the call that paints them.
type bgPaint struct {
	p    *painter
	b    *box
	into *ditem
	// blend is how each layer composites, indexed as the layers; nil
	// when nothing blends.
	blend []typeset.BlendMode
	// tc is the text clip of b, computed once; textDone is whether it was.
	tc       *typeset.Path
	textDone bool
}

// addTo adds d to the group into, or to the display list when into is nil.
func (bg *bgPaint) addTo(into, d *ditem) {
	if into != nil {
		into.kids = append(into.kids, d)
		return
	}
	c := bg.p.sync(bg.p.chain(bg.b, false))
	c.kids = append(c.kids, d)
}

// text points into at a new group clipped to the glyphs of b, added to
// parent, when b draws glyphs; else at a group that paints nothing.
func (bg *bgPaint) text(parent *ditem) {
	if !bg.textDone {
		bg.tc, bg.textDone = bg.p.textClip(bg.b), true
	}
	if bg.tc == nil {
		bg.into = &ditem{group: true}
		return
	}
	bg.into = &ditem{group: true, fx: &groupFx{opacity: 1, clip: bg.tc}}
	bg.addTo(parent, bg.into)
}

// blended opens, when a layer of the background of style st blends with
// what lies beneath it in the background, an isolated group, since the
// layers of a background blend with each other and its colour and not
// with what lies beneath the element (CSS Compositing 1, 9.1), and sets
// each layer's blend mode. A layer over nothing blends with nothing, and
// nothing blends when no layer blends with something.
//
// # Corpus evidence
//
//   - probe, html: Chromium writes a background of a gradient and a
//     colour, background-blend-mode multiply, screen or luminosity, as an
//     isolated transparency group holding the colour and the gradient
//     under /BM of the mode.
func (bg *bgPaint) blended(st *css.Style) {
	modes := layerBlends(st)
	if modes == nil {
		return
	}
	g := &ditem{group: true, fx: &groupFx{opacity: 1, isolate: true}}
	bg.addTo(bg.into, g)
	bg.into, bg.blend = g, modes
}

// layerBlends returns the blend mode of each layer of the background of
// st, nil when no layer blends with a layer or a colour beneath it: a
// layer over nothing blends with nothing.
func layerBlends(st *css.Style) []typeset.BlendMode {
	_, under := colorOf(st.BackgroundColor)
	var modes []typeset.BlendMode
	for i := st.BackgroundLayerCount() - 1; i >= 0; i-- {
		l := st.BackgroundLayer(i)
		if !l.HasImage() {
			continue
		}
		if under && l.Blend != css.BlendNormal {
			if modes == nil {
				modes = make([]typeset.BlendMode, st.BackgroundLayerCount())
			}
			// css.BlendMode orders the sixteen modes as typeset does; a
			// background takes no plus-lighter.
			modes[i] = typeset.BlendMode(l.Blend)
		}
		under = true
	}
	return modes
}

// color paints the background colour of st over colR with colRadii.
func (bg *bgPaint) color(st *css.Style, colR typeset.Rect, colRadii [4][2]float64) {
	c, ok := colorOf(st.BackgroundColor)
	if !ok || colR.Width <= 0 || colR.Height <= 0 {
		return
	}
	path := &typeset.Path{Ops: roundedRect(colR, colRadii), Fill: &typeset.Fill{Color: c}}
	if bg.into == nil {
		bg.p.emit(bg.b, false, path, colR.Y, colR.Y+colR.Height, -1)
		return
	}
	page := -1
	if bg.p.mono >= 0 {
		page = bg.p.mono
	}
	bg.into.kids = append(bg.into.kids, &ditem{frag: path, y0: colR.Y, y1: colR.Y + colR.Height, page: page})
}

// layer paints the image of the background layer l, layer i, over its clip area on
// the border box r with outer corner radii, its borders bd and paddings
// pd: placed in its positioning area (CSS Backgrounds 3, 3.7), attached
// (3.5), sized, positioned and repeated (3.4 to 3.9).
func (bg *bgPaint) layer(i int, l *css.BackgroundLayer, r typeset.Rect, radii [4][2]float64, bd, pd [4]float64) {
	p, b := bg.p, bg.b
	blend := typeset.BlendNormal
	if bg.blend != nil {
		blend = bg.blend[i]
	}
	clipR, clipRadii := clipArea(l.Clip, r, radii, bd, pd)
	if !l.HasImage() || clipR.Width <= 0 || clipR.Height <= 0 {
		return
	}
	area, abd, apd := r, bd, pd
	if s := p.strip; s != nil && s.box == b && !clones(b.st) {
		// CSS Backgrounds 3, 3.7 and 5.4 with box-decoration-break:
		// slice: the image is positioned in the inline box's fragments
		// laid end to end, and this fragment shows its part of it.
		area = typeset.Rect{X: r.X - s.before, Y: r.Y, Width: s.total, Height: r.Height}
		abd, apd = b.bd, b.pd
	}
	switch l.Origin {
	case css.BorderEdge, css.TextEdge:
	case css.PaddingEdge:
		area, _ = insetRect(area, radii, abd)
	case css.ContentEdge:
		area, _ = insetRect(area, radii, addSides(abd, apd))
	}
	if l.Attachment == css.AttachLocal {
		area = p.localArea(b, area, r, pd)
	}
	g := &ditem{group: true, fx: &groupFx{opacity: 1, blend: blend, clip: &typeset.Path{Ops: roundedRect(clipR, clipRadii), Fill: &typeset.Fill{}}}}
	for _, sl := range p.attachedSlices(b, l.Attachment, area, clipR) {
		if l.Gradient != nil {
			for _, pa := range p.e.gradientTiles(l, sl.area, sl.clip, r.Y) {
				t := pa.Ops[0].P[0]
				k := &ditem{frag: pa, y0: t.Y, y1: pa.Ops[2].P[0].Y, page: -1}
				if p.e.chargeDrawn(itemCost(k)); p.e.err != nil {
					return
				}
				g.kids = append(g.kids, k)
			}
			continue
		}
		pic := zoomedPicture(p.e.picture(l.Image, r.Y), bg.b.st.UsedZoom())
		if pic == nil {
			return
		}
		if wm := p.e.frameOf(b); wm != css.HorizontalTB {
			if f := p.e.uprightLayer(l, pic, sl, wm, r.Y); f != nil {
				g.kids = append(g.kids, &ditem{frag: f, y0: sl.clip.Y, y1: sl.clip.Y + sl.clip.Height, page: -1})
			}
			continue
		}
		pic = naturalPicture(pic, sl.area.Width, sl.area.Height)
		if pa := p.e.patternTiles(l, pic, sl.area, sl.clip); pa != nil {
			if !p.addTiles(g, pa) {
				return
			}
			continue
		}
		tiles := p.e.tiles(l, pic, sl.area, sl.clip, r.Y)
		for i, t := range tiles {
			f := p.e.pictureFrag(pic, t, nil)
			if f == nil {
				break
			}
			if i == 0 && pic.Vector != nil {
				tiles = p.e.vectorTiles(tiles, fragNodes(f), r.Y)
			}
			k := &ditem{frag: f, y0: t.Y, y1: t.Y + t.Height, page: -1}
			if p.e.chargeDrawn(itemCost(k)); p.e.err != nil {
				return
			}
			g.kids = append(g.kids, k)
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
	bg.addTo(bg.into, g)
}

// addTiles adds to the layer group g the path pa that paints its tiles
// through a tiling pattern, charging it, and reports false when the budget
// refuses it.
func (p *painter) addTiles(g *ditem, pa *typeset.Path) bool {
	top, bottom := pa.Ops[0].P[0].Y, pa.Ops[2].P[0].Y
	k := &ditem{frag: pa, y0: top, y1: bottom, page: -1}
	if p.e.chargeDrawn(itemCost(k)); p.e.err != nil {
		return false
	}
	g.kids = append(g.kids, k)
	return true
}

// clipArea returns the background painting area that edge names on the
// border box r with outer corner radii, its borders bd and paddings pd
// (CSS Backgrounds 3, 3.8); text paints within the border box.
func clipArea(edge css.BoxEdge, r typeset.Rect, radii [4][2]float64, bd, pd [4]float64) (typeset.Rect, [4][2]float64) {
	switch edge {
	case css.BorderEdge, css.TextEdge:
	case css.PaddingEdge:
		return insetRect(r, radii, bd)
	case css.ContentEdge:
		return insetRect(r, radii, addSides(bd, pd))
	}
	return r, radii
}

// bgSlice is a background positioning area and the part of the
// background painting area its image is drawn in.
type bgSlice struct {
	area, clip typeset.Rect
}

// attachedSlices returns where a background image of b, attached as
// attach, is positioned and drawn: area and clip for a scrolling or local image;
// for a fixed one, on each page clip reaches, that page's area, the
// viewport of paged media, and the part of clip on it, the image repeated
// on every page (CSS Backgrounds 3, 3.5). A box that is transformed or
// inside a transformed box scrolls its fixed image (CSS Transforms 1, 3),
// as does a box laid out where the engine does not paginate. A monolithic
// unit draws its fixed image on its own page.
//
// Chromium's print departs from the clause: it draws a fixed image on a
// block as scroll and none on a table part, while its screen fixes the
// image to the viewport. The clause is kept.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 prints a fixed 96px image on a table row
//     group (CSS2/backgrounds/background-attachment-applies-to-001) as no
//     image at all, the row group orange alone.
func (p *painter) attachedSlices(b *box, attach css.Attachment, area, clip typeset.Rect) []bgSlice {
	if attach != css.AttachFixed || !p.e.paginating() || transformedFrom(b) {
		return []bgSlice{{area: area, clip: clip}}
	}
	var out []bgSlice
	first, last := p.e.pageOf(clip.Y), p.e.pageOf(clip.Y+clip.Height-eps)
	if p.mono >= 0 {
		// A monolithic unit draws on its page alone.
		first, last = p.mono, p.mono
	}
	for k := first; k <= last && k-first < maxFixedPages; k++ {
		w, _ := areaOf(p.e.box(k))
		page := typeset.Rect{Y: p.e.pageTop(k), Width: w, Height: p.e.sliceH(k)}
		if part, ok := intersect(clip, page); ok {
			out = append(out, bgSlice{area: page, clip: part})
		}
	}
	return out
}

// maxFixedPages caps the pages a fixed background image is repeated on
// for one box. Anti-DoS guard, not spec-mandated: each page draws its
// own tiles, and a box spanning more pages than this leaves the rest of
// its background without the image.
const maxFixedPages = 1024

// transformedFrom reports whether b or one of its ancestors is
// transformed.
func transformedFrom(b *box) bool {
	for a, depth := b, 0; a != nil && depth <= maxTreeDepth; a, depth = a.parent, depth+1 {
		if a.transformed() {
			return true
		}
	}
	return false
}

// addSides returns a + b per side.
func addSides(a, b [4]float64) [4]float64 {
	for i := range a {
		a[i] += b[i]
	}
	return a
}

// tiles returns the placements of the image pic of the background layer
// l over the positioning area, within the painting area clip: its size (CSS
// Backgrounds 3, 3.9), position (3.6) and repetition (3.4).
func (e *engine) tiles(l *css.BackgroundLayer, pic *Picture, area, clip typeset.Rect, y float64) []typeset.Rect {
	out, over := tileRects(l, pic, area, clip)
	if over {
		e.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("a background repeated more than %d times was drawn once", maxBackgroundTiles), y)
	}
	return out
}

// tileRects returns what [engine.tiles] does, and true with the first
// tile alone when more than maxBackgroundTiles would cover the clip.
func tileRects(l *css.BackgroundLayer, pic *Picture, area, clip typeset.Rect) ([]typeset.Rect, bool) {
	w, h, x0, y0, ok := tileSize(l, pic, area)
	if !ok {
		return nil, false
	}
	rep := l.Repeat
	xs := positions(rep[0], x0, w, area.X, area.Width, clip.X, clip.Width)
	ys := positions(rep[1], y0, h, area.Y, area.Height, clip.Y, clip.Height)
	if len(xs)*len(ys) > maxBackgroundTiles || len(xs) > maxBackgroundTiles || len(ys) > maxBackgroundTiles {
		return []typeset.Rect{{X: x0, Y: y0, Width: w, Height: h}}, true
	}
	var out []typeset.Rect
	for _, ty := range ys {
		for _, tx := range xs {
			out = append(out, typeset.Rect{X: tx, Y: ty, Width: w, Height: h})
		}
	}
	return out, false
}

// tileSize returns the size w by h of a tile of the image pic of the
// background layer l over the positioning area (CSS Backgrounds 3, 3.9,
// with round's rescaling, 3.4) and the position x0, y0 of the tile its
// background-position places (3.6); false for a size that is not
// positive.
func tileSize(l *css.BackgroundLayer, pic *Picture, area typeset.Rect) (w, h, x0, y0 float64, ok bool) {
	iw, ih := pic.Width, pic.Height
	w, h = iw, ih
	bs := l.Size
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
	rep := l.Repeat
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
	if !numeric.PositiveFinite(w) || !numeric.PositiveFinite(h) {
		return 0, 0, 0, 0, false
	}
	x0 = area.X + resolve(l.Position[0], area.Width-w)
	y0 = area.Y + resolve(l.Position[1], area.Height-h)
	return w, h, x0, y0, true
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
		gap := (areaSize - float64(n*size)) / (n - 1)
		out := make([]float64, 0, int(n)) // fits int: at most limit, checked above
		for i := 0.0; i < n; i++ {
			out = append(out, areaStart+float64(i*(size+gap)))
		}
		return out
	case css.RepeatRepeat, css.RepeatRound:
	}
	first := start - float64(math.Ceil((start-clipStart)/size)*size)
	n := math.Ceil((clipStart + clipSize - first) / size)
	if n > limit {
		return make([]float64, maxBackgroundTiles+1)
	}
	out := make([]float64, 0, max(int(n), 0)) // fits int: at most limit, checked above
	for i := 0.0; i < n; i++ {
		out = append(out, first+float64(i*size))
	}
	return out
}

// shades returns the darker and lighter shade of c a two-tone border style
// draws with. CSS Backgrounds 3, 4.2 leaves them to the user agent; these
// are Blink's (Color::Dark and Color::Light), which the Chromium
// references draw: the brightest channel moved down or up by 0.33 of the
// range, the others scaled with it, and black lightened to #545454. A
// colour of a relative luminance at most #202020's is too dark to darken:
// its shades are its lighter one and that one's lighter. blink selects
// that rule; off, as the Office formats draw them, a dark colour darkens
// to black like any other.
//
// # Corpus evidence
//
//   - probe, html: Chromium 9.0.0's print draws #000 as #545454 and
//     #a8a8a8, #210000 as (117, 0, 0) and (201, 0, 0), and #003000 as
//     black and (0, 132, 0).
func shades(c typeset.Color, blink bool) (typeset.Color, typeset.Color) {
	if blink && veryDark(c) {
		l := lighter(c)
		return l, lighter(l)
	}
	v := max(c.R, c.G, c.B)
	if v <= 0 {
		return typeset.Color{A: c.A}, lighter(c)
	}
	return blinkScale(c, max(v-blinkShift, 0)/v), lighter(c)
}

// blinkShift is the step of Blink's Color::Dark and Color::Light, 0.33 of
// the range rather than a third.
//
// # Corpus evidence
//
//   - probe, html: Chromium draws #210000's lighter shade as 117, which a
//     third would round to 118.
const blinkShift = 0.33

// lighter is Blink's Color::Light: the brightest channel up by 0.33 of the
// range, the others scaled with it, black to #545454.
func lighter(c typeset.Color) typeset.Color {
	v := max(c.R, c.G, c.B)
	if v <= 0 {
		return typeset.Color{R: 0x54 / 255.0, G: 0x54 / 255.0, B: 0x54 / 255.0, A: c.A}
	}
	return blinkScale(c, min(v+blinkShift, 1)/v)
}

// blinkScale scales the channels of c by k and truncates them to 8 bits
// as Blink does (the channel times just under 256, rounded down).
func blinkScale(c typeset.Color, k float64) typeset.Color {
	ch := func(v float64) float64 { return min(math.Floor(min(max(v*k, 0), 1)*255.99998), 255) / 255 }
	return typeset.Color{R: ch(c.R), G: ch(c.G), B: ch(c.B), A: c.A}
}

// veryDark reports a colour whose relative luminance (WCAG 2, sRGB) is at
// most #202020's, the bound Chromium's shades switch at.
//
// # Corpus evidence
//
//   - probe, html: Chromium's shades take #202020 and #300000 as very
//     dark, #212121 and #003000 as not.
func veryDark(c typeset.Color) bool {
	lin := func(v float64) float64 {
		v = min(max(v, 0), 1)
		if v <= 0.04045 {
			return v / 12.92
		}
		return detmath.Pow((v+0.055)/1.055, 2.4)
	}
	l := float64(0.2126*lin(c.R)) + float64(0.7152*lin(c.G)) + float64(0.0722*lin(c.B))
	return l <= lin(0x20/255.0)+1e-9
}

// blink reports [Options.BrowserRules]: the two-tone border shades of
// [shades] and [currentShades] and the auto underline are Blink's, which
// the Office formats do not draw.
func (p *painter) blink() bool { return p.e.opts.BrowserRules }

// quirkShades reports a border side of box b, of style st, whose two-tone
// styles take [currentShades]: a colour of currentColor, on a box other
// than a table box or a cell of a table element, which Blink paints in the
// shades of their computed colour as it paints any colour.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws an outset
//     or inset currentColor border at 168 and 84 on a table, an inline
//     table and a cell of a table element (black text), at 255 and 171 for
//     red text; at 238 and 154 on a block, a flex container and a lone
//     display: table-cell box, whatever the colour.
func quirkShades(b *box, st *css.Style, s css.Side) bool {
	if !st.BorderColorIsCurrent(s) {
		return false
	}
	switch b.tpart { //nolint:exhaustive // every other box keeps the quirk
	case partTable:
		return false
	case partCell:
		for t := b.parent; t != nil; t = t.parent {
			if t.tpart == partTable {
				return t.anon
			}
		}
	}
	return true
}

// currentShades are the dark and light shades of a two-tone border whose
// colour is currentColor: Blink paints #eeeeee and its darker #9a9a9a
// whatever the color property, a legacy quirk.
//
// # Corpus evidence
//
//   - probe, html: Chromium 9.0.0's print draws these shades for every
//     color tried, black and #800000.
func currentShades(a float64) (typeset.Color, typeset.Color) {
	g := func(v float64) typeset.Color { return typeset.Color{R: v / 255, G: v / 255, B: v / 255, A: a} }
	return g(0x9a), g(0xee)
}

// border paints the borders of box b of style st on the border box r with
// outer corner radii and used widths bd, the sides in draw (CSS 2.1, 8.5;
// CSS Backgrounds 3, 4), or its border image in their place (CSS
// Backgrounds 3, 6).
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 fills a solid border of 10px sides and
//     15px top and bottom, a double one of 9px and 15px, and a rounded
//     solid one of 4px top and 10px elsewhere as one even-odd path of the
//     outer and inner edges.
func (p *painter) border(b *box, st *css.Style, r typeset.Rect, radii [4][2]float64, bd [4]float64, draw [4]bool) {
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
	// The ring of the solid and double styles also passes over a side of
	// no width, which paints nothing at the corners it shares, and takes
	// sides of one style and colour whatever their widths: its inner edge
	// is inset by each side's own.
	uniform, ringed := true, true
	for s := range 4 {
		if !visible[s] && bd[s] == 0 {
			if draw[s] {
				uniform = false
			}
			continue
		}
		if !visible[s] || st.Border[s].Style != st.Border[ref].Style || st.Border[s].Color != st.Border[ref].Color {
			uniform, ringed = false, false
		}
		if st.Border[s].Width != st.Border[ref].Width || bd[s] != bd[ref] {
			uniform = false
		}
	}
	if ringed && (st.Border[ref].Style == css.Solid || st.Border[ref].Style == css.Double) {
		// One colour all round needs no cut at the corners: the sides
		// meet without the seam two clipped trapezoids leave on their
		// shared diagonal, as Chromium fills them.

		g := &ditem{}
		p.side(g, css.Side(ref), st.Border[ref], quirkShades(b, st, css.Side(ref)), r, radii, inner, iradii, bd)
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
		l := roundedLength(mid, midRadii)
		if p.blink() {
			// Blink measures the outline in whole pixels, dropping the
			// fraction.
			l = math.Floor(l/css.Pt) * css.Pt
		}
		stroke := dashStroke(bs, c, bd[0], l, true, p.blink())
		p.emit(b, false, &typeset.Path{Ops: ops, Stroke: stroke}, r.Y, r.Y+r.Height, -1)
		return
	}
	for s := range 4 {
		if !visible[s] {
			continue
		}
		g := &ditem{group: true, fx: &groupFx{opacity: 1, clip: &typeset.Path{Ops: sideClip(css.Side(s), r, inner), Fill: &typeset.Fill{}}}}
		if bs := st.Border[s].Style; bs == css.Dashed || bs == css.Dotted {
			// A stroke along the side stays inside it: no clip.
			g = &ditem{group: true}
		}
		p.side(g, css.Side(s), st.Border[s], quirkShades(b, st, css.Side(s)), r, radii, inner, iradii, bd)
		if len(g.kids) == 0 {
			continue
		}
		for _, k := range g.kids {
			k.y0, k.y1, k.page = r.Y, r.Y+r.Height, -1
			if p.mono >= 0 {
				k.page = p.mono
			}
		}
		c := p.holder(b, false)
		c.kids = append(c.kids, g)
	}
}

// dashStroke returns the stroke of a dashed or dotted line of width w
// along a path of length l (CSS Backgrounds 3, 4.2 leaves the pattern to
// the user agent; this is Blink's). Dashes are twice the width, three times
// under 3px, dots round and as wide as the line, and the gaps, nominally
// the width, stretch or shrink so the pattern starts and ends on a dash or
// a dot: on the corners of a side. closed reports a closed path, whose
// pattern repeats a whole number of times around it. blink selects
// [blinkDashStroke].
func dashStroke(bs css.BorderStyle, c typeset.Color, w, l float64, closed, blink bool) *typeset.Stroke {
	if blink {
		return blinkDashStroke(bs, c, w, l, closed)
	}
	st := &typeset.Stroke{Color: c, Width: w}
	dash := 2 * w
	if w < 3*css.Pt {
		dash = 3 * w
	}
	round := bs == css.Dotted && !squareDots(w)
	if bs == css.Dotted {
		dash = w
		if round {
			st.Cap = typeset.RoundCap
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

// blinkDashStroke returns the stroke of a dashed or dotted line of width
// w along a path of length l as Blink strokes it (StyledStrokeData's dash
// effect; CSS Backgrounds 3, 4.2 leaves the pattern to the user agent).
// Dashes are three widths long with gaps of two under 3px, two widths with
// gaps of one from 3px; dots of 3px and less are squares a width long a
// width apart, thicker ones round. A line no longer than two dashes is
// solid, one no longer than two dashes and a gap holds two dashes scaled to
// fit, and otherwise the gaps of dashes and round dots stretch or shrink
// ([blinkGap]) so the pattern starts and ends on a dash or a dot. closed
// reports a closed path.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws dashed and
//     dotted sides 150 and 157px long at widths 1 to 10px with these
//     dashes, dots and gaps, and a dashed side 39px long and 9px wide as
//     two dashes; its PDF route fills the same dashes as rectangles (6px
//     dashes and 4.29px gaps on a 2px side 150px long).
func blinkDashStroke(bs css.BorderStyle, c typeset.Color, w, l float64, closed bool) *typeset.Stroke {
	st := &typeset.Stroke{Color: c, Width: w}
	if bs == css.Dotted && !blinkSquareDots(w) {
		st.Cap = typeset.RoundCap
		if l < 2*w {
			// One dot: the gap passes the end of the line.
			st.Dash = []float64{0, 2 * w}
			return st
		}
		// Blink shortens the pitch by a hundredth of a pixel so that
		// rounding along the line keeps the last dot.
		st.Dash = []float64{0, blinkGap(l, w, w, closed) + w - 0.01*css.Pt}
		return st
	}
	dash, gap := w, w
	if bs == css.Dashed {
		dash, gap = 2*w, w
		if w < 3*css.Pt {
			dash, gap = 3*w, 2*w
		}
	}
	if l <= 2*dash {
		return st
	}
	two := float64(2*dash) + gap
	if closed {
		two += gap
	}
	if l <= two {
		f := l / two
		st.Dash = []float64{dash * f, gap * f}
		return st
	}
	if bs == css.Dashed {
		gap = blinkGap(l, dash, gap, closed)
	}
	st.Dash = []float64{dash, gap}
	return st
}

// blinkSquareDots reports a dotted line w points wide that Blink draws as
// square dots: 3 pixels and less.
func blinkSquareDots(w float64) bool { return w <= 3*css.Pt }

// blinkGap returns the gap closest to gap that fits a whole number of
// dashes of length dash, gaps between them, in length l (Blink's
// SelectBestDashGap): on an open path with a dash at each end, on a closed
// one with as many gaps as dashes.
func blinkGap(l, dash, gap float64, closed bool) float64 {
	if !numeric.Positive(l) || !numeric.Positive(dash+gap) {
		return gap
	}
	avail := l
	if !closed {
		avail += gap
	}
	lo := math.Floor(avail / (dash + gap))
	if lo > float64(maxBackgroundTiles) {
		return gap
	}
	hi := lo + 1
	loN, hiN := lo, hi
	if !closed {
		loN, hiN = lo-1, hi-1
	}
	loGap := math.Inf(1)
	if loN > 0 {
		loGap = (l - float64(lo*dash)) / loN
	}
	g := (l - float64(hi*dash)) / hiN
	if g <= 0 || math.Abs(loGap-gap) < math.Abs(g-gap) {
		g = loGap
	}
	if math.IsInf(g, 0) {
		// One dash: the gap passes the end of the line.
		return l + gap
	}
	return max(g, 0)
}

// blinkDashedSide draws side s of style bs (dashed or dotted), w points
// wide, of the border box r into g as Blink draws a straight side
// (DrawDashedOrDottedBoxSide): a line along the middle of the side from one
// outer corner to the other, left to right or top to bottom, stroked by
// [blinkDashStroke], half a width in at each end for round dots. A line of
// square dots starts and ends on a whole dot
// (EnforceDotsAtEndpoints): where its length in pixels would end it on a
// part of one, Blink fills the end dots as rectangles of their own, some a
// pixel longer, and strokes the pattern between them.
func blinkDashedSide(g *ditem, s css.Side, bs css.BorderStyle, c typeset.Color, r typeset.Rect, w float64) {
	horizontal := s == css.Top || s == css.Bottom
	t0, t1, c0 := r.X, r.X+r.Width, r.Y
	switch s {
	case css.Top:
	case css.Bottom:
		c0 = r.Y + r.Height - w
	case css.Left:
		t0, t1, c0 = r.Y, r.Y+r.Height, r.X
	case css.Right:
		t0, t1, c0 = r.Y, r.Y+r.Height, r.X+r.Width-w
	}
	// Blink snaps the side's ends to whole pixels before it lays the
	// pattern along it.
	t0, t1 = float64(math.Round(t0/css.Pt)*css.Pt), float64(math.Round(t1/css.Pt)*css.Pt)
	l := t1 - t0
	if l <= 0 {
		return
	}
	pt := func(t, u float64) typeset.Point {
		if horizontal {
			return typeset.Point{X: t, Y: u}
		}
		return typeset.Point{X: u, Y: t}
	}
	dot := func(a, z float64) {
		rc := typeset.Rect{X: a, Y: c0, Width: z - a, Height: w}
		if !horizontal {
			rc = typeset.Rect{X: c0, Y: a, Width: w, Height: z - a}
		}
		g.kids = append(g.kids, &ditem{frag: &typeset.Path{Ops: rectOps(rc), Fill: &typeset.Fill{Color: c}}})
	}
	stroke := blinkDashStroke(bs, c, w, l, false)
	switch {
	case bs == css.Dotted && blinkSquareDots(w):
		t0, t1 = blinkEndDots(dot, t0, t1, w, l)
	case bs == css.Dotted:
		// Round caps reach half a width past each end of a dot: the line
		// starts and ends half a width in.
		t0, t1 = t0+float64(w/2), t1-float64(w/2)
	}
	mid := c0 + float64(w/2)
	g.kids = append(g.kids, &ditem{frag: &typeset.Path{Ops: []typeset.PathOp{
		{Kind: typeset.MoveTo, P: [3]typeset.Point{pt(t0, mid)}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{pt(t1, mid)}},
	}, Stroke: stroke}})
}

// blinkEndDots fills by dot the end dots of a line of square dots w points
// wide and l long from t0 to t1, as Blink's EnforceDotsAtEndpoints does for
// widths of 1, 2 and 3 pixels and lengths in whole pixels, and returns the
// ends of the line left to stroke.
func blinkEndDots(dot func(a, z float64), t0, t1, w, l float64) (float64, float64) {
	wp, lp := math.Round(w/css.Pt), math.Round(l/css.Pt)
	m2, m4, m6 := math.Mod(lp, 2), math.Mod(lp, 4), math.Mod(lp, 6)
	start, end := false, false
	var startGrow, startShift, endGrow float64
	if wp == 1 && m2 == 0 || wp == 3 && m6 == 0 {
		start, startGrow, startShift = true, 1, 1
	}
	if wp == 2 && (m4 == 0 || m4 == 1) || wp == 3 && (m6 == 1 || m6 == 2) {
		start, startShift = true, -1
	}
	if wp == 2 && m4 == 0 || wp == 3 && m6 == 1 {
		end = true
	}
	if wp == 2 && m4 == 3 || wp == 3 && (m6 == 4 || m6 == 5) {
		start, startShift = true, 1
	}
	if wp == 3 && m6 == 5 {
		end = true
	} else if wp == 3 && m6 == 0 {
		end, endGrow = true, 1
	}
	if start {
		dot(t0, t0+w+float64(startGrow*css.Pt))
		t0 += float64(2*w) + float64(startShift*css.Pt)
	}
	if end {
		dot(t1-w-float64(endGrow*css.Pt), t1)
		t1 -= w + float64((endGrow+1)*css.Pt)
	}
	return t0, t1
}

// squareDots reports a dotted line too thin for round dots, w points
// wide: under 3 pixels Blink draws its dots as squares, as WeasyPrint does.
func squareDots(w float64) bool { return w < 3*css.Pt }

// bestGap returns the gap closest to gap that fits a whole number of dashes
// of length dash, gaps between them, in length l, dashes on both ends
// (Blink's StrokeData::SelectBestDashGap).
func bestGap(l, dash, gap float64) float64 {
	if !numeric.Positive(l) || !numeric.Positive(dash+gap) {
		return gap
	}
	lo := math.Floor((l + gap) / (dash + gap))
	if lo > float64(maxBackgroundTiles) {
		return gap
	}
	hi := lo + 1
	loGap, hiGap := math.Inf(1), (l-float64(hi*dash))/(hi-1)
	if lo > 1 {
		loGap = (l - float64(lo*dash)) / (lo - 1)
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
func roundedLength(r typeset.Rect, radii [4][2]float64) float64 {
	l := 2 * (r.Width + r.Height)
	for _, c := range radii {
		a, b := c[0], c[1]
		if a <= 0 || b <= 0 {
			continue
		}
		h := (a - b) * (a - b) / ((a + b) * (a + b))
		quarter := float64(math.Pi * (a + b) * (1 + 3*h/(10+math.Sqrt(4-float64(3*h)))) / 4)
		l += quarter - a - b
	}
	return l
}

// sideClip returns the trapezoid of side s between the outer box r and the
// inner box in, its ends cut on the diagonals from the outer to the inner
// corners.
func sideClip(s css.Side, r, in typeset.Rect) []typeset.PathOp {
	pt := func(x, y float64) typeset.Point { return typeset.Point{X: x, Y: y} }
	x0, y0, x1, y1 := r.X, r.Y, r.X+r.Width, r.Y+r.Height
	ix0, iy0, ix1, iy1 := in.X, in.Y, in.X+in.Width, in.Y+in.Height
	var q [4]typeset.Point
	switch s {
	case css.Top:
		q = [4]typeset.Point{pt(x0, y0), pt(x1, y0), pt(ix1, iy0), pt(ix0, iy0)}
	case css.Right:
		q = [4]typeset.Point{pt(x1, y0), pt(x1, y1), pt(ix1, iy1), pt(ix1, iy0)}
	case css.Bottom:
		q = [4]typeset.Point{pt(x1, y1), pt(x0, y1), pt(ix0, iy1), pt(ix1, iy1)}
	case css.Left:
		q = [4]typeset.Point{pt(x0, y1), pt(x0, y0), pt(ix0, iy0), pt(ix0, iy1)}
	}
	ops := []typeset.PathOp{{Kind: typeset.MoveTo, P: [3]typeset.Point{q[0]}}}
	for _, v := range q[1:] {
		ops = append(ops, typeset.PathOp{Kind: typeset.LineTo, P: [3]typeset.Point{v}})
	}
	return append(ops, typeset.PathOp{Kind: typeset.ClosePath})
}

// side draws one border side of style bdr into g, which clips it to the
// side's trapezoid; current reports a side whose two-tone styles paint as
// [currentShades] say ([quirkShades]).
func (p *painter) side(g *ditem, s css.Side, bdr css.Border, current bool, r typeset.Rect, radii [4][2]float64, inner typeset.Rect, iradii [4][2]float64, bd [4]float64) {
	c, _ := colorOf(bdr.Color)
	fill := func(ops []typeset.PathOp, col typeset.Color) {
		g.kids = append(g.kids, &ditem{frag: &typeset.Path{Ops: ops, Fill: &typeset.Fill{Color: col, Rule: typeset.EvenOdd}}})
	}
	frac := func(f float64) (typeset.Rect, [4][2]float64) {
		var d [4]float64
		for i := range d {
			d[i] = bd[i] * f
		}
		return insetRect(r, radii, d)
	}
	dark, light := shades(c, p.blink())
	if current && p.blink() {
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
		if p.blink() {
			blinkDashedSide(g, s, bdr.Style, c, r, w)
			return
		}
		var a, z typeset.Point
		switch s {
		case css.Top:
			a, z = typeset.Point{X: r.X, Y: r.Y + float64(w/2)}, typeset.Point{X: r.X + r.Width, Y: r.Y + float64(w/2)}
		case css.Right:
			a, z = typeset.Point{X: r.X + r.Width - float64(w/2), Y: r.Y}, typeset.Point{X: r.X + r.Width - float64(w/2), Y: r.Y + r.Height}
		case css.Bottom:
			a, z = typeset.Point{X: r.X + r.Width, Y: r.Y + r.Height - float64(w/2)}, typeset.Point{X: r.X, Y: r.Y + r.Height - float64(w/2)}
		case css.Left:
			a, z = typeset.Point{X: r.X + float64(w/2), Y: r.Y + r.Height}, typeset.Point{X: r.X + float64(w/2), Y: r.Y}
		}
		l := detmath.Hypot(z.X-a.X, z.Y-a.Y)
		if bdr.Style == css.Dotted && !squareDots(w) {
			// The dots sit on the line, the first and the last centred
			// half a width in from the corners.
			if l <= w {
				return
			}
			ux, uy := (z.X-a.X)/l, (z.Y-a.Y)/l
			a = typeset.Point{X: a.X + float64(ux*w/2), Y: a.Y + float64(uy*w/2)}
			z = typeset.Point{X: z.X - float64(ux*w/2), Y: z.Y - float64(uy*w/2)}
			l -= w
		}
		g.kids = append(g.kids, &ditem{frag: &typeset.Path{Ops: []typeset.PathOp{
			{Kind: typeset.MoveTo, P: [3]typeset.Point{a}},
			{Kind: typeset.LineTo, P: [3]typeset.Point{z}},
		}, Stroke: dashStroke(bdr.Style, c, w, l, false, false)}})
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
func strokedSide(s css.Side, r typeset.Rect, w float64, c typeset.Color) *typeset.Path {
	var a, z typeset.Point
	switch s {
	case css.Top:
		a, z = typeset.Point{X: r.X, Y: r.Y + float64(w/2)}, typeset.Point{X: r.X + r.Width, Y: r.Y + float64(w/2)}
	case css.Bottom:
		a, z = typeset.Point{X: r.X, Y: r.Y + r.Height - float64(w/2)}, typeset.Point{X: r.X + r.Width, Y: r.Y + r.Height - float64(w/2)}
	case css.Left:
		a, z = typeset.Point{X: r.X + float64(w/2), Y: r.Y}, typeset.Point{X: r.X + float64(w/2), Y: r.Y + r.Height}
	case css.Right:
		a, z = typeset.Point{X: r.X + r.Width - float64(w/2), Y: r.Y}, typeset.Point{X: r.X + r.Width - float64(w/2), Y: r.Y + r.Height}
	}
	return strokedLine(a, z, w, c)
}

// strokedLine returns the line from a to z stroked in c, w points wide and
// at least css.MinBorderStroke, with butt caps.
//
// # Corpus evidence
//
//   - litho/pdf: a flat ODT whose paragraphs have solid borders of 0.02 pt to
//     1 pt prints, through the pinned LibreOffice, strokes of 0.1 pt for
//     every border of one or two twips, 0.15, 0.2, 0.25, 0.3, 0.5, 0.75
//     and 1 pt at the widths above, and no border for 0.01 pt.
func strokedLine(a, z typeset.Point, w float64, c typeset.Color) *typeset.Path {
	return &typeset.Path{Ops: []typeset.PathOp{
		{Kind: typeset.MoveTo, P: [3]typeset.Point{a}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{z}},
	}, Stroke: &typeset.Stroke{Color: c, Width: max(w, css.MinBorderStroke)}}
}
