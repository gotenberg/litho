package xlsx

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// The sides of a differential format's border, in its element order
// (ECMA-376 Part 1, 18.8.4): the outer sides, then the inner lines.
const (
	dxfLeft = iota
	dxfRight
	dxfTop
	dxfBottom
	dxfVertical
	dxfHorizontal
)

// dxf is a differential format a table style element applies (ECMA-376
// Part 1, 18.8.14): only what it sets.
type dxf struct {
	bold, italic *bool
	color        *css.Color
	fill         *css.Color
	border       [6]*sheet.Border
}

// tableStyle is a table style (ECMA-376 Part 1, 18.8.40): the format of
// each element by its type, and the stripe sizes.
type tableStyle struct {
	elements             map[string]*dxf
	rowStripe, colStripe [2]int
}

// readDxf reads a differential format (ECMA-376 Part 1, 18.8.14): its
// font's weight, slant and colour, its fill's colour (a solid fill's
// background colour, else its pattern colour, as LibreOffice reads a
// differential fill) and its border lines.
func (r *reader) readDxf(e *ooxml.Element) *dxf {
	d := &dxf{}
	if f := e.Child(nsMain, "font"); f != nil {
		if b := f.Child(nsMain, "b"); b != nil {
			v := ooxml.Toggle(b)
			d.bold = &v
		}
		if i := f.Child(nsMain, "i"); i != nil {
			v := ooxml.Toggle(i)
			d.italic = &v
		}
		if c, ok := r.color(f.Child(nsMain, "color")); ok {
			d.color = &c
		}
	}
	if pf := e.Child(nsMain, "fill").Child(nsMain, "patternFill"); pf != nil && attr(pf, "patternType") != "none" {
		c, ok := r.color(pf.Child(nsMain, "bgColor"))
		if !ok {
			c, ok = r.color(pf.Child(nsMain, "fgColor"))
		}
		if ok {
			d.fill = &c
		}
	}
	for _, s := range e.Child(nsMain, "border").Kids() {
		side := -1
		switch s.Local {
		case "left", "start":
			side = dxfLeft
		case "right", "end":
			side = dxfRight
		case "top":
			side = dxfTop
		case "bottom":
			side = dxfBottom
		case "vertical":
			side = dxfVertical
		case "horizontal":
			side = dxfHorizontal
		}
		if side < 0 || s.Space != nsMain {
			continue
		}
		if b := r.borderLine(s); b.Line != sheet.LineNone {
			d.border[side] = &b
		}
	}
	return d
}

// parseTableStyle reads a tableStyle element against the differential
// formats dxfs, whose ids it names from base (1 in the presets of the
// standard's annex, 0 in a workbook).
func (r *reader) parseTableStyle(ts *ooxml.Element, dxfs []*ooxml.Element, base int) *tableStyle {
	out := &tableStyle{elements: map[string]*dxf{}, rowStripe: [2]int{1, 1}, colStripe: [2]int{1, 1}}
	for _, el := range ts.Kids() {
		if !el.Is(nsMain, "tableStyleElement") {
			continue
		}
		typ := attr(el, "type")
		size := max(1, min(intAttr(el, "size", 1), 9))
		switch typ {
		case "firstRowStripe":
			out.rowStripe[0] = size
		case "secondRowStripe":
			out.rowStripe[1] = size
		case "firstColumnStripe":
			out.colStripe[0] = size
		case "secondColumnStripe":
			out.colStripe[1] = size
		}
		id := intAttr(el, "dxfId", -1) - base
		if id < 0 || id >= len(dxfs) {
			continue
		}
		out.elements[typ] = r.readDxf(dxfs[id])
	}
	return out
}

// tableStyleNamed returns the table style name: the workbook's own (its
// styles part's tableStyles), else the preset of that name, nil for none.
func (r *reader) tableStyleNamed(name string) *tableStyle {
	if ts, ok := r.styles.tables[name]; ok {
		return ts
	}
	xml, ok := presetTableStyles[name]
	if !ok {
		return nil
	}
	root, err := ooxml.Parse(context.Background(), []byte(`<s xmlns="`+nsMain+`">`+xml+`</s>`), nil)
	if err != nil {
		return nil
	}
	var ts *tableStyle
	if el := root.Child(nsMain, "tableStyles").Child(nsMain, "tableStyle"); el != nil {
		ts = r.parseTableStyle(el, root.Child(nsMain, "dxfs").Kids(), 1)
	}
	r.styles.tables[name] = ts
	return ts
}

// tableInfo is a table's area and the parts of its style it shows
// (ECMA-376 Part 1, 18.5.1.2 table, 18.5.1.5 tableStyleInfo).
type tableInfo struct {
	area                   sheet.Range
	header, total          bool
	firstCol, lastCol      bool
	rowStripes, colStripes bool
}

// maxTableCells caps the cells a table style adds to a sheet. Anti-DoS
// guard, not spec-mandated: a table area can name a whole column; past
// the cap, only the cells the sheet holds are styled.
const maxTableCells = 1 << 16

// styleTable applies the table style ts to the cells of table t, over
// their own style: its fill where the cell has none, its font weight,
// slant and colour where the cell keeps the Normal style's, its borders
// where the cell draws none. Each attribute comes from the first element
// of the table that sets it, in LibreOffice's order: the header row's
// last and first cells, the header and total rows, the first and last
// columns, the row and column stripes outside the total row, the whole
// table.
func (w *sheetReader) styleTable(ts *tableStyle, t tableInfo) {
	r := w.r
	area := t.area
	added := 0
	cache := map[styleKey]*sheet.Style{}
	normal := r.normal()
	for row := area.FirstRow; row <= area.LastRow; row++ {
		for col := area.FirstColumn; col <= area.LastColumn; col++ {
			cell := w.cellRef(row, col, added < maxTableCells)
			if cell == nil {
				continue
			}
			if cell.Style == nil {
				added++
			}
			key := styleKey{base: w.styleAt(cell, row, col), overlay: ts.overlay(t, row, col)}
			if key.overlay == (overlay{}) {
				continue
			}
			st, ok := cache[key]
			if !ok {
				st = key.apply(normal)
				cache[key] = st
			}
			cell.Style = st
		}
	}
}

// overlay is what a table style gives one cell.
type overlay struct {
	bold, italic, hasBold, hasItalic bool
	color, fill                      css.Color
	hasColor, hasFill                bool
	border                           [4]sheet.Border
}

// styleKey caches the styles a table makes.
type styleKey struct {
	base    *sheet.Style
	overlay overlay
}

// apply returns the base style with the overlay, as styleTable says.
func (k styleKey) apply(normal sheet.Style) *sheet.Style {
	st := normal
	if k.base != nil {
		st = *k.base
	}
	st.Font.Families = append([]string(nil), st.Font.Families...)
	o := k.overlay
	if o.hasFill && st.Fill.Color.A == 0 {
		st.Fill.Color = o.fill
	}
	if o.hasColor && st.Font.Color == normal.Font.Color {
		st.Font.Color = o.color
	}
	if o.hasBold && st.Font.Bold == normal.Font.Bold {
		st.Font.Bold = o.bold
	}
	if o.hasItalic && st.Font.Italic == normal.Font.Italic {
		st.Font.Italic = o.italic
	}
	for i, b := range o.border {
		if b.Line != sheet.LineNone && st.Border[i].Line == sheet.LineNone {
			st.Border[i] = b
		}
	}
	return &st
}

// overlay returns what the table style gives the cell at row and col of
// table t.
func (ts *tableStyle) overlay(t tableInfo, row, col int) overlay {
	a := t.area
	header := t.header && row == a.FirstRow
	total := t.total && row == a.LastRow
	bodyRow := row - a.FirstRow
	if t.header {
		bodyRow--
	}
	var chain []*dxf
	add := func(on bool, name string) {
		if d := ts.elements[name]; on && d != nil {
			chain = append(chain, d)
		}
	}
	add(header && col == a.LastColumn, "lastHeaderCell")
	add(header && col == a.FirstColumn, "firstHeaderCell")
	add(header, "headerRow")
	add(total, "totalRow")
	add(t.firstCol && col == a.FirstColumn, "firstColumn")
	add(t.lastCol && col == a.LastColumn, "lastColumn")
	if !total && !header {
		if t.rowStripes && bodyRow >= 0 {
			first := bodyRow%(ts.rowStripe[0]+ts.rowStripe[1]) < ts.rowStripe[0]
			add(!first, "secondRowStripe")
			add(first, "firstRowStripe")
		}
		if t.colStripes {
			rel := col - a.FirstColumn
			first := rel%(ts.colStripe[0]+ts.colStripe[1]) < ts.colStripe[0]
			add(!first, "secondColumnStripe")
			add(first, "firstColumnStripe")
		}
	}
	add(true, "wholeTable")
	var o overlay
	for _, d := range chain {
		if !o.hasBold && d.bold != nil {
			o.bold, o.hasBold = *d.bold, true
		}
		if !o.hasItalic && d.italic != nil {
			o.italic, o.hasItalic = *d.italic, true
		}
		if !o.hasColor && d.color != nil {
			o.color, o.hasColor = *d.color, true
		}
		if !o.hasFill && d.fill != nil {
			o.fill, o.hasFill = *d.fill, true
		}
	}
	// A border side comes from the first element that draws it there: an
	// element's outer side on the edge of the region it covers, its inner
	// line between two of its cells. The rows and the stripes cover the
	// table's width, the columns its height, the whole table both.
	sides := [4]struct {
		edge         bool
		outer, inner int
		vertical     bool
	}{
		css.Top:    {row == a.FirstRow, dxfTop, dxfHorizontal, false},
		css.Bottom: {row == a.LastRow, dxfBottom, dxfHorizontal, false},
		css.Left:   {col == a.FirstColumn, dxfLeft, dxfVertical, true},
		css.Right:  {col == a.LastColumn, dxfRight, dxfVertical, true},
	}
	for side, s := range sides {
		for _, d := range chain {
			region := ts.region(d)
			outer := s.edge || region == regionCell || s.vertical && region == regionColumn || !s.vertical && region == regionRow
			b := d.border[s.inner]
			if outer {
				b = d.border[s.outer]
			}
			if b != nil {
				o.border[side] = *b
				break
			}
		}
	}
	return o
}

// The regions a table style element covers.
const (
	regionTable = iota
	regionRow
	regionColumn
	regionCell
)

// region returns the region the element of format d covers: a row (the
// header and total rows, a row stripe) across the table, a column (the
// first and last columns, a column stripe) down it, one header cell, or
// the whole table.
func (ts *tableStyle) region(d *dxf) int {
	for name, reg := range map[string]int{
		"headerRow": regionRow, "totalRow": regionRow, "firstRowStripe": regionRow, "secondRowStripe": regionRow,
		"firstColumn": regionColumn, "lastColumn": regionColumn, "firstColumnStripe": regionColumn, "secondColumnStripe": regionColumn,
		"firstHeaderCell": regionCell, "lastHeaderCell": regionCell,
	} {
		if ts.elements[name] == d {
			return reg
		}
	}
	return regionTable
}

// cellRef returns the cell at row and col, added with its row when the
// sheet lacks it and add is set, nil otherwise.
func (w *sheetReader) cellRef(row, col int, add bool) *sheet.Cell {
	if c, ok := w.cellAt(row, col); ok || !add {
		return c
	}
	rw := w.row(row)
	rw.Cells = dedupCells(append(rw.Cells, sheet.Cell{Column: col}))
	c, _ := w.cellAt(row, col)
	return c
}

// cellAt returns the cell at row and col, false when the sheet lacks it.
func (w *sheetReader) cellAt(row, col int) (*sheet.Cell, bool) {
	at, ok := w.rowAt[row]
	if !ok {
		return nil, false
	}
	cells := w.sh.Rows[at].Cells
	for i := range cells {
		if cells[i].Column == col {
			return &cells[i], true
		}
	}
	return nil, false
}

// styleAt returns the style the cell draws with: its own, else its row's,
// else its column's, nil for the default.
func (w *sheetReader) styleAt(c *sheet.Cell, row, col int) *sheet.Style {
	if c.Style != nil {
		return c.Style
	}
	if at, ok := w.rowAt[row]; ok && w.sh.Rows[at].Style != nil {
		return w.sh.Rows[at].Style
	}
	for _, cc := range w.sh.Columns {
		if col >= cc.First && col <= cc.Last {
			return cc.Style
		}
	}
	return nil
}
