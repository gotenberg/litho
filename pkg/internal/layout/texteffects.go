package layout

import (
	"errors"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/officefx"
	drawshape "github.com/gotenberg/litho/pkg/internal/shape"
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
// them. The run's box for the shadow's and the reflection's alignment is
// its em, the font size, ending at the baseline, so a reflection starts
// at the baseline and fades over the font size.
//
// # Corpus evidence
//
//   - litho/docx run-w14-effects: Word's reflections of 24 point Calibri
//     runs start 0.6 point above the baseline (their blur's margin), the
//     mirrored descenders cut off, and fade to their end opacity over
//     10.9 points at w14:endPos 45.5%: 45.5% of the 24 point em.
func (p *painter) textEffects(it *lineItem, l *lineBox, page int, alpha float64) {
	fx := it.box.st.TextEffects
	t := it.text
	if fx == nil || t.Face == nil || len(t.Glyphs) == 0 {
		return
	}
	y := l.y + it.baseline
	ops, x0, x1 := p.runOutlines(it, l, "effects")
	if len(ops) == 0 {
		return
	}
	col := t.Color
	col.A *= alpha
	oy := l.y + t.Origin.Y + p.dy
	box := typeset.Rect{X: x0, Y: oy - t.Size, Width: max(x1-x0, 0), Height: t.Size}
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

// runOutlines returns the glyph outlines of item it of line l in the
// painter's coordinates and their ink's horizontal extent; none for a
// run past maxEffectGlyphs, with a warning naming what, or for a face
// without outlines.
func (p *painter) runOutlines(it *lineItem, l *lineBox, what string) ([]typeset.PathOp, float64, float64) {
	t := it.text
	if len(t.Glyphs) > maxEffectGlyphs {
		p.e.warnOnce(warn.LimitReached, warn.Omitted, fmt.Sprintf("the %s of a run of more than %d glyphs are not drawn", what, maxEffectGlyphs), l.y+it.baseline)
		return nil, 0, 0
	}
	f := p.outlineFace(t.Face)
	if f == nil || f.Upem() == 0 {
		return nil, 0, 0
	}
	k := t.Size / float64(f.Upem())
	hs := hscale(t)
	ox, oy := l.x+t.Origin.X+p.dx, l.y+t.Origin.Y+p.dy
	var ops []typeset.PathOp
	for _, g := range t.Glyphs {
		ops = appendGlyph(ops, f, g.GID, ox+g.X*hs, oy+g.Y, k, hs, t.Skew)
	}
	if len(ops) == 0 {
		return nil, 0, 0
	}
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
	return ops, x0, x1
}

// textPaint paints the run c of item it of line l as its
// css.Office.TextPaint says, and reports whether it painted the run: the
// glyphs' text at invisibleOpacity when they are unfilled or filled with
// a gradient, then the gradient filling their outlines over their ink's
// box, then the outlines stroked, both artifacts. It paints nothing and
// reports false for a run without one, which the caller paints.
//
// # Corpus evidence
//
//   - litho/docx run-w14-effects: Word draws "Outline only" and "Gradient
//     fill" as their glyph outlines, stroked or filled with a shading,
//     marked Artifact, over the text painted at opacity 0.
//   - litho/docx run-effects: Word strokes the outlines of a run of
//     w:outline 0.25 point wide in black, unfilled.
func (p *painter) textPaint(it *lineItem, l *lineBox, page int, c *typeset.Text, top, bottom float64) bool {
	tp := it.box.st.TextPaint
	if tp == nil {
		return false
	}
	ops, x0, x1 := p.runOutlines(it, l, "outlines")
	if len(ops) == 0 {
		return false
	}
	alpha := c.Color.A
	if alpha <= 0 {
		alpha = 1
	}
	hidden := tp.NoFill || len(tp.Gradient) >= 2
	if hidden {
		if c.Color.A <= 0 {
			c.Color = typeset.Black
		}
		p.emit(it.box, true, &typeset.Group{Opacity: invisibleOpacity, Transform: typeset.Identity, Content: []typeset.Fragment{c}}, top, bottom, page)
	} else if c.Color.A > 0 {
		p.emit(it.box, true, c, top, bottom, page)
	}
	if len(tp.Gradient) >= 2 {
		stops := make([]typeset.ColorStop, 0, len(tp.Gradient))
		for _, s := range tp.Gradient {
			col, ok := colorOf(s.Color)
			col.A *= alpha
			if !ok {
				col = typeset.Color{}
			}
			stops = append(stops, typeset.ColorStop{Offset: s.Pos, Color: col})
		}
		y0, y1 := inkRows(ops)
		box := typeset.Rect{X: x0, Y: y0, Width: max(x1-x0, 0), Height: max(y1-y0, 0)}
		for _, f := range drawshape.Fill(ops, box, drawshape.Paint{Kind: drawshape.PaintGradient, Stops: stops, Angle: tp.Angle}) {
			p.emit(it.box, true, artifact(f), top, bottom, page)
		}
	}
	if o := tp.Outline; o != nil && numeric.PositiveFinite(o.Width) {
		if col, ok := colorOf(o.Color); ok && col.A*alpha > 0 {
			col.A *= alpha
			p.emit(it.box, true, &typeset.Path{Ops: ops, Stroke: &typeset.Stroke{Color: col, Width: min(o.Width, c.Size), Join: typeset.RoundJoin}}, top, bottom, page)
		}
	}
	return true
}

// invisibleOpacity is the opacity of the text of glyphs painted by their
// outlines: the text stays in the page, extracted and tagged, as Word
// keeps it at opacity 0, which the page model refuses. Over any backdrop
// it moves an 8-bit pixel by under a quarter of a level.
const invisibleOpacity = 1.0 / 1024

// artifact returns f with no structure parent.
func artifact(f typeset.Fragment) typeset.Fragment {
	switch v := f.(type) {
	case *typeset.Path:
		v.Parent = nil
	case *typeset.Image:
		v.Parent = nil
	case *typeset.Text:
		v.Parent = nil
	}
	return f
}

// inkRows returns the vertical extent of the points of ops.
func inkRows(ops []typeset.PathOp) (float64, float64) {
	y0, y1 := ops[0].P[0].Y, ops[0].P[0].Y
	for _, op := range ops {
		n := 1
		switch op.Kind {
		case typeset.ClosePath:
			n = 0
		case typeset.CurveTo:
			n = 3
		}
		for _, q := range op.P[:n] {
			y0, y1 = min(y0, q.Y), max(y1, q.Y)
		}
	}
	return y0, y1
}
