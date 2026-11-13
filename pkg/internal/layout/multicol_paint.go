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
// borders, its column rules, its outside marker and its outline, then each
// column set painted in its column flow and cut at its columns, each
// column moved to its place, and its spanners across the container (CSS
// Multicol 1, 2 and 4). The marker sits by the first line box, whose
// painted place [firstBaseline] already gives, in a column or in a
// spanner: it is painted once, outside the columns. The outline comes
// before those of the boxes inside, as in a box without columns.
func (p *painter) multicol(b *box) {
	if b == p.canvas || b.parent == nil {
		p.borders(b)
	} else {
		p.decorations(b)
	}
	p.linkBox(b)
	p.columnRules(b)
	p.marker(b)
	p.outline(b)
	marker := b.outside
	b.outside = nil
	defer func() { b.outside = marker }()
	for _, s := range b.mc.sets {
		if p.e.err != nil {
			return
		}
		if s.spanner != nil {
			if s.spanner.layered() {
				p.lifted(s.spanner)
			} else {
				p.context(s.spanner)
			}
			continue
		}
		g := &ditem{group: true, opacity: 1}
		nl := len(p.links)
		p.frames = append(p.frames, &frame{container: g, root: b, columns: true, set: s})
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
		c := p.sync(p.chain(b, true))
		cut := p.e.cutColumns(g, b, s, p.dy)
		if mono >= 0 {
			onPage(cut, mono, 0)
		}
		c.kids = append(c.kids, cut...)
		moved := cutLinks(slices.Clone(p.links[nl:]), b, s, p.dy)
		p.links = append(p.links[:nl], moved...)
		for _, o := range outer {
			sdx, sdy := p.dx, p.dy
			p.dx, p.dy = ancestorOffset(o)
			p.unit(o, true)
			p.dx, p.dy = sdx, sdy
		}
	}
}

// setBody paints the content of the column set whose boxes are the kids of
// b, in the painting order of CSS 2.1, Appendix E, and returns the
// positioned boxes it holds whose place does not follow the columns: the
// fixed ones, and those with a vertical offset from a containing block
// outside b.
func (p *painter) setBody(b *box) []*box {
	var blocks, floats, layers []*box
	p.collect(b, &blocks, &floats, &layers)
	var inner, outer []*box
	for _, l := range layers {
		if l.fixed || l.abs && outsideOf(l, b) && (l.st.Top.Kind != css.Auto || l.st.Bottom.Kind != css.Auto) {
			outer = append(outer, l)
			continue
		}
		inner = append(inner, l)
	}
	neg, zero, pos := p.stack(inner)
	for _, c := range neg {
		p.lifted(c)
	}
	for _, k := range blocks {
		p.linkBox(k)
		p.decorations(k)
	}
	for _, f := range floats {
		p.context(f)
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
	for _, k := range blocks {
		p.outline(k)
	}
	return outer
}

// outsideOf reports an absolutely positioned box whose containing block
// lies outside the box b.
func outsideOf(a, b *box) bool {
	for c := a.cbBox; c != nil; c = c.parent {
		if c == b {
			return false
		}
	}
	return true
}

// piece is a display item cut for one column, and whether it reaches past
// the column's slice, which then clips it.
type piece struct {
	it   *ditem
	clip bool
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
	per, pass := e.cutItem(g, b, s, oy, 0)
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
			for k < len(items) && items[k].clip == items[j].clip {
				k++
			}
			run := make([]*ditem, 0, k-j)
			for _, pc := range items[j:k] {
				run = append(run, pc.it)
			}
			moved := &ditem{group: true, opacity: 1, transform: m, kids: run}
			if items[j].clip {
				moved = &ditem{group: true, opacity: 1, clip: band, kids: []*ditem{moved}}
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
		if !it.fixed && depth < maxGroupDepth {
			onPage(it.kids, page, depth+1)
		}
	}
}

// band returns the column flow slice of column i of s in the container b.
func (s *colSet) band(b *box, i int) (float64, float64) {
	f := s.frags[min(max(i, 0), len(s.frags)-1)]
	return b.y + f.vtop, b.y + f.vtop + f.h
}

// cutItem returns the items of the group it per column of s, groups
// rebuilt in each column they reach, and the fixed groups, which follow
// no column. The extents of the items move to their column's place; oy is
// the relative offset the container paints at.
func (e *engine) cutItem(it *ditem, b *box, s *colSet, oy float64, depth int) (map[int][]piece, []*ditem) {
	out := map[int][]piece{}
	var pass []*ditem
	last := len(s.frags) - 1
	for _, k := range it.kids {
		if k.group && k.fixed {
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
			sub, sp := e.cutItem(k, b, s, oy, depth+1)
			pass = append(pass, sp...)
			for _, i := range slices.Sorted(maps.Keys(sub)) {
				cp := *k
				cp.kids, cp.page = nil, 0
				clip := false
				for _, pc := range sub[i] {
					cp.kids = append(cp.kids, pc.it)
					clip = clip || pc.clip
				}
				out[i] = append(out[i], piece{it: &cp, clip: clip})
			}
			continue
		}
		if k.page >= 0 || k.page == pageMono {
			// A glyph run's extent reaches above and below its line box
			// by its ascent and descent: its middle is on its line.
			i := s.fragOf(b, (k.y0+max(k.y1, k.y0))/2-oy)
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
			out[i] = append(out[i], piece{it: &leaf, clip: k.y0 < top-eps || k.y1 > bottom+eps})
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
// rule follows, unless a page or an outer column cuts b before that edge.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws the rule of
//     a balanced two-column container 300 pixels tall, or 300 pixels at
//     the least, holding 100 pixels of content, 300 pixels long; after a
//     spanner, the last row's rule runs to the content edge and the rows
//     before keep their balanced height.
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
			if top, bottom := b.y+r.top, b.contentY()+b.h; si == len(d.sets)-1 && ri == len(s.rows)-1 && bottom > top+r.used && uncut(p.e, b, top, bottom) {
				h = bottom - top
			}
			for j := 0; j+1 < r.cols; j++ {
				mid := d.ruleX(j)
				rect := typeset.Rect{X: b.contentX() + mid - w/2 + p.dx, Y: b.y + r.top + p.dy, Width: w, Height: h}
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

// ruleX returns the offset from the first column's left edge of the
// middle of the gap after column j of a row.
func (d *mcolData) ruleX(j int) float64 {
	if d.xs != nil {
		x, w := d.track(j)
		gap := d.gapAfter(j)
		if d.rtl {
			return d.span - x - w - gap/2
		}
		return x + w + gap/2
	}
	if d.rtl {
		return d.columnX(j) - d.gap/2
	}
	return d.columnX(j) + d.w + d.gap/2
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
	for _, f := range slices.Backward(p.frames) {
		if !f.columns || f.set == nil || len(f.set.frags) == 0 {
			continue
		}
		c, s := f.root, f.set
		at = func(y float64) int { return s.fragOf(c, y) }
		top = func(i int) (float64, float64) { return s.band(c, i) }
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
	for i := i0; i <= i1; i++ {
		y0, y1 := top(i)
		y0, y1 = max(y0+p.dy, r.Y), min(y1+p.dy, r.Y+r.Height)
		if y1 > y0 {
			out = append(out, typeset.Rect{X: r.X, Y: y0, Width: r.Width, Height: y1 - y0})
		}
	}
	return out, true
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
