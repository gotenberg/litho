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

// Notes say what drawing a source's effects left out or drew coarser
// than its rule.
type Notes struct {
	// Skipped reports an effect left out because the effects drawn before
	// it hold the pixels [effect.MaxPagePixels] allows.
	Skipped bool
	// Reduced reports an effect drawn at a lower resolution than its rule
	// gives, past the pixels [effect.MaxPixels] allows a bitmap.
	Reduced bool
}

// join returns n with what o notes too.
func (n Notes) join(o Notes) Notes {
	return Notes{Skipped: n.Skipped || o.Skipped, Reduced: n.Reduced || o.Reduced}
}

// MaxRadius caps a blur, glow, soft edge or reflection radius and a
// distance, in points. Anti-DoS guard, not spec-mandated: the schemas
// allow 2,147,483,647 EMU (169,000 points); 1000 points is wider than any
// page, so a larger value draws the same.
const MaxRadius = 1000

// maxScale caps a shadow's or a reflection's scale factor either way.
// Anti-DoS guard, not spec-mandated: ten times the source's size covers
// any page.
const maxScale = 10

// maxSkew caps a skew angle, in degrees either way. Anti-DoS guard, not
// spec-mandated: ST_FixedAngle allows up to 90 degrees, where the tangent
// is infinite; 89 degrees stretches a shadow 57 times.
const maxSkew = 89

// Source is what an effect is drawn from, in page points with y downward.
type Source struct {
	// Paths is the silhouette: the outlines of what the source paints.
	Paths []effect.Path
	// Bounds is the box a shadow's or a reflection's alignment refers to:
	// a shape's or a picture's box, a run's ink.
	Bounds typeset.Rect
	// Content is what a reflection or a soft edge shows, drawn by
	// [effect.Paint]; nil shows the silhouette in Color.
	Content []typeset.Fragment
	// Color fills the silhouette a reflection shows when Content is nil.
	Color typeset.Color
	// Text marks the glyphs of a run of text, whose effects Word scales
	// by its font size, Size ([wordRun]).
	Text bool
	// Size is the font size of a run of text, in points.
	Size float64
	// Glyphs is the run of text, placed on the page in its colour, that
	// PowerPoint's rules draw a run's effects from ([glyphEffects]); nil
	// for a shape or a picture.
	Glyphs *typeset.Text
}

// Under returns the bitmaps of fx drawn under src, in paint order: the
// reflection, then the outer shadow, then the glow. Each is an
// [typeset.Image] with a nil parent, an artifact. pixels counts the
// pixels of the page's effects; an effect that would pass its cap is left
// out, which the notes say, once the others are drawn. The error is the
// engine's: the budget's refusal, ctx's end; it returns no bitmap with
// one. A run of text on PowerPoint's rules ([css.OfficePowerPoint]) is
// drawn from src.Glyphs ([glyphEffects]), a run of Word's text by
// [wordRun], any other source from its paths.
//
// # Corpus evidence
//
//   - litho/docx run-w14-effects: Word paints a run's reflection, then its
//     shadow, then its glow under the glyphs (the colours of the three
//     bitmaps of "All together" in paint order), each marked Artifact,
//     the glyphs text above.
func Under(ctx context.Context, src *Source, fx *css.OfficeEffects, pixels *effect.Page) ([]typeset.Fragment, Notes, error) {
	if fx == nil {
		return nil, Notes{}, nil
	}
	if FromGlyphs(fx) && src.Glyphs != nil {
		return glyphEffects(ctx, src.Glyphs, fx, pixels)
	}
	if len(src.Paths) == 0 {
		return nil, Notes{}, nil
	}
	if src.Text && fx.Rules == css.OfficeWord {
		return wordRun(ctx, src, fx, pixels)
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
		if err := add(reflection(ctx, src, r, pixels)); err != nil {
			return nil, Notes{}, err
		}
	}
	if sh := fx.Shadow; sh != nil {
		if err := add(shadow(ctx, src, sh, pixels)); err != nil {
			return nil, Notes{}, err
		}
	}
	if g := fx.Glow; g != nil {
		if err := add(glow(ctx, src, g, rulesOf(fx.Rules), pixels)); err != nil {
			return nil, Notes{}, err
		}
	}
	return out, notes, nil
}

// shadow draws an outer shadow: the silhouette mapped by the shadow's
// scale, skew and offset, blurred by a Gaussian of a third of its blur
// radius, in its colour.
//
// # Corpus evidence
//
//   - litho/pptx effects-shadow, litho/docx shapes-effects-3d: PowerPoint's
//     and Word's shadow masks fit a Gaussian of a third of blurRad within
//     half a level to a level, the edge centred on the moved outline.
func shadow(ctx context.Context, src *Source, sh *css.OfficeShadow, pixels *effect.Page) (typeset.Fragment, Notes, error) {
	col, ok := opaqueEnough(sh.Color)
	if !ok {
		return nil, Notes{}, nil
	}
	m := shadowMatrix(sh, src.Bounds)
	sigma := radius(sh.Blur) / 3
	cov, notes, err := cover(ctx, src.Paths, pathBounds(src.Paths), m, sigma, 3*sigma, pixels)
	if err != nil || cov == nil {
		return nil, notes, err
	}
	defer cov.Release()
	if err := blur(ctx, cov, sigma); err != nil {
		return nil, Notes{}, err
	}
	f, err := tinted(ctx, cov, col)
	return f, notes, err
}

// glow draws a glow: the silhouette made opaque, grown and blurred by
// rules ([Rules.GlowMask]), in its colour.
func glow(ctx context.Context, src *Source, g *css.OfficeGlow, rules *Rules, pixels *effect.Page) (typeset.Fragment, Notes, error) {
	col, ok := opaqueEnough(g.Color)
	r := radius(g.Radius)
	if !ok || r <= 0 {
		return nil, Notes{}, nil
	}
	sigma, morph := rules.GlowSize(r)
	gr, notes, ok := take(Grow(pathBounds(src.Paths), r), sigma, morph, pixels)
	if !ok {
		return nil, notes, nil
	}
	cov, err := gr.Coverage(ctx, src.Paths, typeset.Identity)
	if err != nil {
		return nil, Notes{}, err
	}
	mask, err := rules.GlowMask(ctx, gr, cov, r)
	cov.Release()
	if err != nil {
		return nil, Notes{}, err
	}
	img, err := Tinted(ctx, mask, col)
	mask.Release()
	if err != nil {
		return nil, Notes{}, err
	}
	f, err := gr.Image(ctx, img)
	return f, notes, err
}

// reflection draws a reflection: the content mapped by the reflection's
// scale, skew and offset, over the mapped bounds alone, blurred by a Gaussian of a third of its blur
// radius, its opacity running linearly along its fade direction from the
// start opacity at the start position to the end opacity at the end
// position, positions taken along the extent of the mapped bounds.
func reflection(ctx context.Context, src *Source, r *css.OfficeReflection, pixels *effect.Page) (typeset.Fragment, Notes, error) {
	m := reflectionMatrix(r, src.Bounds)
	b := mapRect(src.Bounds, m)
	return reflect(ctx, src, r, m, b, b, pixels)
}

// reflect draws the reflection r of src mapped by m over area, its fade
// running along fade, the mapped bounds, as [reflection] says.
func reflect(ctx context.Context, src *Source, r *css.OfficeReflection, m typeset.Matrix, area, fade typeset.Rect, pixels *effect.Page) (typeset.Fragment, Notes, error) {
	sigma := radius(r.Blur) / 3
	var s *effect.Surface
	var notes Notes
	if src.Content != nil {
		res, grid, n := planArea(area, sigma, 3*sigma, pixels)
		notes = n
		if res.W == 0 {
			return nil, notes, nil
		}
		ss, err := effect.Paint(ctx, []effect.Content{{Fragments: src.Content, Transform: m, Grid: grid, W: res.W, H: res.H}})
		if err != nil {
			return nil, Notes{}, err
		}
		s = ss[0]
	} else {
		if !(src.Color.A > 0) {
			return nil, Notes{}, nil
		}
		cov, n, err := coverArea(ctx, src.Paths, area, m, sigma, 3*sigma, pixels)
		notes = n
		if err != nil || cov == nil {
			return nil, notes, err
		}
		s, err = effect.Tint(ctx, cov, effect.ColorOf(src.Color))
		cov.Release()
		if err != nil {
			return nil, Notes{}, err
		}
	}
	defer s.Release()
	if err := blur(ctx, s, sigma); err != nil {
		return nil, Notes{}, err
	}
	// The fade runs along the extent of the mapped bounds in the fade
	// direction, through their centre.
	b := fade
	dy, dx := sincosDeg(r.FadeDir)
	cx, cy := b.X+float64(b.Width/2), b.Y+float64(b.Height/2)
	half := (math.Abs(float64(dx*b.Width)) + math.Abs(float64(dy*b.Height))) / 2
	k := s.Grid.Scale
	px := func(x, gx float64) float64 { return float64((x - gx) * k) }
	at := func(f float64) (float64, float64) {
		t := float64((2*f - 1) * half)
		return px(cx+float64(t*dx), s.Grid.X), px(cy+float64(t*dy), s.Grid.Y)
	}
	x0, y0 := at(unit(r.StartPos))
	x1, y1 := at(unit(r.EndPos))
	if err := effect.AlphaRamp(ctx, s, x0, y0, x1, y1, unit(r.StartAlpha), unit(r.EndAlpha)); err != nil {
		return nil, Notes{}, err
	}
	f, err := encodeFrag(ctx, s)
	return f, notes, err
}

// cover returns a coverage surface of paths mapped by m, over area mapped
// by m and grown by pad points, at the resolution of a blur of sigma; nil
// when they cover nothing or the page's pixels refuse it, which the notes
// say.
func cover(ctx context.Context, paths []effect.Path, area typeset.Rect, m typeset.Matrix, sigma, pad float64, pixels *effect.Page) (*effect.Surface, Notes, error) {
	return coverArea(ctx, paths, mapRect(area, m), m, sigma, pad, pixels)
}

// coverArea is [cover] over area, in page points already.
func coverArea(ctx context.Context, paths []effect.Path, area typeset.Rect, m typeset.Matrix, sigma, pad float64, pixels *effect.Page) (*effect.Surface, Notes, error) {
	res, grid, notes := planArea(area, sigma, pad, pixels)
	if res.W == 0 {
		return nil, notes, nil
	}
	s, err := effect.New(ctx, effect.Coverage, res.W, res.H, grid)
	if err != nil {
		return nil, Notes{}, err
	}
	k := grid.Scale
	// Page points to pixels: m, then less the grid's origin, times k.
	tr := mul(typeset.Matrix{A: k, D: k, E: float64(-grid.X * k), F: float64(-grid.Y * k)}, m)
	if err := effect.Fill(ctx, s, paths, tr); err != nil {
		s.Release()
		return nil, Notes{}, err
	}
	return s, notes, nil
}

// planArea returns the resolution and grid of an effect over b grown by pad
// points, blurred by sigma. A zero resolution draws nothing: an empty
// effect, or one the page's pixels refuse, which the notes say with a
// resolution the engine's cap lowered.
func planArea(b typeset.Rect, sigma, pad float64, pixels *effect.Page) (effect.Resolution, effect.Grid, Notes) {
	if !numeric.Finite(b.X) || !numeric.Finite(b.Y) || !(b.Width >= 0) || !(b.Height >= 0) {
		return effect.Resolution{}, effect.Grid{}, Notes{}
	}
	b = typeset.Rect{X: b.X - pad, Y: b.Y - pad, Width: b.Width + 2*pad, Height: b.Height + 2*pad}
	res, ok := effect.Plan(sigma, b.Width, b.Height)
	if !ok {
		return effect.Resolution{}, effect.Grid{}, Notes{}
	}
	if !pixels.Take(res) {
		return effect.Resolution{}, effect.Grid{}, Notes{Skipped: true}
	}
	return res, effect.Grid{X: b.X, Y: b.Y, Scale: res.Scale}, Notes{Reduced: res.Reduced}
}

// blur blurs s by a Gaussian of sigma points, nothing for 0.
func blur(ctx context.Context, s *effect.Surface, sigma float64) error {
	if sigma <= 0 {
		return nil
	}
	k := effect.GaussianKernel(float64(sigma * s.Grid.Scale))
	return effect.Blur(ctx, s, k, k, effect.EdgeNone)
}

// tinted returns the coverage cov in colour c as an image.
func tinted(ctx context.Context, cov *effect.Surface, c typeset.Color) (typeset.Fragment, error) {
	s, err := effect.Tint(ctx, cov, effect.ColorOf(c))
	if err != nil {
		return nil, err
	}
	defer s.Release()
	return encodeFrag(ctx, s)
}

// opaqueEnough returns c as a page colour and whether it paints anything.
func opaqueEnough(c css.Color) (typeset.Color, bool) {
	t := typeset.Color{R: unit(c.R), G: unit(c.G), B: unit(c.B), A: unit(c.A)}
	return t, t.A > 0
}

// radius returns r within [0, MaxRadius], 0 for a value that is not
// finite.
func radius(r float64) float64 {
	if !numeric.PositiveFinite(r) {
		return 0
	}
	return min(r, MaxRadius)
}

// unit returns v within [0, 1], 0 for NaN.
func unit(v float64) float64 {
	if !(v > 0) {
		return 0
	}
	return min(v, 1)
}

// offset returns the move of dist points along dir degrees, clockwise
// from the x axis with y downward.
func offset(dist, dir float64) (float64, float64) {
	d := radius(dist)
	s, c := sincosDeg(dir)
	return float64(d * c), float64(d * s)
}

// translate returns the move by (dx, dy).
func translate(dx, dy float64) typeset.Matrix {
	return typeset.Matrix{A: 1, D: 1, E: dx, F: dy}
}

// factor returns a scale factor within [-maxScale, maxScale], 1 for a
// value that is not finite.
func factor(v float64) float64 {
	if !numeric.Finite(v) {
		return 1
	}
	return min(max(v, -maxScale), maxScale)
}

// tanDeg returns the tangent of a skew of deg degrees, within maxSkew.
func tanDeg(deg float64) float64 {
	if !numeric.Finite(deg) || deg == 0 {
		return 0
	}
	s, c := sincosDeg(min(max(deg, -maxSkew), maxSkew))
	return s / c
}

// mul returns the map that applies b, then a.
func mul(a, b typeset.Matrix) typeset.Matrix {
	return typeset.Matrix{
		A: float64(a.A*b.A) + float64(a.C*b.B),
		B: float64(a.B*b.A) + float64(a.D*b.B),
		C: float64(a.A*b.C) + float64(a.C*b.D),
		D: float64(a.B*b.C) + float64(a.D*b.D),
		E: float64(a.A*b.E) + float64(a.C*b.F) + a.E,
		F: float64(a.B*b.E) + float64(a.D*b.F) + a.F,
	}
}

// apply maps p by m.
func apply(m typeset.Matrix, x, y float64) (float64, float64) {
	return float64(m.A*x) + float64(m.C*y) + m.E, float64(m.B*x) + float64(m.D*y) + m.F
}

// mapRect returns the bounds of r mapped by m.
func mapRect(r typeset.Rect, m typeset.Matrix) typeset.Rect {
	x0, y0 := math.Inf(1), math.Inf(1)
	x1, y1 := math.Inf(-1), math.Inf(-1)
	for _, p := range [4][2]float64{{r.X, r.Y}, {r.X + r.Width, r.Y}, {r.X, r.Y + r.Height}, {r.X + r.Width, r.Y + r.Height}} {
		x, y := apply(m, p[0], p[1])
		x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
	}
	return typeset.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

// pathBounds returns the bounds of the points of paths, strokes grown by
// half their width.
func pathBounds(paths []effect.Path) typeset.Rect {
	x0, y0 := math.Inf(1), math.Inf(1)
	x1, y1 := math.Inf(-1), math.Inf(-1)
	for _, p := range paths {
		hw := 0.0
		if numeric.PositiveFinite(p.Width) {
			hw = p.Width / 2
		}
		for _, op := range p.Ops {
			n := 1
			switch op.Kind {
			case typeset.MoveTo, typeset.LineTo:
			case typeset.ClosePath:
				n = 0
			case typeset.CurveTo:
				n = 3
			}
			for _, q := range op.P[:n] {
				if !numeric.Finite(q.X) || !numeric.Finite(q.Y) {
					continue
				}
				x0, y0, x1, y1 = min(x0, q.X-hw), min(y0, q.Y-hw), max(x1, q.X+hw), max(y1, q.Y+hw)
			}
		}
	}
	if x0 > x1 {
		return typeset.Rect{X: math.NaN()}
	}
	return typeset.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

// sincosDeg returns the sine and cosine of deg degrees, the same on
// every machine ([detmath.SincosDegrees]); 0 and 1 for an angle that is not
// finite.
func sincosDeg(deg float64) (float64, float64) {
	if !numeric.Finite(deg) {
		return 0, 1
	}
	return detmath.SincosDegrees(deg)
}

// encodeFrag returns [effect.Encode] of s as a fragment, nil for none.
func encodeFrag(ctx context.Context, s *effect.Surface) (typeset.Fragment, error) {
	img, err := effect.Encode(ctx, s)
	if err != nil || img == nil {
		return nil, err
	}
	return img, nil
}
