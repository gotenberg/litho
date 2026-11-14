package officefx

import (
	"context"
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// glyphEffects returns the bitmaps of fx under the glyphs t on
// PowerPoint's rules, in paint order: the reflection, the shadow, the
// glow, each drawn from what the glyphs paint ([effect.Paint]) by the
// rules of a shape's ([PowerPoint]), its scales and skews about the point
// of the glyphs' ink that its alignment names. A shadow without a blur is
// a copy of the glyphs in its colour. It returns the errors of [Under].
//
// # Corpus evidence
//
//   - litho/pptx: text-fill-effects prints in PowerPoint its runs with an
//     outer shadow, a glow and a reflection as bitmaps marked as
//     artifacts under the text, which stays text.
func glyphEffects(ctx context.Context, t *typeset.Text, fx *css.OfficeEffects, pixels *effect.Page) ([]typeset.Fragment, Notes, error) {
	glyphs := *t
	glyphs.Color = typeset.Color{A: 1}
	glyphs.Parent = nil
	ink, ok, err := inkBounds(ctx, &glyphs)
	if err != nil || !ok {
		return nil, Notes{}, err
	}
	var out []typeset.Fragment
	var notes Notes
	add := func(f typeset.Fragment, n Notes, err error) error {
		notes = notes.join(n)
		if f != nil {
			out = append(out, f)
		}
		return err
	}
	if r := fx.Reflection; r != nil {
		colored := glyphs
		colored.Color = t.Color
		if err := add(glyphReflection(ctx, &colored, r, ink, pixels)); err != nil {
			return nil, Notes{}, err
		}
	}
	if sh := fx.Shadow; sh != nil {
		scaled := *sh
		scaled.Dist = float64(pptScale(t.Size, pptShadowSize) * radius(sh.Dist))
		if err := add(glyphShadow(ctx, &glyphs, &scaled, ink, pixels)); err != nil {
			return nil, Notes{}, err
		}
	}
	if g := fx.Glow; g != nil {
		scaled := *g
		scaled.Radius = float64(pptScale(t.Size, pptGlowSize) * radius(g.Radius))
		if err := add(glyphGlow(ctx, &glyphs, &scaled, ink, pixels)); err != nil {
			return nil, Notes{}, err
		}
	}
	return out, notes, nil
}

// pptShadowSize is the font size, in points, at which PowerPoint moves
// the outer shadow of a run of text the distance it states: see
// [pptScale].
//
// # Corpus evidence
//
//   - probe, pptx: runs of Calibri printed at 12.27, 23.79, 48.31 and
//     72.12 points, each with a shadow 10 points down and no blur:
//     PowerPoint draws the shadow as the glyphs moved 2.89, 4.49, 7.04 and
//     9.36 points (the centroid of its soft mask against the centroid of
//     the glyphs' ink); 10 (size/80)^(2/3) gives 2.87, 4.46, 7.14 and
//     9.33, within 0.11 points. Word's scale gives 2.95, 4.66, 7.59 and
//     10.01.
//   - litho/pptx: text-fill-effects prints a 40 point run's shadow 3
//     points toward 45 degrees 1.29 points down, the rule 1.35.
const pptShadowSize = 80

// pptGlowSize is the font size, in points, at which PowerPoint draws the
// glow of a run of text at the radius it states: see [pptScale].
//
// # Corpus evidence
//
//   - probe, pptx: glows of 8 points on runs of Calibri printed at
//     23.79, 48.31 and 72.12 points fit the shape's rule ([PowerPoint])
//     best at radii scaled by 0.55, 0.89 and 1.04 (rms 5.1, 3.5 and 2.4
//     levels at 100 pixels an inch); (size/66)^(2/3) gives 0.51, 0.81 and
//     1.06. Over the four sizes with 12.27 points, the full radius at 64
//     to 68 points fits best, at 80 points (the shadow's) the levels rise
//     by 0.5 to 2.9 at 48 and 72 points, unscaled by 10 to 25 at 12 and
//     24.
//   - litho/pptx: text-fill-effects prints a 40 point run's glow of 8
//     points best at a radius scaled by 0.70, the rule 0.72.
//   - litho/pptx: effects-glow-softedge-reflection prints a 40 point bold
//     run's glow of 11 points with its half alpha across the middle of
//     the glyphs from 319.25 to 408.31 points; the rule's reaches from
//     318.57 to 408.84, the unscaled radius 1.28 points further each way.
const pptGlowSize = 66

// pptScale returns the factor PowerPoint scales a length of an effect of
// a run of text by at font size size, (size/full)^(2/3), full the size
// at which it draws the length it states: [pptShadowSize] for a shadow's
// distance, [pptGlowSize] for a glow's radius. A size that is not
// positive and finite scales nothing. A shadow's blur and a reflection
// are not scaled.
//
// # Corpus evidence
//
//   - probe, pptx: the reflection of runs 4 points away at 12.27, 23.79,
//     48.31 and 72.12 points mirrors the copy about a line 1.84, 3.22,
//     6.73 and 9.46 points below the baseline, its baseline 3.25, 6.79,
//     12.46 and 18.07 points down, where the reflection drawn about the
//     ink's bottom lies 8.6, 12.5, 20.6 and 29.3 points down: PowerPoint
//     scales it with the size, about the baseline as Word does, by a rule
//     one distance does not settle.
func pptScale(size, full float64) float64 {
	if !numeric.PositiveFinite(size) {
		return 1
	}
	return detmath.Pow(min(size, maxFontSize)/full, 2.0/3)
}

// take returns the grid of an effect over box blurred by sigma after a
// growth or shrink of morph points ([PlanGrid]), and false when it draws
// nothing; notes say whether the page's pixels left it out or the
// engine's cap lowered its resolution.
func take(box typeset.Rect, sigma, morph float64, pixels *effect.Page) (Grid, Notes, bool) {
	g, reduced, ok := PlanGrid(box, sigma, morph)
	if !ok {
		return Grid{}, Notes{}, false
	}
	n := Notes{Reduced: reduced}
	if !pixels.Take(effect.Resolution{W: g.W, H: g.H}) {
		return Grid{}, Notes{Skipped: true}, false
	}
	return g, n, true
}

// glyphMatrix returns the map of a run's shadow or reflection whose ink
// is b: skewed by kx and ky about the ink's centre, scaled by sx and sy
// about the point of the ink align names, then moved dist along dir. A
// shape's skews about the point its alignment names ([Rules.ShadowMatrix]).
//
// # Corpus evidence
//
//   - libreoffice/pptx: sd/tdf114848 prints in PowerPoint the shadows of
//     its two WordArt lines, skewed by kx -30 degrees and scaled by sy 30%
//     about the bottom left, as one bitmap from 78.5 to 634.7 pt across;
//     their glyphs' shadows skewed about the ink's centre reach from 84.9
//     to 644.0 pt, about its bottom left from 99.1 to 658.2 pt.
func glyphMatrix(b typeset.Rect, align css.RectAlign, sx, sy, kx, ky, dist, dir float64) typeset.Matrix {
	sx, sy = factor(sx), factor(sy)
	tx, ty := tanDeg(kx), tanDeg(ky)
	cx, cy := b.X+float64(b.Width/2), b.Y+float64(b.Height/2)
	dx, dy := offset(dist, dir)
	// Scaling about the alignment point is scaling about the centre and
	// moving by the alignment point's offset from the centre times 1 less
	// the scale.
	dx += float64((unit(align.X) - 0.5) * b.Width * (1 - sx))
	dy += float64((unit(align.Y) - 0.5) * b.Height * (1 - sy))
	// x' = sx (x + tx y), y' = sy (ty x + y), about the centre.
	m := typeset.Matrix{A: sx, B: float64(sy * ty), C: float64(sx * tx), D: sy}
	return mul(translate(cx+dx, cy+dy), mul(m, translate(-cx, -cy)))
}

// inkScale is the resolution, in pixels a point, the bounds of what a
// run's glyphs paint are found at: a third of a point.
const inkScale = 3

// inkBounds returns the bounds of what t paints, to a third of a point,
// false when it paints nothing.
func inkBounds(ctx context.Context, t *typeset.Text) (typeset.Rect, bool, error) {
	size := t.Size
	if !numeric.PositiveFinite(size) || len(t.Glyphs) == 0 {
		return typeset.Rect{}, false, nil
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
	box := typeset.Rect{X: t.Origin.X + float64(x0*hs) - 2*size, Y: t.Origin.Y - float64(3*size), Width: float64((x1-x0)*hs) + float64(4*size), Height: 5 * size}
	if !usable(box) || math.Ceil(box.Width*inkScale)*math.Ceil(box.Height*inkScale) > effect.MaxPixels {
		return typeset.Rect{}, false, nil
	}
	w, h := int(math.Ceil(box.Width*inkScale)), int(math.Ceil(box.Height*inkScale)) // fits int: their product is at most effect.MaxPixels, checked above
	surfaces, err := effect.Paint(ctx, []effect.Content{{Fragments: []typeset.Fragment{t}, Grid: effect.Grid{X: box.X, Y: box.Y, Scale: inkScale}, W: w, H: h}})
	if err != nil {
		return typeset.Rect{}, false, err
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
		return typeset.Rect{}, false, nil
	}
	return typeset.Rect{
		X: box.X + float64(px0)/inkScale, Y: box.Y + float64(py0)/inkScale,
		Width: float64(px1-px0+1) / inkScale, Height: float64(py1-py0+1) / inkScale,
	}, true, nil
}

// glyphCoverage returns the coverage of t moved by tr on g: the alpha of
// the glyphs drawn by the renderer.
func glyphCoverage(ctx context.Context, g Grid, t *typeset.Text, tr typeset.Matrix) (*effect.Surface, error) {
	surfaces, err := effect.Paint(ctx, []effect.Content{{Fragments: []typeset.Fragment{t}, Transform: tr, Grid: g.Place(), W: g.W, H: g.H}})
	if err != nil {
		return nil, err
	}
	defer surfaces[0].Release()
	return effect.AlphaOf(ctx, surfaces[0])
}

// glyphShadow returns the shadow of the glyphs t, opaque black, as
// PowerPoint draws a shape's ([PowerPoint]): a bitmap of the glyphs
// mapped by the shadow about ink, blurred, in its colour; a copy of the
// glyphs in its colour for a shadow without a blur.
func glyphShadow(ctx context.Context, t *typeset.Text, sh *css.OfficeShadow, ink typeset.Rect, pixels *effect.Page) (typeset.Fragment, Notes, error) {
	c, ok := opaqueEnough(sh.Color)
	if !ok {
		return nil, Notes{}, nil
	}
	tr := glyphMatrix(ink, sh.Align, sh.SX, sh.SY, sh.KX, sh.KY, sh.Dist, sh.Dir)
	blurRad := radius(sh.Blur)
	if blurRad == 0 {
		cp := *t
		cp.Color = typeset.Color{R: c.R, G: c.G, B: c.B, A: 1}
		return &typeset.Group{Transform: tr, Opacity: c.A, Content: []typeset.Fragment{&cp}}, Notes{}, nil
	}
	g, notes, ok := take(Grow(MapRect(ink, tr), blurRad), blurRad/3, 0, pixels)
	if !ok {
		return nil, notes, nil
	}
	cov, err := glyphCoverage(ctx, g, t, tr)
	if err != nil {
		return nil, Notes{}, err
	}
	defer cov.Release()
	if err := BlurEdge(ctx, cov, effect.GaussianKernel(g.Span(blurRad/3))); err != nil {
		return nil, Notes{}, err
	}
	img, err := Tinted(ctx, cov, c)
	if err != nil {
		return nil, Notes{}, err
	}
	f, err := g.Image(ctx, img)
	return f, notes, err
}

// glyphGlow returns the glow of the glyphs t, opaque black, as PowerPoint
// draws a shape's ([PowerPoint]): their coverage made opaque from half,
// grown and blurred, in the glow's colour.
func glyphGlow(ctx context.Context, t *typeset.Text, gl *css.OfficeGlow, ink typeset.Rect, pixels *effect.Page) (typeset.Fragment, Notes, error) {
	c, ok := opaqueEnough(gl.Color)
	r := radius(gl.Radius)
	if !ok || r <= 0 {
		return nil, Notes{}, nil
	}
	sigma, morph := PowerPoint.GlowSize(r)
	g, notes, ok := take(Grow(ink, r), sigma, morph, pixels)
	if !ok {
		return nil, notes, nil
	}
	cov, err := glyphCoverage(ctx, g, t, typeset.Identity)
	if err != nil {
		return nil, Notes{}, err
	}
	defer cov.Release()
	if err := effect.Threshold(cov, PowerPoint.SilhouetteLevel); err != nil {
		return nil, Notes{}, err
	}
	mask, err := PowerPoint.GrownBlur(ctx, g, cov, r)
	if err != nil {
		return nil, Notes{}, err
	}
	defer mask.Release()
	img, err := Tinted(ctx, mask, c)
	if err != nil {
		return nil, Notes{}, err
	}
	f, err := g.Image(ctx, img)
	return f, notes, err
}

// glyphReflection returns the reflection of t, the glyphs in their
// colour, as PowerPoint draws a shape's: drawn by [effect.Paint] through
// the reflection's map about ink, blurred by a Gaussian of a third of its
// radius, faded from its start opacity to its end opacity between its
// positions along its fade direction across the copy.
func glyphReflection(ctx context.Context, t *typeset.Text, rf *css.OfficeReflection, ink typeset.Rect, pixels *effect.Page) (typeset.Fragment, Notes, error) {
	if !(rf.StartAlpha > 0) && !(rf.EndAlpha > 0) {
		return nil, Notes{}, nil
	}
	mirror := glyphMatrix(ink, rf.Align, rf.SX, rf.SY, rf.KX, rf.KY, rf.Dist, rf.Dir)
	copied := MapRect(ink, mirror)
	blurRad := radius(rf.Blur)
	g, notes, ok := take(Grow(copied, blurRad), blurRad/3, 0, pixels)
	if !ok {
		return nil, notes, nil
	}
	surfaces, err := effect.Paint(ctx, []effect.Content{{
		Fragments: []typeset.Fragment{t}, Transform: mirror,
		Grid: g.Place(), W: g.W, H: g.H,
	}})
	if err != nil {
		return nil, Notes{}, err
	}
	s := surfaces[0]
	defer s.Release()
	if err := BlurEdge(ctx, s, effect.GaussianKernel(g.Span(blurRad/3))); err != nil {
		return nil, Notes{}, err
	}
	x0, y0, x1, y1 := FadeLine(copied, rf)
	px := g.Pixels()
	sx0, sy0 := apply(px, x0, y0)
	sx1, sy1 := apply(px, x1, y1)
	if err := effect.AlphaRamp(ctx, s, sx0, sy0, sx1, sy1, unit(rf.StartAlpha), unit(rf.EndAlpha)); err != nil {
		return nil, Notes{}, err
	}
	img, err := Straight(ctx, s)
	if err != nil {
		return nil, Notes{}, err
	}
	f, err := g.Image(ctx, img)
	return f, notes, err
}

// FromGlyphs reports whether the rules of fx draw a run's effects from
// its glyphs as they paint, [Source.Glyphs], rather than from their
// outlines, [Source.Paths]: PowerPoint's do.
func FromGlyphs(fx *css.OfficeEffects) bool {
	return fx != nil && fx.Rules == css.OfficePowerPoint
}
