package layout

import (
	"cmp"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// tableFormatter lays out a table wrapper box: its captions as blocks of
// the wrapper's width, and the table box between them (CSS 2.1, 17.4).
// When the table's first rows move to the next page, a caption above it
// stays where it is, as in Chromium: the break falls between the caption
// and the table box.
type tableFormatter struct{}

func (tableFormatter) layout(e *engine, w *box, cb cblock, bc *bfc, cursor float64, mc *marginState) float64 {
	td := w.tdata
	t := td.table
	y := cursor
	for _, k := range w.kids {
		if e.err != nil {
			return y
		}
		if k != t {
			y = e.layoutBlock(k, cb, bc, y, mc, breakNone)
			continue
		}
		top := y + mc.strut.sum()
		mc.commit(top)
		e.layoutTable(t, cb, top)
		y = t.y + t.borderH()
	}
	return y
}

// wrapperCB returns the containing block kcb the box b gives its content,
// but for a table wrapper box the height of cb, its own containing block:
// the table box's percentage height resolves against it (CSS 2.1, 17.4
// and 10.5), the wrapper's height being auto.
func wrapperCB(b *box, kcb, cb cblock) cblock {
	if b.tpart == partWrapper {
		kcb.h, kcb.hDefinite = cb.h, cb.hDefinite
	}
	return kcb
}

func (tableFormatter) intrinsic(e *engine, w *box) (float64, float64) {
	td := w.tdata
	t := td.table
	lo, hi := e.tableMinMax(td)
	edges := hEdges(t)
	if t.st.Width.Kind == css.Points {
		if wd, ok := specWidth(t, t.st.Width, 0); ok {
			lo = max(lo, wd+edges)
			hi = lo
		}
	}
	if t.st.MaxWidth.Kind == css.Points {
		if mx, ok := specWidth(t, t.st.MaxWidth, 0); ok {
			hi = max(min(hi, mx+edges), lo)
		}
	}
	if t.st.MinWidth.Kind == css.Points {
		if mn, ok := specWidth(t, t.st.MinWidth, 0); ok {
			lo, hi = max(lo, mn+edges), max(hi, mn+edges)
		}
	}
	capMin := e.captionMin(w)
	return max(lo, capMin), max(hi, lo, capMin)
}

// captionMin returns the widest min-content contribution of the captions
// of the wrapper w, which the table is at least as wide as (CSS 2.1,
// 17.5.2.2).
func (e *engine) captionMin(w *box) float64 {
	out := 0.0
	for _, k := range w.kids {
		if k.tpart == partCaption {
			lo, _ := e.contribution(k)
			out = max(out, lo)
		}
	}
	return out
}

// spacing returns the horizontal and vertical border spacing of td, 0 in
// the collapsing border model (CSS 2.1, 17.6.1).
func (td *tableData) spacing() (float64, float64) {
	if td.collapse {
		return 0, 0
	}
	sp := td.table.st.BorderSpacing
	return max(clamp(sp[0]), 0), max(clamp(sp[1]), 0)
}

// hSpacing returns the horizontal spacing the columns of td take in all:
// one spacing before, between and after the columns.
func (td *tableData) hSpacing() float64 {
	hs, _ := td.spacing()
	if td.nCols == 0 {
		return 0
	}
	n := 1
	for i := range td.nCols {
		if !td.colCollapsed(i) {
			n++
		}
	}
	return float64(n) * hs
}

// colCollapsed reports a column of td removed by visibility: collapse on
// it or its column group: it takes no width and no border spacing, as in
// Chromium (CSS 2.1, 17.5.5).
func (td *tableData) colCollapsed(i int) bool {
	if i < 0 || i >= len(td.cols) {
		return false
	}
	col := td.cols[i]
	return collapsed(col.col) || collapsed(col.group)
}

// rowCollapsed reports a row of td removed by visibility: collapse on it
// or its row group (CSS 2.1, 17.5.5).
func (td *tableData) rowCollapsed(r int) bool {
	row := td.rows[r]
	return collapsed(row.box) || collapsed(row.group)
}

// tableEdges resolves the padding and borders of the table box of td
// against cbw: in the collapsing model the table has no padding and its
// borders are half the outer collapsed ones (CSS 2.1, 17.6.2). A table box
// has no margins: its wrapper takes them.
func (e *engine) tableEdges(td *tableData, cbw float64) {
	t := td.table
	resolveEdges(t, cbw)
	t.m = [4]float64{}
	if td.collapse {
		t.pd = [4]float64{}
		t.bd = td.tableHalves()
	}
}

// cellEdges resolves the padding and borders of cell c of td against the
// table width cbw: in the collapsing model its borders are half its
// collapsed ones. Cells have no margins (CSS 2.1, 17.5).
func (td *tableData) cellEdges(c *box, cbw float64) {
	resolveEdges(c, cbw)
	c.m = [4]float64{}
	if td.collapse {
		c.bd = c.cell.half
	}
}

// fixedLayout reports whether td takes the fixed table layout algorithm
// (CSS 2.1, 17.5.2.1): table-layout fixed and a width other than auto.
func (td *tableData) fixedLayout() bool {
	st := td.table.st
	return st.TableLayout == css.TableFixed && (st.Width.Kind == css.Points || st.Width.Kind == css.Percent)
}

// colMeasure is what the automatic layout knows of one column (CSS 2.1,
// 17.5.2.2): its min- and max-content widths, the fixed width a column or
// cell sets, its percentage, and whether a cell starts in it.
type colMeasure struct {
	min, max float64
	fixed    float64
	hasFixed bool
	pct      float64
	occupied bool
}

// cellMeasure returns the outer min- and max-content widths of cell c,
// borders and padding included (CSS Tables 3, 3.9.1): a fixed width
// replaces the max-content width, never below the min-content one.
func (e *engine) cellMeasure(td *tableData, c *box) (float64, float64) {
	cd := c.cell
	if cd.measured {
		return cd.min, cd.max
	}
	cd.measured = true
	lo, hi := formatterFor(c).intrinsic(e, c)
	td.cellEdges(c, 0)
	edges := hEdges(c)
	if c.st.MinWidth.Kind == css.Points {
		if mn, ok := specWidth(c, c.st.MinWidth, 0); ok {
			lo, hi = max(lo, mn), max(hi, mn)
		}
	}
	cd.min, cd.max = lo+edges, max(hi, lo)+edges
	if w, ok := cellFixed(c); ok {
		cd.max = max(cd.min, w)
	}
	return cd.min, cd.max
}

// cellFixed returns the border box width a fixed width property of cell c
// asks for, its edges resolved.
func cellFixed(c *box) (float64, bool) {
	if c.st.Width.Kind != css.Points {
		return 0, false
	}
	w, _ := specWidth(c, c.st.Width, 0)
	return w + hEdges(c), true
}

// measureColumns returns the measures of every column of td, memoized
// (CSS 2.1, 17.5.2.2): the cells spanning one column first, then the
// spanning cells, narrowest span first, spreading what their columns lack.
func (e *engine) measureColumns(td *tableData) []colMeasure {
	if td.measured {
		return td.cm
	}
	td.measured = true
	nc := td.nCols
	cm := make([]colMeasure, nc)
	for i, col := range td.cols {
		for _, b := range []*box{col.group, col.col} {
			if b == nil {
				continue
			}
			switch b.st.Width.Kind {
			case css.Points:
				cm[i].fixed, cm[i].hasFixed = max(clamp(b.st.Width.Value), 0), true
			case css.Percent:
				cm[i].pct = max(clamp(b.st.Width.Value), 0)
			case css.Auto, css.None, css.Normal, css.Number, css.FlexContent, css.MinContent, css.MaxContent, css.FitContent, css.Stretch:
			}
			if b.st.MinWidth.Kind == css.Points {
				// A column's or column group's min-width is a floor on
				// each column it applies to, as Chromium honours it.
				mw := max(clamp(b.st.MinWidth.Value), 0)
				cm[i].min, cm[i].max = max(cm[i].min, mw), max(cm[i].max, mw)
			}
		}
	}
	var spans []*box
	for _, c := range td.cells {
		if e.err != nil {
			return cm
		}
		cd := c.cell
		lo, hi := e.cellMeasure(td, c)
		cm[cd.col].occupied = true
		if cd.colSpan > 1 {
			spans = append(spans, c)
			continue
		}
		m := &cm[cd.col]
		m.min, m.max = max(m.min, lo), max(m.max, hi)
		if w, ok := cellFixed(c); ok {
			m.fixed, m.hasFixed = max(m.fixed, w), true
		}
		if c.st.Width.Kind == css.Percent {
			m.pct = max(m.pct, clamp(c.st.Width.Value))
		}
	}
	for i := range cm {
		if cm[i].hasFixed {
			cm[i].max = max(cm[i].min, cm[i].fixed)
		}
		cm[i].max = max(cm[i].max, cm[i].min)
	}
	hs, _ := td.spacing()
	slices.SortStableFunc(spans, func(a, b *box) int { return cmp.Compare(a.cell.colSpan, b.cell.colSpan) })
	for _, c := range spans {
		cd := c.cell
		cols := cm[cd.col : cd.col+cd.colSpan]
		inner := float64(cd.colSpan-1) * hs
		lo, hi := e.cellMeasure(td, c)
		sumMin, sumMax, sumPct := 0.0, 0.0, 0.0
		for _, m := range cols {
			sumMin, sumMax, sumPct = sumMin+m.min, sumMax+m.max, sumPct+m.pct
		}
		weights := func(ok func(colMeasure) bool) []float64 {
			w := make([]float64, len(cols))
			for i, m := range cols {
				if ok(m) {
					w[i] = max(m.max, eps)
				}
			}
			return w
		}
		all := func(colMeasure) bool { return true }
		if extra := lo - sumMin - inner; extra > eps {
			grow(cols, weights(all), extra, func(m *colMeasure, d float64) { m.min += d })
		}
		if extra := hi - sumMax - inner; extra > eps {
			grow(cols, weights(all), extra, func(m *colMeasure, d float64) { m.max += d })
		}
		if c.st.Width.Kind == css.Percent {
			if rest := clamp(c.st.Width.Value) - sumPct; rest > eps {
				w := weights(func(m colMeasure) bool { return m.pct == 0 })
				grow(cols, w, rest, func(m *colMeasure, d float64) { m.pct += d })
			}
		}
		for i := range cols {
			cols[i].max = max(cols[i].max, cols[i].min)
		}
	}
	for i := range cm {
		if td.colCollapsed(i) {
			cm[i] = colMeasure{}
		}
	}
	// Percentages past 100% in all are cut, the later columns first.
	total := 0.0
	for i := range cm {
		cm[i].pct = max(min(cm[i].pct, 1-total), 0)
		total += cm[i].pct
	}
	td.cm = cm
	return cm
}

// grow spreads extra over the columns cols in proportion to the weights w,
// equally when they are all zero, through add.
func grow(cols []colMeasure, w []float64, extra float64, add func(*colMeasure, float64)) {
	sum := 0.0
	for _, v := range w {
		sum += v
	}
	if sum <= 0 {
		for i := range cols {
			add(&cols[i], extra/float64(len(cols)))
		}
		return
	}
	for i := range cols {
		add(&cols[i], extra*w[i]/sum)
	}
}

// tableMinMax returns the minimum and maximum border box widths of the
// table of td, MIN and MAX of CSS 2.1, 17.5.2.2: its columns' widths, the
// spacing and its edges. A percentage column makes MAX large enough for
// it to get its share. A fixed layout table takes the widths its columns
// and first row set.
func (e *engine) tableMinMax(td *tableData) (float64, float64) {
	e.tableEdges(td, 0)
	base := td.hSpacing() + hEdges(td.table)
	if td.fixedLayout() {
		w := e.fixedWidths(td, 0)
		sum := 0.0
		for _, v := range w {
			sum += v
		}
		return sum + base, sum + base
	}
	cm := e.measureColumns(td)
	lo, hi := 0.0, 0.0
	pctSum, nonPct, fromPct := 0.0, 0.0, 0.0
	for _, m := range cm {
		lo += m.min
		hi += m.max
		if m.pct > 0 {
			pctSum += m.pct
			fromPct = max(fromPct, m.max/m.pct)
		} else {
			nonPct += m.max
		}
	}
	if pctSum > 0 && pctSum < 1 {
		fromPct = max(fromPct, nonPct/(1-pctSum))
	}
	hi = min(max(hi, fromPct), maxLength)
	return lo + base, max(hi, lo) + base
}

// tableWidth returns the border box width of the table of td in a
// containing block of width cbw with avail points for it (CSS 2.1,
// 17.5.2): its width property, at least MIN; an auto width takes the
// available width within MIN and MAX. min-width and max-width apply, and
// the captions' min-content widths count toward MIN.
func (e *engine) tableWidth(td *tableData, cbw, avail float64) float64 {
	t := td.table
	lo, hi := e.tableMinMax(td)
	e.tableEdges(td, cbw)
	edges := hEdges(t)
	capMin := e.captionMin(td.wrapper)
	lo = max(lo, capMin)
	hi = max(hi, lo)
	used := min(max(lo, avail), hi)
	if w, ok := specWidth(t, t.st.Width, cbw); ok {
		used = w + edges
		if td.fixedLayout() {
			ws := e.fixedWidths(td, w-td.hSpacing())
			sum := 0.0
			for _, v := range ws {
				sum += v
			}
			lo = max(sum+td.hSpacing()+edges, capMin)
		}
	}
	if mx, ok := specWidth(t, t.st.MaxWidth, cbw); ok {
		used = min(used, mx+edges)
	}
	if mn, ok := specWidth(t, t.st.MinWidth, cbw); ok {
		used = max(used, mn+edges)
	}
	return max(used, lo)
}

// wrapperWidth returns the width of the table wrapper w laid out in a
// containing block of width cbw with avail points for its margin box: the
// table box's border box width.
func (e *engine) wrapperWidth(w *box, cbw, avail float64) float64 {
	room := avail - nonAuto(w.m[css.Left], w.st.Margin[css.Left].Kind == css.Auto) - nonAuto(w.m[css.Right], w.st.Margin[css.Right].Kind == css.Auto)
	return e.tableWidth(w.tdata, cbw, max(room, 0))
}

// autoWidths spreads the content width avail over the columns of the
// automatic layout (CSS Tables 3, 3.9.3): between the min-content guess,
// the guess that gives percentage columns their share, the one that gives
// fixed columns their width too, and the max-content guess; past the last
// one, auto columns with content take the rest, then fixed, percentage and
// empty columns.
func autoWidths(cm []colMeasure, avail float64) []float64 {
	n := len(cm)
	out := make([]float64, n)
	if n == 0 {
		return out
	}
	var guess [4][]float64
	var total [4]float64
	for k := range guess {
		guess[k] = make([]float64, n)
	}
	for i, m := range cm {
		pctW := max(m.min, m.pct*avail)
		g := [4]float64{m.min, m.min, m.min, m.max}
		if m.pct > 0 {
			g[1], g[2], g[3] = pctW, pctW, pctW
		} else if m.hasFixed {
			g[2] = m.max
		}
		for k := range guess {
			guess[k][i] = g[k]
			total[k] += g[k]
		}
	}
	if avail <= total[0] {
		copy(out, guess[0])
		return out
	}
	for k := 1; k < 4; k++ {
		if avail <= total[k] {
			f := 0.0
			if d := total[k] - total[k-1]; d > 0 {
				f = (avail - total[k-1]) / d
			}
			for i := range out {
				out[i] = guess[k-1][i] + f*(guess[k][i]-guess[k-1][i])
			}
			return out
		}
	}
	copy(out, guess[3])
	extra := avail - total[3]
	kinds := []func(colMeasure) (bool, float64){
		func(m colMeasure) (bool, float64) { return m.pct == 0 && !m.hasFixed && m.occupied && m.max > 0, m.max },
		func(m colMeasure) (bool, float64) { return m.pct == 0 && !m.hasFixed && m.occupied, 1 },
		func(m colMeasure) (bool, float64) { return m.pct == 0 && m.hasFixed, max(m.max, eps) },
		func(m colMeasure) (bool, float64) { return m.pct > 0, m.pct },
		func(m colMeasure) (bool, float64) { return m.pct == 0 && !m.hasFixed, 1 },
		func(colMeasure) (bool, float64) { return true, 1 },
	}
	for _, kind := range kinds {
		sum := 0.0
		w := make([]float64, n)
		for i, m := range cm {
			if ok, v := kind(m); ok {
				w[i] = v
				sum += v
			}
		}
		if sum <= 0 {
			continue
		}
		for i := range out {
			out[i] += extra * w[i] / sum
		}
		return out
	}
	return out
}

// fixedWidths returns the column widths of the fixed layout of td with
// content columns' width avail, the spacing taken off (CSS 2.1, 17.5.2.1):
// a column's width, else
// the width of the first row's cell in it divided over the columns it
// spans; the other columns share what is left equally, and when every
// column sets its width the excess spreads in proportion.
func (e *engine) fixedWidths(td *tableData, avail float64) []float64 {
	nc := td.nCols
	w := make([]float64, nc)
	set := make([]bool, nc)
	for i, col := range td.cols {
		for _, b := range []*box{col.group, col.col} {
			if b == nil {
				continue
			}
			switch b.st.Width.Kind {
			case css.Points:
				w[i], set[i] = max(clamp(b.st.Width.Value), 0), true
			case css.Percent:
				w[i], set[i] = max(resolve(b.st.Width, avail), 0), true
			case css.Auto, css.None, css.Normal, css.Number, css.FlexContent, css.MinContent, css.MaxContent, css.FitContent, css.Stretch:
			}
		}
	}
	for _, c := range td.cells {
		cd := c.cell
		if cd.row != 0 || c.st.Width.Kind == css.Auto {
			continue
		}
		td.cellEdges(c, avail)
		cw, ok := specWidth(c, c.st.Width, avail)
		if !ok {
			continue
		}
		cw += hEdges(c)
		hs, _ := td.spacing()
		share := max(cw-float64(cd.colSpan-1)*hs, 0) / float64(cd.colSpan)
		for x := cd.col; x < cd.col+cd.colSpan; x++ {
			if !set[x] {
				w[x], set[x] = share, true
			}
		}
	}
	sum, unset := 0.0, 0
	for i := range w {
		if set[i] {
			sum += w[i]
		} else {
			unset++
		}
	}
	rest := avail - sum
	switch {
	case unset > 0:
		for i := range w {
			if !set[i] {
				w[i] = max(rest, 0) / float64(unset)
			}
		}
	case rest > eps && sum > 0:
		for i := range w {
			w[i] += rest * w[i] / sum
		}
	case rest > eps && nc > 0:
		for i := range w {
			w[i] += rest / float64(nc)
		}
	}
	return w
}

// collapsed reports a row or column box with visibility: collapse, whose
// row or column is removed from the table (CSS 2.1, 17.5.5).
func collapsed(b *box) bool {
	return b != nil && b.st.Visibility == css.VisibilityCollapse
}

// columnWidths returns the used width of every column of td for a table
// content box of width w.
func (e *engine) columnWidths(td *tableData, w float64) []float64 {
	avail := max(w-td.hSpacing(), 0)
	var ws []float64
	if td.fixedLayout() {
		ws = e.fixedWidths(td, avail)
	} else {
		ws = autoWidths(e.measureColumns(td), avail)
	}
	for i := range td.cols {
		if td.colCollapsed(i) {
			ws[i] = 0
		}
	}
	return ws
}

// spanW returns the border box width of a cell spanning n columns from
// column c.
func (td *tableData) spanW(c, n int) float64 {
	hs, _ := td.spacing()
	return td.colX[c+n] - td.colX[c] - hs
}

// vaKind returns how a cell aligns vertically (CSS 2.1, 17.5.3): top,
// middle, bottom, or baseline for every other value.
func vaKind(st *css.Style) css.AlignKind {
	switch k := st.VerticalAlign.Kind; k {
	case css.AlignTop, css.Middle, css.AlignBottom:
		return k
	case css.Baseline, css.Sub, css.Super, css.TextTop, css.TextBottom, css.AlignLength:
	}
	return css.Baseline
}

// layoutTable lays out the table box t in the containing block cb, the
// wrapper's content box, its border top at y: column widths (CSS 2.1,
// 17.5.2), row heights and cell alignment (17.5.3), then the rows down the
// pages, header and footer repeated where the table breaks.
func (e *engine) layoutTable(t *box, cb cblock, y float64) {
	td := t.tdata
	t.laidOut = true
	e.tableEdges(td, cb.w)
	t.x, t.y = cb.x, y
	t.w = max(cb.w-hEdges(t), 0)
	hs, vs := td.spacing()
	nc, nr := td.nCols, len(td.rows)
	ws := e.columnWidths(td, t.w)
	td.colX = make([]float64, nc+1)
	td.colX[0] = hs
	for i, v := range ws {
		td.colX[i+1] = td.colX[i] + v + hs
		if td.colCollapsed(i) {
			td.colX[i+1] = td.colX[i]
		}
	}
	if td.collapse && nc > 0 {
		td.colX[0] = 0
		for i, v := range ws {
			td.colX[i+1] = td.colX[i] + v
		}
	}
	e.noPaginate++
	for _, c := range td.cells {
		if e.err != nil {
			break
		}
		e.layoutCell(td, c, t.w)
	}
	e.noPaginate--
	if e.err != nil {
		return
	}
	rowH, base, hasBase := e.rowHeights(td, cb)
	td.rowBase, td.rowHasBase = base, hasBase
	end, pushed := e.placeRows(td, rowH, t.contentY())
	if pushed && e.relayout(y) {
		t.y = e.nextPage(y)
		end, _ = e.placeRows(td, rowH, t.contentY())
	}
	rowH = td.rowH
	for r, row := range td.rows {
		rb := row.box
		rb.laidOut = true
		rb.pd, rb.bd, rb.m = [4]float64{}, [4]float64{}, [4]float64{}
		rb.x = t.contentX() + td.colX[0]
		rb.w = max(td.colX[nc]-td.colX[0]-hs, 0)
		if nc == 0 {
			rb.w = max(t.w-2*hs, 0)
		}
		rb.h = rowH[r]
	}
	for _, c := range td.cells {
		cd := c.cell
		top := td.rows[cd.row].box
		last := td.rows[cd.row+cd.rowSpan-1].box
		h := last.y + last.h - top.y
		off := 0.0
		switch vaKind(c.st) {
		case css.AlignTop:
		case css.Middle:
			off = (h - cd.natH) / 2
		case css.AlignBottom:
			off = h - cd.natH
		case css.Baseline, css.Sub, css.Super, css.TextTop, css.TextBottom, css.AlignLength:
			if hasBase[cd.row] && cd.hasBase {
				off = base[cd.row] - cd.base
			}
		}
		if cd.split {
			off = cd.off
		}
		c.shift(0, top.y+max(off, 0)-c.y)
		for _, s := range cd.struts {
			c.insertStrut(top.y+s.at, s.d, 0)
		}
		c.y = top.y
		c.h = max(h-vEdges(c), 0)
	}
	e.placeGroups(td)
	top := t.contentY()
	if nr > 0 {
		end = max(end, top)
	} else {
		end = top
		if nc > 0 {
			end += vs
		}
	}
	h := end - top
	if sh, ok := specHeight(t, t.st.Height, cb); ok && len(td.groups) > 0 {
		// A table without a row group, empty or not, takes no height
		// from its height property, as in Chromium.
		h = max(h, sh)
	}
	t.h = max(h, 0)
	td.hasBaseline = nr > 0
	if nr > 0 {
		r0 := td.rows[0].box
		td.baseline = r0.y + r0.h - t.y
		if hasBase[0] {
			td.baseline = r0.y + base[0] - t.y
		}
	}
	e.reach(t.y + t.borderH())
}

// layoutCell lays out the content of cell c of td at its column, below the
// table's top, for a table of content width cbw: its height and baseline
// are those of its content, and the table moves it into place.
func (e *engine) layoutCell(td *tableData, c *box, cbw float64) {
	cd := c.cell
	t := td.table
	c.laidOut = true
	td.cellEdges(c, cbw)
	c.x = t.contentX() + td.colX[cd.col]
	c.y = t.contentY()
	c.w = max(td.spanW(cd.col, cd.colSpan)-hEdges(c), 0)
	own := &bfc{}
	mc := &marginState{}
	kcb := cblock{x: c.contentX(), w: c.w}
	saved := e.enterClamp(c, kcb, c.contentY())
	end := formatterFor(c).layout(e, c, kcb, own, c.contentY(), mc)
	e.leaveClamp(saved)
	end += mc.strut.sum()
	mc.commit(end)
	end = max(end, own.bottom(), c.contentY())
	h := end - c.contentY()
	cd.contentEnd = end
	if sh, ok := specHeight(c, c.st.Height, cblock{}); ok {
		// A cell's height is a minimum (CSS 2.1, 17.5.3).
		h = max(h, sh)
	}
	c.h = h
	cd.natH = c.borderH()
	cd.base, cd.hasBase = 0, false
	if by, ok := firstBaseline(c); ok {
		cd.base, cd.hasBase = by-c.y, true
	}
}

// rowHeights returns the height of every row of td (CSS 2.1, 17.5.3): its
// height property, at least its cells' and the room baseline alignment
// takes, with the excess of spanning cells and of the table's height
// spread over the rows; and every row's baseline below its top. A
// percentage height on a row or on a cell resolves, as Chromium does,
// against the table's content height less the border spacing above the
// first row and below the last, when the table's height is not auto; the
// percentages past 100% in all are cut, the later rows first. The table's
// excess goes to the rows of auto height, else to those of a fixed
// height, else to all, in proportion to their heights.
func (e *engine) rowHeights(td *tableData, cb cblock) ([]float64, []float64, []bool) {
	nr := len(td.rows)
	_, vs := td.spacing()
	rowH := make([]float64, nr)
	base := make([]float64, nr)
	below := make([]float64, nr)
	hasBase := make([]bool, nr)
	pct := make([]float64, nr)
	fixed := make([]bool, nr)
	tableH, tableSet := specHeight(td.table, td.table.st.Height, cb)
	for r, row := range td.rows {
		st := row.box.st
		if h, ok := specHeight(row.box, st.Height, cblock{}); ok {
			rowH[r] = h
			fixed[r] = true
		}
		if st.Height.Kind == css.Percent {
			pct[r] = max(clamp(st.Height.Value), 0)
		}
	}
	var spans []*box
	for _, c := range td.cells {
		cd := c.cell
		if cd.rowSpan > 1 {
			spans = append(spans, c)
			continue
		}
		switch c.st.Height.Kind {
		case css.Points:
			fixed[cd.row] = true
		case css.Percent:
			pct[cd.row] = max(pct[cd.row], clamp(c.st.Height.Value))
		case css.Auto, css.None, css.Normal, css.Number, css.FlexContent, css.MinContent, css.MaxContent, css.FitContent, css.Stretch:
		}
		rowH[cd.row] = max(rowH[cd.row], cd.natH)
		if vaKind(c.st) == css.Baseline && cd.hasBase {
			// A cell's baseline may lie below its bottom (a line box
			// whose line-height is smaller than its font): the part
			// below the baseline is then negative, not zero.
			if !hasBase[cd.row] {
				below[cd.row] = cd.natH - cd.base
			}
			base[cd.row] = max(base[cd.row], cd.base)
			below[cd.row] = max(below[cd.row], cd.natH-cd.base)
			hasBase[cd.row] = true
		}
	}
	for r := range rowH {
		if hasBase[r] {
			rowH[r] = max(rowH[r], base[r]+below[r])
		}
	}
	if tableSet {
		ref := max(tableH-2*vs, 0)
		left := 1.0
		for r := range pct {
			pct[r] = min(pct[r], max(left, 0))
			left -= pct[r]
			rowH[r] = max(rowH[r], pct[r]*ref)
		}
	} else {
		clear(pct)
	}
	slices.SortStableFunc(spans, func(a, b *box) int { return cmp.Compare(a.cell.rowSpan, b.cell.rowSpan) })
	for _, c := range spans {
		cd := c.cell
		hs := rowH[cd.row : cd.row+cd.rowSpan]
		have := float64(cd.rowSpan-1) * vs
		for _, h := range hs {
			have += h
		}
		if extra := cd.natH - have; extra > eps {
			spread(hs, extra)
		}
	}
	for r := range td.rows {
		if td.rowCollapsed(r) {
			rowH[r] = 0
		}
	}
	if tableSet && nr > 0 {
		total := float64(nr+1) * vs
		for _, h := range rowH {
			total += h
		}
		if extra := tableH - total; extra > eps {
			lo, hi := td.headN, nr-td.footN
			if lo >= hi {
				lo, hi = 0, nr
			}
			td.spreadRows(rowH[lo:hi], pct[lo:hi], fixed[lo:hi], lo, extra)
		}
	}
	return rowH, base, hasBase
}

// spreadRows spreads the excess extra of a table's height over the rows
// hs, the rows of td from first: the rows of auto height, else those of
// a fixed height, else all of them, collapsed rows aside, in proportion
// to their heights.
func (td *tableData) spreadRows(hs, pct []float64, fixed []bool, first int, extra float64) {
	for _, pick := range []func(int) bool{
		func(i int) bool { return pct[i] == 0 && !fixed[i] },
		func(i int) bool { return pct[i] == 0 },
		func(int) bool { return true },
	} {
		var idx []int
		for i := range hs {
			if pick(i) && !td.rowCollapsed(first+i) {
				idx = append(idx, i)
			}
		}
		if len(idx) == 0 {
			continue
		}
		sub := make([]float64, len(idx))
		for k, i := range idx {
			sub[k] = hs[i]
		}
		spread(sub, extra)
		for k, i := range idx {
			hs[i] = sub[k]
		}
		return
	}
}

// spread adds extra to the heights hs in proportion to them, equally when
// they are all zero.
func spread(hs []float64, extra float64) {
	sum := 0.0
	for _, h := range hs {
		sum += h
	}
	for i := range hs {
		if sum > 0 {
			hs[i] += extra * hs[i] / sum
		} else {
			hs[i] += extra / float64(len(hs))
		}
	}
}

// rowUnit is a run of body rows placed on one page: rows a row-spanning
// cell joins, and rows the break properties keep together.
type rowUnit struct {
	lo, hi int
	h      float64
	// forced reports a forced page break before the unit.
	forced bool
}

// breakBefore returns the break-before value in force before row r: its
// own, or its row group's at the group's first row.
func (td *tableData) breakBefore(r int) (forced, avoid bool) {
	vals := []css.Break{td.rows[r].box.st.BreakBefore}
	if td.firstRow(r) {
		vals = append(vals, td.rows[r].group.st.BreakBefore)
	}
	if r > 0 {
		vals = append(vals, td.rows[r-1].box.st.BreakAfter)
		if td.lastRow(r - 1) {
			vals = append(vals, td.rows[r-1].group.st.BreakAfter)
		}
	}
	for _, v := range vals {
		forced = forced || isForced(v)
		avoid = avoid || v == css.BreakAvoid || v == css.BreakAvoidPage
	}
	return forced, avoid
}

// halfAbove and halfBelow return half the widest collapsed border on the
// edge above and below row r, 0 in the separated model.
func (td *tableData) halfAbove(r int) float64 {
	if !td.collapse || td.nCols == 0 {
		return 0
	}
	h := 0.0
	for c := range td.nCols {
		h = max(h, td.hl[r*td.nCols+c].w/2)
	}
	return h
}

func (td *tableData) halfBelow(r int) float64 { return td.halfAbove(r + 1) }

// placeRows sets the top of every row of td, from start, the content top
// of the table box, and returns the bottom of the table's content. A row
// that does not fit what is left of a page breaks inside its cells at a
// line boundary (CSS Fragmentation 3, 4.4, as Chromium and WeasyPrint
// break table rows): each cell's content after its break continues at the
// top of the row's piece on the next page, under a copy of the header
// group, and the page it leaves ends with a copy of the footer group. A
// row that avoids breaks inside it, or whose content has no line that
// fits, moves to the next page whole instead, as do the runs of rows
// rowUnits keeps together. A row with nothing to break at, taller than a
// page area, lands at a page top and overflows onto the pages below,
// sliced at each cut, the rows after it following its full height. pushed
// reports that the first rows would leave the header alone on the
// table's first page: the caller moves the table to the next page. The
// heights as placed, a split row's gaps included, land in td.rowH.
func (e *engine) placeRows(td *tableData, rowH []float64, start float64) (float64, bool) {
	nr := len(td.rows)
	_, vs := td.spacing()
	gap := vs
	td.seqs = nil
	for _, c := range td.cells {
		c.cell.split, c.cell.struts = false, nil
	}
	rowH = slices.Clone(rowH)
	td.rowH = rowH
	var seq []placedRow
	y := start + vs
	if td.collapse {
		y = start
	}
	if nr == 0 {
		return start, false
	}
	headN, footN := td.headN, td.footN
	paginate := e.paginating()
	sumH := func(lo, hi int) float64 {
		s := 0.0
		for r := lo; r < hi; r++ {
			s += rowH[r]
		}
		if hi > lo {
			s += float64(hi-lo-1) * gap
		}
		return s
	}
	if td.collapse {
		gap = 0
	}
	headH, footH := sumH(0, headN), sumH(nr-footN, nr)
	lastTop := y
	place := func(lo, hi int) {
		for r := lo; r < hi; r++ {
			if td.rowCollapsed(r) {
				// A collapsed row takes no border spacing either: it
				// sits on the bottom of the row above (CSS 2.1, 17.5.5).
				td.rows[r].box.y = y
				if r > 0 {
					td.rows[r].box.y = y - gap
				}
				seq = append(seq, placedRow{r: r})
				continue
			}
			lastTop = y
			td.rows[r].box.y = y
			seq = append(seq, placedRow{r: r})
			y += rowH[r] + gap
		}
		e.reach(y)
	}
	units := e.rowUnits(td, rowH, gap, max(e.pageH-headH-footH-2*vs, 0))
	repeat := paginate && len(units) > 0
	repeatHead := repeat && headN > 0
	repeatFoot := repeat && footN > 0
	footNeed := func(u rowUnit) float64 {
		if td.collapse {
			if footN > 0 {
				return footH + td.halfBelow(nr-1)
			}
			return td.halfBelow(u.hi - 1)
		}
		if footN > 0 {
			return footH + vs
		}
		return 0
	}
	contStart := func(u rowUnit, head bool) float64 {
		switch {
		case head && td.collapse:
			return td.halfAbove(0) + headH
		case head:
			return vs + headH + vs
		case td.collapse:
			return td.halfAbove(u.lo)
		}
		return vs
	}
	if headH+footH > e.pageH/2 {
		// Copies that would fill half the page area leave too little room
		// for the rows they repeat over.
		repeatHead, repeatFoot = false, false
	}
	type footCopy struct {
		seq, at int
		top     float64
	}
	var foots []footCopy
	place(0, headN)
	headTop := td.rows[0].box.y
	page := e.pageOf(y)
	onPage := 0
	// endPage ends the current page: a copy of the footer group at top,
	// then the run of rows, and the next page starts with a copy of the
	// header group when head.
	endPage := func(top float64, foot, head bool) {
		if foot {
			foots = append(foots, footCopy{seq: len(td.seqs), at: len(seq), top: top})
			for r := nr - footN; r < nr; r++ {
				seq = append(seq, placedRow{r: r})
			}
		}
		td.seqs = append(td.seqs, seq)
		seq = nil
		page++
		if head {
			p := e.pageTop(page)
			copyTop := p + vs
			if td.collapse {
				copyTop = p + td.halfAbove(0)
			}
			for r := range headN {
				seq = append(seq, placedRow{r: r, dy: copyTop - headTop})
			}
		}
	}
	// split places the one-row unit u from y, broken across pages at line
	// boundaries inside its cells. Unless force, it places nothing and
	// reports false when no cell keeps a line on the current page.
	split := func(u rowUnit, force bool) bool {
		r := u.lo
		rowTop := y
		cells := td.splitCells(r)
		bottom := func() float64 {
			b := rowH[r]
			for _, sc := range cells {
				b = max(b, sc.bottom)
			}
			return b
		}
		pieceTop, cutTop, first := rowTop, false, true
		for e.err == nil {
			pb := e.pageTop(page + 1)
			if rowTop+bottom()+footNeed(u) <= pb+eps {
				break
			}
			end := pb
			if repeatFoot {
				end -= footNeed(u)
			}
			if !chooseCuts(cells, pieceTop-rowTop, end-rowTop, first && !force) {
				if first && !force {
					return false
				}
				// Nothing fits: the content overflows this page.
				page++
				e.reach(e.pageTop(page))
				continue
			}
			at := e.pageTop(page + 1)
			if repeatHead {
				at += contStart(u, true)
			}
			inner := at
			if repeatHead && td.collapse {
				inner += td.edgeHalf(headN - 1)
			}
			for _, sc := range cells {
				if sc.has {
					sc.open(inner - rowTop)
				}
			}
			seq = append(seq, placedRow{r: r, piece: true, y0: pieceTop, y1: end, cutTop: cutTop, cutBottom: true, first: first})
			endPage(end+gap, repeatFoot, repeatHead)
			pieceTop, cutTop, first = at, true, false
			e.reach(at)
		}
		h := bottom()
		rowH[r] = h
		td.rows[r].box.y = rowTop
		pr := placedRow{r: r}
		if !first {
			pr = placedRow{r: r, piece: true, y0: pieceTop, y1: rowTop + h, cutTop: true}
			for _, sc := range cells {
				sc.c.cell.split = true
			}
		}
		seq = append(seq, pr)
		lastTop = max(pieceTop, rowTop+h-metricSlack)
		y = rowTop + h + gap
		e.reach(y)
		onPage = 1
		return true
	}
	for k, u := range units {
		if e.err != nil {
			return y, false
		}
		if paginate {
			page = max(page, e.pageOf(lastTop))
			need := u.h + footNeed(u)
			pb := e.pageTop(page + 1)
			canBreak := onPage > 0 || k == 0 && (headN > 0 || !e.atPageTop(td.table.y))
			single := u.hi-u.lo == 1
			fits := y+need <= pb+eps
			if !fits && single && (!u.forced || k == 0) && canBreak && !td.avoidsSplit(u.lo) && split(u, false) {
				continue
			}
			brk := u.forced && k > 0 || canBreak && !fits
			if g := td.rows[u.lo].group; canBreak && !brk && td.firstRow(u.lo) && avoidsInside(g) {
				// A row group that avoids breaks inside it and does not
				// fit what is left of the page starts the next one, even
				// when it is taller than a page and breaks there anyway
				// (CSS Fragmentation 3, 4.4). A forced break inside it ends
				// what has to fit.
				end := u.lo
				for end+1 < nr-footN && td.rows[end+1].group == g {
					if forced, _ := td.breakBefore(end + 1); forced {
						break
					}
					end++
				}
				brk = y+sumH(u.lo, end+1)+footNeed(u) > pb+eps
			}
			if brk && k == 0 && !e.atPageTop(td.table.y) && y-td.table.y+need <= e.pageH+eps {
				return y, true
			}
			if brk {
				endPage(y, repeatFoot && onPage > 0, false)
				p := e.pageTop(page)
				if repeatHead && contStart(u, true)+need <= e.pageH+eps {
					copyTop := p + vs
					if td.collapse {
						copyTop = p + td.halfAbove(0)
					}
					for r := range headN {
						seq = append(seq, placedRow{r: r, dy: copyTop - headTop})
					}
					y = p + contStart(u, true)
				} else {
					y = p + contStart(u, false)
				}
				onPage = 0
			}
			if single && y+u.h+footNeed(u) > e.pageTop(page+1)+eps {
				split(u, true)
				continue
			}
		}
		place(u.lo, u.hi)
		if paginate {
			// A run taller than the page overflows onto the pages below.
			lastTop = max(lastTop, y-gap-metricSlack)
		}
		onPage += u.hi - u.lo
	}
	if footN > 0 {
		place(nr-footN, nr)
		for _, f := range foots {
			dy := f.top - td.rows[nr-footN].box.y
			s := td.seqs[f.seq]
			for i := f.at; i < len(s); i++ {
				s[i].dy = dy
			}
		}
	}
	td.seqs = append(td.seqs, seq)
	last := td.rows[nr-1].box
	end := last.y + rowH[nr-1] + vs
	if td.collapse {
		end = last.y + rowH[nr-1]
	}
	return end, false
}

// avoidsSplit reports a row of td that avoids breaks inside it (CSS
// Fragmentation 3, 3.2). A row group or a table that avoids them moves
// whole when it fits a page, rowUnits and the block flow see to that;
// one taller than a page breaks inside anyway, its rows too.
func (td *tableData) avoidsSplit(r int) bool {
	return avoidsInside(td.rows[r].box)
}

// edgeHalf returns half the widest collapsed border below row above of td
// where a copy of the header group meets the piece of a row split across
// pages, whose cut top adds no border.
func (td *tableData) edgeHalf(above int) float64 {
	h := 0.0
	for c := range td.nCols {
		h = max(h, td.hEdge(above, noRow, c).w/2)
	}
	return h
}

// rowUnits groups the body rows of td into the units placement keeps
// whole: rows joined by a row-spanning cell, by break-before or
// break-after avoid between them, or by break-inside avoid on their row
// group, as long as the unit stays within room (CSS Fragmentation 3, 3.1
// and 3.2).
func (e *engine) rowUnits(td *tableData, rowH []float64, gap, room float64) []rowUnit {
	lo, hi := td.headN, len(td.rows)-td.footN
	reach := make([]int, len(td.rows))
	for r := range reach {
		reach[r] = r
	}
	for _, c := range td.cells {
		cd := c.cell
		reach[cd.row] = max(reach[cd.row], cd.row+cd.rowSpan-1)
	}
	height := func(a, b int) float64 {
		s := float64(b-a-1) * gap
		for r := a; r < b; r++ {
			s += rowH[r]
		}
		return s
	}
	var units []rowUnit
	for r := lo; r < hi; {
		end := r
		for x := r; x <= end && x < hi; x++ {
			end = max(end, reach[x])
		}
		end = min(end, hi-1)
		forced, _ := td.breakBefore(r)
		units = append(units, rowUnit{lo: r, hi: end + 1, h: height(r, end+1), forced: forced})
		r = end + 1
	}
	if !e.paginating() {
		return units
	}
	var out []rowUnit
	for _, u := range units {
		if n := len(out); n > 0 {
			prev := &out[n-1]
			_, avoid := td.breakBefore(u.lo)
			g := td.rows[u.lo].group
			inside := g == td.rows[prev.hi-1].group && avoidsInside(g)
			if !u.forced && (avoid || inside) && height(prev.lo, u.hi) <= room {
				prev.hi = u.hi
				prev.h = height(prev.lo, u.hi)
				continue
			}
		}
		out = append(out, u)
	}
	return out
}

// placeGroups sets the boxes of the row groups and the columns of td once
// the rows are placed: a row group spans its rows, a column or column
// group its columns from the first row to the last (CSS 2.1, 17.5.1).
func (e *engine) placeGroups(td *tableData) {
	t := td.table
	nr, nc := len(td.rows), td.nCols
	hs, _ := td.spacing()
	for _, g := range td.groups {
		g.pd, g.bd, g.m = [4]float64{}, [4]float64{}, [4]float64{}
		g.laidOut = true
		g.x, g.w = t.contentX()+td.colX[0], 0
		if nc > 0 {
			g.w = max(td.colX[nc]-td.colX[0]-hs, 0)
		}
		g.y, g.h = t.contentY(), 0
		first, last := -1, -1
		for r, row := range td.rows {
			if row.group == g {
				if first < 0 {
					first = r
				}
				last = r
			}
		}
		if first >= 0 {
			a, b := td.rows[first].box, td.rows[last].box
			g.y, g.h = a.y, max(b.y+b.h-a.y, 0)
		}
	}
	if nr == 0 || nc == 0 {
		return
	}
	top := td.rows[0].box.y
	bottom := td.rows[nr-1].box.y + td.rows[nr-1].box.h
	span := func(b *box, a, z int) {
		b.pd, b.bd, b.m = [4]float64{}, [4]float64{}, [4]float64{}
		b.laidOut = true
		b.x = t.contentX() + td.colX[a]
		b.w = max(td.colX[z+1]-td.colX[a]-hs, 0)
		b.y, b.h = top, max(bottom-top, 0)
	}
	for _, cb := range td.colBoxes {
		a, z := -1, -1
		for i, col := range td.cols {
			if col.col == cb || col.group == cb || col.col != nil && col.col.parent == cb {
				if a < 0 {
					a = i
				}
				z = i
			}
		}
		if a >= 0 {
			span(cb, a, z)
		}
		for _, k := range cb.kids {
			for i, col := range td.cols {
				if col.col == k {
					span(k, i, i)
					break
				}
			}
		}
	}
}
