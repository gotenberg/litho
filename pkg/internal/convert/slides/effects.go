package slides

import (
	"image"
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/raster"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// effectScale is the resolution in pixels a point an effect's bitmap is
// drawn at before its pixel budget applies: 720 pixels an inch, Impress's
// for a printout.
//
// # Corpus evidence
//
//   - pptx: a 10 by 10 pt rectangle with a shadow blurred over 2 pt
//     prints its shadow as a 141 by 141 pixel bitmap over 14 pt, and
//     rectangles of 20, 40 and 45 pt blurred over 1 pt as bitmaps of 220,
//     420 and 470 pixels: the range's side in points times 10, rounded up.
const effectScale = 10

// effectPixels caps the pixels of one effect bitmap: a bitmap past it is
// drawn at the resolution that holds this many pixels, its aspect kept.
// Impress's own budget; also an anti-DoS guard, not spec-mandated: 250,000
// pixels draw a shadow over a whole slide at 2.5 pixels a point, which
// a blur hides, and bound the work and the bytes of every effect.
//
// # Corpus evidence
//
//   - pptx: sd/shape-blur-effect's oval prints its 171.9 by 169.1 pt
//     shadow as a bitmap of 504 by 496 pixels, sd/effectOrder's glows as
//     626 by 399 and 660 by 379, sd-data/n819614's 56 shadows each within
//     a few hundred pixels of 250,000.
const effectPixels = 250000

// effectGrid is the pixel grid of an effect bitmap: the slide area box it
// covers, w by h pixels, and how many pixels a point Impress's filters
// take their radii at.
type effectGrid struct {
	box  compose.Rect
	w, h int
	// unit is the pixels of a point the radii are measured in, the
	// unreduced resolution times the budget's reduction.
	unit float64
}

// newGrid returns the grid of an effect over the slide area box: each side
// at effectScale pixels a point, rounded up, the whole reduced to
// effectPixels when it holds more; false for an area too small to draw.
func newGrid(box compose.Rect) (effectGrid, bool) {
	if !finite(box) || !(box.Width > 0) || !(box.Height > 0) {
		return effectGrid{}, false
	}
	w0, h0 := math.Ceil(box.Width*effectScale), math.Ceil(box.Height*effectScale)
	if w0 < 2 || h0 < 2 {
		return effectGrid{}, false
	}
	w, h := w0, h0
	if w*h > effectPixels {
		f := math.Sqrt(effectPixels / (w * h))
		w, h = max(math.Round(w*f), 1), max(math.Round(h*f), 1)
	}
	g := effectGrid{box: box, w: int(w), h: int(h)} // fits int: at most effectPixels each, set above
	g.unit = effectScale * (w/w0 + h/h0) / 2
	return g, true
}

// pixels returns the transform from slide points to the grid's pixels.
func (g effectGrid) pixels() compose.Matrix {
	sx, sy := float64(g.w)/g.box.Width, float64(g.h)/g.box.Height
	return compose.Matrix{A: sx, D: sy, E: -g.box.X * sx, F: -g.box.Y * sy}
}

// radius returns the radius r points spans on the grid in whole pixels,
// as Impress's filters take it: r at effectScale rounded up, scaled by the
// budget's reduction, then truncated.
func (g effectGrid) radius(r float64) int {
	v := math.Ceil(r*effectScale) * g.unit / effectScale
	if !(v >= 1) {
		return 0
	}
	return int(min(v, raster.MaxKernelRadius)) // fits int: at most MaxKernelRadius
}

// image returns the bitmap img placed over the grid's area, in slide
// coordinates.
func (g effectGrid) image(img image.Image) compose.Fragment {
	return &compose.Image{Rect: g.box, Source: &compose.ImageSource{Image: img}}
}

// silhouette is what a shape paints, as the filters see it: its painted
// paths in slide coordinates and their bounds there.
type silhouette struct {
	paths  []raster.Path
	colors []compose.Color
	// flat is false when a path paints a shading or a picture, which a
	// soft edge's bitmap cannot redraw in one colour.
	flat   bool
	bounds compose.Rect
}

// silhouetteOf returns the silhouette of frags drawn under m (their
// coordinates to the slide's): each path filled or stroked, each picture
// its rectangle. Text is left out: its glyphs have no outline here.
func silhouetteOf(frags []compose.Fragment, m compose.Matrix) *silhouette {
	s := &silhouette{flat: true}
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	add := func(ops []compose.PathOp, width float64, c compose.Color, tr compose.Matrix) {
		ops = mapOps(ops, tr)
		width *= math.Sqrt(math.Abs(tr.A*tr.D - tr.B*tr.C))
		hw := width / 2
		for _, op := range ops {
			n := 1
			if op.Kind == compose.CurveTo {
				n = 3
			}
			if op.Kind == compose.ClosePath {
				continue
			}
			for _, p := range op.P[:n] {
				x0, y0, x1, y1 = min(x0, p.X-hw), min(y0, p.Y-hw), max(x1, p.X+hw), max(y1, p.Y+hw)
			}
		}
		s.paths = append(s.paths, raster.Path{Ops: ops, Width: width})
		s.colors = append(s.colors, c)
	}
	var walk func(fs []compose.Fragment, tr compose.Matrix, depth int)
	walk = func(fs []compose.Fragment, tr compose.Matrix, depth int) {
		for _, f := range fs {
			switch f := f.(type) {
			case *compose.Path:
				if f.Fill != nil {
					if f.Fill.Shading != nil {
						s.flat = false
					}
					add(f.Ops, 0, f.Fill.Color, tr)
				}
				if f.Stroke != nil && f.Stroke.Width > 0 {
					add(f.Ops, f.Stroke.Width, f.Stroke.Color, tr)
				}
			case *compose.Image:
				s.flat = false
				add(rectOps(f.Rect), 0, compose.Color{A: 1}, tr)
			case *compose.Group:
				if depth < maxGroupDepth {
					t := f.Transform
					if t == (compose.Matrix{}) {
						t = compose.Identity
					}
					walk(f.Content, then(t, tr), depth+1)
				}
			}
		}
	}
	walk(frags, m, 0)
	if len(s.paths) == 0 || !(x1 > x0) || !(y1 > y0) {
		return nil
	}
	s.bounds = compose.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
	return s
}

// mapOps returns ops mapped through m.
func mapOps(ops []compose.PathOp, m compose.Matrix) []compose.PathOp {
	out := make([]compose.PathOp, len(ops))
	for i, op := range ops {
		out[i].Kind = op.Kind
		for j, p := range op.P {
			x, y := apply(m, p.X, p.Y)
			out[i].P[j] = compose.Point{X: x, Y: y}
		}
	}
	return out
}

// coverage returns the silhouette's coverage on g, its paths moved by tr
// in slide coordinates first.
func (s *silhouette) coverage(g effectGrid, tr compose.Matrix) *image.Alpha {
	m := image.NewAlpha(image.Rect(0, 0, g.w, g.h))
	raster.Draw(m, s.paths, then(tr, g.pixels()))
	return m
}

// grow returns r grown by d on every side.
func grow(r compose.Rect, d float64) compose.Rect {
	return compose.Rect{X: r.X - d, Y: r.Y - d, Width: r.Width + 2*d, Height: r.Height + 2*d}
}

// intersect returns the overlap of a and b, false for none.
func intersect(a, b compose.Rect) (compose.Rect, bool) {
	x0, y0 := max(a.X, b.X), max(a.Y, b.Y)
	x1, y1 := min(a.X+a.Width, b.X+b.Width), min(a.Y+a.Height, b.Y+b.Height)
	if !(x1 > x0) || !(y1 > y0) {
		return compose.Rect{}, false
	}
	return compose.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}, true
}

// union raises the coverage of m by that of n, source-over, in place.
func union(m, n *image.Alpha) {
	for i, v := range n.Pix {
		a, b := uint32(m.Pix[i]), uint32(v)
		m.Pix[i] = uint8(a + (b*(255-a)+127)/255) //nolint:gosec // a + b(1-a) of two coverages is a coverage
	}
}

// invert returns the inverse of m, false when m is singular.
func invert(m compose.Matrix) (compose.Matrix, bool) {
	det := m.A*m.D - m.B*m.C
	if !(math.Abs(det) > 1e-12) || math.IsInf(det, 0) {
		return compose.Matrix{}, false
	}
	return compose.Matrix{
		A: m.D / det, B: -m.B / det, C: -m.C / det, D: m.A / det,
		E: (m.C*m.F - m.D*m.E) / det, F: (m.B*m.E - m.A*m.F) / det,
	}, true
}

// effectPlan is the effects of one shape: what paints them, in slide
// coordinates, and how many times its glow and soft edge apply.
type effectPlan struct {
	s     *Shape
	sil   *silhouette
	area  compose.Rect
	twice bool
}

// effectsTwice reports whether Impress applies the glow and the soft edge
// of s twice: a shape of the rectangle preset (a PresentationML rect,
// text boxes included, and an OpenDocument custom shape of type
// rectangle), which Impress draws through a rectangle carrying the
// effects inside the custom shape that carries them again. A shape of no
// geometry, an OpenDocument draw:rect, applies them once. The second glow
// is the glow of the first one's outline, two radii out; the second soft
// edge squares the first one's fade.
//
// # Corpus evidence
//
//   - pptx: sd/effectOrder's rectangle prints two glow bitmaps, over its
//     box grown by one and by two glow radii, the outer one opaque from
//     one radius out; a rectangle, a text box and a turned rectangle with
//     a glow print two, the presets roundRect, flowChartProcess,
//     snip1Rect, triangle, ellipse, diamond and a custom rectangle one.
//   - pptx: sd/shape-soft-edges's rectangle fades as the square of a
//     single soft edge: 64 of 255 at the radius, 128 a quarter radius
//     further in; its diamond fades once.
//   - odp: a draw:custom-shape of type rectangle with a glow prints two
//     glow bitmaps, a draw:rect one.
func effectsTwice(s *Shape) bool {
	g := s.Geometry
	return s.Kind == KindGeometry && g != nil && g.Preset == "rect" && g.Custom == nil
}

// effects returns what paints under the geometry shape s for its effects
// (ECMA-376 Part 1, 20.1.8.49 outerShdw, 20.1.8.32 glow, 20.1.8.53
// softEdge; ODF 1.3 Part 3, 20.158 draw:shadow with LibreOffice's
// loext:shadow-blur, loext:glow-radius and loext:softedge-radius), paths
// being the shape's paths in its box, m mapping its parent's coordinates
// to the slide's; and the paths the builder still fills and strokes: none
// when a soft edge has drawn them already.
//
// Impress paints, from the bottom: the shadow of everything above it, the
// glows, then the shape with its soft edge, then its text. A blurred
// shadow, a glow and a soft edge are bitmaps over the slide area they
// reach, clipped to the slide grown by their radius, of effectScale
// pixels a point up to effectPixels; a hard shadow is a copy of the
// shape's fill, line and text in the shadow's colour.
//
// # Corpus evidence
//
//   - pptx: sd/effectOrder's rectangle with a glow and a shadow prints the
//     shadow (a bitmap of the shape and both glows, at 40% opacity), the
//     outer glow, the glow, then the rectangle.
func (b *builder) effects(s *Shape, paths []shape.Path, box compose.Rect, m compose.Matrix, txt *textResult) ([]compose.Fragment, []shape.Path) {
	if s.Shadow == nil && s.Glow == nil && !(s.SoftEdge > 0) {
		return nil, paths
	}
	sm := aboutCentre(box, s.Rotation, s.FlipH, s.FlipV)
	toSlide := then(sm, m)
	var own []compose.Fragment
	for _, p := range paths {
		if p.Fill {
			own = append(own, b.fill(&s.Fill, box, p.Ops, p.Mode)...)
		}
		if p.Stroke {
			own = append(own, b.stroke(&s.Line, p.Ops)...)
		}
	}
	var text []compose.Fragment
	if txt != nil {
		rot := s.Rotation
		if s.FlipV {
			rot += 180
		}
		text = group(then(aboutCentre(box, rot, false, false), invertOr(sm)), txt.frags)
	}
	under, drawn := b.paintEffects(s, own, text, toSlide, effectsTwice(s))
	if drawn != nil {
		return append(under, drawn...), nil
	}
	return under, paths
}

// invertOr returns the inverse of m, the identity when m is singular.
func invertOr(m compose.Matrix) compose.Matrix {
	if inv, ok := invert(m); ok {
		return inv
	}
	return compose.Identity
}

// contentEffects returns frags, a picture's or a table's in the shape's
// box, with what paints under them for s's shadow and glow, m mapping the
// parent's coordinates to the slide's. A picture's silhouette is its
// rectangle, a table's its borders and fills, never its text, which a
// table's shadow leaves out. A soft edge is not drawn on a picture or a
// table.
//
// # Corpus evidence
//
//   - pptx: a picture with a shadow blurred over 6 pt prints its shadow
//     as a bitmap of its box grown by 6 pt, opaque inside; oox/table-shadow
//     prints its table's borders again 10 pt down and right in the
//     shadow's red, its text once.
func (b *builder) contentEffects(s *Shape, frags []compose.Fragment, m compose.Matrix) []compose.Fragment {
	if s.Shadow == nil && s.Glow == nil && !(s.SoftEdge > 0) {
		return frags
	}
	if s.SoftEdge > 0 {
		b.warn(CodeEffect, "the soft edge of "+quote(s.Name)+" is not drawn")
	}
	sm := aboutCentre(s.Box, s.Rotation, s.FlipH, s.FlipV)
	t := *s
	t.SoftEdge = 0
	under, _ := b.paintEffects(&t, withoutText(frags, 0), nil, then(sm, m), false)
	return append(under, frags...)
}

// withoutText returns frags without their text runs, groups copied.
func withoutText(frags []compose.Fragment, depth int) []compose.Fragment {
	var out []compose.Fragment
	for _, f := range frags {
		switch f := f.(type) {
		case *compose.Text:
		case *compose.Group:
			if depth < maxRecolorDepth {
				g := *f
				g.Content = withoutText(f.Content, depth+1)
				out = append(out, &g)
			}
		default:
			out = append(out, f)
		}
	}
	return out
}

// paintEffects returns, in the coordinates of own, what paints under it
// for the effects of s, own being what s paints and text its text, both
// mapped to the slide by toSlide; and, when a soft edge drew own as a
// bitmap, that bitmap to paint instead of own.
func (b *builder) paintEffects(s *Shape, own, text []compose.Fragment, toSlide compose.Matrix, twice bool) ([]compose.Fragment, []compose.Fragment) {
	back, ok := invert(toSlide)
	if !ok {
		return nil, nil
	}
	sil := silhouetteOf(own, toSlide)
	if sil == nil {
		if s.Shadow != nil && s.Shadow.Opacity > 0 && len(text) > 0 && !(s.Shadow.Blur > 0) {
			return group(back, b.hardShadow(s.Shadow, nil, text, toSlide, compose.Rect{})), nil
		}
		if s.Shadow != nil && s.Shadow.Blur > 0 && len(text) > 0 {
			b.warn(CodeEffect, "the shadow of the text of "+quote(s.Name)+" is not drawn")
		}
		return nil, nil
	}
	sw, sh := b.slideSize()
	p := &effectPlan{s: s, sil: sil, area: compose.Rect{Width: sw, Height: sh}, twice: twice}
	var top []compose.Fragment
	var drawn []compose.Fragment
	if s.SoftEdge > 0 {
		if img := p.softEdge(); img != nil {
			drawn = []compose.Fragment{img}
		} else {
			b.warn(CodeEffect, "the soft edge of "+quote(s.Name)+" is not drawn: its fill is not a single colour")
		}
	}
	var glows []compose.Fragment
	if g := s.Glow; g != nil && g.Radius > 0 && g.Opacity > 0 {
		if twice {
			if img := p.glow(true); img != nil {
				glows = append(glows, img)
			}
		}
		if img := p.glow(false); img != nil {
			glows = append(glows, img)
		}
	}
	if sh := s.Shadow; sh != nil && sh.Opacity > 0 {
		if sh.Blur > 0 {
			if img := p.shadow(); img != nil {
				top = append(top, img)
			}
			if len(text) > 0 {
				b.warn(CodeEffect, "the shadow of the text of "+quote(s.Name)+" is not drawn")
			}
		} else {
			hard := own
			if drawn != nil {
				hard = nil
			}
			top = append(top, b.hardShadow(sh, glows, append(append([]compose.Fragment(nil), hard...), text...), toSlide, sil.bounds)...)
			if drawn != nil {
				top = append(top, p.recolorBitmap(drawn[0], sh)...)
			}
		}
	}
	top = append(top, glows...)
	if drawn != nil {
		return group(back, top), group(back, drawn)
	}
	return group(back, top), nil
}

// shadowMatrix returns the shadow's transform in slide coordinates: the
// scale about the centre of bounds, then the offsets (ECMA-376 Part 1,
// 20.1.8.49: sx, sy, dist and dir, which the reader resolves into the
// scale and the offsets). The shadow does not turn with a turned shape:
// its offsets are the slide's.
//
// # Corpus evidence
//
//   - pptx: two rectangles turned 30 degrees with a shadow 20 pt to the
//     right print the same shadow bitmap 20 pt to the right of each,
//     whether rotWithShape is 0 or 1.
func shadowMatrix(sh *Shadow, bounds compose.Rect) compose.Matrix {
	sx, sy := sh.ScaleX, sh.ScaleY
	if sx == 0 || math.IsNaN(sx) || math.IsInf(sx, 0) {
		sx = 1
	}
	if sy == 0 || math.IsNaN(sy) || math.IsInf(sy, 0) {
		sy = 1
	}
	cx, cy := bounds.X+bounds.Width/2, bounds.Y+bounds.Height/2
	m := then(translate(-cx, -cy), compose.Matrix{A: sx, D: sy})
	return then(m, translate(cx+sh.OffsetX, cy+sh.OffsetY))
}

// hardShadow returns a hard shadow: a copy of frags (mapped to the slide
// by toSlide) and of the glow bitmaps (in slide coordinates) in the
// shadow's colour, moved by the shadow's transform, at its opacity, in
// slide coordinates.
func (b *builder) hardShadow(sh *Shadow, glows, frags []compose.Fragment, toSlide compose.Matrix, bounds compose.Rect) []compose.Fragment {
	if bounds.Width == 0 && bounds.Height == 0 {
		bounds = transformRect(toSlide, compose.Rect{})
	}
	c := colorOf(sh.Color)
	c.A = 1
	var content []compose.Fragment
	for _, g := range glows {
		if img, ok := g.(*compose.Image); ok {
			if src, ok := img.Source.Image.(*image.NRGBA); ok {
				content = append(content, &compose.Image{Rect: img.Rect, Source: &compose.ImageSource{Image: retint(src, c)}})
			}
		}
	}
	content = append(content, group(toSlide, recolor(frags, c, 0))...)
	if len(content) == 0 {
		return nil
	}
	g := &compose.Group{Transform: shadowMatrix(sh, bounds), Opacity: min(sh.Opacity, 1), Content: content}
	return []compose.Fragment{g}
}

// recolorBitmap returns the hard shadow of a soft edge's bitmap in slide
// coordinates: its alpha in the shadow's colour, moved and faded.
func (p *effectPlan) recolorBitmap(f compose.Fragment, sh *Shadow) []compose.Fragment {
	img, ok := f.(*compose.Image)
	if !ok {
		return nil
	}
	src, ok := img.Source.Image.(*image.NRGBA)
	if !ok {
		return nil
	}
	c := colorOf(sh.Color)
	c.A = 1
	cp := &compose.Image{Rect: img.Rect, Source: &compose.ImageSource{Image: retint(src, c)}}
	return []compose.Fragment{&compose.Group{Transform: shadowMatrix(sh, p.sil.bounds), Opacity: min(sh.Opacity, 1), Content: []compose.Fragment{cp}}}
}

// retint returns img in the one colour c, its alpha kept.
func retint(img *image.NRGBA, c compose.Color) *image.NRGBA {
	a := image.NewAlpha(img.Bounds())
	for i := range a.Pix {
		a.Pix[i] = img.Pix[4*i+3]
	}
	return raster.Tint(a, c)
}

// maxRecolorDepth bounds the groups recolor descends. Anti-DoS guard, not
// spec-mandated: the builder nests groups maxGroupDepth deep, and a shape
// adds a few of its own.
const maxRecolorDepth = maxGroupDepth + 8

// recolor returns a copy of frags painted in the one colour c: fills,
// strokes and text in c, pictures as their rectangles filled with c.
func recolor(frags []compose.Fragment, c compose.Color, depth int) []compose.Fragment {
	var out []compose.Fragment
	for _, f := range frags {
		switch f := f.(type) {
		case *compose.Path:
			p := &compose.Path{Ops: f.Ops}
			if f.Fill != nil {
				p.Fill = &compose.Fill{Color: c, Rule: f.Fill.Rule}
			}
			if f.Stroke != nil {
				st := *f.Stroke
				st.Color = c
				p.Stroke = &st
			}
			out = append(out, p)
		case *compose.Text:
			t := *f
			t.Color = c
			t.Parent = nil
			out = append(out, &t)
		case *compose.Image:
			out = append(out, &compose.Path{Ops: rectOps(f.Rect), Fill: &compose.Fill{Color: c}})
		case *compose.Group:
			if depth < maxRecolorDepth {
				g := *f
				g.Content = recolor(f.Content, c, depth+1)
				out = append(out, &g)
			}
		}
	}
	return out
}

// glowMask returns a glow's coverage on g of the silhouette moved by tr:
// the silhouette made opaque wherever it paints, grown by half the radius
// and blurred over the other half, so it fades from the outline to
// nothing one radius out (LibreOffice's GlowPrimitive2D). The outer glow
// grows the outline of the first glow instead.
func (p *effectPlan) glowMask(g effectGrid, tr compose.Matrix, outer bool) *image.Alpha {
	half := g.radius(p.s.Glow.Radius) / 2
	base := p.sil.coverage(g, tr)
	raster.Threshold(base)
	inner := raster.Blur(raster.Dilate(base, half), raster.Tent(float64(half)))
	if outer {
		union(base, inner)
		raster.Threshold(base)
		inner = raster.Blur(raster.Dilate(base, half), raster.Tent(float64(half)))
	}
	return inner
}

// glow returns the bitmap of the shape's glow in slide coordinates, the
// outer one of a glow applied twice when outer is set, nil when it covers
// nothing.
//
// # Corpus evidence
//
//   - pptx: sd/shape-glow-effect's oval with a glow of 11 pt at 40% prints
//     a bitmap over its outline grown by 11 pt, at 102 of 255 inside the
//     outline, fading to 0 at the bitmap's edge over 30 pixels at 2.7
//     pixels a point.
func (p *effectPlan) glow(outer bool) compose.Fragment {
	gl := p.s.Glow
	reach := gl.Radius
	if outer {
		reach *= 2
	}
	box, ok := intersect(grow(p.sil.bounds, reach), grow(p.area, gl.Radius))
	if !ok {
		return nil
	}
	g, ok := newGrid(box)
	if !ok {
		return nil
	}
	mask := p.glowMask(g, compose.Identity, outer)
	c := colorOf(gl.Color)
	c.A = min(gl.Opacity, 1)
	return g.image(raster.Tint(mask, c))
}

// softMask returns a soft edge's coverage on g: the silhouette made
// opaque, shrunk by the radius and blurred over it, so it fades from the
// outline to opaque two radii in (LibreOffice's SoftEdgePrimitive2D), the
// fade squared when the soft edge applies twice, times the silhouette's
// own coverage.
func (p *effectPlan) softMask(g effectGrid, tr compose.Matrix) *image.Alpha {
	r := g.radius(p.s.SoftEdge)
	cov := p.sil.coverage(g, tr)
	base := image.NewAlpha(cov.Rect)
	copy(base.Pix, cov.Pix)
	raster.Threshold(base)
	fade := raster.Blur(raster.Erode(base, r), raster.Tent(float64(r)))
	if p.twice {
		raster.Multiply(fade, fade)
	}
	raster.Multiply(fade, cov)
	return fade
}

// softEdge returns the shape drawn as a bitmap whose alpha fades over the
// soft edge's radius, in slide coordinates; nil when the shape paints a
// shading or a picture, which the bitmap cannot redraw, or covers
// nothing.
//
// # Corpus evidence
//
//   - pptx: sd/shape-soft-edges's diamond and rectangle print as bitmaps
//     of their outlines' bounds, the line inside, the text on top; the
//     rectangle, 10 pt soft, is transparent 3 pt in and opaque 20 pt in.
func (p *effectPlan) softEdge() compose.Fragment {
	if !p.sil.flat {
		return nil
	}
	box, ok := intersect(p.sil.bounds, grow(p.area, 2*p.s.SoftEdge))
	if !ok {
		return nil
	}
	g, ok := newGrid(box)
	if !ok {
		return nil
	}
	img := image.NewNRGBA(image.Rect(0, 0, g.w, g.h))
	for i, path := range p.sil.paths {
		m := image.NewAlpha(img.Rect)
		raster.Draw(m, []raster.Path{path}, g.pixels())
		raster.Paint(img, m, p.sil.colors[i])
	}
	fade := p.softMask(g, compose.Identity)
	// The colours keep their own coverage; the fade replaces it with the
	// soft edge's, which already holds the coverage.
	for i, v := range fade.Pix {
		if img.Pix[4*i+3] != 0 {
			img.Pix[4*i+3] = v
		}
	}
	return g.image(img)
}

// shadow returns the bitmap of the shape's blurred shadow in slide
// coordinates: everything the shape paints but its text, glows and soft
// edge included, moved by the shadow's transform, blurred over its
// radius, in its colour at its opacity (LibreOffice's ShadowPrimitive2D:
// the content's own alpha, not made opaque, blurred by a stack blur of
// the radius); nil when it covers nothing.
//
// # Corpus evidence
//
//   - pptx: sd/shape-blur-effect's oval prints its shadow, blurred 11 pt,
//     as a bitmap over the oval's outline moved by the shadow's 34 pt and
//     grown by 11 pt, ramping from 0 to opaque over 22 pt about the
//     outline, 32 of 255 a quarter of the way.
//   - pptx: a rectangle reaching past the slide's right edge prints its
//     shadow's bitmap cut 6 pt, its blur, past the edge.
func (p *effectPlan) shadow() compose.Fragment {
	sh := p.s.Shadow
	tr := shadowMatrix(sh, p.sil.bounds)
	reach := p.sil.bounds
	if gl := p.s.Glow; gl != nil && gl.Radius > 0 && gl.Opacity > 0 {
		if p.twice {
			reach = grow(reach, 2*gl.Radius)
		} else {
			reach = grow(reach, gl.Radius)
		}
	}
	box, ok := intersect(grow(transformRect(tr, reach), sh.Blur), grow(p.area, sh.Blur))
	if !ok {
		return nil
	}
	g, ok := newGrid(box)
	if !ok {
		return nil
	}
	var mask *image.Alpha
	if p.s.SoftEdge > 0 && p.sil.flat {
		mask = p.softMask(g, tr)
	} else {
		mask = p.sil.coverage(g, tr)
	}
	if gl := p.s.Glow; gl != nil && gl.Radius > 0 && gl.Opacity > 0 {
		for _, outer := range []bool{false, true} {
			if outer && !p.twice {
				continue
			}
			m := p.glowMask(g, tr, outer)
			scale(m, gl.Opacity)
			union(mask, m)
		}
	}
	mask = raster.Blur(mask, raster.Tent(float64(g.radius(sh.Blur))))
	c := colorOf(sh.Color)
	c.A = min(sh.Opacity, 1)
	return g.image(raster.Tint(mask, c))
}

// scale multiplies the coverage of m by f, in place.
func scale(m *image.Alpha, f float64) {
	f = min(max(f, 0), 1)
	for i, v := range m.Pix {
		m.Pix[i] = uint8(math.Round(float64(v) * f)) //nolint:gosec // a coverage times a fraction is a coverage
	}
}
