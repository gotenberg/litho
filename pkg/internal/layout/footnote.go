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
	// disp is the painted footnote area of each page, by index, areas
	// the area's boxes and links its link regions, relative to the area,
	// and ndisp the painted note area with its offset in the page area.
	disp  map[int]*ditem
	areas map[int]*box
	links map[int][]pendingLink
	ndisp map[int]placedArea
	// calls are every footnote call, in tree order.
	calls []*box
	// deferred maps a call in columns to the page that refused its body:
	// the column set laid out again does not offer it that page again.
	deferred map[*box]int
	// tried maps a call in columns to the page its body was placed on
	// though the page lacked the room, for the columns laid out again
	// around it to show whether the call stays on that page; forcing
	// places a body so.
	tried   map[*box]int
	forcing bool
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
	f := e.foot
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
	if used+h+fp.nh > ah+eps && !(first && used <= eps) && !f.forcing {
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
		if e.cols != nil {
			// A call in columns places its body on the page its column
			// lies on, as the line's bottom in the page flow.
			e.inPages(l.y, l.y+l.h, func(top, bottom float64) { e.noteColumnCall(call, top, bottom) })
			continue
		}
		e.noteCall(call, l.y, l.y+l.h)
	}
}

// noteCall places the body of the footnote call call, whose line spans
// the flow positions top to bottom (CSS GCPM 3, 3.8, footnote-policy:
// auto), as [engine.noteLine] does.
func (e *engine) noteCall(call *box, top, bottom float64) {
	f := e.foot
	e.unplace(call)
	page := e.pageOf(top + eps)
	if len(f.pending) == 0 {
		ok, room := e.placeNote(page, call, bottom, false)
		if ok {
			return
		}
		if room {
			fp := e.footAt(page)
			if fp.endBy == nil {
				fp.end, fp.endBy = bottom, call
				e.geom.recut(page)
			}
		}
	}
	f.where[call] = -1
	f.pending = append(f.pending, pendingNote{call: call, from: page})
}

// noteColumnCall is [engine.noteCall] for a call in columns, its line
// spanning top to bottom in the page flow: a body the call's page refused
// waits for the next page whatever the columns laid out again leave, and
// the page goes on past the call's line, its columns filling the room the
// footnote area leaves.
func (e *engine) noteColumnCall(call *box, top, bottom float64) {
	f := e.foot
	e.unplace(call)
	page := e.pageOf(top + eps)
	if p, ok := f.tried[call]; ok && p != page {
		// The room the body took moved its call off its page: the body
		// waits for a later page, the call staying where the body left
		// it room.
		delete(f.tried, call)
		if f.deferred == nil {
			f.deferred = map[*box]int{}
		}
		f.deferred[call] = p
	}
	if p, ok := f.deferred[call]; !ok || p != page {
		// Bodies waiting from an earlier page go first; those a call
		// later in the columns left waiting on this page do not, since
		// laying the columns out again reaches this call before them.
		waiting := slices.ContainsFunc(f.pending, func(n pendingNote) bool { return n.from < page })
		if !waiting {
			ok, room := e.placeNote(page, call, bottom, false)
			if ok {
				delete(f.tried, call)
				return
			}
			if _, done := f.tried[call]; room && !done {
				// The columns laid out again in the room the area leaves
				// may move the call's line to a column where it fits.
				f.forcing = true
				ok, _ = e.placeNote(page, call, bottom, false)
				f.forcing = false
				if ok {
					if f.tried == nil {
						f.tried = map[*box]int{}
					}
					f.tried[call] = page
					return
				}
			}
		}
		if f.deferred == nil {
			f.deferred = map[*box]int{}
		}
		f.deferred[call] = page
	}
	f.where[call] = -1
	f.pending = append(f.pending, pendingNote{call: call, from: page})
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
	f.disp, f.areas, f.links = map[int]*ditem{}, map[int]*box{}, map[int][]pendingLink{}
	e.paintNoteAreas()
	for p, fp := range f.pages {
		if len(fp.calls) == 0 || e.err != nil {
			continue
		}
		area := e.layoutArea(p, fp.calls, e.areaStyle(p))
		pt := &painter{e: e, top: &ditem{group: true, opacity: 1}, mono: -1, alpha: 1}
		pt.frames = []*frame{{container: pt.top, root: area}}
		pt.context(area)
		f.disp[p], f.areas[p], f.links[p] = pt.top, area, pt.links
	}
}

// areaOrigin returns where the footnote area of page k, whose page box is
// pb, starts on its page, in the page area's coordinates: under the
// footnote separator and its spaces, as [engine.assembleNotes] places it.
func (e *engine) areaOrigin(k int, pb PageBox) float64 {
	_, ah := areaOf(pb)
	top := ah - e.foot.pages[k].h
	if r := pb.FootnoteRule; r != nil {
		top += r.SpaceAbove + r.Thickness + r.SpaceBelow
	}
	return top
}

// noteAnchors adds to out where the boxes with an ID inside the footnote
// areas landed, page by page, for a document of n pages, unless an
// earlier box took the ID.
func (e *engine) noteAnchors(out map[string]compose.Dest, n int) {
	f := e.foot
	if f == nil {
		return
	}
	for k := range f.pages {
		area := f.areas[k]
		if area == nil || k >= n {
			continue
		}
		pb := e.box(k)
		top := e.areaOrigin(k, pb)
		var walk func(b *box, depth int)
		walk = func(b *box, depth int) {
			if depth > maxTreeDepth {
				return
			}
			if b.id != "" {
				if _, ok := out[b.id]; !ok {
					x, y, ok := b.anchorX, b.anchorY, b.anchorSet
					if !ok && b.laidOut && b.kind != kindInline && b.kind != kindText && b.kind != kindBreak {
						x, y, ok = b.x, b.y, true
					}
					if ok {
						out[b.id] = compose.Dest{Page: k + 1, X: pb.Bleed + pb.Margin[css.Left] + clamp(x), Y: pb.Bleed + pb.Margin[css.Top] + top + max(clamp(y), 0)}
					}
				}
			}
			for _, c := range b.kids {
				walk(c, depth+1)
			}
		}
		walk(area, 0)
	}
}

// noteLinks adds the link regions of the footnote areas to the pages of
// res, internal targets resolved against res.Anchors.
func (e *engine) noteLinks(res *Result, used map[*compose.StructElem]bool) {
	f := e.foot
	if f == nil {
		return
	}
	for k := range f.pages {
		if k >= len(res.Pages) {
			break
		}
		pb := e.box(k)
		top := e.areaOrigin(k, pb)
		for _, pl := range f.links[k] {
			l, ok := e.resolveLink(pl)
			if !ok {
				continue
			}
			r := pl.rect
			l.Rect = compose.Rect{X: pb.Bleed + pb.Margin[css.Left] + r.X, Y: pb.Bleed + pb.Margin[css.Top] + top + r.Y, Width: r.Width, Height: r.Height}
			if l.Parent != nil {
				used[l.Parent] = true
			}
			res.Pages[k].Links = append(res.Pages[k].Links, l)
		}
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

// inPages runs f with the flow positions top and bottom of the column
// flow, which lie in one column, mapped to the page flow around the
// column set, in that fragmentation context. Columns nested in columns
// are left alone.
func (e *engine) inPages(top, bottom float64, f func(top, bottom float64)) {
	c := e.cols
	if c == nil {
		f(top, bottom)
		return
	}
	if c.parent != nil || c.free {
		// The pass that counts the columns forced breaks make lays the
		// content out in columns no page limits: it places no body.
		return
	}
	i := c.of(top + eps)
	if i >= len(c.frags) {
		return
	}
	fr := c.frags[i]
	if fr.row >= len(c.rows) {
		return
	}
	r := c.rows[fr.row]
	ot := r.top + min(max(top-fr.vtop, 0), fr.h)
	ob := r.top + min(max(bottom-fr.vtop, 0), fr.h)
	c.outer(func() {
		if e.paginating() {
			f(ot, ob)
		}
	})
}

// noteSignature returns a value that changes when a footnote area changes
// height, for a column set to lay its content out again around it.
func (e *engine) noteSignature() float64 {
	if e.foot == nil {
		return 0
	}
	sig := 0.0
	for p, fp := range e.foot.pages {
		sig += float64(p+1) * (fp.h + 2*fp.nh)
	}
	return sig
}
