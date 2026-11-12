package pptx

import (
	"context"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/offcrypto"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/svg"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// Convert reads the PPTX package src holds and lays it out as a paged
// document, with the fonts of the index fonts. It returns ctx's error when
// ctx ends first, and the errors office.Options documents:
// typeset.ErrInvalidOptions for options out of range or a nil fonts,
// text.ErrNoFonts for an index without faces, office.ErrEncrypted for an
// encrypted package opts.Password does not open, office.ErrMalformed for a
// package that is not a PresentationML presentation,
// limits.ErrInputTooLarge past a size cap and limits.ErrTooManyPages past
// the page cap. The output depends on src, fonts' index and opts alone.
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
	// The readers add the SVG pictures they parse to the collector; laying
	// the pages out draws them, which may add warnings the reader never saw.
	ctx, drawings := svg.WithCollector(ctx)
	doc, fonts, warnings, err := load(ctx, src, fonts, opts)
	if err != nil {
		return nil, err
	}
	out, err := slides.Convert(ctx, doc, fonts, opts)
	if err != nil {
		return nil, err
	}
	out.Warnings = append(warnings, out.Warnings...)
	out.Warnings = append(out.Warnings, drawings.Take()...)
	return out, nil
}

// load reads src into the slide model with the fonts to lay it out with
// and the reader's warnings.
func load(ctx context.Context, src io.Reader, fonts *text.Fonts, opts office.Options) (*slides.Document, *text.Fonts, []typeset.Warning, error) {
	data, err := office.ReadAll(ctx, src, opts.MaxInputBytes)
	if err != nil {
		return nil, nil, nil, err
	}
	// A zip package comes back unchanged; a compound file is an encrypted
	// package (MS-OFFCRYPTO, 2.3.4.4) opened with the password.
	if data, err = offcrypto.OpenOOXML(ctx, data, opts.Password); err != nil {
		return nil, nil, nil, fmt.Errorf("opening the package: %w", err)
	}
	pkg, err := ooxml.Open(ctx, data)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("opening the package: %w", err)
	}
	return read(ctx, pkg, fonts, opts.Handouts > 0)
}
