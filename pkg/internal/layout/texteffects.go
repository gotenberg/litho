package layout

import (
	"errors"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/officefx"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// maxEffectGlyphs bounds the glyphs of one run whose outlines an Office
// text effect reads: past it the run's effects are left out, with a
// warning. Anti-DoS guard, not spec-mandated: a run of a paragraph holds
// a few hundred glyphs.
const maxEffectGlyphs = 4096

// textEffects paints the Office text effects of item it of line l under
// its glyphs (css.Office.TextEffects): the bitmaps package officefx draws
// from the run's glyph outlines, artifacts, the glyphs staying text above
// them. The run's box for the shadow's and the reflection's alignment
// spans its font's ascent and descent and ends at the baseline, so a
// reflection starts at the baseline and fades over the font's height.
//
// # Corpus evidence
//
//   - litho/docx run-w14-effects: Word's reflections of 24 point Calibri
//     runs start 0.6 point above the baseline (their blur's margin), the
//     mirrored descenders cut off, and fade to their end opacity over
//     10.9 points at w14:endPos 45.5%: 45.5% of the font's 24 point
//     ascent and descent.
func (p *painter) textEffects(it *lineItem, l *lineBox, page int, alpha float64) {
	fx := it.box.st.TextEffects
	t := it.text
	if fx == nil || t.Face == nil || len(t.Glyphs) == 0 {
		return
	}
	y := l.y + it.baseline
	if len(t.Glyphs) > maxEffectGlyphs {
		p.e.warnOnce(warn.LimitReached, warn.Omitted, fmt.Sprintf("the effects of a run of more than %d glyphs are not drawn", maxEffectGlyphs), y)
		return
	}
	f := p.outlineFace(t.Face)
	if f == nil || f.Upem() == 0 {
		return
	}
	k := t.Size / float64(f.Upem())
	hs := hscale(t)
	ox, oy := l.x+t.Origin.X+p.dx, l.y+t.Origin.Y+p.dy
	var ops []typeset.PathOp
	for _, g := range t.Glyphs {
		ops = appendGlyph(ops, f, g.GID, ox+g.X*hs, oy+g.Y, k, hs, t.Skew)
	}
	if len(ops) == 0 {
		return
	}
	col := t.Color
	col.A *= alpha
	// The ink's horizontal extent: the points the outlines pass through
	// or bend towards.
	x0, x1 := ops[0].P[0].X, ops[0].P[0].X
	for _, op := range ops {
		n := 1
		switch op.Kind {
		case typeset.ClosePath:
			n = 0
		case typeset.CurveTo:
			n = 3
		}
		for _, q := range op.P[:n] {
			x0, x1 = min(x0, q.X), max(x1, q.X)
		}
	}
	box := typeset.Rect{X: x0, Y: oy - it.ascent - it.descent, Width: max(x1-x0, 0), Height: it.ascent + it.descent}
	src := &officefx.Source{Paths: []effect.Path{{Ops: ops}}, Bounds: box, Color: col, Text: true}
	frags, err := officefx.Under(p.e.ctx, src, fx, &p.effectPixels)
	switch {
	case errors.Is(err, officefx.ErrPagePixels):
		p.e.warnOnce(warn.LimitReached, warn.Omitted, fmt.Sprintf("text effects past %d pixels are not drawn", effect.MaxPagePixels), y)
	case err != nil && limits.Stops(err):
		p.e.fail(fmt.Errorf("drawing text effects: %w", err))
		return
	case err != nil:
		p.e.warnOnce(warn.ContentUnsupported, warn.Omitted, fmt.Sprintf("text effects are not drawn: %v", err), y)
		return
	}
	top, bottom := y-it.ascent, y+it.descent
	for _, fr := range frags {
		p.emit(it.box, true, fr, top, bottom, page)
	}
}
