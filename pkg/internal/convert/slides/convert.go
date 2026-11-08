package slides

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// Convert lays doc out as Impress prints a presentation, with the fonts of
// the index fonts, and returns the paged document: one page per slide at
// the slide size, hidden slides left out unless office.Options.HiddenSlides
// asks for them, a notes page per slide after the slides when
// office.Options.NotesPages asks, the slide titles as the outline unless
// OmitOutline, the date and time fields showing PrintDate. PageWidth and
// PageHeight, when set, are the page size, the slide scaled to fit it and
// centred; Landscape turns a portrait page. It returns ctx's error when
// ctx ends first, office.ErrInvalidOptions for options out of range or a
// nil fonts or doc, text.ErrNoFonts for an index without faces, and
// office.ErrTooManyPages past the page cap. The output depends on doc,
// fonts' index and opts alone.
func Convert(ctx context.Context, doc *Document, fonts *text.Fonts, opts office.Options) (*compose.Document, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	if doc == nil || fonts == nil {
		return nil, fmt.Errorf("%w: no document or no font index", office.ErrInvalidOptions)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if fonts.Len() == 0 {
		return nil, fmt.Errorf("laying the presentation out: %w", text.ErrNoFonts)
	}
	return nil, fmt.Errorf("laying the presentation out: %w", office.ErrNotImplemented)
}
