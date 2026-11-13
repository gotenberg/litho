package officefx

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// ErrPagePixels reports an effect left out because the effects drawn
// before it hold the pixels [effect.MaxPagePixels] allows.
var ErrPagePixels = errors.New("effect pixels exhausted")

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
	// Text marks the glyphs of a run of text, whose glow and shadow Word
	// draws with tighter falloffs than a shape's: see [textGlowGrow].
	Text bool
}

// The falloffs of a run's glow and shadow, as fractions of the radius:
// the glow grows by textGlowGrow of its radius and blurs by a Gaussian
// of textGlowSigma of it, the shadow blurs by a Gaussian of
// textShadowSigma of its blur radius, where a shape's glow grows by half
// and blurs by a sixth and its shadow blurs by a third.
//
// # Corpus evidence
//
//   - litho/docx run-w14-effects: Word's 8 point glow of 24 point bold
//     glyphs fits an edge 1.37 points out of the glyph, a Gaussian of 0.64
//     points (rms 0.18 levels); its 4 point shadow fits a Gaussian of 0.79
//     points (rms 0.25 levels).
//   - libreoffice/docx ooxmlexport/TextEffects_Glow_Shadow_Reflection: a 5
//     point glow of 11 point glyphs fits an edge 0.45 point out, a
//     Gaussian of about 0.25; an 18 point one an edge 2.35 points out, a
//     Gaussian of 0.83. The three glows' least squares give r/7.5 and
//     r/19.4, within 0.3 point of each. A 15 point shadow of 11 point
//     glyphs blurs by about 1 point, which no fraction of the radius that
//     fits the 4 point one gives: the shadow follows the 24 point sample.
const (
	textGlowGrow    = 1 / 7.5
	textGlowSigma   = 1 / 19.5
	textShadowSigma = 1 / 5.0
)

// Under returns the bitmaps of fx drawn under src, in paint order: the
// reflection, then the outer shadow, then the glow. Each is an
// [typeset.Image] with a nil parent, an artifact. pixels counts the
// pixels of the page's effects; an effect that would pass its cap is left
// out and the error wraps [ErrPagePixels] once the others are drawn.
// Other errors are the engine's: the budget's refusal, ctx's end.
//
// # Corpus evidence
//
//   - litho/docx run-w14-effects: Word paints a run's reflection, then its
//     shadow, then its glow under the glyphs (the colours of the three
//     bitmaps of "All together" in paint order), each marked Artifact,
//     the glyphs text above.
func Under(ctx context.Context, src *Source, fx *css.OfficeEffects, pixels *effect.Page) ([]typeset.Fragment, error) {
	if fx == nil || len(src.Paths) == 0 {
		return nil, nil
	}
	var out []typeset.Fragment
	var skipped error
	add := func(f typeset.Fragment, err error) error {
		switch {
		case errors.Is(err, ErrPagePixels):
			skipped = err
		case err != nil:
			return err
		case f != nil:
			out = append(out, f)
		}
		return nil
	}
	if r := fx.Reflection; r != nil {
		if err := add(reflection(ctx, src, r, pixels)); err != nil {
			return nil, err
		}
	}
	if sh := fx.Shadow; sh != nil {
		if err := add(shadow(ctx, src, sh, pixels)); err != nil {
			return nil, err
		}
	}
	if g := fx.Glow; g != nil {
		if err := add(glow(ctx, src, g, pixels)); err != nil {
			return nil, err
		}
	}
	return out, skipped
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
func shadow(ctx context.Context, src *Source, sh *css.OfficeShadow, pixels *effect.Page) (typeset.Fragment, error) {
	col, ok := opaqueEnough(sh.Color)
	if !ok {
		return nil, nil
	}
	m := mul(translate(offset(sh.Dist, sh.Dir)), scaleSkew(src.Bounds, sh.Align, sh.SX, sh.SY, sh.KX, sh.KY))
	sigma := radius(sh.Blur) / 3
	if src.Text {
		sigma = float64(radius(sh.Blur) * textShadowSigma)
	}
	cov, err := cover(ctx, src.Paths, pathBounds(src.Paths), m, sigma, 3*sigma, pixels)
	if err != nil || cov == nil {
		return nil, err
	}
	defer cov.Release()
	if err := blur(ctx, cov, sigma); err != nil {
		return nil, err
	}
	return tinted(ctx, cov, col)
}

// glow draws a glow: the silhouette made opaque, grown by half the radius
// and blurred by a Gaussian of a sixth of it, in its colour.
//
// # Corpus evidence
//
//   - litho/pptx effects-glow-softedge-reflection: PowerPoint's 18 and 5
//     point glows fit a dilation by half the radius then a Gaussian of a
//     sixth of it (rms 0.50 and 0.24 levels).
//   - litho/docx shapes-effects-3d: Word's 11 point glow fits a Gaussian of
//     1.81 points, a sixth of the radius, centred half the radius out
//     (rms 0.33 levels).
func glow(ctx context.Context, src *Source, g *css.OfficeGlow, pixels *effect.Page) (typeset.Fragment, error) {
	col, ok := opaqueEnough(g.Color)
	r := radius(g.Radius)
	if !ok || r <= 0 {
		return nil, nil
	}
	sigma, grow := r/6, r/2
	if src.Text {
		sigma, grow = float64(r*textGlowSigma), float64(r*textGlowGrow)
	}
	cov, err := cover(ctx, src.Paths, pathBounds(src.Paths), typeset.Identity, sigma, grow+3*sigma, pixels)
	if err != nil || cov == nil {
		return nil, err
	}
	defer cov.Release()
	if err := effect.Threshold(cov, 1); err != nil {
		return nil, err
	}
	px := pixelsOf(grow, cov.Grid.Scale)
	if err := effect.Dilate(ctx, cov, px, px); err != nil {
		return nil, err
	}
	if err := blur(ctx, cov, sigma); err != nil {
		return nil, err
	}
	return tinted(ctx, cov, col)
}

// reflection draws a reflection: the content mapped by the reflection's
// scale, skew and offset, over the mapped bounds alone, blurred by a Gaussian of a third of its blur
// radius, its opacity running linearly along its fade direction from the
// start opacity at the start position to the end opacity at the end
// position, positions taken along the extent of the mapped bounds.
func reflection(ctx context.Context, src *Source, r *css.OfficeReflection, pixels *effect.Page) (typeset.Fragment, error) {
	m := mul(translate(offset(r.Dist, r.Dir)), scaleSkew(src.Bounds, r.Align, r.SX, r.SY, r.KX, r.KY))
	sigma := radius(r.Blur) / 3
	var s *effect.Surface
	if src.Content != nil {
		res, grid, ok := plan(mapRect(src.Bounds, m), sigma, 3*sigma, pixels)
		if !ok {
			return nil, fmt.Errorf("a reflection: %w", ErrPagePixels)
		}
		if res.W == 0 {
			return nil, nil
		}
		ss, err := effect.Paint(ctx, []effect.Content{{Fragments: src.Content, Transform: m, Grid: grid, W: res.W, H: res.H}})
		if err != nil {
			return nil, err
		}
		s = ss[0]
	} else {
		if !(src.Color.A > 0) {
			return nil, nil
		}
		cov, err := cover(ctx, src.Paths, src.Bounds, m, sigma, 3*sigma, pixels)
		if err != nil || cov == nil {
			return nil, err
		}
		s, err = effect.Tint(ctx, cov, effect.ColorOf(src.Color))
		cov.Release()
		if err != nil {
			return nil, err
		}
	}
	defer s.Release()
	if err := blur(ctx, s, sigma); err != nil {
		return nil, err
	}
	// The fade runs along the extent of the mapped bounds in the fade
	// direction, through their centre.
	b := mapRect(src.Bounds, m)
	dy, dx := sincosDeg(r.FadeDir)
	cx, cy := b.X+b.Width/2, b.Y+b.Height/2
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
		return nil, err
	}
	return encode(ctx, s)
}

// SoftEdge returns src's content as a bitmap whose edge fades inside the
// silhouette: the silhouette made opaque, shrunk by the radius and
// blurred by a Gaussian of a third of it, the content drawn through it.
// The bitmap replaces the content; it takes no structure parent, which
// the caller sets. It returns nil when the radius draws nothing, and the
// errors of [Under].
//
// # Corpus evidence
//
//   - litho/pptx effects-glow-softedge-reflection: PowerPoint's 25 and 5
//     point soft edges fit an erosion by the radius then a Gaussian of a
//     third of it (rms 0.44 and 0.53 levels); the shape is a bitmap, no
//     vector path left.
//   - litho/docx shapes-effects-3d: Word's 5 point soft edge fits a
//     Gaussian of 1.66 points, a third of the radius (rms 0.35 levels).
func SoftEdge(ctx context.Context, src *Source, rad float64, pixels *effect.Page) (typeset.Fragment, error) {
	r := radius(rad)
	if r <= 0 || len(src.Paths) == 0 || src.Content == nil {
		return nil, nil
	}
	sigma := r / 3
	cov, err := cover(ctx, src.Paths, pathBounds(src.Paths), typeset.Identity, sigma, 0, pixels)
	if err != nil || cov == nil {
		return nil, err
	}
	defer cov.Release()
	if err := effect.Threshold(cov, 1); err != nil {
		return nil, err
	}
	shrink := pixelsOf(r, cov.Grid.Scale)
	if err := effect.Erode(ctx, cov, shrink, shrink); err != nil {
		return nil, err
	}
	if err := blur(ctx, cov, sigma); err != nil {
		return nil, err
	}
	ss, err := effect.Paint(ctx, []effect.Content{{Fragments: src.Content, Grid: cov.Grid, W: cov.W, H: cov.H}})
	if err != nil {
		return nil, err
	}
	content := ss[0]
	defer content.Release()
	mask, err := effect.Tint(ctx, cov, effect.Color{R: 255, G: 255, B: 255, A: 255})
	if err != nil {
		return nil, err
	}
	defer mask.Release()
	// The content where the mask is: Porter-Duff In of the content on
	// the mask.
	if err := effect.Composite(ctx, mask, content, effect.In); err != nil {
		return nil, err
	}
	return encode(ctx, mask)
}

// cover returns a coverage surface of paths mapped by m, over area mapped
// by m and grown by pad points, at the resolution of a blur of sigma; nil
// when they cover nothing.
func cover(ctx context.Context, paths []effect.Path, area typeset.Rect, m typeset.Matrix, sigma, pad float64, pixels *effect.Page) (*effect.Surface, error) {
	b := mapRect(area, m)
	res, grid, ok := plan(b, sigma, pad, pixels)
	if !ok {
		return nil, ErrPagePixels
	}
	if res.W == 0 {
		return nil, nil
	}
	s, err := effect.New(ctx, effect.Coverage, res.W, res.H, grid)
	if err != nil {
		return nil, err
	}
	k := grid.Scale
	// Page points to pixels: m, then less the grid's origin, times k.
	tr := mul(typeset.Matrix{A: k, D: k, E: float64(-grid.X * k), F: float64(-grid.Y * k)}, m)
	if err := effect.Fill(ctx, s, paths, tr); err != nil {
		s.Release()
		return nil, err
	}
	return s, nil
}

// plan returns the resolution and grid of an effect over b grown by pad
// points, blurred by sigma, false when the page's effects hold their
// pixels. A zero resolution is an empty effect.
func plan(b typeset.Rect, sigma, pad float64, pixels *effect.Page) (effect.Resolution, effect.Grid, bool) {
	if !numeric.Finite(b.X) || !numeric.Finite(b.Y) || !(b.Width >= 0) || !(b.Height >= 0) {
		return effect.Resolution{}, effect.Grid{}, true
	}
	b = typeset.Rect{X: b.X - pad, Y: b.Y - pad, Width: b.Width + 2*pad, Height: b.Height + 2*pad}
	res, ok := effect.Plan(sigma, b.Width, b.Height)
	if !ok {
		return effect.Resolution{}, effect.Grid{}, true
	}
	if !pixels.Take(res) {
		return effect.Resolution{}, effect.Grid{}, false
	}
	return res, effect.Grid{X: b.X, Y: b.Y, Scale: res.Scale}, true
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
	return encode(ctx, s)
}

// encode returns the RGBA surface s as an image placed over its grid,
// colours straight as the soft mask expects (ISO 32000-1, 11.6.5.3).
func encode(ctx context.Context, s *effect.Surface) (typeset.Fragment, error) {
	if err := typeset.ChargeImage(ctx, s.W, s.H, color.NRGBAModel); err != nil {
		return nil, err
	}
	img := image.NewNRGBA(image.Rect(0, 0, s.W, s.H))
	empty := true
	for i := 0; i < len(s.Pix); i += 4 {
		a := s.Pix[i+3]
		if a == 0 {
			continue
		}
		empty = false
		p := img.Pix[i : i+4 : i+4]
		for c := range 3 {
			p[c] = uint8(min((uint32(s.Pix[i+c])*255+uint32(a)/2)/uint32(a), 255)) //nolint:gosec // at most 255
		}
		p[3] = a
	}
	if empty {
		typeset.ReleaseImage(ctx, img)
		return nil, nil
	}
	src, err := typeset.EncodeImage(ctx, img)
	if err != nil {
		return nil, err
	}
	k := s.Grid.Scale
	return &typeset.Image{Rect: typeset.Rect{X: s.Grid.X, Y: s.Grid.Y, Width: float64(s.W) / k, Height: float64(s.H) / k}, Source: src}, nil
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

// pixelsOf returns v points at k pixels a point, rounded to whole pixels.
func pixelsOf(v, k float64) int {
	return int(math.Round(min(float64(v*k), effect.MaxKernelRadius))) // fits int: at most MaxKernelRadius
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

// scaleSkew returns the map that scales by sx and sy and skews by kx and
// ky degrees about the point of b that align names: ECMA-376 Part 1,
// 20.1.8.45 outerShdw sx, sy, kx, ky and algn.
func scaleSkew(b typeset.Rect, align css.RectAlign, sx, sy, kx, ky float64) typeset.Matrix {
	sx, sy = factor(sx), factor(sy)
	tx, ty := tanDeg(kx), tanDeg(ky)
	px, py := b.X+float64(unit(align.X)*b.Width), b.Y+float64(unit(align.Y)*b.Height)
	// Skew after scale: x' = sx x + tx sy y, y' = ty sx x + sy y, about
	// (px, py).
	m := typeset.Matrix{A: sx, B: float64(ty * sx), C: float64(tx * sy), D: sy}
	return mul(translate(px, py), mul(m, translate(-px, -py)))
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

// sincosDeg returns the sine and cosine of deg degrees with results that
// do not depend on the machine: a multiple of 90 degrees exactly, any
// other angle by a Taylor series whose every product is rounded on its
// own, which the math package's Sincos does not promise.
func sincosDeg(deg float64) (float64, float64) {
	if !numeric.Finite(deg) {
		return 0, 1
	}
	d := math.Mod(deg, 360)
	if d < 0 {
		d += 360
	}
	q := math.Floor(d/90 + 0.5)
	r := float64((d - float64(q*90)) * (math.Pi / 180))
	r2 := float64(r * r)
	// Taylor series on |r| <= pi/4 to the 21st power: under 2^-60 left.
	sp := 1.0 / 51090942171709440000
	for _, c := range [...]float64{
		-1.0 / 121645100408832000, 1.0 / 355687428096000, -1.0 / 1307674368000,
		1.0 / 6227020800, -1.0 / 39916800, 1.0 / 362880, -1.0 / 5040, 1.0 / 120,
		-1.0 / 6, 1,
	} {
		sp = float64(sp*r2) + c
	}
	cp := 1.0 / 2432902008176640000
	for _, c := range [...]float64{
		-1.0 / 6402373705728000, 1.0 / 20922789888000, -1.0 / 87178291200,
		1.0 / 479001600, -1.0 / 3628800, 1.0 / 40320, -1.0 / 720, 1.0 / 24,
		-1.0 / 2, 1,
	} {
		cp = float64(cp*r2) + c
	}
	s, c := float64(sp*r), cp
	switch int(q) % 4 { // fits int: q is 0 to 4
	case 1:
		return c, -s
	case 2:
		return -s, -c
	case 3:
		return -c, s
	}
	return s, c
}
