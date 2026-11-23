package layout

import (
	"slices"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// This file lays out subgrids (CSS Grid 2, 9). A grid item that is a grid
// container and says subgrid for its rows or columns adopts, in that axis,
// the tracks its grid area spans in its parent grid: it has no implicit
// tracks there, its lines carry the parent's names beside its own, and
// its items take part in sizing the parent's tracks, each with the
// subgrid's margin, border and padding as an extra margin when it sits at
// the subgrid's edge, and half the difference of the two grids' gaps when
// it sits at an inner line. The subgrid itself contributes nothing to the
// tracks of a subgridded axis and is stretched over its area there. The
// parent places and sizes; the subgrid then lays its items out on the
// lines the parent gives it (gridData.sub), so the box tree stays as it
// is.

// subAxis is what a parent grid gives one subgridded axis of a subgrid.
type subAxis struct {
	// n is the number of tracks the subgrid's area spans; names the
	// parent's names of its n+1 lines.
	n     int
	names [][]string
	// gap is the subgrid's used gap in the axis.
	gap float64
	// starts and ends are the subgrid's tracks from its content box's
	// start edge, set by the parent before it lays the subgrid out; nil
	// until the parent has sized the axis, the subgrid then sizing n
	// auto tracks of its own for a layout off the page cuts.
	starts, ends []float64
}

// gsub marks a grid item hoisted out of a subgrid into an ancestor grid,
// or an empty item holding a subgrid's edge (b nil).
type gsub struct {
	// owner is the subgrid whose item it is, which records its
	// measurement.
	owner *box
	// on reports the axes in which the item takes part in the grid's
	// track sizing.
	on [2]bool
	// extra holds the extra margins at the start and end of each axis.
	extra [2][2]float64
	// aw is the width of the item's column area, from its owner's
	// layout.
	aw float64
}

// extra returns the sum of the extra margins of a hoisted item in axis ax,
// 0 for an item of the grid itself.
func (it *gitem) extra(ax int) float64 {
	if it.sub == nil {
		return 0
	}
	return float64(it.sub.extra[ax][0] + it.sub.extra[ax][1])
}

// itemRecord is the measurement of a grid item in its own grid's last
// layout: its content height and baselines, and the width of its column
// area.
type itemRecord struct {
	ms itemMeasure
	aw float64
}

// subgridAxes reports the axes in which the box k is a subgrid (CSS Grid
// 2, 9): it is a grid item, a grid container, and says subgrid for them.
// Layout and paint containment make an independent formatting context,
// which is never a subgrid (9.1).
func subgridAxes(k *box) [2]bool {
	if k == nil || !k.gridItem || k.grid == nil || k.st.Contain&(css.ContainLayout|css.ContainPaint) != 0 {
		return [2]bool{}
	}
	return [2]bool{k.st.GridTemplateRows.Subgrid, k.st.GridTemplateColumns.Subgrid}
}

// subGap returns the used gap of the subgrid k in axis ax in a parent
// whose gap is pg: normal takes the parent's (CSS Grid 2, 9).
func subGap(k *box, ax int, pg float64) float64 {
	d := k.st.ColumnGap
	if ax == rowAxis {
		d = k.st.RowGap
	}
	if d.Kind == css.Normal {
		return pg
	}
	return gapOf(d, 0, false)
}

// subEdges returns the margin, border and padding of the subgrid k at the
// start and end of axis ax, percentages against cbw, in a parent laid out
// right to left when rtl; an auto margin counts as 0.
func subEdges(k *box, ax int, cbw float64, rtl bool) (float64, float64) {
	pd, bd, m := edgesOf(k, cbw)
	st := k.st
	side := func(s css.Side) float64 {
		out := pd[s] + bd[s]
		if st.Margin[s].Kind != css.Auto {
			out += m[s]
		}
		return out
	}
	if ax == rowAxis {
		return side(css.Top), side(css.Bottom)
	}
	if rtl {
		return side(css.Right), side(css.Left)
	}
	return side(css.Left), side(css.Right)
}

// hoist returns the items of the subgrids among the placed items of p, the
// grid of b, as items of b's grid (CSS Grid 2, 9): each in the tracks of b
// its subgrid's area gives it on the axes the subgrid subgrids, with the
// extra margins of every subgrid it comes through, and an empty item at
// each edge of a subgrid holding the subgrid's margin, border and padding
// there. gap is b's used gaps. It also gives each subgrid its span and
// the parent's line names.
func (e *engine) hoist(b *box, p *gridPlacement, gap [2]float64) []*gitem {
	var out []*gitem
	rtl := b.st.Direction == css.RTL
	for _, it := range p.items {
		k := it.b
		axes := subgridAxes(k)
		if !axes[rowAxis] && !axes[colAxis] {
			continue
		}
		g := k.grid
		changed := false
		var kg [2]float64
		for ax := range 2 {
			if !axes[ax] {
				changed = changed || g.sub[ax] != nil
				g.sub[ax] = nil
				continue
			}
			n := it.span(ax)
			names := make([][]string, n+1)
			for i := range names {
				j := it.start[ax] + i - p.off[ax]
				if j >= 0 && j < len(p.axes[ax].names) {
					names[i] = p.axes[ax].names[j]
				}
			}
			kg[ax] = subGap(k, ax, gap[ax])
			if s := g.sub[ax]; s == nil || s.n != n || !slices.EqualFunc(s.names, names, slices.Equal) {
				g.sub[ax] = &subAxis{n: n, names: names}
				changed = true
			}
			g.sub[ax].gap = kg[ax]
		}
		kp := g.place
		if changed || kp == nil {
			g.place = nil
			kp = g.placement(e, k, gridKids(k), introRep(k))
		}
		// The subgrid's own items, and those it hoists from its own
		// subgrids, on the axes it subgrids.
		var items []*gitem
		for _, kk := range kp.items {
			ka := subgridAxes(kk.b)
			on := [2]bool{axes[rowAxis] && !ka[rowAxis], axes[colAxis] && !ka[colAxis]}
			if on[rowAxis] || on[colAxis] {
				items = append(items, &gitem{b: kk.b, start: kk.start, end: kk.end, sub: &gsub{owner: k, on: on}})
			}
		}
		for _, h := range e.hoist(k, kp, kg) {
			on := [2]bool{h.sub.on[rowAxis] && axes[rowAxis], h.sub.on[colAxis] && axes[colAxis]}
			if on[rowAxis] || on[colAxis] {
				c := *h
				s := *h.sub
				s.on = on
				c.sub = &s
				items = append(items, &c)
			}
		}
		for _, h := range items {
			for ax := range 2 {
				if !h.sub.on[ax] {
					continue
				}
				lo, hi := subEdges(k, ax, 0, rtl)
				half := float64(float64(kg[ax]-gap[ax]) / 2)
				if h.start[ax] <= 0 {
					h.sub.extra[ax][0] += lo
				} else {
					h.sub.extra[ax][0] += half
				}
				if h.end[ax] >= it.span(ax) {
					h.sub.extra[ax][1] += hi
				} else {
					h.sub.extra[ax][1] += half
				}
				h.start[ax] += it.start[ax]
				h.end[ax] += it.start[ax]
			}
		}
		// An edge track holds the subgrid's margin, border and padding
		// even where no item reaches it.
		for ax := range 2 {
			if !axes[ax] {
				continue
			}
			lo, hi := subEdges(k, ax, 0, rtl)
			first := &gitem{start: it.start, end: it.end, sub: &gsub{owner: k}}
			first.sub.on[ax] = true
			first.end[ax] = it.start[ax] + 1
			first.sub.extra[ax][0] = lo
			last := &gitem{start: it.start, end: it.end, sub: &gsub{owner: k}}
			last.sub.on[ax] = true
			last.start[ax] = it.end[ax] - 1
			last.sub.extra[ax][1] = hi
			items = append(items, first, last)
		}
		out = append(out, items...)
	}
	return out
}

// introRep returns the auto repeat counts of the grid container b laid
// out at an indefinite size, as its intrinsic sizes take them.
func introRep(b *box) [2]int {
	st := b.st
	mn, mx := 0.0, inf
	if k := st.MinWidth.Kind; k == css.Points || k == css.Calc {
		mn, _ = specWidth(b, st.MinWidth, 0)
	}
	if st.MaxWidth.Kind == css.Points {
		mx, _ = specWidth(b, st.MaxWidth, 0)
	}
	minH, maxH := heightBounds(b)
	return [2]int{
		repeatCount(st.GridTemplateRows, 0, false, minH, maxH, gapOf(st.RowGap, 0, false)),
		repeatCount(st.GridTemplateColumns, 0, false, mn, mx, gapOf(st.ColumnGap, 0, false)),
	}
}

// axisItems returns the items that size the tracks of axis ax: the grid's
// own, but a subgrid of that axis, and the hoisted ones that take part in
// it.
func axisItems(own, hoisted []*gitem, ax int) []*gitem {
	if len(hoisted) == 0 && !slices.ContainsFunc(own, func(it *gitem) bool { return subgridAxes(it.b)[ax] }) {
		return own
	}
	out := make([]*gitem, 0, len(own)+len(hoisted))
	for _, it := range own {
		if !subgridAxes(it.b)[ax] {
			out = append(out, it)
		}
	}
	for _, h := range hoisted {
		if h.sub.on[ax] {
			out = append(out, h)
		}
	}
	return out
}

// inherited returns the tracks and the line positions the parent grid
// gave the subgridded axis s, false when it has not sized it yet.
func (s *subAxis) inherited() ([]*gtrack, []float64, bool) {
	if s == nil || s.starts == nil {
		return nil, nil, false
	}
	tracks := make([]*gtrack, s.n)
	lines := make([]float64, s.n+1)
	for i := range tracks {
		tracks[i] = &gtrack{base: max(s.ends[i]-s.starts[i], 0), start: s.starts[i]}
		lines[i] = s.starts[i]
	}
	if s.n > 0 {
		lines[s.n] = s.ends[s.n-1]
	}
	return tracks, lines, true
}

// inherit gives each subgrid among the items the tracks of axis ax its
// area spans, from its content box's start edge: an edge track loses the
// subgrid's margin, border and padding, an inner line takes the
// subgrid's gap centred in the parent's gutter (CSS Grid 2, 9). A subgrid
// whose tracks change is laid out again.
func (l *gridLayout) inherit(ax int) {
	tracks := l.tracks[ax]
	for _, it := range l.p.items {
		k := it.b
		if !subgridAxes(k)[ax] {
			continue
		}
		s := k.grid.sub[ax]
		n := it.span(ax)
		if s == nil || s.n != n || it.end[ax] > len(tracks) {
			continue
		}
		pStart := func(j int) float64 { return tracks[j].start }
		pEnd := func(j int) float64 { return tracks[j].start + tracks[j].base }
		lo, hi := subEdges(k, ax, l.colArea(it), ax == colAxis && l.rtl)
		x0 := pStart(it.start[ax]) + lo
		starts := make([]float64, n)
		ends := make([]float64, n)
		for i := range n {
			j := it.start[ax] + i
			if i > 0 {
				mid := float64(float64(pEnd(j-1)+pStart(j)) / 2)
				starts[i] = mid + float64(s.gap/2) - x0
			}
			if i < n-1 {
				mid := float64(float64(pEnd(j)+pStart(j+1)) / 2)
				ends[i] = mid - float64(s.gap/2) - x0
			} else {
				ends[i] = pEnd(j) - hi - x0
			}
			ends[i] = max(ends[i], starts[i])
		}
		if !slices.Equal(starts, s.starts) || !slices.Equal(ends, s.ends) {
			s.starts, s.ends = starts, ends
			l.b.grid.fd.forget(k)
		}
	}
}

// provisional drops the tracks the parent gave the subgrids of axis ax
// among the items, for a layout off the page cuts before the parent sizes
// that axis.
func (l *gridLayout) provisional(ax int) {
	for _, it := range l.p.items {
		k := it.b
		if !subgridAxes(k)[ax] {
			continue
		}
		if s := k.grid.sub[ax]; s != nil && s.starts != nil {
			s.starts, s.ends = nil, nil
			l.b.grid.fd.forget(k)
		}
	}
}

// forget drops the measurements and the layout record of the item k, so
// its next placement lays it out again.
func (fd *flexData) forget(k *box) {
	delete(fd.current, k)
	for key := range fd.measured {
		if key.item == k {
			delete(fd.measured, key)
		}
	}
}

// record keeps the measurement of each item of the grid for a parent
// grid that sizes its rows by them, and gives the hoisted items of the
// row axis the measurements their subgrids recorded.
func (l *gridLayout) record(hoisted []*gitem) {
	g := l.b.grid
	if g.rec != nil || subgridAxes(l.b) != [2]bool{} {
		if g.rec == nil {
			g.rec = map[*box]itemRecord{}
		}
		for _, it := range l.p.items {
			g.rec[it.b] = itemRecord{ms: l.ms[it], aw: l.colArea(it)}
		}
	}
	for _, h := range hoisted {
		if h.b == nil || !h.sub.on[rowAxis] {
			continue
		}
		r := h.sub.owner.grid.rec[h.b]
		l.ms[h] = r.ms
		h.sub.aw = r.aw
	}
}
