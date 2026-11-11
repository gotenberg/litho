package xlsx

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// nsMain is the SpreadsheetML namespace (ECMA-376 Part 1, 18; the Strict
// one reads as it).
const nsMain = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"

// The relationship types the reader follows (ECMA-376 Part 1, 12.3).
const (
	relWorksheet     = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet"
	relChartsheet    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/chartsheet"
	relSharedStrings = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings"
	relStyles        = ooxml.RelStyles
	relDrawing       = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/drawing"
	relChart         = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/chart"
	relExtendedProps = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties"
)

// understood lists the namespaces whose mc:Choice the reader takes
// (ECMA-376 Part 3, 10.2): SpreadsheetML, DrawingML and its spreadsheet
// drawing and picture vocabularies, the Office 2016 charts; any other
// Choice takes its Fallback.
var understood = []string{nsMain, ooxml.NSA, ooxml.NSR, nsXDR, ooxml.NSPic, nsChartEx1, nsChartEx2}

// The namespaces an Office 2016 chart's mc:Choice requires (MS-ODRAWXML,
// 2.24.3): the reader takes the Choice, to name the chart it leaves out,
// rather than the Fallback's message shape.
const (
	nsChartEx1 = "http://schemas.microsoft.com/office/drawing/2015/9/8/chartex"
	nsChartEx2 = "http://schemas.microsoft.com/office/drawing/2015/10/21/chartex"
)

// maxSheets caps the sheets of a workbook. Anti-DoS guard, not
// spec-mandated: LibreOffice Calc holds at most 10,000 sheets.
const maxSheets = 10_000

// reader is one workbook's reading state.
type reader struct {
	// paperW and paperH are the paper of a sheet without a pageSetup.
	paperW, paperH float64
	ctx            context.Context
	pkg            *ooxml.Package
	fonts          *text.Fonts
	theme          *ooxml.Theme
	// styles is the stylesheet, strings the shared string table.
	styles  *styleSheet
	strings []richText
	// date1904 marks a workbook whose dates count from 1904-01-01
	// (ECMA-376 Part 1, 18.2.28 date1904).
	date1904 bool
	// calculated marks a workbook with a formula caching a number other
	// than 0; cachedFormulas are the cells, by sheet index, row and
	// column, whose formula caches a result.
	calculated     bool
	cachedFormulas map[[3]int]bool
	// digitWidth is the widest digit of the Normal font and spaceWidth its
	// space, in points, in whole printer pixels as Excel measures them;
	// screenDigit is the digit in screen pixels ([reader.measureNormalFont]).
	digitWidth, spaceWidth, screenDigit float64
	cells                               int
	// doc is the document being read, which the charts' series read
	// their values from when their part holds no cache.
	doc      *sheet.Document
	warnings []compose.Warning
	warned   map[string]bool
	// pictures caches decoded pictures by part name.
	pictures map[string]*layout.Picture
	// systemColors are the theme's scheme colours that name a system
	// colour, by scheme name.
	systemColors map[string]css.Color
	// pending are the operands of conditional formats that refer to a
	// cell, read once every sheet is.
	pending []pendingOperand
	// pivots are the pivot tables read, placed once every sheet is;
	// pivotCaches names the cache part of each cache id, caches holds
	// the caches read by part.
	pivots      []pendingPivot
	pivotCaches map[int]string
	caches      map[string]*pivotCache
	// tableAreas are the tables of every sheet, allNames the workbook's
	// defined names, which a pivot cache's source may name.
	tableAreas []tableArea
	allNames   []definedName
}

// tableArea is a table of a sheet (ECMA-376 Part 1, 18.5.1.2): its name,
// its range and its totals rows.
type tableArea struct {
	name   string
	sh     *sheet.Sheet
	area   sheet.Range
	totals int
}

// substituteFamily is the face LibreOffice measures and draws a family
// with that no installed font and no metric alias serves, in the pinned
// image (the sheet builder draws with it too).
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/tdf165180_date1904.xlsx, whose Normal font is Aptos
//     Narrow, prints in Noto Sans with columns of its digits.
const substituteFamily = "Noto Sans"

// read reads the workbook pkg holds into the sheet model, with the
// warnings of what it left out or approximated. The default page of opts
// (office.Options.DefaultPage) is the paper of a sheet without a
// pageSetup, Letter without one.
func read(ctx context.Context, pkg *ooxml.Package, fonts *text.Fonts, opts office.Options) (*sheet.Document, []compose.Warning, error) {
	main, err := pkg.MainPart()
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", office.ErrMalformed, err)
	}
	r := &reader{ctx: ctx, pkg: pkg, fonts: fonts, warned: map[string]bool{}, pictures: map[string]*layout.Picture{}, caches: map[string]*pivotCache{}}
	r.paperW, r.paperH = opts.DefaultPage(paperSizes[1].points())
	root, err := pkg.ParseXML(ctx, main, understood)
	if err != nil {
		return nil, nil, err
	}
	if !root.Is(nsMain, "workbook") {
		return nil, nil, fmt.Errorf("%w: the main part is not a SpreadsheetML workbook", office.ErrMalformed)
	}
	if name := pkg.RelatedPart(main, ooxml.RelTheme); name != "" {
		if t, err := pkg.ParseXML(ctx, name, understood); err == nil {
			r.theme = ooxml.ParseTheme(t)
			r.systemColors = systemColors(t)
		} else if !isMalformed(err) {
			return nil, nil, err
		}
	}
	if err := r.readStyles(main); err != nil {
		return nil, nil, err
	}
	r.measureNormalFont()
	if err := r.readSharedStrings(main); err != nil {
		return nil, nil, err
	}
	doc := &sheet.Document{Default: r.normal(), ExcelRules: true}
	cp := pkg.CoreProperties(ctx)
	subject := cp.Subject
	if subject == "" {
		subject = cp.Description
	}
	doc.Meta = compose.Metadata{Title: cp.Title, Author: cp.Creator, Subject: subject, Keywords: cp.Keywords, Lang: cp.Language}
	if err := r.workbook(main, root, doc); err != nil {
		return nil, nil, err
	}
	r.keepCachedResults(doc)
	r.resolveOperands(doc)
	r.resolvePivots(doc)
	return doc, r.warnings, nil
}

// application returns the application docProps/app.xml names, "" for
// none.
func (r *reader) application() string {
	name := r.pkg.RelatedPart("", relExtendedProps)
	if name == "" {
		return ""
	}
	root, err := r.pkg.ParseXML(r.ctx, name, nil)
	if err != nil {
		return ""
	}
	for _, c := range root.Kids() {
		if c.Local == "Application" {
			return strings.TrimSpace(c.Text)
		}
	}
	return ""
}

// recalculates reports whether LibreOffice recalculates the whole
// workbook as it loads it: one an application other than Microsoft's or
// LibreOffice wrote, none of whose formulas caches a number other than 0
// (WorkbookFragment::recalcFormulaCells, isGeneratorKnownGood,
// hasCalculatedFormulaCells).
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/tdf147955.xlsx, written by Apache POI with every result
//     cached as 0, prints its totals computed.
func (r *reader) recalculates() bool {
	app := strings.ToLower(r.application())
	return !strings.HasPrefix(app, "microsoft") && !strings.HasPrefix(app, "libreoffice") && !r.calculated
}

// keepCachedResults clears the formula of every cell of doc whose
// formula caches a result, unless LibreOffice recalculates the workbook
// ([reader.recalculates]): such a cell shows its cached result.
func (r *reader) keepCachedResults(doc *sheet.Document) {
	if len(r.cachedFormulas) == 0 || r.recalculates() {
		return
	}
	for si, sh := range doc.Sheets {
		if sh == nil {
			continue
		}
		for ri := range sh.Rows {
			row := &sh.Rows[ri]
			for ci := range row.Cells {
				if r.cachedFormulas[[3]int{si, row.Index, row.Cells[ci].Column}] {
					row.Cells[ci].Formula = ""
				}
			}
		}
	}
}

// printerPixel is the pixel Excel lays a printed sheet out in, in points:
// a 600 dpi printer's. Column widths, row heights, font sizes and the cell
// insets of a printed sheet are whole numbers of them.
//
// # Corpus evidence
//
//   - litho/xlsx: every page Office for the web prints draws its
//     rectangles at multiples of 0.12 points from the margins, and
//     Calibri 11 text at 11.04 points, Arial 10 at 9.96: 92 and 83
//     pixels.
const printerPixel = 72.0 / 600

// printerPixels rounds a length in points to whole printer pixels.
func printerPixels(v float64) float64 {
	return math.Round(v/printerPixel) * printerPixel
}

// measureNormalFont measures the widest digit and the space of the Normal
// style's font, which the column widths and the indents count in
// (ECMA-376 Part 1, 18.3.1.13 width), as Excel measures them on the
// printer it lays a printed sheet out for: the font size rounded to whole
// printer pixels, the digit's advance at that size rounded to a pixel.
// screenDigit is the digit's width on Excel's 96 dpi screen, in pixels,
// which the default column width counts in. A stylesheet without fonts
// gives Excel no font to measure: the digits are then Calibri 11's. A
// family no font serves is measured in LibreOffice's substitute.
//
// # Corpus evidence
//
//   - litho/xlsx: multipage_grid prints Calibri 11 columns 11 characters
//     wide 62.06 points apart (517 pixels, 11 digits of 47); minimal prints
//     its default columns 51.6 points wide (430 pixels, 9.14 digits).
//   - libreoffice/xlsx: sc/tdf139934.xlsx prints Arial 10 columns of
//     23.4375 characters 129.38 points wide (1078 pixels, digits of 46).
func (r *reader) measureNormalFont() {
	r.digitWidth, r.spaceWidth, r.screenDigit = 47*printerPixel, 24*printerPixel, 7
	s := r.styles
	if len(s.fonts) == 0 {
		return
	}
	x := xf{font: 0}
	if s.normalXf < len(s.styleXfs) {
		x = s.styleXfs[s.normalXf]
	}
	f := r.fontAt(max(x.font, 0))
	if !(f.Size > 0) {
		return
	}
	size := printerPixels(f.Size)
	if size <= 0 {
		return
	}
	style := text.Style{Families: append(append([]string(nil), f.Families...), substituteFamily), Size: size, Italic: f.Italic, NoKerning: true}
	if f.Bold {
		style.Weight = 700
	}
	p, _, err := r.fonts.Shape(r.ctx, []text.ParagraphRun{{Text: "0123456789 ", Style: style}}, text.ParagraphOptions{})
	if err != nil || p.Len() != 11 {
		return
	}
	digit := 0.0
	for i := range 10 {
		digit = max(digit, p.Advance(i))
	}
	if digit > 0 {
		r.digitWidth = max(printerPixels(digit), printerPixel)
		// The screen draws the size at whole pixels of 0.75 points and
		// the digit truncated to a pixel.
		em := math.Round(f.Size / 0.75)
		r.screenDigit = max(math.Floor(digit/size*em+1e-9), 1)
	}
	if sp := p.Advance(10); sp > 0 {
		r.spaceWidth = printerPixels(sp)
	}
}

// warn records a warning, once per message.
func (r *reader) warn(code, msg string) {
	key := code + "\x00" + msg
	if r.warned[key] {
		return
	}
	r.warned[key] = true
	r.warnings = append(r.warnings, compose.Warning{Code: code, Message: msg})
}

// isMalformed reports an error of a part that does not parse.
func isMalformed(err error) bool { return errors.Is(err, office.ErrMalformed) }

// attr returns e's attribute local, "" when it is absent.
func attr(e *ooxml.Element, local string) string {
	v, _ := e.Attr(local)
	return v
}

// textOf returns e's text, "" for a nil e.
func textOf(e *ooxml.Element) string {
	if e == nil {
		return ""
	}
	return e.Text
}

// boolAttr returns e's ST_Boolean attribute local, false when absent.
func boolAttr(e *ooxml.Element, local string) bool {
	v, ok := e.Attr(local)
	if !ok {
		return false
	}
	return ooxml.OnOff(v, true)
}
