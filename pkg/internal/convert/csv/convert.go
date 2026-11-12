package csv

import (
	"context"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
	budget "github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// Convert reads the CSV file src holds and lays it out as a paged
// document, with the fonts of the index fonts. It returns ctx's error when
// ctx ends first, and the errors office.Options documents:
// typeset.ErrInvalidOptions for options out of range or a nil fonts,
// text.ErrNoFonts for an index without faces, office.ErrMalformed for a
// file that is not text, limits.ErrInputTooLarge past a size cap and
// limits.ErrTooManyPages past the page cap. A CSV file is never encrypted:
// opts.Password is ignored. The output depends on src, fonts' index and
// opts alone.
func Convert(ctx context.Context, src io.Reader, fonts *text.Fonts, opts office.Options) (*typeset.Document, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	if fonts == nil {
		return nil, fmt.Errorf("%w: no font index", typeset.ErrInvalidOptions)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if fonts.Len() == 0 {
		return nil, text.ErrNoFonts
	}
	data, err := office.ReadAll(ctx, src, opts.MaxInputBytes)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: the file is empty", office.ErrMalformed)
	}
	doc, warnings, err := Read(ctx, data, fonts, opts)
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
// and Default cell style, with the page header turned off: Calc's default
// header shows the sheet name, which a CSV takes from its file name, a
// name the converter does not know (lok's LokGeometry.xba turns it off
// alike, since there the name is the upload's file name).
//
// # Corpus evidence
//
//   - gotenberg/csv: sales.csv and sheet.csv print on one page with
//     "Page 1" centred at 60.35 points from the bottom and no header;
//     the first row's text sits 1 point right of 56.69 points (2 cm) and
//     its baseline 9.7 points below 56.69 points from the top; the rows
//     are 12.78 points apart.
const (
	// margin is Calc's 2 cm, 1,134 twips.
	margin = 1134.0 / 20
	// footerHeight is the footer's 0.75 cm with its 0.25 cm spacing, 425
	// and 142 twips.
	footerHeight  = 425.0 / 20
	footerSpacing = 142.0 / 20
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
// reads it, with the warnings for what it drops. Its fields end at any of
// opts.CSVSeparators, a comma alone without them; opts.CSVKeepText reads
// every field as text, else a field that reads as a value is one
// ([detect]). fonts measures the column widths; opts.PrintDate gives a
// date without a year its year (the zero time: [fallbackYear]), and the
// default page of opts (office.Options.DefaultPage) is the paper, A4
// without one (sheet.FallbackWidth). It returns ctx's error when ctx ends
// first.
func Read(ctx context.Context, data []byte, fonts *text.Fonts, opts office.Options) (*sheet.Document, []typeset.Warning, error) {
	printDate := opts.PrintDate
	for _, r := range opts.CSVSeparators {
		if r == quote || r == '\n' || r == '\r' || r == utf8.RuneError || !utf8.ValidRune(r) {
			return nil, nil, fmt.Errorf("%w: CSV separator %q", typeset.ErrInvalidOptions, r)
		}
	}
	recs, lim, err := parse(ctx, decode(data), newSeparators(opts.CSVSeparators))
	if err != nil {
		return nil, nil, err
	}
	var warnings []typeset.Warning
	report := func(code warn.Code, effect warn.Effect, msg string) {
		warnings = append(warnings, typeset.Warning{Code: code, Effect: effect, Message: msg})
	}
	if lim.rows {
		report(warn.LimitReached, warn.Omitted, fmt.Sprintf("rows past %d are not read", maxRows))
	}
	if lim.columns {
		report(warn.LimitReached, warn.Omitted, fmt.Sprintf("fields past %d in a row are not read", maxColumns))
	}
	if lim.bytes {
		report(warn.LimitReached, warn.Omitted, fmt.Sprintf("fields are cut at %d bytes", maxFieldBytes))
	}
	if lim.cells {
		report(warn.LimitReached, warn.Omitted, fmt.Sprintf("fields past %d are not read", maxCells))
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
	// Calc names the sheet after the file, which the converter does not
	// know: the name of a new sheet stands in, for the outline.
	sh := &sheet.Sheet{
		Name:               "Sheet1",
		DefaultColumnWidth: columnWidth,
		DefaultRowHeight:   rowHeight,
		Page:               page(opts.DefaultPage(sheet.FallbackWidth, sheet.FallbackHeight)),
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
	mem := budget.BudgetFrom(ctx)
	for ri, rec := range recs {
		if ri%ctxCheckRecords == 0 {
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
			var d detected
			if !opts.CSVKeepText {
				d = detect(f, year)
			}
			c := sheet.Cell{Column: ci, Style: styleOf(d.kind), Value: cellValue(d, f)}
			if d.kind == kDateUS && printDate.IsZero() && !hasYear(f) {
				undated = true
			}
			cols.add(ci, ri, f, d)
			row.Cells = append(row.Cells, c)
		}
		if len(row.Cells) > 0 {
			if err := mem.ChargeUnits(int64(len(row.Cells)), sheet.CellCost, "cells"); err != nil {
				return nil, nil, err
			}
			sh.Rows = append(sh.Rows, row)
		}
	}
	if undated {
		report(warn.ContentUnsupported, warn.Approximated, fmt.Sprintf("a date without a year takes %d, not the current year", fallbackYear))
	}
	sh.Columns = cols.widths(ctx, fonts)
	return &sheet.Document{Default: def, Sheets: []*sheet.Sheet{sh}}, warnings, nil
}

// cellValue returns the value of the field f detected as d.
func cellValue(d detected, f string) sheet.Value {
	switch d.kind {
	case kText:
		return sheet.Value{Kind: sheet.Text, Text: f}
	case kBool:
		return sheet.Value{Kind: sheet.Bool, Number: d.value}
	case kGeneral, kPercent, kCurrency, kScientific, kDateUS, kDateISO, kDateTimeISO, kDateTimeISOT,
		kDateTimeUS, kTime, kElapsed, kTimeFraction:
	}
	return sheet.Value{Kind: sheet.Number, Number: d.value}
}

// page returns the page setup of a CSV import on paper w by h: 2 cm
// margins, the footer's room at the bottom, scale 100, pages numbered
// from 1.
func page(w, h float64) sheet.PageSetup {
	pg := sheet.PageSetup{Width: w, Height: h, Scale: 100, HeaderTop: margin, FooterBottom: margin}
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
