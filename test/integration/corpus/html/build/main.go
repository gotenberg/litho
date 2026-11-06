// Command build regenerates the html corpus's litho PDFs: it converts the
// index.html of every sources/<name>/ directory to pdfs/<name>.pdf with
// pdf.Convert, at the options the diff feature of test/integration/cmd/corpus
// scores with and the Chromium references were made with (chromium.sh), and
// with the pinned fixture fonts alone, so two machines write the same bytes.
//
// Run it from the repository root through `make html-fixtures`, which first
// downloads the fonts versions.env pins:
//
//	go run ./test/integration/corpus/html/build
//
// Each source is converted twice and the build fails when the two runs write
// different bytes. The warnings of a conversion are printed: they name what
// litho does not draw yet, which the diff feature scores.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gotenberg/litho/pkg/pdf"
)

// The page every source is converted on, in points: the page Chromium lays
// out for A4 with Gotenberg's default margins of 0.39 inch (a 595.92 by
// 841.92 point media box and 27.75 point margins, measured on the Chromium
// references), for the print media type. The diff feature (diffPaperWidth
// and its siblings) and chromium.sh use the same; keep the three in step.
const (
	paperWidth  = 595.92
	paperHeight = 841.92
	margin      = 27.75
	media       = "print"
)

// fixtureFamilies are the prefixes of the font directories
// corpus/compose/fetch-fonts.sh installs, the only ones the conversion reads:
// the fonts directory also holds test fonts other corpora draw with.
var fixtureFamilies = []string{"dejavu-", "liberation-", "noto-cjk-sans-", "noto-sans-thai-", "noto-sans-devanagari-"}

func main() {
	sources := flag.String("sources", "test/integration/corpus/html/sources", "Directory of the <name>/index.html sources")
	out := flag.String("out", "test/integration/corpus/html/pdfs", "Directory the PDFs are written to")
	fonts := flag.String("fonts", "test/integration/testdata/fonts", "Directory of the pinned fixture fonts (make html-fixtures fills it)")
	flag.Parse()

	if err := run(context.Background(), *sources, *out, *fonts); err != nil {
		fmt.Fprintf(os.Stderr, "html build: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, sourcesDir, outDir, fontsDir string) error {
	fontDirs, err := familyDirs(fontsDir)
	if err != nil {
		return err
	}
	names, err := filepath.Glob(filepath.Join(sourcesDir, "*", "index.html"))
	if err != nil {
		return fmt.Errorf("listing sources: %w", err)
	}
	if len(names) == 0 {
		return fmt.Errorf("no <name>/index.html source under %s", sourcesDir)
	}
	sort.Strings(names)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", outDir, err)
	}
	var failed []string
	for _, name := range names {
		if err := buildOne(ctx, name, outDir, fontDirs); err != nil {
			fmt.Fprintf(os.Stderr, "  html: %v\n", err)
			failed = append(failed, filepath.Base(filepath.Dir(name)))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d of %d sources failed: %s", len(failed), len(names), strings.Join(failed, ", "))
	}
	return nil
}

// familyDirs returns the subdirectories of dir that fetch-fonts.sh installs,
// sorted, failing when one family is missing.
func familyDirs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w (run make html-fixtures, which downloads the pinned fonts first)", dir, err)
	}
	var dirs []string
	for _, prefix := range fixtureFamilies {
		found := false
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
				dirs = append(dirs, filepath.Join(dir, e.Name()))
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("no %s* font directory under %s: run make html-fixtures, which downloads the pinned fonts first", prefix, dir)
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

func buildOne(ctx context.Context, source, outDir string, fontDirs []string) error {
	src, err := os.ReadFile(source) //nolint:gosec // committed corpus source
	if err != nil {
		return fmt.Errorf("reading %s: %w", source, err)
	}
	base := filepath.Base(filepath.Dir(source))
	opts := []pdf.ConvertOption{
		pdf.WithConvertFontDirs(fontDirs...),
		pdf.WithPaperSize(paperWidth, paperHeight),
		pdf.WithMargins(margin, margin, margin, margin),
		pdf.WithMediaType(media),
		pdf.WithResources(os.DirFS(filepath.Dir(source))),
	}
	first, report, err := convert(ctx, src, opts)
	if err != nil {
		return fmt.Errorf("converting %s: %w", base, err)
	}
	if len(report.Warnings) > 0 {
		var msgs []string
		for _, w := range report.Warnings {
			msgs = append(msgs, fmt.Sprintf("%s on page %d: %s", w.Code, w.Page, w.Message))
		}
		fmt.Printf("  html: %s: %d warnings:\n    %s\n", base, len(msgs), strings.Join(msgs, "\n    "))
	}
	second, _, err := convert(ctx, src, opts)
	if err != nil {
		return fmt.Errorf("converting %s again: %w", base, err)
	}
	if !bytes.Equal(first, second) {
		return fmt.Errorf("converting %s twice wrote different bytes", base)
	}
	dst := filepath.Join(outDir, base+".pdf")
	if err := os.WriteFile(dst, first, 0o644); err != nil { //nolint:gosec // committed fixture, world-readable like the rest of the tree
		return fmt.Errorf("writing %s: %w", dst, err)
	}
	fmt.Printf("  html: %s.pdf, %d pages, %d bytes\n", base, report.Pages, len(first))
	return nil
}

func convert(ctx context.Context, src []byte, opts []pdf.ConvertOption) ([]byte, *pdf.ConvertReport, error) {
	var buf bytes.Buffer
	report, err := pdf.Convert(ctx, bytes.NewReader(src), pdf.FormatHTML, &buf, opts...)
	if err != nil {
		return nil, nil, err
	}
	if report == nil {
		return nil, nil, errors.New("no report")
	}
	return buf.Bytes(), report, nil
}
