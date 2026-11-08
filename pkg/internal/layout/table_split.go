package layout

import (
	"cmp"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// breakCand is a place a cell's content may break across pages: the top
// of a line box or of an in-flow block box (CSS Fragmentation 3, 4.1,
// class A and C break points). soft reports one that orphans, widows or
// break-before and break-after avoid argue against (4.3).
type breakCand struct {
	y    float64
	soft bool
}

// cellBreaks returns the break points inside the cell c, in flow order,
// at its tentative layout. A nested table breaks between its rows, as
// Writer splits a row whose cell holds a table taller than the page; a
// float, an inline block or a replaced box is monolithic here: nothing
// breaks inside it.
func cellBreaks(c *box) []breakCand {
	var out []breakCand
	collectBreaks(c, &out, 0)
	slices.SortStableFunc(out, func(a, b breakCand) int { return cmp.Compare(a.y, b.y) })
	return out
}

// collectBreaks adds the break points inside the block container b to out.
func collectBreaks(b *box, out *[]breakCand, depth int) {
	if depth > maxTreeDepth {
		return
	}
	n := len(b.lines)
	orphans, widows := max(b.st.Orphans, 1), max(b.st.Widows, 1)
	for i, l := range b.lines {
		soft := i > 0 && (i < orphans || n-i < widows)
		*out = append(*out, breakCand{y: l.y, soft: soft})
	}
	var prev *box
	for _, k := range b.kids {
		if k.inline || k.float != css.FloatNone || k.abs || !k.laidOut || (k.kind != kindBlock && k.kind != kindReplaced) {
			continue
		}
		soft := prev != nil && avoidsBetween(prev, k)
		*out = append(*out, breakCand{y: k.y, soft: soft})
		if k.kind == kindBlock && k.tpart == partNone && !avoidsInside(k) {
			collectBreaks(k, out, depth+1)
		}
		if k.kind == kindBlock && k.tpart == partWrapper && !avoidsInside(k) {
			nestedRowBreaks(k, out)
		}
		prev = k
	}
}

// insertStrut opens a gap of d in the box b at flow position cut: every
// line box and box at or below cut moves down by d, and every box that
// spans cut grows by d (CSS Fragmentation 3, 4.4: the content after a
// break continues on the next page).
func (b *box) insertStrut(cut, d float64, depth int) {
	if depth > maxTreeDepth {
		return
	}
	b.h += d
	for _, l := range b.lines {
		if l.y < cut-eps {
			continue
		}
		l.y += d
		for _, it := range l.items {
			if it.atomic != nil {
				it.atomic.shift(0, d)
			}
		}
	}
	if b.outside != nil && b.outside.baseline >= cut-eps {
		b.outside.baseline += d
	}
	for _, k := range b.kids {
		switch {
		case k.abs:
			if k.sy >= cut-eps {
				k.shift(0, d)
			}
		case k.inline && k.kind == kindInline:
			k.strutInline(cut, d)
		case k.inline:
			if (k.kind == kindText || k.kind == kindBreak) && k.anchorY >= cut-eps {
				k.anchorY += d
			}
		case k.kind == kindBlock || k.kind == kindReplaced:
			switch {
			case k.y >= cut-eps:
				k.shift(0, d)
			case k.y+k.borderH() > cut+eps && k.float == css.FloatNone && k.kind == kindBlock && (k.tpart == partNone || nestedGrid(k)):
				k.insertStrut(cut, d, depth+1)
			}
		case k.kind == kindText || k.kind == kindBreak:
			if k.anchorY >= cut-eps {
				k.anchorY += d
			}
		}
	}
}

// strutInline moves the anchors of the inline box b and of what it holds
// at or below cut down by d, the blocks inside it as [box.insertStrut]
// does.
func (b *box) strutInline(cut, d float64) {
	if b.anchorY >= cut-eps {
		b.anchorY += d
	}
	for _, k := range b.kids {
		switch {
		case k.kind == kindInline:
			k.strutInline(cut, d)
		case k.float != css.FloatNone || k.abs:
			if k.y >= cut-eps || k.abs && k.sy >= cut-eps {
				k.shift(0, d)
			}
		case k.kind == kindText || k.kind == kindBreak:
			if k.anchorY >= cut-eps {
				k.anchorY += d
			}
		}
	}
}

// splitCell is a cell of a row being split across pages, its positions
// relative to the row top as the struts already opened leave them.
type splitCell struct {
	c *box
	// cands are the break points, end the bottom of the content and
	// bottom the bottom of the border box.
	cands       []breakCand
	end, bottom float64
	// cut is where the current break cuts the cell, valid when has.
	cut float64
	has bool
	// tail and head are the room a cell that clones its decorations at a
	// break keeps for its bottom border and padding before a cut, and for
	// its top ones after it.
	tail, head float64
}

// splitCells returns the cells of row r of td to split, aligned at their
// top or on the row's baseline: a cell broken across pages does not
// align in the middle or at the bottom.
func (td *tableData) splitCells(r int) []*splitCell {
	var out []*splitCell
	for _, c := range td.cells {
		cd := c.cell
		if cd.row != r {
			continue
		}
		off := 0.0
		if vaKind(c.st) == css.Baseline && cd.hasBase && r < len(td.rowHasBase) && td.rowHasBase[r] {
			off = max(td.rowBase[r]-cd.base, 0)
		}
		cd.off = off
		sc := &splitCell{c: c, end: cd.contentEnd - c.y + off, bottom: cd.natH + off}
		if c.st.BoxDecorationBreak == css.DecorationCloneAll || td.collapse && td.table.st.BoxDecorationBreak == css.DecorationCloneAll {
			// Writer draws a split cell's borders whole on every page,
			// its content clear of them.
			k := 1.0
			if td.collapse {
				// A collapsed border's cell holds half of it; the cloned
				// line lies whole on the piece's side of the cut.
				k = 2
			}
			sc.tail = k*c.bd[css.Bottom] + c.pd[css.Bottom]
			sc.head = k*c.bd[css.Top] + c.pd[css.Top]
		}
		for _, k := range cellBreaks(c) {
			sc.cands = append(sc.cands, breakCand{y: k.y - c.y + off, soft: k.soft})
		}
		out = append(out, sc)
	}
	return out
}

// chooseCuts picks where each cell of cells breaks for a page piece of the
// row from top to limit, relative to the row top: the last break point
// whose content above fits and that orphans and widows allow (CSS 2.1,
// 13.3.3). A cell whose content fits does not break. moveAll lets a cell
// with no such break point move its whole content to the next piece;
// without it, as at the top of a page, the last break point that fits
// serves even against orphans and widows. It reports whether some cell keeps content
// in the piece; when none does, no cell breaks.
func chooseCuts(cells []*splitCell, top, limit float64, moveAll bool) bool {
	kept := false
	for _, sc := range cells {
		sc.has = false
		if sc.end <= limit+metricSlack {
			continue
		}
		limit := limit - sc.tail
		first := -1
		for i, k := range sc.cands {
			if k.y >= top-eps {
				first = i
				break
			}
		}
		if first < 0 {
			continue
		}
		hard, soft := -1, -1
		for i := first + 1; i < len(sc.cands); i++ {
			k := sc.cands[i]
			if k.y > limit+metricSlack {
				break
			}
			if k.y <= sc.cands[first].y+eps {
				continue
			}
			soft = i
			if !k.soft {
				hard = i
			}
		}
		switch {
		case hard >= 0:
			sc.cut, sc.has = sc.cands[hard].y, true
			kept = true
		case moveAll:
			// Orphans and widows rather move the content whole.
			sc.cut, sc.has = sc.cands[first].y, true
		case soft >= 0:
			sc.cut, sc.has = sc.cands[soft].y, true
			kept = true
		}
	}
	if !kept {
		for _, sc := range cells {
			sc.has = false
		}
	}
	return kept
}

// open opens the strut that moves the content of sc from its cut to at,
// relative to the row top, and records it on the cell.
func (sc *splitCell) open(at float64) {
	d := at + sc.head - sc.cut
	if d <= 0 {
		return
	}
	cd := sc.c.cell
	cd.struts = append(cd.struts, cellStrut{at: sc.cut, d: d})
	for i := range sc.cands {
		if sc.cands[i].y >= sc.cut-eps {
			sc.cands[i].y += d
		}
	}
	sc.end += d
	sc.bottom += d
}

// nestedRowBreaks adds the tops of the rows of the table whose wrapper is
// w, but its first, to out: a nested table breaks between its rows. A row
// that avoids a break before it argues against the break.
func nestedRowBreaks(w *box, out *[]breakCand) {
	for _, t := range w.kids {
		if t.tpart != partTable || t.tdata == nil {
			continue
		}
		for i, r := range t.tdata.rows {
			if i == 0 || r.box == nil || !r.box.laidOut {
				continue
			}
			prev := t.tdata.rows[i-1].box
			soft := prev != nil && avoidsBetween(prev, r.box) || avoidsInside(t)
			*out = append(*out, breakCand{y: r.box.y, soft: soft})
		}
	}
}

// nestedGrid reports a box of a nested table that a strut opened between
// two of its rows grows: its wrapper, its table box, a row group. Its rows
// below the strut move down whole.
func nestedGrid(b *box) bool {
	return b.tpart == partWrapper || b.tpart == partTable || b.tpart == partRowGroup
}

// breakSpans moves the content of the cells spanning into row r, whose
// rows break before r at the page cut cut, from the line the cut runs
// through down to at, where row r starts on the next page: a row-spanning
// cell's lines continue on the next page as a split row's do, rather
// than straddling the cut, as Writer breaks them. The cells then sit at
// the top of their rows.
func (td *tableData) breakSpans(r int, cut, at float64) {
	for _, c := range td.cells {
		cd := c.cell
		if cd.rowSpan < 2 || cd.row >= r || cd.row+cd.rowSpan <= r {
			continue
		}
		top := td.rows[cd.row].box.y
		limit := cut - top
		end := cd.contentEnd - c.y
		var cands []float64
		for _, k := range cellBreaks(c) {
			cands = append(cands, k.y-c.y)
		}
		for _, st := range cd.struts {
			end += st.d
			for i := range cands {
				if cands[i] >= st.at-eps {
					cands[i] += st.d
				}
			}
		}
		if end <= limit+metricSlack {
			continue
		}
		brk := -1.0
		for i, y := range cands {
			// The first line whose bottom, the next one's top, passes
			// the cut moves.
			bottom := end
			if i+1 < len(cands) {
				bottom = cands[i+1]
			}
			if y > eps && bottom > limit+metricSlack {
				brk = y
				break
			}
		}
		if brk < 0 {
			continue
		}
		if d := at - top - brk; d > 0 {
			cd.split, cd.off = true, 0
			cd.struts = append(cd.struts, cellStrut{at: brk, d: d})
		}
	}
}
