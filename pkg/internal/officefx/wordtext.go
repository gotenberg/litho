package officefx

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// wordScaleSize is the font size, in points, at which Word draws the glow
// and the outer shadow of a run of text at the lengths they state: see
// [wordScale].
const wordScaleSize = 74

// maxFontSize caps the font size the effects of a run of Word's text are
// scaled by, in points. Anti-DoS guard, not spec-mandated: Word sets no
// font size past 1638 points, and a larger value scales the effects past
// [MaxRadius] anyway.
const maxFontSize = 1638

// wordScale returns the factor Word scales the lengths of a run's w14
// glow and outer shadow by (MS-DOCX 2.6.1.4, 2.6.1.27) at font size size,
// (size/74)^(2/3): the glow's radius, the shadow's distance and its blur
// radius, which the shapes' rules then draw. A size that is not positive
// and finite scales nothing.
//
// # Corpus evidence
//
//   - probe, docx: Calibri runs at 8, 10, 12, 14, 16, 18, 20, 24, 28, 32,
//     36, 40, 48, 60 and 72 points, each with a shadow 10 points down and
//     no blur: Word draws the shadow as the glyphs moved 2.26, 2.56, 2.90,
//     3.21, 3.53, 3.82, 4.13, 4.70, 5.18, 5.70, 6.04, 6.62, 7.51, 8.79 and
//     9.84 points (the centroid of its soft mask against the centroid of
//     the glyphs' ink), unscaled; 10 (size/74)^(2/3) gives 2.27, 2.63,
//     2.97, 3.30, 3.60, 3.90, 4.18, 4.72, 5.23, 5.72, 6.19, 6.64, 7.49,
//     8.70 and 9.82, within 0.15 points, under a pixel at Word's 288
//     pixels an inch. A power of the size fitted on the fifteen gives the
//     exponent 0.669; the 2/3 power puts the full lengths at 74.4 points,
//     at 72 points (an inch) every offset is drawn 2% long. A factor
//     linear in the size, the em or the line height, would move the
//     shadow 9 times as far at 72 points as at 8; Word moves it 4.35
//     times. The square root of the size gives 3; a logarithm of it
//     misses 24 points by 0.14 of the distance. The offsets are not whole
//     pixels at 288 pixels an inch.
//   - probe, docx: at 24 points, shadows 2, 5, 20 and 40 points down are
//     moved 0.94, 2.34, 9.17 and 18.56 points, the rule 0.94, 2.36, 9.44
//     and 18.88: linear in the distance.
//   - probe, docx: glows of 8 points at the fifteen sizes, of 4, 8 and 16
//     points at 12, 24 and 48 points and of 2, 5, 11 and 18 points at 24
//     points, against the glyphs' ink grown by a third of a square then a
//     disc and blurred, on Word's grid: the best growth and deviation are
//     0.45 and a sixth of the scaled radius within a quarter point at
//     every size (8 points at 24: 1.75 and 0.64 for 1.70 and 0.63; at 72:
//     3.50 and 1.36 for 3.53 and 1.31), rms 1.2 to 3.3 levels of 255; a
//     glow of 2 to 5 points at 24 or less draws its blur a pixel wider
//     than a sixth.
//   - probe, docx: shadows blurred 8 points at 12, 24 and 48 points fit a
//     Gaussian of 0.80, 1.25 and 2.05 points (a third of the scaled
//     radius: 0.79, 1.26, 2.00), one blurred 4 points at 24 points 0.65
//     (0.63), rms 0.5 to 1.6 levels.
//   - probe, docx: Times New Roman at 24 points draws the shadow and the
//     glow as Calibri does. Runs of one size whose text is three times as
//     long draw them alike: the factor follows the size alone.
//   - probe, docx: the sizes above 72 points and below 8 are not measured.
func wordScale(size float64) float64 {
	if !numeric.PositiveFinite(size) {
		return 1
	}
	q := min(size, maxFontSize) / wordScaleSize
	return cbrt(float64(q * q))
}

// cbrt returns the cube root of a > 0 by Newton's iteration from above,
// every product rounded on its own, so every machine computes the same
// bits.
func cbrt(a float64) float64 {
	y := max(a, 1)
	for range 200 {
		next := (float64(2*y) + a/float64(y*y)) / 3
		if next >= y {
			break
		}
		y = next
	}
	return y
}

// wordReflectionDist is the font size, in points, a w14 reflection's
// distance is scaled by: Word mirrors a run of size s whose reflection
// lies d points away about a line d s/28 points below its baseline, the
// copy d s/14 points down. See [wordReflection].
const wordReflectionDist = 14

// wordRunBox is the height of the box of a run of Word's text that a
// reflection mirrors and fades over, a share of the font size, from the
// baseline up.
//
// # Corpus evidence
//
//   - probe, docx: Calibri reflections fading from 55% at the baseline to
//     0.3% at 45.5% of the box, and from 50% to 0.3% at 90%, at 12, 24 and
//     48 points, along the stem of the mirrored H: the fades end 0.93 to
//     0.96 sizes down at the end position; Times New Roman's at 24 points
//     end where Calibri's do.
//   - probe, docx: the same reflections grow the line below by 1.08 times
//     the end position of 0.96 to 0.97 sizes less the line's descent.
const wordRunBox = 0.95

// wordGlowReach is how far a glow of a run of Word's text grows the line
// past the run's ascent and descent, a share of its scaled radius
// ([wordScale]).
//
// # Corpus evidence
//
//   - probe, docx: glows of 2, 4, 5, 8, 11, 16 and 18 points at 24
//     points grow the line above by 0.75, 1.51, 1.76, 3.00, 4.00, 5.75 and
//     6.26 points, of 4, 8 and 16 points at 12 points by 0.99, 1.73 and
//     3.48, at 48 points by 2.48, 4.75 and 8.99: 0.73 of the scaled radius
//     rounded up to a quarter point gives every one at 24 points. Taken
//     unrounded, 0.74 leaves 128 of the four probes' 168 growths above and
//     below their lines within a quarter point of Word's and 159 within
//     half a point under the rules of [WordRunReach], 0.235 points rms;
//     0.72 and 0.76 leave more past 0.3 points.
const wordGlowReach = 0.74

// WordRunReach returns how far the effects fx of a run of Word's text
// reach above its ascent and below its descent, in points, none past
// either, the run's font size and descent given: the room Word adds to
// the line that holds the run, which a proportional line spacing then
// scales as it scales the glyphs'. A glow reaches [wordGlowReach] of its
// scaled radius ([wordScale]) every way; an outer shadow its scaled blur
// radius past where its scaled distance moves the run, from the glow's
// reach when the run has both; a reflection the end of its fade below the
// baseline and its scaled blur radius, past the descent. fx on
// PowerPoint's rules reaches nothing.
//
// # Corpus evidence
//
//   - probe, docx: 24 point Calibri lines at 259/240 with a shadow 10
//     points down grow 5.02 points below, 10 points up 4.75 above and 0.25
//     below (the spacing's share of the room above); a shadow blurred 8
//     points grows them 3.76 above and 4.25 below at 24 points, 2.24 and
//     2.52 at 12, 5.99 and 6.98 at 48; one blurred 4 points and 10 down
//     0.00 and 7.00.
//   - probe, docx: a glow of 8 points, a shadow blurred 4 points 3 points
//     to the lower right and a reflection to 45.5% on one 24 point run grow
//     the line 3.76 above and 6.25 below. The greatest of the three
//     effects' reaches gives 2.79 and 4.71, their sum 3.68 and 10.9, the
//     shadow cast from the glyphs over their glow 3.68 and 6.42.
//   - probe, docx: reflections 4 and 8 points away at 24 points grow the
//     line 11.77 and 18.52 below at 45.5%, 29.51 at 90%; 8 points away at
//     12 points 9.28.
func WordRunReach(fx *css.OfficeEffects, size, descent float64) (above, below float64) {
	if fx == nil || fx.Rules != css.OfficeWord || !numeric.PositiveFinite(size) {
		return 0, 0
	}
	k := wordScale(size)
	var glow float64
	if g := fx.Glow; g != nil && g.Color.A > 0 {
		glow = float64(wordGlowReach * float64(k*radius(g.Radius)))
		above, below = glow, glow
	}
	if sh := fx.Shadow; sh != nil && sh.Color.A > 0 {
		s, _ := sincosDeg(sh.Dir)
		dy := float64(s * float64(k*radius(sh.Dist)))
		b := float64(k * radius(sh.Blur))
		above = max(above, glow+b-dy)
		below = max(below, glow+b+dy)
	}
	if rf := fx.Reflection; rf != nil && (rf.StartAlpha > 0 || rf.EndAlpha > 0) {
		end := wordReflectionShift(rf, size) + float64(unit(rf.EndPos)*float64(wordRunBox*size)) + float64(k*radius(rf.Blur))
		below = max(below, end-max(descent, 0))
	}
	return max(above, 0), max(below, 0)
}

// wordReflectionShift returns how far down Word moves the mirrored copy
// of a run of size points for the reflection rf: its distance times the
// size over [wordReflectionDist], the part of it along rf's direction
// that points down.
func wordReflectionShift(rf *css.OfficeReflection, size float64) float64 {
	s, _ := sincosDeg(rf.Dir)
	return max(float64(s*float64(radius(rf.Dist)*min(size, maxFontSize)))/wordReflectionDist, 0)
}

// wordEffects returns the effects of a run of Word's text of the given
// font size, their lengths scaled as Word draws them ([wordScale]).
func wordEffects(fx *css.OfficeEffects, size float64) (glow *css.OfficeGlow, shadow *css.OfficeShadow, refl *css.OfficeReflection) {
	k := wordScale(size)
	if g := fx.Glow; g != nil {
		c := *g
		c.Radius = float64(k * radius(g.Radius))
		glow = &c
	}
	if sh := fx.Shadow; sh != nil {
		c := *sh
		c.Dist = float64(k * radius(sh.Dist))
		c.Blur = float64(k * radius(sh.Blur))
		shadow = &c
	}
	if rf := fx.Reflection; rf != nil {
		c := *rf
		c.Blur = float64(k * radius(rf.Blur))
		refl = &c
	}
	return glow, shadow, refl
}

// wordRun returns the bitmaps of the effects fx of src, a run of Word's
// text ([Source.Text]), in paint order: the reflection, the outer shadow,
// the glow. The lengths are scaled by the run's size ([wordScale]) and
// drawn by Word's rules for a shape's ([Word]); the shadow is cast by the
// glyphs over their glow, and the reflection mirrors the glyphs over their
// glow and shadow ([wordReflection]).
//
// # Corpus evidence
//
//   - probe, docx: a 24 point run with a glow of 8 points, a shadow
//     blurred 4 points 3 points to the lower right and a reflection: Word's
//     shadow against the glyphs moved and blurred rms 9.3 levels, against
//     the glyphs over their glow at 0.4 moved and blurred 3.2; its
//     reflection shows the glow around the mirrored stems, a level of 50
//     around the stems' 111, and on their right side the shadow.
func wordRun(ctx context.Context, src *Source, fx *css.OfficeEffects, pixels *effect.Page) ([]typeset.Fragment, Notes, error) {
	glowFx, shadowFx, reflFx := wordEffects(fx, src.Size)
	var notes Notes
	var glowF, shadowF typeset.Fragment
	if glowFx != nil {
		f, n, err := glow(ctx, src, glowFx, &Word, pixels)
		if err != nil {
			return nil, Notes{}, err
		}
		glowF, notes = f, notes.join(n)
	}
	glyphs := &typeset.Path{Ops: pathOps(src.Paths), Fill: &typeset.Fill{Color: typeset.Color{A: 1}}}
	if shadowFx != nil {
		var f typeset.Fragment
		var n Notes
		var err error
		if glowF != nil {
			f, n, err = contentShadow(ctx, []typeset.Fragment{glowF, glyphs}, src.Bounds, shadowFx, pixels)
		} else {
			f, n, err = shadow(ctx, src, shadowFx, pixels)
		}
		if err != nil {
			return nil, Notes{}, err
		}
		shadowF, notes = f, notes.join(n)
	}
	var out []typeset.Fragment
	if reflFx != nil {
		rsrc := *src
		if glowF != nil || shadowF != nil {
			colored := *glyphs
			colored.Fill = &typeset.Fill{Color: src.Color}
			for _, f := range []typeset.Fragment{shadowF, glowF, &colored} {
				if f != nil {
					rsrc.Content = append(rsrc.Content, f)
				}
			}
		}
		f, n, err := wordReflection(ctx, &rsrc, reflFx, pixels)
		if err != nil {
			return nil, Notes{}, err
		}
		notes = notes.join(n)
		if f != nil {
			out = append(out, f)
		}
	}
	for _, f := range []typeset.Fragment{shadowF, glowF} {
		if f != nil {
			out = append(out, f)
		}
	}
	return out, notes, nil
}

// wordReflection draws the reflection rf of a run of Word's text, its
// blur already scaled: the run's box, [wordRunBox] sizes up from the
// baseline, grown down to a line half the copy's shift below the
// baseline ([wordReflectionShift]), mirrored about that line, so the copy
// lies the whole shift down, faded over the grown box's extent and cut
// off above the line; what is left of the shift along rf's direction
// moves the copy as a shape's reflection moves.
//
// # Corpus evidence
//
//   - probe, docx: reflections 4 and 8 points away of 24 point Calibri
//     put the mirrored baseline (the top of the mirrored H's stems) 7.0
//     and 13.69 points below the baseline, 8 points away at 12 and 48
//     points 6.73 and 27.4: the distance times the size over 14.0 to 14.3.
//     The mirrored descenders show below half that shift and are cut off
//     above it (3.27 points down at 4 points away), and the fade runs from
//     half the shift to the copy's end: 8 points away at 24 points, 61 of
//     126 levels at the top of the stems, falling 4.7 levels a point
//     where the reflection at no distance falls 6.2.
//   - probe, docx: the reflections at no distance start at the baseline,
//     the mirrored descenders cut off.
func wordReflection(ctx context.Context, src *Source, rf *css.OfficeReflection, pixels *effect.Page) (typeset.Fragment, Notes, error) {
	shift := wordReflectionShift(rf, src.Size)
	b := src.Bounds
	base := b.Y + b.Height
	if h := float64(wordRunBox * src.Size); numeric.PositiveFinite(h) {
		b.Y, b.Height = base-h, h
	}
	b.Height += shift / 2
	// The mirror about the grown box's bottom moves the copy the shift
	// down; the rest of the distance along the direction moves it as it
	// moves a shape's.
	sin, cos := sincosDeg(rf.Dir)
	dist := float64(radius(rf.Dist)*min(src.Size, maxFontSize)) / wordReflectionDist
	dx, dy := float64(dist*cos), float64(dist*sin)-shift
	m := mul(translate(dx, dy), skewScale(b, rf.Align, rf.SX, rf.SY, rf.KX, rf.KY))
	fade := mapRect(b, m)
	area := fade
	if src.Content != nil {
		// The glow and the shadow reach past the glyphs: the copy shows
		// them, cut off above the mirror line.
		whole := b
		for _, f := range src.Content {
			if img, ok := f.(*typeset.Image); ok {
				whole = union(whole, img.Rect)
			}
		}
		area = mapRect(whole, m)
		if top := fade.Y; area.Y < top {
			area.Height -= top - area.Y
			area.Y = top
		}
	}
	return reflect(ctx, src, rf, m, area, fade, pixels)
}

// contentShadow draws an outer shadow sh of content, whose alignment box
// is bounds: what the content paints, as one silhouette whose coverage is
// its alpha, mapped by the shadow, blurred by a Gaussian of a third of its
// blur radius, in its colour.
func contentShadow(ctx context.Context, content []typeset.Fragment, bounds typeset.Rect, sh *css.OfficeShadow, pixels *effect.Page) (typeset.Fragment, Notes, error) {
	col, ok := opaqueEnough(sh.Color)
	if !ok {
		return nil, Notes{}, nil
	}
	m := shadowMatrix(sh, bounds)
	sigma := radius(sh.Blur) / 3
	area := bounds
	for _, f := range content {
		if img, ok := f.(*typeset.Image); ok {
			area = union(area, img.Rect)
		}
	}
	res, grid, notes := planArea(mapRect(area, m), sigma, 3*sigma, pixels)
	if res.W == 0 {
		return nil, notes, nil
	}
	ss, err := effect.Paint(ctx, []effect.Content{{Fragments: content, Transform: m, Grid: grid, W: res.W, H: res.H}})
	if err != nil {
		return nil, Notes{}, err
	}
	cov, err := effect.AlphaOf(ctx, ss[0])
	ss[0].Release()
	if err != nil {
		return nil, Notes{}, err
	}
	defer cov.Release()
	if err := blur(ctx, cov, sigma); err != nil {
		return nil, Notes{}, err
	}
	f, err := tinted(ctx, cov, col)
	return f, notes, err
}

// pathOps returns the steps of paths as one path.
func pathOps(paths []effect.Path) []typeset.PathOp {
	var ops []typeset.PathOp
	for _, p := range paths {
		ops = append(ops, p.Ops...)
	}
	return ops
}

// union returns the smallest rectangle holding a and b.
func union(a, b typeset.Rect) typeset.Rect {
	x0, y0 := min(a.X, b.X), min(a.Y, b.Y)
	x1, y1 := max(a.X+a.Width, b.X+b.Width), max(a.Y+a.Height, b.Y+b.Height)
	return typeset.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}
