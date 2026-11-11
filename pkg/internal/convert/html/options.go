package html

import (
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

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

// The range of [Options.Scale], the bounds of a browser's print scale.
const (
	// MinScale is the smallest content scale.
	MinScale = 0.1
	// MaxScale is the largest content scale.
	MaxScale = 2
)

// Options is the setup of an HTML conversion. The zero value asks for an A4
// page without margins, for print, with no resources, backgrounds and the
// outline in, no header or footer, no network.
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
	// only the first page is written.
	SinglePage bool
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
	// HeaderTemplate and FooterTemplate are HTML fragments drawn in the top
	// and bottom margins of every page: each is the inner HTML of a box as
	// wide as the page and as tall as the margin, laid out with the
	// template stylesheet of a browser's print header (see headerfooter.go),
	// where the text of an element of class date, title, url, pageNumber
	// or totalPages is replaced by Date, the document title, BaseURL, the
	// page number and the page count, and an element of class text that
	// reaches into the page area is hidden. Empty draws nothing.
	HeaderTemplate, FooterTemplate string
	// Date is what the date class of the templates shows, formatted for
	// the en-US locale as a browser's print header formats it, "11/6/26,
	// 10:30 AM". The zero time shows nothing. It is the only date of a
	// conversion; the output never depends on the clock.
	Date time.Time
	// BaseURL is the document's URL: what the url class of the templates
	// shows, and what relative references resolve against when it is an
	// http or https URL, fetched through HTTPClient. Empty, relative
	// references resolve in Resources. It is absolute when set.
	BaseURL string
	// HTTPClient fetches the http and https resources the document names:
	// stylesheets, images and fonts, each read up to the resource caps.
	// nil, the default, leaves such a reference out with a warning.
	HTTPClient *http.Client
	// MediaFeatures are the discrete media features the @media rules
	// evaluate against, by lowercase name, such as "prefers-color-scheme":
	// "dark" (Media Queries 5, 2.4). A feature absent keeps its default:
	// light, no-preference, none.
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
	// their !important ones above the document's (CSS Cascading 4, 6.2).
	// Their relative URLs resolve as the document's do.
	UserStylesheets []string
}

// withDefaults returns o with every zero field set to its default and
// the paper turned for Landscape, or an error wrapping
// [ErrInvalidOptions] for a field out of range. It is called once per
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
	if !(o.Scale >= MinScale && o.Scale <= MaxScale) {
		return o, fmt.Errorf("%w: scale %v is not in [%v, %v]", ErrInvalidOptions, o.Scale, MinScale, MaxScale)
	}
	if o.BaseURL != "" {
		u, err := url.Parse(o.BaseURL)
		if err != nil || !u.IsAbs() {
			return o, fmt.Errorf("%w: base URL %q is not an absolute URL", ErrInvalidOptions, o.BaseURL)
		}
	}
	for name := range o.MediaFeatures {
		if name == "" || name != strings.ToLower(name) {
			return o, fmt.Errorf("%w: media feature %q is not a lowercase name", ErrInvalidOptions, name)
		}
	}
	if o.MaxInputBytes < 0 || o.MaxInputBytes > limits.MaxConvertInputBytesCeiling {
		return o, fmt.Errorf("%w: input cap %d is not in [1, %d]", ErrInvalidOptions, o.MaxInputBytes, int64(limits.MaxConvertInputBytesCeiling))
	}
	if o.MaxPages < 0 || o.MaxPages > limits.MaxConvertPagesCeiling {
		return o, fmt.Errorf("%w: page cap %d is not in [1, %d]", ErrInvalidOptions, o.MaxPages, limits.MaxConvertPagesCeiling)
	}
	return o, nil
}
