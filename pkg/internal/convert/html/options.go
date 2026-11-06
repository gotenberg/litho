package html

import (
	"fmt"
	"io/fs"
	"log/slog"
	"math"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// Page setup defaults: A4, the ISO 216 size, 210 by 297 mm.
const (
	// DefaultPageWidth is the width of an A4 page in points.
	DefaultPageWidth = 595.276
	// DefaultPageHeight is the height of an A4 page in points.
	DefaultPageHeight = 841.890
)

// The media types [Options.Media] accepts (Media Queries 4, 2.3).
const (
	// MediaPrint evaluates the stylesheets for paged output, the default.
	MediaPrint = "print"
	// MediaScreen evaluates them as a browser window shows them.
	MediaScreen = "screen"
)

// DefaultMaxInputBytes is the source cap when [Options.MaxInputBytes] is
// zero, the default of the conversion input budget (see
// [limits.DefaultMaxConvertInputBytes], which says why it is safe).
const DefaultMaxInputBytes = limits.DefaultMaxConvertInputBytes

// DefaultMaxPages is the page cap when [Options.MaxPages] is zero, the
// default of the conversion page budget (see
// [limits.DefaultMaxConvertPages], which says why it is safe).
const DefaultMaxPages = limits.DefaultMaxConvertPages

// maxPageSide is the largest page side in points: ISO 32000-1, Annex C.2,
// Table C.1 bounds a page at 14,400 units in either dimension. It is the
// implementation limit, not a litho guard.
const maxPageSide = 14400

// Options is the setup of an HTML conversion. The zero value asks for an A4
// page without margins, for print, with no resources.
type Options struct {
	// PageWidth and PageHeight are the paper size in points, A4 (595.276 by
	// 841.890) when both are zero. Each side lies in (0, 14400]. An @page
	// size descriptor overrides them (CSS Page 3, 5.1).
	PageWidth, PageHeight float64
	// MarginTop, MarginRight, MarginBottom and MarginLeft are the page
	// margins in points, zero for none. Each is zero or more, and they leave
	// room on the page. An @page margin overrides its side.
	MarginTop, MarginRight, MarginBottom, MarginLeft float64
	// Media is the media type the stylesheets are evaluated for:
	// [MediaPrint] when empty, or [MediaScreen].
	Media string
	// Resources holds the files the document's relative URLs name:
	// stylesheets and images, read with the URL path, relative to the
	// root, as the file path. nil leaves every reference out with a
	// warning.
	Resources fs.FS
	// MaxInputBytes caps the source, [DefaultMaxInputBytes] when zero, at
	// most [limits.MaxConvertInputBytesCeiling].
	MaxInputBytes int64
	// MaxPages caps the output pages, [DefaultMaxPages] when zero, at most
	// [limits.MaxConvertPagesCeiling].
	MaxPages int
	// Logger receives diagnostics, nil for none.
	Logger *slog.Logger
}

// withDefaults returns o with every zero field set to its default, or an
// error wrapping [ErrInvalidOptions] for a field out of range.
func (o Options) withDefaults() (Options, error) {
	if o.PageWidth == 0 && o.PageHeight == 0 {
		o.PageWidth, o.PageHeight = DefaultPageWidth, DefaultPageHeight
	}
	if o.Media == "" {
		o.Media = MediaPrint
	}
	if o.MaxInputBytes == 0 {
		o.MaxInputBytes = DefaultMaxInputBytes
	}
	if o.MaxPages == 0 {
		o.MaxPages = DefaultMaxPages
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	for _, v := range []struct {
		name  string
		value float64
	}{{"page width", o.PageWidth}, {"page height", o.PageHeight}} {
		// The negated range also refuses NaN.
		if !(v.value > 0 && v.value <= maxPageSide) {
			return o, fmt.Errorf("%w: %s %v is not in (0, %v]", ErrInvalidOptions, v.name, v.value, maxPageSide)
		}
	}
	for _, v := range []struct {
		name  string
		value float64
	}{
		{"top margin", o.MarginTop},
		{"right margin", o.MarginRight},
		{"bottom margin", o.MarginBottom},
		{"left margin", o.MarginLeft},
	} {
		if !(v.value >= 0 && v.value < math.Inf(1)) {
			return o, fmt.Errorf("%w: %s %v is not a finite number of points, zero or more", ErrInvalidOptions, v.name, v.value)
		}
	}
	if o.MarginLeft+o.MarginRight >= o.PageWidth {
		return o, fmt.Errorf("%w: left and right margins %v and %v leave no room on a page %v points wide", ErrInvalidOptions, o.MarginLeft, o.MarginRight, o.PageWidth)
	}
	if o.MarginTop+o.MarginBottom >= o.PageHeight {
		return o, fmt.Errorf("%w: top and bottom margins %v and %v leave no room on a page %v points high", ErrInvalidOptions, o.MarginTop, o.MarginBottom, o.PageHeight)
	}
	if o.Media != MediaPrint && o.Media != MediaScreen {
		return o, fmt.Errorf("%w: media type %q is neither %q nor %q", ErrInvalidOptions, o.Media, MediaPrint, MediaScreen)
	}
	if o.MaxInputBytes < 0 || o.MaxInputBytes > limits.MaxConvertInputBytesCeiling {
		return o, fmt.Errorf("%w: input cap %d is not in [1, %d]", ErrInvalidOptions, o.MaxInputBytes, int64(limits.MaxConvertInputBytesCeiling))
	}
	if o.MaxPages < 0 || o.MaxPages > limits.MaxConvertPagesCeiling {
		return o, fmt.Errorf("%w: page cap %d is not in [1, %d]", ErrInvalidOptions, o.MaxPages, limits.MaxConvertPagesCeiling)
	}
	return o, nil
}
