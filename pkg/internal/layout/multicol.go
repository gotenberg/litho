package layout

import (
	"math"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// This file lays out multi-column containers (CSS Multicol 1).
//
// A multi-column container is a fragmentation context (CSS Multicol 1, 2;
// CSS Fragmentation 3, 1): its content is laid out in one tall column of
// the column width, in coordinates of its own, the column flow, whose
// first column sits where the container's content starts. The column
// flow is cut into fragmentainers, the columns, by the same break logic
// that cuts the document into pages: while the content is laid out, the
// engine's page queries ([engine.pageOf], [engine.pageTop],
// [engine.areaH]) answer for the columns of the innermost container
// ([colCtx]), so line struts, monolithic pieces, break-before, break-after
// and break-inside, orphans and widows, tables and flex containers break
// at the columns exactly as they break at pages. Painting then cuts the
// display list of the content at the columns and moves each piece to its
// column's place, as the page assembly cuts the document at the pages.
//
// The columns of a column set form rows: a row holds column-count
// columns of one height side by side, and the content past the last one
// goes to overflow columns in the inline direction (CSS Multicol 1, 6.2),
// unless the container is itself fragmented: a row cut by the end of a
// page continues on the next page with a new row (CSS Multicol 1, 8.2 and
// CSS Fragmentation 3, 4). column-height and column-wrap: wrap start a new
// row below a full one (CSS Multicol 2, 3.3 and 3.4). A spanner,
// column-span: all, ends the column set before it and starts a new one
// after it (CSS Multicol 1, 5).

// minColumnHeight is the least height of a column: a container of no
// height still has columns one CSS pixel tall, so the content advances
// through a finite number of them, as Chromium clamps it.
const minColumnHeight = css.Pt

// maxColumns bounds the columns of one column set: past it the columns
// repeat the last one's place. Anti-DoS guard, not spec-mandated: the
// column flow of a long article over a thousand pages needs a few
// thousand columns.
const maxColumns = 1 << 16

// maxBalancePasses bounds the layouts of a column set that balancing its
// last row takes. Anti-DoS guard, not spec-mandated: each pass halves the
// range of heights the balanced height lies in, so thirty narrow a page
// height down to a millionth of a point.
const maxBalancePasses = 30

// unbounded is the height of a column nothing limits: the content of an
// unfragmented container that balances is first laid out in one column
// that tall, to measure it.
const unbounded = maxLength

// mcolData is the multi-column layout of a container.
type mcolData struct {
	// n is the column count of a row, w the column width, gap the column
	// gap (CSS Multicol 1, 3.4 and 4.1).
	n      int
	w, gap float64
	// span is the content width, which an rtl container's columns run
	// across from its right edge.
	span float64
	rtl  bool
	// sets are the column sets and spanners in order.
	sets []*colSet
}

// colSet is a column set of a container, or one of its spanners.
type colSet struct {
	kids    []*box
	spanner *box
	// frags are the columns, rows the rows they sit in, their positions
	// relative to the container's border box top, so they move with it.
	frags []colFrag
	rows  []colRow
}

// colFrag is one column: the top of its slice of the column flow and its
// height, the row it sits in and its index in the row.
type colFrag struct {
	vtop, h  float64
	row, col int
}

// colRow is a row of columns: its top, its columns' height, the height
// it takes in the container, how many columns hold content, and whether a
// row may follow it.
type colRow struct {
	top, h, used float64
	cols         int
	next         bool
	first        int
}

// isMulticol reports a multi-column container: a block container, not a
// table, flex container or replaced box, with a column-count or a
// column-width other than auto (CSS Multicol 1, 2).
func isMulticol(b *box) bool {
	return b.kind == kindBlock && b.flex == nil && b.tpart == partNone && b.st.Columns.Multicol()
}

// columnGap returns the used column-gap of a container whose content box
// is w wide: normal is 1em (CSS Multicol 1, 4.1).
func (e *engine) columnGap(b *box, w float64) float64 {
	g := b.st.ColumnGap
	if g.Kind == css.Normal {
		return e.fontSize(b.st)
	}
	return max(resolve(g, w), 0)
}

// columnLayout returns the used column count and width of a container
// whose content box is u wide, with a gap of g (CSS Multicol 1, 3.4).
func columnLayout(c *css.Columns, u, g float64) (int, float64) {
	u = max(u, 0)
	if c.Width.Kind != css.Points {
		n := max(c.Count, 1)
		return n, max((u-float64(n-1)*g)/float64(n), 0)
	}
	w := max(clamp(c.Width.Value), 0)
	fit := 1
	if w+g > 0 {
		fit = int(min(max(math.Floor((u+g)/(w+g)), 1), maxColumns)) // fits int: capped
	}
	n := fit
	if c.Count > 0 {
		n = min(c.Count, fit)
	}
	return n, max((u+g)/float64(n)-g, 0)
}

// colCtx is the fragmentation context of the column set being laid out:
// it answers the engine's page queries in the column flow's coordinates.
type colCtx struct {
	e      *engine
	parent *colCtx
	// noPag is the engine's monolithic depth outside, restored while the
	// context asks the outer fragmentation context.
	noPag int
	n     int
	// limit is the flow position the container's height or max-height
	// ends at, +Inf for none; colH the column-height, 0 for auto; gap the
	// space between wrapped rows.
	limit float64
	colH  float64
	wrap  bool
	gap   float64
	// start is where the set's first row starts, in the outer flow.
	start float64
	// balanceRow and balanceH override the height of one row, the one
	// balancing tries heights for; balanceRow is -1 for none.
	balanceRow int
	balanceH   float64
	frags      []colFrag
	rows       []colRow
}

// outer runs f in the fragmentation context around c.
func (c *colCtx) outer(f func()) {
	e := c.e
	saved, savedN := e.cols, e.noPaginate
	e.cols, e.noPaginate = c.parent, c.noPag
	f()
	e.cols, e.noPaginate = saved, savedN
}

// newRow appends a row whose top is top in the outer flow and its first
// column.
func (c *colCtx) newRow(top float64) {
	r := len(c.rows)
	h := math.Inf(1)
	if c.colH > 0 {
		h = c.colH
	}
	paged := false
	c.outer(func() {
		if c.e.paginating() {
			if space := c.e.nextPage(top) - top; space < h {
				h, paged = space, true
			}
		}
	})
	if rem := c.limit - top; rem < h {
		h, paged = rem, false
	}
	next := paged || c.wrap && c.colH > 0 && top+h+c.gap < c.limit
	if r == c.balanceRow {
		h = min(h, c.balanceH)
	}
	if math.IsInf(h, 1) {
		h = unbounded
	}
	h = max(h, minColumnHeight)
	vtop := c.start
	if n := len(c.frags); n > 0 {
		vtop = c.frags[n-1].vtop + c.frags[n-1].h
	}
	c.rows = append(c.rows, colRow{top: top, h: h, next: next, first: len(c.frags)})
	c.frags = append(c.frags, colFrag{vtop: vtop, h: h, row: r})
}

// grow appends the next column, reporting false past [maxColumns].
func (c *colCtx) grow() bool {
	n := len(c.frags)
	if n == 0 {
		c.newRow(c.start)
		return true
	}
	if n >= maxColumns || c.e.err != nil {
		return false
	}
	last := c.frags[n-1]
	row := c.rows[last.row]
	if last.col+1 < c.n || !row.next {
		c.frags = append(c.frags, colFrag{vtop: last.vtop + last.h, h: last.h, row: last.row, col: last.col + 1})
		return true
	}
	top := row.top + row.h + c.gap
	if !c.wrap || c.colH <= 0 {
		c.outer(func() { top = c.e.nextPage(row.top) })
	}
	c.newRow(top)
	return true
}

// bottom returns the column flow position the last column ends at.
func (c *colCtx) bottom() float64 {
	l := c.frags[len(c.frags)-1]
	return l.vtop + l.h
}

// of returns the column the column flow position y lies in.
func (c *colCtx) of(y float64) int {
	for len(c.frags) == 0 || c.bottom() <= y+eps {
		if !c.grow() {
			break
		}
	}
	if len(c.frags) == 0 {
		return 0
	}
	if last := c.bottom(); last <= y+eps {
		// Past the cap: the columns beyond are as tall as the last.
		h := c.frags[len(c.frags)-1].h
		return len(c.frags) + int(min((y+eps-last)/h, math.MaxInt32/2))
	}
	i := sort.Search(len(c.frags), func(i int) bool { return c.frags[i].vtop > y+eps+1e-9 }) - 1
	return max(i, 0)
}

// top returns the column flow position of the top of column i.
func (c *colCtx) top(i int) float64 {
	for i >= len(c.frags) {
		if !c.grow() {
			break
		}
	}
	if len(c.frags) == 0 {
		return c.start
	}
	if i < len(c.frags) {
		return c.frags[max(i, 0)].vtop
	}
	last := c.frags[len(c.frags)-1]
	return last.vtop + last.h*float64(i-len(c.frags)+1)
}

// height returns the height of column i.
func (c *colCtx) height(i int) float64 {
	c.top(i)
	if len(c.frags) == 0 {
		return minColumnHeight
	}
	return c.frags[min(max(i, 0), len(c.frags)-1)].h
}

// reach records that the content reached column flow position y: the
// outer context reaches the same place of the column's row.
func (c *colCtx) reach(y float64) {
	i := c.of(y)
	if i >= len(c.frags) || len(c.frags) == 0 {
		return
	}
	f := c.frags[i]
	oy := c.rows[f.row].top + min(max(y-f.vtop, 0), f.h)
	c.outer(func() { c.e.reach(oy) })
}

// index returns the column y lies in without adding columns, the first
// or the last one outside them.
func (c *colCtx) index(y float64) int {
	i := sort.Search(len(c.frags), func(i int) bool { return c.frags[i].vtop > y }) - 1
	return min(max(i, 0), len(c.frags)-1)
}

// multicolFormatter lays out the content of a multi-column container.
type multicolFormatter struct{}

// intrinsic returns the min- and max-content widths of a multi-column
// container: those of a column, at least the column-width, times the
// column count with the gaps between, as Chromium sizes it (CSS Multicol
// 1, 3.4, leaves them open).
func (multicolFormatter) intrinsic(e *engine, b *box) (float64, float64) {
	lo, hi := flowFormatter{}.intrinsic(e, b)
	c := &b.st.Columns
	if c.Width.Kind == css.Points {
		w := max(clamp(c.Width.Value), 0)
		lo = max(lo, w)
		hi = max(hi, lo)
	}
	n := 1
	if c.Count > 0 {
		n = min(c.Count, maxColumns)
	}
	g := 0.0
	if b.st.ColumnGap.Kind == css.Normal {
		g = e.fontSize(b.st)
	} else if b.st.ColumnGap.Kind == css.Points {
		g = max(clamp(b.st.ColumnGap.Value), 0)
	}
	k := float64(n)
	return lo*k + g*(k-1), hi*k + g*(k-1)
}

// layout lays out the content of the multi-column container b: its column
// sets in columns, its spanners across the container (CSS Multicol 1, 3
// to 6). The content of b's content box starts at cursor.
func (multicolFormatter) layout(e *engine, b *box, cb cblock, bc *bfc, cursor float64, mc *marginState) float64 {
	g := e.columnGap(b, cb.w)
	n, w := columnLayout(&b.st.Columns, cb.w, g)
	d := &mcolData{n: n, w: w, gap: g, span: cb.w, rtl: b.st.Direction == css.RTL}
	b.mc = d
	limit := math.Inf(1)
	if cb.hDefinite {
		limit = cursor + cb.h
	}
	if mh, ok := specHeight(b, b.st.MaxHeight, cblock{}); ok {
		limit = min(limit, cursor+mh)
	}
	kcb := cblock{x: cb.x, w: w, h: cb.h, hDefinite: cb.hDefinite}
	var sets []*colSet
	if b.inlineKids {
		sets = []*colSet{{kids: b.kids}}
	} else {
		cur := &colSet{}
		for _, k := range b.kids {
			if k.st.Columns.SpanAll && !k.inline && !k.abs && k.float == css.FloatNone {
				sets = append(sets, cur, &colSet{kids: []*box{k}, spanner: k})
				cur = &colSet{}
				continue
			}
			cur.kids = append(cur.kids, k)
		}
		sets = append(sets, cur)
	}
	y := cursor
	var smc *marginState
	for i, s := range sets {
		if e.err != nil {
			break
		}
		if s.spanner != nil {
			if smc == nil {
				smc = &marginState{kept: true}
			}
			y = e.layoutBlock(s.spanner, cb, bc, y, smc, breakNone)
			d.sets = append(d.sets, s)
			continue
		}
		if smc != nil {
			y = e.pastMargin(y, smc.strut, false)
			smc.commit(y)
			smc = nil
		}
		if !hasFlow(s.kids, b.inlineKids) {
			e.placeOutOfFlow(b, s.kids, kcb, y)
			continue
		}
		spanned := i+1 < len(sets)
		y = e.layoutColumnSet(b, s, kcb, y, limit, spanned)
		d.sets = append(d.sets, s)
	}
	if smc != nil {
		mc.strut = smc.strut
	}
	return y
}

// hasFlow reports kids with content to lay out in columns: in-flow boxes,
// floats, or inline content.
func hasFlow(kids []*box, inline bool) bool {
	if inline {
		return len(kids) > 0
	}
	for _, k := range kids {
		if !k.abs {
			return true
		}
	}
	return false
}

// placeOutOfFlow records the static position of the absolutely positioned
// boxes of a column set with nothing else, at y.
func (e *engine) placeOutOfFlow(b *box, kids []*box, cb cblock, y float64) {
	for _, k := range kids {
		if k.abs {
			staticBlock(k, cb, y)
		}
	}
}

// layoutColumnSet lays out the column set s of the container b, its
// columns cb wide, from y in the outer flow, the container's height
// ending at limit, and returns where the set ends. spanned reports a set
// a spanner follows, whose content balances whatever column-fill says
// (CSS Multicol 1, 6.1).
func (e *engine) layoutColumnSet(b *box, s *colSet, cb cblock, y, limit float64, spanned bool) float64 {
	st := &b.st.Columns
	ctx := &colCtx{e: e, parent: e.cols, noPag: e.noPaginate, n: b.mc.n, limit: limit, start: y, balanceRow: -1}
	if st.Height.Kind == css.Points {
		ctx.colH = max(clamp(st.Height.Value), minColumnHeight)
		ctx.wrap = st.Wrap()
		if b.st.RowGap.Kind != css.Normal {
			ctx.gap = max(resolve(b.st.RowGap, cb.w), 0)
		}
	}
	warnings := len(e.warnings)
	var end float64
	pass := func(row int, h float64) []float64 {
		e.warnings = e.warnings[:warnings]
		ctx.balanceRow, ctx.balanceH = row, h
		ctx.frags, ctx.rows = nil, nil
		end = e.layoutColumns(b, s, cb, ctx)
		return ctx.consumed(s.kids, end)
	}
	cons := pass(-1, 0)
	last := ctx.lastFrag(cons)
	r := ctx.frags[last].row
	row := ctx.rows[r]
	// The content fits a row when it ends in the row's columns, none of
	// which it overflows.
	fits := func() bool {
		l := ctx.lastFrag(cons)
		return ctx.frags[l].row == r && l < ctx.rows[r].first+ctx.n && ctx.rowMax(cons, r) <= ctx.rows[r].h+eps
	}
	balance := (st.Fill != css.FillAuto || spanned || row.h >= unbounded) && ctx.colH <= 0
	if balance && fits() {
		hi := row.h
		total := 0.0
		for i := row.first; i <= last; i++ {
			total += cons[i]
		}
		if hi >= unbounded {
			hi = total
		}
		lo := total / float64(ctx.n)
		best := hi
		bestFits := row.h == hi
		for range maxBalancePasses {
			if hi-lo < 0.01 || !e.relayout(y) {
				break
			}
			mid := (lo + hi) / 2
			cons = pass(r, mid)
			if fits() {
				used := ctx.rowMax(cons, r)
				hi, best, bestFits = min(mid, used), min(mid, used), true
				continue
			}
			lo = mid
		}
		if !bestFits || ctx.rows[r].h != best || !fits() {
			cons = pass(r, best)
		}
	}
	return ctx.finish(b, s, cons, end, balance)
}

// layoutColumns lays the content of the column set s of b out in the
// columns of ctx, and returns the column flow position after it.
func (e *engine) layoutColumns(b *box, s *colSet, cb cblock, ctx *colCtx) float64 {
	saved, savedN := e.cols, e.noPaginate
	e.cols, e.noPaginate = ctx, 0
	defer func() { e.cols, e.noPaginate = saved, savedN }()
	ctx.of(ctx.start)
	own := &bfc{}
	mc := &marginState{kept: true}
	var end float64
	if b.inlineKids {
		end = e.layoutInline(b, cb, own, ctx.start, mc)
	} else {
		kids := b.kids
		b.kids = s.kids
		end = e.layoutBlockKids(b, cb, own, ctx.start, mc)
		b.kids = kids
	}
	end = e.pastMargin(end, mc.strut, false)
	mc.commit(end)
	end = max(end, own.bottom())
	e.reach(end)
	return end
}

// consumed returns how much of each column the content of kids, ending at
// end, takes: the bottom of the last piece in it, from its top. A box
// counts in the column it ends in, its lines and its boxes in theirs.
func (c *colCtx) consumed(kids []*box, end float64) []float64 {
	out := make([]float64, len(c.frags))
	add := func(i int, bottom float64) {
		if i >= 0 && i < len(out) {
			out[i] = max(out[i], bottom-c.frags[i].vtop)
		}
	}
	var walk func(k *box, depth int)
	walk = func(k *box, depth int) {
		if !k.laidOut && k.kind != kindInline || k.abs || k.fixed || depth > maxTreeDepth {
			return
		}
		if k.kind == kindInline {
			for _, kk := range k.kids {
				walk(kk, depth+1)
			}
			return
		}
		if k.kind == kindText || k.kind == kindBreak {
			return
		}
		top, bot := k.y, k.y+k.borderH()
		add(c.index(max(bot-eps, top)), bot)
		if k.mc != nil {
			for _, s := range k.mc.sets {
				for _, r := range s.rows {
					t := k.y + r.top
					add(c.index(t), t+r.used)
				}
			}
			return
		}
		for _, l := range k.lines {
			add(c.index(l.y), l.y+l.h)
		}
		for _, kk := range k.kids {
			walk(kk, depth+1)
		}
	}
	for _, k := range kids {
		walk(k, 0)
	}
	if len(out) > 0 && end > c.start+eps {
		add(c.index(end-eps), end)
	}
	return out
}

// lastFrag returns the last column with content in cons, 0 for none.
func (c *colCtx) lastFrag(cons []float64) int {
	for i := len(cons) - 1; i >= 0; i-- {
		if cons[i] > eps {
			return i
		}
	}
	return 0
}

// rowMax returns the most any column of row r takes in cons.
func (c *colCtx) rowMax(cons []float64, r int) float64 {
	m := 0.0
	for i := c.rows[r].first; i < len(cons) && c.frags[i].row == r; i++ {
		m = max(m, cons[i])
	}
	return m
}

// finish records the columns and rows of the column set s of b, laid out
// in ctx with cons taken of each column and the content ending at end, and
// returns where the set ends in the outer flow: the bottom of its last
// row, which takes the height of its columns when they are balanced or
// their height is fixed, the height of its tallest content otherwise.
func (c *colCtx) finish(b *box, s *colSet, cons []float64, end float64, balanced bool) float64 {
	last := c.lastFrag(cons)
	lr := c.frags[last].row
	s.rows = s.rows[:0]
	s.frags = s.frags[:0]
	for r := 0; r <= lr; r++ {
		row := c.rows[r]
		row.used = row.h
		if r == lr && !balanced && math.IsInf(c.limit, 1) && c.colH <= 0 {
			row.used = min(c.rowMax(cons, r), row.h)
		}
		if r == lr && row.h >= unbounded {
			row.used = c.rowMax(cons, r)
		}
		lastCol := row.first
		for i := row.first; i < len(c.frags) && c.frags[i].row == r && i <= last; i++ {
			if cons[i] > eps {
				lastCol = i
			}
		}
		row.cols = lastCol - row.first + 1
		row.top -= b.y
		s.rows = append(s.rows, row)
	}
	for _, f := range c.frags[:last+1] {
		f.vtop -= b.y
		s.frags = append(s.frags, f)
	}
	lrow := s.rows[len(s.rows)-1]
	return b.y + lrow.top + lrow.used
}

// columnX returns the offset of column col of a row from the first
// column's left edge: columns run left to right, right to left in an rtl
// container (CSS Multicol 1, 2).
func (d *mcolData) columnX(col int) float64 {
	step := float64(col) * (d.w + d.gap)
	if d.rtl {
		return d.span - d.w - step
	}
	return step
}

// shiftOf returns how far column i of s moves its slice of the column
// flow of the container b: to its row and its place in the row.
func (d *mcolData) shiftOf(b *box, s *colSet, i int) (float64, float64) {
	f := s.frags[min(max(i, 0), len(s.frags)-1)]
	r := s.rows[min(f.row, len(s.rows)-1)]
	return d.columnX(f.col), (b.y + r.top) - (b.y + f.vtop)
}

// fragOf returns the column of s the column flow position y of the
// container b lies in, the first or the last one outside them.
func (s *colSet) fragOf(b *box, y float64) int {
	i := sort.Search(len(s.frags), func(i int) bool { return b.y+s.frags[i].vtop > y }) - 1
	return min(max(i, 0), len(s.frags)-1)
}

// setOf returns the column set of the container b that holds its
// descendant d, nil when d sits in a spanner or b lays out no columns.
func setOf(b, d *box) *colSet {
	if b.mc == nil {
		return nil
	}
	k := d
	for k != nil && k.parent != b {
		k = k.parent
	}
	if k == nil {
		return nil
	}
	if b.inlineKids && len(b.mc.sets) > 0 {
		return b.mc.sets[0]
	}
	for _, s := range b.mc.sets {
		if s.spanner != nil {
			continue
		}
		for _, kk := range s.kids {
			if kk == k {
				return s
			}
		}
	}
	return nil
}

// toFlow maps the position (x, y) of the descendant d, in the column flows
// of the multi-column containers around it, to the document's flow.
func toFlow(d *box, x, y float64) (float64, float64) {
	for a := d.parent; a != nil; a = a.parent {
		if a.mc == nil {
			continue
		}
		s := setOf(a, d)
		if s == nil || len(s.frags) == 0 {
			continue
		}
		dx, dy := a.mc.shiftOf(a, s, s.fragOf(a, y))
		x, y = x+dx, y+dy
		d = a
	}
	return x, y
}

// inColumns reports whether the box b sits in the columns of a
// multi-column container.
func inColumns(b *box) bool {
	for a := b.parent; a != nil; a = a.parent {
		if a.mc != nil {
			return true
		}
	}
	return false
}

// forces reports a break value that forces a break at the fragmentainer
// being laid out: a page break, and in columns a column break too, a page
// break inside columns breaking the column (CSS Fragmentation 3, 3.1).
func (e *engine) forces(v css.Break) bool {
	return isForced(v) || e.cols != nil && v == css.Column
}

// flowStart returns where the flow of the fragmentation context starts: a
// forced break there breaks nothing.
func (e *engine) flowStart() float64 {
	if e.cols != nil {
		return e.cols.start
	}
	return 0
}

// layoutPositionedInColumns lays out the absolutely positioned boxes inside
// the multi-column container b, each column set's in its columns, so they
// break at the columns as the flow does (CSS Fragmentation 3, 5.4: an
// absolutely positioned box in a fragmentation context fragments with
// it); the spanners' boxes in the outer flow.
func (e *engine) layoutPositionedInColumns(b *box) {
	for _, s := range b.mc.sets {
		if e.err != nil {
			return
		}
		if s.spanner != nil || len(s.frags) == 0 {
			for _, k := range s.kids {
				if k.abs && k.staticSet {
					e.layoutAbs(k)
				}
				e.layoutPositioned(k)
			}
			continue
		}
		ctx := &colCtx{e: e, parent: e.cols, noPag: e.noPaginate, n: b.mc.n, limit: math.Inf(1), balanceRow: -1}
		for _, f := range s.frags {
			f.vtop += b.y
			ctx.frags = append(ctx.frags, f)
		}
		for _, r := range s.rows {
			r.top += b.y
			r.next = false
			ctx.rows = append(ctx.rows, r)
		}
		ctx.start = ctx.frags[0].vtop
		saved, savedN := e.cols, e.noPaginate
		e.cols, e.noPaginate = ctx, 0
		for _, k := range s.kids {
			if e.err != nil {
				break
			}
			if k.abs {
				if !k.staticSet {
					k.laidOut = false
					continue
				}
				e.layoutAbs(k)
			}
			if k.mc != nil {
				e.layoutPositionedInColumns(k)
				continue
			}
			e.layoutPositioned(k)
		}
		e.cols, e.noPaginate = saved, savedN
	}
}
