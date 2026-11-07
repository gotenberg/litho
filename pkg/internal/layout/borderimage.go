package layout

import (
	"fmt"
	"image"
	"image/draw"
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// nineSource is the image a border image cuts: a picture, or a gradient
// sized to the border image area, w by h points.
type nineSource struct {
	pic  *Picture
	fill *compose.Fill
	w, h float64
	// natural reports an image with a natural size, whose slices give the
	// auto widths (CSS Backgrounds 3, 6.4).
	natural bool
}

// borderImage paints the border image of st over the border box r of b,
// whose used border widths are bd, the sides in draw (CSS Backgrounds 3,
// 6). It reports false when st has none to draw, no image or one that
// failed to load, and the border styles paint instead (6.1). The border
// image is not clipped by border-radius (6.1).
func (p *painter) borderImage(b *box, st *css.Style, r compose.Rect, bd [4]float64, draw [4]bool) bool {
	bi := &st.BorderImage
	if !bi.HasImage() {
		return false
	}
	frag, strip := r, p.strip
	if strip != nil && strip.box == b {
		// CSS Backgrounds 3, 6.6 with box-decoration-break: slice: the
		// image is drawn over the strip of the inline box's fragments
		// laid end to end, and this fragment shows its part of it.
		r = compose.Rect{X: r.X - strip.before, Y: r.Y, Width: strip.total, Height: r.Height}
		bd, draw = b.bd, [4]bool{true, true, true, true}
	} else {
		strip = nil
	}
	// CSS Backgrounds 3, 6.5: the border image area is the border box
	// grown by the outsets, a number multiplying the border width.
	var out [4]float64
	for s := range 4 {
		if !draw[s] {
			continue
		}
		o := bi.Outset[s]
		switch o.Kind { //nolint:exhaustive // the outset is a number or a length
		case css.Number:
			out[s] = clamp(o.Value) * bd[s]
		case css.Points:
			out[s] = clamp(o.Value)
		}
		out[s] = max(out[s], 0)
	}
	area := compose.Rect{
		X: r.X - out[css.Left], Y: r.Y - out[css.Top],
		Width:  r.Width + out[css.Left] + out[css.Right],
		Height: r.Height + out[css.Top] + out[css.Bottom],
	}
	if !(area.Width > 0) || !(area.Height > 0) || math.IsInf(area.Width, 0) || math.IsInf(area.Height, 0) {
		return true
	}
	src, ok := p.nineSource(bi, area, r.Y)
	if !ok {
		return false
	}
	slices := sliceSizes(bi, src)
	widths := imageWidths(bi, src, slices, bd, draw, area)
	pieces := ninePieces(bi, src, slices, widths, area)
	g := &ditem{group: true, opacity: 1}
	tiles := 0
	for _, pc := range pieces {
		pc.crop = p.cropSource(src.pic, pc.src)
		for _, t := range pc.tiles() {
			if tiles == maxBackgroundTiles {
				p.e.warn(codeBackgroundTiles, fmt.Sprintf("a border image repeated more than %d times was cut short", maxBackgroundTiles), r.Y)
				break
			}
			if k := p.nineTile(src, pc, t); k != nil {
				g.kids = append(g.kids, k)
				tiles++
			}
		}
	}
	if len(g.kids) == 0 {
		return true
	}
	if strip != nil {
		// The fragment's part: its own span, reaching the area's ends on
		// the edges it draws.
		x0, x1 := frag.X, frag.X+frag.Width
		if strip.left {
			x0 = min(x0, area.X)
		}
		if strip.right {
			x1 = max(x1, area.X+area.Width)
		}
		g.clip = &compose.Path{Ops: rectOps(compose.Rect{X: x0, Y: area.Y, Width: x1 - x0, Height: area.Height}), Fill: &compose.Fill{}}
	}
	c := p.sync(p.chain(b, false))
	c.kids = append(c.kids, g)
	return true
}

// imageStrip places one fragment of an inline box on the strip of all its
// fragments, laid end to end in the box's direction.
type imageStrip struct {
	box *box
	// before is the width of the strip before the fragment, total the
	// strip's width.
	before, total float64
	// left and right report the fragment draws the box's left or right
	// edge.
	left, right bool
}

// imageStrip returns the place of the fragment f, painted over
// r, on the strip of its box's fragments in the lines of its block
// container (CSS Backgrounds 3, 6.6 and CSS Fragmentation 3, 5.4).
func (p *painter) imageStrip(f *inlineFrag, r compose.Rect) *imageStrip {
	s := &imageStrip{box: f.box, left: f.left, right: f.right}
	c := f.box.parent
	for c != nil && c.kind == kindInline {
		c = c.parent
	}
	if c == nil {
		s.total = r.Width
		return s
	}
	rtl := f.box.st.Direction == css.RTL
	seen := false
	for _, cl := range c.lines {
		for _, g := range cl.frags {
			if g.box != f.box {
				continue
			}
			w := g.x1 - g.x0
			s.total += w
			if g == f {
				seen = true
				continue
			}
			// Right to left, the fragments run from the strip's right
			// end: those after this one lie left of it.
			if !seen && !rtl || seen && rtl {
				s.before += w
			}
		}
	}
	return s
}

// nineSource loads the image of bi for the border image area: a picture
// at its natural size, a vector one without natural dimensions sized to
// the area, or a gradient, whose concrete size is the area's (CSS
// Backgrounds 3, 6.2, the default object size).
func (p *painter) nineSource(bi *css.BorderImage, area compose.Rect, y float64) (nineSource, bool) {
	if bi.Gradient != nil {
		fill, ok := p.e.gradientFill(bi.Gradient, area.Width, area.Height, y)
		if !ok {
			// A gradient that paints nothing is still the border image.
			fill = nil
		}
		return nineSource{fill: fill, w: area.Width, h: area.Height}, true
	}
	pic := p.e.picture(bi.Source, y)
	if pic == nil {
		return nineSource{}, false
	}
	natural := pic.Vector == nil || !pic.NoNaturalWidth && !pic.NoNaturalHeight
	pic = naturalPicture(pic, area.Width, area.Height)
	return nineSource{pic: pic, w: pic.Width, h: pic.Height, natural: natural}, true
}

// sliceSizes returns the slice offsets of bi into the image src, top,
// right, bottom and left: a number counts image pixels, CSS pixels for a
// vector image, a percentage the image's height or width, each clamped
// to the image (CSS Backgrounds 3, 6.3).
func sliceSizes(bi *css.BorderImage, src nineSource) [4]float64 {
	var out [4]float64
	for s := range 4 {
		ref := src.h
		if s == int(css.Left) || s == int(css.Right) {
			ref = src.w
		}
		d := bi.Slice[s]
		v := 0.0
		switch d.Kind { //nolint:exhaustive // a slice is a number or a percentage
		case css.Number:
			v = clamp(d.Value) * css.Pt
		case css.Percent:
			v = clamp(d.Value) * ref
		}
		out[s] = min(max(v, 0), ref)
	}
	return out
}

// imageWidths returns the border image widths of bi, top, right, bottom
// and left (CSS Backgrounds 3, 6.4): a number multiplies the border
// width, a percentage takes the area's width or height, auto the slice's
// size, or the border width for an image without a natural size; widths
// whose opposite pairs overlap are scaled down together. A side cut away
// in draw has none.
func imageWidths(bi *css.BorderImage, src nineSource, slices, bd [4]float64, draw [4]bool, area compose.Rect) [4]float64 {
	var w [4]float64
	for s := range 4 {
		if !draw[s] {
			continue
		}
		ref := area.Height
		if s == int(css.Left) || s == int(css.Right) {
			ref = area.Width
		}
		d := bi.Width[s]
		switch d.Kind { //nolint:exhaustive // the other kinds do not occur
		case css.Number:
			w[s] = clamp(d.Value) * bd[s]
		case css.Points, css.Percent:
			w[s] = resolve(d, ref)
		case css.Auto:
			w[s] = bd[s]
			if src.natural {
				w[s] = slices[s]
			}
		}
		w[s] = max(w[s], 0)
	}
	f := 1.0
	if sum := w[css.Left] + w[css.Right]; sum > 0 {
		f = min(f, area.Width/sum)
	}
	if sum := w[css.Top] + w[css.Bottom]; sum > 0 {
		f = min(f, area.Height/sum)
	}
	if f < 1 {
		for s := range w {
			w[s] *= f
		}
	}
	return w
}

// ninePiece is one of the nine regions of a border image: the part src of
// the image drawn into dst, tiled per axis by rep at the scales sx and
// sy, which turn image points into page points.
type ninePiece struct {
	src, dst compose.Rect
	rep      [2]css.ImageRepeat
	sx, sy   float64
	// crop is the part src of a raster image cut out as an image of its
	// own, nil for a vector image, a gradient or a JPEG kept as it is.
	crop *compose.ImageSource
}

// scaleOf returns w over s, 0 when either is zero.
func scaleOf(w, s float64) float64 {
	if !(w > 0) || !(s > 0) {
		return 0
	}
	return w / s
}

// ninePieces cuts the image src and the border image area into the nine
// regions of CSS Backgrounds 3, 6.6: four corners, stretched to their
// region, four edges, scaled to the border image width across them and
// tiled along them, and, with fill, the middle, scaled by the factors of
// the top and left edges, or failing those the bottom and right ones, or
// not at all.
func ninePieces(bi *css.BorderImage, src nineSource, sl, w [4]float64, area compose.Rect) []ninePiece {
	t, r, b, l := sl[css.Top], sl[css.Right], sl[css.Bottom], sl[css.Left]
	wt, wr, wb, wl := w[css.Top], w[css.Right], w[css.Bottom], w[css.Left]
	mw, mh := src.w-l-r, src.h-t-b
	dx0, dy0 := area.X, area.Y
	dx1, dy1 := area.X+wl, area.Y+wt
	dx2, dy2 := area.X+area.Width-wr, area.Y+area.Height-wb
	dmw, dmh := dx2-dx1, dy2-dy1
	rect := func(x, y, rw, rh float64) compose.Rect { return compose.Rect{X: x, Y: y, Width: rw, Height: rh} }
	stretch := [2]css.ImageRepeat{css.ImageStretch, css.ImageStretch}
	h, v := bi.Repeat[0], bi.Repeat[1]
	out := []ninePiece{
		{src: rect(0, 0, l, t), dst: rect(dx0, dy0, wl, wt), rep: stretch},
		{src: rect(src.w-r, 0, r, t), dst: rect(dx2, dy0, wr, wt), rep: stretch},
		{src: rect(src.w-r, src.h-b, r, b), dst: rect(dx2, dy2, wr, wb), rep: stretch},
		{src: rect(0, src.h-b, l, b), dst: rect(dx0, dy2, wl, wb), rep: stretch},
	}
	ft, fr, fb, fl := scaleOf(wt, t), scaleOf(wr, r), scaleOf(wb, b), scaleOf(wl, l)
	out = append(out,
		ninePiece{src: rect(l, 0, mw, t), dst: rect(dx1, dy0, dmw, wt), rep: [2]css.ImageRepeat{h, css.ImageStretch}, sx: ft, sy: ft},
		ninePiece{src: rect(src.w-r, t, r, mh), dst: rect(dx2, dy1, wr, dmh), rep: [2]css.ImageRepeat{css.ImageStretch, v}, sx: fr, sy: fr},
		ninePiece{src: rect(l, src.h-b, mw, b), dst: rect(dx1, dy2, dmw, wb), rep: [2]css.ImageRepeat{h, css.ImageStretch}, sx: fb, sy: fb},
		ninePiece{src: rect(0, t, l, mh), dst: rect(dx0, dy1, wl, dmh), rep: [2]css.ImageRepeat{css.ImageStretch, v}, sx: fl, sy: fl},
	)
	if bi.Fill {
		sx := ft
		if sx == 0 {
			sx = fb
		}
		if sx == 0 {
			sx = 1
		}
		sy := fl
		if sy == 0 {
			sy = fr
		}
		if sy == 0 {
			sy = 1
		}
		out = append(out, ninePiece{src: rect(l, t, mw, mh), dst: rect(dx1, dy1, dmw, dmh), rep: [2]css.ImageRepeat{h, v}, sx: sx, sy: sy})
	}
	return out
}

// span is one tile along an axis: its start and length.
type span struct{ at, size float64 }

// tileSpans returns the tiles along one axis of a region starting at
// start, size long, of an image part n points long scaled by k (CSS
// Backgrounds 3, 6.6, with Blink's tile placement): stretch draws one
// tile over the region; repeat draws tiles of the scaled size, one
// centred on the region; round fits a whole number of them, stretched to
// fill; space fits as many whole ones as it can, the room left spread
// evenly before, between and after them, and none when not one fits.
func tileSpans(rep css.ImageRepeat, start, size, n, k float64) []span {
	if !(size > 0) || !(n > 0) {
		return nil
	}
	t := n * k
	if rep == css.ImageStretch || !(t > 0) || math.IsInf(t, 0) {
		return []span{{start, size}}
	}
	limit := float64(maxBackgroundTiles)
	switch rep { //nolint:exhaustive // stretch is handled above
	case css.ImageRound:
		c := max(math.Round(size/t), 1)
		if c > limit {
			return nil
		}
		out := make([]span, 0, int(c)) // fits int: at most limit, checked above
		for i := 0.0; i < c; i++ {
			out = append(out, span{start + i*size/c, size / c})
		}
		return out
	case css.ImageSpace:
		c := math.Floor(size / t)
		if c < 1 || c > limit {
			return nil
		}
		gap := (size - c*t) / (c + 1)
		out := make([]span, 0, int(c)) // fits int: at most limit, checked above
		for i := 0.0; i < c; i++ {
			out = append(out, span{start + gap + i*(t+gap), t})
		}
		return out
	}
	// Repeat: a tile centred on the region, then as many as cover it on
	// either side.
	first := start + (size-t)/2
	first -= math.Ceil((first-start)/t) * t
	c := math.Ceil((start + size - first) / t)
	if c > limit {
		return nil
	}
	out := make([]span, 0, max(int(c), 0)) // fits int: at most limit, checked above
	for i := 0.0; i < c; i++ {
		out = append(out, span{first + i*t, t})
	}
	return out
}

// tiles returns the placements of the piece's image part over its
// region.
func (pc ninePiece) tiles() []compose.Rect {
	if !(pc.src.Width > 0) || !(pc.src.Height > 0) || !(pc.dst.Width > 0) || !(pc.dst.Height > 0) {
		return nil
	}
	xs := tileSpans(pc.rep[0], pc.dst.X, pc.dst.Width, pc.src.Width, pc.sx)
	ys := tileSpans(pc.rep[1], pc.dst.Y, pc.dst.Height, pc.src.Height, pc.sy)
	if len(xs)*len(ys) > maxBackgroundTiles {
		return nil
	}
	out := make([]compose.Rect, 0, len(xs)*len(ys))
	for _, y := range ys {
		for _, x := range xs {
			out = append(out, compose.Rect{X: x.at, Y: y.at, Width: x.size, Height: y.size})
		}
	}
	return out
}

// nineTile returns the display item drawing the piece's image part into
// the tile t, clipped to the tile and the piece's region: the whole
// image scaled so the part lands on the tile.
func (p *painter) nineTile(src nineSource, pc ninePiece, t compose.Rect) *ditem {
	clip, ok := intersect(t, pc.dst)
	if !ok {
		return nil
	}
	kx, ky := t.Width/pc.src.Width, t.Height/pc.src.Height
	img := compose.Rect{X: t.X - pc.src.X*kx, Y: t.Y - pc.src.Y*ky, Width: src.w * kx, Height: src.h * ky}
	// A vector image and a gradient are drawn at the image's size, then
	// scaled: the image is cut after it is sized (CSS Backgrounds 3,
	// 6.2), so a vector one keeps its own aspect ratio inside it.
	natural := compose.Rect{Width: src.w, Height: src.h}
	page := -1
	if p.mono >= 0 {
		page = p.mono
	}
	var inner compose.Fragment
	switch {
	case pc.crop != nil:
		// The cut-out part fills the tile: no neighbouring pixel bleeds
		// into it, as Blink draws each piece from its own sub-image.
		f := &compose.Image{Rect: t, Source: pc.crop}
		if clip == t {
			return &ditem{frag: f, y0: t.Y, y1: t.Y + t.Height, page: page}
		}
		inner = f
	case src.pic != nil && src.pic.Vector == nil:
		inner = &compose.Image{Rect: img, Source: src.pic.Source}
	case src.pic != nil:
		inner = p.e.pictureFrag(src.pic, natural, nil)
	case src.fill != nil:
		inner = &compose.Path{Ops: rectOps(natural), Fill: src.fill}
	}
	if inner == nil {
		return nil
	}
	f := inner
	if _, raster := inner.(*compose.Image); !raster {
		f = &compose.Group{Opacity: 1, Transform: compose.Matrix{A: kx, D: ky, E: img.X, F: img.Y}, Content: []compose.Fragment{inner}}
	}
	return &ditem{
		group: true, opacity: 1,
		clip: &compose.Path{Ops: rectOps(clip), Fill: &compose.Fill{}},
		kids: []*ditem{{frag: f, y0: clip.Y, y1: clip.Y + clip.Height, page: page}},
	}
}

// cropKey identifies a part of a raster image cut out by [cropSource].
type cropKey struct {
	src  *compose.ImageSource
	rect image.Rectangle
}

// cropSource returns the part r, in points, of the raster picture pic as
// an image of its own, its edges rounded to whole image pixels; nil for a
// vector picture, a JPEG kept as it is, or an empty part. The parts are
// cut once per paint.
func (p *painter) cropSource(pic *Picture, r compose.Rect) *compose.ImageSource {
	if pic == nil || pic.Source == nil || pic.Source.Image == nil || !(pic.Width > 0) || !(pic.Height > 0) {
		return nil
	}
	img := pic.Source.Image
	bounds := img.Bounds()
	kx, ky := float64(bounds.Dx())/pic.Width, float64(bounds.Dy())/pic.Height
	rect := image.Rect(
		bounds.Min.X+int(math.Round(r.X*kx)), bounds.Min.Y+int(math.Round(r.Y*ky)),
		bounds.Min.X+int(math.Round((r.X+r.Width)*kx)), bounds.Min.Y+int(math.Round((r.Y+r.Height)*ky)),
	).Intersect(bounds)
	if rect.Empty() {
		return nil
	}
	if rect == bounds {
		return pic.Source
	}
	key := cropKey{pic.Source, rect}
	if c, ok := p.crops[key]; ok {
		return c
	}
	out := image.NewNRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(out, out.Bounds(), img, rect.Min, draw.Src)
	c := &compose.ImageSource{Image: out}
	if p.crops == nil {
		p.crops = map[cropKey]*compose.ImageSource{}
	}
	p.crops[key] = c
	return c
}

// intersect returns the overlap of a and b, false when they do not
// overlap.
func intersect(a, b compose.Rect) (compose.Rect, bool) {
	x0, y0 := max(a.X, b.X), max(a.Y, b.Y)
	x1, y1 := min(a.X+a.Width, b.X+b.Width), min(a.Y+a.Height, b.Y+b.Height)
	if x1 <= x0 || y1 <= y0 {
		return compose.Rect{}, false
	}
	return compose.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}, true
}
