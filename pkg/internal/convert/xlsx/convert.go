package xlsx

import (
	"context"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/offcrypto"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// Convert reads the XLSX package src holds and lays it out as a paged
// document, with the fonts of the index fonts. It returns ctx's error when
// ctx ends first, and the errors office.Options documents:
// office.ErrInvalidOptions for options out of range or a nil fonts,
// text.ErrNoFonts for an index without faces, office.ErrEncrypted for an
// encrypted package opts.Password does not open, office.ErrMalformed for a
// package that is not a SpreadsheetML workbook, office.ErrInputTooLarge
// past a size cap and office.ErrTooManyPages past the page cap. The output
// depends on src, fonts' index and opts alone.
func Convert(ctx context.Context, src io.Reader, fonts *text.Fonts, opts office.Options) (*compose.Document, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	if opts.Sheets != nil || opts.SheetNumbers != nil || opts.HiddenSheets || opts.Recalculation != office.RecalculateAsNeeded {
		// Wave 11, cell sheets: remove with the feature.
		return nil, fmt.Errorf("%w: sheet selection and recalculation modes", office.ErrNotImplemented)
	}
	if fonts == nil {
		return nil, fmt.Errorf("%w: no font index", office.ErrInvalidOptions)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if fonts.Len() == 0 {
		return nil, fmt.Errorf("converting XLSX: %w", text.ErrNoFonts)
	}
	doc, warnings, err := load(ctx, src, fonts, opts)
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

// load reads src into the sheet model with the reader's warnings.
func load(ctx context.Context, src io.Reader, fonts *text.Fonts, opts office.Options) (*sheet.Document, []compose.Warning, error) {
	data, err := office.ReadAll(ctx, src, opts.MaxInputBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("reading XLSX: %w", err)
	}
	// A zip package comes back unchanged; a compound file is an encrypted
	// package (MS-OFFCRYPTO, 2.3.4.4) opened with the password.
	if data, err = offcrypto.OpenOOXML(ctx, data, opts.Password); err != nil {
		return nil, nil, fmt.Errorf("opening XLSX: %w", err)
	}
	pkg, err := ooxml.Open(data)
	if err != nil {
		return nil, nil, fmt.Errorf("opening XLSX: %w", err)
	}
	doc, warnings, err := read(ctx, pkg, fonts, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("reading XLSX: %w", err)
	}
	return doc, warnings, nil
}
