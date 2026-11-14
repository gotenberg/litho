package layout

import (
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// extents returns, for every node with an ID whose first box is a laid out
// block-level, inline-block or replaced box landing on one of the n pages,
// its border box less its padding on the page its top lands on, for
// [Result.Extents].
func (e *engine) extents(rb *box, n int) map[string]typeset.Rect {
	out := map[string]typeset.Rect{}
	var walk func(*box)
	walk = func(b *box) {
		if b.id != "" && b.laidOut && b.kind != kindInline && b.kind != kindText && b.kind != kindBreak {
			if _, ok := out[b.id]; !ok {
				x, y := toFlow(b, b.x, b.y)
				d := e.flowDest(clamp(x), max(clamp(y), 0))
				if d.Page <= n {
					r := typeset.Rect{
						X: d.X + b.pd[css.Left], Y: d.Y + b.pd[css.Top],
						Width:  max(b.borderW()-b.pd[css.Left]-b.pd[css.Right], 0),
						Height: max(b.borderH()-b.pd[css.Top]-b.pd[css.Bottom], 0),
					}
					if e.turn != css.HorizontalTB {
						r = turnRect(e.box(d.Page-1), e.turn, r)
					}
					out[b.id] = r
				}
			}
		}
		for _, k := range b.kids {
			walk(k)
		}
	}
	walk(rb)
	return out
}
