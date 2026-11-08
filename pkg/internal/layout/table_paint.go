package layout

import (
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
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
func (p *painter) cellRect(c *box, pr placedRow) (compose.Rect, bool, bool) {
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
// column group, column, row group and row and its own, then the borders:
// the table's and the cells' in the separated model, the resolved grid in
// the collapsing one.
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
			if !td.cellSkipped(c) {
				p.cellBackgrounds(td, c, pr)
			}
		}
	})
	if td.collapse {
		p.collapsedBorders(td)
	} else {
		if !t.hidden {
			p.border(t, t.st, r, radii, t.bd, [4]bool{true, true, true, true})
		}
		p.eachRow(td, false, func(pr placedRow, cells []*box) {
			for _, c := range cells {
				if td.cellSkipped(c) || c.hidden || c.st.EmptyCells == css.Hide && cellEmpty(c) {
					continue
				}
				cr, top, bottom := p.cellRect(c, pr)
				if c.st.BoxDecorationBreak == css.DecorationCloneAll {
					// Writer draws a split cell's borders whole on every
					// page (css.DecorationCloneAll).
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
		})
	}
	for _, c := range td.cells {
		p.linkBox(c)
	}
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
			p.partBackground(b, r)
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
	p.background(c, c.st, r, radii, bd, pd)
}

// partBackground paints the background of the row, row group, column or
// column group b where the cell rectangle clip shows it: its image placed
// on b's own box (CSS 2.1, 17.5.1).
func (p *painter) partBackground(b *box, clip compose.Rect) {
	st := b.st
	if !hasBackground(st) || clip.Width <= 0 || clip.Height <= 0 {
		return
	}
	if c, ok := colorOf(st.BackgroundColor); ok {
		p.emit(b, false, &compose.Path{Ops: rectOps(clip), Fill: &compose.Fill{Color: c}}, clip.Y, clip.Y+clip.Height, -1)
	}
	if st.BackgroundImage == "" && st.BackgroundGradient == nil {
		return
	}
	g := &ditem{group: true, opacity: 1, clip: &compose.Path{Ops: rectOps(clip), Fill: &compose.Fill{}}}
	if st.BackgroundGradient != nil {
		for _, pa := range p.e.gradientTiles(st, p.borderRect(b), clip, clip.Y) {
			g.kids = append(g.kids, &ditem{frag: pa, y0: pa.Ops[0].P[0].Y, y1: pa.Ops[2].P[0].Y, page: p.mono})
		}
	} else {
		pic := p.e.picture(st.BackgroundImage, clip.Y)
		if pic == nil {
			return
		}
		for _, t := range p.e.tiles(st, pic, p.borderRect(b), clip, clip.Y) {
			g.kids = append(g.kids, &ditem{frag: &compose.Image{Rect: t, Source: pic.Source}, y0: t.Y, y1: t.Y + t.Height, page: p.mono})
		}
	}
	if len(g.kids) == 0 {
		return
	}
	c := p.sync(p.chain(b, false))
	c.kids = append(c.kids, g)
}

// tableContent paints the content of every cell of the table box t, row
// by row, copies included: each cell's flow as a block's (CSS 2.1,
// Appendix E), its own decorations already painted by the table.
func (p *painter) tableContent(t *box) {
	td := t.tdata
	p.eachRow(td, true, func(pr placedRow, cells []*box) {
		for _, c := range cells {
			if !td.cellSkipped(c) {
				p.cellFlow(c)
			}
		}
	})
}

// cellFlow paints what the cell c holds: its in-flow blocks'
// decorations, its floats, its line boxes and its blocks' content, then
// its stacking contexts.
func (p *painter) cellFlow(c *box) {
	var blocks, floats, contexts []*box
	p.collect(c, &blocks, &floats, &contexts)
	for _, k := range blocks {
		if !p.artifact {
			p.linkBox(k)
		}
		p.decorations(k)
	}
	for _, f := range floats {
		p.context(f)
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
func (p *painter) collapsedBorders(td *tableData) {
	nc, nr := td.nCols, len(td.rows)
	if nc == 0 || nr == 0 {
		return
	}
	t := td.table
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
		// vertical below, 3 the horizontal right, and the half sizes of
		// the corner square.
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
			return win, max(c[0].w, c[2].w) / 2, max(c[1].w, c[3].w) / 2
		}
		for k := 0; k <= n; k++ {
			for c := range nc {
				b := h[k][c]
				if !b.visible() {
					continue
				}
				x0, x1 := x(c), x(c+1)
				if win, hw, _ := corner(k, c); win == 3 {
					x0 -= hw
				} else {
					x0 += hw
				}
				if win, hw, _ := corner(k, c+1); win == 1 {
					x1 += hw
				} else {
					x1 -= hw
				}
				y := lineY(k)
				if clone && cutLine(seq, k) {
					// The page cut runs along the line: the cloned border
					// lies on the piece's side of it.
					if k == n {
						y -= b.w / 2
					} else {
						y += b.w / 2
					}
				}
				p.segment(t, b, compose.Rect{X: x0, Y: y - b.w/2, Width: x1 - x0, Height: b.w}, true)
			}
		}
		for k := range n {
			for i := 0; i <= nc; i++ {
				b := v[k][i]
				if !b.visible() {
					continue
				}
				y0, y1 := lineY(k), td.bottom(seq[k])+p.dy
				if win, _, hh := corner(k, i); win == 2 {
					y0 -= hh
				} else {
					y0 += hh
				}
				if win, _, hh := corner(k+1, i); win == 0 {
					y1 += hh
				} else {
					y1 -= hh
				}
				cx := x(i)
				p.segment(t, b, compose.Rect{X: cx - b.w/2, Y: y0, Width: b.w, Height: y1 - y0}, false)
			}
		}
		p.mono = mono
	}
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
func (p *painter) segment(t *box, b cborder, r compose.Rect, horizontal bool) {
	if r.Width <= 0 || r.Height <= 0 {
		return
	}
	col, ok := colorOf(b.color)
	if !ok {
		return
	}
	fill := func(rr compose.Rect, c compose.Color) {
		if rr.Width > 0 && rr.Height > 0 {
			p.emit(t, false, &compose.Path{Ops: rectOps(rr), Fill: &compose.Fill{Color: c}}, rr.Y, rr.Y+rr.Height, -1)
		}
	}
	// split returns the first and second part of r across the border's
	// thickness, the first f of it.
	split := func(f float64) (compose.Rect, compose.Rect) {
		a, z := r, r
		if horizontal {
			a.Height = r.Height * f
			z.Y, z.Height = r.Y+a.Height, r.Height-a.Height
		} else {
			a.Width = r.Width * f
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
		dark, light := shades(col)
		if b.style == css.Ridge || b.style == css.Inset {
			dark, light = light, dark
		}
		a, z := split(0.5)
		fill(a, dark)
		fill(z, light)
	case css.Dashed, css.Dotted:
		w := b.w
		a := compose.Point{X: r.X, Y: r.Y + r.Height/2}
		z := compose.Point{X: r.X + r.Width, Y: a.Y}
		if !horizontal {
			a = compose.Point{X: r.X + r.Width/2, Y: r.Y}
			z = compose.Point{X: a.X, Y: r.Y + r.Height}
		}
		stroke := &compose.Stroke{Color: col, Width: w, Dash: []float64{3 * w, 3 * w}}
		if b.style == css.Dotted {
			stroke.Dash = []float64{w, w}
		}
		p.emit(t, false, &compose.Path{Ops: []compose.PathOp{
			{Kind: compose.MoveTo, P: [3]compose.Point{a}},
			{Kind: compose.LineTo, P: [3]compose.Point{z}},
		}, Stroke: stroke}, r.Y, r.Y+r.Height, -1)
	case css.Solid, css.BorderNone, css.BorderHidden:
		if t.st.StrokeBorders {
			// Writer strokes a collapsed cell border along its middle
			// (css.Office.StrokeBorders).
			a := compose.Point{X: r.X, Y: r.Y + r.Height/2}
			z := compose.Point{X: r.X + r.Width, Y: a.Y}
			w := r.Height
			if !horizontal {
				a = compose.Point{X: r.X + r.Width/2, Y: r.Y}
				z = compose.Point{X: a.X, Y: r.Y + r.Height}
				w = r.Width
			}
			p.emit(t, false, strokedLine(a, z, w, col), r.Y, r.Y+r.Height, -1)
			return
		}
		fill(r, col)
	}
}
