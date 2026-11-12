package xlsx

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// The base column width and the row height of a sheet whose format
// properties say nothing (ECMA-376 Part 1, 18.3.1.81 sheetFormatPr, whose
// baseColWidth defaults to 8 characters; Excel's standard height of a
// Calibri 11 row, 20 screen pixels).
const (
	defaultBaseColumn = 8
	standardRowHeight = 15.0
)

// The screen padding Excel adds to a base column width (ECMA-376 Part 1,
// 18.3.1.81 baseColWidth: two pixels a side and a pixel of grid line) and
// the pixel step it rounds the sum up to, at 96 to the inch.
//
// # Corpus evidence
//
//   - litho/xlsx: minimal, without a width, prints Calibri 11 columns 51.6
//     points wide: 64 screen pixels of 8 digits of 7 and the padding,
//     9.14 characters of 47 printer pixels.
const (
	baseColumnPadding = 5
	baseColumnStep    = 8
)

// rowHeightScale is how much shorter Excel prints a row than its height in
// points: a row of h points prints h times 0.968, in whole printer pixels.
// The specification gives a row's height in points (ECMA-376 Part 1,
// 18.3.1.73 ht); the scale is Excel's, measured.
//
// # Corpus evidence
//
//   - litho/xlsx: row_heights prints rows of 6, 18, 30, 60 and 409.5 points
//     5.76, 17.4, 29.04, 58.08 and 396.36 points tall; multipage_grid
//     prints rows of 15 points 14.52 apart.
//   - libreoffice/xlsx: sc/tdf139934.xlsx and sc/database.xlsx print Arial
//     10 rows of 12.75 and 12.8 points 12.36 apart.
//   - libreoffice/xlsx: sc/tdf155046.xlsx prints Calibri 12 rows of 16
//     points 15.6 apart, a scale of 0.975, and poi/xlsx
//     GeneralFormatTests.xlsx Verdana 10 rows of 13 points 12.96 apart:
//     the scale depends on the font, by a rule not measured.
const rowHeightScale = 0.968

// worksheet reads the worksheet part name into sh, the index-th sheet of
// the book (ECMA-376 Part 1, 18.3.1.99 worksheet).
func (r *reader) worksheet(name string, sh *sheet.Sheet, index int, printed bool) error {
	root, err := r.pkg.ParseXML(r.ctx, name, understood)
	if err != nil {
		if isMalformed(err) {
			r.warn("malformed-part", fmt.Sprintf("sheet %q does not parse: it prints empty: %v", sh.Name, err))
			r.defaultSetup(sh)
			return nil
		}
		return err
	}
	ws := &sheetReader{r: r, part: name, sh: sh, index: index, printed: printed}
	return ws.read(root)
}

// sheetReader is the state of one worksheet's reading.
type sheetReader struct {
	r    *reader
	part string
	sh   *sheet.Sheet
	// fitToPage is the sheet's fit-to-page flag (18.3.1.65), which the
	// page setup's fitToWidth and fitToHeight wait for.
	fitToPage bool
	// rowAt indexes sh.Rows by row index.
	rowAt map[int]int
	// index is the sheet's index in the book.
	index int
	// printed marks a sheet that prints; one that does not is read
	// without its drawings and conditional formats.
	printed bool
}

// maxCachedFormulas caps the formulas with a cached result a workbook
// recalculates. Anti-DoS guard, not spec-mandated: a million formulas
// cover the largest workbooks LibreOffice opens in reasonable time; the
// cells past it show their cached results.
const maxCachedFormulas = 1_000_000

// read reads the worksheet root.
func (w *sheetReader) read(root *ooxml.Element) error {
	r, sh := w.r, w.sh
	r.defaultSetup(sh)
	w.rowAt = map[int]int{}
	if pr := root.Child(nsMain, "sheetPr"); pr != nil {
		w.fitToPage = boolAttr(pr.Child(nsMain, "pageSetUpPr"), "fitToPage")
	}
	if v := root.Child(nsMain, "sheetViews").Child(nsMain, "sheetView"); v != nil {
		sh.RightToLeft = boolAttr(v, "rightToLeft")
	}
	if f := root.Child(nsMain, "sheetFormatPr"); f != nil {
		w.formatPr(f)
	}
	w.columns(root.Child(nsMain, "cols"))
	if err := w.sheetData(root.Child(nsMain, "sheetData")); err != nil {
		return err
	}
	for _, m := range root.Child(nsMain, "mergeCells").Kids() {
		if m.Is(nsMain, "mergeCell") {
			if rg, ok := rangeRef(attr(m, "ref")); ok && (rg.FirstRow != rg.LastRow || rg.FirstColumn != rg.LastColumn) {
				sh.Merges = append(sh.Merges, rg)
			}
		}
	}
	if err := w.tables(root.Child(nsMain, "tableParts")); err != nil {
		return err
	}
	w.hyperlinks(root.Child(nsMain, "hyperlinks"))
	w.pageSetup(root)
	if hf := root.Child(nsMain, "headerFooter"); hf != nil {
		sh.HeaderFooter = r.headerFooter(hf)
		dynamicHeights(sh, r.normal().Font.Size)
	}
	sh.Page.RowBreaks = breaks(root.Child(nsMain, "rowBreaks"), maxRows)
	sh.Page.ColumnBreaks = breaks(root.Child(nsMain, "colBreaks"), maxColumns)
	if d := root.Child(nsMain, "drawing"); d != nil && w.printed {
		id, _ := d.AttrNS(ooxml.NSR, "id")
		if err := w.drawing(id); err != nil {
			return err
		}
	}
	if w.printed {
		w.conditionalFormats(root)
	}
	if err := w.pivotTables(); err != nil {
		return err
	}
	if root.Child(nsMain, "picture") != nil && w.printed {
		r.warn("unsupported-content", "a sheet background picture is left out")
	}
	return nil
}

// defaultSetup gives sh what Excel gives a sheet that says nothing: the
// default column width and row height, the page setup of an empty
// pageSetup and pageMargins.
func (r *reader) defaultSetup(sh *sheet.Sheet) {
	sh.DefaultColumnWidth = r.baseColumnWidth(defaultBaseColumn)
	sh.DefaultRowHeight = r.rowHeight(standardRowHeight)
	sh.Page = defaultPage(r.paperW, r.paperH)
}

// columnWidth converts a width in characters of the Normal font's widest
// digit (ECMA-376 Part 1, 18.3.1.13 width, which counts the cell's
// padding in) to points as Excel prints it: the characters times the
// digit's printer pixels, rounded to a pixel.
func (r *reader) columnWidth(chars float64) float64 {
	if math.IsNaN(chars) || chars <= 0 {
		return 0
	}
	return printerPixels(min(chars, maxColumnChars) * r.digitWidth)
}

// baseColumnWidth returns the width in points of a column of a sheet whose
// base column width is base characters (ECMA-376 Part 1, 18.3.1.81
// baseColWidth): the digits and Excel's padding in screen pixels, rounded
// up to its step, read back as characters truncated to 1/256 as Excel
// stores a width, then printed as any width.
func (r *reader) baseColumnWidth(base int) float64 {
	d := max(r.screenDigit, 1)
	px := math.Ceil((float64(base)*d+baseColumnPadding)/baseColumnStep) * baseColumnStep
	return r.columnWidth(math.Floor(px/d*256) / 256)
}

// maxColumnChars is the widest column in characters (ECMA-376 Part 1,
// 18.3.1.13: Excel caps a width at 255 characters).
const maxColumnChars = 255

// formatPr reads the sheet's format properties (ECMA-376 Part 1,
// 18.3.1.81): defaultColWidth, else baseColWidth with Excel's padding;
// defaultRowHeight.
func (w *sheetReader) formatPr(f *ooxml.Element) {
	r, sh := w.r, w.sh
	if v, err := strconv.ParseFloat(attr(f, "defaultColWidth"), 64); err == nil && v > 0 {
		sh.DefaultColumnWidth = r.columnWidth(v)
	} else if base := intAttr(f, "baseColWidth", defaultBaseColumn); base > 0 {
		sh.DefaultColumnWidth = r.baseColumnWidth(min(base, maxColumnChars))
	}
	if v, err := strconv.ParseFloat(attr(f, "defaultRowHeight"), 64); err == nil && v > 0 && !math.IsInf(v, 0) {
		if h := r.rowHeight(v); h > 0 {
			sh.DefaultRowHeight = h
		}
	}
}

// maxRowHeight is the tallest row in points (ECMA-376 Part 1, 18.3.1.73
// ht: Excel caps a row at 409 points).
const maxRowHeight = 409.5

// rowHeight returns a row height in points as Excel prints a row of ht
// points ([rowHeightScale]).
func (r *reader) rowHeight(ht float64) float64 {
	return printerPixels(min(ht, maxRowHeight) * rowHeightScale)
}

// columns reads the column ranges (ECMA-376 Part 1, 18.3.1.13 col),
// sorted and without overlap, the later of two overlapping ranges cut.
func (w *sheetReader) columns(cols *ooxml.Element) {
	r := w.r
	var out []sheet.Column
	for _, c := range cols.Kids() {
		if !c.Is(nsMain, "col") {
			continue
		}
		lo, hi := intAttr(c, "min", 0), intAttr(c, "max", 0)
		if lo < 1 || hi < lo || lo > maxColumns {
			continue
		}
		col := sheet.Column{First: lo - 1, Last: min(hi, maxColumns) - 1, Hidden: boolAttr(c, "hidden")}
		if v, err := strconv.ParseFloat(attr(c, "width"), 64); err == nil {
			col.Width = r.columnWidth(v)
		}
		if s, ok := c.Attr("style"); ok {
			if i, ok := ooxml.Int(s); ok {
				col.Style = r.style(i)
			}
		}
		if col.Width == 0 && !col.Hidden && col.Style == nil {
			continue
		}
		out = append(out, col)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].First < out[j].First })
	kept := out[:0]
	next := 0
	for _, c := range out {
		if c.Last < next {
			continue
		}
		c.First = max(c.First, next)
		kept = append(kept, c)
		next = c.Last + 1
	}
	w.sh.Columns = kept
}

// sheetData reads the rows and cells (ECMA-376 Part 1, 18.3.1.80): a row
// or cell without its index, or with one that does not parse, follows the
// previous one, as LibreOffice reads it. A row prints at the height it
// stores, custom or not, and a row without one at the sheet's default:
// Excel does not fit a row to its text as it opens a workbook.
//
// # Corpus evidence
//
//   - litho/xlsx: fonts prints its row of 14 point text without a height at
//     the default 14.52 points, and row_heights its row of 34.5 points
//     without customHeight 33.4 points tall.
func (w *sheetReader) sheetData(data *ooxml.Element) error {
	r, sh := w.r, w.sh
	next := 0
	for _, re := range data.Kids() {
		if !re.Is(nsMain, "row") {
			continue
		}
		idx := next
		if n, ok := ooxml.Int(attr(re, "r")); ok && n >= 1 {
			idx = n - 1
		}
		if idx >= maxRows {
			continue
		}
		next = idx + 1
		row := sheet.Row{Index: idx, CustomHeight: boolAttr(re, "customHeight"), Hidden: boolAttr(re, "hidden")}
		if v, err := strconv.ParseFloat(attr(re, "ht"), 64); err == nil && v > 0 && !math.IsInf(v, 0) {
			row.Height = r.rowHeight(v)
		}
		if boolAttr(re, "customFormat") {
			row.Style = r.style(intAttr(re, "s", 0))
		}
		col := 0
		for _, c := range re.Kids() {
			if !c.Is(nsMain, "c") {
				continue
			}
			ci := col
			if _, cc, ok := cellRef(attr(c, "r")); ok {
				ci = cc
			}
			if ci >= maxColumns {
				continue
			}
			col = ci + 1
			cell, ok := w.cell(c, ci, &row)
			if !ok {
				continue
			}
			r.cells++
			if r.cells > sheet.MaxCells {
				return fmt.Errorf("%w: more than %d cells", office.ErrInputTooLarge, sheet.MaxCells)
			}
			if err := limits.BudgetFrom(r.ctx).ChargeUnits(1, sheet.CellCost, "cells"); err != nil {
				return err
			}
			row.Cells = append(row.Cells, cell)
		}
		if len(row.Cells) == 0 && row.Height == 0 && !row.Hidden && row.Style == nil {
			continue
		}
		sort.SliceStable(row.Cells, func(i, j int) bool { return row.Cells[i].Column < row.Cells[j].Column })
		row.Cells = dedupCells(row.Cells)
		if at, dup := w.rowAt[idx]; dup {
			// A row listed twice keeps its first record, its cells merged.
			sh.Rows[at].Cells = dedupCells(append(sh.Rows[at].Cells, row.Cells...))
			continue
		}
		w.rowAt[idx] = len(sh.Rows)
		sh.Rows = append(sh.Rows, row)
		if err := r.ctx.Err(); err != nil {
			return err
		}
	}
	sort.SliceStable(sh.Rows, func(i, j int) bool { return sh.Rows[i].Index < sh.Rows[j].Index })
	for i, row := range sh.Rows {
		w.rowAt[row.Index] = i
	}
	return nil
}

// dedupCells keeps the first of the cells of one column, the cells sorted.
func dedupCells(cells []sheet.Cell) []sheet.Cell {
	sort.SliceStable(cells, func(i, j int) bool { return cells[i].Column < cells[j].Column })
	out := cells[:0]
	for i, c := range cells {
		if i > 0 && c.Column == out[len(out)-1].Column {
			continue
		}
		out = append(out, c)
	}
	return out
}

// cell reads a c element at column col of row (ECMA-376 Part 1,
// 18.3.1.4), its value of type t (18.18.11 ST_CellType) the one the file
// stores: a formula with a cached value shows it, as LibreOffice does not
// recalculate a workbook it loads unless [reader.recalculates]; one
// without (18.3.1.40 f, no v) keeps its text for the builder to compute
// (an empty v is none, as budget.xlsx saves its totals), a shared
// formula's other cells excepted. That is office.RecalculateAsNeeded;
// [reader.recalculation] may keep every formula or none. It reports false
// for a cell without a value, a formula or a style.
func (w *sheetReader) cell(c *ooxml.Element, col int, row *sheet.Row) (sheet.Cell, bool) {
	r := w.r
	cell := sheet.Cell{Column: col}
	if s, ok := c.Attr("s"); ok {
		if i, ok := ooxml.Int(s); ok {
			cell.Style = r.style(i)
		}
	}
	v := c.Child(nsMain, "v")
	switch t := attr(c, "t"); t {
	case "s":
		if v != nil {
			if rt, ok := r.sharedText(v.Text); ok {
				cell.Value = sheet.Value{Kind: sheet.Text, Text: rt.text}
				cell.Runs = rt.runs
			}
		}
	case "inlineStr":
		rt := r.inlineText(c)
		cell.Value = sheet.Value{Kind: sheet.Text, Text: rt.text}
		cell.Runs = rt.runs
	case "str":
		if v != nil {
			rt := r.richTextOf(v.Text)
			cell.Value = sheet.Value{Kind: sheet.Text, Text: rt}
		}
	case "b":
		if v != nil {
			b := 0.0
			if strings.TrimSpace(v.Text) == "1" || strings.EqualFold(strings.TrimSpace(v.Text), "true") {
				b = 1
			}
			cell.Value = sheet.Value{Kind: sheet.Bool, Number: b}
		}
	case "e":
		if v != nil {
			cell.Value = sheet.Value{Kind: sheet.Error, Text: strings.TrimSpace(v.Text)}
		}
	case "d":
		if v != nil {
			if n, ok := isoSerial(v.Text); ok {
				cell.Value = sheet.Value{Kind: sheet.Number, Number: n}
			}
		}
	default:
		if v != nil {
			if n, err := strconv.ParseFloat(strings.TrimSpace(v.Text), 64); err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) {
				cell.Value = sheet.Value{Kind: sheet.Number, Number: n}
				if r.date1904 {
					cell.Value.Number += w.dateShift(cell.Style, row, col)
				}
			}
		}
	}
	if cell.Value.Kind == sheet.Text && (cell.Style == nil || !cell.Style.Align.Wrap) {
		joinLines(&cell)
	}
	if cell.Value.Kind == sheet.Text && cell.Value.Text == "" && len(cell.Runs) == 0 {
		cell.Value = sheet.Value{}
	}
	if f := c.Child(nsMain, "f"); f != nil && len(f.Text) <= maxFormulaRunes && r.recalculation != office.RecalculateNever {
		// Under office.RecalculateNever no formula is kept: the cell shows
		// the result the file caches, nothing without one. Under
		// office.RecalculateAlways every formula is kept for the builder
		// to compute, a cached result or not.
		cell.Formula = strings.TrimSpace(f.Text)
		if v != nil && strings.TrimSpace(v.Text) != "" && r.recalculation == office.RecalculateAsNeeded {
			// A cached result shows unless the workbook is recalculated
			// ([reader.keepCachedResults]).
			if t := attr(c, "t"); (t == "" || t == "n") && cell.Value.Kind == sheet.Number && cell.Value.Number != 0 {
				r.calculated = true
			}
			if cell.Formula != "" && len(r.cachedFormulas) < maxCachedFormulas {
				if r.cachedFormulas == nil {
					r.cachedFormulas = map[[3]int]bool{}
				}
				r.cachedFormulas[[3]int{w.index, row.Index, col}] = true
			} else {
				cell.Formula = ""
			}
		}
	}
	if cell.Value.Kind == sheet.Empty && cell.Style == nil && cell.Formula == "" {
		return cell, false
	}
	return cell, true
}

// richTextOf decodes the text of a formula's cached string value.
func (r *reader) richTextOf(s string) string {
	s = decodeXString(s)
	if n := []rune(s); len(n) > maxCellRunes {
		s = string(n[:maxCellRunes])
	}
	return s
}

// maxFormulaRunes caps the formula text kept for a cell. Anti-DoS guard,
// not spec-mandated: Excel stops at 8,192 characters.
const maxFormulaRunes = 8192

// date1904Shift is the days from Calc's null date, 1899-12-30, to the
// 1904 date system's, 1904-01-01 (ECMA-376 Part 1, 18.17.4.1).
const date1904Shift = 1462

// dateShift returns the days to add to a number of a 1904-based workbook
// so its serial counts from Calc's null date: date1904Shift when the
// cell's format shows a date, 0 for any other number, whose value does
// not depend on the date system.
func (w *sheetReader) dateShift(st *sheet.Style, row *sheet.Row, col int) float64 {
	if st == nil {
		st = row.Style
	}
	if st == nil {
		for _, c := range w.sh.Columns {
			if col >= c.First && col <= c.Last {
				st = c.Style
				break
			}
		}
	}
	if st != nil && isDateFormat(st.Format) {
		return date1904Shift
	}
	return 0
}

// nullDate is Calc's day 0, the origin of the serials of the model.
var nullDate = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

// isoSerial parses an ST_Xstring ISO 8601 date and time value of a d cell
// (ECMA-376 Part 1, 18.17.4.1) into a serial from Calc's null date.
func isoSerial(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02T15:04", "2006-01-02", "15:04:05.999999999", "15:04:05", "15:04"} {
		t, err := time.Parse(layout, s)
		if err != nil {
			continue
		}
		if t.Year() == 0 {
			// A time without a date is a fraction of a day.
			return float64(t.Hour())/24 + float64(t.Minute())/1440 + (float64(t.Second())+float64(t.Nanosecond())/1e9)/86400, true
		}
		_, off := t.Zone()
		days := t.Add(time.Duration(off)*time.Second).UTC().Sub(nullDate).Hours() / 24
		return days, true
	}
	return 0, false
}

// maxHyperlinkCells caps the cells one hyperlink element marks. Anti-DoS
// guard, not spec-mandated: a link covers a cell or a merged range.
const maxHyperlinkCells = 4096

// hyperlinks reads the sheet's hyperlinks (ECMA-376 Part 1, 18.3.1.47)
// onto the cells of their range that hold a value: an external target
// through its relationship, its location appended after a #; a location
// alone, a place in the workbook ("Sheet2!A1"), as # and the location,
// which the builder resolves to the page that cell prints on, as
// LibreOffice links it.
func (w *sheetReader) hyperlinks(e *ooxml.Element) {
	r, sh := w.r, w.sh
	visits := 0
	for _, h := range e.Kids() {
		if !h.Is(nsMain, "hyperlink") {
			continue
		}
		rg, ok := rangeRef(attr(h, "ref"))
		if !ok {
			continue
		}
		target := ""
		if id, ok := h.AttrNS(ooxml.NSR, "id"); ok {
			if rel, ok := r.pkg.RelationshipByID(w.part, id); ok && rel.External {
				target = uncURL(rel.Target)
			}
		}
		if loc := decodeXString(attr(h, "location")); loc != "" {
			target += "#" + loc
		}
		if target == "" {
			continue
		}
		n := 0
		rows := sh.Rows
		for at := sort.Search(len(rows), func(i int) bool { return rows[i].Index >= rg.FirstRow }); at < len(rows) && rows[at].Index <= rg.LastRow && n < maxHyperlinkCells; at++ {
			if visits++; visits > maxHyperlinkRows {
				return
			}
			cells := rows[at].Cells
			for i := columnIndex(cells, rg.FirstColumn); i < len(cells) && cells[i].Column <= rg.LastColumn; i++ {
				cells[i].Href = target
				n++
			}
		}
	}
}

// maxHyperlinkRows caps the rows a sheet's hyperlinks visit in all.
// Anti-DoS guard, not spec-mandated: a link covers a cell or a merged
// range, and four million rows cover Excel's 65,530 links a sheet of 64
// rows each.
const maxHyperlinkRows = 1 << 22

// maxBreaks caps the manual page breaks of one axis. Anti-DoS guard, not
// spec-mandated: Excel holds at most 1,026 breaks a sheet.
const maxBreaks = 1 << 12

// breaks reads a rowBreaks or colBreaks element (ECMA-376 Part 1,
// 18.3.1.74, 18.3.1.14): the manual breaks, each the index of the row or
// column that starts a page, rising, as LibreOffice reads them (a break
// at index 0 or a break the application inserted is ignored).
func breaks(e *ooxml.Element, limit int) []int {
	var out []int
	seen := map[int]bool{}
	for _, b := range e.Kids() {
		if len(out) >= maxBreaks {
			break
		}
		if !b.Is(nsMain, "brk") {
			continue
		}
		id := intAttr(b, "id", 0)
		if id <= 0 || id >= limit || !boolAttr(b, "man") || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Ints(out)
	return out
}

// chartsheet reads the chartsheet part name into sh (ECMA-376 Part 1,
// 18.3.1.12 chartsheet): a sheet without cells whose drawing holds its
// chart, its page setup, header and footer as a worksheet's, fitted to one
// page as LibreOffice prints the chart object it imports a chartsheet as.
//
// # Corpus evidence
//
//   - probe, xlsx: sc/tdf107586.xlsx's chartsheet, a 731.7 by 477.9 point
//     chart, given a series prints in LibreOffice 26.8.2.2 on one Letter
//     page at 69%, 505.99 points wide from the 0.7-inch margin.
//   - libreoffice/xlsx: sc/tdf107586.xlsx, whose chart holds no series,
//     prints a blank page.
func (r *reader) chartsheet(name string, sh *sheet.Sheet) error {
	root, err := r.pkg.ParseXML(r.ctx, name, understood)
	if err != nil {
		if isMalformed(err) {
			r.warn("malformed-part", fmt.Sprintf("sheet %q does not parse: it prints empty: %v", sh.Name, err))
			r.defaultSetup(sh)
			return nil
		}
		return err
	}
	w := &sheetReader{r: r, part: name, sh: sh, rowAt: map[int]int{}}
	r.defaultSetup(sh)
	w.pageSetup(root)
	sh.Page.Scale, sh.Page.FitWidth, sh.Page.FitHeight = 0, 1, 1
	if hf := root.Child(nsMain, "headerFooter"); hf != nil {
		sh.HeaderFooter = r.headerFooter(hf)
		dynamicHeights(sh, r.normal().Font.Size)
	}
	if d := root.Child(nsMain, "drawing"); d != nil {
		id, _ := d.AttrNS(ooxml.NSR, "id")
		if err := w.drawing(id); err != nil {
			return err
		}
	}
	// A chart without a series prints nothing.
	kept := sh.Drawings[:0]
	for _, d := range sh.Drawings {
		if d.Chart == nil || chartSeries(d.Chart) > 0 {
			kept = append(kept, d)
		}
	}
	sh.Drawings = kept
	return nil
}

// chartSeries returns the series of the chart c.
func chartSeries(c *chart.Chart) int {
	n := 0
	for _, g := range c.Groups {
		if g != nil {
			n += len(g.Series)
		}
	}
	return n
}

// uncURL returns a Windows network path, \\host\share\file or the file URL
// Excel writes for one (file:///\\host\share\file), as the file URL
// LibreOffice exports it as: file://host/share/file, the backslashes
// turned to slashes; any other target unchanged.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/129969-min.xlsx links file:///\\ndhlis-cbut018\CBUT018\...,
//     which LibreOffice exports as file://ndhlis-cbut018/CBUT018/....
func uncURL(target string) string {
	rest, ok := strings.CutPrefix(target, `file:///\\`)
	if !ok {
		rest, ok = strings.CutPrefix(target, `\\`)
	}
	if !ok || rest == "" {
		return target
	}
	return "file://" + strings.ReplaceAll(rest, `\`, "/")
}

// joinLines drops the line feeds of the text of a cell that does not
// wrap, as LibreOffice shows such a cell of a workbook on one line; a
// carriage return and line feed pair, which XML keeps only escaped
// (_x000D_, XML 1.0, 2.11), still breaks the line.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/cell-multi-line.xlsx prints "Line1\nLine2\nLine3" as
//     Line1Line2Line3 in its cell without wrapText, on three lines in the
//     one with it; sc/preserve_space.xlsx prints "123\n" and "456" as
//     123456; sc/escape-unicode.xlsx prints "Line 1_x000D_\nLine 2" on two
//     lines.
func joinLines(cell *sheet.Cell) {
	if !strings.ContainsAny(cell.Value.Text, "\r\n") {
		return
	}
	cell.Value.Text = dropFeeds(cell.Value.Text)
	runs := cell.Runs[:0]
	for _, r := range cell.Runs {
		r.Text = dropFeeds(r.Text)
		if r.Text != "" {
			runs = append(runs, r)
		}
	}
	cell.Runs = runs
}

// dropFeeds returns s without its lone line feeds and carriage returns,
// each carriage return and line feed pair a line feed.
func dropFeeds(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n':
			b.WriteByte('\n')
			i++
		case s[i] == '\r' || s[i] == '\n':
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
