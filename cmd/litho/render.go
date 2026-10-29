package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"

	"github.com/gotenberg/litho/pkg/pdf"
)

// renderResult is the JSON report of litho render --report.
type renderResult struct {
	File  string       `json:"file"`
	Pages []renderPage `json:"pages"`
}

// renderPage is one page of the JSON report: the file it was written to and
// what [pdf.RenderReport] counts.
type renderPage struct {
	Page                         int    `json:"page"`
	Image                        string `json:"image"`
	PlaceholderGlyphs            int    `json:"placeholderGlyphs"`
	UnreadableGlyphs             int    `json:"unreadableGlyphs"`
	SynthesizedAnnotations       int    `json:"synthesizedAnnotations"`
	AnnotationsWithoutAppearance int    `json:"annotationsWithoutAppearance"`
	StaleAppearances             int    `json:"staleAppearances"`
	TransparencyOverBudget       int    `json:"transparencyOverBudget"`
	CoarseMasks                  int    `json:"coarseMasks"`
	Skipped                      int    `json:"skipped"`
}

// runRender draws pages to image files, one per page, named as pdftoppm names
// them: the -o prefix, a hyphen, the page number and .png, or .tif for a CMYK
// render, which a PNG file cannot hold. With --report it prints, instead of
// the file names, a JSON report of the pages written and what each one
// approximated.
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
	forPrint := fs.Bool("print", false, "Draw the annotations a viewer prints rather than those it shows on screen")
	reportFlag := fs.Bool("report", false,
		"Print a JSON report instead of the file names: each page's image and what it approximated (placeholder glyphs, synthesised or missing annotation appearances, stale widget appearances, transparency over the render budget, coarse soft masks, damaged constructs left out)")
	cmykProfile := fs.String("cmyk-profile", "", cmykProfileUsage)
	boxFlag := fs.String("box", "crop", "Page boundary to draw: crop, media, bleed, trim or art")
	colorModel := fs.String("color-model", "rgb", "Colour model of the pages: rgb (PNG files) or cmyk (separated TIFF files of inks)")
	outputProfile := fs.String("output-profile", "", "ICC profile of the CMYK device --color-model cmyk draws for (default: the document's CMYK output intent, else the built-in SWOP profile)")
	overprint := fs.Bool("overprint", false,
		"Simulate overprinting, as a CMYK printing device shows it: the page composites in CMYK and a painting under overprint leaves the process inks it does not paint as they were")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	files := fs.Args()
	if len(files) != 1 || *output == "" {
		fmt.Fprintln(stderr, "Usage: litho render [-p password] [--pages RANGE] [--dpi N] [--font-dir DIR] [--system-fonts] [--print] [--report] [--cmyk-profile profile.icc] [--box crop|media|bleed|trim|art] [--color-model rgb|cmyk] [--output-profile profile.icc] [--overprint] -o PREFIX <file>")
		return exitError
	}
	box, ok := renderBoxes[*boxFlag]
	if !ok {
		fmt.Fprintf(stderr, "unknown --box %q. Valid: crop, media, bleed, trim, art\n", *boxFlag)
		return exitError
	}
	opts := []pdf.RenderOption{pdf.WithRenderDPI(*dpi), pdf.WithRenderBox(box)}
	cmyk := false
	switch *colorModel {
	case "rgb":
	case "cmyk":
		cmyk = true
		opts = append(opts, pdf.WithRenderColorModel(pdf.RenderCMYK))
	default:
		fmt.Fprintf(stderr, "unknown --color-model %q. Valid: rgb, cmyk\n", *colorModel)
		return exitError
	}
	if *outputProfile != "" {
		if !cmyk {
			fmt.Fprintln(stderr, "--output-profile needs --color-model cmyk")
			return exitError
		}
		profile, err := os.ReadFile(filepath.Clean(*outputProfile))
		if err != nil {
			fmt.Fprintf(stderr, "reading the output profile: %v\n", err)
			return exitError
		}
		opts = append(opts, pdf.WithRenderCMYKProfile(profile))
	}
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
	extra, err := cmykProfileOptions(*cmykProfile)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	if *overprint {
		opts = append(opts, pdf.WithOverprintSimulation())
	}
	path := files[0]
	r, closer, err := openPDF(ctx, path, *password, extra...)
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
		if pageNums, err = pdf.ParsePageRanges(*pagesFlag, pageCount); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return exitError
		}
	} else {
		for i := 1; i <= pageCount; i++ {
			pageNums = append(pageNums, i)
		}
	}
	target := pdf.RenderForView
	if *forPrint {
		target = pdf.RenderForPrint
	}
	opts = append(opts, pdf.WithRenderTarget(target))
	result := renderResult{File: path, Pages: make([]renderPage, 0, len(pageNums))}
	for _, p := range pageNums {
		img, rep, err := r.RenderPageReport(ctx, p, opts...)
		if err != nil {
			fmt.Fprintf(stderr, "%s page %d: %v\n", path, p, err)
			return exitError
		}
		name := fmt.Sprintf("%s-%d.png", *output, p)
		if c, ok := img.(*image.CMYK); ok {
			name = fmt.Sprintf("%s-%d.tif", *output, p)
			err = writeCMYKTIFF(name, c, *dpi)
		} else {
			err = writePNG(name, img)
		}
		if err != nil {
			fmt.Fprintf(stderr, "%s page %d: %v\n", path, p, err)
			return exitError
		}
		if !*reportFlag {
			fmt.Fprintln(stdout, name)
			continue
		}
		result.Pages = append(result.Pages, renderPage{
			Page:                         p,
			Image:                        name,
			PlaceholderGlyphs:            rep.PlaceholderGlyphs,
			UnreadableGlyphs:             rep.UnreadableGlyphs,
			SynthesizedAnnotations:       rep.SynthesizedAnnotations,
			AnnotationsWithoutAppearance: rep.AnnotationsWithoutAppearance,
			StaleAppearances:             rep.StaleAppearances,
			TransparencyOverBudget:       rep.TransparencyOverBudget,
			CoarseMasks:                  rep.CoarseMasks,
			Skipped:                      rep.Skipped,
		})
	}
	if *reportFlag {
		if err := outputJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "writing report: %v\n", err)
			return exitError
		}
	}
	return exitOK
}

// renderBoxes maps the --box values to the page boundaries they select.
var renderBoxes = map[string]pdf.RenderBox{
	"crop":  pdf.RenderCropBox,
	"media": pdf.RenderMediaBox,
	"bleed": pdf.RenderBleedBox,
	"trim":  pdf.RenderTrimBox,
	"art":   pdf.RenderArtBox,
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
