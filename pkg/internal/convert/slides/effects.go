package slides

import (
	"context"
	"image"
	"image/color"
	"math"

	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// effectScale is the resolution in pixels a point an effect's bitmap is
// drawn at before its pixel budget applies: 720 pixels an inch, Impress's
// for a printout.
//
// # Corpus evidence
//
//   - probe, pptx: a 10 by 10 pt rectangle with a shadow blurred over 2 pt
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
//   - libreoffice/pptx: sd/shape-blur-effect's oval prints its 171.9 by 169.1 pt
//     shadow as a bitmap of 504 by 496 pixels, sd/effectOrder's glows as
//     626 by 399 and 660 by 379, sd-data/n819614's 56 shadows each within
//     a few hundred pixels of 250,000.
const effectPixels = 250000

// effectGrid is the pixel grid of an effect bitmap: the slide area box it
// covers, w by h pixels, and how many pixels a point Impress's filters
// take their radii at.
type effectGrid struct {
	box  typeset.Rect
	w, h int
	// unit is the pixels of a point the radii are measured in, the
	// unreduced resolution times the budget's reduction.
	unit float64
}

// newGrid returns the grid of an effect over the slide area box: each side
// at effectScale pixels a point, rounded up, the whole reduced to
// effectPixels when it holds more; false for an area too small to draw.
func newGrid(box typeset.Rect) (effectGrid, bool) {
	if !usable(box) {
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
func (g effectGrid) pixels() typeset.Matrix {
	sx, sy := float64(g.w)/g.box.Width, float64(g.h)/g.box.Height
	return typeset.Matrix{A: sx, D: sy, E: -g.box.X * sx, F: -g.box.Y * sy}
}

// place returns where the grid's surfaces sit on the slide.
func (g effectGrid) place() effect.Grid {
	return effect.Grid{X: g.box.X, Y: g.box.Y, Scale: float64(g.w) / g.box.Width}
}

// surface returns a transparent surface of the grid's size in format f,
// charged to the conversion's budget.
func (g effectGrid) surface(ctx context.Context, f effect.Format) (*effect.Surface, error) {
	return effect.New(ctx, f, g.w, g.h, g.place())
}

// radius returns the radius r points spans on the grid in whole pixels,
// as Impress's filters take it: r at effectScale rounded up, scaled by the
// budget's reduction, then truncated.
func (g effectGrid) radius(r float64) int {
	v := math.Ceil(r*effectScale) * g.unit / effectScale
	if math.IsNaN(v) || v < 1 {
		return 0
	}
	return int(min(v, effect.MaxKernelRadius)) // fits int: at most MaxKernelRadius
}

// tent blurs s in place by the tent of radius r pixels, Impress's stack
// blur, the pixels past the bitmap's edge repeating the edge's: a mask
// its caller clipped keeps its coverage at the cut.
func tent(ctx context.Context, s *effect.Surface, r int) error {
	k := effect.TentKernel(float64(r))
	return effect.Blur(ctx, s, k, k, effect.EdgeDuplicate)
}

// tinted returns the coverage cov in the one colour c as an image whose
// alpha is the coverage times c's alpha, colours straight, charged to the
// budget for [typeset.EncodeImage] to give back.
func tinted(ctx context.Context, cov *effect.Surface, c typeset.Color) (*image.NRGBA, error) {
	if err := typeset.ChargeImage(ctx, cov.W, cov.H, color.NRGBAModel); err != nil {
		return nil, err
	}
	return tint(cov.Pix, cov.W, cov.H, effect.ColorOf(c)), nil
}

// tint returns the w by h alpha samples in the one colour c as an image,
// each alpha times c's.
func tint(alpha []uint8, w, h int, c effect.Color) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i, v := range alpha[:w*h] {
		p := img.Pix[4*i : 4*i+4 : 4*i+4]
		p[0], p[1], p[2], p[3] = c.R, c.G, c.B, uint8((uint32(v)*uint32(c.A)+127)/255) //nolint:gosec // a byte times a fraction of 1
	}
	return img
}

// image returns the bitmap img placed over the grid's area, in slide
// coordinates, encoded at once so the bitmap is dropped; nil when ctx
// ends first.
func (g effectGrid) image(ctx context.Context, img image.Image) typeset.Fragment {
	src, err := typeset.EncodeImage(ctx, img)
	if err != nil {
		return nil
	}
	return &typeset.Image{Rect: g.box, Source: src}
}

// silhouette is what a shape paints, as the filters see it: its painted
// paths in slide coordinates and their bounds there.
type silhouette struct {
	paths  []effect.Path
	colors []typeset.Color
	// flat is false when a path paints a shading or a picture, which a
	// soft edge's bitmap cannot redraw in one colour.
	flat   bool
	bounds typeset.Rect
}

// silhouetteOf returns the silhouette of frags drawn under m (their
// coordinates to the slide's): each path filled or stroked, each picture
// its rectangle, each clipped group its clip. Text is left out: its
// glyphs have no outline here. Every path covers by the non-zero rule,
// its subpaths summed by direction, as Impress's silhouettes do.
func silhouetteOf(frags []typeset.Fragment, m typeset.Matrix) *silhouette {
	s := &silhouette{flat: true}
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	add := func(ops []typeset.PathOp, width float64, c typeset.Color, tr typeset.Matrix) {
		ops = mapOps(ops, tr)
		width *= math.Sqrt(math.Abs(tr.A*tr.D - tr.B*tr.C))
		hw := width / 2
		for _, op := range ops {
			n := 1
			if op.Kind == typeset.CurveTo {
				n = 3
			}
			if op.Kind == typeset.ClosePath {
				continue
			}
			for _, p := range op.P[:n] {
				x0, y0, x1, y1 = min(x0, p.X-hw), min(y0, p.Y-hw), max(x1, p.X+hw), max(y1, p.Y+hw)
			}
		}
		s.paths = append(s.paths, effect.Path{Ops: ops, Width: width})
		s.colors = append(s.colors, c)
	}
	var walk func(fs []typeset.Fragment, tr typeset.Matrix, depth int)
	walk = func(fs []typeset.Fragment, tr typeset.Matrix, depth int) {
		for _, f := range fs {
			switch f := f.(type) {
			case *typeset.Path:
				if f.Fill != nil {
					if f.Fill.Shading != nil {
						s.flat = false
					}
					add(f.Ops, 0, f.Fill.Color, tr)
				}
				if f.Stroke != nil && f.Stroke.Width > 0 {
					add(f.Ops, f.Stroke.Width, f.Stroke.Color, tr)
				}
			case *typeset.Image:
				s.flat = false
				add(rectOps(f.Rect), 0, typeset.Color{A: 1}, tr)
			case *typeset.Group:
				if depth < maxGroupDepth {
					t := f.Transform
					if t == (typeset.Matrix{}) {
						t = typeset.Identity
					}
					if f.Clip != nil {
						// A clipped fill, a picture or a gradient in its
						// shape, covers its clip.
						s.flat = false
						add(f.Clip.Ops, 0, typeset.Color{A: 1}, then(t, tr))
						continue
					}
					walk(f.Content, then(t, tr), depth+1)
				}
			}
		}
	}
	walk(frags, m, 0)
	w, h := x1-x0, y1-y0
	if len(s.paths) == 0 || !numeric.Positive(w) || !numeric.Positive(h) {
		return nil
	}
	s.bounds = typeset.Rect{X: x0, Y: y0, Width: w, Height: h}
	return s
}

// mapOps returns ops mapped through m.
func mapOps(ops []typeset.PathOp, m typeset.Matrix) []typeset.PathOp {
	out := make([]typeset.PathOp, len(ops))
	for i, op := range ops {
		out[i].Kind = op.Kind
		for j, p := range op.P {
			x, y := apply(m, p.X, p.Y)
			out[i].P[j] = typeset.Point{X: x, Y: y}
		}
	}
	return out
}

// coverage returns the coverage on g of paths, moved by tr in slide
// coordinates first.
func coverage(ctx context.Context, g effectGrid, paths []effect.Path, tr typeset.Matrix) (*effect.Surface, error) {
	cov, err := g.surface(ctx, effect.Coverage)
	if err != nil {
		return nil, err
	}
	if err := effect.Fill(ctx, cov, paths, then(tr, g.pixels())); err != nil {
		cov.Release()
		return nil, err
	}
	return cov, nil
}

// coverage returns the silhouette's coverage on g, its paths moved by tr
// in slide coordinates first.
func (s *silhouette) coverage(ctx context.Context, g effectGrid, tr typeset.Matrix) (*effect.Surface, error) {
	return coverage(ctx, g, s.paths, tr)
}

// grow returns r grown by d on every side.
func grow(r typeset.Rect, d float64) typeset.Rect {
	return typeset.Rect{X: r.X - d, Y: r.Y - d, Width: r.Width + 2*d, Height: r.Height + 2*d}
}

// intersect returns the overlap of a and b, false for none.
func intersect(a, b typeset.Rect) (typeset.Rect, bool) {
	x0, y0 := max(a.X, b.X), max(a.Y, b.Y)
	x1, y1 := min(a.X+a.Width, b.X+b.Width), min(a.Y+a.Height, b.Y+b.Height)
	w, h := x1-x0, y1-y0
	if !numeric.Positive(w) || !numeric.Positive(h) {
		return typeset.Rect{}, false
	}
	return typeset.Rect{X: x0, Y: y0, Width: w, Height: h}, true
}

// minDeterminant is the smallest determinant a transform inverts with:
// one closer to zero flattens the shape to a line, whose inverse is not
// finite.
const minDeterminant = 1e-12

// invert returns the inverse of m, false when m is singular.
func invert(m typeset.Matrix) (typeset.Matrix, bool) {
	det := m.A*m.D - m.B*m.C
	if !numeric.Finite(det) || math.Abs(det) <= minDeterminant {
		return typeset.Matrix{}, false
	}
	return typeset.Matrix{
		A: m.D / det, B: -m.B / det, C: -m.C / det, D: m.A / det,
		E: (m.C*m.F - m.D*m.E) / det, F: (m.B*m.E - m.A*m.F) / det,
	}, true
}

// effectPlan is the effects of one shape: what paints them, in slide
// coordinates, and how many times its glow and soft edge apply.
type effectPlan struct {
	s     *Shape
	sil   *silhouette
	area  typeset.Rect
	twice bool
	// page counts the pixels of the slide's effect bitmaps; capped warns
	// once it refuses one.
	page   *effect.Page
	capped func()
}

// grid returns the grid of an effect over box, as [newGrid] does; false
// for an area too small to draw or a bitmap past the slide's pixel cap
// ([effect.MaxPagePixels]), which warns that the effect is left out.
func (p *effectPlan) grid(box typeset.Rect) (effectGrid, bool) {
	g, ok := newGrid(box)
	if !ok {
		return effectGrid{}, false
	}
	if !p.page.Take(effect.Resolution{W: g.w, H: g.h}) {
		p.capped()
		return effectGrid{}, false
	}
	return g, true
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
//   - libreoffice/pptx: sd/effectOrder's rectangle prints two glow bitmaps, over its
//     box grown by one and by two glow radii, the outer one opaque from
//     one radius out; a rectangle, a text box and a turned rectangle with
//     a glow print two, the presets roundRect, flowChartProcess,
//     snip1Rect, triangle, ellipse, diamond and a custom rectangle one.
//   - libreoffice/pptx: sd/shape-soft-edges's rectangle fades as the square of a
//     single soft edge: 64 of 255 at the radius, 128 a quarter radius
//     further in; its diamond fades once.
//   - probe, odp: a draw:custom-shape of type rectangle with a glow prints two
//     glow bitmaps, a draw:rect one.
func effectsTwice(s *Shape) bool {
	g := s.Geometry
	return s.Kind == KindGeometry && g != nil && g.Preset == "rect" && g.Custom == nil
}

// effects returns what paints under the geometry shape s for its effects
// (ECMA-376 Part 1, 20.1.8.45 outerShdw, 20.1.8.32 glow, 20.1.8.53
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
//   - libreoffice/pptx: sd/effectOrder's rectangle with a glow and a shadow prints the
//     shadow (a bitmap of the shape and both glows, at 40% opacity), the
//     outer glow, the glow, then the rectangle.
func (b *builder) effects(s *Shape, paths []shape.Path, box typeset.Rect, m typeset.Matrix, txt *textResult) ([]typeset.Fragment, []shape.Path) {
	if s.Shadow == nil && s.Glow == nil && !numeric.Positive(s.SoftEdge) {
		return nil, paths
	}
	s = turnedShadow(s)
	sm := skewedAboutCentre(box, s.Rotation, s.SkewX, s.FlipH, s.FlipV)
	toSlide := then(sm, m)
	var own []typeset.Fragment
	for _, p := range paths {
		if p.Fill {
			own = append(own, b.fill(&s.Fill, box, p.Ops, p.Mode)...)
		}
		if p.Stroke {
			own = append(own, b.stroke(&s.Line, p.Ops)...)
		}
	}
	var text []typeset.Fragment
	if txt != nil {
		rot := s.Rotation
		if s.FlipV {
			rot += 180
		}
		text = group(then(skewedAboutCentre(box, rot, s.SkewX, false, false), invertOr(sm)), txt.frags)
	}
	under, drawn := b.paintEffects(s, own, text, toSlide, effectsTwice(s))
	if drawn != nil {
		return append(under, drawn...), nil
	}
	return under, paths
}

// invertOr returns the inverse of m, the identity when m is singular.
func invertOr(m typeset.Matrix) typeset.Matrix {
	if inv, ok := invert(m); ok {
		return inv
	}
	return typeset.Identity
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
//   - probe, pptx: a picture with a shadow blurred over 6 pt prints its
//     shadow as a bitmap of its box grown by 6 pt, opaque inside.
//   - libreoffice/pptx: oox/table-shadow prints its table's borders again
//     10 pt down and right in the shadow's red, its text once.
func (b *builder) contentEffects(s *Shape, frags []typeset.Fragment, m typeset.Matrix) []typeset.Fragment {
	if s.Shadow == nil && s.Glow == nil && !numeric.Positive(s.SoftEdge) {
		return frags
	}
	s = turnedShadow(s)
	if s.SoftEdge > 0 {
		b.warn(warn.ContentUnsupported, warn.Approximated, "the soft edge of "+quote(s.Name)+" is not drawn")
	}
	sm := skewedAboutCentre(s.Box, s.Rotation, s.SkewX, s.FlipH, s.FlipV)
	t := *s
	t.SoftEdge = 0
	under, _ := b.paintEffects(&t, withoutText(frags, 0), nil, then(sm, m), false)
	return append(under, frags...)
}

// withoutText returns frags without their text runs, groups copied.
func withoutText(frags []typeset.Fragment, depth int) []typeset.Fragment {
	var out []typeset.Fragment
	for _, f := range frags {
		switch f := f.(type) {
		case *typeset.Text:
		case *typeset.Group:
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
func (b *builder) paintEffects(s *Shape, own, text []typeset.Fragment, toSlide typeset.Matrix, twice bool) ([]typeset.Fragment, []typeset.Fragment) {
	back, ok := invert(toSlide)
	if !ok {
		return nil, nil
	}
	sil := silhouetteOf(own, toSlide)
	if sil == nil {
		if s.Shadow != nil && s.Shadow.Opacity > 0 && len(text) > 0 && !numeric.Positive(s.Shadow.Blur) {
			return group(back, b.hardShadow(b.ctx, s.Shadow, nil, text, toSlide, typeset.Rect{})), nil
		}
		if s.Shadow != nil && s.Shadow.Blur > 0 && len(text) > 0 {
			b.warn(warn.ContentUnsupported, warn.Approximated, "the shadow of the text of "+quote(s.Name)+" is not drawn")
		}
		return nil, nil
	}
	sw, sh := b.slideSize()
	p := &effectPlan{s: s, sil: sil, area: typeset.Rect{Width: sw, Height: sh}, twice: twice, page: &b.effectPixels, capped: func() {
		b.warn(warn.LimitReached, warn.Omitted, "effects past the pixels a slide's effect bitmaps may hold are not drawn")
	}}
	var top []typeset.Fragment
	var drawn []typeset.Fragment
	if s.SoftEdge > 0 {
		if img := p.softEdge(b.ctx); img != nil {
			drawn = []typeset.Fragment{img}
		} else {
			b.warn(warn.ContentUnsupported, warn.Approximated, "the soft edge of "+quote(s.Name)+" is not drawn: its fill is not a single colour")
		}
	}
	var glows []typeset.Fragment
	if g := s.Glow; g != nil && g.Radius > 0 && g.Opacity > 0 {
		if twice {
			if img := p.glow(b.ctx, true); img != nil {
				glows = append(glows, img)
			}
		}
		if img := p.glow(b.ctx, false); img != nil {
			glows = append(glows, img)
		}
	}
	if sh := s.Shadow; sh != nil && sh.Opacity > 0 {
		if sh.Blur > 0 {
			if img := p.shadow(b.ctx); img != nil {
				top = append(top, img)
			}
			if len(text) > 0 {
				b.warn(warn.ContentUnsupported, warn.Approximated, "the shadow of the text of "+quote(s.Name)+" is not drawn")
			}
		} else {
			hard := own
			if drawn != nil {
				hard = nil
			}
			top = append(top, b.hardShadow(b.ctx, sh, glows, append(append([]typeset.Fragment(nil), hard...), text...), toSlide, sil.bounds)...)
			if drawn != nil {
				top = append(top, p.recolorBitmap(b.ctx, drawn[0], sh)...)
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
// 20.1.8.45 outerShdw: sx, sy, dist and dir, which the reader resolves
// into the scale and the offsets, turned with the shape by
// [turnedShadow] when the shadow turns with it).
//
// # Corpus evidence
//
//   - probe, pptx: in Impress, two rectangles turned 30 degrees with a
//     shadow 20 pt to the right print the same shadow bitmap 20 pt to the
//     right of each, whether rotWithShape is 0 or 1.
func shadowMatrix(sh *Shadow, bounds typeset.Rect) typeset.Matrix {
	sx, sy := sh.ScaleX, sh.ScaleY
	if sx == 0 || !numeric.Finite(sx) {
		sx = 1
	}
	if sy == 0 || !numeric.Finite(sy) {
		sy = 1
	}
	cx, cy := bounds.X+bounds.Width/2, bounds.Y+bounds.Height/2
	m := then(translate(-cx, -cy), typeset.Matrix{A: sx, D: sy})
	return then(m, translate(cx+sh.OffsetX, cy+sh.OffsetY))
}

// turnedShadow returns s with its shadow's offsets turned clockwise by
// the shape's rotation when the shadow turns with the shape (ECMA-376
// Part 1, 20.1.8.45 outerShdw rotWithShape: "whether the shadow rotates
// with the shape if the shape is rotated"), else s itself. Impress keeps
// the slide's offsets either way; PowerPoint, the judge of PresentationML,
// turns them.
//
// # Corpus evidence
//
//   - litho/pptx: effects-shadow prints in PowerPoint the shadow of its
//     right arrow turned a quarter turn, 15 pt toward 0 degrees with
//     rotWithShape 1, 15 pt below the arrow.
func turnedShadow(s *Shape) *Shape {
	sh := s.Shadow
	if sh == nil || !sh.RotateWithShape || s.Rotation == 0 || !numeric.Finite(s.Rotation) {
		return s
	}
	sin, cos := math.Sincos(s.Rotation * math.Pi / 180)
	t := *sh
	t.OffsetX, t.OffsetY = sh.OffsetX*cos-sh.OffsetY*sin, sh.OffsetX*sin+sh.OffsetY*cos
	c := *s
	c.Shadow = &t
	return &c
}

// hardShadow returns a hard shadow: a copy of frags (mapped to the slide
// by toSlide) and of the glow bitmaps (in slide coordinates) in the
// shadow's colour, moved by the shadow's transform, at its opacity, in
// slide coordinates.
func (b *builder) hardShadow(ctx context.Context, sh *Shadow, glows, frags []typeset.Fragment, toSlide typeset.Matrix, bounds typeset.Rect) []typeset.Fragment {
	if bounds.Width == 0 && bounds.Height == 0 {
		bounds = transformRect(toSlide, typeset.Rect{})
	}
	c := colorOf(sh.Color)
	c.A = 1
	var content []typeset.Fragment
	for _, g := range glows {
		if img, ok := g.(*typeset.Image); ok {
			if src := retint(ctx, img.Source, c); src != nil {
				content = append(content, &typeset.Image{Rect: img.Rect, Source: src})
			}
		}
	}
	content = append(content, group(toSlide, recolor(frags, c, 0))...)
	if len(content) == 0 {
		return nil
	}
	g := &typeset.Group{Transform: shadowMatrix(sh, bounds), Opacity: min(sh.Opacity, 1), Content: content}
	return []typeset.Fragment{g}
}

// recolorBitmap returns the hard shadow of a soft edge's bitmap in slide
// coordinates: its alpha in the shadow's colour, moved and faded.
func (p *effectPlan) recolorBitmap(ctx context.Context, f typeset.Fragment, sh *Shadow) []typeset.Fragment {
	img, ok := f.(*typeset.Image)
	if !ok {
		return nil
	}
	c := colorOf(sh.Color)
	c.A = 1
	src := retint(ctx, img.Source, c)
	if src == nil {
		return nil
	}
	cp := &typeset.Image{Rect: img.Rect, Source: src}
	return []typeset.Fragment{&typeset.Group{Transform: shadowMatrix(sh, p.sil.bounds), Opacity: min(sh.Opacity, 1), Content: []typeset.Fragment{cp}}}
}

// retint returns the effect bitmap src in the one colour c, its alpha
// kept; nil for a picture that is not an effect bitmap or when ctx ends
// first.
func retint(ctx context.Context, src *typeset.ImageSource, c typeset.Color) *typeset.ImageSource {
	if src == nil || len(src.JPEG) > 0 || src.Gray {
		return nil
	}
	pixels, err := src.Decode(ctx)
	if err != nil {
		return nil
	}
	defer typeset.ReleaseImage(ctx, pixels)
	img, ok := pixels.(*image.NRGBA)
	if !ok {
		return nil
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	if err := typeset.ChargeImage(ctx, w, h, color.NRGBAModel); err != nil {
		return nil
	}
	a := make([]uint8, w*h)
	for i := range a {
		a[i] = img.Pix[4*i+3]
	}
	out, err := typeset.EncodeImage(ctx, tint(a, w, h, effect.ColorOf(c)))
	if err != nil {
		return nil
	}
	return out
}

// maxRecolorDepth bounds the groups recolor descends. Anti-DoS guard, not
// spec-mandated: the builder nests groups maxGroupDepth deep, and a shape
// adds a few of its own.
const maxRecolorDepth = maxGroupDepth + 8

// recolor returns a copy of frags painted in the one colour c: fills,
// strokes and text in c, pictures as their rectangles filled with c.
func recolor(frags []typeset.Fragment, c typeset.Color, depth int) []typeset.Fragment {
	var out []typeset.Fragment
	for _, f := range frags {
		switch f := f.(type) {
		case *typeset.Path:
			p := &typeset.Path{Ops: f.Ops}
			if f.Fill != nil {
				p.Fill = &typeset.Fill{Color: c, Rule: f.Fill.Rule}
			}
			if f.Stroke != nil {
				st := *f.Stroke
				st.Color = c
				p.Stroke = &st
			}
			out = append(out, p)
		case *typeset.Text:
			t := *f
			t.Color = c
			t.Parent = nil
			out = append(out, &t)
		case *typeset.Image:
			out = append(out, &typeset.Path{Ops: rectOps(f.Rect), Fill: &typeset.Fill{Color: c}})
		case *typeset.Group:
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
func (p *effectPlan) glowMask(ctx context.Context, g effectGrid, tr typeset.Matrix, outer bool) (*effect.Surface, error) {
	half := g.radius(p.s.Glow.Radius) / 2
	base, err := p.sil.coverage(ctx, g, tr)
	if err != nil {
		return nil, err
	}
	defer base.Release()
	if err := effect.Threshold(base, 1); err != nil {
		return nil, err
	}
	inner, err := grownBlur(ctx, base, half)
	if err != nil || !outer {
		return inner, err
	}
	defer inner.Release()
	if err := effect.Composite(ctx, base, inner, effect.Over); err != nil {
		return nil, err
	}
	if err := effect.Threshold(base, 1); err != nil {
		return nil, err
	}
	return grownBlur(ctx, base, half)
}

// grownBlur returns a copy of the coverage cov grown by r pixels and
// blurred by the tent of radius r.
func grownBlur(ctx context.Context, cov *effect.Surface, r int) (*effect.Surface, error) {
	out, err := cov.Clone(ctx)
	if err != nil {
		return nil, err
	}
	if err := effect.Dilate(ctx, out, r, r); err != nil {
		out.Release()
		return nil, err
	}
	if err := tent(ctx, out, r); err != nil {
		out.Release()
		return nil, err
	}
	return out, nil
}

// glow returns the bitmap of the shape's glow in slide coordinates, the
// outer one of a glow applied twice when outer is set, nil when it covers
// nothing.
//
// # Corpus evidence
//
//   - libreoffice/pptx: sd/shape-glow-effect's oval with a glow of 11 pt at 40% prints
//     a bitmap over its outline grown by 11 pt, at 102 of 255 inside the
//     outline, fading to 0 at the bitmap's edge over 30 pixels at 2.7
//     pixels a point.
func (p *effectPlan) glow(ctx context.Context, outer bool) typeset.Fragment {
	gl := p.s.Glow
	reach := gl.Radius
	if outer {
		reach *= 2
	}
	box, ok := intersect(grow(p.sil.bounds, reach), grow(p.area, gl.Radius))
	if !ok {
		return nil
	}
	g, ok := p.grid(box)
	if !ok {
		return nil
	}
	mask, err := p.glowMask(ctx, g, typeset.Identity, outer)
	if err != nil {
		return nil
	}
	defer mask.Release()
	c := colorOf(gl.Color)
	c.A = min(gl.Opacity, 1)
	img, err := tinted(ctx, mask, c)
	if err != nil {
		return nil
	}
	return g.image(ctx, img)
}

// softMask returns a soft edge's coverage on g: the silhouette made
// opaque, shrunk by the radius and blurred over it, so it fades from the
// outline to opaque two radii in (LibreOffice's SoftEdgePrimitive2D), the
// fade squared when the soft edge applies twice, times the silhouette's
// own coverage.
func (p *effectPlan) softMask(ctx context.Context, g effectGrid, tr typeset.Matrix) (*effect.Surface, error) {
	r := g.radius(p.s.SoftEdge)
	cov, err := p.sil.coverage(ctx, g, tr)
	if err != nil {
		return nil, err
	}
	defer cov.Release()
	fade, err := cov.Clone(ctx)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			fade.Release()
		}
	}()
	if err := effect.Threshold(fade, 1); err != nil {
		return nil, err
	}
	if err := effect.Erode(ctx, fade, r, r); err != nil {
		return nil, err
	}
	if err := tent(ctx, fade, r); err != nil {
		return nil, err
	}
	if p.twice {
		if err := square(ctx, fade); err != nil {
			return nil, err
		}
	}
	// The coverage In the fade: the fade times the coverage.
	if err := effect.Composite(ctx, fade, cov, effect.In); err != nil {
		return nil, err
	}
	ok = true
	return fade, nil
}

// square multiplies the coverage of s by itself, in place.
func square(ctx context.Context, s *effect.Surface) error {
	c, err := s.Clone(ctx)
	if err != nil {
		return err
	}
	defer c.Release()
	return effect.Composite(ctx, s, c, effect.In)
}

// softEdge returns the shape drawn as a bitmap whose alpha fades over the
// soft edge's radius, in slide coordinates; nil when the shape paints a
// shading or a picture, which the bitmap cannot redraw, or covers
// nothing.
//
// # Corpus evidence
//
//   - libreoffice/pptx: sd/shape-soft-edges's diamond and rectangle print as bitmaps
//     of their outlines' bounds, the line inside, the text on top; the
//     rectangle, 10 pt soft, is transparent 3 pt in and opaque 20 pt in.
func (p *effectPlan) softEdge(ctx context.Context) typeset.Fragment {
	if !p.sil.flat {
		return nil
	}
	box, ok := intersect(p.sil.bounds, grow(p.area, 2*p.s.SoftEdge))
	if !ok {
		return nil
	}
	g, ok := p.grid(box)
	if !ok {
		return nil
	}
	img, err := p.softEdgeImage(ctx, g)
	if err != nil {
		return nil
	}
	return g.image(ctx, img)
}

// softEdgeImage paints the silhouette's paths in their colours on g, each
// over the ones before, and gives each painted pixel the soft edge's
// alpha in place of its own, which the soft edge's coverage already holds.
func (p *effectPlan) softEdgeImage(ctx context.Context, g effectGrid) (*image.NRGBA, error) {
	paint, err := g.surface(ctx, effect.RGBA)
	if err != nil {
		return nil, err
	}
	defer paint.Release()
	for i, path := range p.sil.paths {
		cov, err := coverage(ctx, g, []effect.Path{path}, typeset.Identity)
		if err != nil {
			return nil, err
		}
		layer, err := effect.Tint(ctx, cov, effect.ColorOf(p.sil.colors[i]))
		cov.Release()
		if err != nil {
			return nil, err
		}
		err = effect.Composite(ctx, paint, layer, effect.Over)
		layer.Release()
		if err != nil {
			return nil, err
		}
	}
	fade, err := p.softMask(ctx, g, typeset.Identity)
	if err != nil {
		return nil, err
	}
	defer fade.Release()
	if err := typeset.ChargeImage(ctx, g.w, g.h, color.NRGBAModel); err != nil {
		return nil, err
	}
	img := image.NewNRGBA(image.Rect(0, 0, g.w, g.h))
	for i, v := range fade.Pix {
		px, out := paint.Pix[4*i:4*i+4:4*i+4], img.Pix[4*i:4*i+4:4*i+4]
		if a := px[3]; a != 0 {
			out[0], out[1], out[2], out[3] = unpremultiply(px[0], a), unpremultiply(px[1], a), unpremultiply(px[2], a), v
		}
	}
	return img, nil
}

// unpremultiply returns the colour channel c of a pixel premultiplied by
// its alpha a, not zero, as a straight channel, rounded.
func unpremultiply(c, a uint8) uint8 {
	return uint8(min((uint32(c)*255+uint32(a)/2)/uint32(a), 255)) //nolint:gosec // clamped to a byte
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
//   - libreoffice/pptx: sd/shape-blur-effect's oval prints its shadow, blurred 11 pt,
//     as a bitmap over the oval's outline moved by the shadow's 34 pt and
//     grown by 11 pt, ramping from 0 to opaque over 22 pt about the
//     outline, 32 of 255 a quarter of the way.
//   - probe, pptx: a rectangle reaching past the slide's right edge prints its
//     shadow's bitmap cut 6 pt, its blur, past the edge.
func (p *effectPlan) shadow(ctx context.Context) typeset.Fragment {
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
	g, ok := p.grid(box)
	if !ok {
		return nil
	}
	mask, err := p.shadowMask(ctx, g, tr)
	if err != nil {
		return nil
	}
	defer mask.Release()
	c := colorOf(sh.Color)
	c.A = min(sh.Opacity, 1)
	img, err := tinted(ctx, mask, c)
	if err != nil {
		return nil
	}
	return g.image(ctx, img)
}

// shadowMask returns the coverage of the shadow on g, the silhouette moved
// by tr: the soft edge's coverage, or the silhouette's, with the glows at
// their opacity over it, blurred by the shadow's radius.
func (p *effectPlan) shadowMask(ctx context.Context, g effectGrid, tr typeset.Matrix) (*effect.Surface, error) {
	var mask *effect.Surface
	var err error
	if p.s.SoftEdge > 0 && p.sil.flat {
		mask, err = p.softMask(ctx, g, tr)
	} else {
		mask, err = p.sil.coverage(ctx, g, tr)
	}
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*effect.Surface, error) {
		mask.Release()
		return nil, err
	}
	if gl := p.s.Glow; gl != nil && gl.Radius > 0 && gl.Opacity > 0 {
		lut := effect.OpacityTransfer(gl.Opacity).Table()
		for _, outer := range []bool{false, true} {
			if outer && !p.twice {
				continue
			}
			m, err := p.glowMask(ctx, g, tr, outer)
			if err != nil {
				return fail(err)
			}
			for i, v := range m.Pix {
				m.Pix[i] = lut[v]
			}
			err = effect.Composite(ctx, mask, m, effect.Over)
			m.Release()
			if err != nil {
				return fail(err)
			}
		}
	}
	if err := tent(ctx, mask, g.radius(p.s.Shadow.Blur)); err != nil {
		return fail(err)
	}
	return mask, nil
}
