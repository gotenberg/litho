package plaintext

import (
	"fmt"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// Page setup defaults. A4 is the ISO 216 size, 210 by 297 mm, and the
// margins are 2 cm, the page LibreOffice Writer gives a plain text file it
// opens outside North America.
const (
	// DefaultPageWidth is the width of an A4 page in points.
	DefaultPageWidth = 595.276
	// DefaultPageHeight is the height of an A4 page in points.
	DefaultPageHeight = 841.890
	// DefaultMargin is 2 cm in points, each margin's default.
	DefaultMargin = 56.693
	// DefaultFontSize is the font size in points.
	DefaultFontSize = 12
	// DefaultLineHeight is the line height as a multiple of the font size,
	// the single spacing of a word processor.
	DefaultLineHeight = 1.15
	// DefaultTabWidth is the distance between tab stops, in characters.
	DefaultTabWidth = 8
)

// DefaultFontFamilies are the families asked for, in order, when
// [Options.FontFamilies] is empty: Liberation Serif, the face LibreOffice
// opens a .txt file in, then the generic serif family for a system without
// it. The slice is never mutated.
var DefaultFontFamilies = []string{"Liberation Serif", "serif"}

// DefaultMaxInputBytes is the source cap when [Options.MaxInputBytes] is
// zero. It is the default of the conversion input budget (see
// [limits.DefaultMaxConvertInputBytes], which says why it is safe).
const DefaultMaxInputBytes = limits.DefaultMaxConvertInputBytes

// DefaultMaxPages is the page cap when [Options.MaxPages] is zero. It is the
// default of the conversion page budget (see [limits.DefaultMaxConvertPages],
// which says why it is safe).
const DefaultMaxPages = limits.DefaultMaxConvertPages

// maxPageSide is the largest page side in points: ISO 32000-1, Annex C.2,
// Table C.1 bounds a page at 14,400 units in either dimension. It is the
// implementation limit, not a litho guard.
const maxPageSide = 14400

// maxFontSize is the largest font size [text.Style] accepts, in points.
const maxFontSize = 1000

// maxLineHeight bounds [Options.LineHeight]. Not spec-mandated: it keeps the
// line advance finite, and a multiple past it puts one line on a page at any
// size typesetting uses.
const maxLineHeight = 100

// maxTabWidth bounds [Options.TabWidth]. Anti-DoS guard, not spec-mandated:
// a tab expands to up to TabWidth spaces, so the width bounds how much a
// source of tabs grows before layout. Tab stops sit 2 to 8 characters apart
// in every editor.
const maxTabWidth = 64

// Options is the page setup of a plain text conversion. The zero value asks
// for every default but the margins, which are then none.
type Options struct {
	// PageWidth and PageHeight are the page size in points, A4 (595.276 by
	// 841.890) when both are zero. Each side lies in (0, 14400].
	PageWidth, PageHeight float64
	// MarginTop, MarginRight, MarginBottom and MarginLeft are the margins in
	// points, zero for none; [DefaultMargin] is what a caller without a
	// preference passes. Each is zero or more, and they leave room between
	// them on the page.
	MarginTop, MarginRight, MarginBottom, MarginLeft float64
	// FontFamilies lists the families asked for, in preference order, as
	// [text.Style.Families] reads them; [DefaultFontFamilies] when empty.
	FontFamilies []string
	// FontSize is the font size in points, 12 when zero, at most 1000.
	FontSize float64
	// LineHeight is the distance between baselines as a multiple of the font
	// size, 1.15 when zero, at most 100.
	LineHeight float64
	// TabWidth is the distance between tab stops in characters, 8 when zero,
	// at most 64.
	TabWidth int
	// MaxInputBytes caps the source, [DefaultMaxInputBytes] when zero, at
	// most [limits.MaxConvertInputBytesCeiling].
	MaxInputBytes int64
	// MaxPages caps the output pages, [DefaultMaxPages] when zero, at most
	// [limits.MaxConvertPagesCeiling].
	MaxPages int
	// DefaultLang is the document language, a BCP 47 tag, "" to leave it
	// unknown: a text file names none. The caller validates it.
	DefaultLang string
	// Sink, when set, takes each page as soon as it is full, and the
	// document [Convert] returns holds no page: a page leaves memory once
	// it is written. nil keeps every page in the document.
	Sink compose.PageSink
}

// withDefaults returns o with every zero field set to its default, or an
// error wrapping [ErrInvalidOptions] for a field out of range. It never
// aliases o.FontFamilies.
func (o Options) withDefaults() (Options, error) {
	if o.PageWidth == 0 && o.PageHeight == 0 {
		o.PageWidth, o.PageHeight = DefaultPageWidth, DefaultPageHeight
	}
	if len(o.FontFamilies) == 0 {
		o.FontFamilies = DefaultFontFamilies
	} else {
		o.FontFamilies = slices.Clone(o.FontFamilies)
	}
	if o.FontSize == 0 {
		o.FontSize = DefaultFontSize
	}
	if o.LineHeight == 0 {
		o.LineHeight = DefaultLineHeight
	}
	if o.TabWidth == 0 {
		o.TabWidth = DefaultTabWidth
	}
	if o.MaxInputBytes == 0 {
		o.MaxInputBytes = DefaultMaxInputBytes
	}
	if o.MaxPages == 0 {
		o.MaxPages = DefaultMaxPages
	}

	for _, v := range []struct {
		name  string
		value float64
		max   float64
	}{
		{"page width", o.PageWidth, maxPageSide},
		{"page height", o.PageHeight, maxPageSide},
		{"font size", o.FontSize, maxFontSize},
		{"line height", o.LineHeight, maxLineHeight},
	} {
		// The negated range also refuses NaN.
		if !(v.value > 0 && v.value <= v.max) {
			return o, fmt.Errorf("%w: %s %v is not in (0, %v]", ErrInvalidOptions, v.name, v.value, v.max)
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
	if o.TabWidth < 0 || o.TabWidth > maxTabWidth {
		return o, fmt.Errorf("%w: tab width %d is not in [1, %d]", ErrInvalidOptions, o.TabWidth, maxTabWidth)
	}
	if o.MaxInputBytes < 0 || o.MaxInputBytes > limits.MaxConvertInputBytesCeiling {
		return o, fmt.Errorf("%w: input cap %d is not in [1, %d]", ErrInvalidOptions, o.MaxInputBytes, int64(limits.MaxConvertInputBytesCeiling))
	}
	if o.MaxPages < 0 || o.MaxPages > limits.MaxConvertPagesCeiling {
		return o, fmt.Errorf("%w: page cap %d is not in [1, %d]", ErrInvalidOptions, o.MaxPages, limits.MaxConvertPagesCeiling)
	}
	return o, nil
}
