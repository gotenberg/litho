package sheet

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// maxPageCells caps the cells one page lays out. Anti-DoS guard, not
// spec-mandated: an A3 page at the smallest zoom shows about 200 rows by
// 80 columns of default cells, 16,000 cells.
const maxPageCells = 1 << 17

// pageBuilder builds one page.
type pageBuilder struct {
	*builder
	g          *grid
	p          *pagePlan
	cellBudget int
	// fills, borderPaths and gridPaths are the page's vector content in
	// the coordinates of its cells, under and over the text.
	fills, borderPaths, gridPaths []compose.Fragment
	// circles are the red ellipses around the cells marked invalid.
	circles []compose.Fragment
	// dataW and dataH are the size of the page's cells, headings
	// included, in sheet points.
	dataW, dataH float64
}

// style returns the base style of a node.
func (b *builder) style() *css.Style {
	st := css.Initial()
	st.Display = css.Block
	st.FontFamilies = []string{defaultFamily}
	st.FontSize = defaultFontSize
	st.FontKerning = css.KerningNone
	return st
}

// regions returns the page's regions: the repeated rows over the repeated
// columns, the repeated rows over the band, the band's rows beside the
// repeated columns, the band, after the headings.
func (b *pageBuilder) layoutRegions() []*region {
	g := b.g
	p := b.p
	x0, y0 := 0.0, 0.0
	if g.sh.Page.Headings {
		x0, y0 = headingWidth, headingHeight
	}
	repW, repH := 0.0, 0.0
	if p.repCols != nil {
		repW = g.spanW(p.repCols.first, p.repCols.last)
	}
	if p.repRows != nil {
		repH = g.spanH(p.repRows.first, p.repRows.last)
	}
	bandW := g.spanW(p.cols.first, p.cols.last)
	bandH := g.spanH(p.rows.first, p.rows.last)
	var out []*region
	if p.repRows != nil && p.repCols != nil {
		out = append(out, &region{rows: *p.repRows, cols: *p.repCols, x0: x0, y0: y0, w: repW, h: repH})
	}
	if p.repRows != nil {
		out = append(out, &region{rows: *p.repRows, cols: p.cols, x0: x0 + repW, y0: y0, w: bandW, h: repH})
	}
	if p.repCols != nil {
		out = append(out, &region{rows: p.rows, cols: *p.repCols, x0: x0, y0: y0 + repH, w: repW, h: bandH})
	}
	out = append(out, &region{rows: p.rows, cols: p.cols, x0: x0 + repW, y0: y0 + repH, w: bandW, h: bandH})
	b.dataW, b.dataH = x0+repW+bandW, y0+repH+bandH
	return out
}

// build lays the page's cells out and returns their fragments in sheet
// points from the top-left corner of the cells, with their links.
func (b *pageBuilder) build() ([]compose.Fragment, []compose.Link, *compose.StructElem, error) {
	b.cellBudget = maxPageCells
	regs := b.layoutRegions()
	root := b.style()
	root.Position = css.Relative
	root.Width = css.Pts(b.dataW)
	root.Height = css.Pts(b.dataH)
	table := &layout.Node{Kind: layout.Element, Style: root, Role: "Table", Lang: b.lang}
	for _, rg := range regs {
		table.Children = append(table.Children, b.region(rg))
	}
	if b.g.sh.Page.Headings {
		table.Children = append(table.Children, b.headings(regs)...)
	}
	if b.cellBudget <= 0 {
		b.warn(CodeTooManyCells, fmt.Sprintf("a page of sheet %s holds more than %d cells; the rest are left out", quote(b.g.sh.Name), maxPageCells))
	}
	res, err := layout.Layout(b.ctx, table, b.fonts, layout.Options{
		Page:     layout.PageBox{Width: max(b.dataW, 1), Height: max(b.dataH, 1)},
		Lang:     b.lang,
		MaxPages: maxSubPages,
		Logger:   b.opts.Logger,
	})
	if err != nil {
		if errors.Is(err, layout.ErrTooManyPages) {
			return nil, nil, nil, fmt.Errorf("laying out a page of sheet %s: %w", quote(b.g.sh.Name), err)
		}
		return nil, nil, nil, fmt.Errorf("laying out a page of sheet %s: %w", quote(b.g.sh.Name), err)
	}
	b.warnings = append(b.warnings, res.Warnings...)
	var frags []compose.Fragment
	frags = append(frags, b.fills...)
	frags = append(frags, b.borderPaths...)
	var links []compose.Link
	if len(res.Pages) > 0 {
		frags = append(frags, res.Pages[0].Content...)
		links = res.Pages[0].Links
	}
	frags = append(frags, b.gridPaths...)
	frags = append(frags, b.circles...)
	frags = append(frags, b.drawings(regs)...)
	return frags, links, res.Structure, nil
}

// maxSubPages caps the pages of a layout call that lays out one page.
// Anti-DoS guard, not spec-mandated: the cells fit one page by
// construction.
const maxSubPages = 4

// region lays out one region: its fills and borders as paths, its text in
// row nodes inside a clipping box.
func (b *pageBuilder) region(rg *region) *layout.Node {
	g := b.g
	bs := newBorders()
	rows := map[int]*layout.Node{}
	var rowOrder []int
	box := b.style()
	box.Position = css.Absolute
	box.Left, box.Top = css.Pts(rg.x0), css.Pts(rg.y0)
	box.Width, box.Height = css.Pts(max(rg.w, 0.01)), css.Pts(max(rg.h, 0.01))
	box.Overflow = css.OverflowHidden
	node := &layout.Node{Kind: layout.Element, Style: box}
	clipFills := len(b.fills)
	spans := map[int][][2]float64{}
	add := func(u *unit) {
		n, cx0, cx1 := b.textNode(rg, u)
		if n == nil || cx1 <= rg.x0+epsilon || cx0 >= rg.x0+rg.w-epsilon {
			return
		}
		if cx1-cx0 > u.w+epsilon {
			spans[u.r] = append(spans[u.r], [2]float64{cx0, cx1})
		}
		row, ok := rows[u.r]
		if !ok {
			st := b.style()
			st.Position = css.Absolute
			st.Left = css.Pts(0)
			st.Top = css.Pts(u.y - rg.y0)
			st.Width = css.Pts(max(rg.w, 0.01))
			st.Height = css.Pts(max(g.rowHeight(u.r), 0.01))
			row = &layout.Node{Kind: layout.Element, Style: st, Role: "TR"}
			rows[u.r] = row
			rowOrder = append(rowOrder, u.r)
		}
		row.Children = append(row.Children, n)
	}
	b.units(rg, func(u *unit) {
		if c := u.st.Fill.Color; c.A > 0 {
			b.fills = append(b.fills, &compose.Path{Ops: rectOps(u.x, u.y, u.w, u.h), Fill: &compose.Fill{Color: colorOf(c)}})
		}
		b.drawDataBar(u)
		b.drawIcon(u)
		if u.cell != nil && u.cell.MarkedInvalid {
			b.circles = append(b.circles, invalidCircle(u))
		}
		b.unitBorders(bs, rg, u)
		if _, only := b.g.iconAt(u.r, u.c, u.cell); !only {
			add(u)
		}
	})
	// Text from cells left and right of the region that overflows into
	// it.
	for r := rg.rows.first; r <= rg.rows.last; r++ {
		if g.rowHidden(r) {
			continue
		}
		for _, side := range []int{-1, 1} {
			c := rg.cols.first - 1
			if side > 0 {
				c = rg.cols.last + 1
			}
			for i := 0; i < maxOverflowScan && c >= 0 && c < maxColumns; i, c = i+1, c+side {
				if !b.emptyAt(r, c) {
					break
				}
			}
			cell := g.cell(r, c)
			if !hasContent(cell) || c < 0 || c >= maxColumns {
				continue
			}
			if _, merged := g.merges[[2]int{r, c}]; merged {
				continue
			}
			u := &unit{r: r, c: c, lastR: r, lastC: c, cell: cell, st: g.style(r, c, cell)}
			u.x, u.y = rg.x(g, c), rg.y(g, r)
			u.w, u.h = g.colWidth(c), g.rowHeight(r)
			if u.w <= 0 {
				continue
			}
			add(u)
		}
	}
	b.borderPaths = append(b.borderPaths, bs.paths()...)
	// Fills and borders of a region stay in it.
	if len(b.fills) > clipFills {
		grp := &compose.Group{
			Clip:    &compose.Path{Ops: rectOps(rg.x0, rg.y0, rg.w, rg.h), Fill: &compose.Fill{}},
			Opacity: 1, Transform: compose.Identity, Content: append([]compose.Fragment(nil), b.fills[clipFills:]...),
		}
		b.fills = append(b.fills[:clipFills], grp)
	}
	if g.sh.Page.GridLines {
		b.grid(rg, spans)
	}
	for _, r := range rowOrder {
		node.Children = append(node.Children, rows[r])
	}
	return node
}

// grid draws the grid lines of a region: every row and column edge but
// those inside a merge or crossed by overflowing text, and its frame.
func (b *pageBuilder) grid(rg *region, spans map[int][][2]float64) {
	g := b.g
	stroke := &compose.Stroke{Color: compose.Black, Width: gridWidth}
	add := func(x1, y1, x2, y2 float64) {
		b.gridPaths = append(b.gridPaths, &compose.Path{Ops: lineOps(x1, y1, x2, y2), Stroke: stroke})
	}
	inMerge := func(r, c int, vertical bool) bool {
		if tl, ok := g.covered[[2]int{r, c}]; ok {
			if vertical {
				return c > tl[1]
			}
			return r > tl[0]
		}
		return false
	}
	// Horizontal lines, at the top of every row and the bottom of the
	// last.
	for r := rg.rows.first; r <= rg.rows.last+1; r++ {
		if r <= rg.rows.last && g.rowHidden(r) {
			continue
		}
		y := rg.y(g, r)
		x := rg.x0
		for c := rg.cols.first; c <= rg.cols.last; c++ {
			w := g.colWidth(c)
			if w == 0 {
				continue
			}
			if r > rg.rows.last || !inMerge(r, c, false) {
				add(x, y, x+w, y)
			}
			x += w
		}
	}
	for c := rg.cols.first; c <= rg.cols.last+1; c++ {
		if c <= rg.cols.last && g.colHidden(c) {
			continue
		}
		x := rg.x(g, c)
		y := rg.y0
		for r := rg.rows.first; r <= rg.rows.last; r++ {
			h := g.rowHeight(r)
			if h == 0 {
				continue
			}
			skip := c <= rg.cols.last && inMerge(r, c, true)
			for _, s := range spans[r] {
				if x > s[0]+epsilon && x < s[1]-epsilon {
					skip = true
				}
			}
			if !skip {
				add(x, y, x, y+h)
			}
			y += h
		}
	}
}

// headings draws the row numbers and column letters of a page whose
// sheet prints them: a frame per row and column in black, the label
// centred in Calc's default font (ScPrintFunc::PrintColHdr,
// PrintRowHdr).
func (b *pageBuilder) headings(regs []*region) []*layout.Node {
	g := b.g
	var out []*layout.Node
	stroke := &compose.Stroke{Color: compose.Black, Width: gridWidth}
	label := func(x, y, w, h float64, s string) {
		b.gridPaths = append(b.gridPaths, &compose.Path{Ops: rectOps(x, y, w, h), Stroke: stroke})
		st := b.style()
		st.Position = css.Absolute
		st.Left, st.Top, st.Width, st.Height = css.Pts(x), css.Pts(y), css.Pts(max(w, 0.01)), css.Pts(max(h, 0.01))
		st.Display = css.Flex
		st.FlexDirection = css.FlexColumn
		st.JustifyContent = css.BoxCenter
		st.Overflow = css.OverflowHidden
		p := b.style()
		p.FontSize = headingFontSize
		p.TextAlign = css.AlignCenter
		p.WhiteSpace = css.Pre
		out = append(out, &layout.Node{Kind: layout.Element, Style: st, Children: []*layout.Node{
			{Kind: layout.Element, Style: p, Children: []*layout.Node{{Kind: layout.Text, Style: p, Text: s}}},
		}})
	}
	for _, rg := range regs {
		if rg.y0 == headingHeight {
			for c := rg.cols.first; c <= rg.cols.last; c++ {
				if w := g.colWidth(c); w > 0 {
					label(rg.x(g, c), 0, w, headingHeight, columnName(c))
				}
			}
		}
		if rg.x0 == headingWidth {
			for r := rg.rows.first; r <= rg.rows.last; r++ {
				if h := g.rowHeight(r); h > 0 {
					label(0, rg.y(g, r), headingWidth, h, strconv.Itoa(r+1))
				}
			}
		}
	}
	return out
}

// columnName returns the letters of column c: A to Z, AA and so on.
func columnName(c int) string {
	var b []byte
	c++
	for c > 0 {
		c--
		b = append([]byte{byte('A' + c%26)}, b...)
		c /= 26
	}
	return string(b)
}

// The detective's circle around a cell marked invalid, in points: grown
// 0.25 cm left and right and 0.07 cm up and down from the cell, stroked
// red 0.55 mm wide (ScDetectiveFunc::DrawCircle).
const (
	circleGrowX = 0.25 / 2.54 * 72
	circleGrowY = 0.07 / 2.54 * 72
	circleWidth = 0.055 / 2.54 * 72
)

// invalidCircle returns the red ellipse Calc's detective draws around the
// cell or merge of u, its value refused by its validity rule.
//
// # Corpus evidence
//
//   - ods: sc/deleteCirclesInRowAndCol.ods prints a 1.559-point red
//     ellipse 78.18 by 16.78 points around each 64 by 12.78 point cell
//     marked invalid.
func invalidCircle(u *unit) compose.Fragment {
	x0, y0 := u.x-circleGrowX, u.y-circleGrowY
	rx, ry := u.w/2+circleGrowX, u.h/2+circleGrowY
	ops := ellipse(x0+rx, y0+ry, rx, ry)
	return &compose.Path{Ops: ops, Stroke: &compose.Stroke{Color: compose.Color{R: 1, A: 1}, Width: circleWidth, Join: compose.RoundJoin}}
}
