package layout

import (
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// strokeMiter is the miter limit of a text stroke: Skia's default, 4,
// which Chromium strokes text with.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 strokes "MAW" in
//     Liberation Serif at 120px with a 12px stroke with mitred corners, the
//     apex of the A drawn as a short spike and the sharper bottom corners
//     of the W cut flat, on screen and in print.
const strokeMiter = 4

// textStroke returns the pen -webkit-text-stroke strokes the glyphs of
// style st with (Compatibility Standard, 3.4.6), nil for none or for a
// transparent colour, and whether paint-order paints it before the fill
// (SVG 2, 13.8). The line is centred on the glyph outlines and mitred, as
// Chromium strokes it; its width is bounded to what a text run takes.
func textStroke(st *css.Style) (*typeset.Stroke, bool) {
	w, c, first := st.TextStroke()
	w = min(clamp(w), typeset.MaxOutlineWidth)
	if w <= 0 {
		return nil, false
	}
	col, ok := colorOf(c)
	if !ok {
		return nil, false
	}
	return &typeset.Stroke{Color: col, Width: w, Join: typeset.MiterJoin, Miter: strokeMiter}, first
}

// strokeText paints the run c of item it of line l with the stroke of its
// box, and reports whether it painted the run; it paints nothing and
// reports false for a box without one.
//
// The stroke is how the text run is painted, so the run stays one text
// object, tagged and extracted once, on every output target: text
// rendering mode 2, fill then stroke, or mode 1, stroke alone, when the
// fill is transparent (ISO 32000-1, 9.3.6), each giving a line centred on
// the outline at the pen's width and join. A paint-order that puts the
// stroke first, which no rendering mode does, and a run already stroked
// to embolden it, which takes the one line width of its text object,
// draw the stroke as the glyph outlines' path instead, an artifact, under
// the run filled in mode 0 or over the emboldened run.
func (p *painter) strokeText(it *lineItem, l *lineBox, page int, c *typeset.Text, top, bottom float64) bool {
	pen, first := textStroke(it.box.st)
	if pen == nil {
		return false
	}
	hollow := c.Color.A <= 0
	if hollow || !first && c.Stroke == 0 {
		if hollow {
			c.Stroke = 0
		}
		c.Outline = pen
		p.emit(it.box, true, c, top, bottom, page)
		return true
	}
	ops, _, _ := p.runOutlines(it, l, "stroke")
	path := &typeset.Path{Ops: ops, Stroke: pen}
	if first && len(ops) > 0 {
		p.emit(it.box, true, path, top, bottom, page)
	}
	p.emit(it.box, true, c, top, bottom, page)
	if !first && len(ops) > 0 {
		p.emit(it.box, true, path, top, bottom, page)
	}
	return true
}
