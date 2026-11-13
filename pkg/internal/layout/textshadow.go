package layout

import (
	"slices"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// textShadows paints the text-shadow of item it of line l under its
// glyphs (CSS Text Decoration 3, 4): each shadow a copy of the glyph run
// and of its decorations in the shadow's colour moved by its offsets, the
// last listed painted first, its underlines and overlines under its
// glyphs and its line-throughs over them as the text paints its own. A
// shadow with a blur radius is drawn as a bitmap instead (see
// blurredTextShadow); one whose glyphs have no outlines keeps the sharp
// copy.
func (p *painter) textShadows(it *lineItem, l *lineBox, page int) {
	shadows := it.box.st.TextShadow
	for _, sh := range slices.Backward(shadows) {
		col, ok := colorOf(sh.Color)
		if !ok || col.A <= 0 {
			continue
		}
		ds := &decoShadow{x: clamp(sh.X), y: clamp(sh.Y), color: col}
		c := *it.text
		c.Origin.X = l.x + it.text.Origin.X + p.dx + ds.x
		c.Origin.Y = l.y + it.text.Origin.Y + p.dy + ds.y
		c.Color = col
		c.Parent = nil
		top, bottom := l.y+it.baseline-it.ascent+ds.y, l.y+it.baseline+it.descent+ds.y
		if blur := max(clamp(sh.Blur), 0); blur > 0 && p.blurredTextShadow(it, l, page, ds, blur, &c, top, bottom) {
			continue
		}
		p.textDecorations(it, l, page, css.Underline|css.Overline, ds)
		p.emit(it.box, true, &c, top, bottom, page)
		p.textDecorations(it, l, page, css.LineThrough, ds)
	}
}

// blurredTextShadow paints the text shadow ds of blur radius blur of item
// it of line l as Chromium prints it, and reports whether it did: the
// coverage of the run's glyph outlines and of its decorations' copies,
// moved by the shadow's offsets, blurred as a box shadow is (see
// blurred), in the shadow's colour, an artifact; then c, the run's copy
// at the shadow's place, invisible, so the text extracts as Chromium's
// does. Chromium writes its copy at a constant alpha of zero; litho
// writes it in text rendering mode 3, which neither fills nor strokes
// (ISO 32000-1, 9.3.6), with no transparency: an extractor reads the copy
// from its operators either way, a text extractor reading every glyph
// shown whatever paints it. A dashed decoration line, which the coverage does not dash, keeps
// its sharp copy. It reports false, painting nothing, for a run whose
// face has no outlines and for a shadow the page's effect pixels cannot
// hold.
//
// # Corpus evidence
//
//   - probe, html: Chromium prints a blurred text shadow as a flat colour
//     through a luminosity soft mask of the blurred glyphs at 300 pixels
//     an inch, then the run again at the shadow's offset under /ca 0; a
//     sharp one as a coloured copy of the run.
func (p *painter) blurredTextShadow(it *lineItem, l *lineBox, page int, ds *decoShadow, blur float64, c *typeset.Text, top, bottom float64) bool {
	ops, _, _ := p.runOutlines(it, l, "shadows")
	if len(ops) == 0 {
		return false
	}
	moved := make([]typeset.PathOp, len(ops))
	for i, op := range ops {
		for j := range op.P {
			op.P[j].X += ds.x
			op.P[j].Y += ds.y
		}
		moved[i] = op
	}
	paths := []effect.Path{{Ops: moved}}
	var decos, sharp []typeset.Fragment
	p.capture = &decos
	p.textDecorations(it, l, page, css.Underline|css.Overline|css.LineThrough, ds)
	p.capture = nil
	for _, f := range decos {
		path, ok := f.(*typeset.Path)
		switch {
		case !ok || path.Stroke != nil && len(path.Stroke.Dash) > 0:
			sharp = append(sharp, f)
		case path.Stroke != nil:
			paths = append(paths, effect.Path{Ops: path.Ops, Width: path.Stroke.Width})
		case path.Fill != nil:
			rule := effect.NonZero
			if path.Fill.Rule == typeset.EvenOdd {
				rule = effect.EvenOdd
			}
			paths = append(paths, effect.Path{Ops: path.Ops, Rule: rule})
		}
	}
	x0, y0, x1, y1 := opsBounds(moved)
	for _, q := range paths[1:] {
		if len(q.Ops) == 0 {
			continue
		}
		a, b, cc, d := opsBounds(q.Ops)
		w := float64(q.Width / 2)
		x0, y0, x1, y1 = min(x0, a-w), min(y0, b-w), max(x1, cc+w), max(y1, d+w)
	}
	area := typeset.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
	img := p.blurred(paths, nil, area, blur, c.Color, top)
	if img == nil {
		return false
	}
	p.emit(it.box, true, img, min(top, img.Rect.Y), max(bottom, img.Rect.Y+img.Rect.Height), page)
	for _, f := range sharp {
		p.emit(it.box, true, f, top, bottom, page)
	}
	c.Invisible, c.Stroke = true, 0
	p.emit(it.box, true, c, top, bottom, page)
	return true
}
