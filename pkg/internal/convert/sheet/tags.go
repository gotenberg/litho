package sheet

import (
	"slices"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// tagTable rebuilds the structure of a page's Table element, the first
// Table among root's kids, as a regular table: one TR per row the page
// prints that holds content, the repeated rows first, and in each one
// cell per column it
// prints, the repeated columns first, a merge one cell with its spans
// and nothing for the cells it covers (ISO 14289-1, 7.2; ISO 14289-2,
// 8.2.5.26; ISO 32000-1, 14.8.5.7, Table 349). A run of empty cells in a
// row is one TD spanning them: a reader finds the next cell in its
// column all the same, and a sparse sheet's page does not carry an
// element per empty cell, which doubles the file of a large one. table is the layout tree
// the engine built root from; frags and links are what it laid out,
// whose parents move to the cells that hold them.
//
// Each printed page is its own Table: a sheet cut into pages across and
// down prints columns on one page that the next does not, so one Table
// over the pages could not keep its rows regular. The rows and columns a
// sheet repeats on every page (its print titles) are header cells, TH
// with a column scope for a repeated row and a row scope for a repeated
// column (Table 349): they are what the sheet's author asked to title
// every page with. A spreadsheet has no other header notion, so every
// other cell is a TD.
//
// A cell whose text overflows into the page from a column the page does
// not print belongs to the printed cell of its row nearest that column.
// The row and column headings Calc prints are artifacts: the application
// draws them, like line numbers, and they are not the sheet's content
// (ISO 32000-1, 14.8.2.2.2).
//
// The structure the engine built stays when the page holds more than
// [maxPageCells] cells or when the cells it tagged do not match the
// layout tree.
func (b *pageBuilder) tagTable(table *layout.Node, root *compose.StructElem, frags []compose.Fragment, links []compose.Link) {
	if root == nil {
		return
	}
	var tbl *compose.StructElem
	for _, k := range root.Kids {
		if k.Role == "Table" {
			tbl = k
			break
		}
	}
	if tbl == nil {
		return
	}
	var nodes []*layout.Node
	collectCells(table, &nodes)
	var elems []*compose.StructElem
	collectCellElems(tbl, &elems)
	if len(nodes) != len(elems) {
		return
	}
	rows, cols := b.printedRows(), b.printedCols()
	if len(rows) == 0 || len(cols) == 0 || len(rows) > maxPageCells/len(cols) {
		return
	}
	g := b.g
	key := func(r, c int) [2]int {
		if tl, ok := g.covered[[2]int{r, c}]; ok {
			return tl
		}
		return [2]int{r, c}
	}
	ps := &g.sh.Page
	inRows := func(rg *Range, r int) bool { return rg != nil && r >= rg.FirstRow && r <= rg.LastRow }
	inCols := func(rg *Range, c int) bool { return rg != nil && c >= rg.FirstColumn && c <= rg.LastColumn }
	printed := map[[2]int]bool{}
	for _, r := range rows {
		for _, c := range cols {
			printed[key(r, c)] = true
		}
	}
	// target is the cell each tagged node's content belongs to; filled
	// marks the cells that hold content.
	target := make([][2]int, len(nodes))
	filled := map[[2]int]bool{}
	for i, n := range nodes {
		at := b.cellKeys[n]
		k := key(at[0], at[1])
		if !printed[k] {
			k = nearestCell(key, rows, cols, at)
		}
		target[i] = k
		filled[k] = true
	}
	// A row that holds no content, no header cell and no part of a merge
	// is left out: it tells a reader nothing, and the rows kept stay
	// regular. So is a row whose every column continues a merge of the
	// row kept before it: it would be a TR without a cell, which counts
	// no column (ISO 14289-2, 8.2.5.26), and the merges spanning it span
	// one row fewer instead.
	kept := rows[:0:0]
	for _, r := range rows {
		keep := inRows(ps.RepeatRows, r)
		for _, c := range cols {
			if k := key(r, c); keep || filled[k] || !g.single(k, r, c) || inCols(ps.RepeatColumns, c) {
				keep = true
				break
			}
		}
		if keep && len(kept) > 0 {
			prev := kept[len(kept)-1]
			keep = slices.ContainsFunc(cols, func(c int) bool { return key(r, c) != key(prev, c) })
		}
		if keep {
			kept = append(kept, r)
		}
	}
	rows = kept
	cellAt := map[[2]int]*compose.StructElem{}
	taken := make([]bool, len(rows)*len(cols))
	trs := make([]*compose.StructElem, len(rows))
	for i := range rows {
		trs[i] = &compose.StructElem{Role: "TR"}
	}
	for i, r := range rows {
		for j, c := range cols {
			if taken[i*len(cols)+j] {
				continue
			}
			k := key(r, c)
			colHead, rowHead := inRows(ps.RepeatRows, r), inCols(ps.RepeatColumns, c)
			cs := 1
			if g.single(k, r, c) && !filled[k] && !colHead && !rowHead {
				// A run of empty cells is one cell spanning them: the
				// row keeps its columns, and the page does not carry an
				// element per empty cell.
				for j+cs < len(cols) && !taken[i*len(cols)+j+cs] {
					c2 := cols[j+cs]
					if k2 := key(r, c2); !g.single(k2, r, c2) || filled[k2] || inCols(ps.RepeatColumns, c2) {
						break
					}
					cs++
				}
			} else {
				for j+cs < len(cols) && !taken[i*len(cols)+j+cs] && key(r, cols[j+cs]) == k {
					cs++
				}
			}
			rs := 1
			for i+rs < len(rows) && key(rows[i+rs], c) == k {
				rs++
			}
			for y := i; y < i+rs; y++ {
				for x := j; x < j+cs; x++ {
					taken[y*len(cols)+x] = true
				}
			}
			el := &compose.StructElem{Role: "TD"}
			switch {
			case colHead && rowHead:
				el.Role, el.Scope = "TH", compose.ScopeBoth
			case colHead:
				el.Role, el.Scope = "TH", compose.ScopeColumn
			case rowHead:
				el.Role, el.Scope = "TH", compose.ScopeRow
			}
			if cs > 1 {
				el.ColSpan = cs
			}
			if rs > 1 {
				el.RowSpan = rs
			}
			trs[i].Kids = append(trs[i].Kids, el)
			if _, ok := cellAt[k]; !ok {
				cellAt[k] = el
			}
		}
	}
	remap := map[*compose.StructElem]*compose.StructElem{tbl: nil}
	for i := range nodes {
		el := cellAt[target[i]]
		remap[elems[i]] = el
		if el != nil {
			el.Kids = append(el.Kids, elems[i].Kids...)
		}
	}
	reparent(frags, remap)
	for i := range links {
		if p, ok := remap[links[i].Parent]; ok {
			links[i].Parent = p
		}
	}
	tbl.Kids = trs
}

// single reports that the cell at r, c, whose key is k, is no part of a
// merge.
func (g *grid) single(k [2]int, r, c int) bool {
	if k != [2]int{r, c} {
		return false
	}
	_, merged := g.merges[k]
	return !merged
}

// nearestCell returns the cell of the printed row of at whose column is
// nearest at's, for text overflowing into the page from a column it does
// not print, and at itself when the page does not print its row.
func nearestCell(key func(r, c int) [2]int, rows, cols []int, at [2]int) [2]int {
	if !slices.Contains(rows, at[0]) {
		return at
	}
	best, dist := -1, 0
	for _, c := range cols {
		d := c - at[1]
		if d < 0 {
			d = -d
		}
		if best < 0 || d < dist {
			best, dist = c, d
		}
	}
	return key(at[0], best)
}

// printedRows returns the rows the page prints, in order: the repeated
// rows, then the band's, hidden rows left out.
func (b *pageBuilder) printedRows() []int {
	var out []int
	add := func(bd band) {
		for r := bd.first; r <= bd.last && len(out) < maxPageCells; r++ {
			if !b.g.rowHidden(r) {
				out = append(out, r)
			}
		}
	}
	if b.p.repRows != nil {
		add(*b.p.repRows)
	}
	add(b.p.rows)
	return out
}

// printedCols returns the columns the page prints, in order: the repeated
// columns, then the band's, hidden columns left out.
func (b *pageBuilder) printedCols() []int {
	var out []int
	add := func(bd band) {
		for c := bd.first; c <= bd.last && len(out) < maxPageCells; c++ {
			if !b.g.colHidden(c) {
				out = append(out, c)
			}
		}
	}
	if b.p.repCols != nil {
		add(*b.p.repCols)
	}
	add(b.p.cols)
	return out
}

// collectCells appends the nodes of Role TD under n, in document order.
func collectCells(n *layout.Node, out *[]*layout.Node) {
	for _, k := range n.Children {
		if k.Role == "TD" {
			*out = append(*out, k)
			continue
		}
		collectCells(k, out)
	}
}

// collectCellElems appends the TD elements under the rows of e, in order.
func collectCellElems(e *compose.StructElem, out *[]*compose.StructElem) {
	for _, k := range e.Kids {
		switch k.Role {
		case "TD":
			*out = append(*out, k)
		case "TR":
			collectCellElems(k, out)
		}
	}
}

// reparent moves the fragments of frags, and those of their groups,
// whose parent remap names to the element it maps it to.
func reparent(frags []compose.Fragment, remap map[*compose.StructElem]*compose.StructElem) {
	stack := append([]compose.Fragment(nil), frags...)
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch v := f.(type) {
		case *compose.Group:
			stack = append(stack, v.Content...)
		case *compose.Text:
			if p, ok := remap[v.Parent]; ok {
				v.Parent = p
			}
		case *compose.Image:
			if p, ok := remap[v.Parent]; ok {
				v.Parent = p
			}
		case *compose.Path:
			if p, ok := remap[v.Parent]; ok {
				v.Parent = p
			}
		}
	}
}
