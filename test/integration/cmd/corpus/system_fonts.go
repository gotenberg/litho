package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/test/integration/fixture"
)

// runSystemFonts converts each source as the diff cell does, but with no
// font directory, so litho draws with the fonts it discovers on the system
// it runs on, and fails when a conversion fails, finds no font, or reports a
// character no font covers. CI runs it inside the Gotenberg image, on
// sources whose every character the image's fonts cover: it proves litho
// finds the fonts where Gotenberg runs it.
func runSystemFonts(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("system-fonts", flag.ExitOnError)
	if err := fs.Parse(args); err != nil || fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: corpus system-fonts SOURCE...")
		return 2
	}
	failed := 0
	for _, src := range fs.Args() {
		faces, err := convertWithSystemFonts(ctx, src)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", src, err)
			failed++
			continue
		}
		fmt.Printf("%s: %s\n", src, strings.Join(faces, ", "))
	}
	if failed > 0 {
		fmt.Fprintf(os.Stderr, "%d of %d sources failed\n", failed, fs.NArg())
		return 1
	}
	return 0
}

// convertWithSystemFonts converts src, an HTML or Markdown source directory
// or an Office source, at the options its options.json names and with no
// font directory, and returns the faces the PDF embeds. A warning that a
// character prints as no glyph is an error.
func convertWithSystemFonts(ctx context.Context, src string) ([]string, error) {
	office := !isFile(filepath.Join(src, fixture.HTMLDocument)) && !isFile(filepath.Join(src, fixture.MarkdownDocument))
	data, settings, err := sourceSettings(src, office, nil)
	if err != nil {
		return nil, err
	}
	c, err := conversion(data, settings)
	if err != nil {
		return nil, err
	}
	out, err := os.CreateTemp("", "litho-system-fonts-*.pdf")
	if err != nil {
		return nil, err
	}
	defer os.Remove(out.Name())
	report, err := c.WriteTo(ctx, out)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, w := range report.Warnings {
		if w.Code == models.WarningMissingGlyph {
			missing = append(missing, w.Message)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("characters print as no glyph: %s", strings.Join(missing, "; "))
	}
	faces, err := referenceFonts(ctx, out.Name())
	if err != nil {
		return nil, fmt.Errorf("read the conversion: %w", err)
	}
	if len(faces) == 0 {
		return nil, fmt.Errorf("the conversion embeds no font")
	}
	return faces, nil
}

// isFile reports whether p names a regular file.
func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}
