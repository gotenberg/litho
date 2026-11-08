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
// drawing and picture vocabularies; any other Choice takes its Fallback.
var understood = []string{nsMain, ooxml.NSA, ooxml.NSR, nsXDR, ooxml.NSPic}

// maxSheets caps the sheets of a workbook. Anti-DoS guard, not
// spec-mandated: LibreOffice Calc holds at most 10,000 sheets.
const maxSheets = 10_000

// maxCells caps the cells read from a workbook, all sheets together.
// Anti-DoS guard, not spec-mandated: a million cells print on some twenty
// thousand pages, past any page cap a conversion runs with.
const maxCells = 1_000_000

// reader is one workbook's reading state.
type reader struct {
	ctx   context.Context
	pkg   *ooxml.Package
	fonts *text.Fonts
	theme *ooxml.Theme
	// styles is the stylesheet, strings the shared string table.
	styles  *styleSheet
	strings []richText
	// date1904 marks a workbook whose dates count from 1904-01-01
	// (ECMA-376 Part 1, 18.2.28 date1904).
	date1904 bool
	// mso marks a workbook an Office application wrote, whose row heights
	// LibreOffice rounds down to three quarters of a point.
	mso bool
	// digitWidth is the widest digit of the Normal font and spaceWidth its
	// space, in points, rounded to twips as LibreOffice measures them.
	digitWidth, spaceWidth float64
	cells                  int
	warnings               []compose.Warning
	warned                 map[string]bool
	// pictures caches decoded pictures by part name; pixels counts the
	// pixels decoded against [maxTotalPixels].
	pictures map[string]*layout.Picture
	pixels   int64
}

// read reads the workbook pkg holds into the sheet model, with the
// warnings of what it left out or approximated.
func read(ctx context.Context, pkg *ooxml.Package, fonts *text.Fonts) (*sheet.Document, []compose.Warning, error) {
	main, err := pkg.MainPart()
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", office.ErrMalformed, err)
	}
	r := &reader{ctx: ctx, pkg: pkg, fonts: fonts, warned: map[string]bool{}, pictures: map[string]*layout.Picture{}}
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
		} else if !isMalformed(err) {
			return nil, nil, err
		}
	}
	r.mso = r.officeWritten()
	if err := r.readStyles(main); err != nil {
		return nil, nil, err
	}
	r.measureNormalFont()
	if err := r.readSharedStrings(main); err != nil {
		return nil, nil, err
	}
	doc := &sheet.Document{Default: r.normal()}
	cp := pkg.CoreProperties(ctx)
	subject := cp.Subject
	if subject == "" {
		subject = cp.Description
	}
	doc.Meta = compose.Metadata{Title: cp.Title, Author: cp.Creator, Subject: subject, Keywords: cp.Keywords, Lang: cp.Language}
	if err := r.workbook(main, root, doc); err != nil {
		return nil, nil, err
	}
	return doc, r.warnings, nil
}

// officeWritten reports whether docProps/app.xml names a Microsoft
// application, the test LibreOffice's import makes before it rounds row
// heights down to three quarters of a point.
func (r *reader) officeWritten() bool {
	name := r.pkg.RelatedPart("", relExtendedProps)
	if name == "" {
		return false
	}
	root, err := r.pkg.ParseXML(r.ctx, name, nil)
	if err != nil {
		return false
	}
	for _, c := range root.Kids() {
		if c.Local == "Application" {
			return strings.HasPrefix(strings.ToLower(strings.TrimSpace(c.Text)), "microsoft")
		}
	}
	return false
}

// measureNormalFont measures the widest digit and the space of the Normal
// style's font, which the column widths and the indents count in
// (ECMA-376 Part 1, 18.3.1.13 width), as LibreOffice measures them on its
// reference device: the font size rounded to a hundredth of a
// millimetre, the advance to a twip. A stylesheet without fonts gives
// LibreOffice no font to measure: a digit is then 2 millimetres and a
// space 1, its defaults. Without a face they take the widths of Calibri
// at 11 points.
//
// # Corpus evidence
//
//   - LibreOffice 26.8.2.2: columns 20 characters wide print 110.97
//     points wide for Calibri 11, 121.97 for Calibri 12, 100.99 for
//     Calibri 10, 110.98 for Arial 10, 121.97 for Arial 11 and 119.99 for
//     Times New Roman 12: digits of 111, 122, 101, 111, 122 and 120 twips,
//     the advances of Carlito, Liberation Sans and Liberation Serif at
//     388, 423 and 353 hundredths of a millimetre (Carlito's 111.50 twips
//     at 11 points read as 111). The PDF draws 11-point text at 10.998.
func (r *reader) measureNormalFont() {
	r.digitWidth, r.spaceWidth = 111.0/20, 50.0/20
	s := r.styles
	if len(s.fonts) == 0 {
		r.digitWidth, r.spaceWidth = 2*72/25.4, 72/25.4
		return
	}
	x := xf{font: 0}
	if s.normalXf < len(s.styleXfs) {
		x = s.styleXfs[s.normalXf]
	}
	f := r.fontAt(max(x.font, 0))
	size := hmmPt(math.Round(f.Size * 2540 / 72))
	if size <= 0 {
		return
	}
	style := text.Style{Families: f.Families, Size: size, Italic: f.Italic, NoKerning: true}
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
		r.digitWidth = math.Round(digit*20) / 20
	}
	if sp := p.Advance(10); sp > 0 {
		r.spaceWidth = math.Round(sp*20) / 20
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
