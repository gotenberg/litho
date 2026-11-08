package office

import (
	"fmt"
	"log/slog"
	"math"
	"time"
)

// MaxPageSide is the longest page side in points (ISO 32000-1, Annex
// C.2).
const MaxPageSide = 14400

// Options parametrises an Office conversion. The document's own page
// setup, sections or sheets, margins, headers and footers apply; the
// options override or select among them.
type Options struct {
	// PageWidth and PageHeight, both positive, replace the page size of
	// every section with this one, in points; both zero, the default, keep
	// each section's own. Each side lies in (0, MaxPageSide].
	PageWidth, PageHeight float64
	// Landscape lays every section out in landscape: a portrait section's
	// width and height are swapped, a landscape one is kept.
	Landscape bool
	// Password opens an encrypted document (MS-OFFCRYPTO for OOXML; ODF
	// 1.3 Part 2, 3.4 for OpenDocument). Empty refuses an encrypted document
	// with ErrEncrypted; so does a wrong password.
	Password string
	// OmitOutline leaves the document outline out; otherwise the headings
	// (the outline levels of the paragraph styles) become it, or the sheets
	// of a spreadsheet.
	OmitOutline bool
	// SinglePageSheets lays each sheet of a spreadsheet out on one page the
	// size of its content, as LibreOffice's SinglePageSheets export does;
	// the sheet's paper, scale and page breaks are then ignored, and the
	// page size options above too. A text document ignores it.
	SinglePageSheets bool
	// PrintDate is what the date and time fields of a spreadsheet's headers
	// and footers, and of a presentation's slides, show; the zero time
	// shows nothing for a spreadsheet and the text the file saved for a
	// presentation. It is the only date of a conversion: the output never
	// depends on the clock. A text document ignores it: its date fields
	// show the results its file stores.
	PrintDate time.Time
	// HiddenSlides prints the slides a presentation hides from its show
	// too; off, the default, they are left out, as Impress prints a show.
	// Other documents ignore it.
	HiddenSlides bool
	// NotesPages prints, after the slides of a presentation, one notes
	// page per printed slide: the slide at the top and its notes below, on
	// the notes page size. Other documents ignore it.
	NotesPages bool
	// MaxInputBytes caps the source read, 0 for the converter's default.
	MaxInputBytes int64
	// MaxPages caps the pages laid out, 0 for the layout engine's default.
	MaxPages int
	// Logger receives diagnostics, nil for none.
	Logger *slog.Logger
}

// Validate reports the first option out of range, wrapped in
// [ErrInvalidOptions], nil when every option is in range.
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
