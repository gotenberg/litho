package layout

import "slices"

// textShadows paints the text-shadow of item it of line l under its
// glyphs (CSS Text Decoration 3, 4): each shadow a copy of the glyph run
// in the shadow's colour moved by its offsets, the last listed painted
// first. A blur radius is not drawn: the copy keeps sharp edges.
func (p *painter) textShadows(it *lineItem, l *lineBox, page int, alpha float64) {
	shadows := it.box.st.TextShadow
	for _, sh := range slices.Backward(shadows) {
		col, ok := colorOf(sh.Color)
		col.A *= alpha
		if !ok || col.A <= 0 {
			continue
		}
		c := *it.text
		c.Origin.X = l.x + it.text.Origin.X + p.dx + sh.X
		c.Origin.Y = l.y + it.text.Origin.Y + p.dy + sh.Y
		c.Color = col
		c.Parent = nil
		top, bottom := l.y+it.baseline-it.ascent+sh.Y, l.y+it.baseline+it.descent+sh.Y
		p.emit(it.box, true, &c, top, bottom, page)
	}
}
