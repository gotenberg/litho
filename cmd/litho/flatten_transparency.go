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
	// transparency, which the command does not flatten.
	TransparentAnnotations int `json:"transparentAnnotations"`
	// SkippedPages lists the pages whose content does not read, left as
	// they are.
	SkippedPages []int `json:"skippedPages,omitempty"`
}

// flattenTransparencyPage is one page of the JSON report.
type flattenTransparencyPage struct {
	Page            int     `json:"page"`
	Regions         int     `json:"regions"`
	DPI             float64 `json:"dpi,omitempty"`
	Rasterized      int     `json:"rasterized"`
	KeptText        int     `json:"keptText"`
	TextReordered   int     `json:"textReordered"`
	TransparentText int     `json:"transparentText"`
	GroupRemoved    bool    `json:"groupRemoved,omitempty"`
}

func runFlattenTransparency(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("flatten-transparency", flag.ContinueOnError)
	fs.SetOutput(stderr)
	defaultPageSize := registerDefaultPageSizeFlag(fs)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	format := fs.String("f", "text", "Report format: text or json")
	dpi := fs.Float64("dpi", 300, "Resolution transparent regions are composited at, in pixels per inch")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() < 1 || *output == "" {
		fmt.Fprintln(stderr, "Usage: litho flatten-transparency <file> [--dpi N] -o out.pdf")
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
	report, err := w.FlattenTransparency(ctx, pdf.WithFlattenDPI(*dpi))
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
	regions, approximated := 0, 0
	for _, p := range report.Pages {
		result.Pages = append(result.Pages, flattenTransparencyPage{
			Page:            p.Page,
			Regions:         p.Regions,
			DPI:             p.DPI,
			Rasterized:      p.Rasterized,
			KeptText:        p.KeptText,
			TextReordered:   p.TextReordered,
			TransparentText: p.TransparentText,
			GroupRemoved:    p.GroupRemoved,
		})
		regions += p.Regions
		approximated += p.TextReordered + p.TransparentText
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
	}
	if approximated > 0 {
		fmt.Fprintf(stdout, "Approximated %d text object(s): transparency on text it could not reproduce, or text now drawn above what covered it\n", approximated)
	}
	if len(result.SkippedPages) > 0 {
		fmt.Fprintf(stdout, "Left %d page(s) whose content does not read as they are: %v\n", len(result.SkippedPages), result.SkippedPages)
	}
	if result.TransparentAnnotations > 0 {
		fmt.Fprintf(stdout, "Left %d annotation(s) painting with transparency: annotations are not flattened\n", result.TransparentAnnotations)
	}
	return exitOK
}
