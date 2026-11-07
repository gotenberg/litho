package layout

import (
	"slices"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// This file lays out grid containers (CSS Grid 1). Box generation makes
// every in-flow child of a grid container a grid item, a run of text an
// anonymous one (section 6.1), sorted in order-modified document order
// (6.3). The grid formatter resolves the explicit grid with its auto
// repeats against the available space (7), places the items (8, in
// grid_place.go), sizes the columns then the rows with the track sizing
// algorithm (11, in grid_track.go), distributes the free space with
// justify-content and align-content (10.5), and lays each item out in its
// grid area, aligned with justify-self and align-self, auto margins and
// baselines (10.3 to 10.6). Rows are laid out top down where they land on
// the page cuts: a row whose items cannot start on its page moves to the
// next one, and the struts its items' content takes make it taller (CSS
// Grid 1, 12). An absolutely positioned child whose containing block is
// the grid container takes the grid area its placement names (9). Only
// the horizontal writing mode is laid out, left to right or right to
// left; subgrids and masonry are not.

// gridData is the grid layout state of a grid container.
type gridData struct {
	// fd holds the measurements of the items, shared with the code that
	// measures flex items.
	fd *flexData
	// place is the placement made for the auto repeat counts rep.
	place *gridPlacement
	rep   [2]int
	// lines are the positions of the grid lines from the content box's
	// top-left corner, per axis, set by the last layout, for the
	// absolutely positioned children.
	lines [2][]float64
	// first and last are the container's first and last baselines below
	// its border box top, valid when hasFirst and hasLast.
	first, last       float64
	hasFirst, hasLast bool
}

// isItem reports a flex or grid item, which paints as an inline block
// does and creates a stacking context with a z-index (CSS Flexbox 1, 4.3;
// CSS Grid 1, 6.3).
func (b *box) isItem() bool { return b.flexItem || b.gridItem }

// genGrid generates the children of the grid container b (CSS Grid 1,
// 6.1): as a flex container's, each in-flow child box an item and each run
// of text an anonymous one, sorted in order-modified document order.
func (e *engine) genGrid(b *box, children []*Node, ctx genCtx) {
	e.genFlex(b, children, ctx)
	for _, k := range b.kids {
		if k.flexItem {
			k.flexItem, k.gridItem = false, true
		}
	}
	b.flex = nil
	b.grid = &gridData{}
}

// isGrid reports a grid container's display value.
func isGrid(d css.Display) bool { return d == css.Grid || d == css.InlineGrid }

// gridFormatter lays out the content of a grid container.
type gridFormatter struct{}

func (gridFormatter) layout(e *engine, b *box, cb cblock, _ *bfc, cursor float64, _ *marginState) float64 {
	return e.layoutGrid(b, cb, cursor)
}

// intrinsic returns the min- and max-content widths of a grid container:
// the sums of its columns sized under a min-content and a max-content
// constraint (CSS Grid 1, 11.1 and 11.5).
func (gridFormatter) intrinsic(e *engine, b *box) (float64, float64) {
	items := gridKids(b)
	st := b.st
	gap := gapOf(st.ColumnGap, 0, false)
	mn, mx := 0.0, inf
	if st.MinWidth.Kind == css.Points {
		mn, _ = specWidth(b, st.MinWidth, 0)
	}
	if st.MaxWidth.Kind == css.Points {
		mx, _ = specWidth(b, st.MaxWidth, 0)
	}
	minH, maxH := heightBounds(b)
	rowGap := gapOf(st.RowGap, 0, false)
	rep := [2]int{
		repeatCount(st.GridTemplateRows, 0, false, minH, maxH, rowGap),
		repeatCount(st.GridTemplateColumns, 0, false, mn, mx, gap),
	}
	p := b.grid.placement(b, items, rep)
	out := [2]float64{}
	for i, mode := range []sizing{sizeMin, sizeMax} {
		tracks := p.tracks(colAxis, 0, false)
		z := &gridSizer{tracks: tracks, gap: gap, mode: mode, ax: colAxis, contrib: func(it *gitem, k contribKind) float64 {
			return e.colContrib(it, k, tracks)
		}}
		z.run(p.items, 0, false, 0, inf, false)
		out[i] = z.sum()
	}
	return out[0], max(out[1], out[0])
}

// gridKids returns the grid items of b, in order-modified document order.
func gridKids(b *box) []*box {
	var out []*box
	for _, k := range b.kids {
		if k.gridItem {
			out = append(out, k)
		}
	}
	return out
}

// placement returns the placement of the items of b for the auto repeat
// counts rep, cached until they change.
func (g *gridData) placement(b *box, items []*box, rep [2]int) *gridPlacement {
	if g.place == nil || g.rep != rep {
		g.place = placeGrid(b, items, rep)
		g.rep = rep
	}
	return g.place
}

// tracks returns the tracks of axis ax of the implicit grid, percentages
// resolved against ref when def; the empty repeated tracks of auto-fit
// collapse (CSS Grid 1, 7.2.3.2).
func (p *gridPlacement) tracks(ax int, ref float64, def bool) []*gtrack {
	a := p.axes[ax]
	used := make([]bool, p.n[ax])
	if a.fit {
		for _, it := range p.items {
			for i := it.start[ax]; i < it.end[ax] && i < len(used); i++ {
				used[i] = true
			}
		}
	}
	out := make([]*gtrack, p.n[ax])
	for i := range out {
		j := i - p.off[ax]
		var s css.TrackSize
		switch {
		case j >= 0 && j < len(a.sizes):
			s = a.sizes[j]
		case j >= len(a.sizes):
			s = implicitSize(a.implicit, j-len(a.sizes))
		default:
			s = implicitSize(a.implicit, j)
		}
		t := newTrack(s, ref, def)
		if a.fit && a.autoAt >= 0 && j >= a.autoAt && j < a.autoAt+a.autoN && !used[i] {
			t.collapsed = true
		}
		out[i] = t
	}
	return out
}

// outerW returns the horizontal padding, borders and non-auto margins of
// k against cbw.
func outerW(k *box, cbw float64) float64 {
	pd, bd, m := edgesOf(k, cbw)
	out := pd[css.Left] + pd[css.Right] + bd[css.Left] + bd[css.Right]
	for _, s := range []css.Side{css.Left, css.Right} {
		if k.st.Margin[s].Kind != css.Auto {
			out += m[s]
		}
	}
	return out
}

// outerH returns the vertical padding, borders and non-auto margins of k
// against cbw.
func outerH(k *box, cbw float64) float64 {
	pd, bd, m := edgesOf(k, cbw)
	out := pd[css.Top] + pd[css.Bottom] + bd[css.Top] + bd[css.Bottom]
	for _, s := range []css.Side{css.Top, css.Bottom} {
		if k.st.Margin[s].Kind != css.Auto {
			out += m[s]
		}
	}
	return out
}

// autoMinApplies reports whether the automatic minimum size of the item
// it applies in the axis whose tracks are tracks (CSS Grid 1, 6.6): its
// minimum is auto, it is not a scroll container, and it spans an auto
// minimum track, crossing no flexible track when it spans more than one.
func autoMinApplies(it *gitem, ax int, tracks []*gtrack, minAutoSet bool) bool {
	if !minAutoSet || it.b.st.Overflow != css.OverflowVisible {
		return false
	}
	autoMin, flex := false, false
	for i := it.start[ax]; i < it.end[ax] && i < len(tracks); i++ {
		autoMin = autoMin || tracks[i].min.Kind == css.TrackAuto
		flex = flex || tracks[i].flexible()
	}
	return autoMin && (it.span(ax) == 1 || !flex)
}

// fixedSpan returns the sum of the fixed maximums of the tracks [s, e)
// and their gaps, false when one is not fixed: the stretch fit the
// content-based minimum clamps to (CSS Grid 1, 6.6).
func fixedSpan(tracks []*gtrack, s, e int, gap float64) (float64, bool) {
	sum := gap * float64(max(e-s-1, 0))
	for i := s; i < e && i < len(tracks); i++ {
		t := tracks[i]
		if t.max.Kind != css.TrackLength {
			return 0, false
		}
		sum += t.max.Value
	}
	return sum, true
}

// colAutoMin returns the content box width the automatic minimum size of
// the item k gives it (CSS Grid 1, 6.6): the smaller of its min-content
// width and its specified width, within its max-width.
func (e *engine) colAutoMin(k *box) float64 {
	var c float64
	switch {
	case k.kind == kindReplaced:
		c, _ = e.replacedSize(k, cblock{})
	case k.tpart == partWrapper:
		c, _ = e.intrinsic(k)
		c = max(c-hEdges(k), 0)
	default:
		c, _ = e.contentWidths(k)
	}
	if w, ok := specWidth(k, k.st.Width, 0); ok && k.st.Width.Kind == css.Points {
		c = min(c, w)
	}
	if mx, ok := specWidth(k, k.st.MaxWidth, 0); ok && k.st.MaxWidth.Kind == css.Points {
		c = min(c, mx)
	}
	return c
}

// colContrib returns the outer contribution of kind k of the item it to
// the columns tracks (CSS Grid 1, 11.5): its min- or max-content
// contribution, or its minimum contribution, the outer size its used
// minimum width gives it.
func (e *engine) colContrib(it *gitem, k contribKind, tracks []*gtrack) float64 {
	b := it.b
	switch k {
	case contribMinContent, contribMaxContent:
		lo, hi := e.contribution(b)
		if k == contribMinContent {
			return lo
		}
		return hi
	case contribMinimum:
	}
	out := outerW(b, 0)
	if autoMinApplies(it, colAxis, tracks, b.st.MinWidthAuto) {
		return e.colAutoMin(b) + out
	}
	if mn, ok := specWidth(b, b.st.MinWidth, 0); ok && b.st.MinWidth.Kind == css.Points {
		return mn + out
	}
	return out
}

// gridLayout is the state of one grid container layout.
type gridLayout struct {
	e   *engine
	b   *box
	p   *gridPlacement
	cb  cblock
	rtl bool
	// tracks and gaps per axis; inner the content box size per axis.
	tracks [2][]*gtrack
	gap    [2]float64
	inner  [2]float64
	// measured content heights of the items at their used widths, and
	// their used widths.
	widths map[*gitem]float64
	ms     map[*gitem]itemMeasure
	// shims are the baseline offsets of the items aligned on a shared
	// baseline in their row (CSS Grid 1, 10.6).
	shims map[*gitem]float64
}

// layoutGrid lays out the grid container b's content in its content box
// cb, from top down, and returns the bottom of its content (CSS Grid 1).
func (e *engine) layoutGrid(b *box, cb cblock, top float64) float64 {
	g := b.grid
	g.hasFirst, g.hasLast = false, false
	if g.fd == nil {
		g.fd = &flexData{measured: map[measureKey]itemMeasure{}, current: map[*box]measureKey{}}
	}
	st := b.st
	minH, maxH := heightBounds(b)
	pcb := cb
	if pcb.hDefinite {
		pcb.h = max(min(pcb.h, maxH), minH)
	}
	l := &gridLayout{e: e, b: b, cb: pcb, rtl: st.Direction == css.RTL,
		widths: map[*gitem]float64{}, ms: map[*gitem]itemMeasure{}, shims: map[*gitem]float64{}}
	l.gap[colAxis] = gapOf(st.ColumnGap, cb.w, true)
	l.gap[rowAxis] = gapOf(st.RowGap, pcb.h, pcb.hDefinite)
	var items []*box
	for _, k := range b.kids {
		if e.err != nil {
			return top
		}
		if k.abs {
			// The static position of an absolutely positioned child is
			// the content box's start corner (CSS Grid 1, 9.2).
			x := cb.x
			if l.rtl {
				x = cb.x + cb.w
			}
			setStatic(k, x, top, l.rtl)
			continue
		}
		if k.gridItem {
			k.laidOut = true
			items = append(items, k)
		}
	}
	rep := [2]int{
		repeatCount(st.GridTemplateRows, pcb.h, pcb.hDefinite, minH, maxH, l.gap[rowAxis]),
		repeatCount(st.GridTemplateColumns, cb.w, true, 0, inf, l.gap[colAxis]),
	}
	p := g.placement(b, items, rep)
	l.p = p
	slices.SortStableFunc(p.items, gridOrder)

	// Columns (CSS Grid 1, 11.1, step 1), against the content box width.
	cols := p.tracks(colAxis, cb.w, true)
	l.tracks[colAxis] = cols
	zc := &gridSizer{tracks: cols, gap: l.gap[colAxis], mode: sizeAvail, ax: colAxis, contrib: func(it *gitem, k contribKind) float64 {
		return e.colContrib(it, k, cols)
	}}
	jc := st.JustifyContent.Keyword()
	zc.run(p.items, cb.w, true, 0, inf, jc == css.BoxNormal || jc == css.BoxStretch)
	l.inner[colAxis] = cb.w
	colPos := l.position(colAxis, st.JustifyContent, cb.w)

	// Rows (step 2), from the items laid out at their column widths.
	rows := p.tracks(rowAxis, pcb.h, pcb.hDefinite)
	l.tracks[rowAxis] = rows
	for _, it := range p.items {
		if e.err != nil {
			return top
		}
		l.measure(it, top)
	}
	l.baselineShims()
	zr := &gridSizer{tracks: rows, gap: l.gap[rowAxis], mode: sizeAvail, ax: rowAxis, contrib: func(it *gitem, k contribKind) float64 {
		return l.rowContrib(it, k)
	}}
	ac := st.AlignContent.Keyword()
	zr.run(p.items, pcb.h, pcb.hDefinite, minH, maxH, ac == css.BoxNormal || ac == css.BoxStretch)
	innerH := pcb.h
	if !pcb.hDefinite {
		innerH = max(min(zr.sum(), maxH), minH)
	}
	l.inner[rowAxis] = innerH
	rowPos := l.position(rowAxis, st.AlignContent, innerH)

	end := l.place(top, colPos, rowPos)
	l.baselines()
	return end
}

// position places the tracks of axis ax in a content box of size size,
// the free space distributed by the content alignment a (CSS Grid 1,
// 10.5; CSS Box Alignment 3, 5.1), and returns the line positions from
// the content box's start edge: the gaps between non-collapsed tracks
// collapse around a collapsed one (CSS Grid 1, 7.2.3.2).
func (l *gridLayout) position(ax int, a css.BoxAlign, size float64) []float64 {
	tracks := l.tracks[ax]
	n := 0
	used := 0.0
	for _, t := range tracks {
		if !t.collapsed {
			n++
			used += t.base
		}
	}
	used += l.gap[ax] * float64(max(n-1, 0))
	free := size - used
	kw := a.Keyword()
	if kw == css.BoxNormal || kw == css.BoxStretch {
		a = css.BoxStart
	}
	off, between := distribute(a, free, n, true, ax == colAxis, ax == colAxis && l.rtl)
	if (kw == css.BoxSpaceBetween || kw == css.BoxSpaceAround || kw == css.BoxSpaceEvenly) && free < 0 {
		// A distributed alignment that overflows falls back to start for
		// space-between, center otherwise (CSS Box Alignment 3, 5.1).
		off, between = 0, 0
		if kw != css.BoxSpaceBetween {
			off = free / 2
		}
	}
	lines := make([]float64, len(tracks)+1)
	cur := off
	seen := false
	for i, t := range tracks {
		if !t.collapsed && seen {
			cur += l.gap[ax] + between
		}
		lines[i] = cur
		t.start = cur
		if !t.collapsed {
			cur += t.base
			seen = true
		}
	}
	lines[len(tracks)] = cur
	return lines
}

// colArea returns the width of the item's column area from the sized
// columns.
func (l *gridLayout) colArea(it *gitem) float64 {
	t := l.tracks[colAxis]
	s, e := it.start[colAxis], it.end[colAxis]
	if s < 0 || e > len(t) || e <= s {
		return 0
	}
	sum := 0.0
	n := 0
	for i := s; i < e; i++ {
		sum += t[i].base
		if !t[i].collapsed {
			n++
		}
	}
	return sum + l.gap[colAxis]*float64(max(n-1, 0))
}

// justifySelf returns the used justify-self of the item k of the grid
// container c: auto takes the container's justify-items (CSS Box
// Alignment 3, 6.1).
func justifySelf(k, c *css.Style) css.BoxAlign {
	a := k.JustifySelf
	if a.Keyword() == css.BoxAuto {
		a = c.JustifyItems
	}
	return a
}

// alignSelfOf returns the used align-self of the item k of the grid
// container c.
func alignSelfOf(k, c *css.Style) css.BoxAlign {
	a := k.AlignSelf
	if a.Keyword() == css.BoxAuto {
		a = c.AlignItems
	}
	return a
}

// stretches reports an alignment a that stretches an auto-sized box of
// kind replaced: stretch, and normal for a non-replaced box (CSS Box
// Alignment 3, 6.1; CSS Grid 1, 6.2 as Chromium sizes replaced items).
func stretches(a css.BoxAlign, replaced bool) bool {
	switch a.Keyword() { //nolint:exhaustive // the other values do not stretch
	case css.BoxStretch:
		return true
	case css.BoxNormal, css.BoxAuto:
		return !replaced
	}
	return false
}

// itemWidth sets the edges of the item it against its column area of
// width aw and returns its content width: its width, stretched to the
// area, or its fit-content width, within its min-width, max-width and
// automatic minimum (CSS Grid 1, 6.2 and 6.6).
func (l *gridLayout) itemWidth(it *gitem, aw float64) float64 {
	e := l.e
	k := it.b
	resolveEdges(k, aw)
	st := k.st
	autoMargin := st.Margin[css.Left].Kind == css.Auto || st.Margin[css.Right].Kind == css.Auto
	room := max(aw-hEdges(k)-nonAuto(k.m[css.Left], st.Margin[css.Left].Kind == css.Auto)-nonAuto(k.m[css.Right], st.Margin[css.Right].Kind == css.Auto), 0)
	acb := cblock{w: aw}
	var w float64
	sw, set := specWidth(k, st.Width, aw)
	switch {
	case k.tpart == partWrapper:
		w = e.wrapperWidth(k, aw, aw)
	case k.kind == kindReplaced:
		if !set && stretches(justifySelf(st, l.b.st), true) && !autoMargin {
			w = constrainWidth(k, room, aw)
		} else {
			w, _ = e.replacedSize(k, acb)
		}
	case set:
		w = sw
	case stretches(justifySelf(st, l.b.st), false) && !autoMargin:
		w = room
	default:
		w = e.shrinkToFit(k, aw)
	}
	if k.kind != kindReplaced && k.tpart != partWrapper {
		w = constrainWidth(k, w, aw)
	}
	if autoMinApplies(it, colAxis, l.tracks[colAxis], st.MinWidthAuto) && k.st.Width.Kind != css.Points && k.st.Width.Kind != css.Percent {
		mn := e.colAutoMin(k)
		if _, ok := fixedSpan(l.tracks[colAxis], it.start[colAxis], it.end[colAxis], l.gap[colAxis]); ok {
			mn = min(mn, room)
		}
		w = max(w, mn)
	}
	return w
}

// measure lays the item it out off the page cuts at its used width in
// its column area and records its content height and baselines.
func (l *gridLayout) measure(it *gitem, top float64) {
	e := l.e
	k := it.b
	aw := l.colArea(it)
	w := l.itemWidth(it, aw)
	l.widths[it] = w
	if k.kind == kindReplaced {
		_, h := e.replacedSize(k, cblock{w: aw})
		if r := ratioOf(k); r > 0 && k.st.Height.Kind == css.Auto {
			h = w / r
		}
		if hh, ok := specHeight(k, k.st.Height, cblock{}); ok {
			h = hh
		}
		l.ms[it] = itemMeasure{h: constrainHeight(k, h, cblock{})}
		return
	}
	pcb := cblock{x: l.cb.x, w: aw}
	h, hDef := specHeight(k, k.st.Height, pcb)
	m := e.measureItem(l.b.grid.fd, &fitem{b: k}, w, h, hDef, pcb, l.cb.x, top)
	l.ms[it] = m
}

// usedHeight returns the content height of the item it laid out off the
// page cuts: its specified height or its content's, within its
// min-height and max-height, in an area of height ah when def.
func (l *gridLayout) usedHeight(it *gitem, ah float64, def bool) float64 {
	k := it.b
	acb := cblock{w: l.colArea(it), h: ah, hDefinite: def}
	h := l.ms[it].h
	if hh, ok := specHeight(k, k.st.Height, acb); ok {
		h = hh
	}
	return constrainHeight(k, h, acb)
}

// rowContrib returns the outer contribution of kind kd of the item it to
// the rows: in the block axis its min- and max-content contributions are
// both its height laid out at its width, and its minimum contribution
// its automatic minimum height or its min-height (CSS Grid 1, 11.5 and
// 6.6). A baseline-aligned item adds its shim.
func (l *gridLayout) rowContrib(it *gitem, kd contribKind) float64 {
	k := it.b
	aw := l.colArea(it)
	out := outerH(k, aw) + l.shims[it]
	if kd != contribMinimum {
		return l.usedHeight(it, 0, false) + out
	}
	if autoMinApplies(it, rowAxis, l.tracks[rowAxis], k.st.MinHeightAuto) {
		c := l.ms[it].h
		if h, ok := specHeight(k, k.st.Height, cblock{}); ok {
			c = min(c, h)
		}
		if mx, ok := specHeight(k, k.st.MaxHeight, cblock{}); ok {
			c = min(c, mx)
		}
		return c + out
	}
	if mn, ok := specHeight(k, k.st.MinHeight, cblock{}); ok {
		return mn + out
	}
	return out
}

// baselineAligned reports an item aligned on its row's first baseline,
// and last when on its last one (CSS Grid 1, 10.6).
func (l *gridLayout) baselineAligned(it *gitem) (bool, bool) {
	st := it.b.st
	if st.Margin[css.Top].Kind == css.Auto || st.Margin[css.Bottom].Kind == css.Auto {
		return false, false
	}
	switch alignSelfOf(st, l.b.st).Keyword() { //nolint:exhaustive // the other values align otherwise
	case css.BoxBaseline:
		return true, false
	case css.BoxLastBaseline:
		return true, true
	}
	return false, false
}

// ascent returns the distance from the item's margin box top to its first
// baseline, its border box bottom when it has none.
func (l *gridLayout) ascent(it *gitem) float64 {
	k := it.b
	m := l.ms[it]
	if m.hasFirst {
		return k.m[css.Top] + m.first
	}
	return k.m[css.Top] + l.usedHeight(it, 0, false) + vEdges(k)
}

// baselineShims sets the shims of the items aligned on the first baseline
// of the row they start in: each moves down by the largest ascent of the
// group less its own (CSS Grid 1, 10.6; CSS Box Alignment 3, 9.3).
func (l *gridLayout) baselineShims() {
	groups := map[int][]*gitem{}
	var rows []int
	for _, it := range l.p.items {
		if ok, last := l.baselineAligned(it); ok && !last {
			r := it.start[rowAxis]
			if _, seen := groups[r]; !seen {
				rows = append(rows, r)
			}
			groups[r] = append(groups[r], it)
		}
	}
	slices.Sort(rows)
	for _, r := range rows {
		g := groups[r]
		asc := 0.0
		for _, it := range g {
			asc = max(asc, l.ascent(it))
		}
		for _, it := range g {
			l.shims[it] = asc - l.ascent(it)
		}
	}
}

// selfOffset returns the offset of an item's margin box from its area's
// start edge, along an axis whose free space is free, for the self
// alignment a; inline reports the inline axis, in which left and right
// apply and rtl reverses start and end; selfRTL is the item's own
// direction, for self-start and self-end.
func selfOffset(a css.BoxAlign, free float64, inline, rtl, selfRTL bool) float64 {
	kw := a.Keyword()
	if a.Safe() && free < 0 {
		return 0
	}
	switch kw { //nolint:exhaustive // the other values align at the start
	case css.BoxCenter:
		return free / 2
	case css.BoxEnd, css.BoxFlexEnd, css.BoxLastBaseline:
		return free
	case css.BoxSelfStart, css.BoxSelfEnd:
		atStart := kw == css.BoxSelfStart
		if inline && selfRTL != rtl {
			atStart = !atStart
		}
		if atStart {
			return 0
		}
		return free
	case css.BoxLeft, css.BoxRight:
		if !inline {
			return 0
		}
		if (kw == css.BoxLeft) == rtl {
			return free
		}
		return 0
	}
	return 0
}

// place lays the items out in their areas row by row from top, the
// columns at colPos and the rows at rowPos, and returns the bottom of the
// container's content. A row moves to the next page after a forced break
// between rows, or when one of its items cannot start on the page the row
// starts on; the struts its items' content takes at page cuts make it
// taller and push the rows below (CSS Grid 1, 12).
func (l *gridLayout) place(top float64, colPos, rowPos []float64) float64 {
	e := l.e
	p := l.p
	nRows := p.n[rowAxis]
	byRow := make([][]*gitem, nRows)
	endRow := make([][]*gitem, nRows)
	for _, it := range p.items {
		r := min(max(it.start[rowAxis], 0), max(nRows-1, 0))
		if nRows == 0 {
			break
		}
		byRow[r] = append(byRow[r], it)
		er := min(max(it.end[rowAxis]-1, 0), nRows-1)
		endRow[er] = append(endRow[er], it)
	}
	rows := l.tracks[rowAxis]
	shift := 0.0
	starts := make([]float64, nRows+1)
	bottoms := map[*gitem]float64{}
	for r := range nRows {
		if e.err != nil {
			break
		}
		rowTop := top + rows[r].start + shift
		if e.paginating() && r > 0 && l.forcedBefore(r, byRow, endRow) && !e.atPageTop(rowTop) {
			y := e.nextPage(rowTop)
			shift += y - rowTop
			rowTop = y
		}
		starts[r] = rowTop - top
		for _, it := range byRow[r] {
			bottoms[it] = l.put(it, colPos, rowTop, shift)
		}
		if e.paginating() && !e.atPageTop(rowTop) && l.pushRow(byRow[r], rowTop) && e.relayout(rowTop) {
			y := e.nextPage(rowTop)
			shift += y - rowTop
			rowTop = y
			starts[r] = rowTop - top
			for _, it := range byRow[r] {
				bottoms[it] = l.put(it, colPos, rowTop, shift)
			}
		}
		// The row grows to hold the items ending in it as laid out.
		rowBottom := top + rows[r].start + rows[r].base + shift
		grow := 0.0
		for _, it := range endRow[r] {
			if y, ok := bottoms[it]; ok {
				grow = max(grow, y-rowBottom)
			}
		}
		if grow > eps && e.paginating() {
			shift += grow
		}
	}
	// The lines, from the content box's top-left corner, for the
	// absolutely positioned children.
	g := l.b.grid
	g.lines[colAxis] = l.physLines(colPos)
	ys := make([]float64, nRows+1)
	for r := range nRows {
		ys[r] = starts[r]
	}
	if nRows > 0 {
		ys[nRows] = starts[nRows-1] + rows[nRows-1].base
	} else if len(rowPos) > 0 {
		ys[0] = rowPos[0]
	}
	g.lines[rowAxis] = ys
	return top + l.inner[rowAxis] + shift
}

// physLines returns the column lines as offsets from the content box's
// left edge: mirrored right to left.
func (l *gridLayout) physLines(pos []float64) []float64 {
	out := slices.Clone(pos)
	if l.rtl {
		for i := range out {
			out[i] = l.cb.w - out[i]
		}
	}
	return out
}

// forcedBefore reports a forced break between row r-1 and row r: a
// break-after on an item ending in row r-1 or a break-before on one
// starting in row r (CSS Grid 1, 12).
func (l *gridLayout) forcedBefore(r int, byRow, endRow [][]*gitem) bool {
	for _, it := range endRow[r-1] {
		if isForced(it.b.st.BreakAfter) {
			return true
		}
	}
	for _, it := range byRow[r] {
		if isForced(it.b.st.BreakBefore) {
			return true
		}
	}
	return false
}

// pushRow reports a row laid out at rowTop that must move to the next
// page: one of its items is monolithic or avoids breaks inside, crosses
// a page cut and fits a page, or its content starts on a later page than
// the row, as Chromium starts the items of a row on the same page.
func (l *gridLayout) pushRow(items []*gitem, rowTop float64) bool {
	e := l.e
	page := e.pageOf(rowTop)
	for _, it := range items {
		k := it.b
		if (k.kind == kindReplaced || avoidsInside(k)) && e.straddles(k) && k.borderH() <= e.areaH(page+1) {
			return true
		}
		if k.kind != kindReplaced && hasContent(k) && e.pageOf(firstContent(k)) > page {
			return true
		}
	}
	return false
}

// put lays the item it out in its area, the area's top at rowTop and the
// rows below shifted by shift, and returns the bottom of its margin box.
func (l *gridLayout) put(it *gitem, colPos []float64, rowTop, shift float64) float64 {
	e := l.e
	k := it.b
	st := k.st
	rows := l.tracks[rowAxis]
	r0, r1 := it.start[rowAxis], it.end[rowAxis]
	ah := 0.0
	if r1 > r0 && r1 <= len(rows) {
		ah = max(rows[r1-1].start+rows[r1-1].base-rows[r0].start, 0)
	}
	aw := l.colArea(it)
	w := l.widths[it]
	if it.start[colAxis] < 0 || it.start[colAxis] >= len(l.tracks[colAxis]) {
		return rowTop
	}
	ax0 := l.tracks[colAxis][it.start[colAxis]].start
	resolveEdges(k, aw)
	acb := cblock{x: l.cb.x, w: aw, h: ah, hDefinite: true}

	// Inline axis: auto margins take the free space, else justify-self.
	outerWd := w + hEdges(k) + k.m[css.Left] + k.m[css.Right]
	free := aw - outerWd
	lAuto, rAuto := st.Margin[css.Left].Kind == css.Auto, st.Margin[css.Right].Kind == css.Auto
	off := 0.0
	switch {
	case (lAuto || rAuto) && free > 0:
		switch {
		case lAuto && rAuto:
			k.m[css.Left] += free / 2
			k.m[css.Right] += free / 2
		case lAuto:
			k.m[css.Left] += free
		default:
			k.m[css.Right] += free
		}
		free = 0
	case lAuto || rAuto:
	default:
		off = selfOffset(justifySelf(st, l.b.st), free, true, l.rtl, st.Direction == css.RTL)
	}
	// The logical offset from the start edge maps onto the physical axis.
	var x float64
	if l.rtl {
		x = l.cb.x + l.cb.w - ax0 - off - outerWd + k.m[css.Left]
		if lAuto || rAuto {
			x = l.cb.x + l.cb.w - ax0 - aw + k.m[css.Left]
		}
	} else {
		x = l.cb.x + ax0 + off + k.m[css.Left]
	}

	// Block axis: stretched, specified or content height.
	a := alignSelfOf(st, l.b.st)
	tAuto, bAuto := st.Margin[css.Top].Kind == css.Auto, st.Margin[css.Bottom].Kind == css.Auto
	vOut := vEdges(k) + k.m[css.Top] + k.m[css.Bottom]
	sh, hSet := specHeight(k, st.Height, acb)
	h := l.usedHeight(it, ah, true)
	stretch := !hSet && st.Height.Kind != css.Points && !tAuto && !bAuto && stretches(a, k.kind == kindReplaced)
	if bl, _ := l.baselineAligned(it); bl {
		stretch = false
	}
	if stretch {
		h = constrainHeight(k, max(ah-vOut, 0), acb)
		if k.kind == kindReplaced && st.Width.Kind == css.Auto && !stretches(justifySelf(st, l.b.st), true) {
			if r := ratioOf(k); r > 0 {
				w = constrainWidth(k, h*r, aw)
			}
		}
	} else if hSet {
		h = constrainHeight(k, sh, acb)
	}
	vfree := ah - (h + vOut)
	voff := 0.0
	switch {
	case (tAuto || bAuto) && vfree > 0:
		switch {
		case tAuto && bAuto:
			k.m[css.Top] += vfree / 2
			k.m[css.Bottom] += vfree / 2
		case tAuto:
			k.m[css.Top] += vfree
		default:
			k.m[css.Bottom] += vfree
		}
	case tAuto || bAuto:
	default:
		if bl, last := l.baselineAligned(it); bl && !last {
			voff = l.shims[it]
		} else {
			voff = selfOffset(a, vfree, false, false, false)
		}
	}
	y := rowTop + voff + k.m[css.Top]
	pl := placement{e: e, fd: l.b.grid.fd, pcb: acb}
	switch {
	case k.kind == kindReplaced:
		pl.put(k, x, y, w, h, true)
	case hSet || stretch && dependsOnHeight(k):
		pl.put(k, x, y, w, h, true)
		k.h = h
	case stretch:
		got := pl.put(k, x, y, w, 0, false)
		k.h = h + max(got-l.ms[it].h, 0)
	default:
		got := pl.put(k, x, y, w, 0, false)
		k.h = constrainHeight(k, got, acb)
	}
	return k.y + k.borderH() + k.m[css.Bottom]
}

// baselines sets the first and last baselines of the grid container (CSS
// Grid 1, 10.7): those its first and last rows' baseline-aligned items
// share, else the baseline of the first item in grid order of the first
// row, the last item of the last row alike; an item without a baseline
// lends its border box's bottom edge.
func (l *gridLayout) baselines() {
	g := l.b.grid
	items := l.p.items
	if len(items) == 0 {
		return
	}
	of := func(it *gitem, last bool) float64 {
		k := it.b
		if last {
			if v, ok := lastBaseline(k); ok {
				return v
			}
		} else if v, ok := firstBaseline(k); ok {
			return v
		}
		return k.y + k.borderH()
	}
	first := -1
	for _, it := range items {
		if first < 0 || it.start[rowAxis] < first {
			first = it.start[rowAxis]
		}
	}
	var pick *gitem
	for _, it := range items {
		if it.start[rowAxis] != first {
			continue
		}
		if bl, last := l.baselineAligned(it); bl && !last {
			pick = it
			break
		}
		if pick == nil {
			pick = it
		}
	}
	if pick != nil {
		g.first, g.hasFirst = of(pick, false)-l.b.y, true
	}
	lastRow := -1
	for _, it := range items {
		lastRow = max(lastRow, it.end[rowAxis])
	}
	var lp *gitem
	for _, it := range items {
		if it.end[rowAxis] == lastRow {
			lp = it
		}
	}
	if lp != nil {
		g.last, g.hasLast = of(lp, true)-l.b.y, true
	}
}

// gridBaseline returns the first or last baseline of the grid container b
// in flow coordinates.
func gridBaseline(b *box, last bool) (float64, bool) {
	g := b.grid
	if last {
		return b.y + g.last, g.hasLast
	}
	return b.y + g.first, g.hasFirst
}

// gridAbsCB returns the containing block of the absolutely positioned
// child k of the grid container c, whose padding box is pad: the grid
// area its placement properties name, an auto or missing line standing
// for the padding edge (CSS Grid 1, 9.1).
func gridAbsCB(c, k *box, pad absCB) absCB {
	g := c.grid
	if g == nil || g.place == nil {
		return pad
	}
	out := pad
	cx, cy := c.contentX(), c.contentY()
	st := k.st
	// Columns.
	if xs := g.lines[colAxis]; len(xs) > 0 {
		a, b, aOK, bOK := g.place.absLines(colAxis, st.GridColumnStart, st.GridColumnEnd)
		x0, x1 := pad.x, pad.x+pad.w
		left, right := aOK, bOK
		la, lb := a, b
		if c.st.Direction == css.RTL {
			// The start line is on the right.
			left, right = bOK, aOK
			la, lb = b, a
		}
		if left && la < len(xs) {
			x0 = cx + xs[la]
		}
		if right && lb < len(xs) {
			x1 = cx + xs[lb]
		}
		out.x, out.w = x0, max(x1-x0, 0)
	}
	if ys := g.lines[rowAxis]; len(ys) > 0 {
		a, b, aOK, bOK := g.place.absLines(rowAxis, st.GridRowStart, st.GridRowEnd)
		y0, y1 := pad.y, pad.y+pad.h
		if aOK && a < len(ys) {
			y0 = cy + ys[a]
		}
		if bOK && b < len(ys) {
			y1 = cy + ys[b]
		}
		out.y, out.h = y0, max(y1-y0, 0)
	}
	return out
}

