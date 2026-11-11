package fixture

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/pdf"
)

// The page a source is converted on when its options leave the paper and
// the margins out, in points: what Chromium lays out for the A4 paper
// corpus references asks for (paperWidth=210mm, paperHeight=297mm) with the
// Chromium route's default margins of 0.39 inch. Chromium's references carry
// a 595.92 by 841.92 point media box and a text column 27.75 points in (37
// CSS pixels, see [Source.Margins]), measured on the committed PDFs; converting on the exact A4 of 595.276 by 841.89
// points with 28.08 point margins wraps some paragraphs one line longer and
// shifts every page break after.
const (
	DefaultPaperWidth  = 595.92
	DefaultPaperHeight = 841.92
	DefaultMargin      = 27.75
)

// DefaultMediaType is the media type a source is converted for when its
// options name none: print, as a printer reports.
const DefaultMediaType = "print"

// OptionsFile is the name of the options file of a source directory.
const OptionsFile = "options.json"

// The documents a source directory holds, one of the two: an HTML page,
// posted to the route's /forms/chromium/convert/html, or a Markdown page,
// which corpus references wraps in [MarkdownWrapper] and posts to
// /forms/chromium/convert/markdown.
const (
	HTMLDocument     = "index.html"
	MarkdownDocument = "index.md"
)

// MarkdownWrapper is the index.html corpus references posts beside a Markdown
// source: the document litho's Markdown converter writes around the
// rendered Markdown, so the two engines lay out the same tree.
const MarkdownWrapper = `<!doctype html><html><head><meta charset="utf-8"><title></title></head><body>{{ toHTML "index.md" }}</body></html>`

// Format names the format a fixture's document converts from, by the
// constructor of package pdf that reads it, or, for a format litho does
// not read, by its file extension.
type Format string

// The formats litho converts from, one per constructor of package pdf.
const (
	FormatText     Format = "text"
	FormatHTML     Format = "html"
	FormatMarkdown Format = "markdown"
	FormatDOCX     Format = "docx"
	FormatODT      Format = "odt"
	FormatXLSX     Format = "xlsx"
	FormatODS      Format = "ods"
	FormatCSV      Format = "csv"
	FormatPPTX     Format = "pptx"
	FormatODP      Format = "odp"
)

// ErrUnsupportedFormat reports a document of a format litho does not
// convert from.
var ErrUnsupportedFormat = errors.New("unsupported input format")

// optionsAs returns opts as the option type of one constructor, which every
// option of opts implements: a [pdf.ConvertOption] implements every option
// type, a [pdf.OfficeOption] those of the Office formats, and a
// [pdf.SheetOption] those of the sheet formats.
func optionsAs[T, O any](opts []O) []T {
	out := make([]T, len(opts))
	for i, o := range opts {
		out[i] = any(o).(T)
	}
	return out
}

// maxTemplateBytes bounds a header or footer template read from a source
// directory. Anti-DoS guard, not spec-mandated: the templates of the corpus
// are a few hundred bytes, and a committed file is trusted, so the cap only
// stops a mistake such as naming the wrong file.
const maxTemplateBytes = 64 << 10

// Options are the Chromium route's form fields a source is converted with,
// as options.json spells them. A field left out keeps the corpus default,
// which is what every source without the file is converted with; the
// defaults are not the route's own (A4, backgrounds printed and the CSS page
// size preferred, where the route defaults to Letter, no backgrounds and the
// paper size).
type Options struct {
	// PaperWidth and PaperHeight are the paper size in inches. Left out:
	// A4, posted as 210mm by 297mm, which Chromium lays out on a 595.92 by
	// 841.92 point page.
	PaperWidth  *float64 `json:"paperWidth,omitempty"`
	PaperHeight *float64 `json:"paperHeight,omitempty"`
	// MarginTop, MarginRight, MarginBottom and MarginLeft are the margins in
	// inches. Left out: the route's 0.39 inch, which Chromium lays out as
	// 27.75 points.
	MarginTop    *float64 `json:"marginTop,omitempty"`
	MarginRight  *float64 `json:"marginRight,omitempty"`
	MarginBottom *float64 `json:"marginBottom,omitempty"`
	MarginLeft   *float64 `json:"marginLeft,omitempty"`
	// Landscape swaps the paper's sides. Default false.
	Landscape bool `json:"landscape,omitempty"`
	// Scale scales the content, in [0.1, 2]. Default 1.
	Scale *float64 `json:"scale,omitempty"`
	// SinglePage lays the document out on one page as tall as its content.
	// Default false.
	SinglePage bool `json:"singlePage,omitempty"`
	// NativePages selects the pages written, as "1-5, 8". Default every
	// page.
	NativePages string `json:"nativePages,omitempty"`
	// PreferCSSPageSize lets @page size set the page. Default true: litho
	// honoured @page size before the option existed, and the references of
	// the first sources were built so.
	PreferCSSPageSize *bool `json:"preferCssPageSize,omitempty"`
	// PrintBackground prints background colours and images. Default true:
	// a document converter paints them.
	PrintBackground *bool `json:"printBackground,omitempty"`
	// NativeBookmarks writes the headings as the document outline. Default
	// false, the route's default.
	NativeBookmarks bool `json:"nativeBookmarks,omitempty"`
	// EmulatedMediaType is the media type, print or screen. Default print.
	EmulatedMediaType string `json:"emulatedMediaType,omitempty"`
	// EmulatedMediaFeatures are the media features, such as
	// {"prefers-color-scheme": "dark"}. Default none.
	EmulatedMediaFeatures map[string]string `json:"emulatedMediaFeatures,omitempty"`
	// Header and Footer name a file of the source directory posted as the
	// route's header.html and footer.html, the templates Chromium draws in
	// the top and bottom margins. Default none.
	Header string `json:"header,omitempty"`
	Footer string `json:"footer,omitempty"`
	// UserStylesheets name files of the source directory litho takes as
	// user stylesheets, in order. It is litho's own option, no route field:
	// Chromium prints the source without them, so a source that sets it
	// names ReferenceOf. Default none.
	UserStylesheets []string `json:"userStylesheets,omitempty"`
	// MarkdownTemplate names a file of the source directory litho renders a
	// Markdown document into. It is litho's own option, no route field: a
	// source that sets it names ReferenceOf. Default litho's template.
	MarkdownTemplate string `json:"markdownTemplate,omitempty"`
	// ReferenceOf names another fixture of the same corpus, by its ID,
	// whose reference stands for this source's (see
	// [OfficeOptions.ReferenceOf]). It is no route field. Default none.
	ReferenceOf string `json:"reference_of,omitempty"`
}

// Source is one source directory, read.
type Source struct {
	// Dir is the source directory.
	Dir string
	// Document is the path of the document converted, Dir's index.html or,
	// when there is none, its index.md.
	Document string
	// Format is the format the document is converted from, FormatHTML or
	// FormatMarkdown.
	Format Format
	// Options are the options read from options.json, zero when there is
	// none.
	Options Options
	// HeaderTemplate and FooterTemplate are the contents of the files
	// Options.Header and Options.Footer name.
	HeaderTemplate, FooterTemplate string
	// UserStylesheets are the contents of the files
	// Options.UserStylesheets names, in order.
	UserStylesheets []string
	// MarkdownTemplate is the content of the file Options.MarkdownTemplate
	// names, "" for litho's template.
	MarkdownTemplate string
}

// ErrNoDocument reports a source directory with neither index.html nor
// index.md.
var ErrNoDocument = errors.New("no index.html or index.md")

// Load reads the source directory dir: its document and its options file,
// when there is one, and the templates the options name. An options file
// that names an unknown field, a template outside the directory or a value
// out of the route's range fails the load.
func Load(dir string) (*Source, error) {
	s := &Source{Dir: dir}
	switch {
	case isFile(filepath.Join(dir, HTMLDocument)):
		s.Document, s.Format = filepath.Join(dir, HTMLDocument), FormatHTML
	case isFile(filepath.Join(dir, MarkdownDocument)):
		s.Document, s.Format = filepath.Join(dir, MarkdownDocument), FormatMarkdown
	default:
		return nil, fmt.Errorf("%s: %w", dir, ErrNoDocument)
	}
	data, err := os.ReadFile(filepath.Join(dir, OptionsFile)) //nolint:gosec // committed corpus source
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, fmt.Errorf("reading %s: %w", OptionsFile, err)
	}
	opts, err := ParseOptions(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(dir, OptionsFile), err)
	}
	s.Options = opts
	if s.HeaderTemplate, err = readTemplate(dir, opts.Header); err != nil {
		return nil, err
	}
	if s.FooterTemplate, err = readTemplate(dir, opts.Footer); err != nil {
		return nil, err
	}
	for _, name := range opts.UserStylesheets {
		css, err := readTemplate(dir, name)
		if err != nil {
			return nil, err
		}
		s.UserStylesheets = append(s.UserStylesheets, css)
	}
	if s.MarkdownTemplate, err = readTemplate(dir, opts.MarkdownTemplate); err != nil {
		return nil, err
	}
	return s, nil
}

// ParseOptions decodes an options file strictly: an unknown field, trailing
// data or a value the route refuses is an error, so a misspelt key cannot
// leave a fixture on the defaults unnoticed.
func ParseOptions(data []byte) (Options, error) {
	var o Options
	names := append(slices.Clone(OptionNames), LithoOptionNames...)
	if err := decodeStrict(data, append(names, ReferenceOfName), &o); err != nil {
		return Options{}, err
	}
	return o, o.validate()
}

// decodeStrict decodes the JSON object data into v, refusing trailing data
// and any key outside names.
func decodeStrict(data []byte, names []string, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	var fields map[string]json.RawMessage
	if err := dec.Decode(&fields); err != nil {
		return fmt.Errorf("decoding: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("decoding: data after the object")
	}
	// encoding/json matches field names regardless of case, jq does not:
	// a key must be spelt as the route spells it for both readers to see it.
	for name := range fields {
		if !slices.Contains(names, name) {
			return fmt.Errorf("decoding: unknown field %q", name)
		}
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("decoding: %w", err)
	}
	return nil
}

// OptionNames are the keys of an options file, the JSON names of [Options].
//
//nolint:gochecknoglobals // static list of the schema's keys
var OptionNames = []string{
	"paperWidth", "paperHeight", "marginTop", "marginRight", "marginBottom", "marginLeft",
	"landscape", "scale", "singlePage", "nativePages", "preferCssPageSize", "printBackground",
	"nativeBookmarks", "emulatedMediaType", "emulatedMediaFeatures", "header", "footer",
}

// LithoOptionNames are the keys of an options file that are litho's own
// options: the route has no such field and never receives them.
//
//nolint:gochecknoglobals // static list of the schema's keys
var LithoOptionNames = []string{"userStylesheets", "markdownTemplate"}

// validate checks the values against the ranges the Chromium route accepts.
func (o Options) validate() error {
	for name, v := range map[string]*float64{
		"paperWidth": o.PaperWidth, "paperHeight": o.PaperHeight,
	} {
		if v != nil && (*v <= 0 || math.IsInf(*v, 0) || math.IsNaN(*v)) {
			return fmt.Errorf("%s %v: want a positive size in inches", name, *v)
		}
	}
	for name, v := range map[string]*float64{
		"marginTop": o.MarginTop, "marginRight": o.MarginRight, "marginBottom": o.MarginBottom, "marginLeft": o.MarginLeft,
	} {
		if v != nil && (*v < 0 || math.IsInf(*v, 0) || math.IsNaN(*v)) {
			return fmt.Errorf("%s %v: want a margin of zero or more inches", name, *v)
		}
	}
	if o.Scale != nil && (*o.Scale < 0.1 || *o.Scale > 2) {
		return fmt.Errorf("scale %v: want a value in [0.1, 2]", *o.Scale)
	}
	switch o.EmulatedMediaType {
	case "", "print", "screen":
	default:
		return fmt.Errorf("emulatedMediaType %q: want print or screen", o.EmulatedMediaType)
	}
	for _, name := range append([]string{o.Header, o.Footer, o.MarkdownTemplate}, o.UserStylesheets...) {
		if name != "" && (name != filepath.Base(name) || strings.ContainsAny(name, `/\`) || name == "." || name == "..") {
			return fmt.Errorf("template %q: want a file name of the source directory", name)
		}
	}
	if slices.Contains(o.UserStylesheets, "") {
		return errors.New("userStylesheets: want file names of the source directory")
	}
	if o.ReferenceOf != strings.TrimSpace(o.ReferenceOf) || strings.Contains(o.ReferenceOf, "\x00") {
		return fmt.Errorf("%s %q: want a fixture ID", ReferenceOfName, o.ReferenceOf)
	}
	return nil
}

// readTemplate reads the template file name of dir; "" reads as no template.
func readTemplate(dir, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	f, err := os.Open(filepath.Join(dir, name)) //nolint:gosec // validated file name of a committed corpus source
	if err != nil {
		return "", fmt.Errorf("reading template: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxTemplateBytes+1))
	if err != nil {
		return "", fmt.Errorf("reading template %s: %w", name, err)
	}
	if len(data) > maxTemplateBytes {
		return "", fmt.Errorf("template %s exceeds %d bytes", name, maxTemplateBytes)
	}
	return string(data), nil
}

func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}

// Paper returns the paper size in points, before landscape swaps it: the
// measured A4 when the options leave a side out, else the inches at 72
// points each.
func (s *Source) Paper() (width, height float64) {
	width, height = DefaultPaperWidth, DefaultPaperHeight
	if v := s.Options.PaperWidth; v != nil {
		width = *v * 72
	}
	if v := s.Options.PaperHeight; v != nil {
		height = *v * 72
	}
	return width, height
}

// Margins returns the margins in points: 27.75 for a side the options leave
// out, else what Chromium lays out for the inches: printToPDF keeps the
// margins in whole points, truncating the inches times 72 (printing's
// PageMargins holds integers), and Blink then lays the page out in whole
// CSS pixels, rounding those points times 4/3 to the nearest; the margin is
// that many pixels at 0.75 point each. A whole number of points times 4/3
// never ends in a half, so the rounding has no tie.
//
// # Corpus evidence
//
//   - probe, html: margins of 0.0104, 0.1, 0.2, 0.3, 0.35, 0.37, 0.42917 and 0.6
//     inch put the content box of Chromium's page at 0, 6.75, 14.25, 21,
//     24.75, 26.25, 30 and 42.75 points; rounding the inches to whole
//     pixels instead gives 0.75, 7.5, 14.25, 21.75, 25.5, 27, 30.75 and 43.5,
//     and truncating them to whole pixels 30.75 at 0.42917.
func (s *Source) Margins() (top, right, bottom, left float64) {
	m := func(v *float64) float64 {
		if v == nil {
			return DefaultMargin
		}
		return chromiumMargin(*v)
	}
	o := s.Options
	return m(o.MarginTop), m(o.MarginRight), m(o.MarginBottom), m(o.MarginLeft)
}

// chromiumMargin returns the margin in points Chromium lays out for a
// margin of inches (see [Source.Margins]).
func chromiumMargin(inches float64) float64 {
	return math.Round(math.Trunc(inches*72)*4/3) * 0.75
}

// MediaType returns the media type the stylesheets are evaluated for.
func (s *Source) MediaType() string {
	if s.Options.EmulatedMediaType == "" {
		return DefaultMediaType
	}
	return s.Options.EmulatedMediaType
}

// PreferCSSPageSize reports whether @page size sets the page: true unless
// the options say false.
func (s *Source) PreferCSSPageSize() bool {
	return s.Options.PreferCSSPageSize == nil || *s.Options.PreferCSSPageSize
}

// PrintOptions returns the litho options of the route's print fields, every
// one but the paper, the margins, the media type and the CSS page size,
// which the callers pass themselves: landscape, scale, page ranges, single
// page, backgrounds, outline, header and footer templates and media
// features. A field the options leave out maps to its corpus default. A
// single page sets the page ranges aside: one page has none to select, and
// a conversion refuses the pair.
func (s *Source) PrintOptions() []pdf.HTMLOption {
	o := s.Options
	opts := []pdf.HTMLOption{
		pdf.WithSinglePage(o.SinglePage),
		pdf.WithCanvasBackground(o.PrintBackground == nil || *o.PrintBackground),
		pdf.WithOutline(o.NativeBookmarks),
	}
	if o.Landscape {
		opts = append(opts, pdf.WithOrientation(pdf.OrientationLandscape))
	}
	if o.Scale != nil {
		opts = append(opts, pdf.WithScale(*o.Scale))
	}
	if o.NativePages != "" && !o.SinglePage {
		opts = append(opts, pdf.WithPageRanges(o.NativePages))
	}
	if s.HeaderTemplate != "" {
		opts = append(opts, pdf.WithHeaderTemplate(s.HeaderTemplate))
	}
	if s.FooterTemplate != "" {
		opts = append(opts, pdf.WithFooterTemplate(s.FooterTemplate))
	}
	if len(o.EmulatedMediaFeatures) > 0 {
		opts = append(opts, pdf.WithMediaFeatures(o.EmulatedMediaFeatures))
	}
	for _, css := range s.UserStylesheets {
		opts = append(opts, pdf.WithUserStylesheet(css))
	}
	return opts
}

// Conversion returns the conversion of src, a document of format, an HTML
// or a Markdown one, with opts. A Markdown document renders into template,
// litho's own when it is "".
func Conversion(format Format, src io.Reader, template string, opts ...pdf.HTMLOption) *pdf.Conversion {
	if format == FormatMarkdown {
		all := optionsAs[pdf.MarkdownOption](opts)
		if template != "" {
			all = append(all, pdf.WithMarkdownTemplate(template))
		}
		return pdf.FromMarkdown(src, all...)
	}
	return pdf.FromHTML(src, opts...)
}

// FontFamilies are the prefixes of the font directories
// `corpus fetch --corpus=fonts` installs, the faces every converter oracle
// and fixture builder draws text with. The fonts directory also holds test
// fonts (Ahem, the WeasyPrint test font, resvg's Noto faces), which only the
// oracles written for them read.
//
//nolint:gochecknoglobals // static list of directory prefixes
var FontFamilies = []string{"dejavu-", "liberation-", "noto-cjk-sans-", "noto-sans-thai-", "noto-sans-devanagari-", "carlito-", "caladea-", "noto-core-", "opensymbol-"}
