package layout

import (
	"slices"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// textShadows paints the text-shadow of item it of line l under its
// glyphs (CSS Text Decoration 3, 4): each shadow a copy of the glyph run
// and of its decorations in the shadow's colour moved by its offsets, the
// last listed painted first, its underlines and overlines under its
// glyphs and its line-throughs over them as the text paints its own. A
// blur radius is not drawn: the copy keeps sharp edges.
func (p *painter) textShadows(it *lineItem, l *lineBox, page int, alpha float64) {
	shadows := it.box.st.TextShadow
	for _, sh := range slices.Backward(shadows) {
		col, ok := colorOf(sh.Color)
		if !ok || col.A*alpha <= 0 {
			continue
		}
		ds := &decoShadow{x: sh.X, y: sh.Y, color: col}
		p.textDecorations(it, l, page, alpha, css.Underline|css.Overline, ds)
		c := *it.text
		c.Origin.X = l.x + it.text.Origin.X + p.dx + sh.X
		c.Origin.Y = l.y + it.text.Origin.Y + p.dy + sh.Y
		c.Color = col
		c.Color.A *= alpha
		c.Parent = nil
		top, bottom := l.y+it.baseline-it.ascent+sh.Y, l.y+it.baseline+it.descent+sh.Y
		p.emit(it.box, true, &c, top, bottom, page)
		p.textDecorations(it, l, page, alpha, css.LineThrough, ds)
	}
}
