package officefx

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// InnerMask returns the coverage of an inner shadow on g (ECMA-376 Part
// 1, 20.1.8.40 innerShdw) of the silhouette whose coverage on g is cov,
// which it leaves as it is: the silhouette's complement moved by (-dx,
// -dy) points, blurred by a Gaussian of a third of blurRad points, times
// the silhouette's coverage. The grid's margin past the silhouette, at
// least the offset and the blur, reads as complement.
func InnerMask(ctx context.Context, g Grid, cov *effect.Surface, dx, dy, blurRad float64) (*effect.Surface, error) {
	full, err := g.Surface(ctx, effect.Coverage)
	if err != nil {
		return nil, err
	}
	defer full.Release()
	if err := effect.Flood(ctx, full, effect.Color{A: 255}); err != nil {
		return nil, err
	}
	out, err := cov.Clone(ctx)
	if err != nil {
		return nil, err
	}
	steps := []func() error{
		// The complement: full where the silhouette is not.
		func() error { return effect.Composite(ctx, out, full, effect.Out) },
		func() error { return effect.Offset(ctx, out, -g.Span(dx), -g.Span(dy)) },
		func() error { return BlurEdge(ctx, out, effect.GaussianKernel(g.Span(blurRad/3))) },
		// What the silhouette covers of the moved complement.
		func() error { return effect.Composite(ctx, out, cov, effect.In) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			out.Release()
			return nil, err
		}
	}
	return out, nil
}

// BlurContent blurs the content surface c on g by a Gaussian of a third
// of rad points, in place (ECMA-376 Part 1, 20.1.8.15 blur), the pixels
// past the bitmap's edge transparent.
func BlurContent(ctx context.Context, g Grid, c *effect.Surface, rad float64) error {
	k := effect.GaussianKernel(g.Span(rad / 3))
	return effect.Blur(ctx, c, k, k, effect.EdgeNone)
}

// Overlay blends col over the content surface c on g by mode, in place,
// and keeps the result where the content paints (ECMA-376 Part 1,
// 20.1.8.29 fillOverlay): the colour blended over c, then c's alpha In
// it.
func Overlay(ctx context.Context, g Grid, c *effect.Surface, col effect.Color, mode effect.BlendMode) error {
	layer, err := g.Surface(ctx, effect.RGBA)
	if err != nil {
		return err
	}
	defer layer.Release()
	if err := effect.Flood(ctx, layer, col); err != nil {
		return err
	}
	blended, err := c.Clone(ctx)
	if err != nil {
		return err
	}
	defer blended.Release()
	if err := effect.Blend(ctx, blended, layer, mode); err != nil {
		return err
	}
	return effect.Composite(ctx, c, blended, effect.In)
}

// overlayModes maps an overlay's blend to its blend mode (Compositing 1,
// 5.3): over is normal, mult multiply.
var overlayModes = [...]effect.BlendMode{
	css.OfficeOver:     effect.Normal,
	css.OfficeMultiply: effect.Multiply,
	css.OfficeScreen:   effect.Screen,
	css.OfficeDarken:   effect.Darken,
	css.OfficeLighten:  effect.Lighten,
}

// BlendOf returns the blend mode of an overlay's blend, normal for one
// out of range.
func BlendOf(b css.OfficeBlend) effect.BlendMode {
	if int(b) < len(overlayModes) {
		return overlayModes[b]
	}
	return effect.Normal
}

// fills returns the filled paths of paths, the strokes left out.
func fills(paths []effect.Path) []effect.Path {
	var out []effect.Path
	for _, p := range paths {
		if !numeric.Positive(p.Width) {
			out = append(out, p)
		}
	}
	return out
}

// Inner returns the bitmap of the inner shadow of fx drawn over the fills
// of src, which the caller paints under the source's lines: what lies
// outside the fills' silhouette moved against the shadow's offsets,
// blurred by a Gaussian of a third of its radius, kept where the
// silhouette is, in its colour ([InnerMask]). It returns nil for no inner
// shadow or no fill, and the notes and errors of [Under].
//
// # Corpus evidence
//
//   - litho/pptx: effects-shadow prints in PowerPoint its inner shadow
//     blurred 5 pt, 4 pt toward 225 degrees, at 50% black as a bitmap over
//     the rounded rectangle's fill and under its line, darkest along the
//     top and left edges, half dark 2.9 pt in.
//   - litho/docx: shapes-effects-3d prints in Word its inner shadow blurred
//     9 pt, 3 pt toward 225 degrees, as a bitmap over the rounded
//     rectangle, darkest along its top and left edges.
func Inner(ctx context.Context, src *Source, fx *css.OfficeEffects, pixels *effect.Page) (typeset.Fragment, Notes, error) {
	sh := fx.InnerShadow
	if sh == nil {
		return nil, Notes{}, nil
	}
	col, ok := opaqueEnough(sh.Color)
	paths := fills(src.Paths)
	if !ok || len(paths) == 0 {
		return nil, Notes{}, nil
	}
	dx, dy := offset(sh.Dist, sh.Dir)
	blurRad := radius(sh.Blur)
	margin := float64(detmath.Hypot(dx, dy)) + blurRad
	g, notes, ok := take(Grow(pathBounds(paths), margin), blurRad/3, 0, pixels)
	if !ok {
		return nil, notes, nil
	}
	cov, err := g.Coverage(ctx, paths, typeset.Identity)
	if err != nil {
		return nil, Notes{}, err
	}
	mask, err := InnerMask(ctx, g, cov, dx, dy, blurRad)
	cov.Release()
	if err != nil {
		return nil, Notes{}, err
	}
	img, err := Tinted(ctx, mask, col)
	mask.Release()
	if err != nil {
		return nil, Notes{}, err
	}
	f, err := g.Image(ctx, img)
	return f, notes, err
}

// Content returns src's content as one bitmap for the effects of fx that
// read what it paints, in the effect list's order: its blur (ECMA-376
// Part 1, 20.1.8.15), its fill overlay (20.1.8.29), then its soft edge
// ([Rules.SoftFade]). The bitmap replaces the content; a blur that grows
// reaches its radius past the content's bounds, one that does not is cut
// there. It takes no structure parent, which the caller sets. It returns
// nil when none of the three draws anything, and the notes and errors of
// [Under].
//
// # Corpus evidence
//
//   - litho/pptx: effects-glow-softedge-reflection prints in PowerPoint its
//     blurred rectangle, its rectangle under a multiplied red overlay and
//     its picture with a soft edge each as one bitmap in place of the
//     shape, the blurred one over its box grown by the radius.
//   - litho/docx: shapes-effects-3d prints in Word its rounded rectangle
//     blurred 3 pt as one bitmap over its box grown by 3 pt, and its soft
//     edge as one bitmap over its box.
func Content(ctx context.Context, src *Source, fx *css.OfficeEffects, pixels *effect.Page) (typeset.Fragment, Notes, error) {
	bl, fo := fx.Blur, fx.FillOverlay
	soft := radius(fx.SoftEdge)
	if bl != nil && !(radius(bl.Radius) > 0) {
		bl = nil
	}
	if fo != nil && !(fo.Color.A > 0) {
		fo = nil
	}
	if (bl == nil && fo == nil && soft == 0) || len(src.Paths) == 0 || src.Content == nil {
		return nil, Notes{}, nil
	}
	rules := rulesOf(fx.Rules)
	var reach, sigma, morph float64
	if bl != nil {
		sigma = radius(bl.Radius) / 3
		if bl.Grow {
			reach = radius(bl.Radius)
		}
	}
	if soft > 0 {
		sg, m := rules.SoftSize(soft)
		if sigma == 0 || sg < sigma {
			sigma = sg
		}
		morph = m
	}
	g, notes, ok := take(Grow(pathBounds(src.Paths), reach), sigma, morph, pixels)
	if !ok {
		return nil, notes, nil
	}
	ss, err := effect.Paint(ctx, []effect.Content{{Fragments: src.Content, Grid: g.Place(), W: g.W, H: g.H}})
	if err != nil {
		return nil, Notes{}, err
	}
	out := ss[0]
	defer out.Release()
	if bl != nil {
		if err := BlurContent(ctx, g, out, radius(bl.Radius)); err != nil {
			return nil, Notes{}, err
		}
	}
	if fo != nil {
		if err := Overlay(ctx, g, out, effect.ColorOf(typeset.Color{R: unit(fo.Color.R), G: unit(fo.Color.G), B: unit(fo.Color.B), A: unit(fo.Color.A)}), BlendOf(fo.Blend)); err != nil {
			return nil, Notes{}, err
		}
	}
	if soft > 0 {
		cov, err := g.Coverage(ctx, src.Paths, typeset.Identity)
		if err != nil {
			return nil, Notes{}, err
		}
		fade, err := rules.SoftFade(ctx, g, cov, soft)
		cov.Release()
		if err != nil {
			return nil, Notes{}, err
		}
		// The content's alpha holds the silhouette's coverage: it takes
		// the fade alone.
		err = effect.Mask(ctx, out, fade)
		fade.Release()
		if err != nil {
			return nil, Notes{}, err
		}
	}
	img, err := Straight(ctx, out)
	if err != nil {
		return nil, Notes{}, err
	}
	f, err := g.Image(ctx, img)
	return f, notes, err
}
