package sheet

import (
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
}

// docArea returns the room for cells on a page at zoom percent, in sheet
// points: the paper less the margins, divided by the zoom, less the
// headings (ScPrintFunc::GetDocPageSize).
func (sp *sheetPlan) docArea(zoom int) (float64, float64) {
	z := float64(zoom) / 100
	w := (sp.width - sp.margin[1] - sp.margin[3]) / z
	// A header's and a footer's room is unscaled: only the page margins
	// beyond them are divided by the zoom.
	top, bottom := sp.margin[0]/z, sp.margin[2]/z
	ps := &sp.g.sh.Page
	hf := &sp.g.sh.HeaderFooter
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
// hold content.
func (sp *sheetPlan) layoutPages(a Range, zoom int) ([]band, []band, [][]bool) {
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
	// Columns.
	starts := map[int]bool{}
	var repC *band
	if rc := ps.RepeatColumns; rc != nil {
		repC = &band{first: min(max(rc.FirstColumn, 0), maxColumns-1), last: min(max(rc.LastColumn, 0), maxColumns-1)}
	}
	size := 0.0
	colFound := false
	for c := a.FirstColumn; c <= a.LastColumn; c++ {
		start := false
		w := g.colWidth(c)
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
				pageW -= g.colWidth(i)
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
	// Rows.
	var repR *band
	if rr := ps.RepeatRows; rr != nil {
		repR = &band{first: min(max(rr.FirstRow, 0), maxRows-1), last: min(max(rr.LastRow, 0), maxRows-1)}
	}
	rstarts := map[int]bool{}
	size = 0
	rowFound := false
	for r := a.FirstRow; r <= a.LastRow; r++ {
		start := false
		h := g.rowHeight(r)
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
				pageH -= g.rowHeight(i)
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
		if !sp.g.empty(Range{FirstRow: b.first, LastRow: b.last, FirstColumn: a.FirstColumn, LastColumn: a.LastColumn}) {
			h := make([]bool, len(cols))
			for i, cb := range cols {
				h[i] = sp.g.empty(Range{FirstRow: b.first, LastRow: b.last, FirstColumn: cb.first, LastColumn: cb.last})
			}
			rows = append(rows, b)
			hidden = append(hidden, h)
		}
	}
	for r := a.FirstRow; r <= a.LastRow; r++ {
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
	return cols, rows, hidden
}

// epsilon absorbs the rounding of sums of lengths in points.
const epsilon = 1e-6

// empty reports a range Calc does not print a page for: no cell in it
// shows a value, hidden rows and columns aside, and no drawing overlaps it
// (ScDocument::IsPrintEmpty).
//
// # Corpus evidence
//
//   - ods: sc/tdf104502_hiddenColsCountedInPageCount.ods, values below
//     the first page in a hidden column alone, prints one page.
func (g *grid) empty(a Range) bool {
	i := sort.SearchInts(g.rowOrder, a.FirstRow)
	for ; i < len(g.rowOrder) && g.rowOrder[i] <= a.LastRow; i++ {
		row := g.rows[g.rowOrder[i]]
		cells := row.Cells
		j := sort.Search(len(cells), func(j int) bool { return cells[j].Column >= a.FirstColumn })
		for ; j < len(cells) && cells[j].Column <= a.LastColumn; j++ {
			if hasContent(&cells[j]) && !g.colHidden(cells[j].Column) && !row.Hidden {
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

// zoomFor returns the print zoom of the range a in percent: the sheet's
// scale, or the largest zoom up to 100 at which the range fits the pages
// across and down it asks for, found as Calc finds it
// (ScPrintFunc::CalcZoom): a bisection over whole percents, then for a
// sheet fitted to a width alone 2% less when that saves a page down.
func (sp *sheetPlan) zoomFor(a Range) int {
	ps := &sp.g.sh.Page
	if sp.single {
		return 100
	}
	if ps.Scale > 0 {
		return min(max(ps.Scale, minZoom), maxZoom)
	}
	nW, nH := max(ps.FitWidth, 0), max(ps.FitHeight, 0)
	if nW == 0 && nH == 0 {
		return 100
	}
	nW = max(nW, len(ps.ColumnBreaks)+1)
	nH = max(nH, len(ps.RowBreaks)+1)
	if ps.FitWidth == 0 {
		nW = 0
	}
	if ps.FitHeight == 0 {
		nH = 0
	}
	fits := func(z int) (bool, int) {
		cols, rows, _ := sp.layoutPages(a, z)
		return (nW == 0 || len(cols) <= nW) && (nH == 0 || len(rows) <= nH), len(rows)
	}
	zoom := 100
	lastFit, lastNonFit := 0, 0
	for zoom > minZoom {
		ok, _ := fits(zoom)
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
	return zoom
}

// plan returns the pages of the sheet: per print range, its zoom, then
// its pages in the sheet's page order.
func (sp *sheetPlan) plan() {
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
	} else if u, ok := g.usedRange(); ok {
		areas = []Range{u}
	}
	for _, a := range areas {
		zoom := sp.zoomFor(a)
		sp.zoom = float64(zoom) / 100
		cols, rows, hidden := sp.layoutPages(a, zoom)
		add := func(ri, ci int) {
			if hidden[ri][ci] || len(sp.pages) >= maxSheetPages {
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
			for ri := range rows {
				for ci := range cols {
					add(ri, ci)
				}
			}
		} else {
			for ci := range cols {
				for ri := range rows {
					add(ri, ci)
				}
			}
		}
	}
}

// maxSheetPages caps the pages of one sheet. Anti-DoS guard, not
// spec-mandated: a sheet of a million rows of one point prints under
// 20,000 pages of A4, and the document's page cap applies too.
const maxSheetPages = 1 << 15

// singleSize returns the content size of the sheet's first print range at
// 100%, for a page the size of its content.
//
// # Corpus evidence
//
//   - xlsx: budget-single, budget.xlsx with singlePage, prints pages of
//     192.64 by 109.73, 674.14 by 915.02 and 48.19 by 45.04 points, the
//     sizes of the sheets' cells from A1, the first cell's text a point
//     from the page's left edge.
func (sp *sheetPlan) singleSize() (float64, float64) {
	if len(sp.pages) == 0 {
		return 0, 0
	}
	p := sp.pages[0]
	w, h := 0.0, 0.0
	for c := p.cols.first; c <= p.cols.last; c++ {
		w += sp.g.colWidth(c)
	}
	for r := p.rows.first; r <= p.rows.last; r++ {
		h += sp.g.rowHeight(r)
	}
	if sp.g.sh.Page.Headings {
		w += headingWidth
		h += headingHeight
	}
	return math.Max(w, 1), math.Max(h, 1)
}
