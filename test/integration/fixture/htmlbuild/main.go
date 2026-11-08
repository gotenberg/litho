// Command htmlbuild regenerates the litho PDFs of an html corpus: it converts
// the document of every <corpus>/sources/<name>/ directory, index.html or
// index.md, to <corpus>/pdfs/<name>.pdf with pdf.Convert, at the options the
// directory's options.json names (read through the fixture package, as the
// diff feature of test/integration/cmd/corpus reads it and lib/chromium.sh
// posts it to Chromium), and with the pinned fixture fonts alone, so two
// machines write the same bytes.
//
// Run it from the repository root through `make html-fixtures`, which first
// downloads the fonts versions.env pins and builds litho/html and
// gotenberg/html:
//
//	go run ./test/integration/fixture/htmlbuild -corpus test/integration/corpus/litho/html
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
	"github.com/gotenberg/litho/test/integration/fixture"
)

func main() {
	corpus := flag.String("corpus", "", "Corpus directory holding sources/<name>/ (test/integration/corpus/litho/html or gotenberg/html)")
	fonts := flag.String("fonts", "test/integration/testdata/fonts", "Directory of the pinned fixture fonts (make html-fixtures fills it)")
	flag.Parse()
	if *corpus == "" {
		fmt.Fprintln(os.Stderr, "html build: -corpus is required")
		os.Exit(2)
	}

	if err := run(context.Background(), filepath.Join(*corpus, "sources"), filepath.Join(*corpus, "pdfs"), *fonts); err != nil {
		fmt.Fprintf(os.Stderr, "html build: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, sourcesDir, outDir, fontsDir string) error {
	fontDirs, err := familyDirs(fontsDir)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(sourcesDir)
	if err != nil {
		return fmt.Errorf("listing sources: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, filepath.Join(sourcesDir, e.Name()))
		}
	}
	if len(names) == 0 {
		return fmt.Errorf("no <name>/ source under %s", sourcesDir)
	}
	sort.Strings(names)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", outDir, err)
	}
	var failed []string
	for _, name := range names {
		if err := buildOne(ctx, name, outDir, fontDirs); err != nil {
			fmt.Fprintf(os.Stderr, "  html: %v\n", err)
			failed = append(failed, filepath.Base(name))
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
	for _, prefix := range fixture.FontFamilies {
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

func buildOne(ctx context.Context, dir, outDir string, fontDirs []string) error {
	base := filepath.Base(dir)
	s, err := fixture.Load(dir)
	if err != nil {
		return fmt.Errorf("reading %s: %w", base, err)
	}
	src, err := os.ReadFile(s.Document)
	if err != nil {
		return fmt.Errorf("reading %s: %w", s.Document, err)
	}
	width, height := s.Paper()
	top, right, bottom, left := s.Margins()
	printOpts := s.PrintOptions()
	opts := make([]pdf.ConvertOption, 0, 6+len(printOpts))
	opts = append(opts,
		pdf.WithConvertFontDirs(fontDirs...),
		pdf.WithPaperSize(width, height),
		pdf.WithMargins(top, right, bottom, left),
		pdf.WithMediaType(s.MediaType()),
		pdf.WithPreferCSSPageSize(s.PreferCSSPageSize()),
		pdf.WithResources(os.DirFS(dir)),
	)
	opts = append(opts, printOpts...)
	first, report, err := convert(ctx, src, s.Format, opts)
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
	second, _, err := convert(ctx, src, s.Format, opts)
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

func convert(ctx context.Context, src []byte, format pdf.Format, opts []pdf.ConvertOption) ([]byte, *pdf.ConvertReport, error) {
	var buf bytes.Buffer
	report, err := pdf.Convert(ctx, bytes.NewReader(src), format, &buf, opts...)
	if err != nil {
		return nil, nil, err
	}
	if report == nil {
		return nil, nil, errors.New("no report")
	}
	return buf.Bytes(), report, nil
}
