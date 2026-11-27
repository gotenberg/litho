package ods

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/formula"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
	"github.com/gotenberg/litho/pkg/models"
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
	// maxGroupDepth bounds the nesting of column and row groups, header
	// columns and rows, and their table:table-columns and table:table-rows
	// read: 32. Anti-DoS guard, not spec-mandated: Calc nests groups 8
	// deep.
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
	// locale is the options' locale, which the charts and the data
	// styles that name no language format under.
	locale models.Locale
	ctx    context.Context
	pkg    *odf.Package
	fonts  *text.Fonts
	styles *odf.Styles
	data   *odf.DataStyles
	draw   *odf.DrawStyles

	doc      *sheet.Document
	cellSty  map[string]*sheet.Style
	condSty  map[string]*sheet.Style
	pages    map[string]pageResult
	pics     map[string]*picResult
	cells    int
	rows     int
	merges   int
	drawings int
	// sparks counts the sparklines placed, against maxSparklines.
	sparks int
	// names maps a named range's name, in lower case, to the cell it
	// starts at as a link target in the book ("Sheet.A1").
	names map[string]string
	// keepFormulas keeps the formulas of the cells for the builder to
	// compute; off, under models.RecalculateNever, a cell shows the value
	// the file stores.
	keepFormulas bool

	// mathSetter lays the document's formula objects out, made at the
	// first.
	mathSetter *formula.Setter

	warned   map[string]bool
	warnings []typeset.Warning
	err      error
	steps    int
}

// newReader returns a reader of pkg.
func newReader(ctx context.Context, pkg *odf.Package, fonts *text.Fonts) *reader {
	return &reader{
		ctx: ctx, pkg: pkg, fonts: fonts, styles: odf.NewStyles(pkg), data: odf.NewDataStyles(pkg),
		keepFormulas: true, cellSty: map[string]*sheet.Style{}, condSty: map[string]*sheet.Style{}, pages: map[string]pageResult{}, pics: map[string]*picResult{},
		warned: map[string]bool{}, paperW: sheet.FallbackWidth, paperH: sheet.FallbackHeight,
	}
}

// warn records a warning once per construct: what is a fixed word that
// names it, never the document's data, which goes in msg. Several
// constructs share a code, each warned of once per effect.
func (r *reader) warn(what string, code warn.Code, effect warn.Effect, msg string) {
	k := what + "\x00" + effect.String()
	if r.warned[k] {
		return
	}
	r.warned[k] = true
	r.warnings = append(r.warnings, typeset.Warning{Code: code, Effect: effect, Message: msg})
}

// chartWarning returns the construct a chart's warning of code is warned
// of once as: its missing values, or the rest of what it does not draw.
func chartWarning(code warn.Code) string {
	if code == warn.ChartNoCache {
		return "chart-no-cache"
	}
	return "chart-unsupported"
}

// stop records err as the reader's error when it ends the conversion (see
// limits.Stops) and reports whether it does. A construct whose error does
// not stop the conversion is left out with a warning.
func (r *reader) stop(err error) bool {
	if !limits.Stops(err) {
		return false
	}
	if r.err == nil {
		r.err = err
	}
	return true
}

// ctxCheckInterval is the steps between two checks of ctx: 4,096, a
// check cheap against the work between.
const ctxCheckInterval = 4096

// tick checks ctx every [ctxCheckInterval] steps.
func (r *reader) tick() bool {
	if r.err != nil {
		return false
	}
	r.steps++
	if r.steps%ctxCheckInterval == 0 {
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
	if r.doc.Meta.Lang == "" {
		// A spreadsheet whose metadata names no language states the
		// language of its text in its default cell style, which every
		// cell's text takes unless its own style says otherwise.
		r.doc.Meta.Lang = odf.Lang(r.styles.Default(odf.FamilyCell, odf.TextProps))
	}
	if n, err := intconv.Atoi(r.styles.Default(odf.FamilyCell, odf.CellProps).Get("style:decimal-places")); err == nil && n >= 0 {
		r.doc.GeneralDecimals, r.doc.LimitGeneral = min(n, maxGeneralDecimals), true
	}
	var noPrint []bool
	for _, c := range body.Children {
		switch {
		case c.Is(odf.NSTable, "table"):
			if len(r.doc.Sheets) >= maxSheets {
				r.warn("too-many-sheets", warn.LimitReached, warn.Omitted, fmt.Sprintf("sheets past %d are not read", maxSheets))
				continue
			}
			if sh := r.table(c); sh != nil {
				r.doc.Sheets = append(r.doc.Sheets, sh)
				noPrint = append(noPrint, c.A(odf.NSTable, "print") == "false" && len(sh.Page.PrintAreas) == 0)
			}
		case c.Is(odf.NSTable, "named-expressions"):
			r.namedRanges(c)
		case c.Is(odf.NSTable, "data-pilot-tables"):
			r.warn("data-pilot", warn.ContentUnsupported, warn.Approximated, "pivot tables are shown as the cells the file stores")
		case c.Is(odf.NSTable, "content-validations"):
			r.warn("validations", warn.ContentUnsupported, warn.Approximated, "cell validations are not applied")
		case c.Is(odf.NSTable, "database-ranges"), c.Is(odf.NSTable, "calculation-settings"):
			// They print nothing of their own: no warning.
		}
		if r.err != nil {
			return nil
		}
	}
	hidePrintless(r.doc.Sheets, noPrint)
	r.linkNames()
	return r.doc
}

// maxGeneralDecimals bounds the decimals a General number is rounded to.
// Not spec-mandated: a float64 holds 17 significant digits, so rounding
// past 20 decimals changes no number Calc shows.
const maxGeneralDecimals = 20

// maxNames caps the named ranges read. Anti-DoS guard, not
// spec-mandated: a spreadsheet names a few hundred ranges at most.
const maxNames = 4096

// namedRanges reads the table:named-range elements of a
// table:named-expressions (ODF 1.3, Part 3, 9.4.12) as link targets: the
// first cell of each range. Named expressions and the names in formulas
// are not evaluated.
func (r *reader) namedRanges(el *odf.Element) {
	if len(el.Children) > 0 {
		r.warn("named-expressions", warn.ContentUnsupported, warn.Approximated, "named ranges and expressions are not evaluated in formulas")
	}
	for _, c := range el.Children {
		if !c.Is(odf.NSTable, "named-range") || len(r.names) >= maxNames {
			continue
		}
		name := strings.ToLower(c.A(odf.NSTable, "name"))
		addr, _, _ := strings.Cut(c.A(odf.NSTable, "cell-range-address"), ":")
		addr = strings.ReplaceAll(addr, "$", "")
		if name == "" || addr == "" {
			continue
		}
		if r.names == nil {
			r.names = map[string]string{}
		}
		if _, ok := r.names[name]; !ok {
			r.names[name] = addr
		}
	}
}

// linkNames points the cells' links to a named range ("#Name") at the
// range's first cell, a link that names a sheet keeping it; a link no
// name matches stays as it is.
//
// # Corpus evidence
//
//   - libreoffice/ods: sc/tdf118086.ods links "#Kameras_Anfang" and
//     "#Kameras_Ende", ranges of one cell on the first sheet, which
//     LibreOffice exports as links to page 1.
func (r *reader) linkNames() {
	if len(r.names) == 0 {
		return
	}
	for _, sh := range r.doc.Sheets {
		for i := range sh.Rows {
			for j := range sh.Rows[i].Cells {
				c := &sh.Rows[i].Cells[j]
				target, ok := strings.CutPrefix(c.Href, "#")
				if !ok || r.sheetNamed(target) {
					continue
				}
				if addr, ok := r.names[strings.ToLower(target)]; ok {
					c.Href = "#" + addr
				}
			}
		}
	}
}

// sheetNamed reports a link target that names a sheet of the book.
func (r *reader) sheetNamed(target string) bool {
	for _, sh := range r.doc.Sheets {
		if strings.EqualFold(sh.Name, target) {
			return true
		}
	}
	return false
}

// hidePrintless hides the sheets that print nothing: table:print false
// without print ranges (ODF 1.3, Part 3, 19.697), as Calc reads a sheet
// that neither prints entirely nor has ranges, when another shown sheet
// prints. In a document where no shown sheet prints, every sheet prints
// whole, as Calc does when no sheet has a print range: Calc departs there
// from 19.697, whose false is "table cannot be printed".
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
func (r *reader) metadata() typeset.Metadata {
	var m typeset.Metadata
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
	// standard holds the runs of rows without cells at Calc's standard
	// height, which the model leaves out while the sheet's default is that
	// height.
	standard []pendingRows
	// header rows and columns seen.
	headerRows, headerCols *sheet.Range
	// rowHidden reports the row whose frames are being read hidden.
	rowHidden bool
	// rowStyle is the default cell style of the row being read, nil for
	// none.
	rowStyle *sheet.Style
	// matrices are the ranges of the array formulas read, whose cells
	// hold the array's result.
	matrices []sheet.Range
	// forms is the table's office:forms (ODF 1.3, Part 3, 13.2), nil for
	// none, and formIDs its index by id, made at the first control.
	forms   *odf.Element
	formIDs map[string]*odf.Element
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
	ts := &tableState{sh: sh, colX: []float64{0}, forms: el.Child(odf.NSOffice, "forms")}
	// The sheet's own shapes come first in drawing order, as they come
	// first in the table (ODF 1.3, Part 3, 9.1.2).
	for _, c := range el.All(odf.NSTable, "shapes") {
		for _, s := range c.Children {
			if !s.IsText() {
				r.drawing(ts, s, nil, 0)
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
			r.namedRanges(c)
		case c.Is(nsCalcExt, "conditional-formats"):
			r.conditionalFormats(sh, c)
			calcext = true
		case c.Is(nsCalcExt, "sparkline-groups"):
			r.sparklines(ts, c)
		case c.Is(odf.NSOffice, "forms"):
			if len(c.Children) > 0 {
				r.warn("form-controls", warn.FormWidgetOmitted, warn.Omitted, "form controls are not drawn")
			}
		case c.Is(odf.NSTable, "scenario"):
			r.warn("scenarios", warn.ContentUnsupported, warn.Approximated, "scenarios are not read")
		}
	}
	if !calcext {
		r.styleMaps(sh)
	}
	// The rows a drawing reaches place it, though no cell holds a value
	// there.
	r.flushPending(ts, drawnRows(sh))
	r.listStandardRows(ts, max(drawnRows(sh), lastListed(sh)))
	return sh
}

// repeat returns the repetition count of the attribute local of el
// (ODF 1.3, Part 3, 19.677 and 19.678): at least 1, at most limit.
func repeat(el *odf.Element, local string, limit int) int {
	n, err := intconv.Atoi(el.A(odf.NSTable, local))
	if err != nil || n < 1 {
		return 1
	}
	return min(n, max(limit, 1))
}

// columns reads the table:table-column elements of el and of its column
// groups and header columns (ODF 1.3, Part 3, 9.1.6 to 9.1.11). A group
// whose table:display is false is collapsed: its columns are hidden.
// depth is the number of elements around el that hold columns; columns
// nested past [maxGroupDepth] are left out, with a warning.
func (r *reader) columns(ts *tableState, el *odf.Element, hidden, header bool, depth int) {
	if depth > maxGroupDepth {
		r.warn("table-group-depth", warn.LimitReached, warn.Approximated, fmt.Sprintf("column and row groups nested past %d levels are not read", maxGroupDepth))
		return
	}
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
			r.columns(ts, c, hidden || c.A(odf.NSTable, "display") == "false", header, depth+1)
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
		width = calcTwips(w)
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
	return ts.colX[len(ts.colX)-1] + float64(float64(c-len(ts.colX)+1)*ts.sh.DefaultColumnWidth)
}

// rowsOf reads the rows of el and of its row groups and header rows
// (ODF 1.3, Part 3, 9.1.3, 9.1.7, 9.1.9 and 9.1.10). depth is the number
// of elements around el that hold rows; rows nested past [maxGroupDepth]
// are left out, with a warning.
func (r *reader) rowsOf(ts *tableState, el *odf.Element, hidden, header bool, depth int) {
	if depth > maxGroupDepth {
		r.warn("table-group-depth", warn.LimitReached, warn.Omitted, fmt.Sprintf("column and row groups nested past %d levels are not read", maxGroupDepth))
		return
	}
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
			r.rowsOf(ts, c, hidden || c.A(odf.NSTable, "display") == "false", header, depth+1)
		}
	}
}

// row reads one table:table-row (ODF 1.3, Part 3, 9.1.3) and its
// repetitions: a row holding cells is listed for every repetition, up to
// the cell cap; a row without cells only when its height, hidden flag or
// break is its own. A row whose style sets no style:row-height takes
// Calc's standard height.
//
// # Corpus evidence
//
//   - probe, ods: through gotenberg/gotenberg:9.4.1, rows without a style
//     before, between and after rows of 30 mm print 12.78 points apart, the
//     standard height, three empty repeated ones included.
//   - litho/ods: cjk-cells prints its unstyled first row at the standard
//     height above three rows of 45 mm.
//   - libreoffice/ods: sc/tdf121040.ods prints its rows, whose style says
//     style:use-optimal-row-height false and sets no height, as tall as
//     their content.
//   - probe, ods: through gotenberg/gotenberg:9.4.1, rows whose style
//     states 3 mm without style:use-optimal-row-height print 3 mm tall
//     under text of 10 to 24 points.
//   - libreoffice/ods: sc/lostRowStyle.fods prints its rows of 1 cm and
//     2 cm, whose style says nothing of an optimal height, at those
//     heights.
func (r *reader) row(ts *tableState, el *odf.Element, hidden, header bool) {
	n := repeat(el, "number-rows-repeated", maxRows-ts.row)
	styleName := el.A(odf.NSTable, "style-name")
	props := r.styles.Resolve(odf.ScopeContent, odf.FamilyRow, styleName, odf.RowProps)
	tmpl := sheet.Row{
		Hidden: hidden || el.A(odf.NSTable, "visibility") == "collapse" || el.A(odf.NSTable, "visibility") == "filter",
		Style:  r.cellStyle(el.A(odf.NSTable, "default-cell-style-name")),
	}
	// A row whose style states its height keeps it unless
	// style:use-optimal-row-height says true (ODF 1.3, Part 3, 20.391):
	// Calc sets the height it reads as the user's own.
	optimal := props.Get("style:use-optimal-row-height") == "true"
	if h, ok := odf.Length(props.Get("style:row-height")); ok && h > 0 {
		// Calc measures again, as it loads the file, only the rows of
		// optimal height up to recalcRowLimit; past it, a row whose style
		// states its height keeps it.
		measured := optimal && ts.row+n-1 <= recalcRowLimit
		h = calcTwips(h)
		if measured {
			h = r.optimalFloor(h)
		}
		tmpl.Height = h
		ts.lastRowHeight = h
		tmpl.CustomHeight = !measured
	} else {
		// A row whose style sets no height, or that names no style, is
		// Calc's standard height, whatever the rows before it are, and
		// grows to its content: without a height, there is none to keep.
		tmpl.Height, ts.lastRowHeight = defaultRowHeight, defaultRowHeight
	}
	brk := props.Get("fo:break-before") == "page"
	if header {
		ts.headerRows = extend(ts.headerRows, ts.row, ts.row+n-1, true)
	}
	start := ts.row
	ts.rowStyle = tmpl.Style
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
		r.drawing(ts, f.el, &f.at, 0)
	}
	ts.rowHidden = false
	if len(cells) == 0 {
		// A row without cells is listed for a height or a hidden flag of
		// its own alone, once a later row needs its index.
		if tmpl.Hidden || tmpl.Height > 0 && math.Abs(tmpl.Height-defaultRowHeight) > rowHeightSlack {
			row := tmpl
			row.Index = start
			ts.pending = append(ts.pending, pendingRows{row: row, n: n})
		} else if len(ts.standard) < maxStandardRuns {
			row := tmpl
			row.Index = start
			ts.standard = append(ts.standard, pendingRows{row: row, n: n})
		}
		// A merge of empty cells is kept: Calc prints the pages it
		// reaches ([sheet.Sheet.Merges]).
		for i := 0; i < n && len(merges) > 0; i++ {
			r.addMerges(ts, start+i, merges)
		}
		ts.row = start + n
		ts.rowY += float64(float64(n) * height)
		return
	}
	for i := range n {
		if r.cells+len(cells) > sheet.MaxCells || r.rows >= maxRowEntries {
			r.warn("too-many-cells", warn.LimitReached, warn.Omitted, fmt.Sprintf("cells past %d are not read", sheet.MaxCells))
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
			r.stop(err)
			break
		}
		r.cells += len(cells)
		r.rows++
		ts.sh.Rows = append(ts.sh.Rows, row)
		r.addMerges(ts, idx, merges)
	}
	ts.row = start + n
	ts.rowY += float64(float64(n) * height)
}

// addMerges adds the merges of the row idx, each row offset from it, up to
// [maxMerges] in the document.
func (r *reader) addMerges(ts *tableState, idx int, merges []sheet.Range) {
	for _, m := range merges {
		if r.merges >= maxMerges {
			return
		}
		r.merges++
		ts.sh.Merges = append(ts.sh.Merges, sheet.Range{FirstRow: idx, FirstColumn: m.FirstColumn, LastRow: min(idx+m.LastRow, maxRows-1), LastColumn: m.LastColumn})
	}
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

// overrides reports an empty cell of column col of the row being read,
// in the style st it names, that must be listed though it shows nothing:
// a cell that names its style is in that style, not in the default cell
// style of its row or column (ODF 1.3, Part 3, 19.615
// table:default-cell-style-name), which the builder gives a cell it does
// not list.
func (r *reader) overrides(ts *tableState, col int, st *sheet.Style) bool {
	inherited := ts.rowStyle
	if inherited == nil {
		cols := ts.sh.Columns
		i := sort.Search(len(cols), func(i int) bool { return cols[i].Last >= col })
		if i < len(cols) && cols[i].First <= col {
			inherited = cols[i].Style
		}
	}
	return inherited != nil && inherited != st
}

// setsApart reports a cell style name, resolved as st, that an empty cell
// must be listed for: its style:map conditions apply to the cells that
// name it, empty ones included (16.3), and a font of another size than
// the document default's sets the height Calc measures its row to.
func (r *reader) setsApart(name string, st *sheet.Style) bool {
	return st.Font.Size != r.doc.Default.Font.Size || r.mapped(name)
}

// mapped reports a cell style that holds style:map conditions.
func (r *reader) mapped(name string) bool {
	st := r.styles.Style(odf.ScopeContent, odf.FamilyCell, name)
	if st == nil {
		return false
	}
	for _, m := range st.El.Children {
		if m.Is(odf.NSStyle, "map") {
			return true
		}
	}
	return false
}

// recalcRowLimit is the last row index, from 0, of the rows of optimal
// height Calc measures again as it loads a spreadsheet when the row's
// style states a height: a table:table-row element whose last row lies
// past it keeps the height it stores (ScXMLTableRowContext, its "first
// 200 rows" rule, sc/source/filter/xml/xmlrowi.cxx). A row whose style
// states no height is measured wherever it lies.
//
// # Corpus evidence
//
//   - probe, ods: through gotenberg/gotenberg:9.4.1, 600 rows of optimal
//     height stored 3 mm tall, alternating Liberation Sans 14 and 10
//     point text, print rows 0 to 200 at their measured heights and
//     every row after at 3 mm; without the stored height, every row is
//     measured; marked not optimal, none is.
const recalcRowLimit = 200

// optimalFloor returns the height h of a row of optimal height raised to
// Calc's standard row height when the document's default font is Calc's
// own, Liberation Sans 10: Calc recomputes the height of every row of
// optimal height it reads (ScXMLTableRowContext::endFastElement,
// sc/source/filter/xml/xmlrowi.cxx, and ScDocRowHeightUpdater), and a
// row of that font is never shorter. A taller row keeps h: the sheet's
// optimal heights grow it to its text.
//
// # Corpus evidence
//
//   - probe, ods: rows of optimal height of 0.1in, 0.1681in, 0.3in and
//     0.5in without text, converted by Gotenberg 9.4.1, all print 12.784
//     points apart, 256 twips.
//   - libreoffice/ods: sc/cell-anchored-shapes.ods, rows of optimal height
//     of 0.1681in written by LibreOffice 3.6, prints them 256 twips tall.
func (r *reader) optimalFloor(h float64) float64 {
	f := r.doc.Default.Font
	if f.Size != defaultFontSize || len(f.Families) == 0 || f.Families[0] != defaultFamily {
		return h
	}
	return max(h, defaultRowHeight)
}

// calcTwips returns a row height or column width of v points as Calc
// reads it from a file: the length in 1/100 mm, rounded, the unit of the
// Height and Width properties the ODF import sets (xmloff's measure
// converter), then in twips, rounded (ScTableRowsObj and
// ScTableColumnsObj::setPropertyValue, o3tl::toTwips from mm100,
// sc/source/ui/unoobj/docuno.cxx), as Calc holds both. The value
// returned is that count of twips in points.
//
// # Corpus evidence
//
//   - probe, ods: rows of 0.208in (528.32 hundredths), 0.5cm, 5.3mm,
//     15.3pt, 15.33pt, 0.6cm and 0.3333in, converted by Gotenberg 9.4.1,
//     print as rows of 299, 283, 300, 306, 307, 340 and 480 twips: 0.208in
//     rounded straight to twips would be 300. A row of 5.307mm (530.7
//     hundredths) prints as 301 twips and one of 5.303mm as 300: the
//     hundredths round before the twips do. Columns follow the same rule.
//   - probe, ods: rows of 0.2468in (17.77 points, 355 twips) print 41 a
//     page on an A4 body of 728.5 points, which 17.77-point rows would
//     fill with 40.
func calcTwips(v float64) float64 {
	hmm := math.Round(v * 2540 / 72)
	return math.Round(hmm*72/127) / 20
}

// rowHeightSlack is the difference under which a row's height is the
// default's: 0.05 points, the rounding of the inches an ODS file writes
// (0.178in is 12.816 points for Calc's 12.8).
const rowHeightSlack = 0.05

// maxStandardRuns bounds the runs of standard rows a sheet remembers.
// Anti-DoS guard, not spec-mandated: each run is one row element, and a
// sheet Calc writes holds a few hundred at most.
const maxStandardRuns = 4096

// listStandardRows lists the rows of standard height before the row
// index next that the model left out, when the sheet's default row height,
// the height of its last row element, is another: those rows keep Calc's
// standard height, not the default the rows the model omits take.
//
// # Corpus evidence
//
//   - libreoffice/ods: sc/tdf115655_HideDetail.ods prints its picture,
//     anchored in the second row of 74.86 mm, below a first row of the
//     standard height.
func (r *reader) listStandardRows(ts *tableState, next int) {
	if len(ts.standard) == 0 || math.Abs(ts.sh.DefaultRowHeight-defaultRowHeight) <= rowHeightSlack {
		return
	}
	added := false
	for _, p := range ts.standard {
		for i := 0; i < p.n && p.row.Index+i < next; i++ {
			if r.rows >= maxRowEntries {
				break
			}
			row := p.row
			row.Index += i
			r.rows++
			ts.sh.Rows = append(ts.sh.Rows, row)
			added = true
		}
	}
	if added {
		sort.SliceStable(ts.sh.Rows, func(i, j int) bool { return ts.sh.Rows[i].Index < ts.sh.Rows[j].Index })
	}
}

// lastListed returns one past the last row the sheet lists, 0 for none.
func lastListed(sh *sheet.Sheet) int {
	if len(sh.Rows) == 0 {
		return 0
	}
	return sh.Rows[len(sh.Rows)-1].Index + 1
}

// drawnRows returns one past the last row a drawing anchored to cells
// reaches, 0 for none.
//
// # Corpus evidence
//
//   - libreoffice/ods: sc/tdf137044_CoverHiddenRows.ods anchors a
//     rectangle 5 mm into the fourth row, below three rows of 10 mm and no
//     cell, and Calc prints its top 35 mm below the sheet's origin.
func drawnRows(sh *sheet.Sheet) int {
	n := 0
	for _, d := range sh.Drawings {
		if d.Absolute {
			continue
		}
		n = max(n, d.From.Row+1)
		if d.To != nil {
			n = max(n, d.To.Row+1)
		}
	}
	return n
}

// flushPending lists the pending rows without cells that come before the
// row index next, and keeps those from next on pending.
func (r *reader) flushPending(ts *tableState, next int) {
	var rest []pendingRows
	for _, p := range ts.pending {
		i := 0
		for ; i < p.n && p.row.Index+i < next; i++ {
			if r.rows >= maxRowEntries {
				ts.pending = nil
				return
			}
			row := p.row
			row.Index += i
			r.rows++
			ts.sh.Rows = append(ts.sh.Rows, row)
		}
		if i < p.n {
			p.row.Index += i
			p.n -= i
			rest = append(rest, p)
		}
	}
	ts.pending = rest
}

// maxMatrices bounds the array formula ranges a sheet remembers.
// Anti-DoS guard, not spec-mandated: a sheet holds a few array formulas.
const maxMatrices = 1024

// inMatrix reports a cell inside the range of an array formula read.
func (ts *tableState) inMatrix(r, c int) bool {
	for _, m := range ts.matrices {
		if r >= m.FirstRow && r <= m.LastRow && c >= m.FirstColumn && c <= m.LastColumn {
			return true
		}
	}
	return false
}

// calcError reports the text Calc shows for an error value: #NULL!,
// #DIV/0!, #VALUE!, #REF!, #NAME?, #NUM!, #N/A, or Err: and its code
// (ScGlobal::GetErrorString, ScCompiler::GetErrorConstant).
//
// # Corpus evidence
//
//   - libreoffice/ods: sc/cachedValue.ods saves =SQRT(-1) and =NA(), in
//     cells and in array formulas, as the number 0 with the text Err:502
//     and #N/A, and Calc prints Err:502 and #N/A in every cell of them and
//     of the formulas that refer to them.
func calcError(s string) bool {
	switch s {
	case "#NULL!", "#DIV/0!", "#VALUE!", "#REF!", "#NAME?", "#NUM!", "#N/A":
		return true
	}
	code, ok := strings.CutPrefix(s, "Err:")
	if !ok || code == "" || len(code) > 3 {
		return false
	}
	for _, d := range code {
		if d < '0' || d > '9' {
			return false
		}
	}
	return true
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
		if mc, mr := repeat(c, "number-matrix-columns-spanned", maxColumns-col), repeat(c, "number-matrix-rows-spanned", maxRows-ts.row); (mc > 1 || mr > 1) && len(ts.matrices) < maxMatrices {
			ts.matrices = append(ts.matrices, sheet.Range{FirstRow: ts.row, FirstColumn: col, LastRow: ts.row + mr - 1, LastColumn: col + mc - 1})
		}
		if cell.Value.Kind == sheet.Number && calcError(cell.Value.Text) && (c.A(odf.NSTable, "formula") != "" || ts.inMatrix(ts.row, col)) {
			// Calc reads the cached result of a formula, or of a cell of
			// an array formula's range, whose text is an error as that
			// error, whatever number the cell stores.
			cell.Value = sheet.Value{Kind: sheet.Error, Text: cell.Value.Text}
		}
		name := c.A(odf.NSTable, "style-name")
		named := name != ""
		if named && cell.Style == nil {
			cell.Style = &r.doc.Default
		}
		// An empty cell is listed for its style alone when the style
		// sets something the builder reads off the cells it lists.
		own := named && !ok && r.setsApart(name, cell.Style)
		cs := repeat(c, "number-columns-spanned", maxColumns-col)
		rs := repeat(c, "number-rows-spanned", maxRows-ts.row)
		for _, k := range c.Children {
			switch {
			case k.Name.Space == odf.NSDraw:
				frames = append(frames, frameAt{el: k, at: sheet.Anchor{Row: ts.row, Column: col}})
			case k.Is(odf.NSOffice, "annotation"):
				r.note(ts.sh, k, ts.row, col)
			case k.Is(odf.NSTable, "detective"):
				for _, h := range k.Children {
					if h.Is(odf.NSTable, "highlighted-range") && h.A(odf.NSTable, "marked-invalid") == "true" {
						cell.MarkedInvalid = true
					}
				}
			}
		}
		for i := range n {
			if (ok || own || named && r.overrides(ts, col+i, cell.Style)) && len(cells) < sheet.MaxCells {
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
