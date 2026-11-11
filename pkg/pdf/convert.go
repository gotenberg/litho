package pdf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"

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
	}
	return "source"
}

// Conversion is one source document and how it is laid out as a PDF. A
// constructor makes it, one per format the conversion reads: [FromText],
// [FromHTML], [FromMarkdown], [FromDOCX], [FromODT], [FromXLSX], [FromODS],
// [FromCSV], [FromPPTX] and [FromODP]. Each takes the options of its format
// alone, so an option a format has no use for does not compile.
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
//     size of a presentation.
//  3. A source that names none takes [WithDefaultPaper], else [PaperA4].
//
// [WithOrientation] then turns the pages that are not turned as it asks.
//
// # Output
//
// The file is tagged PDF (ISO 32000-1, 14.8) of the version
// [WithConvertVersion] names, 1.7 by default. Its fonts are embedded, each
// subset to the glyphs drawn. It claims no conformance: remediate it in a
// later pipeline stage to conform to a PDF/A or PDF/UA profile. Identical
// source bytes, options and font files write identical bytes.
//
// # Memory
//
// A conversion spends memory within [Limits.MaxConvertMemoryBytes] and
// fails with [ErrConvertInputTooLarge] past it. A text source, a
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
// [WithMediaType]. No script runs.
//
//   - Layout covers the visual formatting model of CSS 2.1, flexbox, grid
//     without subgrid, multi-column layout, positioning, transforms,
//     generated content and counters, and bidirectional and CJK text.
//   - Paged media covers @page rules with named pages and page-margin
//     boxes (CSS Paged Media 3), and the footnotes, leaders, running
//     elements and string sets of CSS Generated Content for Paged Media.
//   - Stylesheets, images and fonts are read from [WithResources], or
//     fetched through [WithHTTPClient] for http and https URLs. PNG, JPEG,
//     GIF, WebP, BMP, TIFF and SVG images are drawn, a JPEG turned as its
//     EXIF orientation says; an image that cannot be read is replaced by
//     its alt text. @font-face reads TrueType, OpenType, WOFF and WOFF2.
//   - Form controls draw as the text they show. Embedded documents and
//     media are left out.
//
// A construct that is not drawn as the document means it is reported in
// [ConvertReport.Warnings].
//
// Each element maps to the standard structure type of its meaning (ISO
// 32000-1, 14.8.4), images carry their alt text, and the root element's
// lang is the document's language. Links become Link annotations, h1 to h6
// the outline, and <title> and <meta name> author, description and
// keywords the document information.
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
// its common extensions (tables, fenced code, autolinks, strikethrough,
// definition lists, heading identifiers, among others) and placed in the
// template of [WithMarkdownTemplate]. That document is laid out as
// [FromHTML] lays one out, with every option of an HTML source: its
// relative images and links resolve in [WithResources] or against
// [WithBaseURL]. The raw HTML a Markdown document holds is kept,
// unsanitised: no script runs. [Limits.MaxConvertInputBytes] caps the
// Markdown, not the HTML it renders to.
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
// out. Macros never run; an embedded object draws as its preview picture
// when the package holds one. A construct that is not drawn is reported in
// [ConvertReport.Warnings].
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
// [WithFormulaRecalculation] computes it again. Cell comments are left out.
// Macros never run. A construct that is not drawn is reported in
// [ConvertReport.Warnings].
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
// fields split at the separators of [WithCSVSeparators], a field quoted
// with double quotes read as text, and, unless [WithCSVTypeDetection] turns
// it off, a field that reads as a number, a date or a time laid out as one.
// The sheet prints on the paper of [WithDefaultPaper] with a page footer
// and no header.
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
// [ConvertReport.Warnings].
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

// ConvertReport is what a [Conversion] reports beside the PDF.
// Concurrent reads of a value are safe; a write needs synchronization.
type ConvertReport struct {
	// Pages is the number of pages written.
	Pages int
	// Warnings lists what the conversion could not render as the source
	// meant it, in the order met.
	Warnings []ConvertWarning
}

// ConvertWarning is one construct of the source a [Conversion] did not
// render faithfully.
// Concurrent reads of a value are safe; a write needs synchronization.
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
	// picture, "unsupported-element" and "form-control".
	Code string
	// Message explains the construct and what was done instead.
	Message string
	// Page is the 1-based page the construct lands on, 0 when it has none.
	Page int
}

// WriteTo runs the conversion and writes the PDF to dst.
//
// It reads the source to its end and writes dst; it closes neither. On an
// error other than a write error on dst, nothing reaches dst: the whole
// file is built before it is written.
//
// It returns an error wrapping [ErrConversionReused] when the conversion
// already ran, [ErrNilSource] for a nil source, [ErrInvalidLimits] or
// [ErrInvalidConvertOptions] for an option out of range, two options that
// exclude each other or a page range the document has no pages for,
// [ErrConvertInputTooLarge] past [Limits.MaxConvertInputBytes] or
// [Limits.MaxConvertMemoryBytes], or for an HTML document past the
// converter's element, cascade or box caps, [ErrConvertTooManyPages] past
// [Limits.MaxConvertPages], [ErrConvertNoFonts] when the font directories
// hold no usable font, [ErrConvertMalformed] for an Office source that is
// not a document of its format, [ErrConvertEncrypted] for one
// [WithOfficePassword] does not open, [ErrInternalPanic] for a recovered
// panic, and ctx's error when ctx ends first.
func (c *Conversion) WriteTo(ctx context.Context, dst io.Writer) (*ConvertReport, error) {
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
func (c *Conversion) run(ctx context.Context, dst io.Writer, env convertEnv) (_ *ConvertReport, err error) {
	if c == nil {
		return nil, fmt.Errorf("converting: %w: nil conversion", ErrInvalidConvertOptions)
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
		return nil, fmt.Errorf("converting %s: %w: nil destination", name, ErrInvalidConvertOptions)
	}
	if err := o.limits.Validate(); err != nil {
		return nil, err
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

	budget := o.limits.internal().WithDefaults()
	ctx, mem, endBudget := withConvertBudget(ctx, budget)
	defer endBudget()
	defer func() { err = convertBudgetError(o.logger, mem, err) }()
	src := chargedSource(c.src, mem)
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
		return nil, fmt.Errorf("converting %s: indexing fonts: %w", name, err)
	}

	// Every converter tags its output, so the stream marks content as it
	// writes each page.
	stream, err := compose.NewStream(ctx, compose.Options{Logger: o.logger, Version: version, TempDir: o.tempDir, Spool: o.spool}, true)
	if err != nil {
		return nil, fmt.Errorf("converting %s: %w", name, err)
	}
	defer stream.Abort()
	selectPages(stream, &o)

	var doc *compose.Document
	switch c.kind {
	case kindHTML, kindMarkdown:
		doc, err = convertHTML(ctx, src, c.kind, fonts, &o, budget.MaxConvertInputBytes, budget.MaxConvertPages)
	case kindDOCX, kindODT, kindXLSX, kindODS, kindCSV, kindPPTX, kindODP:
		doc, err = convertOffice(ctx, src, c.kind, fonts, &o, budget.MaxConvertInputBytes, budget.MaxConvertPages, stream)
	case kindText:
		doc, err = convertPlainText(ctx, src, fonts, &o, budget.MaxConvertInputBytes, budget.MaxConvertPages, stream)
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
			return nil, fmt.Errorf("converting %s: writing the PDF: %w", name, err)
		}
	}
	tail := *doc
	tail.Pages = nil
	if err := stream.Close(ctx, &tail, dst); err != nil {
		return nil, fmt.Errorf("converting %s: writing the PDF: %w", name, err)
	}

	report := &ConvertReport{Pages: stream.Pages()}
	for _, w := range tail.Warnings {
		report.Warnings = append(report.Warnings, ConvertWarning{Code: w.Code, Message: w.Message, Page: w.Page})
	}
	return report, nil
}

// resolveConvertVersion returns the version v names, 1.7 for "", or an
// error wrapping [ErrInvalidConvertOptions] for one a conversion does not
// write (see [WithConvertVersion]).
func resolveConvertVersion(v string) (compose.Version, error) {
	if v == "" {
		return compose.Version17, nil
	}
	switch version := compose.Version(v); version {
	case compose.Version14, compose.Version15, compose.Version16, compose.Version17, compose.Version20:
		return version, nil
	}
	return "", fmt.Errorf("%w: PDF version %q", ErrInvalidConvertOptions, v)
}

// turned returns the paper w by h turned as o's orientation asks.
func (o *convertConfig) turned(w, h float64) (float64, float64) {
	if o.orientation == OrientationLandscape && w < h || o.orientation == OrientationPortrait && w > h {
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
// [WithDefaultPaper] names, else [PaperA4].
func (o *convertConfig) fallbackPaper() (float64, float64) {
	if o.defaultPaper != (Paper{}) {
		return o.defaultPaper.Width, o.defaultPaper.Height
	}
	return PaperA4.Width, PaperA4.Height
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
// makes them, mapping the text converter's errors onto the public
// sentinels.
func convertPlainText(ctx context.Context, src io.Reader, fonts *text.Fonts, o *convertConfig, maxBytes int64, maxPages int, sink compose.PageSink) (*compose.Document, error) {
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
	switch {
	case errors.Is(err, plaintext.ErrInvalidOptions):
		err = fmt.Errorf("converting text: %w: %w", ErrInvalidConvertOptions, err)
	case errors.Is(err, plaintext.ErrInputTooLarge):
		err = fmt.Errorf("converting text: %w: %w", ErrConvertInputTooLarge, err)
	case err != nil:
		err = fmt.Errorf("converting text: %w", err)
	}
	return doc, err
}

// convertHTML lays an HTML or Markdown source out, mapping the HTML
// converter's errors onto the public sentinels: its option errors to
// [ErrInvalidConvertOptions], every cap on the document's size or
// complexity to [ErrConvertInputTooLarge], its page cap to
// [ErrConvertTooManyPages].
func convertHTML(ctx context.Context, src io.Reader, kind convertKind, fonts *text.Fonts, o *convertConfig, maxBytes int64, maxPages int) (*compose.Document, error) {
	// The converter takes the paper as a portrait sheet and a landscape
	// switch, which also widens a single page. A paper the caller neither
	// turns nor names portrait goes as it is.
	w, h := o.basePaper()
	landscape := o.orientation == OrientationLandscape
	if o.orientation != OrientationSource && w > h {
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
		OmitBackgrounds:       o.omitCanvasBackground,
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
		UserStylesheets:       o.userStylesheets,
	}
	name := kind.name()
	var doc *compose.Document
	var err error
	if kind == kindMarkdown {
		doc, err = markdown.Convert(ctx, src, fonts, markdown.Options{HTML: opts, Template: o.markdownTemplate})
	} else {
		doc, err = htmlconv.Convert(ctx, src, fonts, opts)
	}
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

// convertOffice lays an Office source out, handing its pages to sink as a
// converter that streams makes them, mapping the Office converters' errors
// onto the public sentinels: their option errors to
// [ErrInvalidConvertOptions], their size caps to [ErrConvertInputTooLarge],
// their page cap to [ErrConvertTooManyPages], a package that is not one to
// [ErrConvertMalformed], an encrypted one to [ErrConvertEncrypted].
func convertOffice(ctx context.Context, src io.Reader, kind convertKind, fonts *text.Fonts, o *convertConfig, maxBytes int64, maxPages int, sink compose.PageSink) (*compose.Document, error) {
	conv := docx.Convert
	switch kind {
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
	case kindDOCX, kindText, kindHTML, kindMarkdown:
		// DOCX is the default; the others never reach here.
	}
	dw, dh := o.fallbackPaper()
	opts := office.Options{
		Landscape:         o.orientation == OrientationLandscape,
		Portrait:          o.orientation == OrientationPortrait,
		DefaultPageWidth:  dw,
		DefaultPageHeight: dh,
		Password:          o.password,
		OmitOutline:       o.omitOutline,
		SinglePageSheets:  o.onePagePerSheet,
		PrintDate:         o.date,
		TrackedChanges:    office.TrackedChanges(o.trackedChanges),
		HiddenSheets:      o.hiddenSheets,
		Recalculation:     office.Recalculation(o.recalculation),
		CSVKeepText:       o.csvKeepText,
		HiddenSlides:      o.hiddenSlides,
		NotesPages:        o.notesPages,
		Handouts:          o.handouts,
		DefaultLang:       o.defaultLang,
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
	name := kind.name()
	doc, err := conv(ctx, src, fonts, opts)
	switch {
	case err == nil:
		return doc, nil
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
// specification names, resolved against the pages the document lays out on
// with [ParsePageRanges], in the order it lists them; an empty
// specification keeps every page. A specification [ParsePageRanges] refuses
// fails the stream with [ErrInvalidConvertOptions] wrapping its error.
func selectPages(stream *compose.Stream, o *convertConfig) {
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
