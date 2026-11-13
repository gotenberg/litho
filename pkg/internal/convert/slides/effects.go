package slides

import (
	"context"
	"image"
	"image/color"
	"math"

	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/officefx"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// tentSigma returns the standard deviation of a tent of radius r, in r's
// unit: a triangle of half-width r has variance r^2/6. It sizes the grid
// of an effect Impress blurs by a tent, as [effect.Plan] takes a blur by
// its deviation.
func tentSigma(r float64) float64 {
	return r / math.Sqrt(6)
}

// imageOn returns the bitmap img placed over g's area, in slide
// coordinates, encoded at once so the bitmap is dropped; nil when ctx
// ends first.
func imageOn(ctx context.Context, g officefx.Grid, img image.Image) typeset.Fragment {
	f, err := g.Image(ctx, img)
	if err != nil {
		return nil
	}
	return f
}

// silhouette is what a shape paints, as the filters see it: its painted
// paths in slide coordinates and their bounds there.
type silhouette struct {
	paths  []effect.Path
	colors []typeset.Color
	// flat is false when a path paints a shading or a picture, which a
	// soft edge's bitmap cannot redraw in one colour.
	flat bool
	// filled is true when a path is filled or a picture or a clip covers
	// an area, false for lines alone.
	filled bool
	bounds typeset.Rect
}

// silhouetteOf returns the silhouette of frags drawn under m (their
// coordinates to the slide's): each path filled or stroked, each picture
// its rectangle, each clipped group its clip. Text is left out: its
// glyphs have no outline here. Every path covers by the non-zero rule,
// its subpaths summed by direction, as Impress's silhouettes do.
func silhouetteOf(frags []typeset.Fragment, m typeset.Matrix) *silhouette {
	return silhouetteLined(frags, m, 1)
}

// silhouetteLined returns the silhouette of frags as [silhouetteOf] does,
// each stroke at share times its width.
func silhouetteLined(frags []typeset.Fragment, m typeset.Matrix, share float64) *silhouette {
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
					s.filled = true
					add(f.Ops, 0, f.Fill.Color, tr)
				}
				if f.Stroke != nil && f.Stroke.Width > 0 {
					add(f.Ops, f.Stroke.Width*share, f.Stroke.Color, tr)
				}
			case *typeset.Image:
				s.flat, s.filled = false, true
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
						s.flat, s.filled = false, true
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

// coverage returns the silhouette's coverage on g, its paths moved by tr
// in slide coordinates first.
func (s *silhouette) coverage(ctx context.Context, g officefx.Grid, tr typeset.Matrix) (*effect.Surface, error) {
	return g.Coverage(ctx, s.paths, tr)
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
// coordinates, whose rules draw them and how many times its glow and soft
// edge apply.
type effectPlan struct {
	s   *Shape
	sil *silhouette
	// shadowSil is the silhouette a blurred shadow is drawn from:
	// [officefx.Rules.ShadowLine] of its lines on PowerPoint's rules, sil on
	// Impress's.
	shadowSil *silhouette
	area      typeset.Rect
	impress   bool
	twice     bool
	// page counts the pixels of the slide's effect bitmaps; capped warns
	// once it refuses one, reduced once the engine draws one coarser than
	// its rule.
	page    *effect.Page
	capped  func()
	reduced func()
}

// grid returns the grid of an effect over box whose blur has standard
// deviation sigma points, after growing or shrinking by morph points, by
// the engine's rule on both programs' rules ([officefx.PlanGrid]). It returns false
// for an area too small to draw or a bitmap past the slide's pixel cap
// ([effect.MaxPagePixels]), which warns that the effect is left out.
func (p *effectPlan) grid(box typeset.Rect, sigma, morph float64) (officefx.Grid, bool) {
	g, reduced, ok := officefx.PlanGrid(box, sigma, morph)
	if !ok {
		return officefx.Grid{}, false
	}
	if reduced {
		p.reduced()
	}
	if !p.page.Take(effect.Resolution{W: g.W, H: g.H}) {
		p.capped()
		return officefx.Grid{}, false
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
//   - probe, pptx: in Impress, sd/effectOrder's rectangle prints two glow bitmaps, over its
//     box grown by one and by two glow radii, the outer one opaque from
//     one radius out; a rectangle, a text box and a turned rectangle with
//     a glow print two, the presets roundRect, flowChartProcess,
//     snip1Rect, triangle, ellipse, diamond and a custom rectangle one.
//   - probe, pptx: in Impress, sd/shape-soft-edges's rectangle fades as the square of a
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
// to the slide's; the paths the builder still fills and strokes: none
// when a soft edge has drawn them already; and what paints over the
// shape's fills and under its lines: its inner shadow (ECMA-376 Part 1,
// 20.1.8.40 innerShdw).
//
// Both programs paint, from the bottom: the shadow of everything above it,
// the glows, then the shape with its soft edge, then its text. A blurred
// shadow, a glow and a soft edge are bitmaps over the slide area they
// reach, clipped to the slide grown by their radius; a hard shadow is a
// copy of the shape's fill, line and text in the shadow's colour. Impress
// blurs the bitmaps by a tent, PowerPoint by a Gaussian; litho draws both
// at the resolution [effect.Plan] gives the blur, not at either program's
// own (Impress's 720 pixels an inch within 250,000 pixels a bitmap,
// PowerPoint's 100 pixels an inch): one rule for every format.
//
// # Corpus evidence
//
//   - probe, pptx: in Impress, sd/effectOrder's rectangle with a glow and a shadow prints the
//     shadow (a bitmap of the shape and both glows, at 40% opacity), the
//     outer glow, the glow, then the rectangle.
func (b *builder) effects(s *Shape, paths []shape.Path, box typeset.Rect, m typeset.Matrix, txt *textResult) ([]typeset.Fragment, []shape.Path, []typeset.Fragment) {
	if s.Shadow == nil && s.Glow == nil && !numeric.Positive(s.SoftEdge) && s.InnerShadow == nil && s.Reflection == nil && s.Blur == nil && s.FillOverlay == nil {
		return nil, paths, nil
	}
	s = turnedShadow(s)
	sm := skewedAboutCentre(box, s.Rotation, s.SkewX, s.FlipH, s.FlipV)
	toSlide := then(sm, m)
	var own, fills []typeset.Fragment
	for _, p := range paths {
		if p.Fill {
			f := b.fill(&s.Fill, box, p.Ops, p.Mode)
			own = append(own, f...)
			fills = append(fills, f...)
		}
		if p.Stroke {
			own = append(own, b.stroke(&s.Line, p.Ops)...)
		}
	}
	inner := b.innerShadow(s, fills, toSlide)
	var text []typeset.Fragment
	if txt != nil {
		rot := s.Rotation
		if s.FlipV {
			rot += 180
		}
		text = group(then(skewedAboutCentre(box, rot, s.SkewX, false, false), invertOr(sm)), txt.frags)
	}
	under, drawn := b.paintEffects(s, own, text, toSlide, b.doc.Effects == EffectsImpress && effectsTwice(s))
	if drawn != nil {
		return append(append(under, drawn...), inner...), nil, nil
	}
	return under, paths, inner
}

// invertOr returns the inverse of m, the identity when m is singular.
func invertOr(m typeset.Matrix) typeset.Matrix {
	if inv, ok := invert(m); ok {
		return inv
	}
	return typeset.Identity
}

// contentEffects returns frags, a picture's or a table's in the shape's
// box, with what paints under them for s's shadow, glow and reflection, m
// mapping the parent's coordinates to the slide's. A picture's silhouette
// is its rectangle, a table's its borders and fills, never its text, which
// a table's shadow leaves out. On PowerPoint's rules a picture with a soft
// edge, a blur or a fill overlay is drawn as one bitmap in its place,
// which takes the picture's place in the structure tree; a table's are not
// drawn, nor a soft edge on Impress's rules.
//
// # Corpus evidence
//
//   - probe, pptx: a picture with a shadow blurred over 6 pt prints its
//     shadow as a bitmap of its box grown by 6 pt, opaque inside.
//   - libreoffice/pptx: oox/table-shadow prints its table's borders again
//     10 pt down and right in the shadow's red, its text once.
//   - litho/pptx: effects-glow-softedge-reflection prints in PowerPoint its
//     picture with a soft edge of 15 pt as one bitmap, faded at its edges.
func (b *builder) contentEffects(s *Shape, frags []typeset.Fragment, m typeset.Matrix) []typeset.Fragment {
	if s.Shadow == nil && s.Glow == nil && !numeric.Positive(s.SoftEdge) && s.Reflection == nil && s.Blur == nil && s.FillOverlay == nil {
		return frags
	}
	s = turnedShadow(s)
	t := *s
	if s.Kind != KindPicture || b.doc.Effects == EffectsImpress {
		if s.SoftEdge > 0 || (b.doc.Effects == EffectsPowerPoint && (s.Blur != nil || s.FillOverlay != nil)) {
			b.warn(warn.ContentUnsupported, warn.Approximated, "the soft edge, blur or fill overlay of "+quote(s.Name)+" is not drawn")
		}
		t.SoftEdge, t.Blur, t.FillOverlay = 0, nil, nil
	}
	sm := skewedAboutCentre(s.Box, s.Rotation, s.SkewX, s.FlipH, s.FlipV)
	under, drawn := b.paintEffects(&t, withoutText(frags, 0), nil, then(sm, m), false)
	if drawn != nil {
		adopt(drawn, figureOf(frags, 0))
		return append(under, drawn...)
	}
	return append(under, frags...)
}

// figureOf returns the structure element the first picture of frags hangs
// from, nil for none.
func figureOf(frags []typeset.Fragment, depth int) *typeset.StructElem {
	for _, f := range frags {
		switch f := f.(type) {
		case *typeset.Image:
			if f.Parent != nil {
				return f.Parent
			}
		case *typeset.Group:
			if depth < maxRecolorDepth {
				if p := figureOf(f.Content, depth+1); p != nil {
					return p
				}
			}
		}
	}
	return nil
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
	shadowSil := sil
	if b.doc.Effects == EffectsPowerPoint && sil.filled {
		if ss := silhouetteLined(own, toSlide, officefx.PowerPoint.ShadowLine); ss != nil {
			shadowSil = ss
		}
	}
	p := &effectPlan{
		s: s, sil: sil, shadowSil: shadowSil, area: typeset.Rect{Width: sw, Height: sh},
		impress: b.doc.Effects == EffectsImpress, twice: twice, page: &b.effectPixels,
		capped: func() {
			b.warn(warn.LimitReached, warn.Omitted, "effects past the pixels a slide's effect bitmaps may hold are not drawn")
		},
		reduced: func() {
			b.warn(warn.LimitReached, warn.Approximated, "effects past the pixels an effect bitmap may hold are drawn at a lower resolution")
		},
	}
	var top []typeset.Fragment
	var drawn []typeset.Fragment
	if s.Reflection != nil && !p.impress {
		content := append(append([]typeset.Fragment(nil), own...), text...)
		if img := p.reflection(b.ctx, content, toSlide); img != nil {
			top = append(top, img)
		}
	}
	switch {
	case !p.impress && (s.Blur != nil || s.FillOverlay != nil || s.SoftEdge > 0 && !sil.flat):
		if img := p.contentBitmap(b.ctx, own, toSlide); img != nil {
			drawn = []typeset.Fragment{img}
		} else {
			b.warn(warn.ContentUnsupported, warn.Omitted, "the blur, fill overlay or soft edge of "+quote(s.Name)+" is not drawn")
		}
	case s.SoftEdge > 0:
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
			if u := s.ShadowUnder; u != nil && u.Opacity > 0 {
				top = append(top, b.hardShadow(b.ctx, u, glows, append(append([]typeset.Fragment(nil), hard...), text...), toSlide, sil.bounds)...)
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
// skew, then the scale, about the centre of bounds, then the offsets
// (ECMA-376 Part 1, 20.1.8.45 outerShdw: kx, ky, sx, sy, dist and dir,
// which the reader resolves into the skew, the scale and the offsets,
// turned with the shape by [turnedShadow] when the shadow turns with it).
//
// # Corpus evidence
//
//   - litho/pptx: effects-shadow's shadow skewed by kx -13.34 degrees and
//     scaled by sy -23% about the bottom left: PowerPoint's soft mask
//     against the skew then the scale rms 2.29 levels (at most 10.3)
//     over the bitmap after the best shift; against the scale then the
//     skew 17.91, against either with the angle negated 21.23 and 13.66,
//     against no skew 16.03.
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
	m := translate(-cx, -cy)
	if kx, ky := skewTan(sh.SkewX), skewTan(sh.SkewY); kx != 0 || ky != 0 {
		m = then(m, typeset.Matrix{A: 1, B: ky, C: kx, D: 1})
	}
	m = then(m, typeset.Matrix{A: sx, D: sy})
	return then(m, translate(cx+sh.OffsetX, cy+sh.OffsetY))
}

// skewTan returns the tangent of a skew of deg degrees, deg clamped to
// [-maxSkew, maxSkew]; 0 for an angle that is not finite.
func skewTan(deg float64) float64 {
	if !numeric.Finite(deg) || deg == 0 {
		return 0
	}
	return math.Tan(min(max(deg, -maxSkew), maxSkew) * math.Pi / 180)
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
	out, err := typeset.EncodeImage(ctx, officefx.Tint(a, w, h, effect.ColorOf(c)))
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
// on PowerPoint's rules [officefx.Rules.GlowMask]; on Impress's the
// silhouette made opaque wherever it paints, grown by a square of half the
// radius and blurred by a tent of that half, so it fades to nothing one
// radius out (LibreOffice's GlowPrimitive2D). The outer glow of Impress's
// rectangle grows the outline of the first glow instead.
func (p *effectPlan) glowMask(ctx context.Context, g officefx.Grid, tr typeset.Matrix, outer bool) (*effect.Surface, error) {
	base, err := p.sil.coverage(ctx, g, tr)
	if err != nil {
		return nil, err
	}
	defer base.Release()
	r := p.s.Glow.Radius
	if !p.impress {
		return officefx.PowerPoint.GlowMask(ctx, g, base, r)
	}
	half, k := g.Whole(r/2), effect.TentKernel(g.Span(r/2))
	if err := effect.Threshold(base, 1); err != nil {
		return nil, err
	}
	inner, err := impressGrown(ctx, base, half, k)
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
	return impressGrown(ctx, base, half, k)
}

// glowSize returns the deviation of a glow's blur and how far it grows
// the silhouette, in points, for its grid ([effectPlan.grid]), as
// [effectPlan.glowMask] draws it.
func (p *effectPlan) glowSize() (sigma, morph float64) {
	r := p.s.Glow.Radius
	if p.impress {
		return tentSigma(r / 2), r / 2
	}
	return officefx.PowerPoint.GlowSize(r)
}

// impressGrown returns a copy of the coverage cov grown by a square of n
// pixels and blurred by k.
func impressGrown(ctx context.Context, cov *effect.Surface, n int, k effect.Kernel) (*effect.Surface, error) {
	out, err := cov.Clone(ctx)
	if err != nil {
		return nil, err
	}
	err = effect.Dilate(ctx, out, n, n)
	if err == nil {
		err = officefx.BlurEdge(ctx, out, k)
	}
	if err != nil {
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
//   - probe, pptx: in Impress, sd/shape-glow-effect's oval with a glow of 11 pt at 40% prints
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
	sigma, morph := p.glowSize()
	g, ok := p.grid(box, sigma, morph)
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
	img, err := officefx.Tinted(ctx, mask, c)
	if err != nil {
		return nil
	}
	return imageOn(ctx, g, img)
}

// softMask returns a soft edge's coverage on g: its fade
// ([effectPlan.softFade]) times the silhouette's own coverage.
func (p *effectPlan) softMask(ctx context.Context, g officefx.Grid, tr typeset.Matrix) (*effect.Surface, error) {
	return p.softFade(ctx, g, tr, true)
}

// softFade returns the fade of a soft edge on g, the silhouette moved by
// tr: on PowerPoint's rules [officefx.Rules.SoftFade]; on Impress's the
// silhouette made opaque wherever it paints, shrunk by a square of the
// radius and blurred by a tent of it, so it fades from the outline to
// opaque two radii in (LibreOffice's SoftEdgePrimitive2D; ECMA-376 Part 1,
// 20.1.8.53 softEdge), squared when Impress applies it twice. It is times
// the silhouette's coverage when withCoverage is set: content whose alpha
// holds the coverage already takes the fade alone.
func (p *effectPlan) softFade(ctx context.Context, g officefx.Grid, tr typeset.Matrix, withCoverage bool) (*effect.Surface, error) {
	cov, err := p.sil.coverage(ctx, g, tr)
	if err != nil {
		return nil, err
	}
	defer cov.Release()
	r := p.s.SoftEdge
	var fade *effect.Surface
	if p.impress {
		fade, err = impressFade(ctx, g, cov, r, p.twice)
	} else {
		fade, err = officefx.PowerPoint.SoftFade(ctx, g, cov, r)
	}
	if err != nil {
		return nil, err
	}
	if withCoverage {
		// The coverage In the fade: the fade times the coverage.
		if err := effect.Composite(ctx, fade, cov, effect.In); err != nil {
			fade.Release()
			return nil, err
		}
	}
	return fade, nil
}

// impressFade returns Impress's fade of a soft edge of radius r on g from
// the coverage cov, squared when twice is set.
func impressFade(ctx context.Context, g officefx.Grid, cov *effect.Surface, r float64, twice bool) (*effect.Surface, error) {
	fade, err := cov.Clone(ctx)
	if err != nil {
		return nil, err
	}
	n := g.Whole(r)
	err = effect.Threshold(fade, 1)
	if err == nil {
		err = effect.Erode(ctx, fade, n, n)
	}
	if err == nil {
		err = officefx.BlurEdge(ctx, fade, effect.TentKernel(g.Span(r)))
	}
	if err == nil && twice {
		err = square(ctx, fade)
	}
	if err != nil {
		fade.Release()
		return nil, err
	}
	return fade, nil
}

// softSize returns the deviation of a soft edge's blur and how far it
// shrinks the silhouette, in points, for its grid ([effectPlan.grid]), as
// [effectPlan.softFade] draws it.
func (p *effectPlan) softSize() (sigma, morph float64) {
	r := p.s.SoftEdge
	if p.impress {
		return tentSigma(r), r
	}
	return officefx.PowerPoint.SoftSize(r)
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
//   - probe, pptx: in Impress, sd/shape-soft-edges's diamond and rectangle print as bitmaps
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
	sigma, morph := p.softSize()
	g, ok := p.grid(box, sigma, morph)
	if !ok {
		return nil
	}
	img, err := p.softEdgeImage(ctx, g)
	if err != nil {
		return nil
	}
	return imageOn(ctx, g, img)
}

// softEdgeImage paints the silhouette's paths in their colours on g, each
// over the ones before, and gives each painted pixel the soft edge's
// alpha in place of its own, which the soft edge's coverage already holds.
func (p *effectPlan) softEdgeImage(ctx context.Context, g officefx.Grid) (*image.NRGBA, error) {
	paint, err := g.Surface(ctx, effect.RGBA)
	if err != nil {
		return nil, err
	}
	defer paint.Release()
	for i, path := range p.sil.paths {
		cov, err := g.Coverage(ctx, []effect.Path{path}, typeset.Identity)
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
	if err := typeset.ChargeImage(ctx, g.W, g.H, color.NRGBAModel); err != nil {
		return nil, err
	}
	img := image.NewNRGBA(image.Rect(0, 0, g.W, g.H))
	for i, v := range fade.Pix {
		px, out := paint.Pix[4*i:4*i+4:4*i+4], img.Pix[4*i:4*i+4:4*i+4]
		if a := px[3]; a != 0 {
			out[0], out[1], out[2], out[3] = officefx.Unpremultiply(px[0], a), officefx.Unpremultiply(px[1], a), officefx.Unpremultiply(px[2], a), v
		}
	}
	return img, nil
}

// shadow returns the bitmap of the shape's blurred shadow in slide
// coordinates: everything the shape paints but its text, glows and soft
// edge included, moved by the shadow's transform, blurred over its
// radius, in its colour at its opacity (LibreOffice's ShadowPrimitive2D:
// the content's own alpha, not made opaque, blurred by a stack blur of
// the radius; ECMA-376 Part 1, 20.1.8.45 outerShdw, which PowerPoint
// blurs by a Gaussian of a third of the radius); nil when it covers
// nothing.
//
// # Corpus evidence
//
//   - probe, pptx: in Impress, sd/shape-blur-effect's oval prints its shadow, blurred 11 pt,
//     as a bitmap over the oval's outline moved by the shadow's 34 pt and
//     grown by 11 pt, ramping from 0 to opaque over 22 pt about the
//     outline, 32 of 255 a quarter of the way.
//   - probe, pptx: a rectangle reaching past the slide's right edge prints its
//     shadow's bitmap cut 6 pt, its blur, past the edge.
//   - litho/pptx: effects-shadow prints in PowerPoint its shadows blurred
//     4 and 20 pt as bitmaps whose alpha along a row through the middle
//     rises as a Gaussian of deviation 1.31 and 6.6 pt, a third of the
//     radius, centred on the outline.
//   - libreoffice/pptx: sd/effectOrder prints in PowerPoint the shadow of
//     its rectangle with a glow over the glow too: 40 of 255 where the glow
//     is, the glow's 40% of the shadow's 102.
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
	g, ok := p.grid(box, p.shadowSigma(), 0)
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
	img, err := officefx.Tinted(ctx, mask, c)
	if err != nil {
		return nil
	}
	return imageOn(ctx, g, img)
}

// shadowMask returns the coverage of the shadow on g, the silhouette moved
// by tr: the soft edge's coverage, or the silhouette's, with the glows at
// their opacity over it, blurred by the shadow's radius.
func (p *effectPlan) shadowMask(ctx context.Context, g officefx.Grid, tr typeset.Matrix) (*effect.Surface, error) {
	var mask *effect.Surface
	var err error
	if p.s.SoftEdge > 0 && p.sil.flat {
		mask, err = p.softMask(ctx, g, tr)
	} else {
		mask, err = p.shadowSil.coverage(ctx, g, tr)
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
	k := effect.GaussianKernel(g.Span(p.s.Shadow.Blur / 3))
	if p.impress {
		k = effect.TentKernel(g.Span(p.s.Shadow.Blur))
	}
	if err := officefx.BlurEdge(ctx, mask, k); err != nil {
		return fail(err)
	}
	return mask, nil
}

// shadowSigma returns the deviation of the shadow's blur in points, for
// its grid ([effectPlan.grid]): a third of the radius on PowerPoint's
// rules, the deviation of the tent of the radius on Impress's.
func (p *effectPlan) shadowSigma() float64 {
	if p.impress {
		return tentSigma(p.s.Shadow.Blur)
	}
	return p.s.Shadow.Blur / 3
}

// innerShadow returns the bitmap of the inner shadow of s over its fills
// (ECMA-376 Part 1, 20.1.8.40 innerShdw), in the coordinates of fills,
// which toSlide maps to the slide: what lies outside the fills' silhouette
// moved against the shadow's offsets, blurred by a Gaussian of a third of
// its radius, kept where the silhouette is, in its colour at its opacity.
// It returns nil for a shape without one or without a fill, and on
// Impress's rules, which draw none.
//
// # Corpus evidence
//
//   - litho/pptx: effects-shadow prints in PowerPoint its inner shadow
//     blurred 5 pt, 4 pt toward 225 degrees, at 50% black as a bitmap over
//     the rounded rectangle's fill and under its line, darkest along the
//     top and left edges, half dark 2.9 pt in; its inner shadow blurred
//     15 pt with no offset fades from 82 of 255 at the outline, at 70%.
//   - litho/pptx: effects-shadow's two inner shadows, inside the fill's
//     straight edges, against PowerPoint's soft masks after the best
//     shift: the whole fill as the silhouette rms 0.54 and 1.14 levels,
//     the fill less its line of 1 pt 0.62 and 2.49. The shadow without an
//     offset, which fades from every edge, decides it. PowerPoint's
//     bitmaps sit 0.45 to 0.55 pt right of and below the shape, as its
//     outer shadows' do (up to 0.4 pt right, 0.4 to 0.5 pt below), which a
//     shadow faded on two sides cannot tell from a smaller silhouette.
func (b *builder) innerShadow(s *Shape, fills []typeset.Fragment, toSlide typeset.Matrix) []typeset.Fragment {
	sh := s.InnerShadow
	if sh == nil || sh.Opacity <= 0 || len(fills) == 0 || b.doc.Effects == EffectsImpress {
		return nil
	}
	back, ok := invert(toSlide)
	if !ok {
		return nil
	}
	sil := silhouetteOf(fills, toSlide)
	if sil == nil {
		return nil
	}
	p := &effectPlan{
		s: s, sil: sil, page: &b.effectPixels,
		capped: func() {
			b.warn(warn.LimitReached, warn.Omitted, "effects past the pixels a slide's effect bitmaps may hold are not drawn")
		},
		reduced: func() {
			b.warn(warn.LimitReached, warn.Approximated, "effects past the pixels an effect bitmap may hold are drawn at a lower resolution")
		},
	}
	dx, dy := sh.OffsetX, sh.OffsetY
	margin := math.Hypot(dx, dy) + max(sh.Blur, 0)
	if !numeric.Finite(margin) {
		return nil
	}
	g, ok := p.grid(grow(sil.bounds, margin), sh.Blur/3, 0)
	if !ok {
		return nil
	}
	cov, err := sil.coverage(b.ctx, g, typeset.Identity)
	if err != nil {
		return nil
	}
	mask, err := officefx.InnerMask(b.ctx, g, cov, dx, dy, sh.Blur)
	cov.Release()
	if err != nil {
		return nil
	}
	defer mask.Release()
	c := colorOf(sh.Color)
	c.A = min(sh.Opacity, 1)
	img, err := officefx.Tinted(b.ctx, mask, c)
	if err != nil {
		return nil
	}
	return group(back, []typeset.Fragment{imageOn(b.ctx, g, img)})
}

// reflection returns the bitmap of the shape's reflection in slide
// coordinates (ECMA-376 Part 1, 20.1.8.50 reflection): content, what the
// shape paints with its text, in the coordinates toSlide maps to the
// slide, drawn by [effect.Paint] through the reflection's scale and
// offsets, blurred by a Gaussian of a third of its radius, faded from its
// start opacity to its end opacity between its positions along its fade
// direction across the copy; nil when it covers nothing.
//
// # Corpus evidence
//
//   - litho/pptx: effects-glow-softedge-reflection prints in PowerPoint
//     each reflection as a bitmap of the shape mirrored about its bottom,
//     at 100 pixels an inch, under the shape and under its shadow and glow.
func (p *effectPlan) reflection(ctx context.Context, content []typeset.Fragment, toSlide typeset.Matrix) typeset.Fragment {
	rf := p.s.Reflection
	if rf.StartOpacity <= 0 && rf.EndOpacity <= 0 {
		return nil
	}
	mirror := shadowMatrix(&Shadow{ScaleX: rf.ScaleX, ScaleY: rf.ScaleY, OffsetX: rf.OffsetX, OffsetY: rf.OffsetY}, p.sil.bounds)
	copied := transformRect(mirror, p.sil.bounds)
	blurRad := max(rf.Blur, 0)
	box, ok := intersect(grow(copied, blurRad), grow(p.area, blurRad))
	if !ok {
		return nil
	}
	g, ok := p.grid(box, blurRad/3, 0)
	if !ok {
		return nil
	}
	surfaces, err := effect.Paint(ctx, []effect.Content{{
		Fragments: content, Transform: then(toSlide, mirror),
		Grid: g.Place(), W: g.W, H: g.H,
	}})
	if err != nil {
		return nil
	}
	s := surfaces[0]
	defer s.Release()
	if err := officefx.BlurEdge(ctx, s, effect.GaussianKernel(g.Span(blurRad/3))); err != nil {
		return nil
	}
	x0, y0, x1, y1 := fadeLine(copied, rf)
	px := g.Pixels()
	sx0, sy0 := apply(px, x0, y0)
	sx1, sy1 := apply(px, x1, y1)
	if err := effect.AlphaRamp(ctx, s, sx0, sy0, sx1, sy1, rf.StartOpacity, rf.EndOpacity); err != nil {
		return nil
	}
	img, err := officefx.Straight(ctx, s)
	if err != nil {
		return nil
	}
	return imageOn(ctx, g, img)
}

// fadeLine returns where a reflection's fade starts and ends in slide
// points: along its fade direction across the copy r, at its start and end
// positions of the copy's extent that way.
func fadeLine(r typeset.Rect, rf *Reflection) (x0, y0, x1, y1 float64) {
	sin, cos := math.Sincos(rf.FadeDir * math.Pi / 180)
	if !numeric.Finite(sin) || !numeric.Finite(cos) {
		sin, cos = 1, 0
	}
	cx, cy := r.X+r.Width/2, r.Y+r.Height/2
	// The copy's extent along the direction, from its centre.
	half := (math.Abs(cos)*r.Width + math.Abs(sin)*r.Height) / 2
	at := func(pos float64) (float64, float64) {
		t := -half + pos*2*half
		return cx + t*cos, cy + t*sin
	}
	x0, y0 = at(rf.StartPos)
	x1, y1 = at(rf.EndPos)
	return x0, y0, x1, y1
}

// contentBitmap returns the shape drawn as a bitmap in slide coordinates
// for the effects that read what it paints (ECMA-376 Part 1, 20.1.8.15
// blur, 20.1.8.29 fillOverlay, and 20.1.8.53 softEdge over a shading or
// a picture), applied in the order of the effect list: own, what the
// shape paints but its text, in the coordinates toSlide maps to the
// slide, drawn by [effect.Paint], blurred by a Gaussian of a third of the
// blur's radius, blended with the overlay's colour where it paints, then
// faded by the soft edge. A blur that grows reaches its radius past the
// shape's bounds; one that does not is cut there. It returns nil when
// nothing is drawn.
//
// # Corpus evidence
//
//   - litho/pptx: effects-glow-softedge-reflection prints in PowerPoint its
//     blurred rectangle, its rectangle under a multiplied red overlay and
//     its picture with a soft edge each as one bitmap in place of the
//     shape, the blurred one over its box grown by the radius.
func (p *effectPlan) contentBitmap(ctx context.Context, own []typeset.Fragment, toSlide typeset.Matrix) typeset.Fragment {
	s := p.s
	var reach, sigma, morph float64
	if bl := s.Blur; bl != nil {
		sigma = bl.Radius / 3
		if bl.Grow {
			reach = bl.Radius
		}
	}
	if s.SoftEdge > 0 {
		if sg := s.SoftEdge / 3; sigma == 0 || sg < sigma {
			sigma = sg
		}
		_, morph = officefx.PowerPoint.SoftSize(s.SoftEdge)
	}
	box, ok := intersect(grow(p.sil.bounds, reach), grow(p.area, reach))
	if !ok {
		return nil
	}
	g, ok := p.grid(box, sigma, morph)
	if !ok {
		return nil
	}
	surfaces, err := effect.Paint(ctx, []effect.Content{{Fragments: own, Transform: toSlide, Grid: g.Place(), W: g.W, H: g.H}})
	if err != nil {
		return nil
	}
	out := surfaces[0]
	defer out.Release()
	if bl := s.Blur; bl != nil {
		if err := officefx.BlurContent(ctx, g, out, bl.Radius); err != nil {
			return nil
		}
	}
	if fo := s.FillOverlay; fo != nil {
		col := effect.ColorOf(colorOf(fo.Color))
		col.A = effect.ColorOf(typeset.Color{A: fo.Opacity}).A
		if err := officefx.Overlay(ctx, g, out, col, officefx.BlendOf(fo.Mode)); err != nil {
			return nil
		}
	}
	if s.SoftEdge > 0 {
		if err := p.fadeContent(ctx, g, out); err != nil {
			return nil
		}
	}
	img, err := officefx.Straight(ctx, out)
	if err != nil {
		return nil
	}
	return imageOn(ctx, g, img)
}

// fadeContent fades the content surface c by the shape's soft edge, in
// place: each pixel times the fade, which leaves the content's own
// coverage to its alpha.
func (p *effectPlan) fadeContent(ctx context.Context, g officefx.Grid, c *effect.Surface) error {
	fade, err := p.softFade(ctx, g, typeset.Identity, false)
	if err != nil {
		return err
	}
	defer fade.Release()
	return effect.Mask(ctx, c, fade)
}
