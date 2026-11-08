package layout

import (
	"slices"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// maxFootnotesPerPage bounds the footnote bodies one page's footnote area
// holds: 256. Anti-DoS guard, not spec-mandated: the area is laid out
// again for every footnote placed on its page, and a printed page holds a
// few footnotes; the bodies past the cap wait for the next page.
const maxFootnotesPerPage = 256

// footPage is the footnote area of one page (CSS GCPM 3, 3.4): the calls
// whose bodies it holds, in call order, and the height its margin box
// takes from the bottom of the page area, 0 when empty. end, when set, is
// the flow position the page's flow stops at: a footnote that found no
// room on the page closes it after the line of its call, endBy.
type footPage struct {
	calls []*box
	h     float64
	// notes are the note calls whose bodies the page's note area holds,
	// in call order, and nh the height the area takes from the top of
	// the page area, 0 when it is empty or out of the flow.
	notes []*box
	nh    float64
	end   float64
	endBy *box
}

// pendingNote is a footnote whose body waits for a later page than from.
type pendingNote struct {
	call *box
	from int
}

// footState is the footnotes of one layout: the areas by page, the bodies
// waiting for a page, and where each call's body went.
type footState struct {
	pages   []footPage
	pending []pendingNote
	// where maps a call to its body's page, -1 while it waits.
	where map[*box]int
	// disp is the painted footnote area of each page, by index, and
	// ndisp the painted note area with its offset in the page area.
	disp  map[int]*ditem
	ndisp map[int]placedArea
	// calls are every footnote call, in tree order.
	calls []*box
}

// genFootnote generates the body of the footnote call b, whose node n
// carries it, out of the flow (CSS GCPM 3, 3.2).
func (e *engine) genFootnote(n *Node, b *box, ctx genCtx) {
	if n.Footnote == nil {
		return
	}
	ord := 0
	body := e.genNode(n.Footnote, b, ctx, &ord)
	if len(body) == 0 {
		return
	}
	b.fnBody = body[0]
	b.note = n.Footnote.Style != nil && n.Footnote.Style.Note != ""
	if len(body) > 1 {
		// An inline body split around a block: an anonymous block holds
		// its pieces.
		a := &box{st: anonStyle(n.Footnote.Style), kind: kindBlock, anon: true, first: true, last: true, kids: body, lang: b.lang, elem: b.elem, depth: b.depth}
		for _, k := range body {
			k.parent = a
		}
		e.normalize(a)
		b.fnBody = a
	}
	if e.foot == nil {
		e.foot = &footState{where: map[*box]int{}}
	}
	e.foot.calls = append(e.foot.calls, b)
}

// footAt returns the footnote area of page p.
func (e *engine) footAt(p int) *footPage {
	f := e.foot
	if p < 0 || p > e.maxPages {
		return &footPage{}
	}
	for len(f.pages) <= p {
		f.pages = append(f.pages, footPage{})
	}
	return &f.pages[p]
}

// usableH returns the height of the slice of the flow page p shows, its
// page box being pb: the page area less its footnote area, up to where a
// footnote that found no room closed it (CSS GCPM 3, 3.4.2), top being
// the page's flow position.
func (e *engine) usableH(p int, pb PageBox, top float64) float64 {
	_, h := areaOf(pb)
	if e.foot == nil || p >= len(e.foot.pages) {
		return h
	}
	fp := &e.foot.pages[p]
	h = max(h-fp.h-fp.nh, 0)
	if fp.endBy != nil && fp.end > top {
		h = min(h, fp.end-top)
	}
	// A page always shows some flow, or the layout would never leave it.
	return max(h, 1)
}

// sliceH returns the height of the slice of the flow page k shows.
func (e *engine) sliceH(k int) float64 {
	if e.foot == nil {
		return e.areaH(k)
	}
	return e.pageTop(k+1) - e.pageTop(k)
}

// recut drops the page cuts after page p, which its footnote area moved:
// they resolve again as the layout reaches them.
func (g *geometry) recut(p int) {
	if !g.varies() || p < 0 || p >= len(g.boxes) {
		return
	}
	g.boxes, g.infos, g.tops = g.boxes[:p+1], g.infos[:p+1], g.tops[:p+1]
	g.tops = append(g.tops, g.tops[p]+g.e.usableH(p, g.boxes[p], g.tops[p]))
}

// areaStyle returns the style of the footnote area of page p: the page's
// @footnote style, else a block of the root's inherited properties.
func (e *engine) areaStyle(p int) *css.Style {
	if st := e.box(p).Footnote; st != nil {
		return st
	}
	if e.rootBox != nil {
		return anonStyle(e.rootBox.st)
	}
	return css.Initial()
}

// layoutArea lays out the footnote area of page p holding the bodies of
// calls, styled st, at the origin, off the page cuts, and returns it.
func (e *engine) layoutArea(p int, calls []*box, st *css.Style) *box {
	area := &box{st: st, kind: kindBlock, first: true, last: true, parent: e.rootBox}
	if e.rootBox != nil {
		area.lang, area.depth = e.rootBox.lang, e.rootBox.depth+1
	}
	for _, c := range calls {
		if c.fnBody != nil {
			c.fnBody.parent = area
			area.kids = append(area.kids, c.fnBody)
		}
	}
	e.normalize(area)
	e.noPaginate++
	e.layoutBlock(area, cblock{w: e.areaW(p)}, &bfc{}, 0, &marginState{}, breakNone)
	e.layoutPositioned(area)
	e.noPaginate--
	return area
}

// areaHeight returns the height of the margin box of the area a, laid out
// at the origin.
func areaHeight(a *box) float64 { return max(a.y+a.borderH()+a.m[css.Bottom], 0) }

// placeNote adds the body of call to the footnote area of page p, whose
// flow so far ends at bottom, and reports whether it went there; room
// reports that it lacked the room left on the page rather than the area's
// max-height. A later page, one the call is not on, takes a first body
// whatever its height (CSS GCPM 3, 3.8, footnote-policy: auto).
func (e *engine) placeNote(p int, call *box, bottom float64, later bool) (ok, room bool) {
	if call.note {
		return e.placeAreaNote(p, call, bottom, later)
	}
	fp := e.footAt(p)
	if len(fp.calls) >= maxFootnotesPerPage {
		return false, false
	}
	calls := append(slices.Clone(fp.calls), call)
	st := e.areaStyle(p)
	free := *st
	free.MaxHeight = css.Dimension{Kind: css.None}
	a := e.layoutArea(p, calls, &free)
	first := later && len(fp.calls) == 0
	_, ah := areaOf(e.box(p))
	if mh, ok := specMax(st.MaxHeight, ah); ok && a.h > mh+eps && !first {
		return false, false
	}
	h := areaHeight(e.layoutArea(p, calls, st)) + e.ruleRoom(p)
	used := max(bottom-e.pageTop(p), 0)
	if used+h+fp.nh > ah+eps && !(first && used <= eps) {
		return false, true
	}
	fp.calls, fp.h = calls, h
	e.foot.where[call] = p
	e.geom.recut(p)
	return true, false
}

// specMax returns a max-height of points or a percentage of ref, false
// for none.
func specMax(d css.Dimension, ref float64) (float64, bool) {
	switch d.Kind { //nolint:exhaustive // the other kinds set no maximum
	case css.Points:
		return d.Value, true
	case css.Percent:
		return d.Value * ref, true
	}
	return 0, false
}

// unplace takes the body of call out of where an earlier layout of its
// line put it.
func (e *engine) unplace(call *box) {
	f := e.foot
	p, ok := f.where[call]
	if !ok {
		return
	}
	delete(f.where, call)
	if p < 0 {
		f.pending = slices.DeleteFunc(f.pending, func(n pendingNote) bool { return n.call == call })
		return
	}
	fp := e.footAt(p)
	if call.note {
		fp.notes = slices.DeleteFunc(fp.notes, func(c *box) bool { return c == call })
		fp.nh = 0
		if len(fp.notes) > 0 {
			fp.nh = e.noteRoom(e.layoutNoteArea(p, fp.notes, e.noteStyle(p)))
		}
		if fp.endBy == call {
			fp.endBy, fp.end = nil, 0
		}
		e.geom.recut(p)
		return
	}
	fp.calls = slices.DeleteFunc(fp.calls, func(c *box) bool { return c == call })
	if fp.endBy == call {
		fp.endBy, fp.end = nil, 0
	}
	fp.h = 0
	if len(fp.calls) > 0 {
		fp.h = areaHeight(e.layoutArea(p, fp.calls, e.areaStyle(p))) + e.ruleRoom(p)
	}
	e.geom.recut(p)
}

// flushNotes places the bodies waiting for a page on page p, whose flow so
// far ends at bottom, in call order, until one does not fit.
func (e *engine) flushNotes(p int, bottom float64) {
	f := e.foot
	for len(f.pending) > 0 && e.err == nil {
		n := f.pending[0]
		if p <= n.from {
			return
		}
		if ok, _ := e.placeNote(p, n.call, bottom, true); !ok {
			return
		}
		f.pending = f.pending[1:]
	}
}

// noteLine places the bodies of the footnote calls of the line l, holding
// the characters [start, end) of p (CSS GCPM 3, 3.8, footnote-policy:
// auto): on the line's page when the area keeps within its max-height and
// the page has room for it, else on a later page, a body that lacks the
// room closing the line's page after the line.
func (e *engine) noteLine(p *ifcPara, start, end int, l *lineBox) {
	f := e.foot
	if f == nil || !e.paginating() {
		return
	}
	for _, o := range p.opens {
		call := o.box
		if call.fnBody == nil || o.pos < start || o.pos >= end && (o.pos != end || end != p.n) {
			continue
		}
		e.unplace(call)
		page := e.pageOf(l.y + eps)
		if len(f.pending) == 0 {
			ok, room := e.placeNote(page, call, l.y+l.h, false)
			if ok {
				continue
			}
			if room {
				fp := e.footAt(page)
				if fp.endBy == nil {
					fp.end, fp.endBy = l.y+l.h, call
					e.geom.recut(page)
				}
			}
		}
		f.where[call] = -1
		f.pending = append(f.pending, pendingNote{call: call, from: page})
	}
}

// finishNotes places the bodies still waiting once the flow is laid out,
// on the pages after it, from the page after flowEnd's.
func (e *engine) finishNotes(flowEnd float64) {
	f := e.foot
	if f == nil {
		return
	}
	last := e.pageOf(max(flowEnd-eps, 0))
	for _, c := range f.calls {
		if _, ok := f.where[c]; !ok {
			// A call laid out off the page cuts, inside an inline block or
			// another footnote, has no page of its own: its body follows
			// the flow.
			f.where[c] = -1
			f.pending = append(f.pending, pendingNote{call: c, from: last})
		}
	}
	for p := last; len(f.pending) > 0 && p <= e.maxPages && e.err == nil; p++ {
		e.flushNotes(p, e.pageTop(p))
	}
}

// notePages returns the pages the footnote areas reach.
func (e *engine) notePages() int {
	if e.foot == nil {
		return 0
	}
	n := 0
	for p, fp := range e.foot.pages {
		if len(fp.calls) > 0 || len(fp.notes) > 0 {
			n = p + 1
		}
	}
	return n
}

// paintNotes lays out and paints the footnote area of every page that
// holds bodies.
func (e *engine) paintNotes() {
	f := e.foot
	if f == nil {
		return
	}
	f.disp = map[int]*ditem{}
	e.paintNoteAreas()
	for p, fp := range f.pages {
		if len(fp.calls) == 0 || e.err != nil {
			continue
		}
		area := e.layoutArea(p, fp.calls, e.areaStyle(p))
		pt := &painter{e: e, top: &ditem{group: true, opacity: 1}, mono: -1, alpha: 1}
		pt.frames = []*frame{{container: pt.top, root: area}}
		pt.context(area)
		f.disp[p] = pt.top
	}
}

// assembleNotes adds the footnote area of page k to pg, whose page box is
// pb, at the bottom of the page area (CSS GCPM 3, 3.4.1), over the flow,
// under the footnote separator of pb and its spaces.
func (e *engine) assembleNotes(pg *compose.Page, k int, pb PageBox, used map[*compose.StructElem]bool) {
	f := e.foot
	if f == nil || k >= len(f.pages) {
		return
	}
	e.assembleNoteArea(pg, k, pb, used)
	d := f.disp[k]
	if d == nil {
		return
	}
	frags := e.flatten(d, 1, false)
	for _, fr := range frags {
		markTree(fr, used)
	}
	_, ah := areaOf(pb)
	top := pb.Margin[css.Top] + ah - f.pages[k].h
	e.paintFootnoteRule(pg, pb, top)
	if r := pb.FootnoteRule; r != nil {
		top += r.SpaceAbove + r.Thickness + r.SpaceBelow
	}
	pg.Content = append(pg.Content, &compose.Group{
		Transform: compose.Matrix{A: 1, D: 1, E: pb.Margin[css.Left], F: top},
		Opacity:   1,
		Content:   frags,
	})
}
