package xlsx

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
	"github.com/gotenberg/litho/pkg/models"
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
	// recalculation is the options' recalculation mode.
	recalculation models.Recalculation
	// locale is the options' locale, which the built-in formats follow.
	locale models.Locale
	// digitWidth is the widest digit of the Normal font and spaceWidth its
	// space, in points, in whole printer pixels as Excel measures them;
	// screenDigit is the digit in screen pixels ([reader.measureNormalFont]).
	digitWidth, spaceWidth, screenDigit float64
	// standardPrinted and standardScreen are the standard row height of
	// the Normal font in printer and screen pixels ([standardRow]).
	standardPrinted, standardScreen float64
	cells                           int
	// doc is the document being read, which the charts' series read
	// their values from when their part holds no cache.
	doc      *sheet.Document
	warnings []typeset.Warning
	warned   map[string]bool
	// effectPixels counts the pixels of the drawings' effects, against
	// effect.MaxPagePixels.
	effectPixels effect.Page
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
	// err is the first error that ends the conversion met where a
	// construct is otherwise left out with a warning ([reader.stop]).
	err error
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
// pageSetup, Letter without one, the paperSize SpreadsheetML defaults to
// (ECMA-376 Part 1, 18.3.1.63).
func read(ctx context.Context, pkg *ooxml.Package, fonts *text.Fonts, opts office.Options) (*sheet.Document, []typeset.Warning, error) {
	main, err := pkg.MainPart(ctx)
	if err != nil {
		return nil, nil, err
	}
	r := &reader{ctx: ctx, pkg: pkg, fonts: fonts, recalculation: opts.Recalculation, locale: opts.Locale, warned: map[string]bool{}, pictures: map[string]*layout.Picture{}, caches: map[string]*pivotCache{}}
	r.paperW, r.paperH = opts.DefaultPage(paperSizes[1].points())
	root, err := pkg.ParseXML(ctx, main, understood)
	if err != nil {
		return nil, nil, err
	}
	if !root.Is(nsMain, "workbook") {
		return nil, nil, fmt.Errorf("%w: the main part is not a SpreadsheetML workbook", office.ErrMalformed)
	}
	if name := pkg.RelatedPart(ctx, main, ooxml.RelTheme); name != "" {
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
	doc := &sheet.Document{Default: r.normal(), ExcelRules: true, StandardRowHeight: r.standardPrinted * printerPixel}
	cp, err := pkg.CoreProperties(ctx)
	if err != nil {
		return nil, nil, err
	}
	subject := cp.Subject
	if subject == "" {
		subject = cp.Description
	}
	doc.Meta = typeset.Metadata{Title: cp.Title, Author: cp.Creator, Subject: subject, Keywords: cp.Keywords, Lang: cp.Language}
	if err := r.workbook(main, root, doc, opts); err != nil {
		return nil, nil, err
	}
	r.keepCachedResults(doc)
	r.resolveOperands(doc)
	r.resolvePivots(doc)
	if r.err != nil {
		return nil, nil, r.err
	}
	return doc, r.warnings, nil
}

// application returns the application docProps/app.xml names, "" for
// none.
func (r *reader) application() string {
	name := r.pkg.RelatedPart(r.ctx, "", relExtendedProps)
	if name == "" {
		return ""
	}
	root, err := r.pkg.ParseXML(r.ctx, name, nil)
	if err != nil {
		// A part that does not parse names no application.
		r.stop(err)
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
// ([reader.recalculates]): such a cell shows its cached result. Under
// models.RecalculateAlways no formula is listed, and none cleared.
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
	r.standardPrinted, r.standardScreen = calibriStandardPrinted, calibriStandardScreen
	s := r.styles
	if len(s.fonts) == 0 {
		return
	}
	x := xf{font: 0}
	if s.normalXf < len(s.styleXfs) {
		x = s.styleXfs[s.normalXf]
	}
	// The font records hold a positive size ([reader.fontProps]).
	f := r.fontAt(max(x.font, 0))
	size := printerPixels(f.Size)
	if size <= 0 {
		return
	}
	style := text.Style{Families: append(append([]string(nil), f.Families...), substituteFamily), Size: size, Italic: f.Italic, NoKerning: true}
	if f.Bold {
		style.Weight = 700
	}
	if face := r.fonts.Match(style); face != nil {
		r.standardPrinted, r.standardScreen = standardRow(face.Metrics(1), f.Size)
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
		r.screenDigit = max(math.Floor(float64(digit/size*em)+1e-9), 1)
	}
	if sp := p.Advance(10); sp > 0 {
		r.spaceWidth = printerPixels(sp)
	}
}

// The standard row height of Calibri 11, in printer and screen pixels: what
// a workbook whose Normal font Excel cannot measure prints its rows from.
const (
	calibriStandardPrinted = 121
	calibriStandardScreen  = 20
)

// screenPixelsPerPoint is the screen Excel lays a sheet out on: 96 pixels
// an inch.
const screenPixelsPerPoint = 96.0 / 72

// standardRow returns the standard row height Excel gives a Normal font of
// size points whose face has the metrics m at 1 point, in printer pixels
// ([printerPixel]) and in screen pixels: the font's ascent and descent,
// each rounded to a pixel at the font's size in that device's pixels, and
// its line gap truncated, plus the cell's insets, 8 printer pixels and 2
// screen ones. A row of the default height prints that many printer
// pixels, and a row of h points h in screen pixels times printed over
// screen ([reader.rowHeight]). The specification gives a row's height in
// points (ECMA-376 Part 1, 18.3.1.73 ht) and the default as a value the
// application may compute (18.3.1.81 sheetFormatPr customHeight); the
// rule is Excel's, measured.
//
// # Corpus evidence
//
//   - libreoffice/xlsx, poi/xlsx: rows without a height of books whose
//     sheetFormatPr has no customHeight print, whatever defaultRowHeight
//     says, 121 printer pixels apart for Calibri 11 (sc/Book1_custom.xlsx,
//     sc/Sparklines.xlsx, sc/tdf131380.xlsx and 58896.xlsx at 14.4, 14.25
//     and 14.5 points), 103 for Arial 10 (sc/tdf137543.xlsx at 13.2,
//     sc/autofilternamedrange.xlsx at 12.5), 130 for Calibri 12
//     (sc/tdf155046.xlsx), 109 for Calibri 10 (openxmlsdk/xlsx
//     NoExtDataE8.xlsx) and 108 for Verdana 10 (DateFormatTests.xlsx), what
//     the rule gives.
//   - libreoffice/xlsx: Arial 11 and Liberation Sans 11 print 114
//     (sc/tdf145054.xlsx, poi/xlsx 49325.xlsx), Times New Roman 12 126
//     (poi/xlsx 56315.xlsx), where the rule gives 113 and 123: a face with
//     a line gap is not measured as Excel measures it.
func standardRow(m text.Metrics, size float64) (float64, float64) {
	em := math.Round(size / printerPixel)
	printed := 8 + math.Round(float64(m.Ascent*em)) + math.Round(float64(m.Descent*em)) + math.Floor(float64(m.LineGap*em)+1e-9)
	em = math.Round(float64(size * screenPixelsPerPoint))
	screen := 2 + math.Round(float64(m.Ascent*em)) + math.Round(float64(m.Descent*em)) + math.Floor(float64(m.LineGap*em)+1e-9)
	if !numeric.Positive(printed) || !numeric.Positive(screen) {
		return calibriStandardPrinted, calibriStandardScreen
	}
	return printed, screen
}

// warn records a warning, once per effect and message.
func (r *reader) warn(code warn.Code, effect warn.Effect, msg string) {
	key := code.String() + "\x00" + effect.String() + "\x00" + msg
	if r.warned[key] {
		return
	}
	r.warned[key] = true
	r.warnings = append(r.warnings, typeset.Warning{Code: code, Effect: effect, Message: msg})
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
