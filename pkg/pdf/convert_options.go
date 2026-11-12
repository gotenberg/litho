package pdf

import (
	"io/fs"
	"log/slog"
	"net/http"
	"slices"
	"time"
)

// Paper is a page size in points, 1/72 inch: 25.4 mm is 72 points.
type Paper struct {
	// Width and Height are the sides in points. A paper wider than it is
	// high is a landscape paper.
	Width, Height float64
}

// The papers a conversion names most, each portrait. Their sides are the
// ones ISO 216 and ANSI/ASME Y14.1 give them, to a thousandth of a point.
// Any other size is a [Paper] literal.
var (
	// PaperA3 is ISO 216 A3, 297 by 420 mm.
	PaperA3 = Paper{Width: 841.890, Height: 1190.551}
	// PaperA4 is ISO 216 A4, 210 by 297 mm, the paper of a source that names
	// none unless [WithDefaultPaper] names another.
	PaperA4 = Paper{Width: 595.276, Height: 841.890}
	// PaperA5 is ISO 216 A5, 148 by 210 mm.
	PaperA5 = Paper{Width: 419.528, Height: 595.276}
	// PaperB5 is ISO 216 B5, 176 by 250 mm. The JIS B5 a spreadsheet often
	// names is larger, 182 by 257 mm.
	PaperB5 = Paper{Width: 498.898, Height: 708.661}
	// PaperLetter is North American Letter, 8.5 by 11 inches.
	PaperLetter = Paper{Width: 612, Height: 792}
	// PaperLegal is North American Legal, 8.5 by 14 inches.
	PaperLegal = Paper{Width: 612, Height: 1008}
	// PaperTabloid is North American Tabloid, 11 by 17 inches.
	PaperTabloid = Paper{Width: 792, Height: 1224}
	// PaperExecutive is North American Executive, 7.25 by 10.5 inches.
	PaperExecutive = Paper{Width: 522, Height: 756}
)

// Orientation is the way the pages of a conversion are turned.
type Orientation int

const (
	// OrientationSource keeps each page turned as its paper or its source
	// names it. It is the zero value and the default.
	OrientationSource Orientation = iota
	// OrientationPortrait turns every page that is wider than it is high.
	OrientationPortrait
	// OrientationLandscape turns every page that is higher than it is wide.
	OrientationLandscape
)

// TrackedChanges is how the changes a DOCX or ODT source tracks are printed.
type TrackedChanges int

const (
	// TrackedChangesAccepted prints the text as it reads once every change
	// is accepted: insertions kept, deletions left out. It is the zero value
	// and the default.
	TrackedChangesAccepted TrackedChanges = iota
	// TrackedChangesRejected prints the text as it read before the changes:
	// insertions left out, deletions kept. A change of formatting keeps its
	// new formatting.
	TrackedChangesRejected
	// TrackedChangesMarked prints both, each marked: an insertion
	// underlined, a deletion struck through, in the colour of its author,
	// taken in turn from nine colours in the order the authors first appear.
	// Change bars are not drawn, and a change of formatting is not marked.
	TrackedChangesMarked
)

// Recalculation is when the formulas of an XLSX or ODS source are computed
// again rather than shown as the value the file stores.
type Recalculation int

const (
	// RecalculateAsNeeded computes a formula again when the file gives no
	// reason to trust its stored value: every formula of an ODS source; in
	// an XLSX source, a formula without a stored value, and every formula of
	// a workbook that neither Microsoft Excel nor LibreOffice wrote and none
	// of whose formulas stores a number other than 0. It is the zero value
	// and the default.
	RecalculateAsNeeded Recalculation = iota
	// RecalculateNever shows the value the file stores for every formula,
	// and nothing for a formula without one.
	RecalculateNever
	// RecalculateAlways computes every formula again. A formula litho does
	// not compute keeps its stored value, and so does a cell that shares
	// the formula of another.
	RecalculateAlways
)

// convertConfig collects what the options of a [Conversion] set.
type convertConfig struct {
	logger   *slog.Logger
	fontDirs []string
	limits   Limits
	tempDir  string

	paper        Paper
	paperSet     bool
	defaultPaper Paper
	orientation  Orientation
	pageRanges   string
	omitOutline  bool
	date         time.Time
	defaultLang  string
	version      string

	marginTop, marginRight, marginBottom, marginLeft float64
	marginsSet                                       bool

	families      []string
	fontSize      float64
	fontSizeSet   bool
	lineHeight    float64
	lineHeightSet bool
	tabWidth      int
	tabWidthSet   bool

	media                 string
	resources             fs.FS
	baseURL               string
	httpClient            *http.Client
	mediaFeatures         map[string]string
	scale                 float64
	scaleSet              bool
	noShrinkToFit         bool
	omitCanvasBackground  bool
	headerTemplate        string
	footerTemplate        string
	singlePage            bool
	fractionalFontMetrics bool
	userStylesheets       []string

	markdownTemplate    string
	markdownTemplateSet bool

	password        string
	trackedChanges  TrackedChanges
	formFields      bool
	onePagePerSheet bool
	sheets          []string
	sheetsSet       bool
	sheetNumbers    []int
	sheetNumbersSet bool
	hiddenSheets    bool
	recalculation   Recalculation
	csvSeparators   []rune
	csvSeparatorSet bool
	csvKeepText     bool
	hiddenSlides    bool
	notesPages      bool
	handouts        int

	// spool is compose.Options.Spool and the hold of the conversion's
	// compose.PictureStore: zero in every public use, set by tests to
	// force the spool files on or off.
	spool int64
}

// TextOption tunes a conversion [FromText] makes. Every [ConvertOption] and
// every [MarginOption] is one too.
type TextOption interface {
	applyConvert(c *convertConfig)
	textOption()
}

// MarkdownOption tunes a conversion [FromMarkdown] makes. Every
// [ConvertOption], every [MarginOption] and every [HTMLOption] is one too:
// a Markdown source is laid out as the HTML it renders to.
type MarkdownOption interface {
	applyConvert(c *convertConfig)
	markdownOption()
}

// HTMLOption tunes a conversion [FromHTML] or [FromMarkdown] makes. Every
// [ConvertOption] and every [MarginOption] is one too.
type HTMLOption interface {
	MarkdownOption
	htmlOption()
}

// MarginOption is an option of the sources that carry no page margins of
// their own: [FromText], [FromHTML] and [FromMarkdown] take it.
type MarginOption interface {
	TextOption
	HTMLOption
}

// WordProcessingOption tunes a conversion [FromDOCX] or [FromODT] makes.
// Every [ConvertOption] and every [OfficeOption] is one too.
type WordProcessingOption interface {
	applyConvert(c *convertConfig)
	wordProcessingOption()
}

// SpreadsheetOption tunes a conversion [FromXLSX] or [FromODS] makes. Every
// [ConvertOption], every [OfficeOption] and every [SheetOption] is one too.
type SpreadsheetOption interface {
	applyConvert(c *convertConfig)
	spreadsheetOption()
}

// CSVOption tunes a conversion [FromCSV] makes. Every [ConvertOption] and
// every [SheetOption] is one too.
type CSVOption interface {
	applyConvert(c *convertConfig)
	csvOption()
}

// SheetOption is an option of the sources laid out as sheets: [FromXLSX],
// [FromODS] and [FromCSV] take it.
type SheetOption interface {
	SpreadsheetOption
	CSVOption
}

// PresentationOption tunes a conversion [FromPPTX] or [FromODP] makes.
// Every [ConvertOption] and every [OfficeOption] is one too.
type PresentationOption interface {
	applyConvert(c *convertConfig)
	presentationOption()
}

// OfficeOption is an option of the sources that are Office packages:
// [FromDOCX], [FromODT], [FromXLSX], [FromODS], [FromPPTX] and [FromODP]
// take it.
type OfficeOption interface {
	WordProcessingOption
	SpreadsheetOption
	PresentationOption
}

// ConvertOption is an option every conversion takes: each of [FromText],
// [FromHTML], [FromMarkdown], [FromDOCX], [FromODT], [FromXLSX], [FromODS],
// [FromCSV], [FromPPTX] and [FromODP] accepts it.
type ConvertOption interface {
	MarginOption
	OfficeOption
	SheetOption
}

// The option values, one type per set of sources that take it. Each is the
// function that sets its fields; the marker methods name the sources.
type (
	convertOption        func(*convertConfig)
	marginOption         func(*convertConfig)
	textOption           func(*convertConfig)
	htmlOption           func(*convertConfig)
	markdownOption       func(*convertConfig)
	officeOption         func(*convertConfig)
	wordProcessingOption func(*convertConfig)
	sheetOption          func(*convertConfig)
	spreadsheetOption    func(*convertConfig)
	csvOption            func(*convertConfig)
	presentationOption   func(*convertConfig)
)

func (f convertOption) applyConvert(c *convertConfig) { f(c) }
func (convertOption) textOption()                     {}
func (convertOption) htmlOption()                     {}
func (convertOption) markdownOption()                 {}
func (convertOption) wordProcessingOption()           {}
func (convertOption) spreadsheetOption()              {}
func (convertOption) csvOption()                      {}
func (convertOption) presentationOption()             {}

func (f marginOption) applyConvert(c *convertConfig) { f(c) }
func (marginOption) textOption()                     {}
func (marginOption) htmlOption()                     {}
func (marginOption) markdownOption()                 {}

func (f textOption) applyConvert(c *convertConfig) { f(c) }
func (textOption) textOption()                     {}

func (f htmlOption) applyConvert(c *convertConfig) { f(c) }
func (htmlOption) htmlOption()                     {}
func (htmlOption) markdownOption()                 {}

func (f markdownOption) applyConvert(c *convertConfig) { f(c) }
func (markdownOption) markdownOption()                 {}

func (f officeOption) applyConvert(c *convertConfig) { f(c) }
func (officeOption) wordProcessingOption()           {}
func (officeOption) spreadsheetOption()              {}
func (officeOption) presentationOption()             {}

func (f wordProcessingOption) applyConvert(c *convertConfig) { f(c) }
func (wordProcessingOption) wordProcessingOption()           {}

func (f sheetOption) applyConvert(c *convertConfig) { f(c) }
func (sheetOption) spreadsheetOption()              {}
func (sheetOption) csvOption()                      {}

func (f spreadsheetOption) applyConvert(c *convertConfig) { f(c) }
func (spreadsheetOption) spreadsheetOption()              {}

func (f csvOption) applyConvert(c *convertConfig) { f(c) }
func (csvOption) csvOption()                      {}

func (f presentationOption) applyConvert(c *convertConfig) { f(c) }
func (presentationOption) presentationOption()             {}

// WithPaper lays every page out on paper, whatever the source names: the
// paper replaces the size and the orientation of an HTML document's @page
// rules, of each section of a text document, of each sheet, and
// [WithOrientation] then turns it. Each side must be finite, positive and at
// most 14,400 points (ISO 32000-1, Annex C.2), or the conversion fails with
// [ErrInvalidConvertOptions].
//
//   - [FromHTML], [FromMarkdown]: a page whose @page size differs is laid
//     out at that size and drawn scaled down to fit the paper, centred, the
//     paper turned to the page's orientation.
//   - [FromDOCX], [FromODT], [FromXLSX], [FromODS], [FromCSV]: every section
//     or sheet is laid out again on the paper. A sheet fitted to a page
//     count is fitted to the new paper.
//   - [FromPPTX], [FromODP]: each slide, notes page and handout page is
//     scaled down to fit the paper, never up, and centred on it. A handout
//     page takes the paper's orientation first (see [WithHandouts]).
//
// Without it the source's own size applies, and [WithDefaultPaper] where
// the source names none.
func WithPaper(paper Paper) ConvertOption {
	return convertOption(func(c *convertConfig) { c.paper, c.paperSet = paper, true })
}

// WithDefaultPaper sets the paper of a source that names none: a text or
// Markdown file, an HTML document without an @page size, a section or a
// sheet without a page setup, a CSV file. Without it that paper is
// [PaperA4]. It never replaces a size the source names, which [WithPaper]
// does, and a presentation always names its slide size. Each side must be
// finite, positive and at most 14,400 points, or the conversion fails with
// [ErrInvalidConvertOptions].
func WithDefaultPaper(paper Paper) ConvertOption {
	return convertOption(func(c *convertConfig) { c.defaultPaper = paper })
}

// WithOrientation turns the pages of the conversion: [OrientationPortrait]
// turns every page that is wider than it is high, [OrientationLandscape]
// every page that is higher than it is wide, whatever named the page's
// size. [OrientationSource], the default, turns none. Any other value fails
// the conversion with [ErrInvalidConvertOptions].
//
// In an HTML or Markdown source the paper turns before the @page rules see
// it, and an @page size that applies keeps its own orientation (CSS Paged
// Media 3, 7.1).
func WithOrientation(orientation Orientation) ConvertOption {
	return convertOption(func(c *convertConfig) { c.orientation = orientation })
}

// WithPageRanges selects the pages written, by the page range specification
// [ParsePageRanges] reads, resolved against the pages the source lays out
// on; empty, the default, writes every page. The pages come out in the
// order the specification lists them, a page listed twice written twice. A
// specification [ParsePageRanges] refuses, a page past the last one
// included, fails the conversion with [ErrInvalidConvertOptions], which
// also wraps [ErrInvalidPageRange].
//
// The selection happens after layout: page counters count the whole
// document, a link to a page left out is dropped, and an outline entry on
// one is replaced by its entries. A text source holds every page in memory
// until the last with a selection, where without one each page leaves
// memory once written.
func WithPageRanges(spec string) ConvertOption {
	return convertOption(func(c *convertConfig) { c.pageRanges = spec })
}

// WithOutline sets whether the conversion writes a document outline: the
// headings of an HTML or Markdown source, the paragraphs of a DOCX or ODT
// source whose style has an outline level, the sheets of a spreadsheet, the
// slides of a presentation. true is the default. A text source has no
// outline either way.
func WithOutline(outline bool) ConvertOption {
	return convertOption(func(c *convertConfig) { c.omitOutline = !outline })
}

// WithDate sets the one date of the conversion, which the fields of the
// source that show the current date or time take: the date class of an HTML
// header or footer template, the date and time fields of a spreadsheet's
// headers and footers and of a slide, and in a text document the fields of
// the current date, the current time and the print date that the file
// neither fixes nor locks. A field of the date a document was created or
// saved keeps what the file stores. The zero time, the default, leaves
// every field showing what its file stored, or nothing where the file
// stores none. The output never depends on the clock.
func WithDate(t time.Time) ConvertOption {
	return convertOption(func(c *convertConfig) { c.date = t })
}

// WithDefaultLanguage sets the natural language of a source that names
// none, as a BCP 47 tag such as "en-US" or "ja": the language the file
// declares for its text (ISO 32000-1, 14.9.2), and the one that chooses
// between the regional faces of a font family for Han characters. Empty,
// the default, leaves the language unset. It never replaces a language the
// source names. A tag that is not well formed fails the conversion with
// [ErrInvalidConvertOptions].
func WithDefaultLanguage(lang string) ConvertOption {
	return convertOption(func(c *convertConfig) { c.defaultLang = lang })
}

// WithConvertVersion sets the PDF version the conversion writes: "1.4",
// "1.5", "1.6", "1.7", the default, or "2.0". The header declares it, and
// the file holds no construct a later version defines: what the source
// asks for that the version lacks is written as the nearest thing the
// version has. Any other value fails the conversion with
// [ErrInvalidConvertOptions].
//
// A conversion claims no conformance. To conform to a PDF/A or PDF/UA
// profile, convert at a version the profile builds on and remediate the
// result, as a stage after [Pipeline.Convert]: "1.4" for PDF/A-1; "1.4" to
// "1.7" for PDF/A-2, PDF/A-3 and PDF/UA-1; "2.0" for PDF/A-4 and PDF/UA-2.
func WithConvertVersion(version string) ConvertOption {
	return convertOption(func(c *convertConfig) { c.version = version })
}

// WithConvertFontDirs sets the directories the conversion reads fonts from,
// searched recursively for .ttf, .otf, .ttc and .otc files, in the order
// given: the order decides between faces that match equally. Without it, or
// with no directory, the conversion reads the operating system's font
// directories, the ones [WithSystemFonts] reads, and its output then
// depends on the fonts installed where litho runs.
//
// The faces of a list of directories are indexed once per process and kept
// for later conversions with the same list, so a font added to a directory
// after its first conversion is not seen. A few lists are kept at a time; a
// conversion with a new list past them drops the oldest. Indexing the
// system directories of a desktop takes seconds. An index keeps about a
// kilobyte a face, and the faces conversions draw with: up to 128 MiB of
// parsed fonts and 128 MiB of font programs, the least recently used
// leaving first.
func WithConvertFontDirs(dirs ...string) ConvertOption {
	dirs = slices.Clone(dirs)
	return convertOption(func(c *convertConfig) { c.fontDirs = dirs })
}

// WithConvertLimits sets the resource budgets of the conversion. It reads
// [Limits.MaxConvertInputBytes], [Limits.MaxConvertPages],
// [Limits.MaxConvertMemoryBytes] and [Limits.MaxImagePixels]; a zero field
// keeps its default, and a field out of range fails the conversion with
// [ErrInvalidLimits] before it reads the source.
func WithConvertLimits(l Limits) ConvertOption {
	return convertOption(func(c *convertConfig) { c.limits = l })
}

// WithConvertLogger injects a structured logger for the diagnostics of the
// conversion: font files skipped while indexing, a font program embedded
// whole, a recovered panic. A nil logger discards them, the default. A
// conversion that runs in a [Pipeline] without one logs to the pipeline's.
func WithConvertLogger(logger *slog.Logger) ConvertOption {
	return convertOption(func(c *convertConfig) { c.logger = logger })
}

// WithConvertTempDir sets the directory the conversion writes its
// intermediate files in, removed before it returns, on success and on
// failure. Empty, the default, is the operating system's temporary
// directory, or the pipeline's when the conversion runs in a [Pipeline].
//
// A conversion whose output passes a few MiB keeps the page contents and
// font programs it writes there until the file is complete, instead of in
// memory, and one whose pictures pass 8 MiB, compressed as the file stores
// them, keeps the rest of them there from the moment it reads them.
func WithConvertTempDir(dir string) ConvertOption {
	return convertOption(func(c *convertConfig) { c.tempDir = dir })
}

// WithMargins sets the page margins in points, zero for none. Without it
// every side is 56.693 points, 2 cm. In an HTML or Markdown source an @page
// margin wins on its side. A margin must be finite and zero or more, and
// the margins must leave room on the page, or the conversion fails with
// [ErrInvalidConvertOptions].
//
// An Office source keeps the margins its sections, sheets and slides carry,
// and takes no margin option.
func WithMargins(top, right, bottom, left float64) MarginOption {
	return marginOption(func(c *convertConfig) {
		c.marginTop, c.marginRight, c.marginBottom, c.marginLeft = top, right, bottom, left
		c.marginsSet = true
	})
}

// WithFontFamilies sets the font families a text source is drawn in, in
// preference order, matched without regard to case or spaces. The generic
// families serif, sans-serif, monospace, cursive, fantasy and system-ui
// expand to common families of their kind. The default is Liberation Serif,
// the family a word processor opens a text file in, then serif. A character
// the chosen face lacks is drawn from another face that covers it.
func WithFontFamilies(families ...string) TextOption {
	families = slices.Clone(families)
	return textOption(func(c *convertConfig) { c.families = families })
}

// WithFontSize sets the font size of a text source in points, 12 by
// default. It must be finite, positive and at most 1000, or the conversion
// fails with [ErrInvalidConvertOptions].
func WithFontSize(size float64) TextOption {
	return textOption(func(c *convertConfig) { c.fontSize, c.fontSizeSet = size, true })
}

// WithLineHeight sets how far apart the lines of a text source sit, as a
// multiple of the font size: 1.15 by default, the single spacing of a word
// processor. It must be finite, positive and at most 100, or the conversion
// fails with [ErrInvalidConvertOptions].
func WithLineHeight(factor float64) TextOption {
	return textOption(func(c *convertConfig) { c.lineHeight, c.lineHeightSet = factor, true })
}

// WithTabWidth sets the distance between the tab stops of a text source, in
// characters of the font: 8 by default. A tab moves to the next multiple of
// it from the start of the line. It must be in [1, 64], or the conversion
// fails with [ErrInvalidConvertOptions].
func WithTabWidth(columns int) TextOption {
	return textOption(func(c *convertConfig) { c.tabWidth, c.tabWidthSet = columns, true })
}

// WithResources gives an HTML or Markdown source the files its relative
// URLs name: stylesheets, images and fonts, read from fsys with the URL
// path as the file path, the document sitting at the root. Without it, and
// for every root-relative path or path that leaves the root, the reference
// is left out and reported as a warning. data: URLs need no resources; http
// and https URLs are fetched through [WithHTTPClient], file: URLs never.
//
// A conversion reads at most 32 MiB a file and 256 MiB of files in all, and
// decodes at most 4096 distinct images, each within
// [Limits.MaxImagePixels]; a resource past a cap is left out with a
// warning. An image costs [Limits.MaxConvertMemoryBytes] 4 bytes a pixel (8
// for 16 bits a channel) while it is decoded, then its compressed samples
// for as long as the conversion holds it; a JPEG written as it is costs its
// file. A document whose images the budget cannot hold fails with
// [ErrConvertInputTooLarge].
func WithResources(fsys fs.FS) HTMLOption {
	return htmlOption(func(c *convertConfig) { c.resources = fsys })
}

// WithBaseURL sets the URL of an HTML or Markdown source: what the url
// class of a header or footer template shows, and, for an http or https
// URL, what relative references resolve against, fetched through
// [WithHTTPClient]. Empty, the default, resolves relative references in
// [WithResources]. A URL that is not absolute fails the conversion with
// [ErrInvalidConvertOptions].
func WithBaseURL(u string) HTMLOption {
	return htmlOption(func(c *convertConfig) { c.baseURL = u })
}

// WithHTTPClient fetches the http and https resources an HTML or Markdown
// source names, stylesheets, images and fonts, through client, each read up
// to the caps of [WithResources]. The caller's client decides what the
// document may reach: its transport, its timeouts and its redirects. nil,
// the default, leaves every such reference out with a warning.
func WithHTTPClient(client *http.Client) HTMLOption {
	return htmlOption(func(c *convertConfig) { c.httpClient = client })
}

// WithMediaType sets the media type the stylesheets of an HTML or Markdown
// source are evaluated for: "print", the default, or "screen". Any other
// value fails the conversion with [ErrInvalidConvertOptions].
func WithMediaType(media string) HTMLOption {
	return htmlOption(func(c *convertConfig) { c.media = media })
}

// WithMediaFeatures sets the discrete media features the @media rules of an
// HTML or Markdown source evaluate against, by lowercase name, such as
// "prefers-color-scheme": "dark" (Media Queries 5, 2.4). A feature absent
// keeps its default: light, no-preference, none, what a printer reports. A
// name that is not lowercase fails the conversion with
// [ErrInvalidConvertOptions]. The map is read when the conversion runs and
// must not change until then.
func WithMediaFeatures(features map[string]string) HTMLOption {
	return htmlOption(func(c *convertConfig) { c.mediaFeatures = features })
}

// WithScale scales the content of an HTML or Markdown source: 1, the
// default, draws it as laid out; a scale in [0.1, 2] lays the content out
// in the page area divided by it and draws it at that scale, the page
// keeping its size and margins. Any other value, 0 included, fails the
// conversion with [ErrInvalidConvertOptions].
func WithScale(scale float64) HTMLOption {
	return htmlOption(func(c *convertConfig) { c.scale, c.scaleSet = scale, true })
}

// WithShrinkToFit sets whether an HTML or Markdown source wider than its
// page area is shrunk to fit it. true, the default, lays it out at its
// content's width, at most 1.5 times the area's, and draws it scaled down
// to the area, as browsers print a wide document; false lays it out at the
// area's width and lets the page clip what overflows.
func WithShrinkToFit(shrink bool) HTMLOption {
	return htmlOption(func(c *convertConfig) { c.noShrinkToFit = !shrink })
}

// WithCanvasBackground sets whether the canvas background of an HTML or
// Markdown source prints. true, the default, prints every background; false
// drops the background colours and images of the html and body elements,
// which paint the canvas (CSS Backgrounds 3, 2.11.1), and keeps every other
// element's, so the page is white behind content whose own colours stay
// exact.
func WithCanvasBackground(print bool) HTMLOption {
	return htmlOption(func(c *convertConfig) { c.omitCanvasBackground = !print })
}

// WithHeaderTemplate draws the HTML fragment html in the top margin of
// every page of an HTML or Markdown source: the fragment is the inner HTML
// of a box as wide as the page and as tall as the margin, laid out with the
// stylesheet and defaults of a browser's print header, where the text of an
// element of class date, title, url, pageNumber or totalPages is replaced
// by the date of [WithDate], the document title, the URL of [WithBaseURL],
// the page number and the page count, and an element of class text that
// reaches into the page area is hidden. Empty, the default, draws nothing.
//
// A page-margin box of the document's own stylesheet, or of
// [WithUserStylesheet], draws a header without a template (CSS Paged Media
// 3, 6.4).
func WithHeaderTemplate(html string) HTMLOption {
	return htmlOption(func(c *convertConfig) { c.headerTemplate = html })
}

// WithFooterTemplate draws the HTML fragment html in the bottom margin of
// every page of an HTML or Markdown source, as [WithHeaderTemplate] does in
// the top one.
func WithFooterTemplate(html string) HTMLOption {
	return htmlOption(func(c *convertConfig) { c.footerTemplate = html })
}

// WithSinglePage writes an HTML or Markdown source on one page sized to its
// content: the document is measured first as a screen 800 CSS pixels wide
// would show it, in one continuous column, forced breaks ignored, at least
// 437.12 CSS pixels tall and 800 wide, and the paper becomes that tall plus
// the top and bottom margins, and with [OrientationLandscape] that wide
// plus the left and right margins. The document is printed on it as usual
// and only the first page is written. With [WithPageRanges] the conversion
// fails with [ErrInvalidConvertOptions]: one page has no range to select.
func WithSinglePage(single bool) HTMLOption {
	return htmlOption(func(c *convertConfig) { c.singlePage = single })
}

// WithFractionalFontMetrics sets whether the lines of an HTML or Markdown
// source use the fonts' fractional ascent, descent and line gap. false, the
// default, rounds them to whole CSS pixels first, as browsers do for
// document text, so lines of line-height: normal sit where a browser puts
// them; true keeps the fractions, as print formatters do.
func WithFractionalFontMetrics(fractional bool) HTMLOption {
	return htmlOption(func(c *convertConfig) { c.fractionalFontMetrics = fractional })
}

// WithUserStylesheet adds the stylesheet css to an HTML or Markdown source
// as a user stylesheet: its declarations cascade above the defaults of the
// engine and below the document's own, and its !important declarations
// above the document's (CSS Cascading 4, 6.2). It sets what the caller
// wants of any document without editing it: an @page size or margin, a
// page-margin box, a font, a rule hiding an element. Several stylesheets
// apply in the order given. Its relative URLs resolve as the document's do.
// It styles the document only, not the templates of [WithHeaderTemplate]
// and [WithFooterTemplate], which carry their own stylesheet.
//
// A stylesheet counts against the caps of [WithResources], and a construct
// of it the engine does not support is reported as a warning, as one of the
// document's is.
func WithUserStylesheet(css string) HTMLOption {
	return htmlOption(func(c *convertConfig) { c.userStylesheets = append(c.userStylesheets, css) })
}

// DefaultMarkdownTemplate is the template a Markdown source renders into
// without [WithMarkdownTemplate]: a document without a stylesheet, whose
// title is the first heading of the source.
const DefaultMarkdownTemplate = `<!doctype html><html><head><meta charset="utf-8"><title>{{ .Title }}</title></head><body>{{ toHTML }}</body></html>`

// WithMarkdownTemplate sets the HTML document a Markdown source renders
// into, as a template of html/template. {{ toHTML }} is replaced by the
// HTML the source renders to, and {{ .Title }} by the text of its first
// heading, empty without one. The result is laid out as [FromHTML] lays an
// HTML document out, so the template carries the stylesheet, the fonts and
// the page rules of the output. Without it the template is
// [DefaultMarkdownTemplate].
//
// A template that does not parse, or in which no toHTML call can be
// reached, fails the conversion with [ErrInvalidConvertOptions]: the source
// would not be printed. The template sees the function toHTML and the field
// .Title alone: a builtin function, another field, a range, a template that
// calls itself or one that is not defined, or one that expands past 2^20
// nodes fails the same way, as does an error while executing it. Each
// toHTML call inserts the same HTML, so a second call prints the source
// twice. A template longer than 1 MiB is refused, and one that renders past
// [Limits.MaxConvertInputBytes]'s ceiling fails with
// [ErrConvertInputTooLarge].
func WithMarkdownTemplate(tmpl string) MarkdownOption {
	return markdownOption(func(c *convertConfig) { c.markdownTemplate, c.markdownTemplateSet = tmpl, true })
}

// WithOfficePassword opens an encrypted DOCX, ODT, XLSX, ODS, PPTX or ODP
// source with password. Without it, or with a wrong one, an encrypted
// source fails with [ErrConvertEncrypted]. A source that is not encrypted
// ignores it.
func WithOfficePassword(password string) OfficeOption {
	return officeOption(func(c *convertConfig) { c.password = password })
}

// WithTrackedChanges sets how the changes a DOCX or ODT source tracks are
// printed: [TrackedChangesAccepted], the default, [TrackedChangesRejected]
// or [TrackedChangesMarked]. A source that tracks no change prints the same
// under each. Any other value fails the conversion with
// [ErrInvalidConvertOptions].
//
// The view covers inserted and deleted text, paragraphs and table rows. A
// tracked change of formatting prints with the new formatting under every
// view.
func WithTrackedChanges(view TrackedChanges) WordProcessingOption {
	return wordProcessingOption(func(c *convertConfig) { c.trackedChanges = view })
}

// WithFormFields writes the form controls of a DOCX or ODT source as
// interactive form fields (ISO 32000-1, 12.7), which a reader fills in a
// PDF viewer. false, the default, prints each control as it shows, with
// nothing to fill in.
//
// In a DOCX source, an inline content control (ECMA-376 Part 1, 17.5.2)
// becomes a field: a plain or rich text control or a date picker a text
// field, a drop-down list a combo box, a combo box an editable combo box,
// an Office 2010 check box a check box; and so does a legacy form field,
// FORMTEXT a text field, FORMDROPDOWN a combo box, FORMCHECKBOX a check
// box. In an ODT source, a form control drawn by a draw:control (ODF 1.3,
// Part 3, 13) becomes a field: a text, text area, formatted, date, time or
// number field a text field, a check box a check box, the radio buttons
// of one name one radio button group, a list box a list box, a drop-down
// list box or a combo box a combo box.
//
// A field holds the value its control shows, the items of a list, and its
// control's state: checked, read-only (a locked or disabled control),
// required, its maximum length. Its name is the control's tag, else its
// alias, for a content control, the w:name of a legacy field, the
// form:name of an ODT control, "Field" for none; a name an earlier field
// took gets the suffix "_2", "_3" and so on, but for the radio buttons of
// one name, which stay one group. Its description, the tooltip a viewer
// shows and a screen reader speaks, is the alias, the help text or the
// form:title, else the name.
//
// With or without the option, every control prints what it shows, as
// LibreOffice prints a document without its form fields: the text of a
// content control or a legacy field, a legacy check box as a square,
// crossed when checked; an ODT control's value in its frame, a check
// box's square and cross, a radio button's ring and dot, a list box's
// items with the selected ones white on black. With the option the page
// looks the same: what a control shows moves from the page into its
// field's appearance, which every viewer draws and prints, in the same
// place, the frame and a list's highlight staying on the page, so a
// viewer that does not fill forms shows the same page.
//
// A control a field does not take prints as it shows: a DOCX content
// control around paragraphs or table rows, a picture, building block,
// citation, equation or repeating section control, a control inside
// another; an ODT push button, as a grey box with its label, a hidden,
// image, grid, file or password control, as its empty frame; any control
// in a header or a footer, and one whose content breaks across two
// pages, which [ConvertReport.Warnings] reports. An ODT control in a
// header or a footer, or one that does not print, prints nothing.
//
// Each field costs a widget annotation and an appearance stream, two for
// a check box or a radio button, about a kilobyte. The appearances draw
// with the document's embedded fonts, whose subsets hold only the glyphs
// the document draws, so the text a reader types into a text or choice
// field is set in Helvetica, which the form's default resources name and
// do not embed: every reader holds it, its encoding (WinAnsiEncoding)
// covers Latin text alone, and PDF 2.0 deprecates a font left unembedded.
// No field asks the reader to draw its appearance (/NeedAppearances), and
// in a tagged document each widget hangs from a Form structure element,
// so a file with fields remediates to PDF/A and PDF/UA as it does
// without them: the Helvetica of the default resources draws in no
// content stream.
//
// Not supported: push buttons, signature fields, actions, scripts and
// calculations (a legacy field's macros, a date field's format), a
// field's border and background, which stay on the page, a list box's
// highlight, which does not follow a selection a reader changes, an ODT
// check box's or radio button's form:label, which LibreOffice draws under
// an empty clip, and export values: the items of a list are written by
// the text they show.
func WithFormFields(fields bool) WordProcessingOption {
	return wordProcessingOption(func(c *convertConfig) { c.formFields = fields })
}

// WithOnePagePerSheet lays each sheet of an XLSX, ODS or CSV source out on
// one page the size of its content: the sheet's paper, scale and page
// breaks are set aside. false, the default, cuts each sheet into pages by
// its page setup. With [WithPaper] or [WithOrientation] the conversion
// fails with [ErrInvalidConvertOptions]: the page takes its content's size.
func WithOnePagePerSheet(one bool) SheetOption {
	return sheetOption(func(c *convertConfig) { c.onePagePerSheet = one })
}

// WithSheets prints the sheets of an XLSX or ODS source that names lists,
// by sheet name, matched exactly, in book order, a hidden sheet included
// when named. Without it every sheet that is not hidden prints. A name the
// workbook has no sheet for, an empty list, or [WithSheetNumbers] beside
// it, fails the conversion with [ErrInvalidConvertOptions].
func WithSheets(names ...string) SpreadsheetOption {
	names = slices.Clone(names)
	return spreadsheetOption(func(c *convertConfig) { c.sheets, c.sheetsSet = names, true })
}

// WithSheetNumbers prints the sheets of an XLSX or ODS source that numbers
// lists, by position in the book, the first sheet being 1 and hidden sheets
// counted, in book order, a hidden sheet included when listed. Without it
// every sheet that is not hidden prints. A number the workbook has no sheet
// for, an empty list, or [WithSheets] beside it, fails the conversion with
// [ErrInvalidConvertOptions].
func WithSheetNumbers(numbers ...int) SpreadsheetOption {
	numbers = slices.Clone(numbers)
	return spreadsheetOption(func(c *convertConfig) { c.sheetNumbers, c.sheetNumbersSet = numbers, true })
}

// WithHiddenSheets sets whether the sheets an XLSX or ODS source hides are
// printed. false, the default, leaves them out, as a workbook is printed;
// true prints every sheet, in book order, the sheets an XLSX source hides
// from its own list of hidden sheets and those an ODS source marks as not
// printed included. A workbook that hides every sheet prints its first
// either way.
func WithHiddenSheets(include bool) SpreadsheetOption {
	return spreadsheetOption(func(c *convertConfig) { c.hiddenSheets = include })
}

// WithFormulaRecalculation sets when the formulas of an XLSX or ODS source
// are computed again: [RecalculateAsNeeded], the default,
// [RecalculateNever] or [RecalculateAlways]. Any other value fails the
// conversion with [ErrInvalidConvertOptions].
//
// A computed value depends on nothing outside the file: a formula that
// reads the clock, a random number or another workbook keeps its stored
// value. Use [RecalculateNever] to print exactly what the file's last
// application saved.
func WithFormulaRecalculation(mode Recalculation) SpreadsheetOption {
	return spreadsheetOption(func(c *convertConfig) { c.recalculation = mode })
}

// WithCSVSeparators sets the characters that separate the fields of a CSV
// source, any of which ends a field: a comma by default (RFC 4180, 2). Name
// ';' for the files of locales that write decimal commas, '\t' for
// tab-separated values, or several for a file that mixes them. A separator
// inside a quoted field is text. At most 8 separators, none of them a
// double quote, a line break or U+FFFD, or the conversion fails with
// [ErrInvalidConvertOptions].
func WithCSVSeparators(separators ...rune) CSVOption {
	separators = slices.Clone(separators)
	return csvOption(func(c *convertConfig) { c.csvSeparators, c.csvSeparatorSet = separators, true })
}

// WithCSVTypeDetection sets whether the fields of a CSV source that read as
// numbers, dates and times are laid out as such: right-aligned, in the
// form a spreadsheet shows them, "007" as 7. true is the default. false
// lays every field out as the text the file holds.
func WithCSVTypeDetection(detect bool) CSVOption {
	return csvOption(func(c *convertConfig) { c.csvKeepText = !detect })
}

// WithHiddenSlides sets whether the slides a PPTX or ODP source hides from
// its show are written. false, the default, leaves them out, as a show is
// printed; true writes every slide. Slide numbers count the slides written.
func WithHiddenSlides(include bool) PresentationOption {
	return presentationOption(func(c *convertConfig) { c.hiddenSlides = include })
}

// WithNotesPages writes, after the slides of a PPTX or ODP source, one
// notes page per slide written: the slide drawn in its box and the slide's
// notes below it. A PPTX notes page is the notes size the presentation
// names; an ODP notes page prints on the default paper ([WithDefaultPaper],
// A4 without it), the notes page at its top-left corner, whatever size the
// presentation gives it. With [WithPaper] the notes page is scaled down to
// fit the paper and centred. false, the default, writes the slides alone.
func WithNotesPages(notes bool) PresentationOption {
	return presentationOption(func(c *convertConfig) { c.notesPages = notes })
}

// WithHandouts writes a PPTX or ODP source as handout pages in place of
// its slide pages: slidesPerPage slides drawn small on each page, in show
// order row by row, with the header, footer, date and page number of the
// presentation's handout master. slidesPerPage is 1, 2, 3, 4, 6 or 9; 0,
// the default, writes the slides, one a page. Any other value, or
// [WithNotesPages] beside it, fails the conversion with
// [ErrInvalidConvertOptions].
//
// The page is the handout page of the presentation: the page of an ODP
// handout master, the notes size of a PPTX, the default paper
// ([WithDefaultPaper]) when the source names neither. The slides are laid
// out on it as LibreOffice lays a handout
// out, those of a PPTX too. Three a page, the slides take one column of a
// portrait page or one row of a landscape page, each with seven lines
// ruled beside it for notes.
//
// The handout page takes the orientation of the page it prints on.
// [WithOrientation], or a [WithPaper] of the other orientation, turns it:
// the slides are laid out again on the turned page, six a page in two rows
// of three where they stood in three rows of two, and the header, footer,
// date and page number keep their place in proportion to the page. Another
// shape of the handout master keeps its size. [WithPaper] then scales the
// handout page down to fit the paper, never up, and centres it.
func WithHandouts(slidesPerPage int) PresentationOption {
	return presentationOption(func(c *convertConfig) { c.handouts = slidesPerPage })
}
