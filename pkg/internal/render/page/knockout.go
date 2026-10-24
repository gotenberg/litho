package page

import (
	"image"

	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/render/blend"
	"github.com/gotenberg/litho/pkg/internal/render/raster"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// textKnockoutShare bounds the glyph coverage one text object holds back for
// its knockout: its pixels may reach 1/textKnockoutShare of the render pixel
// budget, past which the glyphs held so far are painted and the object goes
// on as a new one. Anti-DoS, not spec-mandated: ISO 32000-1, 9.3.8 makes a
// whole text object one element, so its glyphs wait for ET, and a text
// object of a million transparent glyphs would otherwise hold them all. A
// quarter of the budget holds far more glyphs than a text object of a real
// page shows.
const textKnockoutShare = 4

// knockGlyph is one glyph fill or stroke a text object holds back: its
// coverage in device pixels, its colour in sRGB and the effects it paints
// with.
type knockGlyph struct {
	cov   *raster.Coverage
	color solid
	fx    effects
}

func coverageRect(cov *raster.Coverage) image.Rectangle {
	return image.Rect(cov.X, cov.Y, cov.X+cov.Width, cov.Y+cov.Height)
}

// glyphCoverage paints the coverage of a glyph's fill or stroke in colour c
// at constant alpha a, under the text knockout parameter (ISO 32000-1,
// 9.3.8): when TK is true, the glyphs of a text object are one element whose
// overlapping glyphs knock each other out rather than composite with one
// another, so a transparent or blended glyph is held back until the object
// ends ([renderer.flushText]). An opaque Normal glyph with nothing held back
// knocks out what lies under it by painting over it, and paints at once. A
// glyph in a pattern colour is painted on its own, outside the knockout.
func (r *renderer) glyphCoverage(s *scope, st *graphics.State, cov *raster.Coverage, c graphics.Color, a float64, stroke bool) error {
	if cov == nil || cov.Empty() {
		return nil
	}
	fx := r.effectsOf(st, a)
	direct := len(s.text.knock) == 0 && !fx.blends() && fx.opacity == surface.Opaque
	if !st.TextKnockout || c.Space.Family == graphics.FamilyPattern || direct {
		if err := r.flushText(s); err != nil {
			return err
		}
		return r.paintCoverage(s, cov, c, a, stroke)
	}
	conv, paints := r.colorFor(s, c.Space)
	if !paints {
		return nil
	}
	cr, cg, cb := conv.RGB(c.Components)
	sol := r.solidOf(s, c, [3]float32{cr, cg, cb})
	// The held glyphs composite into a knockout group of their own, which
	// keeps no spot colorant planes.
	fx.keep, fx.spots = r.overprintPaint(s, st, c, stroke, &sol, false)
	s.text.knock = append(s.text.knock, knockGlyph{cov: cov, color: sol, fx: fx})
	s.text.held += int64(cov.Width) * int64(cov.Height)
	if s.text.held > r.budget/textKnockoutShare {
		return r.flushText(s)
	}
	return nil
}

// flushText paints the glyphs a text object held back as one element (ISO
// 32000-1, 9.3.8): each glyph composited into a non-isolated knockout group
// with its own blend mode, alpha and soft mask, so it composites with the
// group's backdrop rather than with the glyphs before it (11.4.6), and the
// group then composited into the one being painted with Normal, opaque.
//
// poppler 26.09.0 does not implement text knockout: where transparent glyphs
// of one text object overlap, it composites them with each other.
func (r *renderer) flushText(s *scope) error {
	held := s.text.knock
	if len(held) == 0 {
		return nil
	}
	s.text.knock, s.text.held = nil, 0
	if len(held) == 1 {
		// One glyph knocks nothing out.
		return r.paintSolid(held[0].cov, held[0].color, held[0].fx)
	}
	top := r.top()
	var bounds image.Rectangle
	for _, k := range held {
		bounds = bounds.Union(coverageRect(k.cov))
	}
	bounds = bounds.Intersect(top.group.Bounds())
	if bounds.Empty() {
		return nil
	}
	g, release, err := r.newGroup(bounds, top.group.Backdrop(), blend.Attributes{Knockout: true})
	if err != nil {
		return err
	}
	if g == nil {
		r.report.count(TransparencyOverBudget)
		for _, k := range held {
			if err := r.paintSolid(k.cov, k.color, k.fx); err != nil {
				return err
			}
		}
		return nil
	}
	defer release()
	for _, k := range held {
		if k.fx.opacity.Transparent() {
			continue
		}
		mask, err := r.softMask(k.fx.mask)
		if err != nil {
			return err
		}
		if err := g.PaintCoverage(r.ctx, coverageRect(k.cov), k.cov.A, k.color.in, k.fx.params(mask)); err != nil {
			return err
		}
	}
	return g.Into(r.ctx, top.group, blend.Params{})
}
