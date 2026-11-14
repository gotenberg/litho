package layout

import (
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// cellStarts returns, for every row of td, the index of its first cell in
// td.cells, and one past the last row: the cells are in row order.
func cellStarts(td *tableData) []int {
	nr := len(td.rows)
	out := make([]int, nr+1)
	i := 0
	for r := range nr {
		out[r] = i
		for i < len(td.cells) && td.cells[i].cell.row == r {
			i++
		}
	}
	out[nr] = len(td.cells)
	return out
}

// cellSkipped reports a cell of a collapsed row or column, which paints
// nothing (CSS 2.1, 17.5.5).
func (td *tableData) cellSkipped(c *box) bool {
	cd := c.cell
	return td.rowCollapsed(cd.row) || td.colCollapsed(cd.col)
}

// cellEmpty reports a cell without content: no line box, no in-flow or
// floating box (CSS 2.1, 17.6.1.1).
func cellEmpty(c *box) bool {
	if len(c.lines) > 0 {
		return false
	}
	return c.inlineKids || len(c.kids) == 0
}

// eachRow calls fn for every row of td as it was placed, natural or a copy
// of the header or footer, with the painter moved to it: a copy is
// offset, paints on its own page and paints as an artifact (ISO 32000-1,
// 14.8.2.2.2, pagination artifacts). A row paints on the page of its top,
// or across the pages it spans when it overflows them. With content set,
// a row split across pages is met once, at its first piece, and paints on
// every page it reaches.
func (p *painter) eachRow(td *tableData, content bool, fn func(pr placedRow, cells []*box)) {
	starts := cellStarts(td)
	for _, seq := range td.seqs {
		for _, pr := range seq {
			if p.e.err != nil {
				return
			}
			if content && pr.piece && !pr.first {
				continue
			}
			dy, mono, art := p.dy, p.mono, p.artifact
			p.dy += pr.dy
			cells := td.cells[starts[pr.r]:starts[pr.r+1]]
			if p.mono < 0 && (!content || !pr.piece) {
				// A row paints across the pages its row-spanning cells
				// reach too, when the rows they span break between pages.
				bottom := td.bottom(pr)
				if !pr.piece && pr.dy == 0 {
					for _, c := range cells {
						if c.cell.rowSpan > 1 {
							bottom = max(bottom, c.y+c.borderH())
						}
					}
				}
				p.mono = p.e.spanPage(td.top(pr), bottom)
			}
			p.artifact = art || pr.dy != 0
			fn(pr, cells)
			p.dy, p.mono, p.artifact = dy, mono, art
		}
	}
}

// spanPage returns the page the flow extent from top to bottom lies on,
// -1 when it spans pages.
func (e *engine) spanPage(top, bottom float64) int {
	a := e.pageOf(top + eps)
	if e.pageOf(max(bottom-metricSlack, top+eps)) != a {
		return -1
	}
	return a
}

// cellRect returns the border box of cell c in the placed row pr: a piece
// of a split row shows the slice of the cell within it, and reports which
// of its top and bottom edges the slice keeps (box-decoration-break:
// slice, CSS Fragmentation 3, 5.4).
func (p *painter) cellRect(c *box, pr placedRow) (typeset.Rect, bool, bool) {
	r := p.borderRect(c)
	if !pr.piece {
		return r, true, true
	}
	y0, y1 := max(r.Y, pr.y0+p.dy), min(r.Y+r.Height, pr.y1+p.dy)
	r.Y, r.Height = y0, max(y1-y0, 0)
	return r, !pr.cutTop, !pr.cutBottom
}

// tableDecorations paints the layers of the table box t (CSS 2.1, 17.5.1):
// the table's background, then under every cell the backgrounds of its
// column group, column, row group and row and its own, then in the
// separated model the table's and the cells' borders. The resolved grid
// of the collapsing model paints with the content ([painter.tableContent]).
func (p *painter) tableDecorations(t *box) {
	td := t.tdata
	r := p.borderRect(t)
	var radii [4][2]float64
	if !td.collapse {
		radii = radiiOf(t, r.Width, r.Height)
	}
	if !t.hidden {
		p.background(t, t.st, r, radii, t.bd, t.pd)
	}
	p.eachRow(td, false, func(pr placedRow, cells []*box) {
		for _, c := range cells {
			if !td.cellSkipped(c) && partLayer(c) == nil {
				p.cellBackgrounds(td, c, pr)
			}
		}
	})
	if !td.collapse {
		if !t.hidden {
			p.border(t, t.st, r, radii, t.bd, [4]bool{true, true, true, true})
		}
		p.eachRow(td, false, func(pr placedRow, cells []*box) {
			for _, c := range cells {
				if td.cellSkipped(c) || partLayer(c) != nil {
					continue
				}
				p.cellBorder(c, pr)
			}
		})
	}
	for _, c := range td.cells {
		p.linkBox(c)
	}
}

// cellBorder paints the borders of cell c in the placed row pr, in the
// separated borders model.
func (p *painter) cellBorder(c *box, pr placedRow) {
	if c.hidden || c.st.EmptyCells == css.Hide && cellEmpty(c) {
		return
	}
	cr, top, bottom := p.cellRect(c, pr)
	if c.st.BoxDecorationBreak == css.DecorationCloneAll {
		// Writer draws a split cell's borders whole on every page
		// (css.DecorationCloneAll).
		top, bottom = true, true
	}
	bd := c.bd
	var radii [4][2]float64
	if top && bottom {
		radii = radiiOf(c, cr.Width, cr.Height)
	}
	if !top {
		bd[css.Top] = 0
	}
	if !bottom {
		bd[css.Bottom] = 0
	}
	p.border(c, c.st, cr, radii, bd, [4]bool{top, true, bottom, true})
}

// partLayer returns the positioned row group, row or cell the cell c
// paints with, nil for a cell its table paints.
func partLayer(c *box) *box {
	for a := c; a != nil && a.tpart.internal(); a = a.parent {
		if a.layered() {
			return a
		}
	}
	return nil
}

// tablePart paints the positioned row group, row or cell b from its
// stacking context: the backgrounds, separated borders and flows of the
// cells that paint with it, each moved by the relative offsets of the
// parts between it and b (CSS Position 3, 3.3). The collapsed borders
// stay with the table.
func (p *painter) tablePart(b *box) {
	t := b.parent
	for t != nil && t.tpart != partTable {
		t = t.parent
	}
	if t == nil || t.tdata == nil {
		return
	}
	td := t.tdata
	each := func(content bool, fn func(c *box, pr placedRow)) {
		p.eachRow(td, content, func(pr placedRow, cells []*box) {
			for _, c := range cells {
				if td.cellSkipped(c) || partLayer(c) != b {
					continue
				}
				dx, dy := 0.0, 0.0
				for a := c; a != b; a = a.parent {
					ox, oy := offset(a)
					dx += ox
					dy += oy
				}
				p.dx += dx
				p.dy += dy
				fn(c, pr)
				p.dx -= dx
				p.dy -= dy
			}
		})
	}
	each(false, func(c *box, pr placedRow) { p.cellBackgrounds(td, c, pr) })
	// The part's outline and those of the parts it paints inside it come
	// after its content, as a block's do ([painter.outlines]).
	defer p.outline(b)
	defer p.outlines(tableOutlines(b, nil, 0))
	if td.collapse {
		each(true, func(c *box, _ placedRow) { p.cellBlocks(c) })
		p.collapsedBorders(td, b)
		each(true, func(c *box, _ placedRow) { p.cellForeground(c) })
		return
	}
	each(false, p.cellBorder)
	each(true, func(c *box, _ placedRow) { p.cellFlow(c) })
}

// cellBackgrounds paints, on the border box of cell c, the backgrounds of
// its column group, column, row group and row, then its own. In the
// separated model an empty cell with empty-cells: hide paints none (CSS
// 2.1, 17.6.1.1).
func (p *painter) cellBackgrounds(td *tableData, c *box, pr placedRow) {
	if !td.collapse && c.st.EmptyCells == css.Hide && cellEmpty(c) {
		return
	}
	cd := c.cell
	r, top, bottom := p.cellRect(c, pr)
	if r.Height <= 0 {
		return
	}
	col := td.cols[cd.col]
	row := td.rows[cd.row]
	for _, b := range []*box{col.group, col.col, row.group, row.box} {
		if b != nil && !b.hidden {
			p.partBackground(b, c, r)
		}
	}
	if c.hidden {
		return
	}
	var radii [4][2]float64
	if !td.collapse && top && bottom {
		radii = radiiOf(c, r.Width, r.Height)
	}
	bd, pd := c.bd, c.pd
	if !top {
		bd[css.Top], pd[css.Top] = 0, 0
	}
	if !bottom {
		bd[css.Bottom], pd[css.Bottom] = 0, 0
	}
	// A cell casts its box shadows as any box does (CSS Backgrounds 3,
	// 7.1): the outer ones under its background, the inset ones over it.
	p.shadows(c, c.st, r, radii, bd, false)
	p.background(c, c.st, r, radii, bd, pd)
	p.shadows(c, c.st, r, radii, bd, true)
}

// partBackground paints the background of the row, row group, column or
// column group b where the rectangle clip of the cell c shows it: its
// image placed on b's own box (CSS 2.1, 17.5.1). Under background-clip:
// text it paints under the glyphs of c alone (CSS Backgrounds 4, 2.1).
func (p *painter) partBackground(b, c *box, clip typeset.Rect) {
	st := b.st
	if !hasBackground(st) || clip.Width <= 0 || clip.Height <= 0 {
		return
	}
	clipPath := &typeset.Path{Ops: rectOps(clip), Fill: &typeset.Fill{}}
	if clipToText(st) {
		if clipPath = p.textClip(c); clipPath == nil {
			return
		}
	}
	g := &ditem{group: true, fx: &groupFx{opacity: 1, clip: clipPath}}
	if col, ok := colorOf(st.BackgroundColor); ok {
		if clipToText(st) {
			g.kids = append(g.kids, &ditem{frag: &typeset.Path{Ops: rectOps(clip), Fill: &typeset.Fill{Color: col}}, y0: clip.Y, y1: clip.Y + clip.Height, page: p.mono})
		} else {
			p.emit(b, false, &typeset.Path{Ops: rectOps(clip), Fill: &typeset.Fill{Color: col}}, clip.Y, clip.Y+clip.Height, -1)
		}
	}
	for _, sl := range p.attachedSlices(b, st, p.borderRect(b), clip) {
		if st.BackgroundGradient != nil {
			for _, pa := range p.e.gradientTiles(st, sl.area, sl.clip, clip.Y) {
				g.kids = append(g.kids, &ditem{frag: pa, y0: pa.Ops[0].P[0].Y, y1: pa.Ops[2].P[0].Y, page: p.mono})
			}
			continue
		}
		pic := p.e.picture(st.BackgroundImage, clip.Y)
		if pic == nil {
			break
		}
		for _, t := range p.e.tiles(st, pic, sl.area, sl.clip, clip.Y) {
			g.kids = append(g.kids, &ditem{frag: &typeset.Image{Rect: t, Source: pic.Source}, y0: t.Y, y1: t.Y + t.Height, page: p.mono})
		}
	}
	if len(g.kids) == 0 {
		return
	}
	into := p.sync(p.chain(b, false))
	into.kids = append(into.kids, g)
}

// tableContent paints the content of every cell of the table box t, row
// by row, copies included: each cell's flow as a block's (CSS 2.1,
// Appendix E), its own decorations already painted by the table. In the
// collapsing model the resolved borders paint between the cells' block
// backgrounds and the rest of their content.
//
// # Corpus evidence
//
//   - wpt: css-tables/collapsed-border-paint-phase-002 draws its
//     reference in Chromium 156.
//   - probe, html: Chromium 156 paints a collapsed border over the
//     background of a block that overflows its cell into it, and under the
//     text and the float that overflow it.
func (p *painter) tableContent(t *box) {
	td := t.tdata
	each := func(fn func(c *box)) {
		p.eachRow(td, true, func(pr placedRow, cells []*box) {
			for _, c := range cells {
				if !td.cellSkipped(c) && partLayer(c) == nil {
					fn(c)
				}
			}
		})
	}
	if td.collapse {
		each(p.cellBlocks)
		p.collapsedBorders(td, nil)
		each(p.cellForeground)
		return
	}
	each(p.cellFlow)
}

// cellFlow paints what the cell c holds: its in-flow blocks'
// decorations, its floats, its line boxes and its blocks' content, then
// its stacking contexts.
func (p *painter) cellFlow(c *box) {
	p.cellBlocks(c)
	p.cellForeground(c)
}

// cellBlocks paints the decorations of the in-flow blocks of the cell c.
func (p *painter) cellBlocks(c *box) {
	var blocks, floats, contexts []*box
	p.collect(c, &blocks, &floats, &contexts)
	for _, k := range blocks {
		if !p.artifact {
			p.linkBox(k)
		}
		p.decorations(k)
	}
}

// cellForeground paints the rest of what the cell c holds: its floats,
// its line boxes and its blocks' content, then its stacking contexts.
func (p *painter) cellForeground(c *box) {
	var blocks, floats, contexts []*box
	p.collect(c, &blocks, &floats, &contexts)
	for _, f := range floats {
		p.floating(f)
	}
	p.content(c)
	for _, k := range blocks {
		p.content(k)
	}
	for _, k := range contexts {
		p.context(k)
	}
}

// collapsedBorders paints the resolved borders of td's grid in the
// collapsing model (CSS 2.1, 17.6.2), every run of rows on its own: each
// border centred on its grid line, and at each crossing the winning
// border of those that meet there paints the corner (17.6.2.1).
// Of the borders of a positioned part's cells, owner nil paints none, and
// a positioned part paints those it is [segmentLayer] of. A table box
// that is not visible paints none, whatever its cells' visibility: the
// borders belong to the table in this model (CSS 2.1, 17.6.2 and 11.2).
//
// # Corpus evidence
//
//   - wpt: css-tables/visibility-hidden-collapsed-borders draws its
//     reference in Chromium 156.
//   - probe, html: Chromium 156 paints no collapsed border of a hidden
//     table holding a visible cell, and paints those of a hidden cell
//     and of a hidden row in a visible table.
func (p *painter) collapsedBorders(td *tableData, owner *box) {
	nc, nr := td.nCols, len(td.rows)
	t := td.table
	if nc == 0 || nr == 0 || t.st.Visibility != css.Visible {
		return
	}
	// A table whose borders clone at a page cut draws the grid line a
	// split row's piece ends or starts with, as Writer draws a split
	// cell's borders whole on every page (css.DecorationCloneAll).
	clone := t.st.BoxDecorationBreak == css.DecorationCloneAll || td.wrapper != nil && td.wrapper.st.BoxDecorationBreak == css.DecorationCloneAll
	x := func(i int) float64 { return t.contentX() + td.colX[i] + p.dx }
	for _, seq := range td.seqs {
		n := len(seq)
		if n == 0 || p.e.err != nil {
			continue
		}
		mono := p.mono
		if p.mono < 0 {
			p.mono = p.e.spanPage(td.top(seq[0]), td.bottom(seq[n-1]))
		}
		h := make([][]cborder, n+1)
		v := make([][]cborder, n)
		for k := 0; k <= n; k++ {
			var above, below int
			switch k {
			case 0:
				below = seq[0].r
				above = below - 1
			case n:
				above = seq[n-1].r
				below = above + 1
			default:
				above, below = seq[k-1].r, seq[k].r
			}
			h[k] = make([]cborder, nc)
			for c := range nc {
				switch {
				case cutLine(seq, k) && !clone:
				case k > 0 && k < n && seq[k].cutTop:
					// A copy of the header over a piece of a split row: the
					// piece's cut top adds nothing.
					h[k][c] = td.hEdge(above, noRow, c)
				case k > 0 && k < n && seq[k-1].cutBottom:
					h[k][c] = td.hEdge(noRow, below, c)
				default:
					h[k][c] = td.hEdge(above, below, c)
				}
			}
			if k < n {
				v[k] = make([]cborder, nc+1)
				for i := 0; i <= nc; i++ {
					v[k][i] = td.vEdge(seq[k].r, i)
				}
			}
		}
		lineY := func(k int) float64 {
			if k < n {
				return td.top(seq[k]) + p.dy
			}
			return td.bottom(seq[n-1]) + p.dy
		}
		// corner returns which border wins the crossing of line k and grid
		// line i, 0 the vertical above, 1 the horizontal left, 2 the
		// vertical below, 3 the horizontal right, in its two low bits,
		// with a bit 1<<(j+2) for each border j that ties with a dashed or
		// dotted winner, and the half sizes of the corner square.
		corner := func(k, i int) (int, float64, float64) {
			var c [4]cborder
			if k > 0 {
				c[0] = v[k-1][i]
			}
			if i > 0 {
				c[1] = h[k][i-1]
			}
			if k < n {
				c[2] = v[k][i]
			}
			if i < nc {
				c[3] = h[k][i]
			}
			win := 0
			for j := 1; j < 4; j++ {
				if c[j].beats(c[win]) {
					win = j
				}
			}
			if p.blink() && (c[win].style == css.Dashed || c[win].style == css.Dotted) {
				// Blink's tied edges both take the corner
				// (ComputeEdgeJoints): a dashed or dotted run reaches
				// over it when no edge there beats it, so its pattern
				// runs from corner to corner as a box's side does.
				first, w := win, c[win]
				for j := range 4 {
					if j != first && !w.beats(c[j]) && c[j].style == w.style && c[j].w == w.w {
						win |= 1 << (j + 2)
					}
				}
			}
			return win, max(c[0].w, c[2].w) / 2, max(c[1].w, c[3].w) / 2
		}
		// takes reports whether border j of the crossing a corner returns
		// covers it.
		takes := func(win, j int) bool { return win&3 == j || win&(1<<(j+2)) != 0 }
		for k := 0; k <= n; k++ {
			for c := range nc {
				b := h[k][c]
				if !b.visible() || td.segmentLayer(seq, k-1, c, k, c) != owner {
					continue
				}
				x0, x1 := x(c), x(c+1)
				if win, hw, _ := corner(k, c); takes(win, 3) {
					x0 -= hw
				} else {
					x0 += hw
				}
				if win, hw, _ := corner(k, c+1); takes(win, 1) {
					x1 += hw
				} else {
					x1 -= hw
				}
				y := lineY(k)
				if clone && cutLine(seq, k) {
					// The page cut runs along the line: the cloned border
					// lies on the piece's side of it.
					if k == n {
						y -= float64(b.w / 2)
					} else {
						y += float64(b.w / 2)
					}
				}
				p.segment(t, b, typeset.Rect{X: x0, Y: y - float64(b.w/2), Width: x1 - x0, Height: b.w}, true)
			}
		}
		for k := range n {
			for i := 0; i <= nc; i++ {
				b := v[k][i]
				if !b.visible() || td.segmentLayer(seq, k, i-1, k, i) != owner {
					continue
				}
				y0, y1 := lineY(k), td.bottom(seq[k])+p.dy
				if win, _, hh := corner(k, i); takes(win, 2) {
					y0 -= hh
				} else {
					y0 += hh
				}
				if win, _, hh := corner(k+1, i); takes(win, 0) {
					y1 += hh
				} else {
					y1 -= hh
				}
				cx := x(i)
				p.segment(t, b, typeset.Rect{X: cx - float64(b.w/2), Y: y0, Width: b.w, Height: y1 - y0}, false)
			}
		}
		p.mono = mono
	}
}

// segmentLayer returns the positioned part that paints a collapsed border
// segment between the slots (k0, c0) and (k1, c1) of the run of rows seq,
// k an index into seq and c a column: the part of the later of the two
// cells, as the later paints over the earlier, nil when neither is
// positioned.
func (td *tableData) segmentLayer(seq []placedRow, k0, c0, k1, c1 int) *box {
	at := func(k, c int) *box {
		if k < 0 || k >= len(seq) || c < 0 || c >= td.nCols {
			return nil
		}
		if cell := td.cellAt(seq[k].r, c); cell != nil {
			return partLayer(cell)
		}
		return nil
	}
	if l := at(k1, c1); l != nil {
		return l
	}
	return at(k0, c0)
}

// cutLine reports the grid line k of the run of rows seq that a page break
// cuts: the top of a piece of a split row not under a copy of the header,
// the bottom of one not over a copy of the footer. It draws no border
// (box-decoration-break: slice), unless the table clones its decorations
// (css.DecorationCloneAll).
func cutLine(seq []placedRow, k int) bool {
	n := len(seq)
	if k < n && seq[k].cutTop && (k == 0 || seq[k-1].dy == 0) {
		return true
	}
	return k > 0 && seq[k-1].cutBottom && (k == n || seq[k].dy == 0)
}

// segment paints one collapsed border b on the rectangle r, a horizontal
// or a vertical run of the grid. In the collapsing model inset draws as
// ridge and outset as groove (CSS 2.1, 17.6.2).
func (p *painter) segment(t *box, b cborder, r typeset.Rect, horizontal bool) {
	if r.Width <= 0 || r.Height <= 0 {
		return
	}
	col, ok := colorOf(b.color)
	if !ok {
		return
	}
	fill := func(rr typeset.Rect, c typeset.Color) {
		if rr.Width > 0 && rr.Height > 0 {
			p.emit(t, false, &typeset.Path{Ops: rectOps(rr), Fill: &typeset.Fill{Color: c}}, rr.Y, rr.Y+rr.Height, -1)
		}
	}
	// split returns the first and second part of r across the border's
	// thickness, the first f of it.
	split := func(f float64) (typeset.Rect, typeset.Rect) {
		a, z := r, r
		if horizontal {
			a.Height = float64(r.Height * f)
			z.Y, z.Height = r.Y+a.Height, r.Height-a.Height
		} else {
			a.Width = float64(r.Width * f)
			z.X, z.Width = r.X+a.Width, r.Width-a.Width
		}
		return a, z
	}
	switch b.style {
	case css.Double:
		a, _ := split(1.0 / 3)
		_, z := split(2.0 / 3)
		fill(a, col)
		fill(z, col)
	case css.Groove, css.Ridge, css.Inset, css.Outset:
		dark, light := shades(col, p.blink())
		if b.style == css.Ridge || b.style == css.Inset {
			dark, light = light, dark
		}
		a, z := split(0.5)
		fill(a, dark)
		fill(z, light)
	case css.Dashed, css.Dotted:
		w := b.w
		if p.blink() {
			// Blink draws a collapsed border's run as it draws a box's
			// side ([blinkDashedSide]).
			side := css.Top
			if !horizontal {
				side = css.Left
			}
			g := &ditem{}
			blinkDashedSide(g, side, b.style, col, r, w)
			for _, k := range g.kids {
				p.emit(t, false, k.frag, r.Y, r.Y+r.Height, -1)
			}
			return
		}
		a := typeset.Point{X: r.X, Y: r.Y + float64(r.Height/2)}
		z := typeset.Point{X: r.X + r.Width, Y: a.Y}
		if !horizontal {
			a = typeset.Point{X: r.X + float64(r.Width/2), Y: r.Y}
			z = typeset.Point{X: a.X, Y: r.Y + r.Height}
		}
		stroke := &typeset.Stroke{Color: col, Width: w, Dash: []float64{3 * w, 3 * w}}
		if b.style == css.Dotted {
			stroke.Dash = []float64{w, w}
		}
		p.emit(t, false, &typeset.Path{Ops: []typeset.PathOp{
			{Kind: typeset.MoveTo, P: [3]typeset.Point{a}},
			{Kind: typeset.LineTo, P: [3]typeset.Point{z}},
		}, Stroke: stroke}, r.Y, r.Y+r.Height, -1)
	case css.Solid, css.BorderNone, css.BorderHidden:
		if t.st.StrokeBorders {
			// Writer strokes a collapsed cell border along its middle
			// (css.Office.StrokeBorders).
			a := typeset.Point{X: r.X, Y: r.Y + float64(r.Height/2)}
			z := typeset.Point{X: r.X + r.Width, Y: a.Y}
			w := r.Height
			if !horizontal {
				a = typeset.Point{X: r.X + float64(r.Width/2), Y: r.Y}
				z = typeset.Point{X: a.X, Y: r.Y + r.Height}
				w = r.Width
			}
			p.emit(t, false, strokedLine(a, z, w, col), r.Y, r.Y+r.Height, -1)
			return
		}
		fill(r, col)
	}
}
