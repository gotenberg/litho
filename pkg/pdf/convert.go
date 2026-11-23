package pdf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"

	"github.com/gotenberg/litho/pkg/internal/convert/csv"
	"github.com/gotenberg/litho/pkg/internal/convert/docx"
	htmlconv "github.com/gotenberg/litho/pkg/internal/convert/html"
	"github.com/gotenberg/litho/pkg/internal/convert/markdown"
	"github.com/gotenberg/litho/pkg/internal/convert/odp"
	"github.com/gotenberg/litho/pkg/internal/convert/ods"
	"github.com/gotenberg/litho/pkg/internal/convert/odt"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/picture"
	"github.com/gotenberg/litho/pkg/internal/convert/plaintext"
	"github.com/gotenberg/litho/pkg/internal/convert/pptx"
	"github.com/gotenberg/litho/pkg/internal/convert/xlsx"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/models"
)

// convertKind is the format of the source a [Conversion] reads.
type convertKind int

const (
	kindText convertKind = iota
	kindHTML
	kindMarkdown
	kindDOCX
	kindODT
	kindXLSX
	kindODS
	kindCSV
	kindPPTX
	kindODP
	kindImage
)

// name names the kind in an error.
func (k convertKind) name() string {
	switch k {
	case kindText:
		return "text"
	case kindHTML:
		return "HTML"
	case kindMarkdown:
		return "Markdown"
	case kindDOCX:
		return "DOCX"
	case kindODT:
		return "ODT"
	case kindXLSX:
		return "XLSX"
	case kindODS:
		return "ODS"
	case kindCSV:
		return "CSV"
	case kindPPTX:
		return "PPTX"
	case kindODP:
		return "ODP"
	case kindImage:
		return "image"
	}
	return "source"
}

// Conversion is one source document and how it is laid out as a PDF. A
// constructor makes it, one per format the conversion reads: [FromText],
// [FromHTML], [FromMarkdown], [FromDOCX], [FromODT], [FromXLSX], [FromODS],
// [FromCSV], [FromPPTX], [FromODP] and [FromImage]. Each takes the options
// of its format alone, so an option a format has no use for does not
// compile.
//
// [Conversion.WriteTo] runs it alone. [Pipeline.Convert] opens a pipeline on
// its output, so that later stages edit, remediate, encrypt or sign what it
// wrote, and [Pipeline.MergePipelines] runs several at once and merges their
// outputs.
//
// # Paper and orientation
//
// The page size follows one rule in every format:
//
//  1. [WithPaper] sets every page, its size and its orientation, replacing
//     what the source names.
//  2. Without it, the source's own size applies: an @page size, each
//     section of a DOCX or ODT source, each sheet's page setup, the slide
//     size of a presentation, an image's size. An XLSX sheet that names a
//     paper other than Letter, Tabloid, Legal, Statement, Executive, A3,
//     A4, A5, B4 (JIS) or B5 (JIS), an envelope or a postcard, prints on
//     the paper of a sheet that names none, as Excel prints it.
//  3. A source that names none takes [WithDefaultPaper], else [models.PaperA4].
//
// [WithOrientation] then turns the pages that are not turned as it asks.
//
// # Output
//
// The file is tagged PDF (ISO 32000-1, 14.8) of the version
// [WithConvertVersion] names, 1.7 by default. Its fonts are embedded, each
// subset to the glyphs drawn; a font program that cannot be subset is
// embedded whole, which [WithConvertLogger] reports. It claims no
// conformance: remediate it in a
// later pipeline stage to conform to a PDF/A or PDF/UA profile. Identical
// source bytes, options and font files write identical bytes.
//
// # Memory
//
// A conversion spends memory within [models.Limits.MaxConvertMemoryBytes] and
// fails with [ErrConvertInputTooLarge] past it. The index of the installed
// fonts is not charged to it: the process shares it between conversions
// (see [WithConvertFontDirs]). A text source, a
// spreadsheet and a presentation write each page as it is laid out; an HTML
// or Markdown source and a DOCX or ODT source are laid out whole first.
//
// # Ownership and concurrency
//
// A Conversion reads its source once, to its end, and never closes it: it
// runs once, and a second run fails with [ErrConversionReused]. It is not
// safe for concurrent use. Distinct Conversions run concurrently: they
// share only the read-only font index.
type Conversion struct {
	kind convertKind
	src  io.Reader
	cfg  convertConfig
	// used is set by the first run.
	used atomic.Bool
}

// newConversion returns the conversion of src as kind with cfg.
func newConversion(kind convertKind, src io.Reader, cfg convertConfig) *Conversion {
	return &Conversion{kind: kind, src: src, cfg: cfg}
}

// FromText returns the conversion of src, plain text in UTF-8.
//
// A leading byte order mark is dropped, an invalid byte reads as U+FFFD,
// and CR LF and a lone CR end a line as LF does.
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
//   - A tab moves to the next tab stop ([WithTabWidth]). An empty line
//     takes one line height. A form feed starts a new page; form feeds no
//     text follows start none.
//   - Lines sit [WithLineHeight] apart and flow onto the next page when the
//     page is full, a paragraph included: there is no widow or orphan
//     control.
//
// The structure tree holds a Document element with one P element for each
// source line that draws text. The language is [WithDefaultLanguage]'s.
func FromText(src io.Reader, opts ...TextOption) *Conversion {
	var cfg convertConfig
	for _, o := range opts {
		if o != nil {
			o.applyConvert(&cfg)
		}
	}
	return newConversion(kindText, src, cfg)
}

// FromHTML returns the conversion of src, an HTML document with its CSS.
//
// The document is read in the encoding its byte order mark or <meta
// charset> names, UTF-8 otherwise, and laid out for paged media with the
// stylesheets of its <style> and <link rel=stylesheet> elements, their
// imports, its style attributes and its presentational attributes, and
// those of [WithUserStylesheet], evaluated for the media type of
// [WithMediaType]. No script runs: a document that holds one a browser
// would run reports [models.WarningScriptNotRun].
//
// The document's DOCTYPE sets its mode as it does in a browser (HTML
// Standard, 13.2.6.4.1): <!DOCTYPE html> lays it out in no-quirks mode;
// a document without a DOCTYPE, an HTML fragment among them, or with an
// old one, is laid out in quirks mode, with the quirks a printed page
// shows (Quirks Mode Standard); the transitional and frameset DOCTYPEs
// of HTML 4.01 with a system identifier and of XHTML 1.0 give the
// limited-quirks mode. Bands and a frame's srcdoc document are always in
// no-quirks mode.
//
//   - Layout covers the visual formatting model of CSS 2.1, flexbox, grid
//     without subgrid, multi-column layout, positioning, transforms,
//     generated content and counters, and bidirectional and CJK text.
//   - Paged media covers @page rules with named pages, page groups and
//     page-margin boxes (CSS Paged Media 3), and the footnotes, leaders,
//     running elements, string sets, cross-references and bookmarks of
//     CSS Generated Content.
//   - Headers and footers are the page-margin boxes of the document's
//     stylesheets, or the bands of [WithHeader] and [WithFooter]: HTML
//     fragments, each laid out in a document of its own and drawn in the
//     top or the bottom margin of the pages it selects in the place of
//     that margin's page-margin boxes, with the page's numbers, named
//     strings and cross-references (see [models.PageBand]).
//   - CSS animations print at time zero, or finished with
//     [WithFinishedAnimations]. No transition runs.
//   - Stylesheets, images and fonts are read from [WithResources], or
//     fetched through [WithHTTPClient] for http and https URLs. PNG, JPEG,
//     GIF, WebP, BMP, TIFF and SVG images are drawn, a JPEG or a PNG turned
//     as its EXIF orientation says; an image that cannot be read is
//     replaced by its alt text. @font-face reads TrueType, OpenType, WOFF
//     and WOFF2.
//   - Form controls draw with the widgets a browser gives them: text
//     fields, text areas, buttons, check boxes, radio buttons, drop-down
//     and list boxes, sliders, progress bars, meters, colour wells and
//     file controls, in the light or the dark colours of color-scheme
//     and the accent of accent-color. A control whose author styles its
//     border or its background draws that box in the widget's place. A
//     date or time field shows its value in the en-US form.
//   - A frame, an object or an embed draws the first viewport of its
//     document, an HTML document, a picture or a plain text file, which
//     loads as an image does. Audio is left out; a video draws its
//     poster, which loads as an image does, and neither its frames nor
//     its controls.
//
// A construct that is not drawn as the document means it is reported in
// [models.ConvertReport.Warnings].
//
// Each element maps to the standard structure type of its meaning (ISO
// 32000-1, 14.8.4), images carry their alt text, and the root element's
// lang is the document's language. Links become Link annotations, the
// elements with a bookmark-level the outline, h1 to h6 unless a stylesheet
// says otherwise, and <title> and <meta name> author, description and
// keywords the document information. Page-margin boxes and bands are
// artifacts and add no outline entry.
func FromHTML(src io.Reader, opts ...HTMLOption) *Conversion {
	var cfg convertConfig
	for _, o := range opts {
		if o != nil {
			o.applyConvert(&cfg)
		}
	}
	return newConversion(kindHTML, src, cfg)
}

// FromMarkdown returns the conversion of src, a Markdown document in UTF-8.
//
// The Markdown is rendered to HTML by github.com/gomarkdown/markdown with
// its common extensions: tables, fenced code blocks, autolinks,
// strikethrough, definition lists, heading identifiers written {#id},
// backslash line breaks, and math between dollar signs printed as its TeX
// source; and with smart punctuation: curly quotes, en and em dashes,
// fractions. Footnotes, automatic heading identifiers, superscripts and
// subscripts are not supported. The HTML is placed in the template of
// [WithMarkdownTemplate]. That document is laid out as
// [FromHTML] lays one out, with every option of an HTML source: its
// relative images and links resolve in [WithResources] or against
// [WithBaseURL]. The raw HTML a Markdown document holds is kept,
// unsanitised: no script runs. [models.Limits.MaxConvertInputBytes] caps the
// Markdown, not the HTML it renders to. The default template states no
// title; a template that writes <title>{{ .Title }}</title> titles the
// document with its first heading, which a band of [WithHeader] or
// [WithFooter] then shows as string(document-title).
func FromMarkdown(src io.Reader, opts ...MarkdownOption) *Conversion {
	var cfg convertConfig
	for _, o := range opts {
		if o != nil {
			o.applyConvert(&cfg)
		}
	}
	return newConversion(kindMarkdown, src, cfg)
}

// FromDOCX returns the conversion of src, a WordprocessingML document, a
// DOCX file (ECMA-376).
//
// The document is laid out as a word processor prints it: its sections with
// their page size, orientation, margins, columns, headers and footers, its
// paragraph and character styles, numbering and lists, tables, pictures,
// charts, text boxes, footnotes and endnotes, fields, hyperlinks and
// bookmarks. A field shows the result its file stores, a date or time field
// the date of [WithDate] when one is given, and a page number its page.
// [WithTrackedChanges] chooses how tracked changes print. Comments are left
// out of the pages; [WithComments] writes them as annotations. Macros never
// run; an embedded object draws as its preview picture when the package
// holds one. A construct that is not drawn is reported in
// [models.ConvertReport.Warnings].
//
// A source that is not a DOCX package fails with [ErrConvertMalformed], an
// encrypted one [WithOfficePassword] does not open with
// [ErrConvertEncrypted].
func FromDOCX(src io.Reader, opts ...WordProcessingOption) *Conversion {
	return newConversion(kindDOCX, src, wordProcessingConfig(opts))
}

// FromODT returns the conversion of src, an OpenDocument text document, an
// ODT file (ODF 1.3). It is laid out and fails as [FromDOCX] describes.
func FromODT(src io.Reader, opts ...WordProcessingOption) *Conversion {
	return newConversion(kindODT, src, wordProcessingConfig(opts))
}

// wordProcessingConfig applies opts.
func wordProcessingConfig(opts []WordProcessingOption) convertConfig {
	var cfg convertConfig
	for _, o := range opts {
		if o != nil {
			o.applyConvert(&cfg)
		}
	}
	return cfg
}

// FromXLSX returns the conversion of src, a SpreadsheetML workbook: an XLSX
// file (ECMA-376), a macro-enabled XLSM or a template XLTX alike.
//
// The workbook is laid out as a spreadsheet application prints it: the
// sheets [WithSheets], [WithSheetNumbers] or [WithHiddenSheets] select,
// every sheet that is not hidden without them, in book order, each cut into
// pages by its page setup (paper, orientation, margins, scale or
// fit-to-pages, page order, print ranges, repeated rows and columns, manual
// breaks), with its headers and footers, the cell values formatted by their
// number formats and drawn with their fonts, fills, borders, alignment,
// wrapping and rotation, merged cells, conditional formats, pictures,
// charts and hyperlinks. A formula shows the value its file stores unless
// [WithFormulaRecalculation] computes it again. Macros never run. A
// construct that is not drawn is reported in [models.ConvertReport.Warnings].
//
// A sheet prints, as Excel prints it, every page of its used range or
// print range in its page order up to the last that holds something, the
// blank pages between included. When those pages of the workbook pass
// [models.Limits.MaxConvertPages], the pages that hold nothing are left
// out (a page holds something when one of its own cells holds a value, a
// border, a fill or an empty string, a drawing overlaps it, or a text
// runs into it from the left; the repeated rows and columns it shows do
// not count), the pages kept number and count among themselves, and the
// report holds one [models.WarningLimitReached] saying how many were left
// out. A workbook whose pages that hold something pass the limit by
// themselves fails with [ErrConvertTooManyPages]. An OpenDocument
// spreadsheet prints the pages that hold something alone, as LibreOffice
// Calc does, and fails past the limit.
//
// The notes of a sheet's cells print where its page setup asks, as its
// application prints them, and nowhere when it asks for none, the
// default. A workbook's sheet that prints them at the end (cellComments
// atEnd, ECMA-376 Part 1, 18.18.5) prints, after its pages, each note's
// cell and text row by row under the labels Cell and Comment; one that
// prints them as displayed (asDisplayed) draws each note the sheet shows
// in its box beside its cell, with the arrow that points at the cell. A
// threaded comment prints the text of the note its workbook writes for
// it. An OpenDocument spreadsheet whose page style prints annotations
// (style:print, ODF 1.3 Part 3, 20.325) prints, after each sheet's pages,
// each note's cell and text column by column, as LibreOffice Calc prints
// them; Calc prints no note on the sheet itself.
//
// A source that is not a workbook of its format fails with
// [ErrConvertMalformed], an encrypted one [WithOfficePassword] does not
// open with [ErrConvertEncrypted].
func FromXLSX(src io.Reader, opts ...SpreadsheetOption) *Conversion {
	return newConversion(kindXLSX, src, spreadsheetConfig(opts))
}

// FromODS returns the conversion of src, an OpenDocument spreadsheet: an
// ODS file or a flat FODS document (ODF 1.3). It is laid out and fails as
// [FromXLSX] describes.
func FromODS(src io.Reader, opts ...SpreadsheetOption) *Conversion {
	return newConversion(kindODS, src, spreadsheetConfig(opts))
}

// spreadsheetConfig applies opts.
func spreadsheetConfig(opts []SpreadsheetOption) convertConfig {
	var cfg convertConfig
	for _, o := range opts {
		if o != nil {
			o.applyConvert(&cfg)
		}
	}
	return cfg
}

// FromCSV returns the conversion of src, a comma-separated values file (RFC
// 4180) in UTF-8.
//
// The file is laid out as a spreadsheet of one sheet named Sheet1: its
// fields split at the separators of [WithCSVSeparators], double quotes
// enclosing a field that holds a separator, a quote or a line break, and,
// unless [WithCSVTypeDetection] turns it off, a field that reads as a
// number, a date or a time laid out as one, quoted or not.
// The sheet prints on the paper of [WithPaper], else of [WithDefaultPaper],
// with a page footer and no header.
func FromCSV(src io.Reader, opts ...CSVOption) *Conversion {
	var cfg convertConfig
	for _, o := range opts {
		if o != nil {
			o.applyConvert(&cfg)
		}
	}
	return newConversion(kindCSV, src, cfg)
}

// FromPPTX returns the conversion of src, a PresentationML presentation: a
// PPTX file (ECMA-376), a macro-enabled PPTM, a template POTX or a slide
// show PPSX alike.
//
// The presentation is laid out as a presentation prints: one page per slide
// at the slide size, the slide's background and its shapes in stacking
// order, the masters' and layouts' shapes that show through included, each
// shape its geometry with its fill and line, its picture, chart or table,
// its text laid out in its box with the insets, anchor, wrapping, columns
// and autofit the file asks for, bullets and numbering, and the slide
// number, date and footer fields. Hidden slides are left out unless
// [WithHiddenSlides] asks for them; [WithNotesPages] adds a notes page per
// slide, and [WithHandouts] prints handout pages in place of the slides.
// Transitions, animations, narration and comments are not printed. Macros
// never run. A construct that is not drawn is reported in
// [models.ConvertReport.Warnings].
//
// A source that is not a presentation of its format fails with
// [ErrConvertMalformed], an encrypted one [WithOfficePassword] does not
// open with [ErrConvertEncrypted].
func FromPPTX(src io.Reader, opts ...PresentationOption) *Conversion {
	return newConversion(kindPPTX, src, presentationConfig(opts))
}

// FromODP returns the conversion of src, an OpenDocument presentation: an
// ODP file, a template OTP or a flat FODP document (ODF 1.3). It is laid
// out and fails as [FromPPTX] describes.
func FromODP(src io.Reader, opts ...PresentationOption) *Conversion {
	return newConversion(kindODP, src, presentationConfig(opts))
}

// presentationConfig applies opts.
func presentationConfig(opts []PresentationOption) convertConfig {
	var cfg convertConfig
	for _, o := range opts {
		if o != nil {
			o.applyConvert(&cfg)
		}
	}
	return cfg
}

// FromImage returns the conversion of src, an image file: PNG, JPEG, GIF,
// WebP, BMP, TIFF, JPEG 2000, ICO or SVG, told from its bytes.
//
// # Pages
//
// Each picture of the file is a page: the one picture of a PNG, a JPEG, a
// BMP, a JPEG 2000 or an SVG file; every page of a TIFF file, its
// reduced-resolution images left out; the first frame of an animated GIF
// or WebP file, composed on its canvas, the rest of it transparent; the
// largest picture of an ICO file, as a viewer shows an icon.
//
// A page is its picture's size: its pixels at the resolution the file
// states (PNG pHYs, JFIF density, else EXIF resolution, TIFF resolution
// tags, BMP pixels a metre), else 96 pixels an inch, a CSS pixel; a GIF,
// a WebP, an ICO and a JPEG 2000 file state none that is read. An SVG
// page is its width and height, else its view box, in CSS pixels, else
// 300 by 150 pixels. [WithPaper] makes every page the paper, the picture
// scaled down to fit it, never up, and centred; [WithOrientation] turns
// the page and fits the picture to it. A picture always names its size,
// so [WithDefaultPaper] changes nothing. A page of the picture's size may
// pass the 14,400 points ISO 32000-1, Annex C.2 lists, as a single page
// does (see [WithSinglePage]): a side past 1,048,576 points, or 32,767
// under [WithConvertVersion] "1.4", or past 14,400 points on a paper, is
// cut to it, the picture scaled down to fit, and a side under 3 points
// grown to 3, the picture centred, each with a warning. [WithPageRanges]
// selects among the pages.
//
// # Pictures
//
//   - A JPEG is written as it is, never decoded, whatever its process,
//     progressive included, and its colour space; an Adobe CMYK file's
//     inverted samples are read inverted back.
//   - A JPEG 2000 file is written as it is from PDF 1.5 on when it holds
//     one colour specification, one or three colour channels of one depth,
//     no palette and no opacity channel; otherwise, and under PDF 1.4,
//     which has no JPXDecode filter, it is decoded and written losslessly
//     at 8 bits a sample, a deeper component rounded to 8.
//   - Every other format is decoded and written losslessly: an indexed
//     picture indexed, a gray one at the fewest bits that hold its levels,
//     16-bit samples at 16 bits, at 8 under PDF 1.4, which has no 16-bit
//     component.
//   - An alpha channel is a soft mask, which a PDF/A-1 remediation
//     flattens, as it flattens any transparency.
//   - The EXIF orientation of a JPEG, a PNG or a WebP file, and the TIFF
//     orientation, turn the picture as it is drawn, its samples untouched.
//   - An embedded ICC profile of a gray, RGB or CMYK space is kept;
//     without one, the samples are read in the device space of their count,
//     as sRGB for RGB. A profile the PDF version does not read, a version
//     4 profile under PDF 1.4, is left out with a warning. PNG gAMA and
//     cHRM chunks are not applied.
//   - An SVG file stays vector, drawn as an HTML page draws an SVG
//     picture, its text in the fonts of [WithConvertFontDirs]; its image
//     elements are left out with a warning.
//
// # Structure
//
// The structure tree holds a Document element with a Figure a page. The
// figure's description is what the format defines as one: a PNG text
// chunk of keyword Description (PNG 3rd edition, 11.3.3.2), an SVG
// root's title, else its desc (SVG 2, 5.9); else the picture's title: a
// PNG text chunk of keyword Title, the EXIF ImageDescription of a JPEG or
// a WebP file, a TIFF page's own ImageDescription. The document's title
// is what the format defines as a title: a PNG text chunk of keyword
// Title, the EXIF ImageDescription of a JPEG, a TIFF or a WebP file (EXIF
// 2.32, 4.6.5), an SVG root's title. A figure without a description
// fails a PDF/UA remediation unless [WithFigureAltText] gives it one.
//
// # Not supported
//
// HEIF, AVIF, JPEG XL, BigTIFF, cursors and gzip-compressed SVG fail with
// [ErrConvertMalformed], their message naming the format. So do a BMP of
// 1, 2, 4 or 16 bits a pixel or compressed; a JPEG-compressed, CMYK,
// YCbCr or CIE L*a*b* TIFF page; and a JPEG 2000 file of a CMYK, sYCC or
// CIE L*a*b* colour space.
//
// # Errors
//
// A source that is no image of these formats, or whose bytes do not read as
// its format, fails with [ErrConvertMalformed]. A source past
// [models.Limits.MaxConvertInputBytes], a picture past
// [models.Limits.MaxImagePixels] and decoded pixels past
// [models.Limits.MaxConvertMemoryBytes] fail with [ErrConvertInputTooLarge],
// before any of its pixels is decoded; a TIFF file of more pages than
// [models.Limits.MaxConvertPages] fails with [ErrConvertTooManyPages].
func FromImage(src io.Reader, opts ...ImageOption) *Conversion {
	var cfg convertConfig
	for _, o := range opts {
		if o != nil {
			o.applyConvert(&cfg)
		}
	}
	return newConversion(kindImage, src, cfg)
}

// WriteTo runs the conversion and writes the PDF to dst.
//
// It reads the source to its end and writes dst; it closes neither.
// Nothing reaches dst until the whole file is laid out, so a failure before
// then leaves dst as it was. The file is then copied to dst, and a failure
// during that copy, dst's own error, ctx ending or a temporary file that
// does not read back, leaves dst holding part of the file.
//
// It returns an error wrapping [ErrConversionReused] when the conversion
// already ran, [ErrNilSource] for a nil Conversion or a nil source,
// [ErrNilDestination] for a nil dst, [ErrInvalidLimits] or
// [ErrInvalidConvertOptions] for an option out of range, two options that
// exclude each other or a page range the document has no pages for,
// [ErrConvertInputTooLarge] past [models.Limits.MaxConvertInputBytes] or
// [models.Limits.MaxConvertMemoryBytes], or for an HTML document past the
// converter's element, cascade or box caps, [ErrConvertTooManyPages] past
// [models.Limits.MaxConvertPages], [ErrConvertNoFonts] when the font
// directories hold no usable font, [ErrConvertMalformed] for an Office or
// image source that is not a document of its format, [ErrConvertEncrypted] for
// one [WithOfficePassword] does not open, [ErrConvertResourceUnavailable] for
// a resource of an HTML or Markdown source that does not load under
// [WithRequiredResources], [ErrInternalPanic] for a recovered panic, and ctx's
// error when ctx ends first.
func (c *Conversion) WriteTo(ctx context.Context, dst io.Writer) (*models.ConvertReport, error) {
	return c.run(ctx, dst, convertEnv{})
}

// convertEnv is what the caller of a conversion lends it where its own
// options name none: a pipeline lends its logger and its temp directory.
type convertEnv struct {
	logger  *slog.Logger
	tempDir string
}

// run converts c's source and writes the PDF to dst (see
// [Conversion.WriteTo]).
func (c *Conversion) run(ctx context.Context, dst io.Writer, env convertEnv) (_ *models.ConvertReport, err error) {
	if c == nil {
		return nil, fmt.Errorf("converting: nil conversion: %w", ErrNilSource)
	}
	o := c.cfg
	if o.logger == nil {
		o.logger = env.logger
	}
	if o.tempDir == "" {
		o.tempDir = env.tempDir
	}
	defer recoverToError(o.logger, "Conversion.WriteTo", &err)

	name := c.kind.name()
	if c.src == nil {
		return nil, fmt.Errorf("converting %s: %w", name, ErrNilSource)
	}
	if dst == nil {
		return nil, fmt.Errorf("converting %s: %w", name, ErrNilDestination)
	}
	if err := ValidateLimits(o.limits); err != nil {
		return nil, fmt.Errorf("converting %s: %w", name, err)
	}
	if err := o.validate(); err != nil {
		return nil, fmt.Errorf("converting %s: %w", name, err)
	}
	version, err := resolveConvertVersion(o.version)
	if err != nil {
		return nil, fmt.Errorf("converting %s: %w", name, err)
	}
	if !c.used.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("converting %s: %w", name, ErrConversionReused)
	}

	budget := limits.WithDefaults(o.limits)
	ctx, mem, endBudget := limits.WithBudget(ctx, budget)
	defer endBudget()
	defer func() { err = convertBudgetError(o.logger, name, mem, err) }()
	src := &chargedReader{r: c.src, b: mem}
	// The pictures of a large document wait for the file in a temp file of
	// their own, removed on every path once the file is written.
	ctx, pictures := typeset.WithPictureStore(ctx, o.tempDir, o.spool)
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
		return nil, fmt.Errorf("converting %s: indexing fonts: %w", name, err)
	}
	if o.plainGlyphs {
		fonts = fonts.WithPlainGlyphs()
	}

	// Every converter tags its output, so the stream marks content as it
	// writes each page.
	stream, err := typeset.NewStream(ctx, typeset.Options{Logger: o.logger, Version: version, TempDir: o.tempDir, Spool: o.spool}, true)
	if err != nil {
		return nil, fmt.Errorf("converting %s: %w", name, err)
	}
	defer stream.Abort()
	selectPages(stream, &o)

	var doc *typeset.Document
	switch c.kind {
	case kindHTML, kindMarkdown:
		doc, err = convertHTML(ctx, src, c.kind, fonts, &o, version, budget.MaxConvertInputBytes, budget.MaxConvertPages)
	case kindDOCX, kindODT, kindXLSX, kindODS, kindCSV, kindPPTX, kindODP:
		doc, err = convertOffice(ctx, src, c.kind, fonts, &o, version, budget.MaxConvertInputBytes, budget.MaxConvertPages, stream)
	case kindText:
		// The text converter holds a piece of its source at a time, which
		// it charges itself.
		doc, err = convertPlainText(ctx, c.src, fonts, &o, budget.MaxConvertInputBytes, budget.MaxConvertPages, stream)
	case kindImage:
		doc, err = convertImage(ctx, src, fonts, &o, version, budget.MaxConvertInputBytes, budget.MaxConvertPages, stream)
	}
	// The other converters hold their source whole, or the tree they parse
	// it into, until they return.
	mem.Release(src.charged)
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
			return nil, fmt.Errorf("converting %s: writing the PDF: %w", name, err)
		}
	}
	tail := *doc
	tail.Pages = nil
	if err := stream.Close(ctx, &tail, dst); err != nil {
		if errors.Is(err, ErrInvalidConvertOptions) {
			// A page range the laid-out document has no pages for is refused
			// as the stream closes: an option error, not a failed write.
			return nil, fmt.Errorf("converting %s: %w", name, err)
		}
		return nil, fmt.Errorf("converting %s: writing the PDF: %w", name, err)
	}

	report := &models.ConvertReport{Pages: stream.Pages()}
	for _, w := range tail.Warnings {
		report.Warnings = append(report.Warnings, models.ConvertWarning{Code: models.ConvertWarningCode(w.Code.String()), Effect: models.ConvertWarningEffect(w.Effect.String()), Message: w.Message, Page: w.Page})
	}
	return report, nil
}

// resolveConvertVersion returns the version v names, 1.7 for "", or an
// error wrapping [ErrInvalidConvertOptions] for one a conversion does not
// write (see [WithConvertVersion]).
func resolveConvertVersion(v string) (typeset.Version, error) {
	if v == "" {
		return typeset.Version17, nil
	}
	switch version := typeset.Version(v); version {
	case typeset.Version14, typeset.Version15, typeset.Version16, typeset.Version17, typeset.Version20:
		return version, nil
	}
	return "", fmt.Errorf("%w: PDF version %q", ErrInvalidConvertOptions, v)
}

// turned returns the paper w by h turned as o's orientation asks.
func (o *convertConfig) turned(w, h float64) (float64, float64) {
	if o.orientation == models.OrientationLandscape && w < h || o.orientation == models.OrientationPortrait && w > h {
		return h, w
	}
	return w, h
}

// basePaper returns the paper the options name before any turn: the
// caller's, else the default one.
func (o *convertConfig) basePaper() (float64, float64) {
	if o.paperSet {
		return o.paper.Width, o.paper.Height
	}
	return o.fallbackPaper()
}

// fallbackPaper returns the paper of a source that names none: the one
// [WithDefaultPaper] names, else [models.PaperA4].
func (o *convertConfig) fallbackPaper() (float64, float64) {
	if o.defaultPaperSet {
		return o.defaultPaper.Width, o.defaultPaper.Height
	}
	return models.PaperA4.Width, models.PaperA4.Height
}

// margins returns the page margins: the caller's, else 2 cm a side.
func (o *convertConfig) margins() (top, right, bottom, left float64) {
	if o.marginsSet {
		return o.marginTop, o.marginRight, o.marginBottom, o.marginLeft
	}
	d := plaintext.DefaultMargin
	return d, d, d, d
}

// convertPlainText lays a text source out, handing its pages to sink as it
// makes them. Its errors wrap the public sentinels themselves.
func convertPlainText(ctx context.Context, src io.Reader, fonts *text.Fonts, o *convertConfig, maxBytes int64, maxPages int, sink typeset.PageSink) (*typeset.Document, error) {
	w, h := o.turned(o.basePaper())
	top, right, bottom, left := o.margins()
	doc, err := plaintext.Convert(ctx, src, fonts, plaintext.Options{
		PageWidth:     w,
		PageHeight:    h,
		MarginTop:     top,
		MarginRight:   right,
		MarginBottom:  bottom,
		MarginLeft:    left,
		FontFamilies:  o.families,
		FontSize:      o.fontSize,
		LineHeight:    o.lineHeight,
		TabWidth:      o.tabWidth,
		MaxInputBytes: maxBytes,
		MaxPages:      maxPages,
		DefaultLang:   o.defaultLang,
		Sink:          sink,
	})
	if err != nil {
		// The converter's errors wrap the public sentinels themselves.
		err = fmt.Errorf("converting text: %w", err)
	}
	return doc, err
}

// convertHTML lays an HTML or Markdown source out. Its errors wrap the
// public sentinels themselves: its option errors
// [ErrInvalidConvertOptions], every cap on the document's size or
// complexity [ErrConvertInputTooLarge], its page cap
// [ErrConvertTooManyPages].
func convertHTML(ctx context.Context, src io.Reader, kind convertKind, fonts *text.Fonts, o *convertConfig, version typeset.Version, maxBytes int64, maxPages int) (*typeset.Document, error) {
	// The converter takes the paper as a portrait sheet and a landscape
	// switch, which also widens a single page. A paper the caller neither
	// turns nor names portrait goes as it is.
	w, h := o.basePaper()
	landscape := o.orientation == models.OrientationLandscape
	if o.orientation != models.OrientationSource && w > h {
		w, h = h, w
	}
	top, right, bottom, left := o.margins()
	opts := htmlconv.Options{
		PageWidth:     w,
		PageHeight:    h,
		MarginTop:     top,
		MarginRight:   right,
		MarginBottom:  bottom,
		MarginLeft:    left,
		Media:         o.media,
		Resources:     o.resources,
		MaxInputBytes: maxBytes,
		MaxPages:      maxPages,
		Logger:        o.logger,

		Landscape: landscape,
		// Without a caller's paper, the document's @page size is the
		// page (CSS Paged Media 3, 7.1): the default paper only stands in
		// for a page that names none. A single page keeps the paper it
		// measures.
		PreferCSSPageSize:     !o.paperSet && !o.singlePage,
		Scale:                 o.scale,
		SinglePage:            o.singlePage,
		FittedLimit:           typeset.FittedPageLimit(version),
		Version:               version,
		OmitBackgrounds:       o.omitCanvasBackground,
		OmitOutline:           o.omitOutline,
		NoShrinkToFit:         o.noShrinkToFit,
		AnimationsAtEnd:       o.finishedAnimations,
		FractionalFontMetrics: o.fractionalFontMetrics,
		Bands:                 o.htmlBands(),
		PageCounterStart:      o.pageCounterStart(),
		BaseURL:               o.baseURL,
		HTTPClient:            o.httpClient,
		MediaFeatures:         o.mediaFeatures,
		DefaultLang:           o.defaultLang,
		UserStylesheets:       o.userStylesheets,
		DerivedAltText:        o.derivedAltText,
		RequiredResources:     o.requiredResources,
	}
	name := kind.name()
	var doc *typeset.Document
	var err error
	if kind == kindMarkdown {
		doc, err = markdown.Convert(ctx, src, fonts, markdown.Options{HTML: opts, Template: o.markdownTemplate})
	} else {
		doc, err = htmlconv.Convert(ctx, src, fonts, opts)
	}
	if err != nil {
		// The converter's errors wrap the public sentinels themselves.
		return nil, fmt.Errorf("converting %s: %w", name, err)
	}
	return doc, nil
}

// convertOffice lays an Office source out, handing its pages to sink as a
// converter that streams makes them. Its errors wrap the public sentinels
// themselves: its option errors [ErrInvalidConvertOptions], its size caps
// [ErrConvertInputTooLarge], its page cap [ErrConvertTooManyPages], a
// package that is not one [ErrConvertMalformed], an encrypted one
// [ErrConvertEncrypted].
func convertOffice(ctx context.Context, src io.Reader, kind convertKind, fonts *text.Fonts, o *convertConfig, version typeset.Version, maxBytes int64, maxPages int, sink typeset.PageSink) (*typeset.Document, error) {
	name := kind.name()
	var conv func(context.Context, io.Reader, *text.Fonts, office.Options) (*typeset.Document, error)
	switch kind {
	case kindDOCX:
		conv = docx.Convert
	case kindODT:
		conv = odt.Convert
	case kindXLSX:
		conv = xlsx.Convert
	case kindODS:
		conv = ods.Convert
	case kindCSV:
		conv = csv.Convert
	case kindPPTX:
		conv = pptx.Convert
	case kindODP:
		conv = odp.Convert
	case kindText, kindHTML, kindMarkdown, kindImage:
		// run lays these kinds out without an Office converter.
		return nil, fmt.Errorf("converting %s: no Office converter reads it", name)
	}
	dw, dh := o.fallbackPaper()
	opts := office.Options{
		Landscape:         o.orientation == models.OrientationLandscape,
		Portrait:          o.orientation == models.OrientationPortrait,
		DefaultPageWidth:  dw,
		DefaultPageHeight: dh,
		Password:          o.password,
		OmitOutline:       o.omitOutline,
		SinglePageSheets:  o.onePagePerSheet,
		SinglePage:        o.singlePage,
		FittedLimit:       typeset.FittedPageLimit(version),
		PrintDate:         o.date,
		TrackedChanges:    o.trackedChanges,
		Comments:          o.comments,
		FormFields:        o.formFields,
		HiddenSheets:      o.hiddenSheets,
		Recalculation:     o.recalculation,
		CSVKeepText:       o.csvKeepText,
		Locale:            o.locale,
		HiddenSlides:      o.hiddenSlides,
		NotesPages:        o.notesPages,
		Handouts:          o.handouts,
		DefaultLang:       o.defaultLang,
		DerivedAltText:    o.derivedAltText,
		MaxInputBytes:     maxBytes,
		MaxPages:          maxPages,
		Logger:            o.logger,
		Sink:              sink,
	}
	if o.paperSet {
		opts.PageWidth, opts.PageHeight = o.paper.Width, o.paper.Height
	}
	if o.sheetsSet {
		opts.Sheets = o.sheets
	}
	if o.sheetNumbersSet {
		opts.SheetNumbers = o.sheetNumbers
	}
	if o.csvSeparatorSet {
		opts.CSVSeparators = o.csvSeparators
	}
	doc, err := conv(ctx, src, fonts, opts)
	if err != nil {
		// The converters' errors wrap the public sentinels themselves.
		return nil, fmt.Errorf("converting %s: %w", name, err)
	}
	return doc, nil
}

// convertImage lays an image source out, handing its pages to sink as it
// makes them. Its errors wrap the public sentinels themselves: a source
// that is no image of a format litho reads [ErrConvertMalformed], its size
// caps and a picture past the pixel cap [ErrConvertInputTooLarge], its
// page cap [ErrConvertTooManyPages].
func convertImage(ctx context.Context, src io.Reader, fonts *text.Fonts, o *convertConfig, version typeset.Version, maxBytes int64, maxPages int, sink typeset.PageSink) (*typeset.Document, error) {
	opts := picture.Options{
		Landscape:     o.orientation == models.OrientationLandscape,
		Portrait:      o.orientation == models.OrientationPortrait,
		Version:       version,
		DefaultLang:   o.defaultLang,
		MaxInputBytes: maxBytes,
		MaxPages:      maxPages,
		Logger:        o.logger,
		Sink:          sink,
	}
	if o.paperSet {
		opts.PageWidth, opts.PageHeight = o.paper.Width, o.paper.Height
	}
	doc, err := picture.Convert(ctx, src, fonts, opts)
	if err != nil {
		// The converter's errors wrap the public sentinels themselves.
		return nil, fmt.Errorf("converting image: %w", err)
	}
	return doc, nil
}

// selectPages makes stream write the pages that o's page range
// specification names, resolved against the pages the document lays out on
// with [ParsePageRanges], in the order it lists them; an empty
// specification keeps every page. A specification [ParsePageRanges] refuses
// fails the stream with [ErrInvalidConvertOptions] wrapping its error.
func selectPages(stream *typeset.Stream, o *convertConfig) {
	if o.pageRanges == "" {
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
