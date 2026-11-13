package officefx

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// Rules are the measures one Microsoft Office program draws its effects
// by where Word and PowerPoint part ways. [PowerPoint] holds
// PowerPoint's; Word's are the package's plain rules ([Under]).
type Rules struct {
	// GlowGrow is how far a glow grows the silhouette before its blur, a
	// share of the radius.
	GlowGrow float64
	// GlowSquare divides a glow's growth into the share it grows by a
	// square, one part in GlowSquare, and the share it grows by a disc,
	// the rest: a square with rounded corners. 1 grows by a square
	// alone.
	GlowSquare int
	// SoftShrink is how far a soft edge shrinks the silhouette before its
	// blur, a share of the radius.
	SoftShrink float64
	// ShadowLine is the share of a filled shape's line width the
	// silhouette of its blurred outer shadow takes.
	ShadowLine float64
	// SilhouetteLevel is the coverage from which a pixel belongs to the
	// silhouette a glow grows and a soft edge shrinks.
	SilhouetteLevel uint8
}

// PowerPoint are PowerPoint's rules.
//
// GlowGrow is 5/12, half the radius less half the blur's deviation of a
// sixth of it. GlowSquare is 3: a disc alone draws a glow's corners and
// the diagonals of a curve short of where PowerPoint prints them, a
// square alone past them. SoftShrink is 11/12, the radius less a quarter
// of the blur's deviation of a third of it. ShadowLine is a half: the
// line at half its width, the silhouette reaching a quarter of the line
// past the outline; a shape of lines alone takes its whole lines.
// SilhouetteLevel is half: PowerPoint's glows and soft edges sit where
// the outline is, not half a pixel out of it.
//
// # Corpus evidence
//
//   - litho/pptx: effects-glow-softedge-reflection prints glows of 5 and
//     18 pt whose fades, fitted on both sides of a row through the
//     middle, are centred 0.43 and 0.42 radii out of the outline.
//   - libreoffice/pptx: sd/effectOrder's glow of 28 pt is centred 0.43
//     radii out; sd/shape-glow-effect's of 11 pt 0.46 out of the fill,
//     0.42 out of a line of 0.75 pt.
//   - litho/pptx: effects-glow-softedge-reflection's ellipse with a glow
//     of 18 pt, along 16 rays off the axes, from the outline out:
//     PowerPoint's soft mask against a square's growth rms 15.45 levels
//     (at most 48.8), a disc's 6.85 (23.6), a third square 1.68 (6.9), a
//     quarter 1.75 (7.3), a fifth 2.45 (9.8); its rectangle whose
//     effectDag holds a glow of 8 pt, along its corners' diagonals: 9.56,
//     8.14, 0.38, 1.71, 2.88.
//   - libreoffice/pptx: sd/effectOrder's rectangle with a glow of 28 pt,
//     along its corners' diagonals: square 6.18 (16.0), disc 3.80 (9.7),
//     a third 0.77 (2.3), a quarter 0.36 (1.4), a fifth 0.84 (2.4).
//   - litho/pptx: effects-glow-softedge-reflection prints soft edges of 5
//     and 25 pt whose fades, fitted on both sides of a row through the
//     middle, are centred 0.94 and 0.92 radii in from the outline.
//   - libreoffice/pptx: sd/shape-soft-edges's soft edge of 10 pt, on a
//     rectangle with a hairline, is centred 0.85 radii in.
//   - litho/pptx: effects-shadow's rounded rectangles with a line of 1 pt,
//     their shadows blurred 4 and 20 pt and the theme's shadow of its
//     effectRef shape, against PowerPoint's soft masks over the whole
//     bitmap after the best shift: rms 0.73, 0.84 and 0.59 levels with
//     half the line, 2.29, 0.67 and 2.76 with the whole line, 1.43, 1.65
//     and 2.74 with none; three quarters 1.47, 0.60, 1.47; a quarter
//     0.78, 1.27, 1.52. Its shadow scaled to 110% fits the whole line
//     best: 0.54, half 2.21.
//   - litho/pptx: effects-glow-softedge-reflection's glow of 18 pt, drawn
//     from the silhouette of every pixel the outline touches, lies 0.3 pt,
//     half a pixel, further out on both sides than PowerPoint prints it.
var PowerPoint = Rules{
	GlowGrow:        5.0 / 12,
	GlowSquare:      3,
	SoftShrink:      11.0 / 12,
	ShadowLine:      0.5,
	SilhouetteLevel: 128,
}

// GlowSize returns the deviation of the blur of a glow of radius r and
// how far it grows the silhouette, in points: a Gaussian of a sixth of
// the radius after a growth of GlowGrow of it.
func (r *Rules) GlowSize(rad float64) (sigma, morph float64) {
	return rad / 6, rad * r.GlowGrow
}

// GrownBlur returns a copy of the coverage cov on g grown and blurred for
// a glow of radius rad ([Rules.GlowSize]): by a square of a GlowSquare-th
// of the growth, then a disc of the rest, then blurred, the pixels past
// the bitmap's edge repeating the edge's.
func (r *Rules) GrownBlur(ctx context.Context, g Grid, cov *effect.Surface, rad float64) (*effect.Surface, error) {
	sigma, morph := r.GlowSize(rad)
	return r.Grown(ctx, cov, g.Whole(morph), effect.GaussianKernel(g.Span(sigma)))
}

// Grown returns a copy of the coverage cov grown by n pixels as a glow
// grows ([Rules.GlowSquare]) and blurred by k, the pixels past the
// bitmap's edge repeating the edge's.
func (r *Rules) Grown(ctx context.Context, cov *effect.Surface, n int, k effect.Kernel) (*effect.Surface, error) {
	out, err := cov.Clone(ctx)
	if err != nil {
		return nil, err
	}
	sq := n / max(r.GlowSquare, 1)
	err = effect.Dilate(ctx, out, sq, sq)
	if err == nil && sq < n {
		err = effect.DilateDisc(ctx, out, float64(n-sq))
	}
	if err == nil {
		err = BlurEdge(ctx, out, k)
	}
	if err != nil {
		out.Release()
		return nil, err
	}
	return out, nil
}

// shadowMatrix returns the map of an outer shadow sh of a source whose
// bounds are b: skewed by kx and ky, then scaled by sx and sy, about the
// point of b its alignment names, then moved dist along dir (ECMA-376
// Part 1, 20.1.8.45 outerShdw), as Word and PowerPoint map it.
//
// # Corpus evidence
//
//   - litho/pptx: effects-shadow's shadow skewed by kx -13.34 degrees and
//     scaled by sy -23% about the bottom left: PowerPoint's soft mask
//     against the skew then the scale rms 2.29 levels (at most 10.3)
//     over the bitmap after the best shift; against the scale then the
//     skew 17.91, against either with the angle negated 21.23 and 13.66,
//     against no skew 16.03.
//   - litho/docx: shapes-effects-3d's shadow skewed by kx 20 degrees and
//     scaled by sy 23% about the bottom right: Word's soft mask against
//     the skew then the scale rms 0.38 levels (at most 1.3) over the
//     bitmap, unshifted; against the scale then the skew 10.05 (48.8).
func shadowMatrix(sh *css.OfficeShadow, b typeset.Rect) typeset.Matrix {
	return mul(translate(offset(sh.Dist, sh.Dir)), skewScale(b, sh.Align, sh.SX, sh.SY, sh.KX, sh.KY))
}

// reflectionMatrix returns the map of a reflection rf of a source whose
// bounds are b, as [shadowMatrix] maps a shadow (ECMA-376 Part 1,
// 20.1.8.50 reflection).
func reflectionMatrix(rf *css.OfficeReflection, b typeset.Rect) typeset.Matrix {
	return mul(translate(offset(rf.Dist, rf.Dir)), skewScale(b, rf.Align, rf.SX, rf.SY, rf.KX, rf.KY))
}

// skewScale returns the map that skews by kx and ky degrees, then scales
// by sx and sy, about the point of b that align names.
func skewScale(b typeset.Rect, align css.RectAlign, sx, sy, kx, ky float64) typeset.Matrix {
	sx, sy = factor(sx), factor(sy)
	tx, ty := tanDeg(kx), tanDeg(ky)
	px, py := b.X+float64(unit(align.X)*b.Width), b.Y+float64(unit(align.Y)*b.Height)
	// x' = sx (x + tx y), y' = sy (ty x + y), about (px, py).
	m := typeset.Matrix{A: sx, B: float64(sy * ty), C: float64(sx * tx), D: sy}
	return mul(translate(px, py), mul(m, translate(-px, -py)))
}

// FadeLine returns where a reflection's fade starts and ends in page
// points: along its fade direction across the copy r, at its start and
// end positions of the copy's extent that way.
func FadeLine(r typeset.Rect, rf *css.OfficeReflection) (x0, y0, x1, y1 float64) {
	sin, cos := sincosDeg(rf.FadeDir)
	cx, cy := r.X+r.Width/2, r.Y+r.Height/2
	// The copy's extent along the direction, from its centre.
	half := (float64(abs(cos)*r.Width) + float64(abs(sin)*r.Height)) / 2
	at := func(pos float64) (float64, float64) {
		t := float64((2*unit(pos) - 1) * half)
		return cx + float64(t*cos), cy + float64(t*sin)
	}
	x0, y0 = at(rf.StartPos)
	x1, y1 = at(rf.EndPos)
	return x0, y0, x1, y1
}

// abs returns |v|.
func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// Grow returns r grown by d on every side.
func Grow(r typeset.Rect, d float64) typeset.Rect {
	return typeset.Rect{X: r.X - d, Y: r.Y - d, Width: r.Width + 2*d, Height: r.Height + 2*d}
}

// MapRect returns the bounds of r mapped by m.
func MapRect(r typeset.Rect, m typeset.Matrix) typeset.Rect {
	return mapRect(r, m)
}
