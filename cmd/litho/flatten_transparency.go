package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/pdf"
)

// flattenTransparencyResult is the JSON report of flatten-transparency.
type flattenTransparencyResult struct {
	File  string                    `json:"file"`
	Pages []flattenTransparencyPage `json:"pages"`
	// TransparentAnnotations counts the annotations left painting with
	// transparency, which the command does not draw into the page.
	TransparentAnnotations int `json:"transparentAnnotations"`
	// SkippedPages lists the pages whose content does not read, left as
	// they are.
	SkippedPages []int `json:"skippedPages,omitempty"`
}

// flattenTransparencyPage is one page of the JSON report.
type flattenTransparencyPage struct {
	Page                  int     `json:"page"`
	Regions               int     `json:"regions"`
	DPI                   float64 `json:"dpi,omitempty"`
	Rasterized            int     `json:"rasterized"`
	KeptText              int     `json:"keptText"`
	TextUnderTransparency int     `json:"textUnderTransparency"`
	TransparentText       int     `json:"transparentText"`
	GroupRemoved          bool    `json:"groupRemoved,omitempty"`
	Annotations           int     `json:"annotations,omitempty"`
	EmptiedAnnotations    int     `json:"emptiedAnnotations,omitempty"`
	JPEGImages            int     `json:"jpegImages,omitempty"`
}

func runFlattenTransparency(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("flatten-transparency", flag.ContinueOnError)
	fs.SetOutput(stderr)
	defaultPageSize := registerDefaultPageSizeFlag(fs)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	format := fs.String("f", "text", "Report format: text or json")
	dpi := fs.Float64("dpi", 300, "Resolution transparent regions are composited at, in pixels per inch")
	printed := fs.Bool("printed-annotations", false, "Draw every printed annotation into the page, not only those painting with transparency")
	jpegQuality := fs.Int("jpeg-quality", 0, "Encode a composite of images and shadings only as JPEG at this quality, 1 to 100, unless Flate is no larger (default: every composite lossless)")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() < 1 || *output == "" {
		fmt.Fprintln(stderr, "Usage: litho flatten-transparency <file> [--dpi N] [--printed-annotations] [--jpeg-quality Q] -o out.pdf")
		return exitError
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintf(stderr, "unknown format %q. Valid: text, json\n", *format)
		return exitError
	}

	r, closer, err := openPDF(ctx, fs.Arg(0), *password)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	defer closer()

	// This command has no encryption flag, so it is never encrypting.
	warnEncryptionDropped(stderr, r)

	w := pdf.NewWriter(r, defaultPageSize.writeOptions()...)
	defer releaseWriter(w, stderr)

	// Before the output file exists: a refused flattening leaves nothing
	// behind.
	opts := []pdf.FlattenOption{pdf.WithFlattenDPI(*dpi), pdf.WithFlattenJPEGQuality(*jpegQuality)}
	if *printed {
		opts = append(opts, pdf.WithFlattenPrintedAnnotations())
	}
	report, err := w.FlattenTransparency(ctx, opts...)
	if err != nil {
		fmt.Fprintf(stderr, "flattening transparency: %v\n", err)
		return exitError
	}

	dst, closeDst, err := createOutput(*output)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	// A document with nothing to flatten is rewritten whole all the same, so
	// the output is what the encryption warning above says it is, as every
	// other mutation command writes it.
	if err := w.WriteTo(ctx, dst, pdf.WithFullRewrite()); err != nil {
		abandonOutput(*output, closeDst)
		fmt.Fprintf(stderr, "writing output: %v\n", err)
		return exitError
	}
	if err := closeDst(); err != nil {
		fmt.Fprintf(stderr, "closing output: %v\n", err)
		return exitError
	}

	// -o - writes the document to stdout, so the report goes to stderr. See
	// reportWriter.
	return printFlattenTransparencyReport(reportWriter(*output, stdout, stderr), stderr, *format, fs.Arg(0), report)
}

// printFlattenTransparencyReport writes the report in format, text or json.
func printFlattenTransparencyReport(stdout, stderr io.Writer, format, file string, report *pdf.FlattenReport) int {
	result := flattenTransparencyResult{
		File:                   file,
		Pages:                  []flattenTransparencyPage{},
		TransparentAnnotations: report.TransparentAnnotations,
		SkippedPages:           report.SkippedPages,
	}
	regions, approximated, annotations, emptied := 0, 0, 0, 0
	for _, p := range report.Pages {
		result.Pages = append(result.Pages, flattenTransparencyPage{
			Page:                  p.Page,
			Regions:               p.Regions,
			DPI:                   p.DPI,
			Rasterized:            p.Rasterized,
			KeptText:              p.KeptText,
			TextUnderTransparency: p.TextUnderTransparency,
			TransparentText:       p.TransparentText,
			GroupRemoved:          p.GroupRemoved,
			Annotations:           p.Annotations,
			EmptiedAnnotations:    p.EmptiedAnnotations,
			JPEGImages:            p.JPEGImages,
		})
		regions += p.Regions
		annotations += p.Annotations
		emptied += p.EmptiedAnnotations
		approximated += p.TextUnderTransparency + p.TransparentText
	}
	if format == "json" {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			fmt.Fprintf(stderr, "encoding report: %v\n", err)
			return exitError
		}
		return exitOK
	}
	fmt.Fprintf(stdout, "Flattened %d page(s): %d region(s) composited\n", len(result.Pages), regions)
	for _, p := range result.Pages {
		if p.Regions == 0 {
			continue
		}
		fmt.Fprintf(stdout, "Page %d: %d region(s) at %g dpi, %d object(s) rasterized, %d text object(s) kept as text\n",
			p.Page, p.Regions, p.DPI, p.Rasterized, p.KeptText)
		if p.JPEGImages > 0 {
			fmt.Fprintf(stdout, "Page %d: %d composite(s) encoded as JPEG\n", p.Page, p.JPEGImages)
		}
	}
	if annotations > 0 {
		fmt.Fprintf(stdout, "Drew %d annotation(s) into the page content\n", annotations)
	}
	if emptied > 0 {
		fmt.Fprintf(stdout, "Removed the transparent appearance of %d annotation(s) a viewer does not display\n", emptied)
	}
	if approximated > 0 {
		fmt.Fprintf(stdout, "Approximated %d text object(s): transparency on text it could not reproduce, or text drawn above the transparency that showed it tinted\n", approximated)
	}
	if len(result.SkippedPages) > 0 {
		fmt.Fprintf(stdout, "Left %d page(s) whose content does not read as they are: %v\n", len(result.SkippedPages), result.SkippedPages)
	}
	if result.TransparentAnnotations > 0 {
		fmt.Fprintf(stdout, "Left %d annotation(s) painting with transparency: form fields (flatten-form draws them), pop-ups, and annotations litho cannot draw\n", result.TransparentAnnotations)
	}
	return exitOK
}
