package odp

import (
	"context"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// Convert reads the ODP package, or the flat FODP document, src holds and
// lays it out as a paged document, with the fonts of the index fonts. It
// returns ctx's error when ctx ends first, and the errors office.Options
// documents: office.ErrInvalidOptions for options out of range or a nil
// fonts, text.ErrNoFonts for an index without faces, office.ErrEncrypted
// for an encrypted package opts.Password does not open, office.ErrMalformed
// for a document that is not an OpenDocument presentation,
// office.ErrInputTooLarge past a size cap and office.ErrTooManyPages past
// the page cap. The output depends on src, fonts' index and opts alone.
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
		return nil, fmt.Errorf("converting ODP: %w", text.ErrNoFonts)
	}
	data, err := office.ReadAll(ctx, src, opts.MaxInputBytes)
	if err != nil {
		return nil, fmt.Errorf("reading ODP: %w", err)
	}
	doc, warnings, err := Read(ctx, data, fonts, opts.Password)
	if err != nil {
		return nil, err
	}
	out, err := slides.Convert(ctx, doc, fonts, opts)
	if err != nil {
		return nil, err
	}
	out.Warnings = append(warnings, out.Warnings...)
	return out, nil
}

// Read reads the ODP package, or the flat FODP document, data into the
// presentation model, with the warnings for the constructs it does not
// read: the master pages, their placeholders and presentation styles
// resolved into every shape (ODF 1.3, Part 3, 10.2 and 16.9). fonts,
// which may be nil, parses the text of SVG pictures; password opens an
// encrypted package. It fails as [Convert] does.
func Read(ctx context.Context, data []byte, fonts *text.Fonts, password string) (*slides.Document, []compose.Warning, error) {
	pkg, err := odf.Open(ctx, data, odf.Options{Password: password})
	if err != nil {
		return nil, nil, fmt.Errorf("reading ODP: %w", err)
	}
	switch pkg.MimeType {
	case "", odf.MimePresentation, odf.MimePresentationTemplate:
	default:
		return nil, nil, fmt.Errorf("reading ODP: %w: the document is %q, not a presentation", office.ErrMalformed, pkg.MimeType)
	}
	body := odf.Section(pkg.Content, "body").Child(odf.NSOffice, "presentation")
	if body == nil {
		return nil, nil, fmt.Errorf("reading ODP: %w: the document has no office:presentation body", office.ErrMalformed)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	r := newReader(ctx, pkg, fonts)
	doc := r.document(body)
	if r.err != nil {
		return nil, nil, fmt.Errorf("reading ODP: %w", r.err)
	}
	return doc, r.warnings, nil
}
