package layout

import (
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// tablePart is a box's part in a table (CSS 2.1, 17.2).
type tablePart uint8

const (
	partNone tablePart = iota
	// partWrapper is the table wrapper box: the table box and its captions.
	partWrapper
	// partTable is the table box, which holds the grid.
	partTable
	// partCaption is a table caption, a block box of the wrapper.
	partCaption
	// partRowGroup, partRow and partCell make up the grid.
	partRowGroup
	partRow
	partCell
	// partColumn and partColGroup are the column boxes, which paint
	// backgrounds and borders but hold no content.
	partColumn
	partColGroup
)

// internal reports an internal table box, laid out and painted by its
// table.
func (p tablePart) internal() bool { return p >= partRowGroup }

// tabular reports a box that belongs inside a table: a caption or an
// internal table box (CSS 2.1, 17.2.1).
func (p tablePart) tabular() bool { return p >= partCaption }

// Origins of a collapsed border, by precedence (CSS 2.1, 17.6.2.1, rule
// 4): a cell's wins over a row's, then a row group's, a column's, a column
// group's and the table's.
const (
	originTable uint8 = iota
	originColGroup
	originColumn
	originRowGroup
	originRow
	originCell
)

// cborder is one resolved border of the collapsing border model.
type cborder struct {
	w      float64
	style  css.BorderStyle
	color  css.Color
	origin uint8
}

// visible reports a border that paints.
func (c cborder) visible() bool {
	_, ok := colorOf(c.color)
	return c.w > 0 && ok && c.style != css.BorderNone && c.style != css.BorderHidden
}

// styleRank orders border styles for conflict resolution (CSS 2.1,
// 17.6.2.1, rule 3): double wins over solid, dashed, dotted, ridge,
// outset, groove and inset, in that order; none loses to all.
func styleRank(s css.BorderStyle) int {
	switch s {
	case css.Double:
		return 8
	case css.Solid:
		return 7
	case css.Dashed:
		return 6
	case css.Dotted:
		return 5
	case css.Ridge:
		return 4
	case css.Outset:
		return 3
	case css.Groove:
		return 2
	case css.Inset:
		return 1
	case css.BorderNone, css.BorderHidden:
	}
	return 0
}

// beats reports whether a wins over b (CSS 2.1, 17.6.2.1, rules 2 to 4):
// the wider, then the better style, then the closer origin. A tie keeps b,
// the one met first, which is further up or left (rule 5).
func (a cborder) beats(b cborder) bool {
	if a.w != b.w {
		return a.w > b.w
	}
	if ra, rb := styleRank(a.style), styleRank(b.style); ra != rb {
		return ra > rb
	}
	return a.origin > b.origin
}

// resolver collects the borders that meet on one edge.
type resolver struct {
	best   cborder
	hidden bool
}

// add considers border bd of the given origin.
func (r *resolver) add(bd css.Border, origin uint8) {
	if bd.Style == css.BorderHidden {
		// Hidden suppresses every border at the edge (rule 1).
		r.hidden = true
		return
	}
	c := cborder{w: max(clamp(bd.Width), 0), style: bd.Style, color: bd.Color, origin: origin}
	if bd.Style == css.BorderNone {
		c.w = 0
	}
	if c.beats(r.best) {
		r.best = c
	}
}

// result returns the winning border.
func (r *resolver) result() cborder {
	if r.hidden {
		return cborder{}
	}
	return r.best
}

// tableCol is one column of the grid and the column and column group
// boxes that apply to it, nil for none.
type tableCol struct {
	col, group *box
}

// tableRow is one row of the grid and its row group.
type tableRow struct {
	box, group *box
}

// cellData is the grid slot of a cell and its measures.
type cellData struct {
	row, col, rowSpan, colSpan int
	// half holds half of the collapsed border on each side, indexed by
	// [css.Side], in the collapsing border model.
	half [4]float64
	// min and max are the outer min- and max-content widths.
	min, max float64
	measured bool
	// natH is the border box height of the content, base the baseline
	// offset from the border box top, at the tentative layout. A cell
	// without a line box has no baseline: it takes no part in its row's
	// and aligns at the top, as browsers do.
	natH, base float64
	hasBase    bool
}

// placedRow is a row as it lands in the flow: the row itself, or a copy of
// a header or footer row moved by dy. clip is the page a row taller than
// the page area is clipped to, -1 for none.
type placedRow struct {
	r    int
	dy   float64
	clip int
}

// top returns the top of the placed row.
func (td *tableData) top(p placedRow) float64 { return td.rows[p.r].box.y + p.dy }

// bottom returns the bottom of the placed row.
func (td *tableData) bottom(p placedRow) float64 {
	rb := td.rows[p.r].box
	return rb.y + rb.h + p.dy
}

// tableData is the table model of one table (CSS 2.1, 17.5): its grid, its
// collapsed borders and where its rows landed.
type tableData struct {
	wrapper, table *box
	collapse       bool
	cols           []tableCol
	// colBoxes lists the column and column group boxes in tree order.
	colBoxes []*box
	// groups lists the row groups in visual order: the header group first
	// and the footer group last (CSS 2.1, 17.2).
	groups []*box
	rows   []tableRow
	cells  []*box
	nCols  int
	// grid maps each slot, row-major, to the index of its cell, -1 for an
	// empty slot.
	grid []int32
	// headN and footN are the rows of the header and footer groups, the
	// first and last rows of the grid.
	headN, footN int
	// hl holds the collapsed border of every row edge, (rows + 1) by
	// columns, and vl of every column edge, rows by (columns + 1).
	hl, vl []cborder

	measured bool
	cm       []colMeasure

	// colX holds the left edge of every column relative to the table's
	// content box, and one past the last; in the collapsing model, the
	// grid lines the borders centre on.
	colX []float64
	// seqs lists the runs of rows painted together: a page's rows, copies
	// of the header and footer included, top to bottom.
	seqs [][]placedRow
	// baseline is the first row's baseline below the table box top.
	baseline    float64
	hasBaseline bool
}

// cellAt returns the cell covering slot (r, c), nil for none.
func (td *tableData) cellAt(r, c int) *box {
	if r < 0 || r >= len(td.rows) || c < 0 || c >= td.nCols {
		return nil
	}
	i := td.grid[r*td.nCols+c]
	if i < 0 {
		return nil
	}
	return td.cells[i]
}

// wrapperStyle returns the style of a table's wrapper box: the properties
// that place the table, its margins, position, float and breaks (CSS 2.1,
// 17.4); the table box takes the others.
func wrapperStyle(st *css.Style) *css.Style {
	w := *st
	w.Border = [4]css.Border{}
	w.BorderRadius = [4]css.Radius{}
	w.Padding = [4]css.Dimension{css.Pts(0), css.Pts(0), css.Pts(0), css.Pts(0)}
	w.BackgroundColor = css.Transparent
	w.BackgroundImage = ""
	w.BackgroundGradient = nil
	w.Width, w.Height = css.Dimension{}, css.Dimension{}
	w.MinWidth, w.MinHeight = css.Pts(0), css.Pts(0)
	w.MaxWidth, w.MaxHeight = css.Dimension{Kind: css.None}, css.Dimension{Kind: css.None}
	w.Overflow = css.OverflowVisible
	w.BoxSizing = css.ContentBox
	return &w
}

// tableStyle returns the style of a table box: the table's own, without
// what its wrapper takes.
func tableStyle(st *css.Style) *css.Style {
	t := *st
	t.Margin = [4]css.Dimension{css.Pts(0), css.Pts(0), css.Pts(0), css.Pts(0)}
	t.Position = css.Static
	t.ZIndex = css.ZIndex{Auto: true}
	t.Transform = nil
	t.Float = css.FloatNone
	t.Clear = css.ClearNone
	t.Opacity = 1
	t.BreakBefore, t.BreakAfter, t.BreakInside = css.BreakAuto, css.BreakAuto, css.InsideAuto
	t.Top, t.Right, t.Bottom, t.Left = css.Dimension{}, css.Dimension{}, css.Dimension{}, css.Dimension{}
	return &t
}

// partStyle returns the style of an anonymous table box of display d
// inside a box of style parent (CSS 2.1, 17.2.1).
func partStyle(parent *css.Style, d css.Display) *css.Style {
	st := anonStyle(parent)
	st.Display = d
	return st
}

// anonPart returns an anonymous table box of part p and style st inside
// parent.
func (e *engine) anonPart(parent *box, st *css.Style, p tablePart) *box {
	e.boxes++
	if e.boxes > MaxBoxes {
		e.fail(fmt.Errorf("%w: more than %d", ErrTooManyBoxes, MaxBoxes))
	}
	b := &box{
		st: st, kind: kindBlock, anon: true, first: true, last: true, parent: parent,
		lang: parent.lang, elem: parent.elem, link: parent.link, depth: parent.depth + 1, tpart: p,
	}
	b.hidden = st.Visibility != css.Visible
	if p == partCell {
		b.cell = &cellData{}
	}
	return b
}

// genTable generates the boxes of a table element n of style st: a
// wrapper box holding the captions and the table box (CSS 2.1, 17.4).
func (e *engine) genTable(n *Node, st *css.Style, ctx genCtx, float css.Float) *box {
	w := e.newBox(n, wrapperStyle(st), kindBlock, ctx)
	w.tpart = partWrapper
	w.float = float
	w.inline = float == css.FloatNone && st.Display == css.InlineTable
	cctx := e.enter(n, w, ctx)
	if w.float != css.FloatNone || w.inline {
		cctx.decos = ownDecos(st)
	}
	t := e.newBox(nil, tableStyle(st), kindBlock, cctx)
	t.tpart = partTable
	t.parent = w
	tctx := cctx
	tctx.depth++
	var raw []*box
	ordinal := new(int)
	for _, c := range n.Children {
		if e.err != nil {
			break
		}
		raw = append(raw, e.genNode(c, t, tctx, ordinal)...)
	}
	e.buildTable(w, t, raw)
	return w
}

// genPart generates an internal table box or a caption of node n.
func (e *engine) genPart(n *Node, st *css.Style, ctx genCtx) *box {
	b := e.newBox(n, st, kindBlock, ctx)
	cctx := e.enter(n, b, ctx)
	if st.Display == css.TableCaption || st.Display == css.TableCell {
		b.tpart = partCaption
		if st.Display == css.TableCell {
			b.tpart = partCell
			b.cell = &cellData{}
		}
		e.genChildren(b, n.Children, cctx)
		return b
	}
	if st.Display == css.TableRow {
		b.tpart = partRow
		e.fixRow(b, e.genRaw(b, n.Children, cctx))
		return b
	}
	b.tpart = partRowGroup
	e.fixGroup(b, e.genRaw(b, n.Children, cctx))
	return b
}

// genColumn generates a column or column group box of node n inside
// parent. A column holds nothing, a column group only its columns (CSS
// 2.1, 17.2.1, rules 1.1 and 1.2).
func (e *engine) genColumn(n *Node, st *css.Style, ctx genCtx) *box {
	b := e.newBox(n, st, kindBlock, ctx)
	cctx := e.enter(n, b, ctx)
	b.tpart = partColumn
	if st.Display != css.TableColumnGroup {
		return b
	}
	b.tpart = partColGroup
	for _, c := range n.Children {
		if c == nil || c.Kind != Element || c.Style == nil || c.Style.Display != css.TableColumn || e.err != nil {
			continue
		}
		if cctx.depth >= maxTreeDepth {
			break
		}
		k := e.genColumn(c, c.Style, cctx)
		k.parent = b
		b.kids = append(b.kids, k)
	}
	return b
}

// genRaw generates the boxes of children inside b without fixing them up.
func (e *engine) genRaw(b *box, children []*Node, ctx genCtx) []*box {
	var out []*box
	ordinal := new(int)
	for _, c := range children {
		if e.err != nil {
			break
		}
		out = append(out, e.genNode(c, b, ctx, ordinal)...)
	}
	return out
}

// isSpace reports a text box of white space alone.
func isSpace(b *box) bool {
	return b.kind == kindText && strings.Trim(b.text, " \t\n\r\f") == ""
}

// dropSpaces removes the runs of white space between table boxes, and at
// the start and end of a tabular container (CSS 2.1, 17.2.1, rules 1.3 and
// 1.4).
func dropSpaces(kids []*box) []*box {
	var out []*box
	for i := 0; i < len(kids); {
		if !isSpace(kids[i]) {
			out = append(out, kids[i])
			i++
			continue
		}
		j := i
		for j < len(kids) && isSpace(kids[j]) {
			j++
		}
		before := i == 0 || kids[i-1].tpart.tabular()
		after := j == len(kids) || kids[j].tpart.tabular()
		if !before || !after {
			out = append(out, kids[i:j]...)
		}
		i = j
	}
	return out
}

// adopt makes kids the children of the block container b: runs of table
// boxes get an anonymous table (CSS 2.1, 17.2.1, rule 3), then inline runs
// anonymous blocks.
func (e *engine) adopt(b *box, kids []*box) {
	for _, k := range e.wrapOrphans(b, kids, false) {
		k.parent = b
		b.kids = append(b.kids, k)
	}
	e.normalize(b)
}

// wrapOrphans wraps every run of captions and internal table boxes among
// the children kids of parent in an anonymous table, an inline-table when
// inline (CSS 2.1, 17.2.1, rule 3).
func (e *engine) wrapOrphans(parent *box, kids []*box, inline bool) []*box {
	found := false
	for _, k := range kids {
		found = found || k.tpart.tabular()
	}
	if !found {
		return kids
	}
	var out []*box
	for i := 0; i < len(kids); {
		if !kids[i].tpart.tabular() {
			out = append(out, kids[i])
			i++
			continue
		}
		var run []*box
		j := i
		for j < len(kids) {
			k := kids[j]
			if k.tpart.tabular() {
				run = append(run, k)
				j++
				continue
			}
			s := j
			for s < len(kids) && isSpace(kids[s]) {
				s++
			}
			if s > j && s < len(kids) && kids[s].tpart.tabular() {
				j = s
				continue
			}
			break
		}
		d := css.Table
		if inline {
			d = css.InlineTable
		}
		w := e.anonPart(parent, partStyle(parent.st, d), partWrapper)
		w.inline = inline
		t := e.anonPart(w, partStyle(parent.st, d), partTable)
		e.buildTable(w, t, run)
		out = append(out, w)
		i = j
	}
	return out
}

// fixRow makes kids the children of row: cells as they are, every other
// run in an anonymous cell (CSS 2.1, 17.2.1, rule 2).
func (e *engine) fixRow(row *box, kids []*box) {
	kids = dropSpaces(kids)
	for i := 0; i < len(kids); {
		if kids[i].tpart == partCell {
			kids[i].parent = row
			row.kids = append(row.kids, kids[i])
			i++
			continue
		}
		j := i
		for j < len(kids) && kids[j].tpart != partCell {
			j++
		}
		c := e.anonPart(row, partStyle(row.st, css.TableCell), partCell)
		e.adopt(c, kids[i:j])
		row.kids = append(row.kids, c)
		i = j
	}
}

// fixGroup makes kids the children of the row group g: rows as they are,
// every other run in an anonymous row (CSS 2.1, 17.2.1, rule 2).
func (e *engine) fixGroup(g *box, kids []*box) {
	kids = dropSpaces(kids)
	for i := 0; i < len(kids); {
		if kids[i].tpart == partRow {
			kids[i].parent = g
			g.kids = append(g.kids, kids[i])
			i++
			continue
		}
		j := i
		for j < len(kids) && kids[j].tpart != partRow {
			j++
		}
		r := e.anonPart(g, partStyle(g.st, css.TableRow), partRow)
		e.fixRow(r, kids[i:j])
		g.kids = append(g.kids, r)
		i = j
	}
}

// spanOf returns a span from a node field: at least 1.
func spanOf(v int) int { return max(v, 1) }

// buildTable sorts the generated children raw of the table box t into
// captions, columns and row groups, wraps loose rows and content in
// anonymous row groups and rows (CSS 2.1, 17.2.1), places the cells in the
// grid (17.5) and resolves the collapsed borders (17.6.2).
func (e *engine) buildTable(w, t *box, raw []*box) {
	td := &tableData{wrapper: w, table: t, collapse: t.st.BorderCollapse == css.CollapseBorders}
	w.tdata, t.tdata = td, td
	t.parent = w
	raw = dropSpaces(raw)
	var captions, groups, pending, loose []*box
	flushLoose := func() {
		if len(loose) == 0 {
			return
		}
		r := e.anonPart(t, partStyle(t.st, css.TableRow), partRow)
		e.fixRow(r, loose)
		pending = append(pending, r)
		loose = nil
	}
	flushRows := func() {
		flushLoose()
		if len(pending) == 0 {
			return
		}
		g := e.anonPart(t, partStyle(t.st, css.TableRowGroup), partRowGroup)
		for _, r := range pending {
			r.parent = g
		}
		g.kids = pending
		groups = append(groups, g)
		pending = nil
	}
	for _, k := range raw {
		switch k.tpart {
		case partCaption:
			flushRows()
			captions = append(captions, k)
		case partColumn, partColGroup:
			flushRows()
			k.parent = t
			td.colBoxes = append(td.colBoxes, k)
		case partRowGroup:
			flushRows()
			groups = append(groups, k)
		case partRow:
			flushLoose()
			pending = append(pending, k)
		case partNone, partWrapper, partTable, partCell:
			loose = append(loose, k)
		}
	}
	flushRows()
	var head, foot *box
	var bodies []*box
	for _, g := range groups {
		switch {
		case head == nil && !g.anon && g.st.Display == css.TableHeaderGroup:
			head = g
		case foot == nil && !g.anon && g.st.Display == css.TableFooterGroup:
			foot = g
		default:
			bodies = append(bodies, g)
		}
	}
	if head != nil {
		td.groups = append(td.groups, head)
	}
	td.groups = append(td.groups, bodies...)
	if foot != nil {
		td.groups = append(td.groups, foot)
	}
	for _, g := range td.groups {
		g.parent = t
	}
	t.kids = td.groups
	w.kids = nil
	for _, side := range []css.CaptionSide{css.CaptionTop, css.CaptionBottom} {
		for _, c := range captions {
			if c.st.CaptionSide == side {
				c.parent = w
				w.kids = append(w.kids, c)
			}
		}
		if side == css.CaptionTop {
			w.kids = append(w.kids, t)
		}
	}
	e.placeCells(td, head, foot)
	if td.collapse {
		td.resolveBorders()
	}
}

// placeCells lays the rows of td's groups out in the grid and places every
// cell in the first free slot of its row, spanning the slots its spans ask
// for: a row span stops at the end of its row group, and a span that would
// overlap an earlier cell stops short of it.
func (e *engine) placeCells(td *tableData, head, foot *box) {
	tooLarge := func(what string) {
		e.warnOnce(codeTableTooLarge, fmt.Sprintf("a table with more than %s: the rest left out", what), -1)
	}
	for _, g := range td.groups {
		kept := g.kids[:0]
		for _, r := range g.kids {
			if len(td.rows) >= maxTableRows {
				tooLarge(fmt.Sprintf("%d rows", maxTableRows))
				continue
			}
			kept = append(kept, r)
			td.rows = append(td.rows, tableRow{box: r, group: g})
		}
		g.kids = kept
		if g == head {
			td.headN = len(g.kids)
		}
		if g == foot {
			td.footN = len(g.kids)
		}
	}
	for _, cb := range td.colBoxes {
		add := func(col, group *box, span int) {
			for range span {
				if len(td.cols) >= maxTableColumns {
					tooLarge(fmt.Sprintf("%d columns", maxTableColumns))
					return
				}
				td.cols = append(td.cols, tableCol{col: col, group: group})
			}
		}
		switch {
		case cb.tpart == partColumn:
			add(cb, nil, spanOf(cb.node.Span))
		case len(cb.kids) == 0:
			add(nil, cb, spanOf(cb.node.Span))
		default:
			for _, c := range cb.kids {
				add(c, cb, spanOf(c.node.Span))
			}
		}
	}
	nr := len(td.rows)
	occ := make([][]int32, nr)
	free := func(r, c int) bool { return c >= len(occ[r]) || occ[r][c] < 0 }
	area := 0
	groupEnd := make([]int, nr)
	for i := nr - 1; i >= 0; i-- {
		groupEnd[i] = i
		if i+1 < nr && td.rows[i+1].group == td.rows[i].group {
			groupEnd[i] = groupEnd[i+1]
		}
	}
	for ri, row := range td.rows {
		col := 0
		kept := row.box.kids[:0]
		for _, c := range row.box.kids {
			for !free(ri, col) {
				col++
			}
			if col >= maxTableColumns {
				tooLarge(fmt.Sprintf("%d columns", maxTableColumns))
				continue
			}
			if len(td.cells) >= maxTableCells {
				tooLarge(fmt.Sprintf("%d cells", maxTableCells))
				continue
			}
			cs, rs := 1, 1
			if c.node != nil {
				cs, rs = spanOf(c.node.ColSpan), spanOf(c.node.RowSpan)
			}
			cs = min(cs, maxTableColumns-col)
			rs = min(rs, groupEnd[ri]-ri+1)
			for k := 1; k < cs; k++ {
				if !free(ri, col+k) {
					cs = k
					break
				}
			}
		rows:
			for k := 1; k < rs; k++ {
				for x := col; x < col+cs; x++ {
					if !free(ri+k, x) {
						rs = k
						break rows
					}
				}
			}
			if area+rs*cs > maxTableSlots {
				tooLarge(fmt.Sprintf("%d slots", maxTableSlots))
				continue
			}
			area += rs * cs
			idx := int32(len(td.cells)) //nolint:gosec // fits int32: at most maxTableCells, checked above
			for r := ri; r < ri+rs; r++ {
				for len(occ[r]) < col+cs {
					occ[r] = append(occ[r], -1)
				}
				for x := col; x < col+cs; x++ {
					occ[r][x] = idx
				}
			}
			c.cell = &cellData{row: ri, col: col, rowSpan: rs, colSpan: cs}
			td.cells = append(td.cells, c)
			kept = append(kept, c)
			col += cs
		}
		row.box.kids = kept
	}
	nc := len(td.cols)
	for _, o := range occ {
		nc = max(nc, len(o))
	}
	if nc > 0 && nr*nc > maxTableSlots {
		// The grid is too large: keep the rows that fit.
		tooLarge(fmt.Sprintf("%d slots", maxTableSlots))
		keep := maxTableSlots / nc
		td.truncateRows(keep)
		nr = keep
	}
	td.nCols = nc
	for len(td.cols) < nc {
		td.cols = append(td.cols, tableCol{})
	}
	td.grid = make([]int32, nr*nc)
	for i := range td.grid {
		td.grid[i] = -1
	}
	rtl := td.table.st.Direction == css.RTL
	if rtl {
		// The first column is the rightmost one (CSS 2.1, 17.5): the grid
		// is kept in visual order.
		for _, c := range td.cells {
			c.cell.col = nc - c.cell.col - c.cell.colSpan
		}
		for i, j := 0, len(td.cols)-1; i < j; i, j = i+1, j-1 {
			td.cols[i], td.cols[j] = td.cols[j], td.cols[i]
		}
	}
	for i, c := range td.cells {
		cd := c.cell
		for r := cd.row; r < cd.row+cd.rowSpan && r < nr; r++ {
			for x := cd.col; x < cd.col+cd.colSpan; x++ {
				td.grid[r*nc+x] = int32(i) // fits int32: at most maxTableCells
			}
		}
	}
}

// truncateRows keeps the first keep rows of td and the cells that start in
// them, their row spans cut at the last row kept.
func (td *tableData) truncateRows(keep int) {
	if keep >= len(td.rows) {
		return
	}
	for _, r := range td.rows[keep:] {
		r.box.kids = nil
		g := r.group
		kept := g.kids[:0]
		for _, k := range g.kids {
			if k != r.box {
				kept = append(kept, k)
			}
		}
		g.kids = kept
	}
	td.rows = td.rows[:keep]
	cells := td.cells[:0]
	for _, c := range td.cells {
		if c.cell.row < keep {
			c.cell.rowSpan = min(c.cell.rowSpan, keep-c.cell.row)
			cells = append(cells, c)
		}
	}
	td.cells = cells
	// The footer rows were the last ones: they are gone.
	td.headN = min(td.headN, keep)
	td.footN = 0
}

// firstCol reports whether column c starts its column group.
func (td *tableData) firstCol(c int) bool {
	return c == 0 || td.cols[c-1].group != td.cols[c].group
}

// lastCol reports whether column c ends its column group.
func (td *tableData) lastCol(c int) bool {
	return c == td.nCols-1 || td.cols[c+1].group != td.cols[c].group
}

// firstRow reports whether row r starts its row group.
func (td *tableData) firstRow(r int) bool {
	return r == 0 || td.rows[r-1].group != td.rows[r].group
}

// lastRow reports whether row r ends its row group.
func (td *tableData) lastRow(r int) bool {
	return r == len(td.rows)-1 || td.rows[r+1].group != td.rows[r].group
}

// hEdge resolves the collapsed border of column c between row above and
// row below (CSS 2.1, 17.6.2.1): -1 above is the table's top edge and the
// row count below its bottom edge. The rows need not be adjacent: a copy
// of the header meets the row it repeats over.
func (td *tableData) hEdge(above, below, c int) cborder {
	n := len(td.rows)
	ca, cb := td.cellAt(above, c), td.cellAt(below, c)
	if ca != nil && ca == cb {
		return cborder{}
	}
	var r resolver
	col := td.cols[c]
	if above < 0 {
		r.add(td.table.st.Border[css.Top], originTable)
		if col.group != nil {
			r.add(col.group.st.Border[css.Top], originColGroup)
		}
		if col.col != nil {
			r.add(col.col.st.Border[css.Top], originColumn)
		}
	} else {
		if ca != nil && ca.cell.row+ca.cell.rowSpan-1 == above {
			r.add(ca.st.Border[css.Bottom], originCell)
		}
		r.add(td.rows[above].box.st.Border[css.Bottom], originRow)
		if td.lastRow(above) {
			r.add(td.rows[above].group.st.Border[css.Bottom], originRowGroup)
		}
	}
	if below >= n {
		r.add(td.table.st.Border[css.Bottom], originTable)
		if col.group != nil {
			r.add(col.group.st.Border[css.Bottom], originColGroup)
		}
		if col.col != nil {
			r.add(col.col.st.Border[css.Bottom], originColumn)
		}
	} else if below >= 0 {
		if cb != nil && cb.cell.row == below {
			r.add(cb.st.Border[css.Top], originCell)
		}
		r.add(td.rows[below].box.st.Border[css.Top], originRow)
		if td.firstRow(below) {
			r.add(td.rows[below].group.st.Border[css.Top], originRowGroup)
		}
	}
	return r.result()
}

// vEdge resolves the collapsed border of row r on the grid line before
// column i, the column count for the last line (CSS 2.1, 17.6.2.1).
func (td *tableData) vEdge(row, i int) cborder {
	cl, cr := td.cellAt(row, i-1), td.cellAt(row, i)
	if cl != nil && cl == cr {
		return cborder{}
	}
	var r resolver
	rb, g := td.rows[row].box, td.rows[row].group
	if i == 0 {
		r.add(td.table.st.Border[css.Left], originTable)
		r.add(g.st.Border[css.Left], originRowGroup)
		r.add(rb.st.Border[css.Left], originRow)
	} else {
		if cl != nil && cl.cell.col+cl.cell.colSpan == i {
			r.add(cl.st.Border[css.Right], originCell)
		}
		if col := td.cols[i-1]; col.col != nil {
			r.add(col.col.st.Border[css.Right], originColumn)
		}
		if col := td.cols[i-1]; col.group != nil && td.lastCol(i-1) {
			r.add(col.group.st.Border[css.Right], originColGroup)
		}
	}
	if i == td.nCols {
		r.add(td.table.st.Border[css.Right], originTable)
		r.add(g.st.Border[css.Right], originRowGroup)
		r.add(rb.st.Border[css.Right], originRow)
	} else {
		if cr != nil && cr.cell.col == i {
			r.add(cr.st.Border[css.Left], originCell)
		}
		if col := td.cols[i]; col.col != nil {
			r.add(col.col.st.Border[css.Left], originColumn)
		}
		if col := td.cols[i]; col.group != nil && td.firstCol(i) {
			r.add(col.group.st.Border[css.Left], originColGroup)
		}
	}
	return r.result()
}

// resolveBorders resolves every collapsed border of the grid and the half
// widths every cell takes on each side (CSS 2.1, 17.6.2).
func (td *tableData) resolveBorders() {
	nr, nc := len(td.rows), td.nCols
	if nc == 0 {
		return
	}
	td.hl = make([]cborder, (nr+1)*nc)
	for j := 0; j <= nr; j++ {
		for c := range nc {
			td.hl[j*nc+c] = td.hEdge(j-1, j, c)
		}
	}
	td.vl = make([]cborder, nr*(nc+1))
	for r := range nr {
		for i := 0; i <= nc; i++ {
			td.vl[r*(nc+1)+i] = td.vEdge(r, i)
		}
	}
	for _, c := range td.cells {
		cd := c.cell
		var h [4]float64
		for x := cd.col; x < cd.col+cd.colSpan; x++ {
			h[css.Top] = max(h[css.Top], td.hl[cd.row*nc+x].w/2)
			h[css.Bottom] = max(h[css.Bottom], td.hl[(cd.row+cd.rowSpan)*nc+x].w/2)
		}
		for r := cd.row; r < cd.row+cd.rowSpan; r++ {
			h[css.Left] = max(h[css.Left], td.vl[r*(nc+1)+cd.col].w/2)
			h[css.Right] = max(h[css.Right], td.vl[r*(nc+1)+cd.col+cd.colSpan].w/2)
		}
		cd.half = h
	}
}

// tableHalves returns half of the table's outer collapsed borders, which
// are its border widths in the collapsing model (CSS 2.1, 17.6.2): the
// first row's for the left and right, the widest of the first row's top
// and the last row's bottom.
func (td *tableData) tableHalves() [4]float64 {
	var h [4]float64
	nr, nc := len(td.rows), td.nCols
	if nr == 0 || nc == 0 {
		for s := range 4 {
			if bs := td.table.st.Border[s].Style; bs != css.BorderNone && bs != css.BorderHidden {
				h[s] = max(clamp(td.table.st.Border[s].Width), 0) / 2
			}
		}
		return h
	}
	h[css.Left] = td.vl[0].w / 2
	h[css.Right] = td.vl[nc].w / 2
	for c := range nc {
		h[css.Top] = max(h[css.Top], td.hl[c].w/2)
		h[css.Bottom] = max(h[css.Bottom], td.hl[nr*nc+c].w/2)
	}
	return h
}
