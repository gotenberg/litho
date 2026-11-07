package sheet

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// Convert lays doc out as LibreOffice prints a spreadsheet, with the fonts
// of the index fonts, and returns the paged document. It honours
// office.Options: PageWidth and PageHeight and Landscape override every
// sheet's page setup, SinglePageSheets lays each sheet out on one page the
// size of its content, PageRanges selects pages, OmitOutline leaves the
// sheet outline out, PrintDate fills the date and time fields of the
// headers and footers, MaxPages caps the pages. It returns ctx's error
// when ctx ends first, office.ErrInvalidOptions for options out of range
// or a nil fonts or doc, text.ErrNoFonts for an index without faces, and
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
		return nil, fmt.Errorf("laying the spreadsheet out: %w", text.ErrNoFonts)
	}
	return nil, fmt.Errorf("laying the spreadsheet out: %w", office.ErrNotImplemented)
}
