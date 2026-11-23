package sheet

import (
	"context"
	"math"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// Calc's print headings (sc/source/ui/inc/printfun.hxx): the row number
// column is 1 cm wide and the column letter row 12.8 points tall, drawn in
// the default cell font, Liberation Sans 10 points, with black frames.
const (
	headingWidth    = 72 / 2.54
	headingHeight   = 12.8
	headingFontSize = 10
)

// excelGridLine is the room Excel gives the grid line beside a page's
// columns and below its rows, in points: a screen pixel drawn as 8
// printer pixels, kept whatever the print scale. The specification gives
// a page's printable width and height as the paper less its margins
// (ECMA-376 Part 1, 18.3.1.62); the line's room is Excel's, measured.
//
// # Corpus evidence
//
//   - poi/xlsx: 54288.xlsx, Arial 10 columns J to S of 4060 printer pixels
//     on an A4 page with 0.75-inch margins, 4061 pixels of room, prints S
//     on the next page.
//   - litho/xlsx: minimal and column_widths clip their cells from 0.96
//     points right of the left margin, at 100% and at 77.6%.
//   - litho/xlsx: the 30 paper-grid sheets whose second page is full print
//     rows of 121 printer pixels as many a page as fit their paper less
//     the margins and from 5 to 11 pixels more:
//     paper-grid-margins-narrow-a5-portrait fits 35 in 4,361 pixels (36
//     take 4,356), paper-grid-b5-landscape 28 in 3,399 (28 take 3,388).
const excelGridLine = 8 * excelPixel

// The print zoom bounds, in percent: Calc's ZOOM_MIN
// (sc/source/ui/view/printfun.cxx) and the largest scale its dialog
// takes.
const (
	minZoom = 10
	maxZoom = 400
)

// band is a run of rows or columns printed on one page.
type band struct {
	first, last int
}

// pagePlan is one printed page: the sheet, the rows and columns of its
// bands and the repeated ones it shows, its zoom.
type pagePlan struct {
	sp         *sheetPlan
	area       Range
	rows, cols band
	// repRows and repCols are the repeated rows and columns the page
	// shows before its bands, nil for none.
	repRows, repCols *band
	// number is the page number its page fields show.
	number int
	// notes, when not nil, makes the page one of the sheet's pages of
	// notes, laid out on the sheet's paper ([builder.notePages]): it
	// shows no cell. noteTags, on the first, holds the structure of all.
	notes    *typeset.Page
	noteTags *typeset.StructElem
}

// sheetPlan is how one sheet prints.
type sheetPlan struct {
	g   *grid
	idx int
	// zoom is the print scale, 1 for 100%.
	zoom float64
	// width and height are the paper, margin its margins.
	width, height float64
	margin        [4]float64
	// cuts are the print ranges cut into pages, full the pages of cells
	// they print by the sheet's program's rule; pages are the pages
	// [sheetPlan.emit] makes, notes the sheet's pages of notes, which
	// follow them, and capped reports cells past [maxSheetPages] left out.
	cuts   []cut
	full   int64
	pages  []*pagePlan
	notes  []*pagePlan
	capped bool
	// single reports a page sized to the sheet's content.
	single bool
	// fitting reports a layout zoomFor tries.
	fitting bool
	// first is the first page of the first print range, printed or not:
	// what a book without a printed page shows; firstRows the first row
	// band layoutPages cut, kept or not.
	first     *pagePlan
	firstRows *band
}

// docArea returns the room for cells on a page at zoom percent, in sheet
// points: the paper less the margins, divided by the zoom, less the
// headings (ScPrintFunc::GetDocPageSize), the room of a header and a
// footer in the margins unscaled. Under Excel's rules the margins divide
// whole, and the grid line that closes a page's last column and the one
// below its last row take their room too, unscaled ([excelGridLine]).
//
// # Corpus evidence
//
//   - litho/xlsx paper-fit-a5-landscape: fitted to the width of an A5
//     page at a scale below 100, Excel starts the cells at the 0.75 inch
//     top margin and prints 36 rows a page, where the header's room
//     scaled would start them 15 points higher and fit 40.
func (sp *sheetPlan) docArea(zoom int) (float64, float64) {
	z := float64(zoom) / 100
	w := (sp.width - sp.margin[1] - sp.margin[3]) / z
	if sp.g.doc.ExcelRules {
		w -= excelGridLine / z
	}
	// A header's and a footer's room is unscaled: only the page margins
	// beyond them are divided by the zoom.
	top, bottom := sp.margin[0]/z, sp.margin[2]/z
	ps := &sp.g.sh.Page
	hf := &sp.g.sh.HeaderFooter
	if sp.g.doc.ExcelRules {
		// Excel keeps the margins whole: no header or footer room.
		hf = &HeaderFooter{}
	}
	if !sp.single && headerShown(hf, false) && ps.HeaderTop >= 0 && ps.HeaderTop < sp.margin[0] {
		top = ps.HeaderTop/z + sp.margin[0] - ps.HeaderTop
	}
	if !sp.single && headerShown(hf, true) && ps.FooterBottom >= 0 && ps.FooterBottom < sp.margin[2] {
		bottom = ps.FooterBottom/z + sp.margin[2] - ps.FooterBottom
	}
	h := sp.height/z - top - bottom
	if sp.g.doc.ExcelRules {
		h -= excelGridLine / z
	}
	if sp.g.sh.Page.Headings {
		w -= headingWidth
		h -= headingHeight
	}
	return w, h
}

// columnCap returns the widest a column prints under Excel's rules, in
// sheet points: the paper less its side margins and the headings, at the
// sheet's scale; 0, no bound, under Calc's rules and for a sheet fitted
// to pages, whose zoom is not known yet. Excel cuts a wider column to the
// page, and the text of its cells overflows from the narrower column. No
// clause of ECMA-376 bounds a column's width by the page (18.3.1.13 col
// width stops at 255 characters); the rule is Excel's, measured.
//
// # Corpus evidence
//
//   - litho/xlsx: wide_columns, columns A and B of 255 and 120 characters
//     on a Letter page of 0.7 inch margins, prints A on page 1 and B on
//     page 2, and the text of A2 on pages 2 and 3 starting 511.3 and
//     1022.6 points left of the margin: both columns print 511.2 points
//     wide, and the text overflows over B into C.
func (sp *sheetPlan) columnCap() float64 {
	ps := &sp.g.sh.Page
	if !sp.g.doc.ExcelRules || sp.single || (ps.Scale <= 0 && (ps.FitWidth > 0 || ps.FitHeight > 0)) {
		return 0
	}
	z := 1.0
	if ps.Scale > 0 {
		z = float64(min(max(ps.Scale, minZoom), maxZoom)) / 100
	}
	w := (sp.width - sp.margin[css.Left] - sp.margin[css.Right]) / z
	if ps.Headings {
		w -= headingWidth
	}
	return max(w, 0)
}

// layoutPages cuts the range a into pages at zoom percent, as Calc's
// UpdatePageBreaks and PrintPageRanges::calculate do: column and row
// bands at the room a page leaves, the repeated rows and columns taking
// theirs from every band after them, manual breaks unless the sheet fits
// that direction to pages, and the bands of rows without content left out.
// It returns the column bands and, per row band kept, which column bands
// hold content. Under Excel's rules it keeps every band and returns no
// such flags. ctx's error, checked every [rowsPerCheck] rows or columns,
// stops it.
func (sp *sheetPlan) layoutPages(ctx context.Context, a Range, zoom int) ([]band, []band, [][]bool, error) {
	g := sp.g
	ps := &g.sh.Page
	pageW, pageH := sp.docArea(zoom)
	fitW := ps.Scale <= 0 && ps.FitWidth > 0
	fitH := ps.Scale <= 0 && ps.FitHeight > 0
	colBreak := map[int]bool{}
	if !fitW && !sp.single {
		for _, c := range ps.ColumnBreaks {
			colBreak[c] = true
		}
	}
	rowBreak := map[int]bool{}
	if !fitH && !sp.single {
		for _, r := range ps.RowBreaks {
			rowBreak[r] = true
		}
	}
	starts := map[int]bool{}
	var repC *band
	if rc := ps.RepeatColumns; rc != nil {
		repC = &band{first: min(max(rc.FirstColumn, 0), maxColumns-1), last: min(max(rc.LastColumn, 0), maxColumns-1)}
	}
	size := 0.0
	colFound := false
	if g.doc.ExcelRules && sp.fitting && repC != nil {
		// Excel fits the print titles beside every page's columns, the
		// first page's too ([sheetPlan.zoomFor]).
		for i := repC.first; i <= repC.last; i++ {
			pageW -= g.layoutColWidth(i)
		}
		colFound = true
	}
	for c := a.FirstColumn; c <= a.LastColumn; c++ {
		if (c-a.FirstColumn)%rowsPerCheck == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, nil, err
			}
		}
		start := false
		w := g.layoutColWidth(c)
		if (size+w > pageW+epsilon && !sp.single) || colBreak[c] {
			if c != a.FirstColumn {
				starts[c] = true
			}
			size = 0
			start = true
		} else if c == a.FirstColumn {
			start = true
		}
		if start && repC != nil && c > repC.first && !colFound {
			for i := repC.first; i <= repC.last; i++ {
				pageW -= g.layoutColWidth(i)
			}
			// Calc moves on past the repeated columns when a page
			// starts among them, the next column's width uncounted.
			for c <= repC.last {
				c++
				delete(starts, c)
			}
			colFound = true
		}
		size += w
	}
	var cols []band
	vis := false
	from := a.FirstColumn
	for c := a.FirstColumn; c <= a.LastColumn; c++ {
		if c > a.FirstColumn && vis && starts[c] {
			cols = append(cols, band{from, c - 1})
			from = c
			vis = false
		}
		if !g.colHidden(c) {
			vis = true
		}
	}
	if vis {
		cols = append(cols, band{from, a.LastColumn})
	}
	var repR *band
	if rr := ps.RepeatRows; rr != nil {
		repR = &band{first: min(max(rr.FirstRow, 0), maxRows-1), last: min(max(rr.LastRow, 0), maxRows-1)}
	}
	rstarts := map[int]bool{}
	size = 0
	rowFound := false
	for r := a.FirstRow; r <= a.LastRow; r++ {
		if (r-a.FirstRow)%rowsPerCheck == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, nil, err
			}
		}
		start := false
		h := sp.pagedRowHeight(r, zoom)
		if (size+h > pageH+epsilon && !sp.single) || rowBreak[r] {
			if r != a.FirstRow {
				rstarts[r] = true
			}
			size = 0
			start = true
		} else if r == a.FirstRow {
			start = true
		}
		if start && repR != nil && r > repR.first && !rowFound {
			for i := repR.first; i <= repR.last; i++ {
				pageH -= sp.pagedRowHeight(i, zoom)
			}
			for i := r; i <= repR.last; i++ {
				delete(rstarts, i)
			}
			rowFound = true
		}
		size += h
	}
	var rows []band
	var hidden [][]bool
	vis = false
	from = a.FirstRow
	flush := func(last int) {
		b := band{from, last}
		if sp.firstRows == nil {
			sp.firstRows = &b
		}
		if g.doc.ExcelRules {
			// Excel keeps every band: [sheetPlan.lastPrinted] cuts the
			// pages after the last that holds something.
			rows = append(rows, b)
			return
		}
		if !sp.g.empty(Range{FirstRow: b.first, LastRow: b.last, FirstColumn: a.FirstColumn, LastColumn: a.LastColumn}) {
			h := make([]bool, len(cols))
			leftEmpty := false
			for i, cb := range cols {
				h[i] = sp.g.empty(Range{FirstRow: b.first, LastRow: b.last, FirstColumn: cb.first, LastColumn: cb.last})
				// A page that text overflows into from the left prints,
				// unless the page left of it does not (ScDocument::IsPrintEmpty).
				if h[i] && !leftEmpty && cb.first > 0 && sp.g.extendPrint(cb.first-1, b.first, b.last) >= cb.first {
					h[i] = false
				}
				leftEmpty = h[i]
			}
			rows = append(rows, b)
			hidden = append(hidden, h)
		}
	}
	for r := a.FirstRow; r <= a.LastRow; r++ {
		if (r-a.FirstRow)%rowsPerCheck == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, nil, err
			}
		}
		if r > a.FirstRow && vis && rstarts[r] {
			flush(r - 1)
			from = r
			vis = false
		}
		if !g.rowHidden(r) {
			vis = true
		}
	}
	if vis {
		flush(a.LastRow)
	}
	return cols, rows, hidden, nil
}

// pagedRowHeight returns the room row r takes on a page at zoom percent,
// in sheet points: its height, which under Excel's rules a zoom other than
// 100 prints rounded to a whole printer pixel, row by row.
//
// # Corpus evidence
//
//   - litho/xlsx: paper-fit and paper-fit-letter-portrait, fitted to the
//     width of a Letter page at 60%, print rows of 121 printer pixels 73
//     apart (72.6 rounded) and 76 rows under the header row on the first
//     page, where rows of 72.6 would fit 77; paper-fit-a3-portrait at 87%
//     prints them 105 apart (105.27), paper-fit-a5-landscape at 58% 70
//     (70.18), fit_to_page at 39% 47 (47.19), and draws them so.
func (sp *sheetPlan) pagedRowHeight(r, zoom int) float64 {
	h := sp.g.layoutRowHeight(r)
	if !sp.g.doc.ExcelRules || zoom == 100 || zoom <= 0 || sp.single {
		return h
	}
	return excelZoomed(h, float64(zoom)/100)
}

// excelZoomed returns a row of h points as Excel prints it at the scale
// z: its height at the scale rounded to a printer pixel, in sheet points
// ([sheetPlan.pagedRowHeight]).
func excelZoomed(h, z float64) float64 {
	if !numeric.Positive(z) {
		return h
	}
	return math.Round(float64(h*z)/excelPixel) * excelPixel / z
}

// epsilon absorbs the rounding of sums of lengths in points.
const epsilon = 1e-6

// empty reports a range Calc does not print a page for: no cell in it
// shows a value or draws a border, hidden rows and columns aside, and no
// drawing overlaps it (ScDocument::IsPrintEmpty, which prints "sheets with
// borders even if there is no cell content"). Under Excel's rules a cell
// whose own style paints a fill or a border, or whose file stores an
// empty string, counts as a value: Excel prints a page of formatted cells
// alone.
//
// # Corpus evidence
//
//   - libreoffice/ods: sc/tdf104502_hiddenColsCountedInPageCount.ods, values below
//     the first page in a hidden column alone, prints one page;
//     sc/tdf103734.ods prints its fifth and sixth pages, which hold
//     filled and bordered cells without a value.
//   - probe, ods: a value in A1, J1 and J2 with a border, M6 with one,
//     on A4 at seven columns a page, prints the bordered cells on a second
//     page; with a fill in J1 and J2 in place of the borders, one page.
func (g *grid) empty(a Range) bool {
	i := sort.SearchInts(g.rowOrder, a.FirstRow)
	for ; i < len(g.rowOrder) && g.rowOrder[i] <= a.LastRow; i++ {
		row := g.rows[g.rowOrder[i]]
		if row.Hidden {
			continue
		}
		cells := row.Cells
		j := sort.Search(len(cells), func(j int) bool { return cells[j].Column >= a.FirstColumn })
		for ; j < len(cells) && cells[j].Column <= a.LastColumn; j++ {
			if c := &cells[j]; g.shows(c) && !g.colHidden(c.Column) {
				return false
			}
		}
	}
	for k := range g.sh.Drawings {
		if r, ok := g.drawingCells(&g.sh.Drawings[k]); ok && r.FirstRow <= a.LastRow && r.LastRow >= a.FirstRow &&
			r.FirstColumn <= a.LastColumn && r.LastColumn >= a.FirstColumn {
			return false
		}
	}
	return true
}

// shows reports a cell that makes its page print ([grid.empty]): one with
// a value or a border, and under Excel's rules one whose style paints or
// whose file stores an empty string.
func (g *grid) shows(c *Cell) bool {
	return hasContent(c) || lined(c.Style) || (g.doc.ExcelRules && (c.EmptyText || visible(c.Style)))
}

// lastPrinted returns the row and column bands of the last page Excel
// prints of a range cut into the bands rows and cols, in the sheet's page
// order: the last that holds a cell [grid.empty] counts, a drawing, text
// overflowing from the left, or repeated rows or columns that hold one;
// (-1, -1) when none does. Excel prints every page of the range before
// it, the blank ones too, and none after it. No clause of ECMA-376 states
// which pages print; the rule is Excel's, measured.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/tdf137091.xlsx prints 20 pages of A1:CV52, 18
//     of them blank; sc/invalid_ext_data_validation.xlsx prints the blank
//     pages of its second band of rows but the last, 13 pages of 14;
//     chart2/fdo70609.xlsx and sc/image_hyperlink.xlsx print a blank page
//     left of a drawing.
//   - poi/xlsx: 61652.xlsx prints 15 pages of its Model sheet, 2 bands of
//     rows by 8 of columns, the last page left out; 47737.xlsx prints a
//     last page holding the repeated rows alone; 62629_toMerge.xlsx
//     prints eight pages blank but for the footer before its table.
//   - litho/xlsx: sparse_far_cells prints the blank page between A1 and
//     D120.
//   - litho/xlsx: merged_cells prints a second page for the rows of its
//     merged cell A15:B18 below a manual break after row 16, the lower
//     half of its centred text on it.
func (sp *sheetPlan) lastPrinted(ctx context.Context, a Range, rows, cols []band) (int, int, error) {
	ri, ci, err := sp.lastHolding(ctx, a, rows, cols)
	if err != nil {
		return 0, 0, err
	}
	mr, mc := sp.lastMerged(rows, cols)
	if mr < 0 {
		return ri, ci, nil
	}
	later := mc > ci || (mc == ci && mr > ri)
	if sp.g.sh.Page.Order == OverThenDown {
		later = mr > ri || (mr == ri && mc > ci)
	}
	if later {
		return mr, mc, nil
	}
	return ri, ci, nil
}

// lastMerged returns the row and column bands of the last page, in the
// sheet's page order, that a merged cell showing something reaches, any
// merged cell under Excel's rules, as [grid.usedRange] counts them: the
// page of its bottom-right cell, the merge clipped to the bands; (-1, -1)
// when none does. A merged cell prints on every page it reaches, as
// [sheetPlan.lastPrinted] says.
func (sp *sheetPlan) lastMerged(rows, cols []band) (int, int) {
	g := sp.g
	if len(rows) == 0 || len(cols) == 0 {
		return -1, -1
	}
	at := func(bs []band, v int) int {
		i := sort.Search(len(bs), func(i int) bool { return bs[i].last >= v })
		return min(i, len(bs)-1)
	}
	lastR, lastC := -1, -1
	for at0, m := range g.merges {
		c := g.cell(at0[0], at0[1])
		if !g.doc.ExcelRules && (c == nil || !g.shows(c)) || m.LastRow < rows[0].first || m.LastColumn < cols[0].first {
			continue
		}
		r, cc := at(rows, m.LastRow), at(cols, m.LastColumn)
		later := cc > lastC || (cc == lastC && r > lastR)
		if g.sh.Page.Order == OverThenDown {
			later = r > lastR || (r == lastR && cc > lastC)
		}
		if later {
			lastR, lastC = r, cc
		}
	}
	return lastR, lastC
}

// lastHolding returns the row and column bands of the last page that
// holds something, in the sheet's page order, merged cells aside
// ([sheetPlan.lastPrinted]).
func (sp *sheetPlan) lastHolding(ctx context.Context, a Range, rows, cols []band) (int, int, error) {
	g := sp.g
	ps := &g.sh.Page
	var repR, repC *Range
	if rr := ps.RepeatRows; rr != nil {
		repR = &Range{FirstRow: max(rr.FirstRow, 0), LastRow: min(rr.LastRow, maxRows-1)}
	}
	if rc := ps.RepeatColumns; rc != nil {
		repC = &Range{FirstColumn: max(rc.FirstColumn, 0), LastColumn: min(rc.LastColumn, maxColumns-1)}
	}
	// holds reports whether the cells of rb by cb print something, or the
	// repeated rows or columns, which every page that reaches past their
	// first shows. A band that runs the whole range stands for a strip of
	// pages.
	holds := func(rb, cb band) bool {
		if !g.empty(Range{FirstRow: rb.first, LastRow: rb.last, FirstColumn: cb.first, LastColumn: cb.last}) {
			return true
		}
		if cb.first > 0 && g.extendPrint(cb.first-1, rb.first, rb.last) >= cb.first {
			return true
		}
		if repR != nil && rb.last > repR.FirstRow && !g.empty(Range{FirstRow: repR.FirstRow, LastRow: repR.LastRow, FirstColumn: cb.first, LastColumn: cb.last}) {
			return true
		}
		return repC != nil && cb.last > repC.FirstColumn && !g.empty(Range{FirstRow: rb.first, LastRow: rb.last, FirstColumn: repC.FirstColumn, LastColumn: repC.LastColumn})
	}
	allRows, allCols := band{a.FirstRow, a.LastRow}, band{a.FirstColumn, a.LastColumn}
	if ps.Order == OverThenDown {
		for ri := len(rows) - 1; ri >= 0; ri-- {
			if !holds(rows[ri], allCols) {
				continue
			}
			for ci := len(cols) - 1; ci >= 0; ci-- {
				if (len(cols)-1-ci)%rowsPerCheck == 0 {
					if err := ctx.Err(); err != nil {
						return 0, 0, err
					}
				}
				if holds(rows[ri], cols[ci]) {
					return ri, ci, nil
				}
			}
		}
		return -1, -1, nil
	}
	for ci := len(cols) - 1; ci >= 0; ci-- {
		if !holds(allRows, cols[ci]) {
			continue
		}
		for ri := len(rows) - 1; ri >= 0; ri-- {
			if (len(rows)-1-ri)%rowsPerCheck == 0 {
				if err := ctx.Err(); err != nil {
					return 0, 0, err
				}
			}
			if holds(rows[ri], cols[ci]) {
				return ri, ci, nil
			}
		}
	}
	return -1, -1, nil
}

// zoomFor returns the print zoom of the range a in percent: the sheet's
// scale, or the largest zoom up to 100 at which the range fits the pages
// across and down it asks for, found as Calc finds it
// (ScPrintFunc::CalcZoom): a bisection over whole percents, then for a
// sheet fitted to a width alone 2% less when that saves a page down.
// Under Excel's rules the width fitted holds the print titles beside the
// columns of every page, the first one's too, and a manual break adds no
// page to those the sheet asks for: Excel ignores manual breaks on a
// sheet it fits to pages. ctx's error stops it.
//
// # Corpus evidence
//
//   - poi/xlsx: AverageTaxRates.xlsx fits its first sheet, of columns A
//     to T with a manual break after K, to one page, which Excel prints
//     on one page, its 11-point text at 7.8 points as on the sheets
//     without a break.
//   - probe, xlsx: sheets of A4 landscape fitted one page wide, columns A
//     to O of 7,478 printer pixels in 6,168 of room (the grid line's 8
//     out), print in Excel for the web at 82% (0.825), at 71% with A as
//     print titles (over 8,606: A's 1,128 counted again), 82% with a print
//     title row, 77% with a picture to the edge of column P (7,908, P's
//     430 in the range: 0.780) and 68% with both (9,036).
//   - litho/xlsx: real_budget, print titles A and 3 and charts to the edge
//     of P, prints at about 68%.
func (sp *sheetPlan) zoomFor(ctx context.Context, a Range) (int, error) {
	ps := &sp.g.sh.Page
	if sp.single {
		return 100, nil
	}
	if ps.Scale > 0 {
		return min(max(ps.Scale, minZoom), maxZoom), nil
	}
	nW, nH := max(ps.FitWidth, 0), max(ps.FitHeight, 0)
	if nW == 0 && nH == 0 {
		return 100, nil
	}
	if !sp.g.doc.ExcelRules {
		nW = max(nW, len(ps.ColumnBreaks)+1)
		nH = max(nH, len(ps.RowBreaks)+1)
	}
	if ps.FitWidth == 0 {
		nW = 0
	}
	if ps.FitHeight == 0 {
		nH = 0
	}
	var err error
	sp.fitting = true
	defer func() { sp.fitting = false }()
	fits := func(z int) (bool, int) {
		cols, rows, _, lerr := sp.layoutPages(ctx, a, z)
		if lerr != nil {
			err = lerr
		}
		return (nW == 0 || len(cols) <= nW) && (nH == 0 || len(rows) <= nH), len(rows)
	}
	zoom := 100
	lastFit, lastNonFit := 0, 0
	for zoom > minZoom {
		ok, _ := fits(zoom)
		if err != nil {
			return 0, err
		}
		if ok {
			if zoom == 100 {
				break
			}
			lastFit = zoom
			zoom = (lastNonFit + zoom) / 2
			if lastFit == zoom {
				break
			}
			continue
		}
		if zoom-lastFit <= 1 {
			zoom = lastFit
			break
		}
		lastNonFit = zoom
		zoom = (lastFit + zoom) / 2
	}
	zoom = max(zoom, minZoom)
	if nW > 0 && nH == 0 {
		if _, pagesY := fits(zoom); pagesY > 1 {
			smaller := int(float64(zoom) * 0.98)
			if smaller < zoom && smaller >= minZoom {
				if _, y := fits(smaller); y != pagesY {
					zoom = smaller
				}
			}
		}
	}
	return zoom, err
}

// plan cuts the sheet into pages: per print range, its zoom, then its
// row and column bands and which of their pages the sheet's program
// prints, counted in full. [sheetPlan.emit] makes the pages. ctx's error
// stops it.
func (sp *sheetPlan) plan(ctx context.Context) error {
	g := sp.g
	ps := &g.sh.Page
	var areas []Range
	if len(ps.PrintAreas) > 0 {
		for _, a := range ps.PrintAreas {
			a = clampRange(a)
			if a.LastRow >= a.FirstRow && a.LastColumn >= a.FirstColumn {
				areas = append(areas, a)
			}
		}
	} else if u, ok := g.usedRange(sp.single); ok {
		areas = []Range{u}
	}
	for _, a := range areas {
		zoom, err := sp.zoomFor(ctx, a)
		if err != nil {
			return err
		}
		sp.zoom = float64(zoom) / 100
		if !sp.single {
			g.setZoom(sp.zoom)
		}
		sp.firstRows = nil
		cols, rows, hidden, err := sp.layoutPages(ctx, a, zoom)
		if err != nil {
			return err
		}
		c := cut{a: a, rows: rows, cols: cols, hidden: hidden}
		if g.doc.ExcelRules {
			if c.lastR, c.lastC, err = sp.lastPrinted(ctx, a, rows, cols); err != nil {
				return err
			}
		}
		if sp.first == nil && sp.firstRows != nil && len(cols) > 0 {
			sp.first = &pagePlan{sp: sp, area: a, rows: *sp.firstRows, cols: cols[0]}
		}
		sp.cuts = append(sp.cuts, c)
		sp.full += sp.count(&c)
	}
	return nil
}

// cut is a print range cut into pages: its row and column bands, and
// which of their pages the sheet's program prints.
type cut struct {
	a          Range
	rows, cols []band
	// hidden flags, under Calc's rules, the pages of each row band that
	// hold nothing, which Calc leaves out.
	hidden [][]bool
	// lastR and lastC are, under Excel's rules, the bands of the last page
	// Excel prints ([sheetPlan.lastPrinted]), -1 for none.
	lastR, lastC int
}

// pageKey names the page of row band ri and column band ci of a cut.
type pageKey struct{ ri, ci int }

// shown reports whether the sheet's program prints the page of row band
// ri and column band ci of c: under Excel's rules every page up to the
// last that holds something, in the page order; under Calc's those that
// hold something.
func (sp *sheetPlan) shown(c *cut, ri, ci int) bool {
	if !sp.g.doc.ExcelRules {
		return !c.hidden[ri][ci]
	}
	if sp.g.sh.Page.Order == OverThenDown {
		return ri < c.lastR || (ri == c.lastR && ci <= c.lastC)
	}
	return ci < c.lastC || (ci == c.lastC && ri <= c.lastR)
}

// page returns the page of row band ri and column band ci of c, its bands
// less the repeated rows and columns it starts among, nil when nothing of
// them is left.
func (sp *sheetPlan) page(c *cut, ri, ci int) *pagePlan {
	ps := &sp.g.sh.Page
	p := &pagePlan{sp: sp, area: c.a, rows: c.rows[ri], cols: c.cols[ci]}
	if rc := ps.RepeatColumns; rc != nil {
		p.cols, p.repCols = repeated(p.cols, max(rc.FirstColumn, 0), min(rc.LastColumn, maxColumns-1))
	}
	if rr := ps.RepeatRows; rr != nil {
		p.rows, p.repRows = repeated(p.rows, max(rr.FirstRow, 0), min(rr.LastRow, maxRows-1))
	}
	if p.cols.first > p.cols.last || p.rows.first > p.rows.last {
		return nil
	}
	return p
}

// repeated returns the band b a page shows once the repeated lines first
// to last take the place of those of b it starts among, and the repeated
// band the page shows before it, nil for none.
func repeated(b band, first, last int) (band, *band) {
	b = trimRepeated(b, first, last)
	if b.first > last && first <= last {
		return b, &band{first, last}
	}
	return b, nil
}

// trimRepeated returns the band b less the repeated lines first to last
// it starts among.
func trimRepeated(b band, first, last int) band {
	if b.first > first && b.first <= last {
		b.first = last + 1
	}
	return b
}

// count returns how many pages of c the sheet's program prints
// ([sheetPlan.shown]) that [sheetPlan.page] keeps. Under Excel's rules it
// counts by arithmetic, never page by page, so a range of millions of
// blank pages costs its bands alone; the count of the format's largest
// grid, 2^20 rows by 2^14 columns, fits an int64.
func (sp *sheetPlan) count(c *cut) int64 {
	ps := &sp.g.sh.Page
	keptR := func(ri int) bool {
		if rr := ps.RepeatRows; rr != nil {
			b := trimRepeated(c.rows[ri], max(rr.FirstRow, 0), min(rr.LastRow, maxRows-1))
			return b.first <= b.last
		}
		return true
	}
	keptC := func(ci int) bool {
		if rc := ps.RepeatColumns; rc != nil {
			b := trimRepeated(c.cols[ci], max(rc.FirstColumn, 0), min(rc.LastColumn, maxColumns-1))
			return b.first <= b.last
		}
		return true
	}
	// sum counts the kept bands among the first n.
	sum := func(n int, kept func(int) bool) int64 {
		var k int64
		for i := range n {
			if kept(i) {
				k++
			}
		}
		return k
	}
	if !sp.g.doc.ExcelRules {
		cols := make([]bool, len(c.cols))
		for ci := range cols {
			cols[ci] = keptC(ci)
		}
		var n int64
		for ri := range c.rows {
			if !keptR(ri) {
				continue
			}
			for ci, ok := range cols {
				if ok && !c.hidden[ri][ci] {
					n++
				}
			}
		}
		return n
	}
	if c.lastR < 0 || c.lastC < 0 {
		return 0
	}
	one := func(ok bool) int64 {
		if ok {
			return 1
		}
		return 0
	}
	if ps.Order == OverThenDown {
		return sum(c.lastR, keptR)*sum(len(c.cols), keptC) + one(keptR(c.lastR))*sum(c.lastC+1, keptC)
	}
	return sum(c.lastC, keptC)*sum(len(c.rows), keptR) + one(keptC(c.lastC))*sum(c.lastR+1, keptR)
}

// emit makes the sheet's pages, in its page order, its pages of notes
// after them: every page its program prints, or, when sparse, those that
// hold something ([sheetPlan.contentPages]). It returns how many of the
// pages its program prints hold something, every page when not sparse.
// A sheet makes at most [maxSheetPages] pages of cells, capped reporting
// the rest left out. ctx's error stops it.
func (sp *sheetPlan) emit(ctx context.Context, sparse bool) (int64, error) {
	sp.pages, sp.capped = nil, false
	var held int64
	add := func(c *cut, ri, ci int) {
		if !sp.shown(c, ri, ci) {
			return
		}
		p := sp.page(c, ri, ci)
		if p == nil {
			return
		}
		held++
		if len(sp.pages) >= maxSheetPages {
			sp.capped = true
			return
		}
		sp.pages = append(sp.pages, p)
	}
	overThenDown := sp.g.sh.Page.Order == OverThenDown
	for i := range sp.cuts {
		c := &sp.cuts[i]
		if sparse {
			keys, err := sp.contentPages(ctx, c)
			if err != nil {
				return 0, err
			}
			for _, k := range keys {
				add(c, k.ri, k.ci)
			}
			continue
		}
		// outer and inner are the bands of the page order: columns then
		// rows down then over, rows then columns over then down. Under
		// Excel's rules no page past the last it prints is visited.
		outer, inner := len(c.cols), len(c.rows)
		lastO, lastI := c.lastC, c.lastR
		if overThenDown {
			outer, inner = inner, outer
			lastO, lastI = lastI, lastO
		}
		excel := sp.g.doc.ExcelRules
		if excel {
			outer = min(outer, lastO+1)
		}
		for o := 0; o < outer && !sp.capped; o++ {
			for in := 0; in < inner && !sp.capped && (!excel || o < lastO || in <= lastI); in++ {
				if overThenDown {
					add(c, o, in)
				} else {
					add(c, in, o)
				}
			}
		}
	}
	if !sparse {
		held = sp.full
	}
	sp.pages = append(sp.pages, sp.notes...)
	return held, nil
}

// maxSheetPages caps the pages of one sheet. Anti-DoS guard, not
// spec-mandated: a sheet of a million rows of one point prints under
// 20,000 pages of A4, and the document's page cap applies too.
const maxSheetPages = 1 << 15

// singleSize returns the content size of the sheet's first print range at
// 100%, for a page the size of its content: the sum of the layout widths
// and heights, which the cells, printed truncated ([calcPrinted]), may
// fall a little short of.
//
// # Corpus evidence
//
//   - gotenberg/xlsx: budget-single, budget.xlsx with singlePage, prints pages of
//     192.64 by 109.73, 674.14 by 915.02 and 48.19 by 45.04 points, the
//     sizes of the sheets' cells from A1, the first cell's text a point
//     from the page's left edge.
//   - probe, ods: 20 columns of 1,000 twips by 150 rows of 256, converted
//     by Gotenberg 9.4.1 with singlePage, print on a page of 1,000.03 by
//     1,920.02 points, the twips' sums, while the rows print 12.784 points
//     apart.
func (sp *sheetPlan) singleSize() (float64, float64) {
	if len(sp.pages) == 0 {
		return 0, 0
	}
	p := sp.pages[0]
	w, h := 0.0, 0.0
	for c := p.cols.first; c <= p.cols.last; c++ {
		w += sp.g.layoutColWidth(c)
	}
	for r := p.rows.first; r <= p.rows.last; r++ {
		h += sp.g.layoutRowHeight(r)
	}
	if sp.g.sh.Page.Headings {
		w += headingWidth
		h += headingHeight
	}
	return math.Max(w, 1), math.Max(h, 1)
}
