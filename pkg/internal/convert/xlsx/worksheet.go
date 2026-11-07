package xlsx

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// The column width and row height Calc gives a sheet whose format
// properties say nothing (ECMA-376 Part 1, 18.3.1.81 sheetFormatPr):
// LibreOffice's import starts every sheet at 8.5 digits a column, and
// leaves a row at Calc's standard height, 256 twips, which it grows to
// fit the row's text.
//
// # Corpus evidence
//
//   - LibreOffice 26.8.2.2: a sheet without sheetFormatPr prints columns
//     47.20 points apart for Calibri 11 and Arial 10, 42.95 for Calibri
//     10, 51.85 for Calibri 12 and Arial 11, 51.00 for Times New Roman
//     12 (8.5 digits each), and rows 12.79 points apart at 10 points,
//     13.77 at 11 and 15.0 at 12 (the standard height, grown to the text).
const (
	defaultColumnDigits = 8.5
	standardRowHeight   = 256.0 / 20
)

// paddingPixels is the padding LibreOffice adds to a sheet's base column
// width (ECMA-376 Part 1, 18.3.1.81 baseColWidth), five pixels at 96 to
// the inch, in points.
//
// # Corpus evidence
//
//   - LibreOffice 26.8.2.2: baseColWidth 8 prints columns 48.13 points
//     apart for Calibri 11 (8 digits of 5.55 points and 3.75), 44.14 for
//     Calibri 10, 52.53 for Calibri 12, and baseColWidth 10 59.24 for
//     Calibri 11.
const paddingPixels = 5 * 0.75

// worksheet reads the worksheet part name into sh (ECMA-376 Part 1,
// 18.3.1.99 worksheet).
func (r *reader) worksheet(name string, sh *sheet.Sheet) error {
	root, err := r.pkg.ParseXML(r.ctx, name, understood)
	if err != nil {
		if isMalformed(err) {
			r.warn("malformed-part", fmt.Sprintf("sheet %q does not parse: it prints empty: %v", sh.Name, err))
			r.defaultSetup(sh)
			return nil
		}
		return err
	}
	ws := &sheetReader{r: r, part: name, sh: sh}
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
}

// read reads the worksheet root.
func (w *sheetReader) read(root *ooxml.Element) error {
	r, sh := w.r, w.sh
	r.defaultSetup(sh)
	w.rowAt = map[int]int{}
	if pr := root.Child(nsMain, "sheetPr"); pr != nil {
		w.fitToPage = boolAttr(pr.Child(nsMain, "pageSetUpPr"), "fitToPage", false)
	}
	if v := root.Child(nsMain, "sheetViews").Child(nsMain, "sheetView"); v != nil {
		sh.RightToLeft = boolAttr(v, "rightToLeft", false)
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
	w.hyperlinks(root.Child(nsMain, "hyperlinks"))
	w.pageSetup(root)
	if hf := root.Child(nsMain, "headerFooter"); hf != nil {
		sh.HeaderFooter = r.headerFooter(hf)
	}
	sh.Page.RowBreaks = breaks(root.Child(nsMain, "rowBreaks"), maxRows)
	sh.Page.ColumnBreaks = breaks(root.Child(nsMain, "colBreaks"), maxColumns)
	if d := root.Child(nsMain, "drawing"); d != nil {
		id, _ := d.AttrNS(ooxml.NSR, "id")
		if err := w.drawing(id); err != nil {
			return err
		}
	}
	if root.Child(nsMain, "conditionalFormatting") != nil {
		r.warn("unsupported-content", "conditional formatting is not applied")
	}
	if root.Child(nsMain, "picture") != nil {
		r.warn("unsupported-content", "a sheet background picture is left out")
	}
	return nil
}

// defaultSetup gives sh what Calc gives a sheet that says nothing: the
// default column width and row height, the page setup of an empty
// pageSetup and pageMargins.
func (r *reader) defaultSetup(sh *sheet.Sheet) {
	sh.DefaultColumnWidth = r.columnWidth(defaultColumnDigits)
	sh.DefaultRowHeight = standardRowHeight
	sh.Page = defaultPage()
}

// columnWidth converts a width in characters of the Normal font's widest
// digit to points as LibreOffice imports it (ECMA-376 Part 1, 18.3.1.13
// width): the digits times the digit's width, rounded to a twip.
func (r *reader) columnWidth(chars float64) float64 {
	if math.IsNaN(chars) || chars <= 0 {
		return 0
	}
	return math.Round(min(chars, maxColumnChars)*r.digitWidth*20) / 20
}

// maxColumnChars is the widest column in characters (ECMA-376 Part 1,
// 18.3.1.13: Excel caps a width at 255 characters).
const maxColumnChars = 255

// formatPr reads the sheet's format properties (ECMA-376 Part 1,
// 18.3.1.81): defaultColWidth, else baseColWidth with LibreOffice's five
// pixels of padding; defaultRowHeight, rounded down to three quarters of
// a point for a workbook Office wrote, as LibreOffice does.
func (w *sheetReader) formatPr(f *ooxml.Element) {
	r, sh := w.r, w.sh
	if v, err := strconv.ParseFloat(attr(f, "defaultColWidth"), 64); err == nil && v > 0 {
		sh.DefaultColumnWidth = r.columnWidth(v)
	} else if base := intAttr(f, "baseColWidth", 8); base > 0 {
		sh.DefaultColumnWidth = r.columnWidth(float64(base) + paddingPixels/r.digitWidth)
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

// rowHeight returns a row height in points as LibreOffice imports it,
// rounded down to three quarters of a point for a workbook Office wrote,
// then to a twip.
func (r *reader) rowHeight(ht float64) float64 {
	ht = min(ht, maxRowHeight)
	if r.mso {
		ht -= math.Mod(ht, 0.75)
	}
	return math.Round(ht*20) / 20
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
		col := sheet.Column{First: lo - 1, Last: min(hi, maxColumns) - 1, Hidden: boolAttr(c, "hidden", false)}
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
// or cell without its index follows the previous one. A row's height is
// read when it is custom alone: LibreOffice gives every other row its
// optimal height, the sheet's default height grown to the row's text,
// whatever height the file stores.
//
// # Corpus evidence
//
//   - LibreOffice 26.8.2.2: rows of 30 and 8 points without customHeight
//     print 15 points high on a sheet of default height 15, and 13.77 (the
//     height of Calibri 11 text) on one of default height 10; a custom row
//     of 30 points prints 30. singlepagesheets-scrolled.xlsx prints its
//     rows of 16.2 points 15.65 apart.
func (w *sheetReader) sheetData(data *ooxml.Element) error {
	r, sh := w.r, w.sh
	next := 0
	for _, re := range data.Kids() {
		if !re.Is(nsMain, "row") {
			continue
		}
		idx := next
		if v, ok := re.Attr("r"); ok {
			n, ok := ooxml.Int(v)
			if !ok || n < 1 {
				continue
			}
			idx = n - 1
		}
		if idx >= maxRows {
			continue
		}
		next = idx + 1
		row := sheet.Row{Index: idx, CustomHeight: boolAttr(re, "customHeight", false), Hidden: boolAttr(re, "hidden", false)}
		if v, err := strconv.ParseFloat(attr(re, "ht"), 64); err == nil && v > 0 && !math.IsInf(v, 0) && row.CustomHeight {
			row.Height = r.rowHeight(v)
		}
		if boolAttr(re, "customFormat", false) {
			row.Style = r.style(intAttr(re, "s", 0))
		}
		col := 0
		for _, c := range re.Kids() {
			if !c.Is(nsMain, "c") {
				continue
			}
			ci := col
			if ref, ok := c.Attr("r"); ok {
				_, cc, ok := cellRef(ref)
				if !ok {
					continue
				}
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
			if r.cells > maxCells {
				return fmt.Errorf("%w: more than %d cells", office.ErrInputTooLarge, maxCells)
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
// stores: a formula is not computed, its cached value shown. It reports
// false for a cell without a value or a style.
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
	if cell.Value.Kind == sheet.Text && cell.Value.Text == "" && len(cell.Runs) == 0 {
		cell.Value = sheet.Value{}
	}
	if cell.Value.Kind == sheet.Empty && cell.Style == nil {
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

// hyperlinks reads the sheet's hyperlinks (ECMA-376 Part 1, 18.3.1.47):
// an external target through its relationship becomes the link of the
// cells of its range that hold a value; a location inside the workbook
// is not followed.
func (w *sheetReader) hyperlinks(e *ooxml.Element) {
	r, sh := w.r, w.sh
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
				target = rel.Target
				if loc := attr(h, "location"); loc != "" {
					target += "#" + loc
				}
			}
		}
		if target == "" {
			if attr(h, "location") != "" {
				r.warn("unsupported-content", "a link to a place in the workbook is left out")
			}
			continue
		}
		n := 0
		for ri := rg.FirstRow; ri <= rg.LastRow && n < maxHyperlinkCells; ri++ {
			at, ok := w.rowAt[ri]
			if !ok {
				continue
			}
			cells := sh.Rows[at].Cells
			for i := range cells {
				if cells[i].Column >= rg.FirstColumn && cells[i].Column <= rg.LastColumn {
					cells[i].Href = target
					n++
				}
			}
		}
	}
}

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
		if !b.Is(nsMain, "brk") || len(out) >= maxBreaks {
			continue
		}
		id := intAttr(b, "id", 0)
		if id <= 0 || id >= limit || !boolAttr(b, "man", false) || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Ints(out)
	return out
}
