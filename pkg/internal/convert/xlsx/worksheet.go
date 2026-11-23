package xlsx

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/warn"
	"github.com/gotenberg/litho/pkg/models"
)

// defaultBaseColumn is the base column width of a sheet whose format
// properties say nothing (ECMA-376 Part 1, 18.3.1.81 sheetFormatPr, whose
// baseColWidth defaults to 8 characters).
const defaultBaseColumn = 8

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

// worksheet reads the worksheet part name into sh, the index-th sheet of
// the book (ECMA-376 Part 1, 18.3.1.99 worksheet).
func (r *reader) worksheet(name string, sh *sheet.Sheet, index int, printed bool) error {
	root, err := r.pkg.ParseXML(r.ctx, name, understood)
	if err != nil {
		if isMalformed(err) {
			r.warn(warn.PartMalformed, warn.Omitted, fmt.Sprintf("sheet %q does not parse: it prints empty: %v", sh.Name, err))
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
	// noteRuns holds the rich text of each note by cell, read for a sheet
	// that prints its notes as displayed.
	noteRuns map[[2]int][]text.Run
	// customDefault reports a default row height marked custom (18.3.1.81
	// sheetFormatPr customHeight): a row without a height of its own keeps
	// it, unmeasured (sheet.Document.FitRows).
	customDefault bool
	// arrays are the ranges of the array formulas read so far that reach
	// the row being read (18.3.1.40 f, t array, ref).
	arrays []sheet.Range
}

// maxArrayFormulas caps the array formula ranges a sheet keeps while its
// rows are read. Anti-DoS guard, not spec-mandated: a range is dropped
// once the rows pass it, and a sheet holds a few at a time.
const maxArrayFormulas = 1 << 12

// arrayMember reports whether the cell at row and col lies in the range
// of an array formula read before it, outside its anchor cell.
//
// # Corpus evidence
//
//   - poi/xlsx spreadsheet/66365.xlsx: B2, in the range B1:B2 of the
//     array formula {=A1:A2} and stored as the shared string "Bob",
//     prints as #VALUE!; so did litho/xlsx dynamic_arrays's spilled text
//     cells while the fixture stored them as shared strings, where Excel's
//     own files store them as formula strings (t="str"), which print.
func (w *sheetReader) arrayMember(row, col int) bool {
	keep := w.arrays[:0]
	in := false
	for _, a := range w.arrays {
		if a.LastRow < row {
			continue
		}
		keep = append(keep, a)
		if row >= a.FirstRow && col >= a.FirstColumn && col <= a.LastColumn && (row != a.FirstRow || col != a.FirstColumn) {
			in = true
		}
	}
	w.arrays = keep
	return in
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
		sh.HeaderFooter = r.headerFooter(hf, w.printed, w.headerPictures(root))
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
		w.sparklines(root)
	}
	if d := root.Child(nsMain, "legacyDrawing"); d != nil && w.printed {
		id, _ := d.AttrNS(ooxml.NSR, "id")
		if err := w.controls(id); err != nil {
			return err
		}
	}
	w.notes(root)
	if w.printed {
		w.conditionalFormats(root)
	}
	if err := w.pivotTables(); err != nil {
		return err
	}
	if root.Child(nsMain, "picture") != nil && w.printed {
		r.warn(warn.ContentUnsupported, warn.Omitted, "a sheet background picture is left out")
	}
	return nil
}

// defaultSetup gives sh what Excel gives a sheet that says nothing: the
// default column width and row height, the page setup of an empty
// pageSetup and pageMargins.
func (r *reader) defaultSetup(sh *sheet.Sheet) {
	sh.DefaultColumnWidth = r.baseColumnWidth(defaultBaseColumn)
	sh.DefaultRowHeight = r.standardPrinted * printerPixel
	sh.Page = defaultPage(r.paperW, r.paperH)
}

// columnWidth converts a width in characters of the Normal font's widest
// digit (ECMA-376 Part 1, 18.3.1.13 width, which counts the cell's
// padding in) to points as Excel prints it: the width in whole screen
// pixels, as the clause turns it into pixels at run time
// (Truncate(((256 * width + Truncate(128 / digit)) / 256) * digit)), read
// back in digits, times the digit's printer pixels, rounded to a pixel.
//
// # Corpus evidence
//
//   - probe, xlsx: Excel for the web prints Calibri 11 columns of 9.5
//     characters 443 printer pixels wide at 100% (66 screen pixels, 9.43
//     digits of 47), of 11 and 24 characters 517 and 1,128.
func (r *reader) columnWidth(chars float64) float64 {
	if !numeric.Positive(chars) {
		return 0
	}
	d := max(r.screenDigit, 1)
	chars = min(chars, maxColumnChars)
	px := math.Floor(float64((float64(256*chars)+math.Floor(128/d))/256*d) + 1e-9)
	return printerPixels(px / d * r.digitWidth)
}

// baseColumnWidth returns the width in points of a column of a sheet whose
// base column width is base characters (ECMA-376 Part 1, 18.3.1.81
// baseColWidth): the digits and Excel's padding in screen pixels, rounded
// up to its step, read back as characters truncated to 1/256 as Excel
// stores a width, then printed as any width.
func (r *reader) baseColumnWidth(base int) float64 {
	d := max(r.screenDigit, 1)
	px := math.Ceil((float64(float64(base)*d)+baseColumnPadding)/baseColumnStep) * baseColumnStep
	return r.columnWidth(math.Floor(px/d*256) / 256)
}

// maxColumnChars is the widest column in characters (ECMA-376 Part 1,
// 18.3.1.13: Excel caps a width at 255 characters).
const maxColumnChars = 255

// formatPr reads the sheet's format properties (ECMA-376 Part 1,
// 18.3.1.81): defaultColWidth, else baseColWidth with Excel's padding;
// defaultRowHeight, which a default within [standardSlack] of the Normal
// font's standard row and not marked customHeight leaves at the standard
// row ([standardRow]): 18.3.1.81 marks with customHeight a default "different
// from the default value", and another program's standard row for the
// same font, 14.4 or 14.25 points for Calibri 11, is no choice of the
// user's.
//
// # Corpus evidence
//
//   - libreoffice/xlsx, poi/xlsx: defaults of 14.4, 14.25, 14.5 and 14.85
//     points under Calibri 11 (19 to 19.8 screen pixels against a standard
//     of 20), 12.5, 13 and 13.2 under Arial 10 (16.7 to 17.6 against 17),
//     16 under Calibri 12, 15.75 under Times New Roman 12 and 24 under Comic
//     Sans MS 16 (32 against 34) print at the standard row:
//     sc/Book1_custom.xlsx, sc/tdf137543.xlsx, sc/tdf155046.xlsx,
//     chart2/barchart_outend.xlsx, 58896.xlsx and 56315.xlsx among them.
//   - libreoffice/xlsx: sc/forum-mso-en4-145327.xlsx keeps its default of
//     12.75 points under Calibri 11 (17 pixels against 20), and
//     openxmlsdk/xlsx NoExtDataB1.xlsx its 16.5 under Arial 10 (22
//     against 17).
func (w *sheetReader) formatPr(f *ooxml.Element) {
	r, sh := w.r, w.sh
	if v, err := strconv.ParseFloat(attr(f, "defaultColWidth"), 64); err == nil && v > 0 {
		sh.DefaultColumnWidth = r.columnWidth(v)
	} else if base := intAttr(f, "baseColWidth", defaultBaseColumn); base > 0 {
		sh.DefaultColumnWidth = r.baseColumnWidth(min(base, maxColumnChars))
	}
	if v, err := strconv.ParseFloat(attr(f, "defaultRowHeight"), 64); err == nil && numeric.PositiveFinite(v) {
		px := math.Round(float64(min(v, maxRowHeight) * screenPixelsPerPoint))
		if h := r.rowHeight(v); h > 0 && (boolAttr(f, "customHeight") || math.Abs(px-r.standardScreen) > standardSlack) {
			sh.DefaultRowHeight = h
			w.customDefault = boolAttr(f, "customHeight")
		}
	}
}

// standardSlack is how far, in screen pixels, a sheet's default row height
// not marked custom may stand from the Normal font's standard row and
// still print as it ([sheetReader.formatPr]).
const standardSlack = 2

// maxRowHeight is the tallest row in points (ECMA-376 Part 1, 18.3.1.73
// ht: Excel caps a row at 409 points).
const maxRowHeight = 409.5

// rowHeight returns a row height in points as Excel prints a row of ht
// points: its screen pixels times the Normal font's standard row in
// printer pixels over its standard row in screen pixels ([standardRow]),
// truncated to a printer pixel.
//
// # Corpus evidence
//
//   - litho/xlsx: row_heights prints rows of 6, 18, 30, 60 and 409.5
//     points 48, 145, 242, 484 and 3303 printer pixels tall; multipage_grid
//     prints rows of 15 points 121 apart.
//   - poi/xlsx: 56644.xlsx prints its rows of 14.45 points 116 pixels tall
//     (116.56 truncated), and libreoffice/xlsx sc/tdf118668.xlsx its rows of
//     12.75 points 102 (102.85), in Calibri 11 books; sc/tdf139934.xlsx
//     and sc/database.xlsx print Arial 10 rows of 12.75 points 103.
//   - libreoffice/xlsx: sc/tdf145129_DefaultRowHeight_28.35pt_SavedByExcel.xlsx
//     prints its custom default of 28.35 points 228 pixels tall (228.7), and
//     litho/xlsx fills_patterns its rows of 24 points 193 (193.6).
func (r *reader) rowHeight(ht float64) float64 {
	px := float64(min(ht, maxRowHeight)*screenPixelsPerPoint) * r.standardPrinted / r.standardScreen
	return math.Floor(px+1e-6) * printerPixel
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
		if v, err := strconv.ParseFloat(attr(re, "ht"), 64); err == nil && numeric.PositiveFinite(v) {
			row.Height = r.rowHeight(v)
		} else if w.customDefault {
			// A custom default height is the row's own.
			row.CustomHeight = true
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
				return fmt.Errorf("%w: more than %d cells", limits.ErrInputTooLarge, sheet.MaxCells)
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
// formula's other cells excepted. That is models.RecalculateAsNeeded;
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
	if f := c.Child(nsMain, "f"); attr(f, "t") == "array" && len(w.arrays) < maxArrayFormulas {
		if a, ok := rangeRef(attr(f, "ref")); ok && a.FirstRow == row.Index && a.FirstColumn == col {
			w.arrays = append(w.arrays, a)
		}
	}
	switch t := attr(c, "t"); t {
	case "s", "inlineStr":
		if (v != nil || c.Child(nsMain, "is") != nil) && w.arrayMember(row.Index, col) {
			// A cell of an array formula's range holds the formula's
			// result; Excel prints one that stores a string constant
			// there as #VALUE!.
			cell.Value = sheet.Value{Kind: sheet.Error, Text: "#VALUE!"}
			break
		}
		if t == "inlineStr" {
			rt := r.inlineText(c)
			cell.Value = sheet.Value{Kind: sheet.Text, Text: rt.text}
			cell.Runs = rt.runs
			break
		}
		if v != nil {
			if rt, ok := r.sharedText(v.Text); ok {
				cell.Value = sheet.Value{Kind: sheet.Text, Text: rt.text}
				cell.Runs = rt.runs
			}
		}
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
			if n, err := strconv.ParseFloat(strings.TrimSpace(v.Text), 64); err == nil && numeric.Finite(n) {
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
		cell.EmptyText = true
	}
	if f := c.Child(nsMain, "f"); f != nil && formulaFits(f.Text) && r.recalculation != models.RecalculateNever {
		// Under models.RecalculateNever no formula is kept: the cell shows
		// the result the file caches, nothing without one. Under
		// models.RecalculateAlways every formula is kept for the builder
		// to compute, a cached result or not.
		cell.Formula = strings.TrimSpace(f.Text)
		if v != nil && strings.TrimSpace(v.Text) != "" && r.recalculation == models.RecalculateAsNeeded {
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
	if f := c.Child(nsMain, "f"); f != nil && cell.Value.Kind != sheet.Empty {
		if href, ok := hyperlinkFormula(f.Text); ok {
			cell.Href = href
		}
	}
	if cell.Value.Kind == sheet.Empty && cell.Style == nil && cell.Formula == "" && !cell.EmptyText {
		return cell, false
	}
	return cell, true
}

// hyperlinkFormula returns the target of a formula that is a call of
// HYPERLINK whose first argument, link_location, is a string literal
// (ECMA-376 Part 1, 18.17.7.170): the cell links to it, as Excel prints
// it. A location in the workbook, written "#Sheet2!A1", keeps its #, as a
// hyperlink element's location reads ([sheetReader.hyperlinks]). Any other
// formula, or a target computed by an expression, links nowhere.
//
// # Corpus evidence
//
//   - litho/xlsx hyperlinks: Excel's PDF links the cell whose formula is
//     HYPERLINK("https://github.com/gotenberg","GitHub") to that address.
func hyperlinkFormula(f string) (string, bool) {
	f = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(f), "="))
	const name = "HYPERLINK("
	if len(f) < len(name) || !strings.EqualFold(f[:len(name)], name) {
		return "", false
	}
	rest := strings.TrimLeft(f[len(name):], " ")
	if !strings.HasPrefix(rest, `"`) {
		return "", false
	}
	var b strings.Builder
	i := 1
	for ; i < len(rest); i++ {
		if rest[i] != '"' {
			b.WriteByte(rest[i])
			continue
		}
		if i+1 < len(rest) && rest[i+1] == '"' {
			// A doubled quote is one quote in the string.
			b.WriteByte('"')
			i++
			continue
		}
		break
	}
	if i >= len(rest) {
		return "", false
	}
	after := strings.TrimSpace(rest[i+1:])
	if !strings.HasPrefix(after, ",") && after != ")" {
		return "", false
	}
	target := strings.TrimSpace(b.String())
	return target, target != ""
}

// richTextOf decodes the text of a formula's cached string value.
func (r *reader) richTextOf(s string) string {
	s = decodeXString(s)
	if n := []rune(s); len(n) > maxCellRunes {
		s = string(n[:maxCellRunes])
	}
	return s
}

// maxFormulaRunes caps the formula text kept for a cell, in characters.
// Anti-DoS guard, not spec-mandated: Excel stops at 8,192 characters.
const maxFormulaRunes = 8192

// formulaFits reports whether the formula text s holds at most
// [maxFormulaRunes] characters. A text of more bytes than the cap's longest
// UTF-8 form is not counted.
func formulaFits(s string) bool {
	return len(s) <= utf8.UTFMax*maxFormulaRunes && utf8.RuneCountInString(s) <= maxFormulaRunes
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
			if rel, ok := r.pkg.RelationshipByID(w.r.ctx, w.part, id); ok && rel.External {
				target = linkURL(rel.Target)
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
// chart, its page setup, header and footer as a worksheet's, printed on
// one page ([chartsheetPage]). A chart without a series prints its frame,
// as Excel prints it.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/tdf107586.xlsx, openxmlsdk/xlsx:
//     v2FxTestFiles/spreadsheet/ChartSheet.xlsx, poi/xlsx:
//     spreadsheet/56557.xlsx: charts without a series, whose chartsheets
//     Excel prints as the chart's frame alone, without the title one of
//     them names, where LibreOffice prints a blank page.
func (r *reader) chartsheet(name string, sh *sheet.Sheet) error {
	root, err := r.pkg.ParseXML(r.ctx, name, understood)
	if err != nil {
		if isMalformed(err) {
			r.warn(warn.PartMalformed, warn.Omitted, fmt.Sprintf("sheet %q does not parse: it prints empty: %v", sh.Name, err))
			r.defaultSetup(sh)
			return nil
		}
		return err
	}
	w := &sheetReader{r: r, part: name, sh: sh, rowAt: map[int]int{}}
	r.defaultSetup(sh)
	w.pageSetup(root)
	if hf := root.Child(nsMain, "headerFooter"); hf != nil {
		sh.HeaderFooter = r.headerFooter(hf, true, nil)
	}
	if d := root.Child(nsMain, "drawing"); d != nil {
		id, _ := d.AttrNS(ooxml.NSR, "id")
		if err := w.drawing(id); err != nil {
			return err
		}
	}
	// A chart without a series prints its frame alone, without title or
	// legend, as Excel prints it; a frame that shows nothing is an
	// artifact (ISO 14289-1, 7.1).
	for i := range sh.Drawings {
		if d := &sh.Drawings[i]; d.Chart != nil && chartSeries(d.Chart) == 0 {
			d.Chart.Title, d.Chart.Legend = nil, nil
			d.Decorative = true
		}
	}
	chartsheetPage(sh)
	return nil
}

// chartsheetPage lays the chartsheet sh out on one page, as Excel prints
// a chartsheet: its chart at the size of its drawing, from the top-left
// margin, unscaled, cut at the right margin when it is wider than the
// page. The sheet holds no cell: its first cell, the one it prints, spans
// every drawing, its column cut to the page's width as a wider column is.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/tdf107586.xlsx, a 731.7 by 477.9 point chart
//     on a chartsheet without a pageSetup, prints on one Letter page from
//     the 0.7 and 0.75 inch margins, 477.9 points tall, cut at the right
//     margin; openxmlsdk/xlsx v2FxTestFiles/spreadsheet/ChartSheet.xlsx,
//     684.5 by 496.5 points, the same.
//   - litho/xlsx: chart_sheet's two chartsheets, on A4 landscape and
//     Letter portrait, print their 682.7 by 495.5 and 472.4 by 629.9
//     point charts at their size from the margins.
//   - probe, xlsx: a 733 by 479 point chart on Letter portrait and on
//     Letter landscape prints at its size, cut at the right margin; a 300
//     by 200 point chart on Letter portrait at its size. On chartsheets
//     without a pageSetup, the same workbook printed both charts 733 by
//     479 points on A4 landscape: not followed, the corpus's chartsheets
//     without one print on the default paper at their size.
func chartsheetPage(sh *sheet.Sheet) {
	p := &sh.Page
	p.Scale, p.FitWidth, p.FitHeight = 100, 0, 0
	right := p.Width - p.Margin[css.Left] - p.Margin[css.Right]
	bottom := p.Height - p.Margin[css.Top] - p.Margin[css.Bottom]
	for _, d := range sh.Drawings {
		if d.Absolute && numeric.Finite(d.X+d.Width) && numeric.Finite(d.Y+d.Height) {
			right, bottom = max(right, d.X+d.Width), max(bottom, d.Y+d.Height)
		}
	}
	sh.DefaultColumnWidth = min(right+1, office.MaxPageSide)
	sh.DefaultRowHeight = min(bottom+1, office.MaxPageSide)
	p.PrintAreas = []sheet.Range{{}}
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

// linkURL returns the URI Excel writes for an external link target: a
// Windows network path, \\host\share\file or the file URL Excel writes
// for one (file:///\\host\share\file), as the file URL
// file://host/share/file; any other file URL or relative path with its
// backslashes turned to slashes; a target of another scheme unchanged.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/129969-min.xlsx links file:///\\ndhlis-cbut018\CBUT018\...,
//     which Excel prints as file://ndhlis-cbut018/CBUT018/....
//   - libreoffice/xlsx: sc/tdf111876.xlsx links ..\xls\bug-fixes.xls,
//     which Excel prints as ../xls/bug-fixes.xls; sc/chart_hyperlink.xlsx
//     links a chart to file:///C:\TEMP\test.xlsx, which Excel prints as
//     file:///C:/TEMP/test.xlsx.
func linkURL(target string) string {
	rest, ok := strings.CutPrefix(target, `file:///\\`)
	if !ok {
		rest, ok = strings.CutPrefix(target, `\\`)
	}
	if ok && rest != "" {
		return "file://" + strings.ReplaceAll(rest, `\`, "/")
	}
	if scheme, ok := uriScheme(target); ok && !strings.EqualFold(scheme, "file") {
		return target
	}
	return strings.ReplaceAll(target, `\`, "/")
}

// uriScheme returns the scheme of the URI s (RFC 3986, 3.1: a letter, then
// letters, digits, "+", "-" or "."), false when s starts with none. One
// letter before the colon is a Windows drive, not a scheme.
func uriScheme(s string) (string, bool) {
	i := strings.IndexByte(s, ':')
	if i < 2 {
		return "", false
	}
	for k := range i {
		c := s[k]
		letter := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		other := (c < '0' || c > '9') && c != '+' && c != '-' && c != '.'
		if !letter && (k == 0 || other) {
			return "", false
		}
	}
	return s[:i], true
}

// joinLines drops the line feeds and carriage returns of the text of a
// cell that does not wrap (ECMA-376 Part 1, 18.8.1 alignment wrapText), a
// pair escaped as _x000D_ and a line feed included: Excel prints such a
// cell on one line and gives its line ends no room.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/cell-multi-line.xlsx prints "Line1\nLine2\nLine3"
//     on one line in its cell without wrapText, each line feed a space
//     taken back to no advance, and on three lines in the one with it;
//     sc/escape-unicode.xlsx prints "Line 1_x000D_\nLine 2_x000D_\n..." on
//     one line, the carriage return and the line feed each a space taken
//     back to no advance.
//   - libreoffice/xlsx: sc/preserve_space.xlsx prints a run "123\n" and a
//     run "456" with a gap of 5.6 points between them, at the end of a run,
//     which this rule does not give.
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

// dropFeeds returns s without its line feeds and carriage returns.
func dropFeeds(s string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(s)
}
