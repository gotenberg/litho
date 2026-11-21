package html

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
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

// The range of [Options.Scale], the bounds of a browser's print scale.
const (
	// MinScale is the smallest content scale.
	MinScale = 0.1
	// MaxScale is the largest content scale.
	MaxScale = 2
)

// Options is the setup of an HTML conversion. The zero value asks for an A4
// page without margins, for print, with no resources, backgrounds and the
// outline in, no band, no network.
//
// The print options are litho's own. Where a browser's print dialog has
// the same control, the option behaves as the browser does, so a document
// prints the same way through either; each option says so where it
// applies.
type Options struct {
	// PageWidth and PageHeight are the paper size in points, A4 (595.276 by
	// 841.890) when both are zero. Each side lies in (0, 14400]. They name
	// the portrait sheet; see Landscape and PreferCSSPageSize for what an
	// @page size descriptor does to them.
	PageWidth, PageHeight float64
	// Landscape swaps the paper's width and height, so the page is
	// PageHeight wide by PageWidth tall.
	Landscape bool
	// PreferCSSPageSize lets an @page size descriptor set the page size and
	// orientation (CSS Page 3, 5.1). Without it, the default, the paper is
	// the page: a document whose @page size differs is laid out at its
	// @page size and scaled down to fit the paper, as browsers print a
	// document whose page size is not the paper's. @page margins apply
	// either way.
	PreferCSSPageSize bool
	// Scale scales the content: 1 when zero, else in [MinScale, MaxScale].
	// The page keeps its size and margins; the content is laid out in the
	// page area divided by Scale and drawn at Scale, the way a browser's
	// print scale works.
	Scale float64
	// SinglePage writes one page sized to the content: the document is
	// measured first as a screen 800 CSS pixels wide would show it, in one
	// continuous column without margins or forced breaks, its height at
	// least 437.12 CSS pixels and its width at least 800. The paper is
	// then as tall as that height plus the top and bottom margins, and in
	// Landscape as wide as that width plus the left and right margins;
	// the document is printed on it as usual, forced breaks cutting, and
	// only the first page is written. The page may pass the 14,400 points
	// of ISO 32000-1, Annex C.2, up to FittedLimit: content measured past
	// it is left out, with a warning.
	SinglePage bool
	// FittedLimit is the largest side, in points, of the single page:
	// [typeset.FittedPageLimit] of the version the file is written at.
	// Zero takes that of PDF 1.7.
	FittedLimit float64
	// OmitBackgrounds drops the background colours and images of the html
	// and body elements, which paint the canvas (CSS 2.1, 14.2), and keeps
	// every other element's and the page box's: the page comes out white
	// behind content whose own colours are exact, which is what a browser
	// prints with backgrounds off before its economy mode blanks the
	// rest. Off, every background prints.
	OmitBackgrounds bool
	// OmitOutline leaves the document outline out. Off, the headings
	// become the outline.
	OmitOutline bool
	// NoShrinkToFit lays a document wider than its page area out at the
	// area's width, the page clipping what overflows, as WeasyPrint
	// prints. Off, the default, such a document is laid out again at its
	// content's width, at most 1.5 times the area's, and drawn scaled down
	// to the area, as browsers print (blink::LocalFrameView::
	// ForceLayoutForPagination).
	NoShrinkToFit bool
	// FractionalFontMetrics lays lines out with the fonts' fractional
	// ascent, descent and line gap, as WeasyPrint does. Off, the default,
	// they are rounded to whole CSS pixels first, as browsers round them
	// for document text (blink::SimpleFontData::PlatformInit), which puts
	// every line of line-height: normal where a browser puts it.
	FractionalFontMetrics bool
	// Bands are the headers and footers drawn in the top and bottom
	// margins, in order: on a page two bands of the same margin select,
	// the later one draws (see [Band]). At most [MaxBands].
	Bands []Band
	// PageCounterStart is the value of the page counter before the first
	// page: the first page is numbered PageCounterStart + 1, 1 when zero
	// (CSS Page 3, 6.1). The page context's counter properties apply on
	// top of it. It lies in [MinPageCounterStart, MaxPageCounterStart].
	PageCounterStart int
	// BaseURL is the document's URL: what relative references resolve
	// against when it is an http or https URL, fetched through HTTPClient. Empty, relative
	// references resolve in Resources. It is absolute when set.
	BaseURL string
	// HTTPClient fetches the http and https resources the document names:
	// stylesheets, images and fonts, each read up to the resource caps.
	// nil, the default, leaves such a reference out with a warning.
	HTTPClient *http.Client
	// MediaFeatures are the discrete media features the @media rules
	// evaluate against, by lowercase name, such as "prefers-color-scheme":
	// "dark" (Media Queries 5, 2.4). A feature absent keeps the default
	// css.Media gives it for the media type.
	MediaFeatures map[string]string
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
	// DefaultLang is the document language, a BCP 47 tag, when the root
	// element names none, "" to leave it unknown. The caller validates it.
	DefaultLang string
	// UserStylesheets are stylesheets of the user origin, in order: their
	// declarations cascade above the user agent's and below the document's,
	// their !important ones above the document's (CSS Cascading 4, 6.2),
	// and an author declaration of revert takes their value. They hold
	// what an author sheet holds: @page rules and their margin boxes,
	// whose size counts as one the document names for PreferCSSPageSize,
	// @font-face, @media and @import. Their relative URLs resolve against
	// the document's base. Each counts against the resource caps as a
	// linked stylesheet does, and one past them is left out with a
	// warning. A band that is not isolated is styled by them too.
	UserStylesheets []string
	// DerivedAltText gives an image its markup does not describe the
	// description the document states of it apart from alt: the caption
	// of a figure the image alone fills (HTML Standard, 4.8.4.4.11), its
	// aria-label, an svg element's desc (SVG 2, 5.9). false, the default,
	// leaves such an image without one.
	DerivedAltText bool
	// AnimationsAtEnd prints each CSS animation at the end of its active
	// interval, as the page shows once its animations have run: an
	// animation's last keyframe where its fill mode keeps it, the
	// element's own style where it does not, the end of the first
	// iteration for one that repeats forever. Off, the default, prints
	// every animation at time zero, the moment a browser prints a page
	// that has just loaded. A paused animation, and one on a scroll or a
	// view timeline, prints the same either way. No transition runs
	// either way: a document's style changes once, when it loads.
	AnimationsAtEnd bool
	// RequiredResources ends the conversion with [ErrResourceUnavailable]
	// at the first stylesheet, @import, picture or font the document names
	// that does not load (see [loadFailed]), where false, the default,
	// leaves it out with a warning.
	RequiredResources bool

	// stop cancels the conversion's context with the error of a required
	// resource that did not load; [convert] sets it, nil when
	// RequiredResources is false. It does not outlive the call.
	stop context.CancelCauseFunc
	// maxElements caps the elements of the document: [MaxElements] when
	// zero, which a larger value cannot raise. A test lowers it to hold a
	// document at the cap without cascading the full count.
	maxElements int
}

// elementCap returns the cap on the elements of the document: o's own when
// it is set below [MaxElements], [MaxElements] otherwise.
func (o Options) elementCap() int {
	if o.maxElements > 0 && o.maxElements < MaxElements {
		return o.maxElements
	}
	return MaxElements
}

// withDefaults returns o with every zero field set to its default and
// the paper turned for Landscape, or an error wrapping
// [typeset.ErrInvalidOptions] for a field out of range. It is called once per
// conversion: Landscape stays set, so a second call would turn the paper
// back.
func (o Options) withDefaults() (Options, error) {
	if o.PageWidth == 0 && o.PageHeight == 0 {
		o.PageWidth, o.PageHeight = DefaultPageWidth, DefaultPageHeight
	}
	if o.Landscape {
		// The paper turns before the @page rules see it, as a browser's
		// landscape setting turns the print settings' paper
		// (printing::PrintSettings::SetOrientation); the margins stay on
		// their sides.
		o.PageWidth, o.PageHeight = o.PageHeight, o.PageWidth
	}
	if o.Media == "" {
		o.Media = MediaPrint
	}
	if o.Scale == 0 {
		o.Scale = 1
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
		// A NaN is not positive, and is refused with what is not.
		if !numeric.Positive(v.value) || v.value > maxPageSide {
			return o, fmt.Errorf("%w: %s %v is not in (0, %v]", typeset.ErrInvalidOptions, v.name, v.value, maxPageSide)
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
		if !isMargin(v.value) {
			return o, fmt.Errorf("%w: %s %v is not a finite number of points, zero or more", typeset.ErrInvalidOptions, v.name, v.value)
		}
	}
	if o.MarginLeft+o.MarginRight >= o.PageWidth {
		return o, fmt.Errorf("%w: left and right margins %v and %v leave no room on a page %v points wide", typeset.ErrInvalidOptions, o.MarginLeft, o.MarginRight, o.PageWidth)
	}
	if o.MarginTop+o.MarginBottom >= o.PageHeight {
		return o, fmt.Errorf("%w: top and bottom margins %v and %v leave no room on a page %v points high", typeset.ErrInvalidOptions, o.MarginTop, o.MarginBottom, o.PageHeight)
	}
	if o.Media != MediaPrint && o.Media != MediaScreen {
		return o, fmt.Errorf("%w: media type %q is neither %q nor %q", typeset.ErrInvalidOptions, o.Media, MediaPrint, MediaScreen)
	}
	if !numeric.Finite(o.Scale) || o.Scale < MinScale || o.Scale > MaxScale {
		return o, fmt.Errorf("%w: scale %v is not in [%v, %v]", typeset.ErrInvalidOptions, o.Scale, MinScale, MaxScale)
	}
	if o.BaseURL != "" {
		u, err := url.Parse(o.BaseURL)
		if err != nil || !u.IsAbs() {
			return o, fmt.Errorf("%w: base URL %q is not an absolute URL", typeset.ErrInvalidOptions, o.BaseURL)
		}
	}
	for name := range o.MediaFeatures {
		if name == "" || name != strings.ToLower(name) {
			return o, fmt.Errorf("%w: media feature %q is not a lowercase name", typeset.ErrInvalidOptions, name)
		}
	}
	if o.MaxInputBytes < 0 || o.MaxInputBytes > limits.MaxConvertInputBytesCeiling {
		return o, fmt.Errorf("%w: input cap %d is not in [1, %d]", typeset.ErrInvalidOptions, o.MaxInputBytes, int64(limits.MaxConvertInputBytesCeiling))
	}
	if o.MaxPages < 0 || o.MaxPages > limits.MaxConvertPagesCeiling {
		return o, fmt.Errorf("%w: page cap %d is not in [1, %d]", typeset.ErrInvalidOptions, o.MaxPages, limits.MaxConvertPagesCeiling)
	}
	if o.PageCounterStart < MinPageCounterStart || o.PageCounterStart > MaxPageCounterStart {
		return o, fmt.Errorf("%w: page counter start %d is not in [%d, %d]", typeset.ErrInvalidOptions, o.PageCounterStart, MinPageCounterStart, MaxPageCounterStart)
	}
	if err := checkBands(o.Bands); err != nil {
		return o, err
	}
	return o, nil
}

// fittedLimit returns the largest side of a single page (see
// [Options.FittedLimit]).
func (o Options) fittedLimit() float64 {
	if o.FittedLimit > 0 {
		return o.FittedLimit
	}
	return typeset.FittedPageLimit(typeset.Version17)
}
