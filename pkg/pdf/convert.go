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
	"github.com/gotenberg/litho/pkg/internal/convert/csv"
	"github.com/gotenberg/litho/pkg/internal/convert/docx"
	htmlconv "github.com/gotenberg/litho/pkg/internal/convert/html"
	"github.com/gotenberg/litho/pkg/internal/convert/markdown"
	"github.com/gotenberg/litho/pkg/internal/convert/odp"
	"github.com/gotenberg/litho/pkg/internal/convert/ods"
	"github.com/gotenberg/litho/pkg/internal/convert/odt"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/plaintext"
	"github.com/gotenberg/litho/pkg/internal/convert/pptx"
	"github.com/gotenberg/litho/pkg/internal/convert/xlsx"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/models"
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
	// FormatMarkdown is a Markdown document, rendered to HTML and laid out
	// as [FormatHTML] is. See [Convert].
	FormatMarkdown Format = "markdown"
	// FormatDOCX is a WordprocessingML document, a DOCX file (ECMA-376).
	// See [Convert] for what is drawn.
	FormatDOCX Format = "docx"
	// FormatXLSX is a SpreadsheetML workbook, an XLSX file (ECMA-376), a
	// macro-enabled XLSM or a template XLTX alike; macros never run. See
	// [Convert].
	FormatXLSX Format = "xlsx"
	// FormatODS is an OpenDocument spreadsheet, an ODS file, or a flat FODS
	// document (ODF 1.3). See [Convert].
	FormatODS Format = "ods"
	// FormatCSV is a comma-separated values file (RFC 4180), laid out as a
	// spreadsheet of one sheet. See [Convert].
	FormatCSV Format = "csv"
	// FormatPPTX is a PresentationML presentation, a PPTX file (ECMA-376),
	// a macro-enabled PPTM, a template POTX or a slide show PPSX alike;
	// macros never run. See [Convert].
	FormatPPTX Format = "pptx"
	// FormatODP is an OpenDocument presentation, an ODP file, a template
	// OTP, or a flat FODP document (ODF 1.3). See [Convert].
	FormatODP Format = "odp"
	// FormatODT is an OpenDocument text document, an ODT file (ODF 1.3).
	// See [Convert] for what is drawn.
	FormatODT Format = "odt"
)

// ConvertOption tunes [Convert].
type ConvertOption func(*convertOptions)

// convertOptions collects what the [ConvertOption] values set.
type convertOptions struct {
	logger                               *slog.Logger
	fontDirs                             []string
	width, height                        float64
	paperSet                             bool
	marginTop, marginRight, marginBottom float64
	marginLeft                           float64
	marginsSet                           bool
	families                             []string
	fontSize                             float64
	fontSizeSet                          bool
	limits                               Limits
	media                                string
	resources                            fs.FS
	landscape                            bool
	preferCSSPageSize                    bool
	scale                                float64
	scaleSet                             bool
	singlePage                           bool
	hiddenSlides                         bool
	notesPages                           bool
	pageRanges                           string
	omitBackgrounds                      bool
	omitOutline                          bool
	noShrinkToFit                        bool
	fractionalFontMetrics                bool
	headerTemplate, footerTemplate       string
	date                                 time.Time
	baseURL                              string
	httpClient                           *http.Client
	mediaFeatures                        map[string]string
	password                             string
	version                              string
	pdfa                                 models.PDFAProfile
	pdfua                                models.PDFUAProfile
	tempDir                              string
	defaultWidth, defaultHeight          float64
	defaultLang                          string

	// spool is compose.Options.Spool and the hold of the conversion's
	// compose.PictureStore: zero in every public use, set by tests to
	// force the spool files on or off.
	spool int64
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
// of a desktop takes seconds. An index keeps about a kilobyte a face, and
// the faces conversions draw with: up to 128 MiB of parsed fonts and 128
// MiB of font programs, the least recently used leaving first. A directory
// of the few families a service needs costs a fraction of that.
func WithConvertFontDirs(dirs ...string) ConvertOption {
	return func(o *convertOptions) {
		o.fontDirs = slices.Clone(dirs)
	}
}

// WithPaperSize sets the size of every page in points, width then height,
// in every format: the paper replaces the size and the orientation the
// source names, and [WithLandscape] then turns it. Each side must be
// finite, positive and at most 14,400 points (ISO 32000-1, Annex C.2), or
// [Convert] fails with [ErrInvalidConvertOptions], whatever the format.
// Both zero is the same as no paper.
//
// Without it, the source's own size applies: an @page size of a
// [FormatHTML] or [FormatMarkdown] source, each section of a
// [FormatDOCX] or [FormatODT] source, each sheet of a spreadsheet, the
// slide size of a presentation. A source that names none takes
// [WithDefaultPaperSize], A4 without it.
//
//   - [FormatHTML] and [FormatMarkdown]: the paper is the page unless
//     [WithPreferCSSPageSize] lets an @page size win, as browsers print.
//     A page whose @page size differs is laid out at that size and drawn
//     scaled down to fit the paper, centred, the paper turned to the
//     page's orientation.
//   - [FormatDOCX], [FormatODT] and the spreadsheets: every section or
//     sheet is laid out on the paper, portrait or landscape as the paper
//     is, as LibreOffice applies a paper. A sheet fitted to a page count
//     is fitted to the new paper.
//   - [FormatPPTX] and [FormatODP]: each slide, and each notes page, is
//     scaled down to fit the paper, never up, and centred on it.
//   - With [WithSinglePage], a sheet takes its content's size and the
//     paper is reported as ignored.
func WithPaperSize(width, height float64) ConvertOption {
	return func(o *convertOptions) {
		o.width, o.height = width, height
		o.paperSet = width != 0 || height != 0
	}
}

// WithMargins sets the page margins in points, zero for none. Without it
// every side is 56.693 points, 2 cm. A margin must be finite and zero or
// more, and the margins must leave room on the page, or [Convert] fails
// with [ErrInvalidConvertOptions], whatever the format.
//
// [FormatText], [FormatHTML] and [FormatMarkdown] apply them, an @page
// margin winning on its side. The Office formats keep the margins their
// documents carry: there, margins other than the default are reported
// with [CodeOptionIgnored].
func WithMargins(top, right, bottom, left float64) ConvertOption {
	return func(o *convertOptions) {
		o.marginTop, o.marginRight, o.marginBottom, o.marginLeft = top, right, bottom, left
		o.marginsSet = true
	}
}

// WithFontFamilies sets the font families a [FormatText] source is drawn
// in, in preference order, matched without regard to case or spaces. The
// generic families serif, sans-serif, monospace, cursive, fantasy and
// system-ui expand to common families of their kind. The default is
// Liberation Serif, the family a word processor opens a text file in, then
// serif. A character the chosen face lacks is drawn from another face that
// covers it. Every other format takes its fonts from the document: there,
// a family list is reported with [CodeOptionIgnored].
func WithFontFamilies(families ...string) ConvertOption {
	return func(o *convertOptions) {
		o.families = slices.Clone(families)
	}
}

// WithFontSize sets the font size in points of a [FormatText] source, 12
// by default. It must be finite, positive and at most 1000, or [Convert]
// fails with [ErrInvalidConvertOptions], whatever the format. Lines sit
// 1.15 times the size apart. Every other format takes its sizes from the
// document: there, the option is reported with [CodeOptionIgnored].
func WithFontSize(size float64) ConvertOption {
	return func(o *convertOptions) {
		o.fontSize, o.fontSizeSet = size, true
	}
}

// WithMediaType sets the media type the stylesheets of a [FormatHTML] or
// [FormatMarkdown] source are evaluated for: "print", the default, or
// "screen". Any other value is refused with [ErrInvalidConvertOptions],
// whatever the format; "screen" is reported with [CodeOptionIgnored] for
// the other formats.
func WithMediaType(media string) ConvertOption {
	return func(o *convertOptions) { o.media = media }
}

// WithResources gives a [FormatHTML] source the files its relative URLs
// name: stylesheets and images, read from fsys with the URL path as the
// file path, the document sitting at the root. Without it, and for every
// root-relative path or path that leaves the root, the reference is left
// out and reported as a warning. data: URLs need no resources; http and
// https URLs are fetched through [WithHTTPClient], file: URLs never.
//
// A conversion reads at most 32 MiB a file and 256 MiB of files in all,
// and decodes at most 4096 distinct images, each within
// [Limits.MaxImagePixels]; a resource past a cap is left out with a
// warning. A [FormatMarkdown] source reads its images alike; the other
// formats report resources with [CodeOptionIgnored]. An image costs
// [Limits.MaxConvertMemoryBytes] 4 bytes a pixel (8 for 16 bits a channel)
// while it is decoded, then its compressed samples for as long as the
// conversion holds it; a JPEG written as it is costs its file: a document
// whose images the budget cannot hold fails with [ErrConvertInputTooLarge].
func WithResources(fsys fs.FS) ConvertOption {
	return func(o *convertOptions) { o.resources = fsys }
}

// WithLandscape turns every portrait page to landscape, its long side
// across, in every format, whatever named its size: the paper of
// [WithPaperSize], the size the source names or the default paper. A
// landscape page is kept.
//
//   - [FormatHTML] and [FormatMarkdown]: the paper turns before the @page
//     rules see it, as a browser's landscape setting turns its paper. An
//     @page size that applies (with [WithPreferCSSPageSize], or without
//     [WithPaperSize]) wins over it, as in Chromium; an @page orientation
//     alone turns the page again.
//   - [FormatDOCX] and [FormatODT]: every section; spreadsheets: every
//     sheet; presentations: every slide page and notes page.
//   - With [WithSinglePage], a sheet takes its content's size and the
//     option is reported with [CodeOptionIgnored].
func WithLandscape(landscape bool) ConvertOption {
	return func(o *convertOptions) { o.landscape = landscape }
}

// WithPreferCSSPageSize lets an @page size descriptor of a [FormatHTML]
// or [FormatMarkdown] source set the page size and orientation over
// [WithPaperSize] (CSS Page 3, 5.1), as Chromium's preferCSSPageSize
// does. Without it, the default, the caller's paper is the page: a
// document whose @page size differs is laid out at its @page size and
// scaled down to fit the paper, centred on it, never scaled up. Without
// [WithPaperSize] the @page size is the page either way. @page margins
// apply either way. The other formats report true with
// [CodeOptionIgnored].
func WithPreferCSSPageSize(prefer bool) ConvertOption {
	return func(o *convertOptions) { o.preferCSSPageSize = prefer }
}

// WithPrintScale scales the content of a [FormatHTML] or [FormatMarkdown]
// source: 1, the default, draws it as laid out; a scale in [0.1, 2] lays
// the content out in the page area divided by it and draws it at that
// scale, the page keeping its size and margins. Any other value, 0
// included, is refused with [ErrInvalidConvertOptions], whatever the
// format. The other formats report a scale other than 1 with
// [CodeOptionIgnored]: a spreadsheet's print scale is its page setup's.
func WithPrintScale(scale float64) ConvertOption {
	return func(o *convertOptions) { o.scale, o.scaleSet = scale, true }
}

// WithSinglePage writes a [FormatHTML] or [FormatMarkdown] source on one
// page sized to its content: the document is measured first as a screen
// 800 CSS pixels wide would show it, in one continuous column, forced
// breaks ignored, at least 437.12 CSS pixels tall and 800 wide, and the
// paper becomes that tall plus the top and bottom margins, and with
// [WithLandscape] that wide plus the left and right margins. The document
// is printed on it as usual and only the first page is written;
// [WithPageRanges] is reported as ignored. For a [FormatXLSX], [FormatODS]
// or [FormatCSV] source it lays each sheet out on one page the size of its
// content: the sheet's paper, scale and page breaks are ignored, and
// [WithPaperSize] and [WithLandscape] are reported as ignored. The other
// formats report it with [CodeOptionIgnored].
func WithSinglePage(single bool) ConvertOption {
	return func(o *convertOptions) { o.singlePage = single }
}

// WithPageRanges selects the pages written, in every format, by the page
// range specification [ParsePageRanges] reads ("1-3,7", "even", "odd",
// "last"), resolved against the pages the document lays out on; empty, the
// default, writes every page. The pages come out in the order the
// specification lists them, a page listed twice written twice, as [Split]
// honours a specification. A specification [ParsePageRanges] refuses,
// including a page past the last one, fails the conversion with
// [ErrInvalidConvertOptions], which also wraps [ErrInvalidPageRange]. The
// selection happens after layout: page counters in headers, footers and
// margin boxes count the whole document, a link to a page left out is
// dropped, and an outline entry on one is replaced by its entries.
func WithPageRanges(spec string) ConvertOption {
	return func(o *convertOptions) { o.pageRanges = spec }
}

// WithPrintBackground sets whether the canvas background of a [FormatHTML]
// or [FormatMarkdown] source prints. true, the default, prints every
// background; false drops the background colours and images of the html
// and body elements, which paint the canvas, and keeps every other
// element's, so the page is white behind content whose own colours stay
// exact. The other formats report false with [CodeOptionIgnored].
func WithPrintBackground(print bool) ConvertOption {
	return func(o *convertOptions) { o.omitBackgrounds = !print }
}

// WithShrinkToFit sets whether a [FormatHTML] or [FormatMarkdown] source
// wider than its page area is shrunk to fit it. true, the default, lays it
// out at its content's width, at most 1.5 times the area's, and draws it
// scaled down to the area, as browsers print a wide document; false lays
// it out at the area's width and lets the page clip what overflows. The
// other formats report false with [CodeOptionIgnored].
func WithShrinkToFit(shrink bool) ConvertOption {
	return func(o *convertOptions) { o.noShrinkToFit = !shrink }
}

// WithFractionalFontMetrics sets whether the lines of a [FormatHTML] or
// [FormatMarkdown] source use the fonts' fractional ascent, descent and
// line gap. false, the default, rounds them to whole CSS pixels first, as
// browsers do for document text, so lines of line-height: normal sit where
// a browser puts them; true keeps the fractions, as print formatters do.
// The other formats report true with [CodeOptionIgnored].
func WithFractionalFontMetrics(fractional bool) ConvertOption {
	return func(o *convertOptions) { o.fractionalFontMetrics = fractional }
}

// WithOutline sets whether the headings of a [FormatHTML] or
// [FormatMarkdown] source, the paragraphs of a [FormatDOCX] or [FormatODT]
// source whose style has an outline level, the sheets of a [FormatXLSX],
// [FormatODS] or [FormatCSV] source, or the slides of a [FormatPPTX] or
// [FormatODP] source, become the document outline. true is the default. A
// [FormatText] source has no outline and reports false with
// [CodeOptionIgnored].
func WithOutline(outline bool) ConvertOption {
	return func(o *convertOptions) { o.omitOutline = !outline }
}

// WithConvertPassword opens an encrypted [FormatDOCX], [FormatODT],
// [FormatXLSX], [FormatODS], [FormatPPTX] or [FormatODP] source with
// password. Without it, or with a wrong one, an encrypted source fails
// with [ErrConvertEncrypted]. [FormatText], [FormatHTML], [FormatMarkdown]
// and [FormatCSV] are never encrypted and report a password with
// [CodeOptionIgnored].
func WithConvertPassword(password string) ConvertOption {
	return func(o *convertOptions) { o.password = password }
}

// WithHiddenSlides sets whether the slides a [FormatPPTX] or [FormatODP]
// source hides from its show are written. false, the default, leaves
// them out, as a show is printed; true writes every slide. Slide numbers
// count the slides written. The other formats report true with
// [CodeOptionIgnored].
func WithHiddenSlides(include bool) ConvertOption {
	return func(o *convertOptions) { o.hiddenSlides = include }
}

// WithNotesPages writes, after the slides of a [FormatPPTX] or
// [FormatODP] source, one notes page per slide written: the slide drawn
// in its box and the slide's notes below it. A PPTX notes page is the
// notes size the presentation names; an ODP notes page prints on Letter,
// the notes page at its top-left corner, as Impress prints them. With
// [WithPaperSize] the notes page is scaled down to fit the paper and
// centred. false, the default, writes the slides alone. The other formats
// report true with [CodeOptionIgnored].
func WithNotesPages(notes bool) ConvertOption {
	return func(o *convertOptions) { o.notesPages = notes }
}

// WithHeaderTemplate draws the HTML fragment html in the top margin of
// every page of a [FormatHTML] or [FormatMarkdown] source: the fragment is
// the inner HTML of a box as wide as the page and as tall as the margin,
// laid out with the stylesheet and defaults of a browser's print header,
// where the text of an element of class date, title, url, pageNumber or
// totalPages is replaced by the print date ([WithPrintDate]), the
// document title, the document URL ([WithBaseURL]), the page number and
// the page count, and an element of class text that reaches into the page
// area is hidden. Empty, the default, draws nothing. The other formats
// report a template with [CodeOptionIgnored].
func WithHeaderTemplate(html string) ConvertOption {
	return func(o *convertOptions) { o.headerTemplate = html }
}

// WithFooterTemplate draws the HTML fragment html in the bottom margin of
// every page of a [FormatHTML] or [FormatMarkdown] source, as
// [WithHeaderTemplate] does in the top one. The other formats report a
// template with [CodeOptionIgnored].
func WithFooterTemplate(html string) ConvertOption {
	return func(o *convertOptions) { o.footerTemplate = html }
}

// WithPrintDate sets what the date class of the header and footer
// templates of a [FormatHTML] or [FormatMarkdown] source shows, formatted
// for the en-US locale as "11/6/26, 10:30 AM", what the date and time
// fields of the headers and footers of a [FormatXLSX], [FormatODS] or
// [FormatCSV] source show, as "11/06/2026" and "10:30:00", and what the
// date and time fields of the slides of a [FormatPPTX] or [FormatODP]
// source show in place of the text their file saved. The zero time, the
// default, shows nothing in a spreadsheet and the saved text in a
// presentation. It is the only date of a conversion: the output never
// depends on the clock. A [FormatDOCX] or [FormatODT] source shows the
// field results its file stores, and a [FormatText] source has no date:
// both report a date with [CodeOptionIgnored].
func WithPrintDate(t time.Time) ConvertOption {
	return func(o *convertOptions) { o.date = t }
}

// WithBaseURL sets the URL of a [FormatHTML] or [FormatMarkdown] source:
// what the url class of the header and footer templates shows, and, for an
// http or https URL, what relative references resolve against, fetched
// through [WithHTTPClient]. Empty, the default, resolves relative
// references in [WithResources]. A URL that is not absolute is refused
// with [ErrInvalidConvertOptions], whatever the format. The other formats
// report a URL with [CodeOptionIgnored].
func WithBaseURL(u string) ConvertOption {
	return func(o *convertOptions) { o.baseURL = u }
}

// WithHTTPClient fetches the http and https resources a [FormatHTML] or
// [FormatMarkdown] source names, stylesheets, images and fonts, through c,
// each read up to the caps of [WithResources]. The caller's client decides
// what the document may reach: its transport, its timeouts and its
// redirects. nil, the default, leaves every such reference out with a
// warning. The other formats report a client with [CodeOptionIgnored].
func WithHTTPClient(c *http.Client) ConvertOption {
	return func(o *convertOptions) { o.httpClient = c }
}

// WithMediaFeatures sets the discrete media features the @media rules of
// a [FormatHTML] or [FormatMarkdown] source evaluate against, by lowercase
// name, such as "prefers-color-scheme": "dark" (Media Queries 5, 2.4). A
// feature absent keeps its default: light, no-preference, none, what a
// printer reports. A name that is not lowercase is refused with
// [ErrInvalidConvertOptions], whatever the format. The other formats
// report features with [CodeOptionIgnored].
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
	// Conformance says what the profiles of [WithConvertPDFA] and
	// [WithConvertPDFUA] still find in the file: an empty Residual means it
	// conforms to them. nil without a profile.
	Conformance *RemediationReport
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
	// out, "resource-no-network" for an http or https one without
	// [WithHTTPClient], "resource-http-status" for one the server answered
	// outside 2xx, "image-format-unsupported" for an AVIF, JPEG XL or ICO
	// picture, "unsupported-element" and "form-control". [CodeOptionIgnored]
	// names an option the source's format does not take.
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
//   - The page is the @page size, else the paper, with the margins of
//     [WithMargins] unless an @page rule sets one (see "Paper and
//     orientation" below). :first, :left and :right page rules do not
//     apply.
//   - Stylesheets and images are read from [WithResources], or fetched
//     through [WithHTTPClient] for http and https URLs. PNG, JPEG, GIF,
//     WebP, BMP and TIFF images are drawn at their pixel size at 96 dpi
//     unless styled otherwise, a JPEG turned as its EXIF orientation says,
//     and <picture> and srcset choose their source as Chromium does at a
//     device pixel ratio of 1; an image that cannot be read is replaced by
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
// # Markdown
//
// [FormatMarkdown] reads Markdown in UTF-8 and renders it to HTML with
// github.com/gomarkdown/markdown at its defaults (tables, fenced code,
// autolinks, strikethrough, definition lists, footnotes), in a document
// without a stylesheet whose title is the first heading. That document is laid out as [FormatHTML]
// is, with every option of an HTML source: its relative images and links
// resolve in [WithResources] or against [WithBaseURL]. The raw HTML a
// Markdown document holds is kept, unsanitised: no script runs.
// [Limits.MaxConvertInputBytes] caps the Markdown, not the HTML it renders
// to.
//
// # DOCX and ODT
//
// [FormatDOCX] reads a WordprocessingML package and [FormatODT] an
// OpenDocument text package, and lays each out as a word processor prints
// it: the document's sections with their page size, orientation, margins,
// columns, headers and footers, its paragraph and character styles,
// numbering and lists, tables, pictures, footnotes and endnotes, fields,
// hyperlinks and bookmarks. [WithPaperSize] and [WithLandscape] override
// the sections' page setup; [WithPageRanges], [WithOutline] and
// [WithConvertPassword] apply; the other options of an HTML source, and
// [WithMargins], [WithFontFamilies] and [WithFontSize], do not, and are
// reported with [CodeOptionIgnored]: the document carries its own. A
// construct that is not drawn is reported as a warning. Macros never run; embedded objects draw as their preview
// picture when the package holds one.
//
// # XLSX, ODS and CSV
//
// [FormatXLSX] reads a SpreadsheetML workbook, [FormatODS] an OpenDocument
// spreadsheet and [FormatCSV] a comma-separated values file (fields
// separated by commas, semicolons or tabs, quoted with double quotes, in
// UTF-8, with numbers, dates and times detected, one sheet named Sheet1),
// and lays them out as a spreadsheet application prints a workbook: every
// sheet that is not hidden, in book order, its
// print ranges cut into pages by the sheet's page setup (paper,
// orientation, margins, scale or fit-to-pages, page order, repeated rows
// and columns, manual breaks), with its headers and footers, the cell
// values formatted by their number formats and drawn with their fonts,
// fills, borders, alignment, wrapping and rotation, merged cells, pictures,
// charts and hyperlinks. Formulas are never evaluated: a cell shows the
// value its file stores. A CSV file prints with a page footer and no
// header. [WithPaperSize] and [WithLandscape] override every sheet's
// page setup; [WithSinglePage] lays each sheet out on one page the size of
// its content; [WithPageRanges], [WithOutline], [WithConvertPassword] and
// [WithPrintDate] apply; the other options are reported as for a DOCX
// source. A construct that is not drawn is reported as a warning.
//
// # PPTX and ODP
//
// [FormatPPTX] reads a PresentationML presentation and [FormatODP] an
// OpenDocument presentation, and lays them out as a presentation prints:
// one page per slide at the slide size, the slide's background and its
// shapes in stacking order, the masters' and layouts' shapes that show
// through included, each shape its geometry with its fill and line, its
// picture, chart or table, its text laid out in its box with the insets,
// anchor, wrapping, columns and autofit the file asks for, bullets and
// numbering, and the slide number, date and footer fields. Hidden slides
// are left out unless [WithHiddenSlides] asks for them; [WithNotesPages]
// adds a notes page per slide. [WithPaperSize] and [WithLandscape] set the
// page size, the slide scaled to fit it; [WithPageRanges], [WithOutline]
// (the slide titles), [WithConvertPassword] and [WithPrintDate] apply;
// the other options are reported as for a DOCX source. Transitions,
// animations, narration and comments are not printed. A construct that
// is not drawn is reported as a warning.
//
// # Paper and orientation
//
// The page size follows one rule in every format:
//
//  1. [WithPaperSize] sets every page, its size and its orientation,
//     replacing what the source names. One exception, as browsers print:
//     with [WithPreferCSSPageSize], an @page size of an HTML or Markdown
//     source wins over it.
//  2. Without it, the source's own size applies: an @page size, each
//     section of a DOCX or ODT source, each sheet's page setup, the slide
//     size of a presentation, the notes size of its notes pages.
//  3. A source that names none, a text or Markdown file, an HTML document
//     without an @page size, a section or a sheet without a page setup, a
//     CSV file, takes [WithDefaultPaperSize], else A4.
//
// [WithLandscape] then turns every portrait page to landscape, in every
// format; in an HTML or Markdown source an @page size that applies keeps
// its own orientation, as in Chromium. A slide on a paper of another
// size is scaled down to fit it, never up, and centred; a sheet fitted
// to a page count is fitted to the new paper.
//
// # Options a format does not take
//
// Every option is checked whatever the format: an option out of range
// fails with [ErrInvalidConvertOptions] even where the format would not
// read it. An option set to something other than its default that the
// source's format does not take adds one [CodeOptionIgnored] warning
// naming it; each option's documentation says which formats take it.
//
// # All formats
//
// The fonts are embedded, each subset to the glyphs drawn. The file is PDF
// 1.7 unless [WithConvertVersion] names another version, and claims no
// conformance unless [WithConvertPDFA] or [WithConvertPDFUA] names a
// profile.
//
// # Errors
//
// Convert returns an error wrapping [ErrUnsupportedFormat] for a format it
// does not read, [ErrInvalidLimits] or [ErrInvalidConvertOptions] for an
// option out of range or a page range specification the document has no
// pages for, [ErrConvertInputTooLarge] past
// [Limits.MaxConvertInputBytes] or for an HTML document past the
// converter's element, cascade or box caps, [ErrConvertTooManyPages] past
// [Limits.MaxConvertPages], [ErrConvertNoFonts] when the font directories
// hold no usable font, [ErrConvertMalformed] for an Office source that
// is not a document of its format, [ErrConvertEncrypted] for one
// [WithConvertPassword] does not open, [ErrInternalPanic] for a recovered panic,
// and ctx's error when ctx ends first. On an error other than a write error on dst,
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

	switch format {
	case FormatText, FormatHTML, FormatMarkdown, FormatDOCX, FormatODT, FormatXLSX, FormatODS, FormatCSV, FormatPPTX, FormatODP:
	default:
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
	if err := o.validate(); err != nil {
		return nil, err
	}
	target, err := resolveConvertTarget(&o)
	if err != nil {
		return nil, err
	}
	budget := o.limits.internal().WithDefaults()
	ctx, mem, endBudget := withConvertBudget(ctx, budget)
	defer endBudget()
	defer func() { err = convertBudgetError(o.logger, mem, err) }()
	src = chargedSource(src, mem)
	// The pictures of a large document wait for the file in a temp file of
	// their own, removed on every path once the file is written.
	ctx, pictures := compose.WithPictureStore(ctx, o.tempDir, o.spool)
	defer func() {
		if err := pictures.Close(); err != nil && o.logger != nil {
			o.logger.Debug("convert: removing the picture store failed", slog.Any("error", err))
		}
	}()

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
	// Every converter tags its output, so the stream marks content as it
	// writes each page.
	stream, err := compose.NewStream(ctx, compose.Options{Logger: o.logger, Version: target.version, TempDir: o.tempDir, Spool: o.spool}, true)
	if err != nil {
		return nil, fmt.Errorf("writing the PDF: %w", err)
	}
	defer stream.Abort()
	selectPages(stream, format, &o)
	var doc *compose.Document
	switch format {
	case FormatHTML, FormatMarkdown:
		doc, err = convertHTML(ctx, src, format, fonts, &o, budget.MaxConvertInputBytes, budget.MaxConvertPages)
	case FormatDOCX, FormatODT, FormatXLSX, FormatODS, FormatCSV, FormatPPTX, FormatODP:
		doc, err = convertOffice(ctx, src, format, fonts, &o, budget.MaxConvertInputBytes, budget.MaxConvertPages)
	case FormatText:
		w, h := o.width, o.height
		if !o.paperSet {
			w, h = o.defaultPaper()
		}
		if o.landscape && w < h {
			w, h = h, w
		}
		doc, err = plaintext.Convert(ctx, src, fonts, plaintext.Options{
			PageWidth:     w,
			PageHeight:    h,
			MarginTop:     o.marginTop,
			MarginRight:   o.marginRight,
			MarginBottom:  o.marginBottom,
			MarginLeft:    o.marginLeft,
			FontFamilies:  o.families,
			FontSize:      o.fontSize,
			MaxInputBytes: budget.MaxConvertInputBytes,
			MaxPages:      budget.MaxConvertPages,
			DefaultLang:   o.defaultLang,
			Sink:          stream,
		})
		switch {
		case errors.Is(err, plaintext.ErrInvalidOptions):
			err = fmt.Errorf("converting text: %w: %w", ErrInvalidConvertOptions, err)
		case errors.Is(err, plaintext.ErrInputTooLarge):
			err = fmt.Errorf("converting text: %w: %w", ErrConvertInputTooLarge, err)
		case err != nil:
			err = fmt.Errorf("converting text: %w", err)
		}
	}
	if err != nil {
		return nil, err
	}
	if err := mem.Err(); err != nil {
		return nil, err
	}
	// A converter that does not hand its pages over as it makes them
	// returns them: they go to the stream now.
	for _, p := range doc.Pages {
		if err := stream.Page(ctx, p); err != nil {
			return nil, fmt.Errorf("writing the PDF: %w", err)
		}
	}
	tail := *doc
	tail.Pages = nil
	conformance, err := writeTarget(ctx, stream, &tail, dst, target, &o)
	if err != nil {
		return nil, err
	}

	report := &ConvertReport{Pages: stream.Pages(), Conformance: conformance, Warnings: ignoredOptions(format, &o)}
	for _, w := range tail.Warnings {
		report.Warnings = append(report.Warnings, ConvertWarning{Code: w.Code, Message: w.Message, Page: w.Page})
	}
	return report, nil
}

// convertHTML lays an HTML or Markdown source out, mapping the HTML
// converter's errors onto the public sentinels: its option errors to
// [ErrInvalidConvertOptions], every cap on the document's size or
// complexity to [ErrConvertInputTooLarge], its page cap to
// [ErrConvertTooManyPages].
func convertHTML(ctx context.Context, src io.Reader, format Format, fonts *text.Fonts, o *convertOptions, maxBytes int64, maxPages int) (*compose.Document, error) {
	conv, name := htmlconv.Convert, "HTML"
	if format == FormatMarkdown {
		conv, name = markdown.Convert, "Markdown"
	}
	// Without a caller's paper, the document's @page size is the page, as
	// a browser prints a document on no paper of its own (CSS Page 3,
	// 5.1): the default paper only stands in for a page that names none.
	// A single page keeps the paper it measures.
	w, h, prefer := o.width, o.height, o.preferCSSPageSize
	if !o.paperSet {
		w, h = o.defaultPaper()
		prefer = prefer || !o.singlePage
	}
	doc, err := conv(ctx, src, fonts, htmlconv.Options{
		PageWidth:     w,
		PageHeight:    h,
		MarginTop:     o.marginTop,
		MarginRight:   o.marginRight,
		MarginBottom:  o.marginBottom,
		MarginLeft:    o.marginLeft,
		Media:         o.media,
		Resources:     o.resources,
		MaxInputBytes: maxBytes,
		MaxPages:      maxPages,
		Logger:        o.logger,

		Landscape:             o.landscape,
		PreferCSSPageSize:     prefer,
		Scale:                 o.scale,
		SinglePage:            o.singlePage,
		OmitBackgrounds:       o.omitBackgrounds,
		OmitOutline:           o.omitOutline,
		NoShrinkToFit:         o.noShrinkToFit,
		FractionalFontMetrics: o.fractionalFontMetrics,
		HeaderTemplate:        o.headerTemplate,
		FooterTemplate:        o.footerTemplate,
		Date:                  o.date,
		BaseURL:               o.baseURL,
		HTTPClient:            o.httpClient,
		MediaFeatures:         o.mediaFeatures,
		DefaultLang:           o.defaultLang,
	})
	switch {
	case err == nil:
		return doc, nil
	case errors.Is(err, htmlconv.ErrInvalidOptions):
		return nil, fmt.Errorf("converting %s: %w: %w", name, ErrInvalidConvertOptions, err)
	case errors.Is(err, htmlconv.ErrInputTooLarge), errors.Is(err, htmlconv.ErrTooManyElements), errors.Is(err, htmlconv.ErrTooDeep),
		errors.Is(err, css.ErrTooComplex), errors.Is(err, layout.ErrTooManyBoxes):
		return nil, fmt.Errorf("converting %s: %w: %w", name, ErrConvertInputTooLarge, err)
	case errors.Is(err, htmlconv.ErrTooManyPages):
		return nil, fmt.Errorf("converting %s: %w: %w", name, ErrConvertTooManyPages, err)
	default:
		return nil, fmt.Errorf("converting %s: %w", name, err)
	}
}

// convertOffice lays an Office source out, mapping the Office converters'
// errors onto the public sentinels: their option errors to
// [ErrInvalidConvertOptions], their size caps to [ErrConvertInputTooLarge],
// their page cap to [ErrConvertTooManyPages], a package that is not one
// to [ErrConvertMalformed], an encrypted one to [ErrConvertEncrypted], and
// a converter not written yet to [ErrUnsupportedFormat].
func convertOffice(ctx context.Context, src io.Reader, format Format, fonts *text.Fonts, o *convertOptions, maxBytes int64, maxPages int) (*compose.Document, error) {
	conv, name := docx.Convert, "DOCX"
	switch format {
	case FormatODT:
		conv, name = odt.Convert, "ODT"
	case FormatXLSX:
		conv, name = xlsx.Convert, "XLSX"
	case FormatODS:
		conv, name = ods.Convert, "ODS"
	case FormatCSV:
		conv, name = csv.Convert, "CSV"
	case FormatPPTX:
		conv, name = pptx.Convert, "PPTX"
	case FormatODP:
		conv, name = odp.Convert, "ODP"
	case FormatDOCX, FormatText, FormatHTML, FormatMarkdown:
		// DOCX is the default; the others never reach here.
	}
	dw, dh := o.defaultPaper()
	opts := office.Options{
		Landscape:         o.landscape,
		DefaultPageWidth:  dw,
		DefaultPageHeight: dh,
		Password:          o.password,
		OmitOutline:       o.omitOutline,
		SinglePageSheets:  o.singlePage,
		PrintDate:         o.date,
		HiddenSlides:      o.hiddenSlides,
		NotesPages:        o.notesPages,
		DefaultLang:       o.defaultLang,
		MaxInputBytes:     maxBytes,
		MaxPages:          maxPages,
		Logger:            o.logger,
	}
	if o.paperSet {
		opts.PageWidth, opts.PageHeight = o.width, o.height
	}
	doc, err := conv(ctx, src, fonts, opts)
	switch {
	case err == nil:
		return doc, nil
	case errors.Is(err, office.ErrNotImplemented):
		return nil, fmt.Errorf("converting %s: %w: %w", name, ErrUnsupportedFormat, err)
	case errors.Is(err, office.ErrInvalidOptions):
		return nil, fmt.Errorf("converting %s: %w: %w", name, ErrInvalidConvertOptions, err)
	case errors.Is(err, office.ErrInputTooLarge), errors.Is(err, layout.ErrTooManyBoxes):
		return nil, fmt.Errorf("converting %s: %w: %w", name, ErrConvertInputTooLarge, err)
	case errors.Is(err, office.ErrTooManyPages):
		return nil, fmt.Errorf("converting %s: %w: %w", name, ErrConvertTooManyPages, err)
	default:
		// ErrMalformed and ErrEncrypted are the public sentinels themselves.
		return nil, fmt.Errorf("converting %s: %w", name, err)
	}
}

// selectPages makes stream write the pages that o's page range
// specification names, resolved against the pages the document lays out
// on with [ParsePageRanges], in the order it lists them; an empty
// specification keeps every page, and so does a single page of an HTML
// source, which is the one page written whatever the specification says.
// A specification [ParsePageRanges] refuses fails the stream with
// [ErrInvalidConvertOptions] wrapping its error.
func selectPages(stream *compose.Stream, format Format, o *convertOptions) {
	if o.pageRanges == "" || o.singlePage && (format == FormatHTML || format == FormatMarkdown) {
		return
	}
	spec := o.pageRanges
	stream.Select(func(count int) ([]int, error) {
		pages, err := ParsePageRanges(spec, count)
		if err != nil {
			return nil, fmt.Errorf("selecting pages: %w: %w", ErrInvalidConvertOptions, err)
		}
		return pages, nil
	})
}

// maxConvertFontIndexes bounds the font indexes [convertFonts] keeps, one per
// list of directories. Anti-DoS guard, not spec-mandated: an index holds up
// to 256 MiB of parsed fonts and font programs, so a caller cycling through
// directory lists must not pile them up. A service converts with one list, the system
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
