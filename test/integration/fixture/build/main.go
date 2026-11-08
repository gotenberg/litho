// Command build regenerates the litho PDFs of a gotenberg Office corpus: it
// converts the document of every <corpus>/sources/<name>/
// directory to <corpus>/pdfs/<name>.pdf with pdf.Convert, at the options the
// directory's options.json names (read through the fixture package, as the
// diff feature of test/integration/cmd/corpus reads it and lib/libreoffice.sh
// posts it to LibreOffice), and with the pinned fixture fonts alone, so two
// machines write the same bytes. The libreoffice corpora are scored by the
// diff feature but not built here: their documents are not committed.
//
// Run it from the repository root through `make office-fixtures`, which
// first downloads the fonts versions.env pins:
//
//	go run ./test/integration/fixture/build -corpus test/integration/corpus/gotenberg/docx
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
	corpus := flag.String("corpus", "", "Corpus directory holding sources/<name>/ (test/integration/corpus/gotenberg/docx, odt, xlsx, ods, csv, pptx or odp)")
	fonts := flag.String("fonts", "test/integration/testdata/fonts", "Directory of the pinned fixture fonts (make office-fixtures fills it)")
	flag.Parse()
	if *corpus == "" {
		fmt.Fprintln(os.Stderr, "office build: -corpus is required")
		os.Exit(2)
	}
	if err := run(context.Background(), *corpus, *fonts); err != nil {
		fmt.Fprintf(os.Stderr, "office build: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, corpusDir, fontsDir string) error {
	fontDirs, err := familyDirs(fontsDir)
	if err != nil {
		return err
	}
	sourcesDir := filepath.Join(corpusDir, "sources")
	entries, err := os.ReadDir(sourcesDir)
	if err != nil {
		return fmt.Errorf("listing sources: %w", err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(sourcesDir, e.Name()))
		}
	}
	if len(dirs) == 0 {
		return fmt.Errorf("no <name>/ source under %s", sourcesDir)
	}
	sort.Strings(dirs)
	outDir := filepath.Join(corpusDir, "pdfs")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", outDir, err)
	}
	label := filepath.Base(corpusDir)
	var failed []string
	for _, dir := range dirs {
		if err := buildOne(ctx, label, dir, outDir, fontDirs); err != nil {
			fmt.Fprintf(os.Stderr, "  %s: %v\n", label, err)
			failed = append(failed, filepath.Base(dir))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d of %d sources failed: %s", len(failed), len(dirs), strings.Join(failed, ", "))
	}
	return nil
}

// familyDirs returns the subdirectories of dir that fetch-fonts.sh installs,
// sorted, failing when one family is missing.
func familyDirs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w (run make office-fixtures, which downloads the pinned fonts first)", dir, err)
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
			return nil, fmt.Errorf("no %s* font directory under %s: run make office-fixtures, which downloads the pinned fonts first", prefix, dir)
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

func buildOne(ctx context.Context, label, dir, outDir string, fontDirs []string) error {
	base := filepath.Base(dir)
	s, err := fixture.LoadOffice(dir)
	if err != nil {
		return fmt.Errorf("reading %s: %w", base, err)
	}
	src, err := os.ReadFile(s.Document)
	if err != nil {
		return fmt.Errorf("reading %s: %w", s.Document, err)
	}
	opts := append([]pdf.ConvertOption{pdf.WithConvertFontDirs(fontDirs...)}, s.ConvertOptions()...)
	first, report, err := convert(ctx, src, s.Format, opts)
	if err != nil {
		return fmt.Errorf("converting %s: %w", base, err)
	}
	if len(report.Warnings) > 0 {
		var msgs []string
		for _, w := range report.Warnings {
			msgs = append(msgs, fmt.Sprintf("%s on page %d: %s", w.Code, w.Page, w.Message))
		}
		fmt.Printf("  %s: %s: %d warnings:\n    %s\n", label, base, len(msgs), strings.Join(msgs, "\n    "))
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
	fmt.Printf("  %s: %s.pdf, %d pages, %d bytes\n", label, base, report.Pages, len(first))
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
