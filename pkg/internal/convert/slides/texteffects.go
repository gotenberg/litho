package slides

import (
	"context"
	"math"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// textAnchor is a run whose effects are drawn from its glyphs: the
// effects and the colour of its text.
type textAnchor struct {
	fx    *TextEffects
	color typeset.Color
}

// The channels of the colour that marks the glyph copies layout draws
// for a run with effects (a text shadow of no offset and no blur, under
// the run's glyphs, with no structure parent): red and blue fixed, green
// carrying the run's index in steps of anchorStep from anchorGreen. No
// document gives such a colour: its red is 2^-40, under any 8-bit,
// 16-bit or percentage channel but 0.
const (
	anchorRed   = 1.0 / (1 << 40)
	anchorGreen = 0.25
	anchorBlue  = 1 - anchorRed
	anchorStep  = 1.0 / (1 << 30)
)

// maxTextAnchors caps the runs with effects of one text body. Anti-DoS
// guard, not spec-mandated: the marker colour counts 2^29 of them, and
// a body past this many draws the rest without their effects; a slide
// holds a few dozen runs.
const maxTextAnchors = 1 << 16

// textAnchorStyle returns the text shadow that marks the glyphs of a run
// of style rs for its effects ([builder.drawTextEffects]), nil when the
// run has none or the rules are Impress's, which draw no text effect.
func (b *builder) textAnchorStyle(rs *RunStyle) []css.Shadow {
	if rs.Effects == nil || b.doc.Effects != EffectsPowerPoint || len(b.anchors) >= maxTextAnchors {
		return nil
	}
	i := len(b.anchors)
	b.anchors = append(b.anchors, textAnchor{fx: rs.Effects, color: colorOf(textColor(rs.Color))})
	return []css.Shadow{{Color: css.Color{R: anchorRed, G: anchorGreen + float64(i)*anchorStep, B: anchorBlue, A: 1}}}
}

// anchorOf returns the run whose marker colour c is, false for a colour
// that marks none.
func (b *builder) anchorOf(c typeset.Color) (textAnchor, bool) {
	if c.R != anchorRed || c.B != anchorBlue {
		return textAnchor{}, false
	}
	f := (c.G - anchorGreen) / anchorStep
	if !numeric.Finite(f) || f < 0 || f >= float64(len(b.anchors)) || f != math.Trunc(f) {
		return textAnchor{}, false
	}
	return b.anchors[int(f)], true // fits int: below len(b.anchors), checked above
}

// drawTextEffects returns frags, laid-out text, with each glyph copy
// that marks a run's effects replaced by the bitmaps of those effects
// (ECMA-376 Part 1, 21.1.2.3.9 rPr effectLst), drawn under the run's
// glyphs, which stay text. The bitmaps have no structure parent: they
// are artifacts (ISO 14289-1, 7.1), as PowerPoint marks them.
//
// # Corpus evidence
//
//   - litho/pptx: text-fill-effects prints in PowerPoint its runs with an
//     outer shadow, a glow and a reflection as bitmaps marked as
//     artifacts under the text, which stays text.
func (b *builder) drawTextEffects(frags []typeset.Fragment, depth int) []typeset.Fragment {
	if len(b.anchors) == 0 || depth > maxRecolorDepth {
		return frags
	}
	out := make([]typeset.Fragment, 0, len(frags))
	for _, f := range frags {
		switch f := f.(type) {
		case *typeset.Text:
			if a, ok := b.anchorOf(f.Color); ok && f.Parent == nil {
				out = append(out, b.runEffects(f, a)...)
				continue
			}
		case *typeset.Group:
			f.Content = b.drawTextEffects(f.Content, depth+1)
		}
		out = append(out, f)
	}
	return out
}

// runEffects returns the bitmaps of the effects of a, drawn from the
// glyphs of t, a copy of the run's glyphs whose alpha is the opacity the
// run is drawn at: from the bottom, its reflection, its shadow and its
// glow. Each sits on the bounds of what the glyphs paint.
func (b *builder) runEffects(t *typeset.Text, a textAnchor) []typeset.Fragment {
	glyphs := *t
	glyphs.Color = typeset.Color{A: 1}
	ink, ok := b.inkBounds(b.ctx, &glyphs)
	if !ok {
		return nil
	}
	p := &effectPlan{
		sil:  &silhouette{bounds: ink},
		area: typeset.Rect{X: -office.MaxPageSide, Y: -office.MaxPageSide, Width: 2 * office.MaxPageSide, Height: 2 * office.MaxPageSide},
		page: &b.effectPixels,
		capped: func() {
			b.warn(warn.LimitReached, warn.Omitted, "effects past the pixels a slide's effect bitmaps may hold are not drawn")
		},
		reduced: func() {
			b.warn(warn.LimitReached, warn.Approximated, "effects past the pixels an effect bitmap may hold are drawn at a lower resolution")
		},
	}
	opacity := min(max(t.Color.A, 0), 1)
	var out []typeset.Fragment
	if rf := a.fx.Reflection; rf != nil {
		r := *rf
		r.OffsetX += (a.fx.ReflectionAlignX - 0.5) * ink.Width * (1 - r.ScaleX)
		r.OffsetY += (a.fx.ReflectionAlignY - 0.5) * ink.Height * (1 - r.ScaleY)
		r.StartOpacity *= opacity
		r.EndOpacity *= opacity
		p.s = &Shape{Reflection: &r}
		colored := *t
		colored.Color = a.color
		if img := p.reflection(b.ctx, []typeset.Fragment{&colored}, typeset.Identity); img != nil {
			out = append(out, img)
		}
	}
	if sh := a.fx.Shadow; sh != nil {
		s := *sh
		s.OffsetX += (a.fx.ShadowAlignX - 0.5) * ink.Width * (1 - s.ScaleX)
		s.OffsetY += (a.fx.ShadowAlignY - 0.5) * ink.Height * (1 - s.ScaleY)
		s.Opacity *= opacity
		out = append(out, p.textShadow(b.ctx, &glyphs, &s)...)
	}
	if gl := a.fx.Glow; gl != nil {
		g := *gl
		g.Opacity *= opacity
		p.s = &Shape{Glow: &g}
		if img := p.textGlow(b.ctx, &glyphs); img != nil {
			out = append(out, img)
		}
	}
	return out
}

// inkScale is the resolution, in pixels a point, the bounds of what a
// run's glyphs paint are found at: a third of a point.
const inkScale = 3

// inkBounds returns the bounds of what t paints, to a third of a point,
// false when it paints nothing or ctx ends first.
func (b *builder) inkBounds(ctx context.Context, t *typeset.Text) (typeset.Rect, bool) {
	size := t.Size
	if !numeric.PositiveFinite(size) || len(t.Glyphs) == 0 {
		return typeset.Rect{}, false
	}
	x0, x1 := math.Inf(1), math.Inf(-1)
	for _, g := range t.Glyphs {
		x0, x1 = min(x0, g.X), max(x1, g.X)
	}
	hs := t.HScale
	if hs == 0 {
		hs = 1
	}
	// The run's advance box, grown by two sizes every way: no glyph of a
	// face reaches that far past its origin.
	box := typeset.Rect{X: t.Origin.X + x0*hs - 2*size, Y: t.Origin.Y - 3*size, Width: (x1-x0)*hs + 4*size, Height: 5 * size}
	if !usable(box) || math.Ceil(box.Width*inkScale)*math.Ceil(box.Height*inkScale) > effect.MaxPixels {
		return typeset.Rect{}, false
	}
	w, h := int(math.Ceil(box.Width*inkScale)), int(math.Ceil(box.Height*inkScale)) // fits int: their product is at most effect.MaxPixels, checked above
	surfaces, err := effect.Paint(ctx, []effect.Content{{Fragments: []typeset.Fragment{t}, Grid: effect.Grid{X: box.X, Y: box.Y, Scale: inkScale}, W: w, H: h}})
	if err != nil {
		return typeset.Rect{}, false
	}
	s := surfaces[0]
	defer s.Release()
	px0, py0, px1, py1 := w, h, -1, -1
	for y := range h {
		for x := range w {
			if s.Pix[(y*w+x)*4+3] != 0 {
				px0, py0, px1, py1 = min(px0, x), min(py0, y), max(px1, x), max(py1, y)
			}
		}
	}
	if px1 < 0 {
		return typeset.Rect{}, false
	}
	return typeset.Rect{
		X: box.X + float64(px0)/inkScale, Y: box.Y + float64(py0)/inkScale,
		Width: float64(px1-px0+1) / inkScale, Height: float64(py1-py0+1) / inkScale,
	}, true
}

// glyphCoverage returns the coverage of t moved by tr on g: the alpha of
// the glyphs drawn by the renderer.
func glyphCoverage(ctx context.Context, g effectGrid, t *typeset.Text, tr typeset.Matrix) (*effect.Surface, error) {
	surfaces, err := effect.Paint(ctx, []effect.Content{{Fragments: []typeset.Fragment{t}, Transform: tr, Grid: g.place(), W: g.w, H: g.h}})
	if err != nil {
		return nil, err
	}
	defer surfaces[0].Release()
	return effect.AlphaOf(ctx, surfaces[0])
}

// textShadow returns the shadow of the glyphs t, opaque black, as
// PowerPoint draws a shape's: a bitmap of the glyphs moved by the
// shadow's transform about the bounds of what they paint, blurred by a
// Gaussian of a third of its radius, in its colour at its opacity; a
// copy of the glyphs in its colour for a shadow without a blur.
func (p *effectPlan) textShadow(ctx context.Context, t *typeset.Text, sh *Shadow) []typeset.Fragment {
	if sh.Opacity <= 0 {
		return nil
	}
	tr := shadowMatrix(sh, p.sil.bounds)
	c := colorOf(sh.Color)
	if !numeric.Positive(sh.Blur) {
		cp := *t
		c.A = 1
		cp.Color = c
		return []typeset.Fragment{&typeset.Group{Transform: tr, Opacity: min(sh.Opacity, 1), Content: []typeset.Fragment{&cp}}}
	}
	g, ok := p.grid(grow(transformRect(tr, p.sil.bounds), sh.Blur), sh.Blur/3, 0)
	if !ok {
		return nil
	}
	cov, err := glyphCoverage(ctx, g, t, tr)
	if err != nil {
		return nil
	}
	defer cov.Release()
	if err := blur(ctx, cov, effect.GaussianKernel(g.span(sh.Blur/3))); err != nil {
		return nil
	}
	c.A = min(sh.Opacity, 1)
	img, err := tinted(ctx, cov, c)
	if err != nil {
		return nil
	}
	return []typeset.Fragment{g.image(ctx, img)}
}

// textGlow returns the glow of the glyphs t, opaque black, as PowerPoint
// draws a shape's ([effectPlan.glowKernel]): their coverage made opaque
// from half, grown and blurred, in the glow's colour at its opacity; nil
// when it covers nothing.
func (p *effectPlan) textGlow(ctx context.Context, t *typeset.Text) typeset.Fragment {
	gl := p.s.Glow
	if gl.Opacity <= 0 || !numeric.Positive(gl.Radius) {
		return nil
	}
	sigma, morph := p.glowSize()
	g, ok := p.grid(grow(p.sil.bounds, gl.Radius), sigma, morph)
	if !ok {
		return nil
	}
	cov, err := glyphCoverage(ctx, g, t, typeset.Identity)
	if err != nil {
		return nil
	}
	defer cov.Release()
	if err := effect.Threshold(cov, p.silhouetteLevel()); err != nil {
		return nil
	}
	half, k := p.glowKernel(g)
	mask, err := p.grownBlur(ctx, cov, half, k)
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
