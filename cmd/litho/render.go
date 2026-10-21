package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"

	"github.com/gotenberg/litho/pkg/pdf"
)

// runRender draws pages to PNG files, one per page, named as pdftoppm names
// them: the -o prefix, a hyphen, the page number and .png.
func runRender(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	fs.SetOutput(stderr)
	password := fs.String("p", "", "PDF password")
	pagesFlag := fs.String("pages", "", "Page range (e.g., 1-3,5,last); every page by default")
	dpi := fs.Float64("dpi", pdf.DefaultRenderDPI, "Resolution in pixels per inch")
	output := fs.String("o", "", "Output prefix: page N is written to PREFIX-N.png")
	fontDir := fs.String("font-dir", "",
		"Directory of TrueType, OpenType or CFF font files, searched recursively, to draw the fonts the document does not embed: a font takes the file of its name, its family, or a metric-compatible family (Arial for Helvetica); a font no file serves draws its glyphs as boxes")
	systemFonts := fs.Bool("system-fonts", false,
		"Also search the operating system's font directories, after --font-dir, for the fonts the document does not embed; the pages then depend on the fonts installed on this host")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	files := fs.Args()
	if len(files) != 1 || *output == "" {
		fmt.Fprintln(stderr, "Usage: litho render [-p password] [--pages RANGE] [--dpi N] [--font-dir DIR] [--system-fonts] -o PREFIX <file>")
		return exitError
	}
	opts := []pdf.RenderOption{pdf.WithRenderDPI(*dpi)}
	if *fontDir != "" {
		info, err := os.Stat(*fontDir)
		if err != nil {
			fmt.Fprintf(stderr, "--font-dir: %v\n", err)
			return exitError
		}
		if !info.IsDir() {
			fmt.Fprintf(stderr, "--font-dir: %s is not a directory\n", *fontDir)
			return exitError
		}
		opts = append(opts, pdf.WithRenderFontFiles(os.DirFS(*fontDir)))
	}
	if *systemFonts {
		opts = append(opts, pdf.WithRenderSystemFonts())
	}
	path := files[0]
	r, closer, err := openPDF(ctx, path, *password)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", path, err)
		return exitError
	}
	defer closer()

	pageCount, err := r.PageCount(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", path, err)
		return exitError
	}
	pageNums := make([]int, 0, pageCount)
	if *pagesFlag != "" {
		if pageNums, err = parsePages(*pagesFlag, pageCount); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return exitError
		}
	} else {
		for i := 1; i <= pageCount; i++ {
			pageNums = append(pageNums, i)
		}
	}
	for _, p := range pageNums {
		img, err := r.RenderPage(ctx, p, opts...)
		if err != nil {
			fmt.Fprintf(stderr, "%s page %d: %v\n", path, p, err)
			return exitError
		}
		name := fmt.Sprintf("%s-%d.png", *output, p)
		if err := writePNG(name, img); err != nil {
			fmt.Fprintf(stderr, "%s page %d: %v\n", path, p, err)
			return exitError
		}
		fmt.Fprintln(stdout, name)
	}
	return exitOK
}

// writePNG encodes img to the file name, replacing any file there.
func writePNG(name string, img image.Image) (err error) {
	f, err := os.Create(name) //nolint:gosec // the output path is the caller's argument
	if err != nil {
		return fmt.Errorf("creating %s: %w", name, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("closing %s: %w", name, cerr)
		}
	}()
	if err := png.Encode(f, img); err != nil {
		return fmt.Errorf("encoding %s: %w", name, err)
	}
	return nil
}
