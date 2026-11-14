package layout

import (
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// pageMono is the page of a display item a column cut moved: a line or a
// monolithic piece whose page follows its extent, which lies on one page.
const pageMono = -2

// farEdge bounds the clip of a column's slice across the inline axis and
// past the first and the last column: content overflowing a column paints
// beside it (CSS Multicol 1, 8.1).
const farEdge = 1e5

// multicol paints the multi-column container b: its background and
// borders, its column rules and its outside marker, then each column set
// painted in its column flow and cut at its columns, each column moved to
// its place, and its spanners across the container (CSS Multicol 1, 2 and
// 4), then its outline. The marker sits by the first line box, whose
// painted place [firstBaseline] already gives, in a column or in a
// spanner: it is painted once, outside the columns. The outline comes
// after those of the boxes inside, as in a box without columns
// ([painter.outlines]).
func (p *painter) multicol(b *box) {
	if b == p.canvas || b.parent == nil {
		p.borders(b)
	} else {
		p.decorations(b)
	}
	p.linkBox(b)
	p.columnRules(b)
	p.marker(b)
	marker := b.outside
	b.outside = nil
	defer func() { b.outside = marker }()
	for _, s := range b.mc.sets {
		if p.e.err != nil {
			return
		}
		if s.spanner != nil {
			if s.spanner.layered() {
				// A positioned spanner paints the positioned boxes inside
				// it too: no column set and no stacking context around the
				// container lists them (CSS 2.1, Appendix E).
				sdx, sdy := p.dx, p.dy
				p.dx, p.dy = ancestorOffset(s.spanner)
				p.unit(s.spanner, true)
				p.dx, p.dy = sdx, sdy
			} else {
				p.context(s.spanner)
			}
			continue
		}
		g := &ditem{group: true}
		nl := len(p.links)
		fr := &frame{container: g, root: b, columns: true, set: s, dx: p.dx, dy: p.dy}
		p.frames = append(p.frames, fr)
		kids := b.kids
		b.kids = s.kids
		// The columns cut what the set paints by its extent: a page an
		// atomic inline or a table row gives its content applies to the
		// cut pieces, not to what is cut.
		mono := p.mono
		p.mono = -1
		outer := p.setBody(b)
		p.mono = mono
		b.kids = kids
		p.frames = p.frames[:len(p.frames)-1]
		outer = append(outer, fr.escaped...)
		// The boxes outside the columns of a negative layer paint under
		// the columns, the others over them (CSS 2.1, Appendix E).
		var neg, rest []*box
		for _, o := range outer {
			if o.isStackingContext() && o.zLevel() < 0 {
				neg = append(neg, o)
				continue
			}
			rest = append(rest, o)
		}
		p.paintOuter(neg)
		c := p.sync(p.chain(b, true))
		cut := p.e.cutColumns(g, b, s, p.dy)
		if mono >= 0 {
			onPage(cut, mono, 0)
		}
		c.kids = append(c.kids, cut...)
		moved := cutLinks(slices.Clone(p.links[nl:]), b, s, p.dy)
		p.links = append(p.links[:nl], moved...)
		p.paintOuter(rest)
	}
	p.outline(b)
}

// paintOuter paints the positioned boxes of a column set whose place does
// not follow its columns, with the relative offsets their ancestors give
// them.
//
// A box whose containing block lies outside a multi-column container
// around too paints outside that container's columns as well.
func (p *painter) paintOuter(boxes []*box) {
	for _, o := range boxes {
		if f := p.cur(); f.columns && leavesColumns(o, f.root) {
			f.escaped = append(f.escaped, o)
			continue
		}
		sdx, sdy := p.dx, p.dy
		p.dx, p.dy = ancestorOffset(o)
		p.unit(o, o.isStackingContext())
		p.dx, p.dy = sdx, sdy
	}
}

// leavesColumns reports a positioned box l inside the multi-column
// container b laid out outside its columns ([engine.escapeColumns]): an
// absolutely positioned box whose containing block lies outside b's
// column flow, or a box inside such a box.
func leavesColumns(l, b *box) bool {
	for a := l; a != nil && a != b; a = a.parent {
		if a.abs && !holdsInside(a.cbBox, b) {
			return true
		}
	}
	return false
}

// setBody paints the content of the column set whose boxes are the kids of
// b, in the painting order of CSS 2.1, Appendix E, and returns the
// positioned boxes it holds whose place does not follow the columns, in
// painting order ([leavesColumns]).
func (p *painter) setBody(b *box) []*box {
	var blocks, floats, layers []*box
	p.collect(b, &blocks, &floats, &layers)
	var outer []*box
	keep := func(ls []*box) []*box {
		out := ls[:0]
		for _, l := range ls {
			if leavesColumns(l, b) {
				outer = append(outer, l)
				continue
			}
			out = append(out, l)
		}
		return out
	}
	neg, zero, pos := p.stack(layers)
	neg, zero, pos = keep(neg), keep(zero), keep(pos)
	for _, c := range neg {
		p.lifted(c)
	}
	for _, k := range blocks {
		p.linkBox(k)
		p.decorations(k)
	}
	for _, f := range floats {
		p.floating(f)
	}
	p.content(b)
	for _, k := range blocks {
		p.content(k)
	}
	for _, c := range zero {
		p.lifted(c)
	}
	for _, c := range pos {
		p.lifted(c)
	}
	p.outlines(blocks)
	return outer
}

// piece is a display item cut for one column, and whether it reaches past
// the column's slice, which then clips it. up and down are how far the
// clip reaches over the room the column keeps at its top and its end for
// the cloned edges of the boxes inside the item's box ([clonedSpans]).
type piece struct {
	it       *ditem
	clip     bool
	up, down float64
}

// clonedSpan is the column flow extent of a box whose cloned edges take
// room in the columns it crosses ([engine.enterClone]), and those edges.
type clonedSpan struct {
	y0, y1      float64
	top, bottom float64
}

// clonedSpans returns the boxes among kids and their descendants whose
// cloned edges take room in the columns they cross.
func clonedSpans(kids []*box, out []clonedSpan, depth int) []clonedSpan {
	if depth > maxTreeDepth {
		return out
	}
	for _, k := range kids {
		if !k.laidOut || k.abs || k.fixed || k.kind != kindBlock || k.mc != nil {
			continue
		}
		if clones(k.st) && k.st.Height.Kind == css.Auto && vEdges(k) > 0 {
			out = append(out, clonedSpan{y0: k.y, y1: k.y + k.borderH(), top: k.bd[css.Top] + k.pd[css.Top], bottom: k.bd[css.Bottom] + k.pd[css.Bottom]})
		}
		out = clonedSpans(k.kids, out, depth+1)
	}
	return out
}

// cloneReach returns how far a slice of an item whose column flow extent is
// y0 to y1, cut at the column flow position at, fills the room the cut
// keeps for cloned edges: that of the cloned boxes the cut crosses which
// lie inside the item, whose top is not above y0, nor their bottom below
// y1. The item is then the background of a box around them, which fills
// its column, and not content inside them.
func cloneReach(spans []clonedSpan, at, y0, y1 float64, top bool) float64 {
	r := 0.0
	for _, c := range spans {
		if c.y0 >= at-eps || c.y1 <= at+eps || c.y0 < y0-eps || c.y1 > y1+eps {
			continue
		}
		if top {
			r += c.top
		} else {
			r += c.bottom
		}
	}
	return r
}

// cutColumns cuts the display list g of the column set s of b, painted in
// its column flow, at its columns: each column's slice moves to its place
// under a translation, the items crossing a cut sliced and clipped to it
// (box-decoration-break: slice), the lines and monolithic pieces whole in
// the column they start in (CSS Fragmentation 3, 4.4).
func (e *engine) cutColumns(g *ditem, b *box, s *colSet, oy float64) []*ditem {
	if len(s.frags) == 0 {
		return g.kids
	}
	per, pass := e.cutItem(g, b, s, oy, clonedSpans(s.kids, nil, 0), 0)
	var out []*ditem
	for _, i := range slices.Sorted(maps.Keys(per)) {
		items := per[i]
		dx, dy := b.mc.shiftOf(s, i)
		var m *typeset.Matrix
		if dx != 0 || dy != 0 {
			m = &typeset.Matrix{A: 1, D: 1, E: dx, F: dy}
		}
		y0, y1 := s.band(b, i)
		y0, y1 = y0+oy+dy, y1+oy+dy
		if i == 0 {
			y0 = -farEdge
		}
		if i == len(s.frags)-1 {
			y1 = farEdge
		}
		band := &typeset.Path{Ops: rectOps(typeset.Rect{X: -farEdge, Y: y0, Width: 2 * farEdge, Height: y1 - y0}), Fill: &typeset.Fill{}}
		for j := 0; j < len(items); {
			k := j + 1
			for k < len(items) && items[k].clip == items[j].clip && items[k].up == items[j].up && items[k].down == items[j].down {
				k++
			}
			run := make([]*ditem, 0, k-j)
			for _, pc := range items[j:k] {
				run = append(run, pc.it)
			}
			moved := &ditem{group: true, kids: run, fx: &groupFx{opacity: 1, transform: m}}
			if items[j].clip {
				clip := band
				if up, down := items[j].up, items[j].down; up > 0 || down > 0 {
					clip = &typeset.Path{Ops: rectOps(typeset.Rect{X: -farEdge, Y: y0 - up, Width: 2 * farEdge, Height: y1 - y0 + up + down}), Fill: &typeset.Fill{}}
				}
				moved = &ditem{group: true, kids: []*ditem{moved}, fx: &groupFx{opacity: 1, clip: clip}}
			}
			out = append(out, moved)
			j = k
		}
	}
	return append(out, pass...)
}

// onPage puts the leaves of items on page alone, groups at most
// maxGroupDepth deep, but those of a fixed box, which paints on every
// page.
func onPage(items []*ditem, page, depth int) {
	for _, it := range items {
		if !it.group {
			it.page = page
			continue
		}
		if !it.fixedGroup() && depth < maxGroupDepth {
			onPage(it.kids, page, depth+1)
		}
	}
}

// band returns the column flow slice of column i of s in the container b.
func (s *colSet) band(b *box, i int) (float64, float64) {
	f := s.frags[min(max(i, 0), len(s.frags)-1)]
	return b.y + f.vtop, b.y + f.end()
}

// cutItem returns the items of the group it per column of s, groups
// rebuilt in each column they reach, and the fixed groups, which follow
// no column. The extents of the items move to their column's place; oy is
// the relative offset the container paints at.
func (e *engine) cutItem(it *ditem, b *box, s *colSet, oy float64, spans []clonedSpan, depth int) (map[int][]piece, []*ditem) {
	out := map[int][]piece{}
	var pass []*ditem
	last := len(s.frags) - 1
	for _, k := range it.kids {
		if k.group && k.fixedGroup() {
			pass = append(pass, k)
			continue
		}
		if k.group && k.page == pageMono {
			// A monolithic box's group moves whole to the column its top
			// is in.
			i := s.fragOf(b, k.y0-oy+eps)
			_, dy := b.mc.shiftOf(s, i)
			out[i] = append(out[i], piece{it: moveItem(k, dy, 0)})
			continue
		}
		if k.group {
			if depth >= maxGroupDepth {
				continue
			}
			sub, sp := e.cutItem(k, b, s, oy, spans, depth+1)
			pass = append(pass, sp...)
			for _, i := range slices.Sorted(maps.Keys(sub)) {
				cp := *k
				cp.kids, cp.page = nil, 0
				clip, up, down := false, 0.0, 0.0
				for _, pc := range sub[i] {
					cp.kids = append(cp.kids, pc.it)
					clip = clip || pc.clip
					up, down = max(up, pc.up), max(down, pc.down)
				}
				if clip && k.fx != nil && k.fx.shift {
					// The fragment is cut at its column before its
					// relative offset moves it.
					top, bottom := s.band(b, i)
					top, bottom = top+oy-up, bottom+oy+down
					if i == 0 {
						top = -farEdge
					}
					if i == last {
						bottom = farEdge
					}
					band := &typeset.Path{Ops: rectOps(typeset.Rect{X: -farEdge, Y: top, Width: 2 * farEdge, Height: bottom - top}), Fill: &typeset.Fill{}}
					cp.kids = []*ditem{{group: true, kids: cp.kids, fx: &groupFx{opacity: 1, clip: band}}}
					clip, up, down = false, 0, 0
				}
				out[i] = append(out[i], piece{it: &cp, clip: clip, up: up, down: down})
			}
			continue
		}
		if k.page >= 0 || k.page == pageMono {
			// A glyph run's extent reaches above and below its line box
			// by its ascent and descent: its middle is on its line.
			i := s.fragOf(b, float64((k.y0+max(k.y1, k.y0))/2)-oy)
			_, dy := b.mc.shiftOf(s, i)
			leaf := *k
			leaf.y0, leaf.y1, leaf.page = k.y0+dy, k.y1+dy, pageMono
			out[i] = append(out[i], piece{it: &leaf})
			continue
		}
		i0, i1 := s.fragOf(b, k.y0-oy), s.fragOf(b, max(k.y1-1e-3, k.y0)-oy)
		for i := i0; i <= i1; i++ {
			top, bottom := s.band(b, i)
			top, bottom = top+oy, bottom+oy
			if i == 0 {
				top = -farEdge
			}
			if i == last {
				bottom = farEdge
			}
			_, dy := b.mc.shiftOf(s, i)
			leaf := *k
			if i1 > i0 {
				leaf.frag = cloneFragment(k.frag)
			}
			leaf.y0, leaf.y1, leaf.page = max(k.y0, top)+dy, min(k.y1, bottom)+dy, -1
			pc := piece{it: &leaf, clip: k.y0 < top-eps || k.y1 > bottom+eps}
			if len(spans) > 0 && i > i0 {
				pc.up = cloneReach(spans, top-oy, k.y0-oy, k.y1-oy, true)
			}
			if len(spans) > 0 && i < i1 {
				pc.down = cloneReach(spans, bottom-oy, k.y0-oy, k.y1-oy, false)
			}
			out[i] = append(out[i], pc)
		}
	}
	return out, pass
}

// cutLinks moves the link regions of the column set s of b, recorded in
// its column flow at the relative offset oy, to the columns they cross,
// cut at each.
func cutLinks(links []pendingLink, b *box, s *colSet, oy float64) []pendingLink {
	if len(s.frags) == 0 {
		return links
	}
	var out []pendingLink
	for _, l := range links {
		r := l.rect
		i0, i1 := s.fragOf(b, r.Y-oy), s.fragOf(b, max(r.Y+r.Height-1e-3, r.Y)-oy)
		for i := i0; i <= i1; i++ {
			top, bottom := s.band(b, i)
			top, bottom = top+oy, bottom+oy
			if i == i0 {
				top = min(top, r.Y)
			}
			if i == i1 {
				bottom = max(bottom, r.Y+r.Height)
			}
			y0, y1 := max(r.Y, top), min(r.Y+r.Height, bottom)
			if y1 <= y0 {
				continue
			}
			dx, dy := b.mc.shiftOf(s, i)
			out = append(out, pendingLink{lk: l.lk, rect: typeset.Rect{X: r.X + dx, Y: y0 + dy, Width: r.Width, Height: y1 - y0}})
		}
	}
	return out
}

// columnRules paints the column rules of b in the middle of the gaps
// between the columns of each row that both hold content, as tall as the
// row's columns (CSS Multicol 1, 4.2 to 4.5). The columns of the last row
// of the last column set, after every spanner, reach down to b's content
// edge when b is taller than its content, as Chromium makes them: the
// rule follows, to the end of the page or the outer column that cuts b
// before that edge.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws the rule of
//     a balanced two-column container 300 pixels tall, or 300 pixels at
//     the least, holding 100 pixels of content, 300 pixels long; after a
//     spanner, the last row's rule runs to the content edge and the rows
//     before keep their balanced height.
//   - probe, html: Chromium 156 prints the rule of such a container that a
//     page cuts to the end of the page area.
//   - wpt, css/css-multicol/multicol-breaking-000.html and -001: Chromium
//     draws the rule of an inner container 300 pixels tall, balanced to 60
//     pixels, to the end of the 100 pixel outer column.
func (p *painter) columnRules(b *box) {
	c := &b.st.Columns
	w := c.RuleWidth
	if c.RuleStyle == css.BorderNone || c.RuleStyle == css.BorderHidden || w <= 0 || b.hidden {
		return
	}
	d := b.mc
	st := *b.st
	st.Border = [4]css.Border{}
	st.Border[css.Left] = css.Border{Width: w, Style: c.RuleStyle, Color: c.RuleColor}
	for si, s := range d.sets {
		if s.spanner != nil {
			continue
		}
		for ri, r := range s.rows {
			h := r.used
			if top, bottom := b.y+r.top, b.contentY()+b.h; si == len(d.sets)-1 && ri == len(s.rows)-1 && bottom > top+r.used {
				if uncut(p.e, b, top, bottom) {
					h = bottom - top
				} else {
					end := fragmentEnd(p.e, b, top)
					if clones(b.st) {
						// The fragment holds b's cloned bottom edges.
						end -= b.bd[css.Bottom] + b.pd[css.Bottom]
					}
					h = max(min(bottom, end)-top, h)
				}
			}
			for j := 0; j+1 < r.cols; j++ {
				mid := d.ruleX(j)
				rect := typeset.Rect{X: b.contentX() + mid - float64(w/2) + p.dx, Y: b.y + r.top + p.dy, Width: w, Height: h}
				if rect.Height <= 0 {
					continue
				}
				p.border(b, &st, rect, [4][2]float64{}, [4]float64{css.Left: w}, [4]bool{css.Left: true})
			}
		}
	}
}

// uncut reports that no page and no column of the nearest multi-column
// ancestor cuts the box b between the flow positions y0 and y1.
func uncut(e *engine, b *box, y0, y1 float64) bool {
	for a := b.parent; a != nil; a = a.parent {
		if a.mc == nil {
			continue
		}
		if s := setOf(a, b); s != nil {
			return s.fragOf(a, y0+eps) == s.fragOf(a, y1-eps)
		}
		break
	}
	return e.pageOf(y0+eps) == e.pageOf(y1-eps)
}

// fragmentEnd returns the flow position the fragmentainer y lies in
// ends at: the column of the nearest multi-column ancestor of b, or the
// page.
func fragmentEnd(e *engine, b *box, y float64) float64 {
	for a := b.parent; a != nil; a = a.parent {
		if a.mc == nil {
			continue
		}
		if s := setOf(a, b); s != nil && len(s.frags) > 0 {
			_, end := s.band(a, s.fragOf(a, y+eps))
			return end
		}
		break
	}
	return e.nextPage(y + eps)
}

// ruleX returns the offset from the first column's left edge of the
// middle of the gap after column j of a row.
func (d *mcolData) ruleX(j int) float64 {
	if d.xs != nil {
		x, w := d.track(j)
		gap := d.gapAfter(j)
		if d.rtl {
			return d.span - x - w - float64(gap/2)
		}
		return x + w + float64(gap/2)
	}
	if d.rtl {
		return d.columnX(j) - float64(d.gap/2)
	}
	return d.columnX(j) + d.w + float64(d.gap/2)
}

// moveItem returns a copy of the display item it with its extents, and
// those of the items it holds, moved down by dy.
func moveItem(it *ditem, dy float64, depth int) *ditem {
	cp := *it
	cp.y0, cp.y1 = it.y0+dy, it.y1+dy
	if !it.group {
		if cp.page >= 0 {
			cp.page = pageMono
		}
		return &cp
	}
	if depth >= maxGroupDepth {
		cp.kids = nil
		return &cp
	}
	cp.kids = make([]*ditem, len(it.kids))
	for i, k := range it.kids {
		cp.kids[i] = moveItem(k, dy, depth+1)
	}
	return &cp
}

// fragments returns the border boxes b paints its background and borders
// in: its own, sliced at the breaks, or with box-decoration-break: clone
// one per column of the column set being painted it crosses, each with
// all its edges (CSS Fragmentation 3, 5.4); pages slice it, as Chromium
// prints it. columns reports one border box per column.
func (p *painter) fragments(b *box) (frags []typeset.Rect, columns bool) {
	r := p.borderRect(b)
	if !clones(b.st) || b.parent == nil {
		return []typeset.Rect{r}, false
	}
	var top func(i int) (float64, float64)
	var at func(y float64) int
	var frag func(i int) colFrag
	var root *box
	for _, f := range slices.Backward(p.frames) {
		if !f.columns || f.set == nil || len(f.set.frags) == 0 {
			continue
		}
		c, s := f.root, f.set
		at = func(y float64) int { return s.fragOf(c, y) }
		top = func(i int) (float64, float64) { return s.band(c, i) }
		frag = func(i int) colFrag { return s.frags[min(max(i, 0), len(s.frags)-1)] }
		root = c
		break
	}
	if at == nil {
		if b.st.BoxDecorationBreak == css.DecorationCloneAll {
			return p.pageFragments(r), false
		}
		return []typeset.Rect{r}, false
	}
	i0, i1 := at(r.Y-p.dy+eps), at(r.Y+r.Height-p.dy-eps)
	if i1 <= i0 || i1-i0 > maxColumns {
		return []typeset.Rect{r}, false
	}
	out := make([]typeset.Rect, 0, i1-i0+1)
	outerTop, outerBottom := outerClones(b, root)
	for i := i0; i <= i1; i++ {
		y0, y1 := top(i)
		y0, y1 = max(y0+p.dy, r.Y), min(y1+p.dy, r.Y+r.Height)
		// A fragment reaches over the room its column keeps for the
		// cloned edges of b and of the boxes inside it, not over the
		// room of the boxes around b ([engine.enterClone]).
		if f := frag(i); i > i0 {
			y0 -= max(f.lead-outerTop, 0)
		}
		if f := frag(i); i < i1 {
			y1 += max(f.tail-outerBottom, 0)
		}
		if y1 > y0 {
			out = append(out, typeset.Rect{X: r.X, Y: y0, Width: r.Width, Height: y1 - y0})
		}
	}
	return out, true
}

// outerClones returns the top and bottom borders and padding of the
// boxes between b and the multi-column container root whose cloned edges
// take room in the columns they cross ([engine.enterClone]).
func outerClones(b, root *box) (top, bottom float64) {
	for a, n := b.parent, 0; a != nil && a != root && n < maxTreeDepth; a, n = a.parent, n+1 {
		if clones(a.st) && a.st.Height.Kind == css.Auto {
			top += a.bd[css.Top] + a.pd[css.Top]
			bottom += a.bd[css.Bottom] + a.pd[css.Bottom]
		}
	}
	return top, bottom
}

// paintWhole paints one column fragment of box b, whose decorations clone,
// by paint, and marks what it adds to the display list as lying whole in
// the column of its extent: the shadow and the outline round a fragment
// reach past its column's slice and stay with it rather than be cut at
// the column's edge (CSS Fragmentation 3, 5.4: each fragment is wrapped
// independently).
func (p *painter) paintWhole(b *box, paint func()) {
	c := p.sync(p.chain(b, false))
	n := len(c.kids)
	paint()
	for _, k := range c.kids[n:] {
		wholeItem(k, 0)
	}
}

// wholeItem marks the leaves of the display item it as lying whole in the
// column their extent's middle is in.
func wholeItem(it *ditem, depth int) {
	if !it.group {
		it.page = pageMono
		return
	}
	if depth >= maxGroupDepth {
		return
	}
	for _, k := range it.kids {
		wholeItem(k, depth+1)
	}
}
