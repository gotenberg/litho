package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/gotenberg/litho/pkg/pdf"
	"github.com/gotenberg/litho/test/integration/fixture"
)

// htmlSettings are the options one converter feature converts its sources
// with.
type htmlSettings struct {
	// Media is the media type the stylesheets are evaluated for.
	Media string
	// Width and Height are the paper size in points; both zero keep the
	// converter's default.
	Width, Height float64
	// Top, Right, Bottom and Left are the page margins in points.
	Top, Right, Bottom, Left float64
	// Resources is the file system relative URLs resolve in.
	Resources fs.FS
	// FontDirs are the font directories, in preference order.
	FontDirs []string
	// PreferCSSPageSize lets an @page size descriptor set the page: the
	// paper above is then the default paper of a page that names none.
	PreferCSSPageSize bool
	// Format is the source's format; empty converts HTML.
	Format fixture.Format
	// MarkdownTemplate is the template a Markdown source renders into, ""
	// for litho's own.
	MarkdownTemplate string
	// Print are the remaining print options of an HTML or Markdown source,
	// after the ones above: landscape, scale, page ranges, single page,
	// backgrounds, outline, header and footer templates and media features
	// (see fixture.Source.PrintOptions in test/integration/fixture).
	Print []pdf.HTMLOption
	// Office is an Office source, nil for an HTML or Markdown one. It
	// converts at FontDirs, Extra and the options of its options.json alone
	// (fixture.Office.Conversion): an Office document carries its own page
	// setup and styles.
	Office *fixture.Office
	// Extra are options every format takes, after all the others: the
	// version an output target asks for.
	Extra []pdf.ConvertOption
}

// htmlConverter converts an HTML source to a PDF. It is [convertHTML]
// outside tests; a test hands in a stub to drive the features without the
// converter.
type htmlConverter func(ctx context.Context, src []byte, s htmlSettings) ([]byte, error)

// convertHTML converts src with litho's HTML converter at the given
// settings and returns the PDF bytes, or, for an Office source, with the
// Office converter at its own options. The report's warnings are not an
// error: the oracles score what was drawn.
func convertHTML(ctx context.Context, src []byte, s htmlSettings) ([]byte, error) {
	c, err := conversion(src, s)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if _, err := c.WriteTo(ctx, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// conversion returns the conversion of src at s.
func conversion(src []byte, s htmlSettings) (*pdf.Conversion, error) {
	if s.Office != nil {
		opts := make([]pdf.ConvertOption, 0, 1+len(s.Extra))
		if len(s.FontDirs) > 0 {
			opts = append(opts, pdf.WithConvertFontDirs(s.FontDirs...))
		}
		return s.Office.Conversion(bytes.NewReader(src), append(opts, s.Extra...)...)
	}
	opts := []pdf.HTMLOption{
		pdf.WithMediaType(s.Media),
		pdf.WithMargins(s.Top, s.Right, s.Bottom, s.Left),
	}
	if s.Width > 0 && s.Height > 0 {
		paper := pdf.Paper{Width: s.Width, Height: s.Height}
		if s.PreferCSSPageSize {
			opts = append(opts, pdf.WithDefaultPaper(paper))
		} else {
			opts = append(opts, pdf.WithPaper(paper))
		}
	}
	if s.Resources != nil {
		opts = append(opts, pdf.WithResources(s.Resources))
	}
	if len(s.FontDirs) > 0 {
		opts = append(opts, pdf.WithConvertFontDirs(s.FontDirs...))
	}
	opts = append(opts, s.Print...)
	for _, o := range s.Extra {
		opts = append(opts, o)
	}
	format := s.Format
	if format == "" {
		format = fixture.FormatHTML
	}
	return fixture.Conversion(format, bytes.NewReader(src), s.MarkdownTemplate, opts...), nil
}

// sourceSettings reads a converter corpus source, a source directory or an
// Office document, and returns its document's bytes with the settings the
// diff and targets cells convert it at: the options its options.json names,
// and the font directories fonts. office reads it as an Office source.
func sourceSettings(srcPath string, office bool, fonts []string) ([]byte, htmlSettings, error) {
	if office {
		source, err := fixture.LoadOffice(srcPath)
		if err != nil {
			return nil, htmlSettings{}, fmt.Errorf("read: %w", err)
		}
		src, err := os.ReadFile(source.Document)
		if err != nil {
			return nil, htmlSettings{}, fmt.Errorf("read: %w", err)
		}
		return src, htmlSettings{FontDirs: fonts, Format: source.Format, Office: source}, nil
	}
	dir := srcPath
	if info, err := os.Stat(srcPath); err == nil && !info.IsDir() {
		dir = filepath.Dir(srcPath)
	}
	source, err := fixture.Load(dir)
	if err != nil {
		return nil, htmlSettings{}, fmt.Errorf("read: %w", err)
	}
	src, err := os.ReadFile(source.Document)
	if err != nil {
		return nil, htmlSettings{}, fmt.Errorf("read: %w", err)
	}
	width, height := source.Paper()
	top, right, bottom, left := source.Margins()
	return src, htmlSettings{
		Media:             source.MediaType(),
		Width:             width,
		Height:            height,
		Top:               top,
		Right:             right,
		Bottom:            bottom,
		Left:              left,
		Resources:         os.DirFS(dir),
		FontDirs:          fonts,
		PreferCSSPageSize: source.PreferCSSPageSize(),
		Format:            source.Format,
		MarkdownTemplate:  source.MarkdownTemplate,
		Print:             source.PrintOptions(),
	}, nil
}

// fixtureFontsRoot returns the directory the fixture fonts are fetched into:
// test/integration/testdata/fonts from the repository root.
func fixtureFontsRoot() string {
	for _, c := range []string{
		filepath.Join("test", "integration", "testdata", "fonts"),
		filepath.Join("testdata", "fonts"),
		filepath.Join("..", "..", "testdata", "fonts"),
	} {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	return filepath.Join("test", "integration", "testdata", "fonts")
}

// fixtureFamilyPrefixes are the font directories `corpus fetch --corpus=fonts`
// installs, the faces every converter oracle draws text with
// (fixture.FontFamilies). The fonts root also holds the test fonts (Ahem, the
// WeasyPrint test font), which only the oracles written for them read.
//
//nolint:gochecknoglobals // static list of directory prefixes
var fixtureFamilyPrefixes = fixture.FontFamilies

// errFontsMissing reports a font directory a converter feature needs and
// the testdata lacks.
var errFontsMissing = errors.New("fixture fonts missing")

// fixtureFontDirs returns the font directories under root a converter
// feature converts with: the fixture families, then each extra directory
// (a test font). It fails, naming the make target that fetches them, when one
// is missing.
func fixtureFontDirs(root, target string, extra ...string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("%w: %s unreadable (run %s): %w", errFontsMissing, root, target, err)
	}
	var dirs []string
	for _, prefix := range fixtureFamilyPrefixes {
		found := false
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
				dirs = append(dirs, filepath.Join(root, e.Name()))
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: no %s* directory under %s (run %s)", errFontsMissing, prefix, root, target)
		}
	}
	for _, name := range extra {
		p := filepath.Join(root, name)
		if info, err := os.Stat(p); err != nil || !info.IsDir() {
			return nil, fmt.Errorf("%w: no %s directory under %s (run %s)", errFontsMissing, name, root, target)
		}
		dirs = append(dirs, p)
	}
	return dirs, nil
}

// maxConvertedPages bounds the pages a converter feature renders of one
// conversion. Anti-DoS guard, not spec-mandated: a layout bug that loops
// pages would otherwise draw thousands. The longest source, the html
// corpus's article, runs to 6 pages in Chromium.
const maxConvertedPages = 64

// renderPDF opens a PDF held in memory and renders its first pages, at most
// maxPages and maxConvertedPages, at dpi over white. It returns the page
// count beside the images.
func renderPDF(ctx context.Context, data []byte, dpi float64, maxPages int) ([]image.Image, error) {
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open the conversion: %w", err)
	}
	defer r.Close()
	n, err := r.PageCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("count pages: %w", err)
	}
	last := min(n, maxConvertedPages)
	if maxPages > 0 {
		last = min(last, maxPages)
	}
	pages := make([]image.Image, 0, last)
	for p := 1; p <= last; p++ {
		img, err := r.RenderPage(ctx, p, pdf.WithRenderDPI(dpi))
		if err != nil {
			return nil, fmt.Errorf("render page %d: %w", p, err)
		}
		pages = append(pages, img)
	}
	return pages, nil
}

// reportName turns a fixture ID into a file name: path separators become
// double underscores, so the reports of one cell sit in one directory.
func reportName(id string) string {
	return strings.ReplaceAll(id, "/", "__")
}
