package ods

import (
	"context"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/svg"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/models"
)

// Convert reads the ODS package, or the flat FODS document, src holds and
// lays it out as a paged document, with the fonts of the index fonts. It
// returns ctx's error when ctx ends first, and the errors office.Options
// documents: typeset.ErrInvalidOptions for options out of range or a nil
// fonts, text.ErrNoFonts for an index without faces, office.ErrEncrypted
// for an encrypted package opts.Password does not open, office.ErrMalformed
// for a document that is not an OpenDocument spreadsheet,
// limits.ErrInputTooLarge past a size cap and limits.ErrTooManyPages past
// the page cap. The output depends on src, fonts' index and opts alone.
func Convert(ctx context.Context, src io.Reader, fonts *text.Fonts, opts office.Options) (*typeset.Document, error) {
	// The instances of variable fonts the conversion draws belong to it.
	fonts = fonts.ForConversion()
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
	// The readers add the SVG pictures they parse to the collector; laying
	// the pages out draws them, which may add warnings the reader never saw.
	ctx, drawings := svg.WithCollector(ctx)
	data, err := office.ReadAll(ctx, src, opts.MaxInputBytes)
	if err != nil {
		return nil, err
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
	out.Warnings = append(out.Warnings, drawings.Take()...)
	return out, nil
}

// Read reads the ODS package, or the flat FODS document, data into the
// spreadsheet model, with the warnings for the constructs it does not read.
// fonts, which may be nil, parses the text of SVG pictures; opts.Password
// opens an encrypted package, and the default page of opts
// (office.Options.DefaultPage) is the paper of a page style that names none,
// A4 without one (sheet.FallbackWidth); under models.RecalculateNever no cell
// keeps its formula. It fails as [Convert] does.
func Read(ctx context.Context, data []byte, fonts *text.Fonts, opts office.Options) (*sheet.Document, []typeset.Warning, error) {
	pkg, err := odf.Open(ctx, data, odf.Options{Password: opts.Password})
	if err != nil {
		return nil, nil, fmt.Errorf("opening the package: %w", err)
	}
	switch pkg.MimeType {
	case "", odf.MimeSpreadsheet, odf.MimeSpreadsheetTemplate:
	default:
		return nil, nil, fmt.Errorf("%w: the document is %q, not a spreadsheet", office.ErrMalformed, pkg.MimeType)
	}
	body := odf.Section(pkg.Content, "body").Child(odf.NSOffice, "spreadsheet")
	if body == nil {
		return nil, nil, fmt.Errorf("%w: the document has no office:spreadsheet body", office.ErrMalformed)
	}
	r := newReader(ctx, pkg, fonts)
	r.paperW, r.paperH = opts.DefaultPage(sheet.FallbackWidth, sheet.FallbackHeight)
	r.keepFormulas = opts.Recalculation != models.RecalculateNever
	r.locale = opts.Locale
	r.data = r.data.WithLocale(opts.Locale)
	doc := r.document(body)
	if r.err != nil {
		return nil, nil, r.err
	}
	return doc, r.warnings, nil
}
