// Command build regenerates the litho/text corpus fixtures: it converts every
// UTF-8 plain text file of the sources directory to a PDF of the same base
// name with pdf.Convert, at fixed options and with the pinned fixture fonts
// alone, so two machines write the same bytes.
//
// Run it from the repository root through `make text-fixtures`, which
// first downloads the fonts versions.env pins:
//
//	go run ./test/integration/corpus/litho/text/build
//
// Each source is converted twice and the build fails when the two runs write
// different bytes, when the conversion reports a warning (a character no
// fixture font covers is drawn as a placeholder), or when the font directory
// holds no font.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gotenberg/litho/pkg/pdf"
)

// The layout every fixture is converted at: A4, 2 cm margins, the serif
// generic family at 12 points. The values are the converter's defaults,
// stated so a default that moves does not move the fixtures.
const (
	paperWidth  = 595.276
	paperHeight = 841.890
	margin      = 56.693
	fontSize    = 12
	fontFamily  = "serif"
)

func main() {
	sources := flag.String("sources", "test/integration/corpus/litho/text/sources", "Directory of the .txt sources")
	out := flag.String("out", "test/integration/corpus/litho/text/pdfs", "Directory the PDFs are written to")
	fonts := flag.String("fonts", "test/integration/testdata/fonts", "Directory of the pinned fixture fonts (make text-fixtures fills it)")
	flag.Parse()

	if err := run(context.Background(), *sources, *out, *fonts); err != nil {
		fmt.Fprintf(os.Stderr, "text build: %v\n", err)
		os.Exit(1)
	}
}

// fixtureFamilies are the prefixes of the font directories `corpus fetch --corpus=fonts`
// installs. The fonts directory also holds the test fonts other corpora draw
// with (Ahem, the WeasyPrint test font), which must not take part in the
// litho/text fixtures' font matching, so only these subdirectories are read.
var fixtureFamilies = []string{"dejavu-", "liberation-", "noto-cjk-sans-"}

func run(ctx context.Context, sourcesDir, outDir, fontsDir string) error {
	if err := checkFonts(fontsDir); err != nil {
		return err
	}
	fontDirs, err := familyDirs(fontsDir)
	if err != nil {
		return err
	}
	names, err := filepath.Glob(filepath.Join(sourcesDir, "*.txt"))
	if err != nil {
		return fmt.Errorf("listing sources: %w", err)
	}
	if len(names) == 0 {
		return fmt.Errorf("no .txt source under %s", sourcesDir)
	}
	sort.Strings(names)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", outDir, err)
	}
	opts := []pdf.ConvertOption{
		pdf.WithConvertFontDirs(fontDirs...),
		pdf.WithPaperSize(paperWidth, paperHeight),
		pdf.WithMargins(margin, margin, margin, margin),
		pdf.WithFontFamilies(fontFamily),
		pdf.WithFontSize(fontSize),
	}
	for _, name := range names {
		if err := buildOne(ctx, name, outDir, opts); err != nil {
			return err
		}
	}
	return nil
}

// familyDirs returns the subdirectories of dir that `corpus fetch --corpus=fonts` installs,
// sorted, failing when one family is missing.
func familyDirs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
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
			return nil, fmt.Errorf("no %s* font directory under %s: run make text-fixtures, which downloads the pinned fonts first", prefix, dir)
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

// checkFonts fails unless dir holds at least one font file, so a missing
// download reads as such rather than as a conversion that fell back to
// nothing.
func checkFonts(dir string) error {
	found := false
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch strings.ToLower(filepath.Ext(d.Name())) {
		case ".ttf", ".otf", ".ttc", ".otc":
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil || !found {
		return fmt.Errorf("no font under %s: run make text-fixtures, which downloads the pinned fonts first", dir)
	}
	return nil
}

func buildOne(ctx context.Context, source, outDir string, opts []pdf.ConvertOption) error {
	src, err := os.ReadFile(source) //nolint:gosec // committed corpus source
	if err != nil {
		return fmt.Errorf("reading %s: %w", source, err)
	}
	base := strings.TrimSuffix(filepath.Base(source), ".txt")
	first, report, err := convert(ctx, src, opts)
	if err != nil {
		return fmt.Errorf("converting %s: %w", base, err)
	}
	if len(report.Warnings) > 0 {
		var msgs []string
		for _, w := range report.Warnings {
			msgs = append(msgs, fmt.Sprintf("%s on page %d: %s", w.Code, w.Page, w.Message))
		}
		return fmt.Errorf("converting %s: %d warnings, so a fixture font is missing:\n  %s", base, len(msgs), strings.Join(msgs, "\n  "))
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
	fmt.Printf("  litho/text: %s.pdf, %d pages, %d bytes\n", base, report.Pages, len(first))
	return nil
}

func convert(ctx context.Context, src []byte, opts []pdf.ConvertOption) ([]byte, *pdf.ConvertReport, error) {
	var buf bytes.Buffer
	report, err := pdf.Convert(ctx, bytes.NewReader(src), pdf.FormatText, &buf, opts...)
	if err != nil {
		return nil, nil, err
	}
	if report == nil {
		return nil, nil, errors.New("no report")
	}
	return buf.Bytes(), report, nil
}
