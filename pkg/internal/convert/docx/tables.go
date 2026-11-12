package docx

import (
	"maps"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// maxGridColumns caps the grid columns of a table. Anti-DoS guard, not
// spec-mandated: Word allows 63 columns.
const maxGridColumns = 1000

// defaultCellMargin is the left and right cell margin of a table that sets
// none, 108 twips (Word's Normal Table style, ECMA-376 Part 1, 17.4.42).
const defaultCellMargin = 5.4

// widthSpec is a table measure (17.18.87 ST_TblWidth): points for dxa, a
// fraction for pct, or auto.
type widthSpec struct {
	kind string
	v    float64
}

// parseWidth reads a tblW, tcW or similar element.
func parseWidth(e *ooxml.Element) *widthSpec {
	if e == nil {
		return nil
	}
	typ := attrOr(e, "type")
	val := attrOr(e, "w")
	switch {
	case typ == "pct" || strings.HasSuffix(val, "%"):
		if before, ok := strings.CutSuffix(val, "%"); ok {
			v, err := strconv.ParseFloat(before, 64)
			if err != nil {
				return nil
			}
			return &widthSpec{kind: "pct", v: max(0, min(10, v/100))}
		}
		v, ok := ooxml.Int(val)
		if !ok {
			return nil
		}
		// Fiftieths of a percent.
		return &widthSpec{kind: "pct", v: max(0, min(10, float64(v)/5000))}
	case typ == "dxa" || typ == "":
		v, ok := ooxml.Twips(val)
		if !ok {
			return nil
		}
		return &widthSpec{kind: "dxa", v: max(0, v)}
	case typ == "nil":
		return &widthSpec{kind: "dxa"}
	}
	return &widthSpec{kind: "auto"}
}

// tableLook is a w:tblLook (17.4.56): which conditional formats apply.
type tableLook struct {
	firstRow, lastRow, firstCol, lastCol, noHBand, noVBand bool
}

// tableProps are the table properties the converter reads (17.4.60).
type tableProps struct {
	style       string
	width       *widthSpec
	jc          *string
	ind         *float64
	borders     map[string]*borderSpec
	shading     *css.Color
	layoutFixed *bool
	cellMar     map[string]float64
	look        *tableLook
	bidi        *bool
	floating    *ooxml.Element
	rowBand     *int
	colBand     *int
}

// merge returns t with the properties o sets laid over it.
func (t tableProps) merge(o *tableProps) tableProps {
	if o == nil {
		return t
	}
	if o.style != "" {
		t.style = o.style
	}
	if o.width != nil {
		t.width = o.width
	}
	if o.jc != nil {
		t.jc = o.jc
	}
	if o.ind != nil {
		t.ind = o.ind
	}
	t.borders = mergeBorders(t.borders, o.borders)
	if o.shading != nil {
		t.shading = o.shading
	}
	if o.layoutFixed != nil {
		t.layoutFixed = o.layoutFixed
	}
	if len(o.cellMar) > 0 {
		m := map[string]float64{}
		maps.Copy(m, t.cellMar)
		maps.Copy(m, o.cellMar)
		t.cellMar = m
	}
	if o.look != nil {
		t.look = o.look
	}
	if o.bidi != nil {
		t.bidi = o.bidi
	}
	if o.floating != nil {
		t.floating = o.floating
	}
	if o.rowBand != nil {
		t.rowBand = o.rowBand
	}
	if o.colBand != nil {
		t.colBand = o.colBand
	}
	return t
}

// mergeBorders returns a with the borders of b laid over it.
func mergeBorders(a, b map[string]*borderSpec) map[string]*borderSpec {
	if len(b) == 0 {
		return a
	}
	m := make(map[string]*borderSpec, len(a)+len(b))
	maps.Copy(m, a)
	maps.Copy(m, b)
	return m
}

// parseBorders reads a tblBorders, tcBorders or pBdr element, start and
// end read as left and right.
func (d *document) parseBorders(e *ooxml.Element) map[string]*borderSpec {
	m := map[string]*borderSpec{}
	for _, b := range e.Kids() {
		name := b.Local
		switch name {
		case "start":
			name = "left"
		case "end":
			name = "right"
		}
		m[name] = d.parseBorder(b)
	}
	return m
}

// parseMargins reads a tblCellMar or tcMar element into points by side
// name.
func parseMargins(e *ooxml.Element) map[string]float64 {
	m := map[string]float64{}
	for _, c := range e.Kids() {
		name := c.Local
		switch name {
		case "start":
			name = "left"
		case "end":
			name = "right"
		}
		if ws := parseWidth(c); ws != nil && ws.kind == "dxa" {
			m[name] = ws.v
		}
	}
	return m
}

// parseTableProps reads a w:tblPr.
func (d *document) parseTableProps(e *ooxml.Element) *tableProps {
	t := &tableProps{}
	for _, c := range e.Kids() {
		if c.Space != w {
			continue
		}
		val := attrOr(c, "val")
		switch c.Local {
		case "tblStyle":
			t.style = val
		case "tblW":
			t.width = parseWidth(c)
		case "jc":
			t.jc = new(val)
		case "tblInd":
			if ws := parseWidth(c); ws != nil && ws.kind == "dxa" {
				v, _ := ooxml.Twips(attrOr(c, "w"))
				t.ind = new(v)
			}
		case "tblBorders":
			t.borders = d.parseBorders(c)
		case "shd":
			if col, ok := shadingColor(c); ok {
				t.shading = &col
			}
		case "tblLayout":
			t.layoutFixed = new(attrOr(c, "type") == "fixed")
		case "tblCellMar":
			t.cellMar = parseMargins(c)
		case "tblLook":
			t.look = parseLook(c)
		case "bidiVisual":
			t.bidi = new(ooxml.Toggle(c))
		case "tblpPr":
			t.floating = c
		case "tblStyleRowBandSize":
			if v, ok := ooxml.Int(val); ok && v > 0 {
				t.rowBand = new(v)
			}
		case "tblStyleColBandSize":
			if v, ok := ooxml.Int(val); ok && v > 0 {
				t.colBand = new(v)
			}
		}
	}
	return t
}

// parseLook reads a w:tblLook, from its attributes or its hexadecimal
// val (17.4.56).
func parseLook(c *ooxml.Element) *tableLook {
	l := &tableLook{}
	if v, err := strconv.ParseUint(attrOr(c, "val"), 16, 16); err == nil {
		l.firstRow, l.lastRow = v&0x20 != 0, v&0x40 != 0
		l.firstCol, l.lastCol = v&0x80 != 0, v&0x100 != 0
		l.noHBand, l.noVBand = v&0x200 != 0, v&0x400 != 0
	}
	for _, f := range []struct {
		name string
		dst  *bool
	}{{"firstRow", &l.firstRow}, {"lastRow", &l.lastRow}, {"firstColumn", &l.firstCol}, {"lastColumn", &l.lastCol}, {"noHBand", &l.noHBand}, {"noVBand", &l.noVBand}} {
		if v, ok := c.Attr(f.name); ok {
			*f.dst = ooxml.OnOff(v, true)
		}
	}
	return l
}

// cellProps are the cell properties the converter reads (17.4.70).
type cellProps struct {
	width    *widthSpec
	span     int
	vMerge   *string
	borders  map[string]*borderSpec
	shading  *css.Color
	margins  map[string]float64
	vAlign   *string
	noWrap   *bool
	textDir  string
	hideMark bool
}

// merge returns c with the properties o sets laid over it.
func (c cellProps) merge(o *cellProps) cellProps {
	if o == nil {
		return c
	}
	if o.width != nil {
		c.width = o.width
	}
	if o.span > 0 {
		c.span = o.span
	}
	if o.vMerge != nil {
		c.vMerge = o.vMerge
	}
	c.borders = mergeBorders(c.borders, o.borders)
	if o.shading != nil {
		c.shading = o.shading
	}
	if len(o.margins) > 0 {
		m := map[string]float64{}
		maps.Copy(m, c.margins)
		maps.Copy(m, o.margins)
		c.margins = m
	}
	if o.vAlign != nil {
		c.vAlign = o.vAlign
	}
	if o.noWrap != nil {
		c.noWrap = o.noWrap
	}
	if o.textDir != "" {
		c.textDir = o.textDir
	}
	return c
}

// parseCellProps reads a w:tcPr.
func (d *document) parseCellProps(e *ooxml.Element) *cellProps {
	c := &cellProps{}
	for _, x := range e.Kids() {
		if x.Space != w {
			continue
		}
		val, hasVal := x.Attr("val")
		switch x.Local {
		case "tcW":
			c.width = parseWidth(x)
		case "gridSpan":
			if v, ok := ooxml.Int(val); ok && v > 0 {
				c.span = min(v, maxGridColumns)
			}
		case "vMerge":
			if !hasVal {
				val = "continue"
			}
			c.vMerge = new(val)
		case "tcBorders":
			c.borders = d.parseBorders(x)
		case "shd":
			if col, ok := shadingColor(x); ok {
				c.shading = &col
			}
		case "tcMar":
			c.margins = parseMargins(x)
		case "vAlign":
			c.vAlign = new(val)
		case "noWrap":
			c.noWrap = new(ooxml.Toggle(x))
		case "textDirection":
			c.textDir = val
		case "hideMark":
			c.hideMark = ooxml.Toggle(x)
		}
	}
	return c
}

// rowProps are the row properties the converter reads (17.4.82).
type rowProps struct {
	height     *float64
	hRule      string
	cantSplit  *bool
	header     *bool
	gridBefore int
	gridAfter  int
}

// parseRowProps reads a w:trPr.
func parseRowProps(e *ooxml.Element) *rowProps {
	r := &rowProps{}
	for _, x := range e.Kids() {
		if x.Space != w {
			continue
		}
		val := attrOr(x, "val")
		switch x.Local {
		case "trHeight":
			if v, ok := ooxml.Twips(val); ok && v > 0 {
				r.height = new(v)
			}
			r.hRule = attrOr(x, "hRule")
		case "cantSplit":
			r.cantSplit = new(ooxml.Toggle(x))
		case "tblHeader":
			r.header = new(ooxml.Toggle(x))
		case "gridBefore":
			if v, ok := ooxml.Int(val); ok && v > 0 {
				r.gridBefore = min(v, maxGridColumns)
			}
		case "gridAfter":
			if v, ok := ooxml.Int(val); ok && v > 0 {
				r.gridAfter = min(v, maxGridColumns)
			}
		}
	}
	return r
}

// cellRef is a cell placed on the grid while a table is built.
type cellRef struct {
	node  *layout.Node
	col   int
	span  int
	props cellProps
	xml   *ooxml.Element
}

// tableRows returns the w:tr of a table, through the content controls and
// custom XML that may wrap them.
func tableRows(t *ooxml.Element, depth int) []*ooxml.Element {
	var out []*ooxml.Element
	for _, c := range t.Kids() {
		switch {
		case c.Is(w, "tr"):
			out = append(out, c)
		case depth < maxNesting && (c.Is(w, "sdt") || c.Is(w, "customXml") || c.Is(w, "sdtContent")):
			out = append(out, tableRows(c, depth+1)...)
		}
	}
	return out
}

// rowCells returns the w:tc of a row, through the wrappers.
func rowCells(r *ooxml.Element, depth int) []*ooxml.Element {
	var out []*ooxml.Element
	for _, c := range r.Kids() {
		switch {
		case c.Is(w, "tc"):
			out = append(out, c)
		case depth < maxNesting && (c.Is(w, "sdt") || c.Is(w, "customXml") || c.Is(w, "sdtContent")):
			out = append(out, rowCells(c, depth+1)...)
		}
	}
	return out
}

// table builds a w:tbl (17.4.38) in the CSS table model: a table box of
// fixed layout over its grid columns, collapsed borders, a header row
// group of its leading tblHeader rows, cells spanning the grid columns of
// their gridSpan and the rows of their vertical merge, the table style's
// conditional formatting applied per cell (17.7.6).
func (d *document) table(s *story, t *ooxml.Element, depth int) *layout.Node {
	if depth > maxNesting {
		d.warn("too-deep", "a table nested too deep is left out")
		return nil
	}
	direct := d.parseTableProps(t.Child(w, "tblPr"))
	chain := d.styles.tableStyle(direct.style)
	var tp tableProps
	var styleCell cellProps
	var stylePara paraProps
	var styleRun runProps
	conds := map[string]*condFormat{}
	for _, st := range chain {
		tp = tp.merge(st.tbl)
		styleCell = styleCell.merge(st.tc)
		stylePara = stylePara.merge(st.ppr)
		styleRun = styleRun.merge(st.rpr)
		maps.Copy(conds, st.cond)
	}
	tp = tp.merge(direct)
	look := tableLook{}
	if tp.look != nil {
		look = *tp.look
	}
	var grid []float64
	for _, g := range t.Child(w, "tblGrid").Kids() {
		if g.Is(w, "gridCol") && len(grid) < maxGridColumns {
			v, _ := ooxml.Twips(attrOr(g, "w"))
			grid = append(grid, max(0, v))
		}
	}
	rows := tableRows(t, depth)
	// Place every cell on the grid, extending the grid for rows wider than
	// it.
	type rowRef struct {
		props *rowProps
		cells []*cellRef
	}
	placed := make([]rowRef, 0, len(rows))
	for _, r := range rows {
		rp := parseRowProps(r.Child(w, "trPr"))
		col := rp.gridBefore
		var cells []*cellRef
		for _, tc := range rowCells(r, depth) {
			cp := d.parseCellProps(tc.Child(w, "tcPr"))
			span := max(cp.span, 1)
			cells = append(cells, &cellRef{col: col, span: span, props: *cp, xml: tc})
			col += span
		}
		for len(grid) < col+rp.gridAfter && len(grid) < maxGridColumns {
			grid = append(grid, 0)
		}
		placed = append(placed, rowRef{props: rp, cells: cells})
	}
	ncols := len(grid)
	if ncols == 0 || len(placed) == 0 {
		return nil
	}
	// Columns without a width share what the table's width leaves.
	total := 0.0
	zero := 0
	for _, g := range grid {
		total += g
		if g == 0 {
			zero++
		}
	}
	if zero > 0 {
		fill := 36.0
		if tp.width != nil && tp.width.kind == "dxa" && tp.width.v > total {
			fill = (tp.width.v - total) / float64(zero)
		}
		for i := range grid {
			if grid[i] == 0 {
				grid[i] = fill
			}
		}
	}
	tableSt := d.blockStyle()
	tableSt.Display = css.Table
	tableSt.BorderCollapse = css.CollapseBorders
	// In the collapsing border model the table's decoration break
	// decides for every cell's: Writer closes a split row's borders.
	tableSt.BoxDecorationBreak = css.DecorationCloneAll
	tableSt.TableLayout = css.TableFixed
	tableSt.LineHeight = css.Dimension{Kind: css.Normal}
	width := 0.0
	for _, g := range grid {
		width += g
	}
	tableSt.Width = css.Pts(width)
	if tp.width != nil && tp.width.kind == "pct" && tp.width.v > 0 {
		tableSt.Width = css.Dimension{Kind: css.Percent, Value: tp.width.v}
	}
	cellMar := map[string]float64{"left": defaultCellMargin, "right": defaultCellMargin, "top": 0, "bottom": 0}
	maps.Copy(cellMar, tp.cellMar)
	indent := 0.0
	if tp.ind != nil {
		indent = *tp.ind
	}
	if d.settings.compat < 15 && tp.ind != nil {
		// Before Word 2013 an indent places the first cell's text, so the
		// table's edge sits its left margin further out, as LibreOffice
		// lays such documents out (MS-DOCX, 2.3.4); a table without an
		// indent keeps its edge at the margin.
		left := cellMar["left"]
		if cells := placed[0].cells; len(cells) > 0 {
			if m, ok := cells[0].props.margins["left"]; ok {
				left = m
			}
		}
		indent -= left
	}
	tableSt.Margin[css.Left] = css.Pts(indent)
	// Only the table's own alignment (17.4.28) places it: Word prints a
	// table whose row carries the legacy row alignment (17.4.27) where
	// the table's properties put it.
	if jc := tp.jc; jc != nil {
		switch *jc {
		case "center":
			tableSt.Margin[css.Left], tableSt.Margin[css.Right] = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
		case "right", "end":
			tableSt.Margin[css.Left] = css.Dimension{Kind: css.Auto}
		}
	}
	if boolOf(tp.bidi) {
		tableSt.Direction = css.RTL
	}
	if tp.floating != nil {
		d.floatTable(tableSt, tp.floating)
	}
	tbl := &layout.Node{Kind: layout.Element, Style: tableSt, Role: "Table"}
	for _, g := range grid {
		cs := d.blockStyle()
		cs.Display = css.TableColumn
		cs.Width = css.Pts(g)
		tbl.Children = append(tbl.Children, &layout.Node{Kind: layout.Element, Style: cs})
	}
	rowBand, colBand := 1, 1
	if tp.rowBand != nil {
		rowBand = *tp.rowBand
	}
	if tp.colBand != nil {
		colBand = *tp.colBand
	}
	// open maps a grid column to the cell a vertical merge continues.
	open := make([]*cellRef, ncols)
	var header, body []*layout.Node
	inHeader := true
	nrows := len(placed)
	for ri, row := range placed {
		rst := d.blockStyle()
		rst.Display = css.TableRow
		// A height without a rule is a minimum, as Word and LibreOffice
		// read it; an exact one fixes the row's height.
		if row.props.height != nil && row.props.hRule != "auto" {
			rst.Height = css.Pts(*row.props.height)
			setExactHeight(rst, row.props.hRule == "exact")
		}
		if boolOf(row.props.cantSplit) {
			rst.BreakInside = css.InsideAvoid
		}
		isHeader := inHeader && boolOf(row.props.header)
		if !isHeader {
			inHeader = false
		}
		rn := &layout.Node{Kind: layout.Element, Style: rst, Role: "TR"}
		for ci, c := range row.cells {
			if c.col >= ncols {
				break
			}
			span := min(c.span, ncols-c.col)
			if c.props.vMerge != nil && *c.props.vMerge != "restart" {
				if prev := open[c.col]; prev != nil && prev.node != nil {
					prev.node.RowSpan = max(prev.node.RowSpan, 1) + 1
					continue
				}
			}
			// The conditional formats of the cell, from the weakest
			// (17.7.6).
			bodyRow := ri
			if look.firstRow {
				bodyRow--
			}
			types := []string{"wholeTable"}
			if !look.noVBand {
				vc := c.col
				if look.firstCol {
					vc--
				}
				if vc >= 0 {
					types = append(types, []string{"band1Vert", "band2Vert"}[(vc/colBand)%2])
				}
			}
			if !look.noHBand && bodyRow >= 0 {
				types = append(types, []string{"band1Horz", "band2Horz"}[(bodyRow/rowBand)%2])
			}
			first, last := ri == 0, ri == nrows-1
			firstCol, lastCol := c.col == 0, c.col+span >= ncols
			if look.firstCol && firstCol {
				types = append(types, "firstCol")
			}
			if look.lastCol && lastCol {
				types = append(types, "lastCol")
			}
			if look.firstRow && first {
				types = append(types, "firstRow")
			}
			if look.lastRow && last {
				types = append(types, "lastRow")
			}
			for _, corner := range []struct {
				name string
				on   bool
			}{{"nwCell", first && firstCol}, {"neCell", first && lastCol}, {"swCell", last && firstCol}, {"seCell", last && lastCol}} {
				if corner.on {
					types = append(types, corner.name)
				}
			}
			cp := styleCell
			cc := &cond{ppr: stylePara, rpr: styleRun}
			tblBorders := tp.borders
			shading := tp.shading
			for _, typ := range types {
				cf := conds[typ]
				if cf == nil {
					continue
				}
				cp = cp.merge(cf.tc)
				cc.ppr = cc.ppr.merge(cf.ppr)
				cc.rpr = cc.rpr.merge(cf.rpr)
				if cf.tbl != nil {
					tblBorders = mergeBorders(tblBorders, cf.tbl.borders)
					if cf.tbl.shading != nil {
						shading = cf.tbl.shading
					}
				}
			}
			cp = cp.merge(&c.props)
			cst := d.cellStyle(cp, cellMar, tblBorders, shading, first, last, firstCol, lastCol)
			if ci == 0 && row.props.gridBefore > 0 {
				cst.Border[css.Left] = css.Border{}
			}
			ts := s
			d.nested++
			d.inCell++
			kids := finish(d.blocks(ts, cc, c.xml, depth+1))
			d.inCell--
			d.nested--
			role := "TD"
			if isHeader {
				role = "TH"
			}
			cn := &layout.Node{Kind: layout.Element, Style: cst, Role: role, Children: kids}
			if isHeader {
				// A tblHeader row repeats atop every page as the table's
				// heading (ECMA-376 Part 1, 17.4.49): its cells head
				// their columns (ISO 32000-1, 14.8.5.7, Table 349;
				// ISO 14289-1, 7.5).
				cn.Scope = compose.ScopeColumn
			}
			if span > 1 {
				cn.ColSpan = span
			}
			c.node = cn
			for k := c.col; k < c.col+span; k++ {
				open[k] = nil
			}
			if c.props.vMerge != nil && *c.props.vMerge == "restart" {
				open[c.col] = c
			}
			rn.Children = append(rn.Children, cn)
		}
		if isHeader {
			header = append(header, rn)
		} else {
			body = append(body, rn)
		}
	}
	if len(header) > 0 {
		hs := d.blockStyle()
		hs.Display = css.TableHeaderGroup
		tbl.Children = append(tbl.Children, &layout.Node{Kind: layout.Element, Style: hs, Children: header})
	}
	bs := d.blockStyle()
	bs.Display = css.TableRowGroup
	tbl.Children = append(tbl.Children, &layout.Node{Kind: layout.Element, Style: bs, Children: body})
	return tbl
}

// cellStyle returns the style of a table cell: its margins as padding,
// its borders (its own, else the table's outer or inside borders by its
// place), its shading and its vertical alignment.
func (d *document) cellStyle(cp cellProps, tableMar map[string]float64, tb map[string]*borderSpec, shading *css.Color, first, last, firstCol, lastCol bool) *css.Style {
	st := d.blockStyle()
	st.Display = css.TableCell
	st.LineHeight = css.Dimension{Kind: css.Normal}
	for side, name := range map[css.Side]string{css.Top: "top", css.Right: "right", css.Bottom: "bottom", css.Left: "left"} {
		v := tableMar[name]
		if m, ok := cp.margins[name]; ok {
			v = m
		}
		st.Padding[side] = css.Pts(v)
	}
	pick := func(own, outer, inner string, edge bool) css.Border {
		if b := cp.borders[own]; b != nil {
			return b.Border
		}
		name := inner
		if edge {
			name = outer
		}
		if b := tb[name]; b != nil {
			return b.Border
		}
		return css.Border{}
	}
	st.Border[css.Top] = pick("top", "top", "insideH", first)
	st.Border[css.Bottom] = pick("bottom", "bottom", "insideH", last)
	st.Border[css.Left] = pick("left", "left", "insideV", firstCol)
	st.Border[css.Right] = pick("right", "right", "insideV", lastCol)
	strokeBorders(st)
	if shading != nil {
		st.BackgroundColor = *shading
	}
	if cp.shading != nil {
		st.BackgroundColor = *cp.shading
	}
	st.VerticalAlign = css.VerticalAlign{Kind: css.AlignTop}
	if cp.vAlign != nil {
		switch *cp.vAlign {
		case "center":
			st.VerticalAlign = css.VerticalAlign{Kind: css.Middle}
		case "bottom":
			st.VerticalAlign = css.VerticalAlign{Kind: css.AlignBottom}
		}
	}
	if boolOf(cp.noWrap) {
		st.WhiteSpace = css.Pre
	}
	// Writer draws a cell's borders whole on every page its row spans:
	// the bottom border at the cut, the top border again on the next
	// page.
	st.BoxDecorationBreak = css.DecorationCloneAll
	if cp.textDir != "" && cp.textDir != "lrTb" && cp.textDir != "tb" {
		setWritingMode(st, textDirection(cp.textDir))
	}
	return st
}

// floatTable floats a table with a w:tblpPr (17.4.58) on the side its
// horizontal alignment says, a position at page coordinates approximated
// by the float; a centred one is centred in its own line, as far below
// the text as its vertical offset says (a float keeps its place: its
// margin would push the text beside it down too).
func (d *document) floatTable(st *css.Style, f *ooxml.Element) {
	st.Float = css.FloatLeft
	switch attrOr(f, "tblpXSpec") {
	case "right", "outside":
		st.Float = css.FloatRight
	case "center":
		st.Float = css.FloatNone
		st.Margin[css.Left], st.Margin[css.Right] = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
		tableOffset(st, f)
		return
	}
	for side, name := range map[css.Side]string{css.Left: "leftFromText", css.Right: "rightFromText", css.Top: "topFromText", css.Bottom: "bottomFromText"} {
		if v, ok := ooxml.Twips(attrOr(f, name)); ok {
			st.Margin[side] = css.Pts(max(0, v))
		}
	}
}

// tableOffset moves a floating table anchored to the text (17.4.58
// vertAnchor text, the default) down by its tblpY, when positive and not
// an alignment.
func tableOffset(st *css.Style, f *ooxml.Element) {
	if attrOr(f, "vertAnchor") == "page" && !hasAttr(f, "tblpYSpec") {
		if v, ok := ooxml.Twips(attrOr(f, "tblpY")); ok {
			setPageTop(st, v)
		}
		return
	}
	if a := attrOr(f, "vertAnchor"); a != "" && a != "text" || hasAttr(f, "tblpYSpec") {
		return
	}
	if v, ok := ooxml.Twips(attrOr(f, "tblpY")); ok && v > 0 {
		st.Margin[css.Top] = css.Pts(v)
	}
}
