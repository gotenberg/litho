package layout

import (
	"slices"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// This file resolves a grid container's explicit grid and places its
// items (CSS Grid 1, 7 and 8): the explicit tracks with their line names
// and the implicit names of the template's areas (7.1 to 7.3), the
// placement properties resolved to lines (8.3.1), and the auto-placement
// algorithm, sparse or dense, row or column (8.5), which grows the
// implicit grid (7.5).

// The axes of a grid, indices of the per-axis arrays.
const (
	rowAxis = 0
	colAxis = 1
)

// gridAxis is the explicit grid of one axis, expanded.
type gridAxis struct {
	// sizes are the explicit tracks, names the names of each explicit
	// line, len(sizes)+1 of them.
	sizes []css.TrackSize
	names [][]string
	// autoAt and autoN are the first repeated track and the number of
	// repeated tracks, autoAt -1 for no auto repeat; fit reports auto-fit.
	autoAt, autoN int
	fit           bool
	// implicit are the grid-auto-rows or grid-auto-columns sizes.
	implicit []css.TrackSize
}

// gridPlacement is the placed grid of a container.
type gridPlacement struct {
	axes [2]gridAxis
	// n is the number of tracks of the implicit grid in each axis, off
	// the index in it of explicit line 1.
	n, off [2]int
	items  []*gitem
}

// gitem is one grid item: its area in the implicit grid, track indices
// [start, end) per axis.
type gitem struct {
	b          *box
	start, end [2]int
}

// span returns the number of tracks the item spans in axis ax.
func (it *gitem) span(ax int) int { return it.end[ax] - it.start[ax] }

// repeatCount returns the number of times the auto repeat of the track
// list l repeats in a content box of size avail, definite when def, or of
// minimum size minSize, maximum size maxSize (CSS Grid 1, 7.2.3.2): the
// largest number that does not overflow a definite size or maximum, the
// smallest that fills a definite minimum, else 1. gap is the gutter.
func repeatCount(l css.TrackList, avail float64, def bool, minSize, maxSize, gap float64) int {
	r := l.Auto
	if r == nil || len(r.Sizes) == 0 {
		return 0
	}
	ref, refDef := avail, def
	limitMax := true
	switch {
	case def:
	case maxSize < inf:
		ref, refDef = maxSize, true
	case minSize > 0:
		ref, refDef, limitMax = minSize, true, false
	}
	if !refDef {
		return 1
	}
	// Each track counts as its maximum sizing function when that is
	// definite, else its minimum one; fit-content() as its argument.
	size := func(s css.TrackSize) float64 {
		b := s.Max
		if !fixedBreadth(b) {
			b = s.Min
		}
		if !fixedBreadth(b) {
			return 0
		}
		return breadthOf(b, ref)
	}
	fixed := 0.0
	for _, s := range l.Sizes {
		fixed += size(s)
	}
	nFixed := len(l.Sizes)
	rep := 0.0
	for _, s := range r.Sizes {
		rep += size(s)
	}
	nRep := len(r.Sizes)
	// total(k) = fixed + k*rep + gaps between nFixed + k*nRep tracks.
	total := func(k int) float64 {
		n := nFixed + k*nRep
		return fixed + float64(k)*rep + gap*float64(max(n-1, 0))
	}
	room := (MaxTracksPerAxis - nFixed) / nRep
	if room < 1 {
		return 1
	}
	if rep+gap*float64(nRep) <= eps {
		// Repetitions take no room: one, as Chromium resolves it.
		return 1
	}
	k := 1
	if limitMax {
		for k < room && total(k+1) <= ref+eps {
			k++
		}
		return k
	}
	for k < room && total(k) < ref-eps {
		k++
	}
	return k
}

// fixedBreadth reports a length or percentage breadth.
func fixedBreadth(b css.TrackBreadth) bool {
	return b.Kind == css.TrackLength || b.Kind == css.TrackPercent
}

// breadthOf returns a fixed breadth's size against ref.
func breadthOf(b css.TrackBreadth, ref float64) float64 {
	if b.Kind == css.TrackPercent {
		return max(b.Value*ref, 0)
	}
	return max(b.Value, 0)
}

// explicitAxis expands the track list l with n auto repetitions, and the
// implicit names the areas give its lines (CSS Grid 1, 7.3.2): an area foo
// names its start lines foo-start and its end lines foo-end. The areas
// also extend the explicit grid to their own size (7.1).
func explicitAxis(l css.TrackList, n int, areas *css.GridAreas, ax int, implicit []css.TrackSize) gridAxis {
	names, sizes, at := l.Expand(n)
	g := gridAxis{sizes: sizes, names: names, autoAt: at, implicit: implicit}
	if l.Auto != nil && at >= 0 {
		g.autoN = n * len(l.Auto.Sizes)
		g.fit = l.Auto.Fit
	}
	if areas != nil {
		count := areas.Rows
		if ax == colAxis {
			count = areas.Cols
		}
		for len(g.sizes) < count {
			g.sizes = append(g.sizes, implicitSize(implicit, len(g.sizes)))
			g.names = append(g.names, nil)
		}
		for _, a := range areas.Areas {
			s, e := a.Row0, a.Row1
			if ax == colAxis {
				s, e = a.Col0, a.Col1
			}
			if s < len(g.names) {
				g.names[s] = append(slices.Clip(g.names[s]), a.Name+"-start")
			}
			if e < len(g.names) {
				g.names[e] = append(slices.Clip(g.names[e]), a.Name+"-end")
			}
		}
	}
	if len(g.sizes) > MaxTracksPerAxis {
		g.sizes = g.sizes[:MaxTracksPerAxis]
		g.names = g.names[:MaxTracksPerAxis+1]
	}
	return g
}

// implicitSize returns the size of the implicit track i places after the
// explicit grid's first line, counting from 0 at that line: the auto
// sizes cycle forward after the explicit grid and backward before it (CSS
// Grid 1, 7.6).
func implicitSize(implicit []css.TrackSize, i int) css.TrackSize {
	if len(implicit) == 0 {
		return css.TrackSize{}
	}
	n := len(implicit)
	return implicit[((i%n)+n)%n]
}

// MaxTracksPerAxis bounds the tracks of a grid's implicit grid in one
// axis: an item placed past it is clamped to its last track. Anti-DoS
// guard, not spec-mandated (CSS Grid 1, 8.3.1 lets implementations clamp
// the implicit grid): the placement keeps one cell per track pair, and
// real grids use some tens of tracks.
const MaxTracksPerAxis = css.MaxGridTracks

// lineSet resolves line names of one axis.
type lineSet struct {
	names [][]string
}

// lines returns the explicit lines, 0-based, called name.
func (l lineSet) lines(name string) []int {
	var out []int
	for i, ns := range l.names {
		if slices.Contains(ns, name) {
			out = append(out, i)
		}
	}
	return out
}

// last returns the index of the explicit grid's last line.
func (l lineSet) last() int { return len(l.names) - 1 }

// nth returns the line n lines after from (before when n < 0) among those
// called name, counting the implicit lines past the explicit grid as
// called name too when the explicit grid has too few (CSS Grid 1, 8.3.1).
// from is exclusive; the result is in explicit line coordinates.
func (l lineSet) nth(name string, n, from int) int {
	ls := l.lines(name)
	if n > 0 {
		k := 0
		for _, i := range ls {
			if i > from {
				k++
				if k == n {
					return i
				}
			}
		}
		return max(l.last(), from) + n - k
	}
	k := 0
	for _, i := range slices.Backward(ls) {
		if i < from {
			k++
			if k == -n {
				return i
			}
		}
	}
	return min(0, from) - (-n - k)
}

// definite resolves a non-span, non-auto placement value g of a start
// edge (end false) or an end edge to an explicit line index (CSS Grid 1,
// 8.3).
func (l lineSet) definite(g css.GridLine, end bool) int {
	if g.N == 0 {
		// A lone name: the area's edge line foo-start or foo-end first,
		// else the first line called foo (as 1 foo).
		suffix := "-start"
		if end {
			suffix = "-end"
		}
		if ls := l.lines(g.Name + suffix); len(ls) > 0 {
			return ls[0]
		}
		return l.nth(g.Name, 1, -1)
	}
	if g.Name == "" {
		if g.N > 0 {
			return g.N - 1
		}
		return l.last() + 1 + g.N
	}
	if g.N > 0 {
		return l.nth(g.Name, g.N, -1)
	}
	return l.nth(g.Name, g.N, l.last()+1)
}

// axisPos is an item's placement in one axis, in explicit line
// coordinates: definite, or a span to auto-place.
type axisPos struct {
	start, end int
	definite   bool
	span       int
}

// resolveAxis resolves the placement properties s and e of one axis (CSS
// Grid 1, 8.3.1): two definite lines, swapped when reversed and made one
// track apart when equal; a definite line and a span; or a span alone,
// auto-placed.
func (l lineSet) resolveAxis(s, e css.GridLine) axisPos {
	if s.Span && e.Span {
		e = css.GridLine{}
	}
	switch {
	case !s.IsAuto() && !s.Span && !e.IsAuto() && !e.Span:
		a, b := l.definite(s, false), l.definite(e, true)
		if a > b {
			a, b = b, a
		}
		if a == b {
			b = a + 1
		}
		return axisPos{start: a, end: b, definite: true}
	case !s.IsAuto() && !s.Span:
		a := l.definite(s, false)
		switch {
		case e.Span && e.Name != "":
			return axisPos{start: a, end: l.nth(e.Name, e.N, a), definite: true}
		case e.Span:
			return axisPos{start: a, end: a + e.N, definite: true}
		}
		return axisPos{start: a, end: a + 1, definite: true}
	case !e.IsAuto() && !e.Span:
		b := l.definite(e, true)
		switch {
		case s.Span && s.Name != "":
			return axisPos{start: l.nth(s.Name, -s.N, b), end: b, definite: true}
		case s.Span:
			return axisPos{start: b - s.N, end: b, definite: true}
		}
		return axisPos{start: b - 1, end: b, definite: true}
	}
	span := 1
	switch {
	case s.Span:
		span = s.N
	case e.Span:
		span = e.N
	}
	// A named span of an auto-placed item counts as a span of 1 (CSS
	// Grid 1, 8.3.1).
	if s.Span && s.Name != "" || e.Span && e.Name != "" {
		span = 1
	}
	return axisPos{span: max(min(span, MaxTracksPerAxis), 1)}
}

// clampPos keeps a definite position inside the bounds the implicit grid
// may reach.
func clampPos(p axisPos) axisPos {
	lim := MaxTracksPerAxis
	p.start = max(min(p.start, lim-1), -lim+1)
	p.end = max(min(p.end, lim), p.start+1)
	return p
}

// placeGrid resolves the explicit grid of the container b, its auto
// repeats repeated rep times per axis, and places items, its in-flow
// children (CSS Grid 1, 8.5).
func placeGrid(b *box, items []*box, rep [2]int) *gridPlacement {
	st := b.st
	p := &gridPlacement{}
	p.axes[rowAxis] = explicitAxis(st.GridTemplateRows, rep[rowAxis], st.GridTemplateAreas, rowAxis, st.GridAutoRows)
	p.axes[colAxis] = explicitAxis(st.GridTemplateColumns, rep[colAxis], st.GridTemplateAreas, colAxis, st.GridAutoColumns)
	sets := [2]lineSet{{names: p.axes[rowAxis].names}, {names: p.axes[colAxis].names}}
	type pending struct {
		it  *gitem
		pos [2]axisPos
	}
	ps := make([]pending, 0, len(items))
	// lo and hi bound the lines used, in explicit coordinates.
	lo := [2]int{0, 0}
	hi := [2]int{len(p.axes[rowAxis].sizes), len(p.axes[colAxis].sizes)}
	for _, k := range items {
		ks := k.st
		pos := [2]axisPos{
			sets[rowAxis].resolveAxis(ks.GridRowStart, ks.GridRowEnd),
			sets[colAxis].resolveAxis(ks.GridColumnStart, ks.GridColumnEnd),
		}
		for ax := range 2 {
			if pos[ax].definite {
				pos[ax] = clampPos(pos[ax])
				lo[ax] = min(lo[ax], pos[ax].start)
				hi[ax] = max(hi[ax], pos[ax].end)
			}
		}
		ps = append(ps, pending{it: &gitem{b: k}, pos: pos})
	}
	flow := rowAxis
	if st.GridAutoFlow&css.GridFlowColumn != 0 {
		flow = colAxis
	}
	cross := 1 - flow
	// The implicit grid's cross axis holds the explicit tracks, the
	// definite positions and the widest auto span (8.5, step 3).
	for _, q := range ps {
		if !q.pos[cross].definite {
			hi[cross] = max(hi[cross], lo[cross]+q.pos[cross].span)
		}
	}
	for ax := range 2 {
		p.off[ax] = -lo[ax]
		p.n[ax] = min(hi[ax]-lo[ax], MaxTracksPerAxis)
	}
	occ := &occupancy{n: p.n}
	toImplicit := func(q *pending) {
		for ax := range 2 {
			if q.pos[ax].definite {
				q.it.start[ax] = max(min(q.pos[ax].start+p.off[ax], MaxTracksPerAxis-1), 0)
				q.it.end[ax] = max(min(q.pos[ax].end+p.off[ax], MaxTracksPerAxis), q.it.start[ax]+1)
			}
		}
	}
	// Step 1: the items definite in both axes.
	for i := range ps {
		q := &ps[i]
		toImplicit(q)
		if q.pos[0].definite && q.pos[1].definite {
			occ.mark(q.it)
		}
	}
	dense := st.GridAutoFlow&css.GridFlowDense != 0
	// Step 2: the items locked to a flow-axis track (8.5, step 2), each
	// at the earliest cross position free of overlap, past the previous
	// one of the same track unless dense.
	cursors := map[int]int{}
	for i := range ps {
		q := &ps[i]
		if !q.pos[flow].definite || q.pos[cross].definite {
			continue
		}
		span := q.pos[cross].span
		at := q.it.start[flow]
		c := 0
		if !dense {
			c = cursors[at]
		}
		for !occ.free(flow, at, q.it.end[flow], c, c+span) && c < MaxTracksPerAxis {
			c++
		}
		c = min(c, MaxTracksPerAxis-span)
		q.it.start[cross], q.it.end[cross] = c, c+span
		occ.grow(cross, c+span)
		occ.mark(q.it)
		cursors[at] = c + span
	}
	// Step 4: the rest, with the auto-placement cursor.
	curF, curC := 0, 0
	for i := range ps {
		q := &ps[i]
		if q.pos[flow].definite {
			continue
		}
		spanF := q.pos[flow].span
		if q.pos[cross].definite {
			cs, ce := q.it.start[cross], q.it.end[cross]
			if dense {
				curF = 0
			} else if cs < curC {
				curF++
			}
			for !occ.free(flow, curF, curF+spanF, cs, ce) && curF < MaxTracksPerAxis {
				curF++
			}
		} else {
			spanC := min(q.pos[cross].span, occ.n[cross])
			spanC = max(spanC, 1)
			if dense {
				curF, curC = 0, 0
			}
			for curF < MaxTracksPerAxis {
				if curC+spanC > occ.n[cross] {
					curF++
					curC = 0
					continue
				}
				if occ.free(flow, curF, curF+spanF, curC, curC+spanC) {
					break
				}
				curC++
			}
			q.it.start[cross], q.it.end[cross] = curC, curC+spanC
		}
		curF = min(curF, MaxTracksPerAxis-spanF)
		q.it.start[flow], q.it.end[flow] = curF, curF+spanF
		occ.grow(flow, curF+spanF)
		occ.mark(q.it)
		// The cursor stays at the item's start: the next search moves
		// past the cells it takes.
		curC = q.it.start[cross]
	}
	p.n = occ.n
	for _, q := range ps {
		p.items = append(p.items, q.it)
	}
	return p
}

// occupancy is the set of grid cells taken by placed items: one bit per
// cell, rows[r] holding the columns of row r. An item spanning every track
// of a grid at [MaxTracksPerAxis] covers a million cells, which take
// 128 KiB.
type occupancy struct {
	n    [2]int
	rows [][]uint64
}

// grow makes the implicit grid at least n tracks in axis ax.
func (o *occupancy) grow(ax, n int) { o.n[ax] = min(max(o.n[ax], n), MaxTracksPerAxis) }

// mark takes the cells of it.
func (o *occupancy) mark(it *gitem) {
	o.grow(rowAxis, it.end[rowAxis])
	o.grow(colAxis, it.end[colAxis])
	c0, c1 := it.start[colAxis], it.end[colAxis]
	if c1 <= c0 {
		return
	}
	for r := it.start[rowAxis]; r < it.end[rowAxis]; r++ {
		for len(o.rows) <= r {
			o.rows = append(o.rows, nil)
		}
		row := o.rows[r]
		for len(row) <= (c1-1)/64 {
			row = append(row, 0)
		}
		for c := c0; c < c1; c++ {
			row[c/64] |= 1 << (c % 64)
		}
		o.rows[r] = row
	}
}

// taken reports whether the cell at row r and column c is taken.
func (o *occupancy) taken(r, c int) bool {
	if r < 0 || r >= len(o.rows) || c < 0 || c/64 >= len(o.rows[r]) {
		return false
	}
	return o.rows[r][c/64]&(1<<(c%64)) != 0
}

// free reports whether the area of tracks [f0, f1) in axis flow and
// [c0, c1) in the other is free.
func (o *occupancy) free(flow, f0, f1, c0, c1 int) bool {
	for f := f0; f < f1; f++ {
		for c := c0; c < c1; c++ {
			r, col := f, c
			if flow == colAxis {
				r, col = c, f
			}
			if o.taken(r, col) {
				return false
			}
		}
	}
	return true
}

// absLines returns the implicit-grid lines, in [0, n], that the
// placement values s and e of an absolutely positioned child's edges in
// axis ax name, and whether each names one: auto, or a line outside the
// implicit grid, is the padding edge instead (CSS Grid 1, 9.1).
func (p *gridPlacement) absLines(ax int, s, e css.GridLine) (int, int, bool, bool) {
	sDef := !s.IsAuto() && !s.Span
	eDef := !e.IsAuto() && !e.Span
	if !sDef && !eDef {
		return 0, 0, false, false
	}
	pos := lineSet{names: p.axes[ax].names}.resolveAxis(s, e)
	a, b := pos.start+p.off[ax], pos.end+p.off[ax]
	in := func(x int) bool { return x >= 0 && x <= p.n[ax] }
	aOK := in(a) && (sDef || s.Span && eDef)
	bOK := in(b) && (eDef || e.Span && sDef)
	return a, b, aOK, bOK
}

// gridOrder compares items in grid order: row-major by their area's
// start; a stable sort keeps order-modified document order among equals
// (CSS Grid 1, 5.4).
func gridOrder(a, b *gitem) int {
	if a.start[rowAxis] != b.start[rowAxis] {
		return a.start[rowAxis] - b.start[rowAxis]
	}
	return a.start[colAxis] - b.start[colAxis]
}
