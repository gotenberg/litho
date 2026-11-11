package ods

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// The caps of one spreadsheet.
const (
	// maxSheets bounds the tables read: 1,024. Anti-DoS guard, not
	// spec-mandated: Calc's own limit is 10,000 sheets, and a real book
	// holds a few dozen.
	maxSheets = 1024
	// maxColumns is the columns of a sheet, 16,384, and maxRows its rows,
	// 1,048,576: Calc's sheet size since LibreOffice 7.4. A column or row
	// past them is not read, as Calc drops it on import.
	maxColumns = 16384
	maxRows    = 1 << 20
	// maxRowEntries bounds the rows the model lists for one document,
	// with or without cells: 1,048,576. Anti-DoS guard, not spec-mandated:
	// a row without cells is listed for its height alone, and a sheet's
	// rows are at most maxRows.
	maxRowEntries = 1 << 20
	// maxDrawings bounds the pictures and charts one document places:
	// 4,096. Anti-DoS guard, not spec-mandated: an illustrated report
	// holds a few dozen.
	maxDrawings = 4096
	// maxBreaks bounds the manual page breaks of one sheet: 4,096.
	// Anti-DoS guard, not spec-mandated: a repeated column or row with a
	// break asks for one per repetition, and a printed sheet holds a few
	// dozen pages.
	maxBreaks = 4096
	// maxGroupDepth bounds the nesting of column and row groups read:
	// 32. Anti-DoS guard, not spec-mandated: Calc nests groups 8 deep.
	maxGroupDepth = 32
	// maxMerges bounds the merged ranges of one document: 100,000.
	// Anti-DoS guard, not spec-mandated: a repeated merged cell asks for
	// one range per repetition.
	maxMerges = 100_000
)

// The defaults of a Calc sheet, the en-US image's.
const (
	// defaultColumnWidth is Calc's standard column width, 1,280 twips
	// (STD_COL_WIDTH, sc/inc/global.hxx): 64 points, the 0.889in an ODS
	// file writes for its default columns.
	defaultColumnWidth = 64
	// defaultRowHeight is Calc's standard row height for the Default cell
	// style's Liberation Sans 10, 256 twips: 12.8 points, the 0.178in an
	// ODS file writes. The rows of a CSV import are 12.78 points apart in
	// the image's PDF.
	defaultRowHeight = 12.8
	// defaultFontSize is the Default cell style's size when the document
	// names none, and defaultFamily its face.
	defaultFontSize = 10
	defaultFamily   = "Liberation Sans"
)

// reader reads one document into the spreadsheet model.
type reader struct {
	// paperW and paperH are the paper of a page style that names none.
	paperW, paperH float64
	ctx            context.Context
	pkg            *odf.Package
	fonts          *text.Fonts
	styles         *odf.Styles
	data           *odf.DataStyles
	draw           *odf.DrawStyles

	doc      *sheet.Document
	cellSty  map[string]*sheet.Style
	condSty  map[string]*sheet.Style
	pages    map[string]pageResult
	pics     map[string]*picResult
	cells    int
	rows     int
	merges   int
	drawings int

	warned   map[string]bool
	warnings []compose.Warning
	err      error
	steps    int
}

// newReader returns a reader of pkg.
func newReader(ctx context.Context, pkg *odf.Package, fonts *text.Fonts) *reader {
	return &reader{
		ctx: ctx, pkg: pkg, fonts: fonts, styles: odf.NewStyles(pkg), data: odf.NewDataStyles(pkg),
		cellSty: map[string]*sheet.Style{}, condSty: map[string]*sheet.Style{}, pages: map[string]pageResult{}, pics: map[string]*picResult{},
		warned: map[string]bool{}, paperW: letterWidth, paperH: letterHeight,
	}
}

// warn records the warning code once.
func (r *reader) warn(code, msg string) {
	if r.warned[code] {
		return
	}
	r.warned[code] = true
	r.warnings = append(r.warnings, compose.Warning{Code: code, Message: msg})
}

// tick checks ctx every 4,096 steps.
func (r *reader) tick() bool {
	if r.err != nil {
		return false
	}
	r.steps++
	if r.steps%4096 == 0 {
		if err := r.ctx.Err(); err != nil {
			r.err = err
			return false
		}
	}
	return true
}

// document reads the office:spreadsheet body (ODF 1.3, Part 3, 9.1).
func (r *reader) document(body *odf.Element) *sheet.Document {
	r.doc = &sheet.Document{Meta: r.metadata(), Default: r.defaultStyle()}
	var noPrint []bool
	for _, c := range body.Children {
		switch {
		case c.Is(odf.NSTable, "table"):
			if len(r.doc.Sheets) >= maxSheets {
				r.warn("too-many-sheets", fmt.Sprintf("sheets past %d are not read", maxSheets))
				continue
			}
			if sh := r.table(c); sh != nil {
				r.doc.Sheets = append(r.doc.Sheets, sh)
				noPrint = append(noPrint, c.A(odf.NSTable, "print") == "false" && len(sh.Page.PrintAreas) == 0)
			}
		case c.Is(odf.NSTable, "named-expressions"):
			if len(c.Children) > 0 {
				r.warn("named-expressions", "named ranges and expressions are not read")
			}
		case c.Is(odf.NSTable, "data-pilot-tables"):
			r.warn("data-pilot", "pivot tables are shown as the cells the file stores")
		case c.Is(odf.NSTable, "content-validations"):
			r.warn("validations", "cell validations are not applied")
		case c.Is(odf.NSTable, "database-ranges"), c.Is(odf.NSTable, "calculation-settings"):
		}
		if r.err != nil {
			return nil
		}
	}
	hidePrintless(r.doc.Sheets, noPrint)
	return r.doc
}

// hidePrintless hides the sheets that print nothing: table:print false
// without print ranges (ODF 1.3, Part 3, 19.689), as Calc reads a sheet
// that neither prints entirely nor has ranges, when another shown sheet
// prints; in a document where no shown sheet prints, every sheet prints
// whole, as Calc does when no sheet has a print range.
//
// # Corpus evidence
//
//   - libreoffice/ods: sc/bug-fixes.ods prints Sheet2 to Sheet4 and not Sheet1, whose
//     table:print is false; sc/tdf132105.ods prints its one shown sheet,
//     whose table:print is false.
func hidePrintless(sheets []*sheet.Sheet, noPrint []bool) {
	prints := false
	for i, sh := range sheets {
		if !sh.Hidden && !noPrint[i] {
			prints = true
		}
	}
	if !prints {
		return
	}
	for i, sh := range sheets {
		if noPrint[i] {
			sh.Hidden = true
		}
	}
}

// metadata reads meta.xml's office:meta (ODF 1.3, Part 3, 4): the title,
// the creator, the subject, else the description, the keywords, the
// generator and the language; never a date.
func (r *reader) metadata() compose.Metadata {
	var m compose.Metadata
	meta := odf.Section(r.pkg.Meta, "meta")
	if meta == nil {
		return m
	}
	get := func(space, local string) string {
		return strings.Join(strings.Fields(meta.Child(space, local).TextContent()), " ")
	}
	m.Title = get(odf.NSDC, "title")
	m.Author = get(odf.NSDC, "creator")
	if m.Author == "" {
		m.Author = get(odf.NSMeta, "initial-creator")
	}
	m.Subject = get(odf.NSDC, "subject")
	if m.Subject == "" {
		m.Subject = get(odf.NSDC, "description")
	}
	var kw []string
	for _, k := range meta.All(odf.NSMeta, "keyword") {
		if s := strings.TrimSpace(k.TextContent()); s != "" {
			kw = append(kw, s)
		}
	}
	m.Keywords = strings.Join(kw, ", ")
	m.Creator = get(odf.NSMeta, "generator")
	m.Lang = get(odf.NSDC, "language")
	return m
}

// tableState is what one table:table's reading tracks.
type tableState struct {
	sh *sheet.Sheet
	// colX holds the left edge of each column read, from the sheet's
	// origin, hidden columns taking no room; one more entry than columns.
	colX []float64
	// row is the index of the next row, rowY its top edge.
	row  int
	rowY float64
	// lastRowHeight is the height of the last row element read, the
	// sheet's default row height (Calc writes the rest of the sheet as a
	// final repeated row).
	lastRowHeight float64
	// lastColumnWidth alike, from the final repeated column.
	lastColumnWidth float64
	// pending is a run of rows without cells whose height, hidden flag or
	// break is their own: listed once a later row needs their index.
	pending []pendingRows
	// header rows and columns seen.
	headerRows, headerCols *sheet.Range
	// rowHidden reports the row whose frames are being read hidden.
	rowHidden bool
}

// pendingRows is n copies of row, without cells, from row.Index.
type pendingRows struct {
	row sheet.Row
	n   int
}

// table reads one table:table (ODF 1.3, Part 3, 9.1.2) into a sheet.
func (r *reader) table(el *odf.Element) *sheet.Sheet {
	name := el.A(odf.NSTable, "name")
	st := r.styles.Style(odf.ScopeContent, odf.FamilyTable, el.A(odf.NSTable, "style-name"))
	tp := r.styles.Resolve(odf.ScopeContent, odf.FamilyTable, el.A(odf.NSTable, "style-name"), odf.TableProps)
	sh := &sheet.Sheet{
		Name:   name,
		Hidden: tp.Get("table:display") == "false",
		// Writing mode rl-tb on the table property mirrors the sheet
		// (ODF 1.3, Part 3, 20.404).
		RightToLeft:        strings.HasPrefix(tp.Get("style:writing-mode"), "rl"),
		DefaultColumnWidth: defaultColumnWidth,
		DefaultRowHeight:   defaultRowHeight,
	}
	master := "Default"
	if st != nil && st.HasMasterPage && st.MasterPage != "" {
		master = st.MasterPage
	}
	pr := r.pageSetup(master)
	sh.Page, sh.HeaderFooter, sh.HideZeros = pr.page, pr.hf, pr.hideZeros
	ts := &tableState{sh: sh, colX: []float64{0}}
	// The sheet's own shapes come first in drawing order, as they come
	// first in the table (ODF 1.3, Part 3, 9.1.2).
	for _, c := range el.All(odf.NSTable, "shapes") {
		for _, s := range c.Children {
			if !s.IsText() {
				r.drawing(ts, s, nil)
			}
		}
	}
	r.columns(ts, el, false, false, 0)
	r.rowsOf(ts, el, false, false, 0)
	if ts.lastColumnWidth > 0 {
		sh.DefaultColumnWidth = ts.lastColumnWidth
	}
	if ts.lastRowHeight > 0 {
		sh.DefaultRowHeight = ts.lastRowHeight
	}
	if ts.headerRows != nil {
		sh.Page.RepeatRows = ts.headerRows
	}
	if ts.headerCols != nil {
		sh.Page.RepeatColumns = ts.headerCols
	}
	if pr := el.A(odf.NSTable, "print-ranges"); pr != "" {
		sh.Page.PrintAreas = parseRanges(pr)
	}
	calcext := false
	for _, c := range el.Children {
		switch {
		case c.Is(odf.NSTable, "named-expressions"):
			if len(c.Children) > 0 {
				r.warn("named-expressions", "named ranges and expressions are not read")
			}
		case c.Is(nsCalcExt, "conditional-formats"):
			r.conditionalFormats(sh, c)
			calcext = true
		case c.Is(odf.NSOffice, "forms"):
			if len(c.Children) > 0 {
				r.warn("form-controls", "form controls are not drawn")
			}
		case c.Is(odf.NSTable, "scenario"):
			r.warn("scenarios", "scenarios are not read")
		}
	}
	if !calcext {
		r.styleMaps(sh)
	}
	return sh
}

// repeat returns the repetition count of the attribute local of el
// (ODF 1.3, Part 3, 19.677 and 19.678): at least 1, at most limit.
func repeat(el *odf.Element, local string, limit int) int {
	n, err := strconv.Atoi(el.A(odf.NSTable, local))
	if err != nil || n < 1 {
		return 1
	}
	return min(n, max(limit, 1))
}

// columns reads the table:table-column elements of el and of its column
// groups and header columns (ODF 1.3, Part 3, 9.1.6 to 9.1.11). A group
// whose table:display is false is collapsed: its columns are hidden.
func (r *reader) columns(ts *tableState, el *odf.Element, hidden, header bool, depth int) {
	for _, c := range el.Children {
		if !r.tick() {
			return
		}
		switch {
		case c.Is(odf.NSTable, "table-column"):
			r.column(ts, c, hidden, header)
		case c.Is(odf.NSTable, "table-columns"):
			r.columns(ts, c, hidden, header, depth+1)
		case c.Is(odf.NSTable, "table-header-columns"):
			r.columns(ts, c, hidden, true, depth+1)
		case c.Is(odf.NSTable, "table-column-group"):
			if depth < maxGroupDepth {
				r.columns(ts, c, hidden || c.A(odf.NSTable, "display") == "false", header, depth+1)
			}
		}
	}
}

// column reads one table:table-column (ODF 1.3, Part 3, 9.1.6): its
// repetition, width (style:column-width, 20.237), break (fo:break-before,
// 20.191), visibility (19.749) and default cell style.
func (r *reader) column(ts *tableState, el *odf.Element, hidden, header bool) {
	first := len(ts.colX) - 1
	if first >= maxColumns {
		return
	}
	n := repeat(el, "number-columns-repeated", maxColumns-first)
	props := r.styles.Resolve(odf.ScopeContent, odf.FamilyColumn, el.A(odf.NSTable, "style-name"), odf.ColumnProps)
	width := 0.0
	if w, ok := odf.Length(props.Get("style:column-width")); ok && w > 0 {
		width = w
	}
	hidden = hidden || el.A(odf.NSTable, "visibility") == "collapse" || el.A(odf.NSTable, "visibility") == "filter"
	col := sheet.Column{
		First: first, Last: first + n - 1, Width: width, Hidden: hidden,
		Style: r.cellStyle(el.A(odf.NSTable, "default-cell-style-name")),
	}
	if width > 0 || hidden || col.Style != nil {
		ts.sh.Columns = append(ts.sh.Columns, col)
	}
	if props.Get("fo:break-before") == "page" {
		for i := first; i < first+n && len(ts.sh.Page.ColumnBreaks) < maxBreaks; i++ {
			if i > 0 {
				ts.sh.Page.ColumnBreaks = append(ts.sh.Page.ColumnBreaks, i)
			}
		}
	}
	if header {
		ts.headerCols = extend(ts.headerCols, first, first+n-1, false)
	}
	if width > 0 {
		ts.lastColumnWidth = width
	}
	w := width
	if w == 0 {
		w = defaultColumnWidth
	}
	if hidden {
		w = 0
	}
	for range n {
		ts.colX = append(ts.colX, ts.colX[len(ts.colX)-1]+w)
	}
}

// extend returns rg grown to cover first..last, rows when rows is set,
// else columns.
func extend(rg *sheet.Range, first, last int, rows bool) *sheet.Range {
	if rg == nil {
		rg = &sheet.Range{FirstRow: first, LastRow: last, FirstColumn: first, LastColumn: last}
		if rows {
			rg.FirstColumn, rg.LastColumn = 0, maxColumns-1
		} else {
			rg.FirstRow, rg.LastRow = 0, maxRows-1
		}
		return rg
	}
	if rows {
		rg.FirstRow, rg.LastRow = min(rg.FirstRow, first), max(rg.LastRow, last)
	} else {
		rg.FirstColumn, rg.LastColumn = min(rg.FirstColumn, first), max(rg.LastColumn, last)
	}
	return rg
}

// colLeft returns the left edge of column c from the sheet's origin.
func (ts *tableState) colLeft(c int) float64 {
	if c < len(ts.colX) {
		return ts.colX[c]
	}
	return ts.colX[len(ts.colX)-1] + float64(c-len(ts.colX)+1)*ts.sh.DefaultColumnWidth
}

// rowsOf reads the rows of el and of its row groups and header rows
// (ODF 1.3, Part 3, 9.1.3, 9.1.7, 9.1.9 and 9.1.10).
func (r *reader) rowsOf(ts *tableState, el *odf.Element, hidden, header bool, depth int) {
	for _, c := range el.Children {
		if !r.tick() || ts.row >= maxRows {
			return
		}
		switch {
		case c.Is(odf.NSTable, "table-row"):
			r.row(ts, c, hidden, header)
		case c.Is(odf.NSTable, "table-rows"):
			r.rowsOf(ts, c, hidden, header, depth+1)
		case c.Is(odf.NSTable, "table-header-rows"):
			r.rowsOf(ts, c, hidden, true, depth+1)
		case c.Is(odf.NSTable, "table-row-group"):
			if depth < maxGroupDepth {
				r.rowsOf(ts, c, hidden || c.A(odf.NSTable, "display") == "false", header, depth+1)
			}
		}
	}
}

// row reads one table:table-row (ODF 1.3, Part 3, 9.1.3) and its
// repetitions: a row holding cells is listed for every repetition, up to
// the cell cap; a row without cells only when its height, hidden flag or
// break is its own.
func (r *reader) row(ts *tableState, el *odf.Element, hidden, header bool) {
	n := repeat(el, "number-rows-repeated", maxRows-ts.row)
	styleName := el.A(odf.NSTable, "style-name")
	props := r.styles.Resolve(odf.ScopeContent, odf.FamilyRow, styleName, odf.RowProps)
	tmpl := sheet.Row{
		Hidden: hidden || el.A(odf.NSTable, "visibility") == "collapse" || el.A(odf.NSTable, "visibility") == "filter",
		Style:  r.cellStyle(el.A(odf.NSTable, "default-cell-style-name")),
	}
	if h, ok := odf.Length(props.Get("style:row-height")); ok && h > 0 {
		tmpl.Height = h
		ts.lastRowHeight = h
	}
	// style:use-optimal-row-height false keeps the height the user set
	// (ODF 1.3, Part 3, 20.391).
	tmpl.CustomHeight = props.Get("style:use-optimal-row-height") == "false" && tmpl.Height > 0
	brk := props.Get("fo:break-before") == "page"
	if header {
		ts.headerRows = extend(ts.headerRows, ts.row, ts.row+n-1, true)
	}
	start := ts.row
	cells, merges, frames := r.rowCells(ts, el)
	if n >= visAttrStop && !hasValue(cells) {
		// A long run of rows whose cells only carry a style: Calc leaves
		// it out of the used range (SC_VISATTR_STOP).
		cells, merges = nil, nil
	}
	height := tmpl.Height
	if height == 0 {
		height = defaultRowHeight
	}
	if tmpl.Hidden {
		height = 0
	}
	for i := 0; brk && i < n && len(ts.sh.Page.RowBreaks) < maxBreaks; i++ {
		if start+i > 0 {
			ts.sh.Page.RowBreaks = append(ts.sh.Page.RowBreaks, start+i)
		}
	}
	ts.rowHidden = tmpl.Hidden
	for _, f := range frames {
		r.drawing(ts, f.el, &f.at)
	}
	ts.rowHidden = false
	if len(cells) == 0 {
		// A row without cells is listed for a height or a hidden flag of
		// its own alone, once a later row needs its index.
		if tmpl.Hidden || tmpl.Height > 0 && math.Abs(tmpl.Height-defaultRowHeight) > rowHeightSlack {
			row := tmpl
			row.Index = start
			ts.pending = append(ts.pending, pendingRows{row: row, n: n})
		}
		ts.row = start + n
		ts.rowY += float64(n) * height
		return
	}
	for i := range n {
		if r.cells+len(cells) > sheet.MaxCells || r.rows >= maxRowEntries {
			r.warn("too-many-cells", fmt.Sprintf("cells past %d are not read", sheet.MaxCells))
			break
		}
		idx := start + i
		r.flushPending(ts, idx)
		row := tmpl
		row.Index = idx
		row.Cells = cells
		if i > 0 {
			row.Cells = append([]sheet.Cell(nil), cells...)
		}
		if err := limits.BudgetFrom(r.ctx).ChargeUnits(int64(len(cells)), sheet.CellCost, "cells"); err != nil {
			// The refusal sticks and cancels the conversion, which stops at
			// the reader's next context check and reports it.
			break
		}
		r.cells += len(cells)
		r.rows++
		ts.sh.Rows = append(ts.sh.Rows, row)
		for _, m := range merges {
			if r.merges >= maxMerges {
				break
			}
			r.merges++
			ts.sh.Merges = append(ts.sh.Merges, sheet.Range{FirstRow: idx, FirstColumn: m.FirstColumn, LastRow: min(idx+m.LastRow, maxRows-1), LastColumn: m.LastColumn})
		}
	}
	ts.row = start + n
	ts.rowY += float64(n) * height
}

// visAttrStop is the run of rows with equal attributes and no content
// below which Calc stops looking for visible attributes when it finds a
// sheet's used range: 84 rows (SC_VISATTR_STOP, sc/source/core/data
// attarray.cxx, "more than a default page size").
const visAttrStop = 84

// hasValue reports a cell with a value among cells.
func hasValue(cells []sheet.Cell) bool {
	for _, c := range cells {
		if c.Value.Kind != sheet.Empty {
			return true
		}
	}
	return false
}

// rowHeightSlack is the difference under which a row's height is the
// default's: 0.05 points, the rounding of the inches an ODS file writes
// (0.178in is 12.816 points for Calc's 12.8).
const rowHeightSlack = 0.05

// flushPending lists the pending rows without cells that come before the
// row index next.
func (r *reader) flushPending(ts *tableState, next int) {
	for _, p := range ts.pending {
		for i := 0; i < p.n && p.row.Index+i < next; i++ {
			if r.rows >= maxRowEntries {
				ts.pending = nil
				return
			}
			row := p.row
			row.Index += i
			r.rows++
			ts.sh.Rows = append(ts.sh.Rows, row)
		}
	}
	ts.pending = nil
}

// frameAt is a frame of a cell, with the cell it sits in.
type frameAt struct {
	el *odf.Element
	at sheet.Anchor
}

// rowCells reads the cells of a row element (ODF 1.3, Part 3, 9.1.4 and
// 9.1.5): those with a value, a visible style or a frame, the merges they
// start (row offsets in LastRow, relative to the row), and the frames
// anchored in them.
func (r *reader) rowCells(ts *tableState, el *odf.Element) ([]sheet.Cell, []sheet.Range, []frameAt) {
	var cells []sheet.Cell
	var merges []sheet.Range
	var frames []frameAt
	col := 0
	for _, c := range el.Children {
		if !r.tick() || col >= maxColumns {
			break
		}
		covered := c.Is(odf.NSTable, "covered-table-cell")
		if !covered && !c.Is(odf.NSTable, "table-cell") {
			continue
		}
		n := repeat(c, "number-columns-repeated", maxColumns-col)
		if covered {
			col += n
			continue
		}
		cell, ok := r.cell(c)
		cs := repeat(c, "number-columns-spanned", maxColumns-col)
		rs := repeat(c, "number-rows-spanned", maxRows-ts.row)
		for _, k := range c.Children {
			switch {
			case k.Name.Space == odf.NSDraw:
				frames = append(frames, frameAt{el: k, at: sheet.Anchor{Row: ts.row, Column: col}})
			case k.Is(odf.NSOffice, "annotation"):
				r.warn("comments", "cell comments are not printed")
			case k.Is(odf.NSTable, "detective"):
				for _, h := range k.Children {
					if h.Is(odf.NSTable, "highlighted-range") && h.A(odf.NSTable, "marked-invalid") == "true" {
						cell.MarkedInvalid = true
					}
				}
			}
		}
		for i := range n {
			if ok && len(cells) < sheet.MaxCells {
				cell.Column = col + i
				cells = append(cells, cell)
			}
			if cs > 1 || rs > 1 {
				merges = append(merges, sheet.Range{FirstColumn: col + i, LastColumn: min(col+i+cs-1, maxColumns-1), LastRow: rs - 1})
			}
		}
		col += n
	}
	return cells, merges, frames
}
