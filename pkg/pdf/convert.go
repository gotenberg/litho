package pdf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gotenberg/litho/pkg/internal/compose"
	htmlconv "github.com/gotenberg/litho/pkg/internal/convert/html"
	"github.com/gotenberg/litho/pkg/internal/convert/plaintext"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// Format names an input format [Convert] reads.
type Format string

// The formats [Convert] reads.
const (
	// FormatText is plain text in UTF-8. See [Convert] for how it is laid
	// out.
	FormatText Format = "text"
	// FormatHTML is an HTML document with its CSS. See [Convert] for what
	// is drawn.
	FormatHTML Format = "html"
)

// ConvertOption tunes [Convert].
type ConvertOption func(*convertOptions)

// convertOptions collects what the [ConvertOption] values set.
type convertOptions struct {
	logger                               *slog.Logger
	fontDirs                             []string
	width, height                        float64
	marginTop, marginRight, marginBottom float64
	marginLeft                           float64
	marginsSet                           bool
	families                             []string
	fontSize                             float64
	limits                               Limits
	media                                string
	resources                            fs.FS
	landscape                            bool
	preferCSSPageSize                    bool
	scale                                float64
	singlePage                           bool
	pageRanges                           string
	omitBackgrounds                      bool
	omitOutline                          bool
	noShrinkToFit                        bool
	headerTemplate, footerTemplate       string
	date                                 time.Time
	baseURL                              string
	httpClient                           *http.Client
	mediaFeatures                        map[string]string
}

// WithConvertLogger injects a structured logger for the diagnostics of a
// [Convert] call: font files skipped while indexing, a font program embedded
// whole, a recovered panic. A nil logger discards them, the default.
func WithConvertLogger(logger *slog.Logger) ConvertOption {
	return func(o *convertOptions) {
		o.logger = logger
	}
}

// WithConvertFontDirs sets the directories [Convert] reads fonts from,
// searched recursively for .ttf, .otf, .ttc and .otc files, in the order
// given: the order decides between faces that match equally. Without it, or
// with no directory, Convert reads the operating system's font directories,
// the ones [WithSystemFonts] reads, and its output then depends on the fonts
// installed where litho runs.
//
// The faces of a list of directories are indexed once per process and kept
// for later calls with the same list, so a font added to a directory after
// its first conversion is not seen. A few lists are kept at a time; a call
// with a new list past them drops the oldest. Indexing the system directories
// of a desktop takes seconds, and the index holds every font file it read in
// memory, up to 1 GiB of them, with their parsed tables beside: a desktop of
// several hundred fonts costs gigabytes. A directory of the few families a
// service needs costs a fraction of that.
func WithConvertFontDirs(dirs ...string) ConvertOption {
	return func(o *convertOptions) {
		o.fontDirs = slices.Clone(dirs)
	}
}

// WithPaperSize sets the page size in points, width then height. The default
// is A4, 595.276 by 841.890 points. Each side must be finite, positive and at
// most 14,400 points (ISO 32000-1, Annex C.2), or [Convert] fails with
// [ErrInvalidConvertOptions]. Both zero keep the default.
func WithPaperSize(width, height float64) ConvertOption {
	return func(o *convertOptions) {
		o.width, o.height = width, height
	}
}

// WithMargins sets the page margins in points, zero for none. Without it
// every side is 56.693 points, 2 cm. A margin must be finite and zero or
// more, and the margins must leave room on the page, or [Convert] fails
// with [ErrInvalidConvertOptions].
func WithMargins(top, right, bottom, left float64) ConvertOption {
	return func(o *convertOptions) {
		o.marginTop, o.marginRight, o.marginBottom, o.marginLeft = top, right, bottom, left
		o.marginsSet = true
	}
}

// WithFontFamilies sets the font families a [FormatText] source is drawn
// in, in preference order,
// matched without regard to case or spaces. The generic families serif,
// sans-serif, monospace, cursive, fantasy and system-ui expand to common
// families of their kind. The default is Liberation Serif, the family a word
// processor opens a text file in, then serif. A character the chosen face
// lacks is drawn from another face that covers it. A [FormatHTML] source
// takes its fonts from its stylesheets and ignores this option.
func WithFontFamilies(families ...string) ConvertOption {
	return func(o *convertOptions) {
		o.families = slices.Clone(families)
	}
}

// WithFontSize sets the font size in points of a [FormatText] source, 12
// by default. It must be finite, positive and at most 1000, or [Convert]
// fails with [ErrInvalidConvertOptions]. Lines sit 1.15 times the size
// apart. A [FormatHTML] source takes its sizes from its stylesheets and
// ignores this option.
func WithFontSize(size float64) ConvertOption {
	return func(o *convertOptions) {
		o.fontSize = size
	}
}

// WithMediaType sets the media type the stylesheets of a [FormatHTML]
// source are evaluated for: "print", the default, or "screen". Any other
// value is refused with [ErrInvalidConvertOptions].
func WithMediaType(media string) ConvertOption {
	return func(o *convertOptions) { o.media = media }
}

// WithResources gives a [FormatHTML] source the files its relative URLs
// name: stylesheets and images, read from fsys with the URL path as the
// file path, the document sitting at the root. Without it, and for every
// absolute URL, root-relative path or path that leaves the root, the
// reference is left out and reported as a warning. data: URLs need no
// resources.
//
// A conversion reads at most 32 MiB a file and 256 MiB of files in all,
// decodes at most 4096 distinct images of at most 67 million pixels each
// and 268 million in all; a resource past a cap is left out with a
// warning.
func WithResources(fsys fs.FS) ConvertOption {
	return func(o *convertOptions) { o.resources = fsys }
}

// WithLandscape swaps the paper's width and height of a [FormatHTML]
// source, so the page is as wide as the paper is tall: [WithPaperSize]
// names the portrait sheet.
func WithLandscape(landscape bool) ConvertOption {
	return func(o *convertOptions) { o.landscape = landscape }
}

// WithPreferCSSPageSize lets an @page size descriptor of a [FormatHTML]
// source set the page size and orientation (CSS Page 3, 5.1). Without it,
// the default, the paper is the page: a document whose @page size differs
// is laid out at its @page size and scaled down to fit the paper, as
// Chromium prints without preferCSSPageSize. @page margins apply either
// way.
func WithPreferCSSPageSize(prefer bool) ConvertOption {
	return func(o *convertOptions) { o.preferCSSPageSize = prefer }
}

// WithPrintScale scales the content of a [FormatHTML] source: 1, the
// default, draws it as laid out; a scale in [0.1, 2] lays the content out
// in the page area divided by it and draws it at that scale, the page
// keeping its size and margins. Any other value is refused with
// [ErrInvalidConvertOptions].
func WithPrintScale(scale float64) ConvertOption {
	return func(o *convertOptions) { o.scale = scale }
}

// WithSinglePage writes a [FormatHTML] source on one page sized to its
// content, as Gotenberg's singlePage does. The route measures the document
// on Chromium's screen before printing, so the content is measured in one
// continuous column 800 CSS pixels wide, forced breaks ignored, at least
// 437.12 CSS pixels tall and 800 wide: the paper becomes that tall plus the
// top and bottom margins, and with [WithLandscape] that wide plus the left
// and right margins. The document is printed on it as usual and only the
// first page is written, whatever [WithPageRanges] says.
func WithSinglePage(single bool) ConvertOption {
	return func(o *convertOptions) { o.singlePage = single }
}

// WithPageRanges selects the pages of a [FormatHTML] source written,
// one-based, in document order, as "1-5, 8, 11-13"; empty, the default,
// writes every page. A range past the last page is ignored. A selection
// that keeps no page, a range whose start exceeds its end and a malformed
// specification are refused with [ErrInvalidConvertOptions]. Page
// counters in headers, footers and margin boxes count the whole document.
func WithPageRanges(spec string) ConvertOption {
	return func(o *convertOptions) { o.pageRanges = spec }
}

// WithPrintBackground sets whether the canvas background of a [FormatHTML]
// source prints. true, the default, prints every background; false drops
// the background colours and images of the html and body elements, which
// paint the canvas, and keeps every other element's, as Gotenberg's
// Chromium route does with printBackground false (it injects html, body {
// background: none !important } and makes every colour exact; Gotenberg
// issue 1154).
func WithPrintBackground(print bool) ConvertOption {
	return func(o *convertOptions) { o.omitBackgrounds = !print }
}

// WithShrinkToFit sets whether a [FormatHTML] source wider than its page
// area is shrunk to fit it. true, the default, lays it out at its content's
// width, at most 1.5 times the area's, and draws it scaled down to the
// area, as Chromium prints; false lays it out at the area's width and lets
// the page clip what overflows, as WeasyPrint prints.
func WithShrinkToFit(shrink bool) ConvertOption {
	return func(o *convertOptions) { o.noShrinkToFit = !shrink }
}

// WithOutline sets whether the headings of a [FormatHTML] source become
// the document outline. true is the default.
func WithOutline(outline bool) ConvertOption {
	return func(o *convertOptions) { o.omitOutline = !outline }
}

// WithHeaderTemplate draws the HTML fragment html in the top margin of
// every page of a [FormatHTML] source, as Chromium's printToPDF
// headerTemplate: the fragment is the inner HTML of a box as wide as the
// page and as tall as the margin, with Chromium's header stylesheet and
// defaults, where the text of an element of class date, title, url,
// pageNumber or totalPages is replaced by the print date
// ([WithPrintDate]), the document title, the document URL
// ([WithBaseURL]), the page number and the page count, and an element of
// class text that reaches into the page area is hidden. Empty, the
// default, draws nothing.
func WithHeaderTemplate(html string) ConvertOption {
	return func(o *convertOptions) { o.headerTemplate = html }
}

// WithFooterTemplate draws the HTML fragment html in the bottom margin of
// every page of a [FormatHTML] source, as [WithHeaderTemplate] does in the
// top one.
func WithFooterTemplate(html string) ConvertOption {
	return func(o *convertOptions) { o.footerTemplate = html }
}

// WithPrintDate sets what the date class of the header and footer
// templates shows, formatted as Chromium formats it for the en-US locale,
// "11/6/26, 10:30 AM". The zero time, the default, shows nothing. It is
// the only date of a conversion: the output never depends on the clock.
func WithPrintDate(t time.Time) ConvertOption {
	return func(o *convertOptions) { o.date = t }
}

// WithBaseURL sets the URL of a [FormatHTML] source: what the url class
// of the header and footer templates shows, and, for an http or https
// URL, what relative references resolve against, fetched through
// [WithHTTPClient]. Empty, the default, resolves relative references in
// [WithResources]. A URL that is not absolute is refused with
// [ErrInvalidConvertOptions].
func WithBaseURL(u string) ConvertOption {
	return func(o *convertOptions) { o.baseURL = u }
}

// WithHTTPClient fetches the http and https resources a [FormatHTML]
// source names, stylesheets, images and fonts, through c, each read up to
// the caps of [WithResources]. The caller's client decides what the
// document may reach: its transport, its timeouts and its redirects.
// nil, the default, leaves every such reference out with a warning.
func WithHTTPClient(c *http.Client) ConvertOption {
	return func(o *convertOptions) { o.httpClient = c }
}

// WithMediaFeatures sets the discrete media features the @media rules of
// a [FormatHTML] source evaluate against, by lowercase name, such as
// "prefers-color-scheme": "dark" (Media Queries 5, 2.4). A feature absent
// keeps its default: light, no-preference, none, what a printer reports.
// A name that is not lowercase is refused with
// [ErrInvalidConvertOptions].
func WithMediaFeatures(features map[string]string) ConvertOption {
	return func(o *convertOptions) { o.mediaFeatures = features }
}

// WithConvertLimits sets the resource budgets of a [Convert] call. It reads
// [Limits.MaxConvertInputBytes] and [Limits.MaxConvertPages]; a zero field
// keeps its default, and a field out of range makes Convert fail with
// [ErrInvalidLimits] before it reads the source.
func WithConvertLimits(l Limits) ConvertOption {
	return func(o *convertOptions) {
		o.limits = l
	}
}

// ConvertReport is what [Convert] reports beside the PDF.
type ConvertReport struct {
	// Pages is the number of pages written.
	Pages int
	// Warnings lists what the conversion could not render as the source
	// meant it, in the order met.
	Warnings []ConvertWarning
}

// ConvertWarning is one construct of the source [Convert] did not render
// faithfully.
type ConvertWarning struct {
	// Code is a stable identifier in kebab-case. "missing-glyph" flags a
	// character no font covers, drawn as a placeholder glyph, reported once
	// per character. "line-split" flags a source line of more than 65,536
	// characters, laid out in pieces that each start a new line. An HTML
	// conversion adds, among others, "unsupported-css-property" and the
	// other codes of a construct the cascade dropped, "resource-refused",
	// "resource-failed" and "resource-limit" for a stylesheet or image left
	// out, "unsupported-image", "unsupported-element" and "form-control".
	Code string
	// Message explains the construct and what was done instead.
	Message string
	// Page is the 1-based page the construct lands on, 0 when it has none.
	Page int
}

// Convert reads src in the given format, lays it out and writes the PDF to
// dst.
//
// # Plain text
//
// [FormatText] reads UTF-8. A leading byte order mark is dropped, an invalid
// byte reads as U+FFFD, and CR LF and a lone CR end a line as LF does.
//
//   - Each source line is a paragraph, wrapped at the width between the
//     margins at the line break opportunities of UAX #14, a word wider than
//     the line at a character boundary. Spaces are kept. There is no
//     hyphenation and no justification.
//   - Paragraph direction follows the first strong character (UAX #9), so
//     right-to-left text shapes and orders right to left; within a line, a
//     run of the opposite direction is reversed as a block, and deeper
//     embedding levels are not reordered. Every line starts at the left
//     margin.
//   - A tab moves to the next multiple of 8 characters from the start of the
//     line. An empty line takes one line height. A form feed starts a new
//     page; form feeds no text follows start none.
//   - Lines sit 1.15 times the font size apart and flow onto the next page
//     when the page is full, a paragraph included: there is no widow or
//     orphan control.
//
// The output is a tagged PDF 1.7 file (ISO 32000-1, 14.8): a Document
// element with one P element for each source line that draws text. Its
// language is left unset.
//
// # HTML
//
// [FormatHTML] reads an HTML document in the encoding its byte order mark
// or <meta charset> names, UTF-8 otherwise, and lays it out as CSS 2.1's
// visual formatting model asks for paged media, with the stylesheets of
// its <style> and <link rel=stylesheet> elements, their imports, its style
// attributes and its presentational attributes, evaluated for the media
// type of [WithMediaType]. No script runs.
//
//   - The page is the paper and margins of [WithPaperSize] and
//     [WithMargins] unless an @page rule sets a size, an orientation or a
//     margin. :first, :left and :right page rules do not apply.
//   - Stylesheets and images are read from [WithResources]. PNG, JPEG, GIF,
//     WebP, BMP and TIFF images are drawn at their pixel size at 96 dpi
//     unless styled otherwise; an image that cannot be read is replaced by
//     its alt text. SVG is not drawn yet.
//   - Form controls draw as the text they show. Embedded documents, media
//     and inline SVG are left out.
//   - Flex and grid layout lay out as blocks, and absolute and fixed
//     positioning as static, with a warning.
//
// The output is tagged: each element maps to the standard structure type
// of its meaning (ISO 32000-1, 14.8.4), images carry their alt text, and
// the root element's lang is the document's language. Links become Link
// annotations, h1 to h6 the outline, and <title> and <meta name> author,
// description and keywords the document information.
//
// # Both formats
//
// The fonts are embedded, each subset to the glyphs drawn. Nothing is
// written for PDF/A or PDF/UA conformance; remediate the output for that.
//
// # Errors
//
// Convert returns an error wrapping [ErrUnsupportedFormat] for a format it
// does not read, [ErrInvalidLimits] or [ErrInvalidConvertOptions] for an
// option out of range, [ErrConvertInputTooLarge] past
// [Limits.MaxConvertInputBytes] or for an HTML document past the
// converter's element, cascade or box caps, [ErrConvertTooManyPages] past
// [Limits.MaxConvertPages], [ErrConvertNoFonts] when the font directories
// hold no usable font, [ErrInternalPanic] for a recovered panic, and ctx's
// error when ctx ends first. On an error other than a write error on dst,
// nothing reaches dst: the whole file is built before it is written.
//
// # Ownership and concurrency
//
// Convert reads src to its end and writes dst; it closes neither. It is safe
// to call concurrently: calls share only the read-only font index.
//
// Identical source bytes, options and font files write identical bytes.
func Convert(ctx context.Context, src io.Reader, format Format, dst io.Writer, opts ...ConvertOption) (_ *ConvertReport, err error) {
	var o convertOptions
	for _, opt := range opts {
		opt(&o)
	}
	defer recoverToError(o.logger, "Convert", &err)

	if format != FormatText && format != FormatHTML {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedFormat, format)
	}
	if src == nil {
		return nil, fmt.Errorf("converting: %w", ErrNilSource)
	}
	if dst == nil {
		return nil, fmt.Errorf("%w: nil destination", ErrInvalidConvertOptions)
	}
	if err := o.limits.Validate(); err != nil {
		return nil, err
	}
	budget := o.limits.internal().WithDefaults()

	dirs := o.fontDirs
	if len(dirs) == 0 {
		dirs = hostFontDirs()
	}
	fonts, err := convertFonts(ctx, o.logger, dirs)
	if err != nil {
		return nil, fmt.Errorf("indexing fonts: %w", err)
	}

	if !o.marginsSet {
		o.marginTop, o.marginRight, o.marginBottom, o.marginLeft = plaintext.DefaultMargin, plaintext.DefaultMargin, plaintext.DefaultMargin, plaintext.DefaultMargin
	}
	var doc *compose.Document
	if format == FormatHTML {
		doc, err = convertHTML(ctx, src, fonts, &o, budget.MaxConvertInputBytes, budget.MaxConvertPages)
	} else {
		doc, err = plaintext.Convert(ctx, src, fonts, plaintext.Options{
			PageWidth:     o.width,
			PageHeight:    o.height,
			MarginTop:     o.marginTop,
			MarginRight:   o.marginRight,
			MarginBottom:  o.marginBottom,
			MarginLeft:    o.marginLeft,
			FontFamilies:  o.families,
			FontSize:      o.fontSize,
			MaxInputBytes: budget.MaxConvertInputBytes,
			MaxPages:      budget.MaxConvertPages,
		})
		if err != nil {
			err = fmt.Errorf("converting text: %w", err)
		}
	}
	if err != nil {
		return nil, err
	}
	if err := compose.Write(ctx, doc, dst, compose.Options{Logger: o.logger}); err != nil {
		return nil, fmt.Errorf("writing the PDF: %w", err)
	}

	report := &ConvertReport{Pages: len(doc.Pages)}
	for _, w := range doc.Warnings {
		report.Warnings = append(report.Warnings, ConvertWarning{Code: w.Code, Message: w.Message, Page: w.Page})
	}
	return report, nil
}

// convertHTML lays an HTML source out, mapping the HTML converter's errors
// onto the public sentinels: its option errors to
// [ErrInvalidConvertOptions], every cap on the document's size or
// complexity to [ErrConvertInputTooLarge], its page cap to
// [ErrConvertTooManyPages].
func convertHTML(ctx context.Context, src io.Reader, fonts *text.Fonts, o *convertOptions, maxBytes int64, maxPages int) (*compose.Document, error) {
	doc, err := htmlconv.Convert(ctx, src, fonts, htmlconv.Options{
		PageWidth:     o.width,
		PageHeight:    o.height,
		MarginTop:     o.marginTop,
		MarginRight:   o.marginRight,
		MarginBottom:  o.marginBottom,
		MarginLeft:    o.marginLeft,
		Media:         o.media,
		Resources:     o.resources,
		MaxInputBytes: maxBytes,
		MaxPages:      maxPages,
		Logger:        o.logger,

		Landscape:         o.landscape,
		PreferCSSPageSize: o.preferCSSPageSize,
		Scale:             o.scale,
		SinglePage:        o.singlePage,
		PageRanges:        o.pageRanges,
		OmitBackgrounds:   o.omitBackgrounds,
		OmitOutline:       o.omitOutline,
		NoShrinkToFit:     o.noShrinkToFit,
		HeaderTemplate:    o.headerTemplate,
		FooterTemplate:    o.footerTemplate,
		Date:              o.date,
		BaseURL:           o.baseURL,
		HTTPClient:        o.httpClient,
		MediaFeatures:     o.mediaFeatures,
	})
	switch {
	case err == nil:
		return doc, nil
	case errors.Is(err, htmlconv.ErrInvalidOptions):
		return nil, fmt.Errorf("converting HTML: %w: %w", ErrInvalidConvertOptions, err)
	case errors.Is(err, htmlconv.ErrInputTooLarge), errors.Is(err, htmlconv.ErrTooManyElements), errors.Is(err, htmlconv.ErrTooDeep),
		errors.Is(err, css.ErrTooComplex), errors.Is(err, layout.ErrTooManyBoxes):
		return nil, fmt.Errorf("converting HTML: %w: %w", ErrConvertInputTooLarge, err)
	case errors.Is(err, htmlconv.ErrTooManyPages):
		return nil, fmt.Errorf("converting HTML: %w: %w", ErrConvertTooManyPages, err)
	default:
		return nil, fmt.Errorf("converting HTML: %w", err)
	}
}

// maxConvertFontIndexes bounds the font indexes [convertFonts] keeps, one per
// list of directories. Anti-DoS guard, not spec-mandated: an index holds the
// font files it read, up to 1 GiB, so a caller cycling through directory
// lists must not pile them up. A service converts with one list, the system
// directories or its own, and rarely with more than two.
const maxConvertFontIndexes = 4

// errFontIndexAbandoned stands for an index whose build stopped without a
// result, a panic recovered further up: callers waiting on it build again.
var errFontIndexAbandoned = errors.New("font index build abandoned")

// convertFontIndex is one cached index, ready once its channel closes.
type convertFontIndex struct {
	ready chan struct{}
	fonts *text.Fonts
	err   error
}

// convertFontCache holds the font indexes of [Convert], keyed by directory
// list. Indexing a desktop's font directories takes seconds, which every
// call would otherwise pay again.
//
// The key keeps the order of the list rather than sorting it, because the
// order decides between faces that match equally: a per-user directory
// listed first wins over a system one, and two orders of one set are two
// indexes. Entries leave oldest first past [maxConvertFontIndexes].
var convertFontCache = struct {
	sync.RWMutex
	entries map[string]*convertFontIndex
	// order lists the keys oldest first.
	order []string
}{entries: make(map[string]*convertFontIndex)}

// convertFontKey returns the cache key of dirs: each cleaned, in order.
func convertFontKey(dirs []string) string {
	clean := make([]string, len(dirs))
	for i, d := range dirs {
		clean[i] = filepath.Clean(d)
	}
	return strings.Join(clean, "\x00")
}

// convertFonts returns the font index of dirs, built once and shared by every
// later call with the same list. Concurrent first calls build it once: the
// others wait for it, or for their ctx. A build that fails, which only ctx
// ending does, is not kept.
func convertFonts(ctx context.Context, logger *slog.Logger, dirs []string) (*text.Fonts, error) {
	key := convertFontKey(dirs)
	for {
		e, build := claimConvertFontIndex(key)
		if build {
			return buildConvertFontIndex(ctx, logger, key, dirs, e)
		}
		select {
		case <-e.ready:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if e.err == nil {
			return e.fonts, nil
		}
		// The build failed for its own caller's ctx, or was abandoned, and
		// left the cache; this call's ctx decides whether to try again.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
}

// claimConvertFontIndex returns the entry of key and whether the caller must
// build it, which it must when the entry is new.
func claimConvertFontIndex(key string) (*convertFontIndex, bool) {
	convertFontCache.RLock()
	e := convertFontCache.entries[key]
	convertFontCache.RUnlock()
	if e != nil {
		return e, false
	}
	convertFontCache.Lock()
	defer convertFontCache.Unlock()
	if e := convertFontCache.entries[key]; e != nil {
		return e, false
	}
	e = &convertFontIndex{ready: make(chan struct{})}
	convertFontCache.entries[key] = e
	convertFontCache.order = append(convertFontCache.order, key)
	for len(convertFontCache.order) > maxConvertFontIndexes {
		delete(convertFontCache.entries, convertFontCache.order[0])
		convertFontCache.order = convertFontCache.order[1:]
	}
	return e, true
}

// buildConvertFontIndex indexes dirs into e, the entry of key, and releases
// the calls waiting on it. A failed or abandoned build leaves the cache, so
// the next call builds again.
func buildConvertFontIndex(ctx context.Context, logger *slog.Logger, key string, dirs []string, e *convertFontIndex) (_ *text.Fonts, err error) {
	e.err = errFontIndexAbandoned
	defer func() {
		if e.err != nil {
			dropConvertFontIndex(key, e)
		}
		close(e.ready)
	}()
	fonts, err := text.NewFonts(ctx, logger, dirs)
	if err != nil {
		e.err = err
		return nil, err
	}
	e.fonts, e.err = fonts, nil
	return fonts, nil
}

// dropConvertFontIndex removes e from the cache when it is still the entry of
// key.
func dropConvertFontIndex(key string, e *convertFontIndex) {
	convertFontCache.Lock()
	defer convertFontCache.Unlock()
	if convertFontCache.entries[key] != e {
		return
	}
	delete(convertFontCache.entries, key)
	if i := slices.Index(convertFontCache.order, key); i >= 0 {
		convertFontCache.order = slices.Delete(convertFontCache.order, i, i+1)
	}
}
