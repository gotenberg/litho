package csv

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// Convert reads the CSV file src holds and lays it out as a paged
// document, with the fonts of the index fonts. It returns ctx's error when
// ctx ends first, and the errors office.Options documents:
// office.ErrInvalidOptions for options out of range or a nil fonts,
// text.ErrNoFonts for an index without faces, office.ErrMalformed for a
// file that is not text, office.ErrInputTooLarge past a size cap and
// office.ErrTooManyPages past the page cap. A CSV file is never encrypted:
// opts.Password is ignored. The output depends on src, fonts' index and
// opts alone.
func Convert(ctx context.Context, src io.Reader, fonts *text.Fonts, opts office.Options) (*compose.Document, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	if fonts == nil {
		return nil, fmt.Errorf("%w: no font index", office.ErrInvalidOptions)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if fonts.Len() == 0 {
		return nil, fmt.Errorf("converting CSV: %w", text.ErrNoFonts)
	}
	data, err := office.ReadAll(ctx, src, opts.MaxInputBytes)
	if err != nil {
		return nil, fmt.Errorf("reading CSV: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("reading CSV: %w: the file is empty", office.ErrMalformed)
	}
	doc, warnings, err := Read(ctx, data, fonts, opts.PrintDate)
	if err != nil {
		return nil, err
	}
	out, err := sheet.Convert(ctx, doc, fonts, opts)
	if err != nil {
		return nil, err
	}
	out.Warnings = append(warnings, out.Warnings...)
	return out, nil
}

// The page and cell defaults of a CSV import, Calc's Default page style
// and Default cell style in the en-US image, with the route's header
// turned off (lok's LokGeometry.xba sets HeaderIsOn false on every page
// style of a CSV, since the header shows the sheet name, the upload's
// file name).
//
// # Corpus evidence
//
//   - litho: sales.csv and sheet.csv print on one 612 by 792 point page
//     with "Page 1" centred at 60.35 points from the bottom and no header;
//     the first row's text sits 1 point right of 56.69 points (2 cm) and
//     its baseline 9.7 points below 56.69 points from the top; the rows
//     are 12.78 points apart.
const (
	pageWidth, pageHeight = 612, 792
	margin                = 2 * 72 / 2.54
	// footerHeight is the footer's 0.75 cm with its 0.25 cm spacing.
	footerHeight  = 0.75 * 72 / 2.54
	footerSpacing = 0.25 * 72 / 2.54
	// rowHeight is Calc's standard row height for Liberation Sans 10, 256
	// twips.
	rowHeight = 12.8
	// columnWidth is Calc's standard column width, 1,280 twips, which a
	// column without content keeps.
	columnWidth = 64
	// cellPadding is Calc's default cell margin, 20 twips on every side
	// (SvxMarginItem's default, the Default cell style's ATTR_MARGIN).
	cellPadding = 1
	fontFamily  = "Liberation Sans"
	fontSize    = 10
)

// fallbackYear is the year a date without one ("1/10") takes when the
// options carry no print date: Calc takes the current year, which a
// conversion that never reads the clock does not know; the reader takes
// the year its behaviour was measured in.
const fallbackYear = 2026

// Read reads the CSV data into the spreadsheet model as Calc's import
// through the route reads it, with the warnings for what it drops. fonts
// measures the column widths; printDate gives a date without a year its
// year (the zero time: [fallbackYear]). It returns ctx's error when ctx
// ends first.
func Read(ctx context.Context, data []byte, fonts *text.Fonts, printDate time.Time) (*sheet.Document, []compose.Warning, error) {
	recs, lim, err := parse(ctx, decode(data))
	if err != nil {
		return nil, nil, err
	}
	var warnings []compose.Warning
	warn := func(code, msg string) { warnings = append(warnings, compose.Warning{Code: code, Message: msg}) }
	if lim.rows {
		warn("too-many-rows", fmt.Sprintf("rows past %d are not read", maxRows))
	}
	if lim.columns {
		warn("too-many-columns", fmt.Sprintf("fields past %d in a row are not read", maxColumns))
	}
	if lim.bytes {
		warn("field-too-long", fmt.Sprintf("fields are cut at %d bytes", maxFieldBytes))
	}
	if lim.cells {
		warn("too-many-cells", fmt.Sprintf("fields past %d are not read", maxCells))
	}
	year := fallbackYear
	if !printDate.IsZero() {
		year = printDate.Year()
	}
	def := sheet.Style{
		Font:    sheet.Font{Families: []string{fontFamily}, Size: fontSize},
		Padding: [4]float64{cellPadding, cellPadding, cellPadding, cellPadding},
		Format:  "General",
	}
	sh := &sheet.Sheet{
		DefaultColumnWidth: columnWidth,
		DefaultRowHeight:   rowHeight,
		Page:               page(),
	}
	sh.HeaderFooter.Odd.Footer[1] = []sheet.Span{{Text: "Page "}, {Field: sheet.FieldPage}}
	sh.HeaderFooter.Even, sh.HeaderFooter.First = sh.HeaderFooter.Odd, sh.HeaderFooter.Odd
	sh.HeaderFooter.FooterMinHeight = footerHeight - footerSpacing
	styles := map[kind]*sheet.Style{}
	styleOf := func(k kind) *sheet.Style {
		if k == kText || k == kGeneral || k == kBool {
			return nil
		}
		if st := styles[k]; st != nil {
			return st
		}
		st := def
		st.Format = formatCodes[k]
		styles[k] = &st
		return &st
	}
	cols := newColumns()
	undated := false
	for ri, rec := range recs {
		if ri%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
		}
		var row sheet.Row
		row.Index = ri
		for ci, f := range rec {
			if f == "" {
				continue
			}
			d := detect(f, year)
			c := sheet.Cell{Column: ci, Style: styleOf(d.kind)}
			switch d.kind {
			case kText:
				c.Value = sheet.Value{Kind: sheet.Text, Text: f}
			case kBool:
				c.Value = sheet.Value{Kind: sheet.Bool, Number: d.value}
			default:
				c.Value = sheet.Value{Kind: sheet.Number, Number: d.value}
				if d.kind == kDateUS && printDate.IsZero() && !hasYear(f) {
					undated = true
				}
			}
			cols.add(ci, ri, f, d)
			row.Cells = append(row.Cells, c)
		}
		if len(row.Cells) > 0 {
			sh.Rows = append(sh.Rows, row)
		}
	}
	if undated {
		warn("date-without-year", fmt.Sprintf("a date without a year takes %d, not the current year", fallbackYear))
	}
	sh.Columns = cols.widths(ctx, fonts)
	return &sheet.Document{Default: def, Sheets: []*sheet.Sheet{sh}}, warnings, nil
}

// page returns the page setup of a CSV import: Letter, 2 cm margins, the
// footer's room at the bottom, scale 100, pages numbered from 1.
func page() sheet.PageSetup {
	pg := sheet.PageSetup{Width: pageWidth, Height: pageHeight, Scale: 100, HeaderTop: margin, FooterBottom: margin}
	for i := range pg.Margin {
		pg.Margin[i] = margin
	}
	pg.Margin[css.Bottom] += footerHeight
	return pg
}

// hasYear reports a date field that names its year: two slashes, or a
// month name and a year.
func hasYear(f string) bool {
	n := 0
	for i := 0; i < len(f); i++ {
		if f[i] == '/' {
			n++
		}
	}
	return n != 1
}
