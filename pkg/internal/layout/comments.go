package layout

import (
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// commentLines records, for each comment whose Annot element (Node.Comment)
// has a fragment on line l, the box the element takes on the line: the
// union of its fragments from their left edge to their right, over the
// height of their content area, the text a reader selects. An empty
// element records its point, a square as tall as its content area at its
// place, which the line's height stands in for when it has none. The
// regions travel with the line's links, cut and moved with them by
// columns and footnote areas, and reach the page as comment marks
// ([engine.commentRegion]).
func (p *painter) commentLines(l *lineBox) {
	var order []*link
	for _, f := range l.frags {
		n := f.box.node
		if n == nil || n.Comment == nil || f.box.hidden {
			continue
		}
		ox, oy := inlineOffset(f.box)
		r := typeset.Rect{X: l.x + f.x0 + p.dx + ox, Y: l.y + f.top + p.dy + oy, Width: f.x1 - f.x0, Height: f.bottom - f.top}
		if r.Height <= 0 {
			r.Y, r.Height = l.y+p.dy+oy, l.h
		}
		if r.Height <= 0 {
			continue
		}
		point := r.Width <= 0
		if point {
			r.Width = r.Height
		}
		i := -1
		for k, lk := range order {
			if lk.comment == n.Comment && lk.point == point {
				i = k
				break
			}
		}
		if i < 0 {
			order = append(order, &link{comment: n.Comment, point: point, elem: f.box.elem})
			p.links = append(p.links, pendingLink{lk: order[len(order)-1], rect: r})
			continue
		}
		// The same comment's elements on one line, two fragments of one
		// element or an element reopened where another closed, make one
		// box.
		for j := len(p.links) - 1; j >= 0; j-- {
			if p.links[j].lk == order[i] {
				p.links[j].rect = union(p.links[j].rect, r)
				break
			}
		}
	}
}

// commentRegion adds the comment region pl, a line's box in flow
// coordinates, to the page of res its middle lies on, cut at that page's
// cuts: a line is on one page, though the glyphs of a line opening a page
// may reach above its cut.
func (e *engine) commentRegion(res *Result, pl pendingLink, used map[*typeset.StructElem]bool) {
	r := pl.rect
	p := e.pageOf(max(r.Y+float64(r.Height/2), 0))
	if p >= len(res.Pages) {
		return
	}
	if rect, ok := e.pageRect(p, r); ok {
		addComment(res.Pages[p], pl.lk, rect, used)
	}
}

// addComment places the comment of the region lk at rect on pg: a line of
// the page's mark of the comment, made with the first line, or the point
// of a mark of its own when lk is a point. A comment's element is kept in
// the structure tree, used, even when it holds no content.
func addComment(pg *typeset.Page, lk *link, rect typeset.Rect, used map[*typeset.StructElem]bool) {
	if rect.Width <= 0 || rect.Height <= 0 {
		return
	}
	if lk.elem != nil {
		used[lk.elem] = true
	}
	if lk.point {
		pg.Comments = append(pg.Comments, typeset.CommentMark{Comment: lk.comment, At: rect, Parent: lk.elem})
		return
	}
	for i := range pg.Comments {
		if m := &pg.Comments[i]; m.Comment == lk.comment && m.Lines != nil {
			m.Lines = append(m.Lines, rect)
			return
		}
	}
	pg.Comments = append(pg.Comments, typeset.CommentMark{Comment: lk.comment, Lines: []typeset.Rect{rect}, Parent: lk.elem})
}
