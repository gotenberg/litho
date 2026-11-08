package sheet

import (
	"fmt"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// The frame widths of Calc's DataPilot output, in points: the inner lines
// and the table's outer edge (SC_DP_FRAME_INNER_BOLD and
// SC_DP_FRAME_OUTER_BOLD, 20 and 40 twips).
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/pivottable_outline_mode.xlsx strokes its pivot table's
//     outer edge at 2.00009 points and the lines inside at 1.00005.
const (
	pivotInnerFrame = 1
	pivotOuterFrame = 2
)

// pivotIndent is the indent of one level of a row field's members:
// Calc's 13 pixels at 96 to the inch (ScDPOutput::outputRowHeader).
const pivotIndent = 13 * 0.75

// pivotStyle is one of the cell styles Calc's DataPilot output applies
// (the STR_PIVOT_STYLENAME styles, made by lcl_SetStyleById).
type pivotStyle uint8

// The DataPilot cell styles: the corner (Pivot Table Corner), the data
// (Pivot Table Value) and the field buttons (Pivot Table Field) add
// nothing to Default; the categories (Pivot Table Category) align left,
// the titles of the totals (Pivot Table Title) align left in bold, the
// totals (Pivot Table Result) are bold.
const (
	pivotPlain pivotStyle = iota
	pivotCategory
	pivotTitle
	pivotResult
)

// pivotOutCell is one cell of a pivot table's output.
type pivotOutCell struct {
	value  Value
	style  pivotStyle
	format string
	indent float64
	border [4]Border
}

// pivotOut is a pivot table's output being written: its cells and the
// positions Calc computes (ScDPOutput::CalcSizes).
type pivotOut struct {
	cells                          map[[2]int]*pivotOutCell
	tabStartCol, tabStartRow       int
	memberStartCol, memberStartRow int
	dataStartCol, dataStartRow     int
	tabEndCol, tabEndRow           int
	// needRows and needCols are the rows and columns of the data area a
	// block starts at (ScDPOutputImpl::AddRow and AddCol).
	needRows, needCols map[int]bool
}

// at returns the output cell (r, c), creating it.
func (o *pivotOut) at(r, c int) *pivotOutCell {
	k := [2]int{r, c}
	if x, ok := o.cells[k]; ok {
		return x
	}
	x := &pivotOutCell{}
	o.cells[k] = x
	return x
}

// setStyle applies the style s over the range, as lcl_SetStyleById does.
func (o *pivotOut) setStyle(c1, r1, c2, r2 int, s pivotStyle) {
	if c1 > c2 || r1 > r2 {
		return
	}
	for r := r1; r <= r2; r++ {
		for c := c1; c <= c2; c++ {
			o.at(r, c).style = s
		}
	}
}

// line returns a solid black frame line of width w.
func frameLine(w float64) Border {
	return Border{Line: LineSolid, Width: w, Color: css.Color{A: 1}}
}

// blockFrame frames the range as ScDPOutputImpl::OutputBlockFrame does:
// each edge in the inner line, or the outer one where it is the table's;
// with hori, the lines between the range's rows in the inner line too;
// the lines between its columns are left as they are.
func (o *pivotOut) blockFrame(c1, r1, c2, r2 int, hori bool) {
	if c1 > c2 || r1 > r2 {
		return
	}
	edge := func(outer bool) Border {
		if outer {
			return frameLine(pivotOuterFrame)
		}
		return frameLine(pivotInnerFrame)
	}
	left, top := edge(c1 == o.tabStartCol), edge(r1 == o.tabStartRow)
	right, bottom := edge(c2 == o.tabEndCol), edge(r2 == o.tabEndRow)
	for r := r1; r <= r2; r++ {
		o.at(r, c1).border[css.Left] = left
		o.at(r, c2).border[css.Right] = right
	}
	for c := c1; c <= c2; c++ {
		o.at(r1, c).border[css.Top] = top
		o.at(r2, c).border[css.Bottom] = bottom
		if hori {
			for r := r1; r < r2; r++ {
				o.at(r, c).border[css.Bottom] = frameLine(pivotInnerFrame)
				o.at(r+1, c).border[css.Top] = frameLine(pivotInnerFrame)
			}
		}
	}
}

// boxFrame frames every side of the range in the inner line, as
// lcl_SetFrame does.
func (o *pivotOut) boxFrame(c1, r1, c2, r2 int) {
	for r := r1; r <= r2; r++ {
		o.at(r, c1).border[css.Left] = frameLine(pivotInnerFrame)
		o.at(r, c2).border[css.Right] = frameLine(pivotInnerFrame)
	}
	for c := c1; c <= c2; c++ {
		o.at(r1, c).border[css.Top] = frameLine(pivotInnerFrame)
		o.at(r2, c).border[css.Bottom] = frameLine(pivotInnerFrame)
	}
}

// pivotOutput computes the output of the pivot table p of the sheet in
// doc, false when it cannot: a source that is not a range of a sheet, or
// a result past the caps (ScDPOutput::Output).
func (b *builder) pivotOutput(p *Pivot) (*pivotOut, bool) {
	src, ok := b.readPivotSource(p)
	if !ok {
		return nil, false
	}
	p = resolveNames(p, src.names)
	pr := &pivotRun{b: b, p: p, src: src}
	pr.recs = pr.active()
	rows := pr.axis(p.Rows, p.RowGrand, true)
	cols := pr.axis(p.Columns, p.ColumnGrand, false)
	aggs := pr.results(rows, cols)
	if pr.overflow {
		return nil, false
	}
	o := &pivotOut{cells: map[[2]int]*pivotOutCell{}, needRows: map[int]bool{}, needCols: map[int]bool{}}
	nPages := len(p.Pages)
	pageSize := 0
	if p.FilterButton || nPages > 0 {
		pageSize = nPages + 1
		if p.FilterButton {
			pageSize++
		}
	}
	headerSize := 1
	if p.HideHeader {
		headerSize = 0
	}
	compact, anyCompact := rowCompact(rows)
	o.tabStartCol, o.tabStartRow = p.Column, p.Row+pageSize
	o.memberStartCol, o.memberStartRow = o.tabStartCol, o.tabStartRow+headerSize
	o.dataStartCol = o.memberStartCol + rowColumns(compact, anyCompact)
	colFields := len(cols.levels)
	o.dataStartRow = o.memberStartRow + colFields
	nCols, nRows := len(cols.entries), len(rows.entries)
	o.tabEndCol = o.dataStartCol + nCols - 1
	if nPages > 0 && o.tabEndCol < o.tabStartCol+1 {
		o.tabEndCol = o.tabStartCol + 1
	}
	o.tabEndRow = o.dataStartRow + nRows - 1
	if p.Row < 0 || p.Column < 0 || o.tabEndCol >= maxColumns || o.tabEndRow >= maxRows {
		return nil, false
	}
	if p.FilterButton {
		o.at(p.Row, p.Column).value = Value{Kind: Text, Text: "Filter"}
	}
	pr.pageFields(o)
	// The data description names the one data field; a table without
	// one has Calc's "(empty)" measure (ScDPSource::getDataDescription,
	// ScDPResultData::SetMeasureData).
	desc := ""
	switch len(p.Data) {
	case 0:
		desc = "(empty)"
	case 1:
		desc = pr.dataName(0, PivotAuto)
	}
	if desc != "" {
		o.at(o.tabStartRow, o.tabStartCol).value = Value{Kind: Text, Text: desc}
	}
	if o.dataStartRow > o.tabStartRow {
		o.setStyle(o.tabStartCol, o.tabStartRow, o.tabEndCol, o.dataStartRow-1, pivotPlain)
	}
	o.setStyle(o.dataStartCol, o.dataStartRow, o.tabEndCol, o.tabEndRow, pivotPlain)
	pr.columnHeaders(o, cols, anyCompact)
	pr.rowHeaders(o, rows, compact, anyCompact)
	if nCols == 1 && nRows > 0 && colFields == 0 && desc != "" {
		// One data field and no column field: its name heads the data
		// column.
		o.at(o.dataStartRow-1, o.dataStartCol).value = Value{Kind: Text, Text: desc}
	}
	pr.dataCells(o, rows, cols, aggs)
	o.dataArea()
	return o, true
}

// rowCompact returns, per row level, whether it lays out compact, and
// whether any does.
func rowCompact(rows *pivotAxis) ([]bool, bool) {
	out := make([]bool, len(rows.levels))
	any := false
	for i := range rows.levels {
		out[i] = rows.levels[i].f.Layout == PivotCompact
		any = any || out[i]
	}
	return out, any
}

// rowColumns returns the columns the row fields take: one each, the
// compact ones sharing the column of the next (GetColumnsForRowFields).
func rowColumns(compact []bool, any bool) int {
	if !any {
		return len(compact)
	}
	n := 0
	for _, c := range compact {
		if !c {
			n++
		}
	}
	if compact[len(compact)-1] {
		n++
	}
	return n
}

// fieldCaption returns the caption of the field f.
func (pr *pivotRun) fieldCaption(f *PivotField) string {
	if f.Caption != "" {
		return f.Caption
	}
	if f.Field == PivotDataLayout {
		return "Data"
	}
	if f.Field >= 0 && f.Field < len(pr.src.names) {
		return pr.src.names[f.Field]
	}
	return ""
}

// fieldCell writes the button of field f at (r, c), framed when inTable
// (ScDPOutput::FieldCell).
func (pr *pivotRun) fieldCell(o *pivotOut, r, c int, f *PivotField, inTable bool) {
	x := o.at(r, c)
	x.value = Value{Kind: Text, Text: pr.fieldCaption(f)}
	x.style = pivotPlain
	if inTable {
		o.boxFrame(c, r, c, r)
	}
}

// pageFields writes the page fields above the table, each its caption and
// its selection: "- all -", the one member shown, or "- multiple -"
// (ScDPOutput::outputPageFields).
func (pr *pivotRun) pageFields(o *pivotOut) {
	for i := range pr.p.Pages {
		f := &pr.p.Pages[i]
		r := pr.p.Row + i
		if pr.p.FilterButton {
			r++
		}
		pr.fieldCell(o, r, pr.p.Column, f, false)
		val := "- all -"
		switch {
		case f.Field < 0 || f.Field >= len(pr.src.names):
		case f.Selected != nil:
			val = pr.caption(f.Field, *f.Selected)
		case len(f.Hidden) > 0:
			var shown []Value
			for i, ok := range pr.shownItems(f, true) {
				if ok {
					shown = append(shown, pr.src.items[f.Field][i])
				}
			}
			switch len(shown) {
			case 0:
			case 1:
				val = pr.caption(f.Field, shown[0])
			default:
				val = "- multiple -"
			}
		}
		x := o.at(r, pr.p.Column+1)
		x.value = Value{Kind: Text, Text: val}
		o.boxFrame(pr.p.Column+1, r, pr.p.Column+1, r)
	}
}

// headerCell writes a member result at (r, c), and for a subtotal its
// frame and its title and result styles (ScDPOutput::HeaderCell).
func (pr *pivotRun) headerCell(o *pivotOut, r, c int, pc *pivotCell, colHeader bool, level int) {
	if pc.has {
		x := o.at(r, c)
		if !pc.sub && (pc.value.Kind == Number || pc.value.Kind == Bool) && finite(pc.value.Number) {
			x.value = pc.value
		} else {
			x.value = Value{Kind: Text, Text: pc.caption}
		}
	}
	if !pc.sub {
		return
	}
	if colHeader {
		o.blockFrame(c, o.memberStartRow+level, c, o.dataStartRow-1, false)
		o.setStyle(c, o.memberStartRow+level, c, o.dataStartRow-1, pivotTitle)
		o.setStyle(c, o.dataStartRow, c, o.tabEndRow, pivotResult)
		return
	}
	o.blockFrame(o.memberStartCol+level, r, o.dataStartCol-1, r, false)
	o.setStyle(o.memberStartCol+level, r, o.dataStartCol-1, r, pivotTitle)
	o.setStyle(o.dataStartCol, r, o.tabEndCol, r, pivotResult)
}

// fieldFormat returns the number format of a level's members.
func (pr *pivotRun) fieldFormat(lv *pivotLevel) string {
	if lv.data {
		return ""
	}
	return pr.src.formats[lv.f.Field]
}

// columnHeaders writes the column fields' buttons and members
// (ScDPOutput::outputColumnHeaders).
func (pr *pivotRun) columnHeaders(o *pivotOut, cols *pivotAxis, anyCompactRow bool) {
	n := len(cols.levels)
	for f := range n {
		lv := &cols.levels[f]
		hc := o.dataStartCol + f
		if o.memberStartRow > o.tabStartRow {
			switch {
			case !anyCompactRow || n == 1:
				pr.fieldCell(o, o.tabStartRow, hc, lv.f, true)
			case f == 0:
				x := o.at(o.tabStartRow, hc)
				x.value = Value{Kind: Text, Text: "Column Labels"}
			}
		}
		r := o.memberStartRow + f
		for j := range cols.entries {
			pc := &cols.entries[j].cells[f]
			c := o.dataStartCol + j
			pr.headerCell(o, r, c, pc, true, f)
			switch {
			case pc.has && !pc.sub:
				end := j
				for end+1 < len(cols.entries) && cols.entries[end+1].cells[f].cont {
					end++
				}
				ec := o.dataStartCol + end
				if f+1 < n {
					if f+2 == n {
						o.needCols[c] = true
						if c+1 == ec {
							o.blockFrame(c, r, ec, r+1, true)
						}
					} else {
						o.blockFrame(c, r, ec, r, false)
					}
					o.setStyle(c, r, ec, o.dataStartRow-1, pivotCategory)
				} else {
					o.setStyle(c, r, c, o.dataStartRow-1, pivotCategory)
				}
			case pc.sub:
				o.needCols[c] = true
			}
			if pc.has || pc.sub {
				o.at(r, c).format = pr.fieldFormat(lv)
			}
		}
		if f == 0 && n == 1 && o.memberStartRow > o.tabStartRow {
			o.blockFrame(o.dataStartCol, o.tabStartRow, o.tabEndCol, r-1, false)
		}
	}
}

// rowHeaders writes the row fields' buttons and members, indenting the
// members as Calc does (ScDPOutput::outputRowHeader).
func (pr *pivotRun) rowHeaders(o *pivotOut, rows *pivotAxis, compact []bool, anyCompact bool) {
	n := len(rows.levels)
	framed := make([]bool, len(rows.entries))
	colOffset, indentLevel := 0, 0
	for f := range n {
		lv := &rows.levels[f]
		hr := o.dataStartRow - 1
		if hr >= 0 {
			switch {
			case !anyCompact || n == 1:
				pr.fieldCell(o, hr, o.tabStartCol+f, lv.f, true)
			case f == 0:
				x := o.at(hr, o.tabStartCol)
				x.value = Value{Kind: Text, Text: "Row Labels"}
			}
		}
		c := o.memberStartCol + colOffset
		for i := range rows.entries {
			pc := &rows.entries[i].cells[f]
			r := o.dataStartRow + i
			pr.headerCell(o, r, c, pc, false, colOffset)
			switch {
			case pc.has && !pc.sub:
				if f+1 < n {
					end := i
					for end+1 < len(rows.entries) && rows.entries[end+1].cells[f].cont {
						end++
					}
					er := o.dataStartRow + end
					o.needRows[r] = true
					if !framed[i] {
						o.blockFrame(c, r, o.tabEndCol, er, false)
						framed[i] = true
					}
					o.blockFrame(c, r, c, er, false)
					if f == n-2 {
						o.blockFrame(c+1, r, c+1, er, false)
					}
					o.setStyle(c, r, o.dataStartCol-1, er, pivotCategory)
				} else {
					o.setStyle(c, r, o.dataStartCol-1, r, pivotCategory)
				}
				steps := indentLevel
				if f+1 < n && pr.p.ExpandCollapse {
					steps++
				}
				o.at(r, c).indent = float64(steps) * pivotIndent
			case pc.sub:
				o.needRows[r] = true
			}
			if pc.has || pc.sub {
				o.at(r, c).format = pr.fieldFormat(lv)
			}
		}
		if !compact[f] {
			colOffset++
			indentLevel = 0
		} else {
			indentLevel++
		}
	}
}

// dataCells writes the results with the data fields' number formats
// (ScDPOutput::DataCell): a count's in General, the others in their
// source field's.
func (pr *pivotRun) dataCells(o *pivotOut, rows, cols *pivotAxis, aggs []*pivotAgg) {
	if aggs == nil {
		return
	}
	nc := len(cols.entries)
	for i := range rows.entries {
		for j := range cols.entries {
			re, ce := &rows.entries[i], &cols.entries[j]
			if !re.data || !ce.data {
				continue
			}
			m, fn, ok := pr.cellFunction(re, ce)
			if !ok {
				continue
			}
			v, has := aggs[i*nc+j].result(fn)
			if !has {
				continue
			}
			x := o.at(o.dataStartRow+i, o.dataStartCol+j)
			x.value = v
			if v.Kind == Number {
				x.format = pr.dataFormat(m)
			}
		}
	}
}

// dataFormat returns the number format of the data field m: General for
// a count, else its source field's (ScDPDimension, SC_UNO_DP_NUMBERFO).
func (pr *pivotRun) dataFormat(m int) string {
	d := &pr.p.Data[m]
	if d.Function == PivotCount || d.Function == PivotCountNums || d.Field < 0 || d.Field >= len(pr.src.formats) {
		return ""
	}
	return pr.src.formats[d.Field]
}

// dataArea frames the data area's blocks and the row and column areas
// (ScDPOutputImpl::OutputDataArea).
func (o *pivotOut) dataArea() {
	o.needRows[o.dataStartRow] = true
	o.needCols[o.dataStartCol] = true
	rows := sortedKeys(o.needRows)
	cols := sortedKeys(o.needCols)
	all := o.tabEndRow-o.dataStartRow+1 == len(rows)
	cols = append(cols, o.tabEndCol+1)
	rows = append(rows, o.tabEndRow+1)
	for k := 0; k < len(cols)-1; k++ {
		switch {
		case all:
			o.blockFrame(cols[k], rows[0], cols[k+1]-1, rows[len(rows)-1]-1, true)
		case k < len(cols)-2:
			for i := k % 2; i < len(rows)-2; i += 2 {
				o.blockFrame(cols[k], rows[i], cols[k+1]-1, rows[i+1]-1, false)
			}
			if len(rows) >= 2 {
				o.blockFrame(cols[k], rows[len(rows)-2], cols[k+1]-1, rows[len(rows)-1]-1, false)
			}
		default:
			for i := 0; i < len(rows)-1; i++ {
				o.blockFrame(cols[k], rows[i], cols[k+1]-1, rows[i+1]-1, false)
			}
		}
	}
	if o.tabStartCol != o.dataStartCol {
		if o.tabStartRow != o.dataStartRow {
			o.blockFrame(o.tabStartCol, o.tabStartRow, o.dataStartCol-1, o.dataStartRow-1, false)
		}
		o.blockFrame(o.tabStartCol, o.dataStartRow, o.dataStartCol-1, o.tabEndRow, false)
	}
	o.blockFrame(o.dataStartCol, o.tabStartRow, o.tabEndCol, o.dataStartRow-1, false)
}

// sortedKeys returns the keys of m, rising, within the sheet.
func sortedKeys(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

// applyPivots returns doc with the output of every sheet's pivot tables
// written over its cells, as Calc rebuilds a pivot table when it loads a
// workbook (oox::xls::PivotTable::finalizeImport): the area the file saved
// the output in is emptied of values and styles, then the output computed
// from the source cells is written, each cell in Calc's DataPilot style
// over the document's default. A pivot table whose source is not a sheet
// range keeps the cells the file saved, with a warning. doc is not
// changed.
func (b *builder) applyPivots(doc *Document) *Document {
	any := false
	for _, sh := range doc.Sheets {
		any = any || (sh != nil && len(sh.Pivots) > 0)
	}
	if !any {
		return doc
	}
	out := *doc
	out.Sheets = append([]*Sheet(nil), doc.Sheets...)
	for i, sh := range doc.Sheets {
		if sh == nil || len(sh.Pivots) == 0 {
			continue
		}
		cells := map[[2]int]*Cell{}
		var cleared []Range
		for k := range sh.Pivots {
			p := &sh.Pivots[k]
			o, ok := b.pivotOutput(p)
			if !ok {
				b.warn(CodePivot, fmt.Sprintf("pivot table %s: its output is printed as the file saved it", quote(p.Name)))
				continue
			}
			if p.Clear != nil {
				cleared = append(cleared, clampRange(*p.Clear))
			}
			// Calc empties the whole output area before writing it
			// (ScDPOutput::Output, DeleteAreaTab).
			cleared = append(cleared, Range{FirstRow: p.Row, FirstColumn: p.Column, LastRow: o.tabEndRow, LastColumn: o.tabEndCol})
			for k, x := range o.cells {
				cells[k] = &Cell{Column: k[1], Value: x.value, Style: b.pivotCellStyle(x)}
			}
		}
		if len(cells) > 0 || len(cleared) > 0 {
			out.Sheets[i] = withCells(sh, cells, cleared, b.pivotCellStyle(nil))
		}
	}
	return &out
}

// pivotCellStyle returns the style of an output cell: the document's
// default with the DataPilot style's attributes, the cell's frame, number
// format and indent; x nil for an emptied cell.
func (b *builder) pivotCellStyle(x *pivotOutCell) *Style {
	st := b.doc.Default
	if x == nil {
		return &st
	}
	st.Border = x.border
	st.Format = x.format
	switch x.style {
	case pivotCategory:
		st.Align.Horizontal = HLeft
	case pivotTitle:
		st.Align.Horizontal = HLeft
		st.Font.Bold = true
	case pivotResult:
		st.Font.Bold = true
	case pivotPlain:
	}
	if x.indent > 0 {
		st.Align.Indent = x.indent
		if st.Align.Horizontal == HGeneral {
			st.Align.Horizontal = HLeft
		}
	}
	return &st
}

// withCells returns a copy of sh whose cells at the keys of cells are
// replaced by them, and whose other cells in a cleared range are emptied
// to the style plain.
func withCells(sh *Sheet, cells map[[2]int]*Cell, cleared []Range, plain *Style) *Sheet {
	inCleared := func(r, c int) bool {
		for _, rg := range cleared {
			if r >= rg.FirstRow && r <= rg.LastRow && c >= rg.FirstColumn && c <= rg.LastColumn {
				return true
			}
		}
		return false
	}
	out := *sh
	byRow := map[int][]Cell{}
	for k, c := range cells {
		byRow[k[0]] = append(byRow[k[0]], *c)
	}
	out.Rows = make([]Row, 0, len(sh.Rows)+len(byRow))
	seen := map[int]bool{}
	for _, row := range sh.Rows {
		add := byRow[row.Index]
		if seen[row.Index] {
			out.Rows = append(out.Rows, row)
			continue
		}
		seen[row.Index] = true
		kept := make([]Cell, 0, len(row.Cells)+len(add))
		for _, c := range row.Cells {
			if _, replaced := cells[[2]int{row.Index, c.Column}]; replaced {
				continue
			}
			if inCleared(row.Index, c.Column) {
				c = Cell{Column: c.Column, Style: plain}
			}
			kept = append(kept, c)
		}
		kept = append(kept, add...)
		sort.SliceStable(kept, func(i, j int) bool { return kept[i].Column < kept[j].Column })
		row.Cells = kept
		out.Rows = append(out.Rows, row)
	}
	var extra []int
	for r := range byRow {
		if !seen[r] {
			extra = append(extra, r)
		}
	}
	sort.Ints(extra)
	for _, r := range extra {
		add := byRow[r]
		sort.SliceStable(add, func(i, j int) bool { return add[i].Column < add[j].Column })
		out.Rows = append(out.Rows, Row{Index: r, Cells: add})
	}
	sort.SliceStable(out.Rows, func(i, j int) bool { return out.Rows[i].Index < out.Rows[j].Index })
	return &out
}

// resolveNames returns p with its fields that name their source column
// resolved against the source's labels, those no label matches left out
// (oox::xls::PivotTableField::convertRowColPageField finds the DataPilot
// field by its cache field's name).
func resolveNames(p *Pivot, labels []string) *Pivot {
	named := false
	for _, fs := range [][]PivotField{p.Rows, p.Columns, p.Pages} {
		for i := range fs {
			named = named || fs[i].Name != ""
		}
	}
	for i := range p.Data {
		named = named || p.Data[i].Name != ""
	}
	if !named {
		return p
	}
	find := func(name string) int {
		for i, l := range labels {
			if l == name {
				return i
			}
		}
		return -1
	}
	fields := func(fs []PivotField) []PivotField {
		var out []PivotField
		for _, f := range fs {
			if f.Name != "" && f.Field != PivotDataLayout {
				if f.Field = find(f.Name); f.Field < 0 {
					continue
				}
			}
			out = append(out, f)
		}
		return out
	}
	q := *p
	q.Rows, q.Columns, q.Pages = fields(p.Rows), fields(p.Columns), fields(p.Pages)
	q.Data = nil
	for _, d := range p.Data {
		if d.Name != "" {
			if d.Field = find(d.Name); d.Field < 0 {
				continue
			}
		}
		q.Data = append(q.Data, d)
	}
	return &q
}
