package layout

import (
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
// by the rules the effects name, artifacts, the glyphs staying text above
// them. Word's rules draw them from the run's glyph outlines at lengths
// scaled by the font size, the run's box for the shadow's and the
// reflection's alignment its em ending at the baseline
// ([officefx.WordRunReach] says how far they reach); PowerPoint's from
// the glyphs as they paint.
func (p *painter) textEffects(it *lineItem, l *lineBox, page int) {
	fx := it.box.st.TextEffects
	t := it.text
	if fx == nil || t.Face == nil || len(t.Glyphs) == 0 {
		return
	}
	y := l.y + it.baseline
	src := &officefx.Source{Color: t.Color, Text: true, Size: t.Size}
	if officefx.FromGlyphs(fx) {
		glyphs := *t
		glyphs.Origin = typeset.Point{X: l.x + t.Origin.X + p.dx, Y: l.y + t.Origin.Y + p.dy}
		glyphs.Parent = nil
		src.Glyphs = &glyphs
	} else {
		ops, x0, x1 := p.runOutlines(it, l, "effects")
		if len(ops) == 0 {
			return
		}
		oy := l.y + t.Origin.Y + p.dy
		src.Paths = []effect.Path{{Ops: ops}}
		src.Bounds = typeset.Rect{X: x0, Y: oy - t.Size, Width: max(x1-x0, 0), Height: t.Size}
	}
	pixels := &p.effectPixels
	if p.e.opts.EffectPixels != nil {
		pixels = p.e.opts.EffectPixels
	}
	frags, notes, err := officefx.Under(p.e.ctx, src, fx, pixels)
	switch {
	case err != nil && limits.Stops(err):
		p.e.fail(fmt.Errorf("drawing text effects: %w", err))
		return
	case err != nil:
		p.e.warnOnce(warn.ContentUnsupported, warn.Omitted, fmt.Sprintf("text effects are not drawn: %v", err), y)
		return
	}
	if notes.Skipped {
		p.e.warnOnce(warn.LimitReached, warn.Omitted, fmt.Sprintf("text effects past %d pixels are not drawn", effect.MaxPagePixels), y)
	}
	if notes.Reduced {
		p.e.warnOnce(warn.LimitReached, warn.Approximated, fmt.Sprintf("text effects past %d pixels a bitmap are drawn at a lower resolution", effect.MaxPixels), y)
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
		ops = appendGlyph(ops, f, g.GID, ox+float64(g.X*hs), oy+g.Y, k, hs, t.Skew)
	}
	if len(ops) == 0 {
		return nil, 0, 0
	}
	x0, _, x1, _ := opsBounds(ops)
	return ops, x0, x1
}

// textPaint paints the run c of item it of line l as its
// css.Office.TextPaint says, and reports whether it painted the run: the
// glyphs' text invisible (typeset.Text.Invisible, text rendering mode 3)
// when they are unfilled or filled with a gradient, so the text is
// extracted and tagged once while its outlines show it, then the gradient
// filling their outlines over their ink's box, then the outlines stroked,
// both artifacts. It paints nothing and reports false for a run without
// one, which the caller paints.
//
// # Corpus evidence
//
//   - litho/docx run-w14-effects: Word draws "Outline only" and "Gradient
//     fill" as their glyph outlines, stroked or filled with a shading,
//     marked Artifact, over the text painted at opacity 0.
//   - litho/docx run-w14-effects and shapes-wordart, libreoffice/docx
//     ooxmlexport/TextEffects_TextFill: converted to the fourteen PDF/A and
//     PDF/UA targets, veraPDF passes every one with the text invisible as
//     with it at opacity 1/1024, and pdftotext reads the same text;
//     shapes-wordart's PDF/A-1b file falls from 133.7 to 52.7 KB, the
//     run no longer a transparency to flatten.
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
		c.Invisible, c.Stroke = true, 0
		p.emit(it.box, true, c, top, bottom, page)
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
		_, y0, _, y1 := opsBounds(ops)
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

// opsBounds returns the extent of the points ops pass through or bend
// towards, ops not empty.
func opsBounds(ops []typeset.PathOp) (x0, y0, x1, y1 float64) {
	x0, y0, x1, y1 = ops[0].P[0].X, ops[0].P[0].Y, ops[0].P[0].X, ops[0].P[0].Y
	for _, op := range ops {
		n := 1
		switch op.Kind {
		case typeset.MoveTo, typeset.LineTo:
		case typeset.ClosePath:
			n = 0
		case typeset.CurveTo:
			n = 3
		}
		for _, q := range op.P[:n] {
			x0, y0, x1, y1 = min(x0, q.X), min(y0, q.Y), max(x1, q.X), max(y1, q.Y)
		}
	}
	return x0, y0, x1, y1
}
