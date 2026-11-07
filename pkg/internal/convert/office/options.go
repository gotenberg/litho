package office

import (
	"fmt"
	"log/slog"
	"math"
)

// MaxPageSide is the longest page side in points (ISO 32000-1, Annex
// C.2).
const MaxPageSide = 14400

// Options parametrises an Office conversion. The document's own page
// setup, sections, margins, headers and footers apply; the options
// override or select among them as Gotenberg's LibreOffice route does.
type Options struct {
	// PageWidth and PageHeight, both positive, replace the page size of
	// every section with this one, in points, the way the route's paperSize
	// does; both zero, the default, keep each section's own. Each side lies
	// in (0, MaxPageSide].
	PageWidth, PageHeight float64
	// Landscape lays every section out in landscape: a portrait section's
	// width and height are swapped, a landscape one is kept.
	Landscape bool
	// Password opens an encrypted document (MS-OFFCRYPTO for OOXML; ODF
	// 1.3 Part 2, 3.4 for OpenDocument). Empty refuses an encrypted document
	// with ErrEncrypted; so does a wrong password.
	Password string
	// PageRanges selects the pages written, one-based, in document order,
	// as "1-5, 8, 11-13"; empty writes every page. A range past the last
	// page is ignored; a selection keeping no page, a start past its end or
	// a malformed specification fails with ErrInvalidOptions. Page number
	// fields count the whole document.
	PageRanges string
	// OmitOutline leaves the document outline out; otherwise the headings
	// (the outline levels of the paragraph styles) become it.
	OmitOutline bool
	// MaxInputBytes caps the source read, 0 for the converter's default.
	MaxInputBytes int64
	// MaxPages caps the pages laid out, 0 for the layout engine's default.
	MaxPages int
	// Logger receives diagnostics, nil for none.
	Logger *slog.Logger
}

// Validate reports the first option out of range, wrapped in
// [ErrInvalidOptions], nil when every option is in range. PageRanges is
// checked when the page count is known.
func (o Options) Validate() error {
	if (o.PageWidth == 0) != (o.PageHeight == 0) {
		return fmt.Errorf("%w: page width and height come together", ErrInvalidOptions)
	}
	for name, v := range map[string]float64{"width": o.PageWidth, "height": o.PageHeight} {
		if v != 0 && (math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 || v > MaxPageSide) {
			return fmt.Errorf("%w: page %s %v is not in (0, %d]", ErrInvalidOptions, name, v, MaxPageSide)
		}
	}
	if o.MaxInputBytes < 0 {
		return fmt.Errorf("%w: max input bytes %d is negative", ErrInvalidOptions, o.MaxInputBytes)
	}
	if o.MaxPages < 0 {
		return fmt.Errorf("%w: max pages %d is negative", ErrInvalidOptions, o.MaxPages)
	}
	return nil
}
