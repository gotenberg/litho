package sheet

import (
	"context"
	"math"
	"sort"
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
// columns, in points: a screen pixel drawn as 8 printer pixels, kept
// whatever the print scale. The specification gives a page's printable
// width as the paper less its margins (ECMA-376 Part 1, 18.3.1.62); the
// line's room is Excel's, measured.
//
// # Corpus evidence
//
//   - poi/xlsx: 54288.xlsx, Arial 10 columns J to S of 4060 printer pixels
//     on an A4 page with 0.75-inch margins, 4061 pixels of room, prints S
//     on the next page.
//   - litho/xlsx: minimal and column_widths clip their cells from 0.96
//     points right of the left margin, at 100% and at 77.6%.
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
	pages         []*pagePlan
	// single reports a page sized to the sheet's content.
	single bool
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
// whole, and the grid line that closes a page's last column takes its
// room too, unscaled ([excelGridLine]).
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
	if sp.g.sh.Page.Headings {
		w -= headingWidth
		h -= headingHeight
	}
	return w, h
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
		h := g.layoutRowHeight(r)
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
				pageH -= g.layoutRowHeight(i)
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
	excel := g.doc.ExcelRules
	i := sort.SearchInts(g.rowOrder, a.FirstRow)
	for ; i < len(g.rowOrder) && g.rowOrder[i] <= a.LastRow; i++ {
		row := g.rows[g.rowOrder[i]]
		if row.Hidden {
			continue
		}
		cells := row.Cells
		j := sort.Search(len(cells), func(j int) bool { return cells[j].Column >= a.FirstColumn })
		for ; j < len(cells) && cells[j].Column <= a.LastColumn; j++ {
			c := &cells[j]
			shows := hasContent(c) || lined(c.Style) || (excel && (c.EmptyText || visible(c.Style)))
			if shows && !g.colHidden(c.Column) {
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
func (sp *sheetPlan) lastPrinted(ctx context.Context, a Range, rows, cols []band) (int, int, error) {
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
// ctx's error stops it.
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
	nW = max(nW, len(ps.ColumnBreaks)+1)
	nH = max(nH, len(ps.RowBreaks)+1)
	if ps.FitWidth == 0 {
		nW = 0
	}
	if ps.FitHeight == 0 {
		nH = 0
	}
	var err error
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

// plan fills the pages of the sheet: per print range, its zoom, then its
// pages in the sheet's page order. ctx's error stops it.
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
		sp.firstRows = nil
		cols, rows, hidden, err := sp.layoutPages(ctx, a, zoom)
		if err != nil {
			return err
		}
		lastR, lastC := len(rows), len(cols)
		if g.doc.ExcelRules {
			if lastR, lastC, err = sp.lastPrinted(ctx, a, rows, cols); err != nil {
				return err
			}
		}
		if sp.first == nil && sp.firstRows != nil && len(cols) > 0 {
			sp.first = &pagePlan{sp: sp, area: a, rows: *sp.firstRows, cols: cols[0]}
		}
		// shown reports whether the page of row band ri and column band
		// ci prints: under Excel's rules every page up to the last that
		// holds something, in the page order; under Calc's those that
		// hold something.
		shown := func(ri, ci int) bool {
			if !g.doc.ExcelRules {
				return !hidden[ri][ci]
			}
			if ps.Order == OverThenDown {
				return ri < lastR || (ri == lastR && ci <= lastC)
			}
			return ci < lastC || (ci == lastC && ri <= lastR)
		}
		add := func(ri, ci int) {
			if !shown(ri, ci) {
				return
			}
			p := &pagePlan{sp: sp, area: a, rows: rows[ri], cols: cols[ci]}
			if rc := ps.RepeatColumns; rc != nil {
				first, last := max(rc.FirstColumn, 0), min(rc.LastColumn, maxColumns-1)
				if p.cols.first > first && p.cols.first <= last {
					p.cols.first = last + 1
				}
				if p.cols.first > last && first <= last {
					p.repCols = &band{first, last}
				}
			}
			if rr := ps.RepeatRows; rr != nil {
				first, last := max(rr.FirstRow, 0), min(rr.LastRow, maxRows-1)
				if p.rows.first > first && p.rows.first <= last {
					p.rows.first = last + 1
				}
				if p.rows.first > last && first <= last {
					p.repRows = &band{first, last}
				}
			}
			if p.cols.first > p.cols.last || p.rows.first > p.rows.last {
				return
			}
			sp.pages = append(sp.pages, p)
		}
		if ps.Order == OverThenDown {
			for ri := 0; ri < len(rows) && len(sp.pages) < maxSheetPages; ri++ {
				for ci := 0; ci < len(cols) && len(sp.pages) < maxSheetPages; ci++ {
					add(ri, ci)
				}
			}
		} else {
			for ci := 0; ci < len(cols) && len(sp.pages) < maxSheetPages; ci++ {
				for ri := 0; ri < len(rows) && len(sp.pages) < maxSheetPages; ri++ {
					add(ri, ci)
				}
			}
		}
	}
	return nil
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
