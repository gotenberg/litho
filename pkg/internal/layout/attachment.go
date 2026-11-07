package layout

import (
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// localArea returns the background positioning area of the box b whose
// background-attachment is local, area being the one its origin gives on
// its border box r (CSS Backgrounds 3, 3.5): for a scroll container, the
// area grown to the scrollable overflow, its in-flow content and line
// boxes and the end padding past them (CSS Overflow 3, 2.2), as though
// the area scrolled with the content. Any other box keeps area. Fixed
// needs nothing here: Chromium prints it as scroll.
func (p *painter) localArea(b *box, area, r compose.Rect, pd [4]float64) compose.Rect {
	if b.kind != kindBlock || b.st.Overflow == css.OverflowVisible || b == p.viewport {
		return area
	}
	// Flow coordinates to the painted ones: the relative offset in force.
	dx, dy := r.X-b.x, r.Y-b.y
	right, bottom := area.X+area.Width, area.Y+area.Height
	grow := func(x1, y1 float64) {
		right = max(right, x1+dx+pd[css.Right])
		bottom = max(bottom, y1+dy+pd[css.Bottom])
	}
	for _, l := range b.lines {
		end := l.x
		for _, it := range l.items {
			end = max(end, l.x+it.x+it.w)
		}
		grow(end, l.y+l.h)
	}
	for _, k := range b.kids {
		if k.laidOut && !k.abs && k.kind != kindInline && k.kind != kindText && k.kind != kindBreak {
			grow(k.x+k.borderW()+max(k.m[css.Right], 0), k.y+k.borderH()+max(k.m[css.Bottom], 0))
		}
	}
	return compose.Rect{X: area.X, Y: area.Y, Width: right - area.X, Height: bottom - area.Y}
}
