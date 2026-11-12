package docx

import (
	"maps"
	"math"
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
	cellSpacing *float64
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
	if o.cellSpacing != nil {
		t.cellSpacing = o.cellSpacing
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

// parseBorders reads a tblBorders or tcBorders element, start and end
// read as left and right, each border as wide as Word draws it.
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
		bs := d.parseBorder(b)
		if v, ok := ooxml.Int(attrOr(b, "sz")); ok && bs.Width > 0 {
			if w := tableBorderWidth(attrOr(b, "val"), float64(max(2, min(96, v)))/8); w > 0 {
				bs.Width = w
			}
		}
		m[name] = bs
	}
	return m
}

// tableBorderWidth returns the width Word draws a table border of style
// val and sz w points at, 0 where the border is as wide as w or a double
// line's three widths: ST_Border (ECMA-376 Part 1, 17.18.2) leaves the
// drawn width of the compound lines to the application.
//
// # Corpus evidence
//
//   - libreoffice/docx ooxmlexport/all_gaps_word.docx: Word draws a
//     small-gap thick and thin border a 0.75 pt line and a 0.75 pt gap
//     beside the thick line of sz (1.5 to 6 pt); a medium-gap one a gap
//     and a thin line of half the sz each, in quarter points (0.5 to
//     6 pt); a large-gap one a 1.5 pt line, a 0.75 pt line and a gap as
//     wide as the sz (0.5 to 6 pt); a 3D border the sz between two lines
//     of 0.75 pt below 3 pt and of 1.5 pt from 3 pt (1.5 to 6 pt).
//   - litho/docx tables-borders: at sz 12 (1.5 pt) Word draws a triple
//     border 7.5 pt wide (three 1.5 pt lines, two 1.5 pt gaps) and at sz
//     6 a wave border 3 pt wide; its rows grow by those widths.
func tableBorderWidth(val string, w float64) float64 {
	switch val {
	case "triple":
		return 5 * w
	case "thinThickSmallGap", "thickThinSmallGap":
		return w + 1.5
	case "thinThickMediumGap", "thickThinMediumGap":
		return w + 2*math.Floor(w/2*4)/4
	case "thinThickLargeGap", "thickThinLargeGap":
		return w + 2.25
	case "threeDEmboss", "threeDEngrave":
		if w < 3 {
			return w + 1.5
		}
		return w + 3
	case "wave":
		return 4 * w
	}
	return 0
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
		case "tblCellSpacing":
			if ws := parseWidth(c); ws != nil && ws.kind == "dxa" {
				t.cellSpacing = new(ws.v)
			}
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
	spaced := tp.cellSpacing != nil && *tp.cellSpacing > 0
	if spaced {
		d.spaceCells(tableSt, *tp.cellSpacing, tp.borders)
	}
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
	if d.settings.compat < 15 && tp.ind != nil && d.inCell == 0 {
		// Before Word 2013 an indent places the first cell's text, so the
		// table's edge sits its left margin further out, as LibreOffice
		// lays such documents out (MS-DOCX, 2.3.4); a table without an
		// indent keeps its edge at the margin. Word places a nested
		// table's edge at its indent from the cell's text whatever the
		// mode (see [tableIndent]).
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
			cst := d.cellStyle(cp, cellMar, tblBorders, shading, cellPlace{first, last, firstCol, lastCol, spaced})
			if ci == 0 && row.props.gridBefore > 0 {
				cst.Border[css.Left] = css.Border{}
			}
			ts := s
			d.nested++
			d.inCell++
			bs := d.blocks(ts, cc, c.xml, depth+1)
			if n := len(bs); n >= 2 && endsWithTableMark(c.xml) && bs[n-2].node.Role == "Table" && bs[n-1].node.Role == "P" && bs[n-1].node.ID == "" {
				bs = bs[:n-1]
			}
			kids := finish(bs)
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
		alignRowTops(rn.Children, spaced)
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

// endsWithTableMark reports a cell whose last paragraph is empty and
// follows a nested table: the paragraph a cell must end with (ECMA-376
// Part 1, 17.4.66), which Word does not lay out after a table.
//
// # Corpus evidence
//
//   - litho/docx tables-nested: Word ends the row of a cell holding a
//     table and an empty paragraph at the nested table's bottom border;
//     the paragraph's line and its 8 pt space after take no room.
//   - libreoffice/docx ooxmlexport/fdo73389.docx: the row of a cell ending
//     with a nested table and an empty paragraph ends at the nested
//     table, 13.2 pt above where one more line would end it.
func endsWithTableMark(tc *ooxml.Element) bool {
	var prev, last *ooxml.Element
	for _, k := range tc.Kids() {
		if k.Space != w || k.Local == "tcPr" || k.Local == "bookmarkStart" || k.Local == "bookmarkEnd" || k.Local == "proofErr" {
			continue
		}
		prev, last = last, k
	}
	return last != nil && prev != nil && last.Is(w, "p") && prev.Is(w, "tbl") && emptyParagraph(last) && last.Child(w, "pPr").Child(w, "numPr") == nil
}

// alignRowTops starts the content of every cell of a row below the
// thickest top border of the row, as Word lays a row out: a cell under a
// thinner border gets the difference as padding, half of it where the
// collapsing border model already starts each cell half its own border
// lower, all of it where the table spaces its cells apart.
//
// # Corpus evidence
//
//   - libreoffice/docx ooxmlexport/all_gaps_word.docx: Word prints the
//     text of a row whose cells have top borders 3 to 7.5 pt wide on one
//     line, 20.7 pt below the paragraph above, where it would be 2.2 pt
//     higher under the thinnest border alone; the text of a row of a
//     table with cell spacing whose cells have top borders 0.25 to 6 pt
//     wide on one line too.
func alignRowTops(cells []*layout.Node, spaced bool) {
	share := 0.5
	if spaced {
		share = 1
	}
	top := func(st *css.Style) float64 {
		if b := st.Border[css.Top]; b.Style != css.BorderNone {
			return b.Width
		}
		return 0
	}
	thickest := 0.0
	for _, c := range cells {
		thickest = max(thickest, top(c.Style))
	}
	for _, c := range cells {
		if d := thickest - top(c.Style); d > 0 && c.Style.Padding[css.Top].Kind == css.Points {
			c.Style.Padding[css.Top] = css.Pts(c.Style.Padding[css.Top].Value + d*share)
		}
	}
}

// spaceCells separates the cells of the table st by its cell spacing
// (ECMA-376 Part 1, 17.4, tblCellSpacing) in the separated borders model:
// the table draws its outer borders around its cells, which draw their
// own.
//
// # Corpus evidence
//
//   - litho/docx tables-borders: with a cell spacing of 72 twips (3.6 pt)
//     Word leaves 7 pt between two cells and 6.25 pt between a cell and
//     the table's border, its columns 107.3 pt apart on a 100 pt grid,
//     its rows 32.5 pt apart around 25.5 pt cells: twice the spacing
//     between the cells, as the spacing is the space each cell keeps on
//     its sides.
func (d *document) spaceCells(st *css.Style, spacing float64, tb map[string]*borderSpec) {
	st.BorderCollapse = css.SeparateBorders
	st.BorderSpacing = [2]float64{2 * spacing, 2 * spacing}
	for side, name := range map[css.Side]string{css.Top: "top", css.Right: "right", css.Bottom: "bottom", css.Left: "left"} {
		if b := tb[name]; b != nil {
			st.Border[side] = b.Border
		}
	}
	strokeBorders(st)
}

// cellPlace is where a cell sits in its table: on which of its edges,
// and whether the table spaces its cells apart.
type cellPlace struct {
	first, last, firstCol, lastCol, spaced bool
}

// cellStyle returns the style of a table cell: its margins as padding,
// its borders (its own, else the table's outer or inside borders by its
// place), its shading and its vertical alignment.
func (d *document) cellStyle(cp cellProps, tableMar map[string]float64, tb map[string]*borderSpec, shading *css.Color, at cellPlace) *css.Style {
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
	st.Border[css.Top] = pick("top", "top", "insideH", at.first)
	st.Border[css.Bottom] = pick("bottom", "bottom", "insideH", at.last)
	st.Border[css.Left] = pick("left", "left", "insideV", at.firstCol)
	st.Border[css.Right] = pick("right", "right", "insideV", at.lastCol)
	strokeBorders(st)
	// Word measures a cell's left and right margins from the middle of
	// its collapsed border (17.4.42 tblCellMar): the inner half of the
	// border takes from the margin. A spaced cell's margin starts inside
	// its whole border.
	for _, side := range []css.Side{css.Left, css.Right} {
		if at.spaced {
			break
		}
		if b := st.Border[side]; b.Style != css.BorderNone && b.Width > 0 && st.Padding[side].Kind == css.Points {
			st.Padding[side] = css.Pts(max(0, st.Padding[side].Value-b.Width/2))
		}
	}
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
