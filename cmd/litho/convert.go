package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gotenberg/litho/pkg/pdf"
)

// convertUsage is the synopsis litho convert prints on a missing argument.
const convertUsage = "Usage: litho convert [--from text|html|markdown|docx|odt|xlsx|ods|csv|pptx|odp] [--paper A4|Letter|WxH] [--landscape] [--margin PT | --margin T,R,B,L] " +
	"[--font FAMILY] [--font-size PT] [--media print|screen] [--media-feature NAME=VALUE]... [--resources DIR] [--base-url URL] " +
	"[--prefer-css-page-size] [--scale N] [--single-page] [--pages RANGES] [--background=false] [--outline=false] " +
	"[--header FILE] [--footer FILE] [--date RFC3339|now] [--password PASSWORD] [--hidden-slides] [--notes-pages] [--font-dir DIR]... [-f json] -o out.pdf <file|http(s) URL>"

// convertFormats maps the --from values, and the file extensions that imply
// them, to the formats [pdf.Convert] reads. Immutable lookup tables.
var (
	convertFormats = map[string]pdf.Format{
		"text":     pdf.FormatText,
		"html":     pdf.FormatHTML,
		"markdown": pdf.FormatMarkdown,
		"docx":     pdf.FormatDOCX,
		"odt":      pdf.FormatODT,
		"xlsx":     pdf.FormatXLSX,
		"ods":      pdf.FormatODS,
		"csv":      pdf.FormatCSV,
		"pptx":     pdf.FormatPPTX,
		"odp":      pdf.FormatODP,
	}
	convertExtensions = map[string]pdf.Format{
		".txt":      pdf.FormatText,
		".text":     pdf.FormatText,
		".html":     pdf.FormatHTML,
		".htm":      pdf.FormatHTML,
		".md":       pdf.FormatMarkdown,
		".markdown": pdf.FormatMarkdown,
		".docx":     pdf.FormatDOCX,
		".odt":      pdf.FormatODT,
		".fodt":     pdf.FormatODT,
		".xlsx":     pdf.FormatXLSX,
		".xlsm":     pdf.FormatXLSX,
		".xltx":     pdf.FormatXLSX,
		".xltm":     pdf.FormatXLSX,
		".ods":      pdf.FormatODS,
		".fods":     pdf.FormatODS,
		".ots":      pdf.FormatODS,
		".csv":      pdf.FormatCSV,
		".pptx":     pdf.FormatPPTX,
		".pptm":     pdf.FormatPPTX,
		".potx":     pdf.FormatPPTX,
		".ppsx":     pdf.FormatPPTX,
		".odp":      pdf.FormatODP,
		".fodp":     pdf.FormatODP,
		".otp":      pdf.FormatODP,
	}
)

// convertWarningJSON is one warning of the -f json report.
type convertWarningJSON struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Page    int    `json:"page,omitempty"`
}

// convertResult is the -f json report of litho convert.
type convertResult struct {
	File     string               `json:"file"`
	Output   string               `json:"output"`
	Format   string               `json:"format"`
	Pages    int                  `json:"pages"`
	Warnings []convertWarningJSON `json:"warnings"`
}

// stringsFlag is a flag that repeats, each value appended in order.
type stringsFlag []string

// String implements [flag.Value].
func (f *stringsFlag) String() string {
	if f == nil {
		return ""
	}
	return strings.Join(*f, ",")
}

// Set implements [flag.Value].
func (f *stringsFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

// marginFlag is the value of --margin: one length for every side, or four
// for top, right, bottom and left.
type marginFlag struct {
	set                      bool
	top, right, bottom, left float64
	text                     string
}

// errMargin reports a --margin value the flag does not accept.
var errMargin = errors.New("expected one length in points, or four separated by commas: top,right,bottom,left")

// String implements [flag.Value].
func (f *marginFlag) String() string {
	if f == nil {
		return ""
	}
	return f.text
}

// Set implements [flag.Value]. The library checks the range.
func (f *marginFlag) Set(value string) error {
	parts := strings.Split(value, ",")
	if len(parts) != 1 && len(parts) != 4 {
		return errMargin
	}
	v := make([]float64, len(parts))
	for i, p := range parts {
		n, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return errMargin
		}
		v[i] = n
	}
	if len(v) == 1 {
		v = []float64{v[0], v[0], v[0], v[0]}
	}
	f.set, f.top, f.right, f.bottom, f.left, f.text = true, v[0], v[1], v[2], v[3], value
	return nil
}

// runConvert converts a plain text or HTML file to a tagged PDF.
func runConvert(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("convert", flag.ContinueOnError)
	fs.SetOutput(stderr)
	from := fs.String("from", "", "Input format: text, html, markdown, docx, odt, xlsx, ods, csv, pptx or odp. Without it, the file extension decides: .txt and .text are text, .html and .htm are html, .md and .markdown are markdown, .docx is docx, .odt and .fodt are odt, "+
		".xlsx, .xlsm, .xltx and .xltm are xlsx, .ods, .fods and .ots are ods, .csv is csv, .pptx, .pptm, .potx and .ppsx are pptx, .odp, .fodp and .otp are odp; "+
		"for an http(s) URL, the Content-Type, then the URL's extension, then html")
	output := fs.String("o", "", "Output file (required), - for stdout")
	format := fs.String("f", "text", "Report format: text (warnings on stderr) or json (the report on stdout)")
	var paper pageSizeFlag
	fs.Var(&paper, "paper", "Page size: letter, legal, a3, a4, a5, or WIDTHxHEIGHT in points (default a4)")
	var margin marginFlag
	fs.Var(&margin, "margin", "Margins in points: one for every side, or top,right,bottom,left (default 56.693, 2 cm)")
	var fonts stringsFlag
	fs.Var(&fonts, "font", "Font family of a text file, repeatable in preference order (default Liberation Serif, then serif); HTML takes its fonts from its CSS")
	fontSize := fs.Float64("font-size", 0, "Font size in points of a text file (default 12); HTML takes its sizes from its CSS")
	media := fs.String("media", "print", "Media type the stylesheets of an HTML file are evaluated for: print or screen")
	resources := fs.String("resources", "", "Directory an HTML file's relative URLs (stylesheets, images) are read from (default: the input file's directory; none for standard input)")
	var fontDirs stringsFlag
	fs.Var(&fontDirs, "font-dir", "Directory of font files, searched recursively; repeatable. Without it, the system font directories")
	landscape := fs.Bool("landscape", false, "Swap the paper's width and height of an HTML file")
	preferCSS := fs.Bool("prefer-css-page-size", false, "Let an @page size of an HTML file set the page size; without it the paper is the page and such a document is scaled to fit it")
	scale := fs.Float64("scale", 1, "Scale of the content of an HTML file, from 0.1 to 2; the page keeps its size")
	hiddenSlides := fs.Bool("hidden-slides", false, "Write the slides a presentation hides from its show too")
	notesPages := fs.Bool("notes-pages", false, "Write a notes page after the slides of a presentation, one per slide")
	singlePage := fs.Bool("single-page", false, "Lay an HTML file out on one page as tall as its content, or each sheet of a spreadsheet on one page the size of its content")
	pages := fs.String("pages", "", "Pages to write, as 1-3,7, even, odd or last (default all)")
	background := fs.Bool("background", true, "Print the backgrounds of an HTML file; --background=false drops the html and body backgrounds, the canvas")
	outlineFlag := fs.Bool("outline", true, "Make the headings of an HTML file the document outline; --outline=false leaves it out")
	header := fs.String("header", "", "File of the HTML fragment drawn in the top margin of every page of an HTML file (classes date, title, url, pageNumber, totalPages)")
	footer := fs.String("footer", "", "File of the HTML fragment drawn in the bottom margin of every page of an HTML file")
	date := fs.String("date", "", "Date the header and footer of an HTML file or a spreadsheet show, RFC 3339 or now (default none)")
	baseURL := fs.String("base-url", "", "URL of the HTML file: shown by the url class of the header and footer; for an http(s) URL, relative references resolve against it "+
		"and the CLI fetches every http(s) resource the document names (30 s timeout a request); a service embedding litho brings its own HTTP client")
	var mediaFeatures stringsFlag
	fs.Var(&mediaFeatures, "media-feature", "Media feature an HTML file's @media rules see, as NAME=VALUE such as prefers-color-scheme=dark; repeatable")
	password := fs.String("password", "", "Password of an encrypted DOCX or ODT file; --landscape, --paper, --pages and --outline apply to such a file too, over its own page setup")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() != 1 || *output == "" {
		fmt.Fprintln(stderr, convertUsage)
		return exitError
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintf(stderr, "invalid -f %q: expected text or json\n", *format)
		return exitError
	}
	if *format == "json" && (*output == "-") {
		fmt.Fprintln(stderr, "-f json writes the report to stdout, so -o must name a file")
		return exitError
	}
	input := fs.Arg(0)
	remote := isRemoteInput(input)
	var inFormat pdf.Format
	var err error
	if !remote || *from != "" {
		if inFormat, err = convertFormat(*from, input); err != nil {
			fmt.Fprintln(stderr, err)
			return exitError
		}
	}
	for _, dir := range fontDirs {
		if info, err := os.Stat(dir); err != nil {
			fmt.Fprintf(stderr, "--font-dir: %v\n", err)
			return exitError
		} else if !info.IsDir() {
			fmt.Fprintf(stderr, "--font-dir: %s is not a directory\n", dir)
			return exitError
		}
	}

	if *media != "print" && *media != "screen" {
		fmt.Fprintf(stderr, "invalid --media %q: expected print or screen\n", *media)
		return exitError
	}
	printOpts, err := convertPrintOptions(*landscape, *preferCSS, *scale, *singlePage, *pages, *background, *outlineFlag, *header, *footer, *date, *baseURL, mediaFeatures)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	// A remote document is fetched first: its Content-Type may decide its
	// format.
	var src io.Reader = os.Stdin
	var client *http.Client
	if remote || isRemoteInput(*baseURL) {
		client = cliHTTPClient()
	}
	if remote {
		body, format, final, err := fetchInput(ctx, client, input, inFormat)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", input, err)
			return exitError
		}
		defer body.Close() //nolint:errcheck // read-only response body
		src, inFormat = body, format
		if *baseURL == "" {
			printOpts = append(printOpts, pdf.WithBaseURL(final))
		}
	}
	htmlLike := inFormat == pdf.FormatHTML || inFormat == pdf.FormatMarkdown
	officeLike := inFormat == pdf.FormatDOCX || inFormat == pdf.FormatODT
	resDir := *resources
	if resDir == "" && input != "-" && !remote {
		resDir = filepath.Dir(input)
	}
	if !htmlLike {
		resDir = ""
	}
	if resDir != "" {
		if info, err := os.Stat(resDir); err != nil {
			fmt.Fprintf(stderr, "--resources: %v\n", err)
			return exitError
		} else if !info.IsDir() {
			fmt.Fprintf(stderr, "--resources: %s is not a directory\n", resDir)
			return exitError
		}
	}

	opts := []pdf.ConvertOption{pdf.WithConvertFontDirs(fontDirs...), pdf.WithFontFamilies(fonts...), pdf.WithFontSize(*fontSize)}
	if htmlLike {
		opts = append(opts, pdf.WithMediaType(*media))
		opts = append(opts, printOpts...)
		if client != nil {
			opts = append(opts, pdf.WithHTTPClient(client))
		}
		if resDir != "" {
			// An os.Root refuses a path or a symbolic link that leaves
			// resDir, so the document reads nothing outside it.
			root, err := os.OpenRoot(resDir)
			if err != nil {
				fmt.Fprintf(stderr, "--resources: %v\n", err)
				return exitError
			}
			defer root.Close() //nolint:errcheck // read-only directory handle
			opts = append(opts, pdf.WithResources(root.FS()))
		}
	}
	if officeLike {
		opts = append(opts, pdf.WithLandscape(*landscape), pdf.WithPageRanges(*pages), pdf.WithOutline(*outlineFlag), pdf.WithConvertPassword(*password), pdf.WithHiddenSlides(*hiddenSlides), pdf.WithNotesPages(*notesPages))
	}
	if paper.set {
		opts = append(opts, pdf.WithPaperSize(paper.width, paper.height))
	}
	if margin.set {
		opts = append(opts, pdf.WithMargins(margin.top, margin.right, margin.bottom, margin.left))
	}
	if logger := cliLoggerFrom(ctx); logger != nil {
		opts = append(opts, pdf.WithConvertLogger(logger))
	}
	if limits, ok := cliLimitsFrom(ctx); ok {
		opts = append(opts, pdf.WithConvertLimits(limits))
	}

	if input != "-" && !remote {
		f, err := os.Open(input) //nolint:gosec // The input path is the command's own argument.
		if err != nil {
			fmt.Fprintf(stderr, "%v\n", err)
			return exitError
		}
		defer f.Close() //nolint:errcheck // read-only input
		src = f
	}

	dst, closeDst, err := createOutput(*output)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	report, err := pdf.Convert(ctx, src, inFormat, dst, opts...)
	if err != nil {
		abandonOutput(*output, closeDst)
		fmt.Fprintf(stderr, "%s: %v\n", input, err)
		return exitError
	}
	if err := closeDst(); err != nil {
		fmt.Fprintf(stderr, "closing output: %v\n", err)
		return exitError
	}

	if *format == "json" {
		res := convertResult{File: input, Output: *output, Format: string(inFormat), Pages: report.Pages, Warnings: []convertWarningJSON{}}
		for _, w := range report.Warnings {
			res.Warnings = append(res.Warnings, convertWarningJSON(w))
		}
		return jsonOut(stdout, stderr, res)
	}
	for _, w := range report.Warnings {
		if w.Page > 0 {
			fmt.Fprintf(stderr, "warning: page %d: %s: %s\n", w.Page, w.Code, w.Message)
		} else {
			fmt.Fprintf(stderr, "warning: %s: %s\n", w.Code, w.Message)
		}
	}
	return exitOK
}

// convertPrintOptions turns the print flags of litho convert into options
// of [pdf.Convert]: it reads the header and footer files, parses --date and
// splits every --media-feature at its first "=". The library checks the
// ranges.
func convertPrintOptions(landscape, preferCSS bool, scale float64, singlePage bool, pages string, background, outline bool, header, footer, date, baseURL string, mediaFeatures []string) ([]pdf.ConvertOption, error) {
	opts := []pdf.ConvertOption{
		pdf.WithLandscape(landscape),
		pdf.WithPreferCSSPageSize(preferCSS),
		pdf.WithPrintScale(scale),
		pdf.WithSinglePage(singlePage),
		pdf.WithPageRanges(pages),
		pdf.WithPrintBackground(background),
		pdf.WithOutline(outline),
		pdf.WithBaseURL(baseURL),
	}
	for _, f := range []struct {
		flag, path string
		with       func(string) pdf.ConvertOption
	}{{"--header", header, pdf.WithHeaderTemplate}, {"--footer", footer, pdf.WithFooterTemplate}} {
		if f.path == "" {
			continue
		}
		data, err := os.ReadFile(f.path) //nolint:gosec // The template path is the command's own argument.
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.flag, err)
		}
		opts = append(opts, f.with(string(data)))
	}
	switch date {
	case "":
	case "now":
		opts = append(opts, pdf.WithPrintDate(time.Now()))
	default:
		t, err := time.Parse(time.RFC3339, date)
		if err != nil {
			return nil, fmt.Errorf("--date: %w: expected RFC 3339, such as 2026-11-06T10:30:00Z, or now", err)
		}
		opts = append(opts, pdf.WithPrintDate(t))
	}
	if len(mediaFeatures) > 0 {
		features := make(map[string]string, len(mediaFeatures))
		for _, mf := range mediaFeatures {
			name, value, ok := strings.Cut(mf, "=")
			if !ok || name == "" {
				return nil, fmt.Errorf("--media-feature %q: expected NAME=VALUE", mf)
			}
			features[strings.ToLower(name)] = value
		}
		opts = append(opts, pdf.WithMediaFeatures(features))
	}
	return opts, nil
}

// convertFormat returns the format --from names, or, without it, the one the
// extension of path implies.
func convertFormat(from, path string) (pdf.Format, error) {
	if from != "" {
		f, ok := convertFormats[strings.ToLower(from)]
		if !ok {
			return "", fmt.Errorf("unsupported --from %q: litho convert reads text, html, markdown, docx, odt, xlsx, ods, csv, pptx and odp", from)
		}
		return f, nil
	}
	if path == "-" {
		return "", errors.New("standard input has no extension to tell its format: pass --from text, html, markdown, docx, odt, xlsx, ods, csv, pptx or odp")
	}
	ext := strings.ToLower(filepath.Ext(path))
	f, ok := convertExtensions[ext]
	if !ok {
		return "", fmt.Errorf("cannot tell the format of %s from its extension %q: pass --from text, html, markdown, docx, odt, xlsx, ods or csv", path, ext)
	}
	return f, nil
}
