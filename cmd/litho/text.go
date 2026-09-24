package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/gotenberg/litho/pkg/pdf"
)

type textPageResult struct {
	Page int    `json:"page"`
	Text string `json:"text"`
}

type textResult struct {
	File  string           `json:"file"`
	Pages []textPageResult `json:"pages"`
}

// runText extracts and prints the text of the selected pages of each named
// file, every page when -pages is absent.
//
// # Corpus evidence
//
//   - pdfjs JBIG2Globals.pdf: a searchable scan whose OCR layer uses
//     rendering mode 3 extracts 0 words by default and 1459 with -invisible.
func runText(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("text", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("f", "text", "Output format: text or json")
	password := fs.String("p", "", "PDF password")
	pagesFlag := fs.String("pages", "", "Page range (e.g., 1-3,5,last)")
	// Text a viewer does not paint is still text. Rendering mode 3 (ISO 32000-1,
	// Table 106) is how a scanned page carries its OCR layer, so a searchable
	// scan reads as empty without -invisible.
	invisible := fs.Bool("invisible", false, "Include invisible text (rendering mode 3), such as an OCR layer")
	clipPath := fs.Bool("clip-path", false, "Include text the graphics-state clipping path (W/W*) excludes")
	clipped := fs.Bool("clipped", false, "Include text outside the visible CropBox")
	artifacts := fs.Bool("artifacts", false, "Include text marked as an /Artifact (headers, footers, watermarks)")
	hidden := fs.Bool("hidden", false, "Include all of the above")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(stderr, "Usage: litho text [-f json] [-p password] [--pages RANGE] <file> [file...]")
		return exitError
	}

	var results []textResult
	for _, path := range files {
		r, closer, err := openPDF(ctx, path, *password)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return exitError
		}

		pageCount, err := r.PageCount(ctx)
		if err != nil {
			closer()
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return exitError
		}

		var pageNums []int
		if *pagesFlag != "" {
			pageNums, err = parsePages(*pagesFlag, pageCount)
			if err != nil {
				closer()
				fmt.Fprintf(stderr, "%s: %v\n", path, err)
				return exitError
			}
		} else {
			for i := 1; i <= pageCount; i++ {
				pageNums = append(pageNums, i)
			}
		}

		res := textResult{File: path}
		for _, p := range pageNums {
			text, err := r.ExtractText(ctx, p, textOptions(*hidden, *invisible, *clipPath, *clipped, *artifacts)...)
			if err != nil {
				fmt.Fprintf(stderr, "%s page %d: %v\n", path, p, err)
				text = ""
			}
			res.Pages = append(res.Pages, textPageResult{Page: p, Text: text})
		}
		results = append(results, res)
		closer()
	}

	switch *format {
	case "json":
		if len(results) == 1 {
			return jsonOut(stdout, stderr, results[0])
		}
		return jsonOut(stdout, stderr, results)
	default:
		for _, res := range results {
			if len(results) > 1 {
				fmt.Fprintf(stdout, "--- %s ---\n", res.File)
			}
			for _, p := range res.Pages {
				text := strings.TrimSpace(p.Text)
				if text != "" {
					fmt.Fprintln(stdout, text)
				}
			}
		}
	}
	return exitOK
}

// textOptions turns the extraction flags into reader options. The reader
// already knows how to include each kind of unpainted text; the text command
// simply had no way to ask, so an OCR layer was unreachable from the CLI.
func textOptions(hidden, invisible, clipPath, clipped, artifacts bool) []pdf.ExtractTextOption {
	var opts []pdf.ExtractTextOption
	if hidden {
		opts = append(opts, pdf.WithHiddenText())
	}
	if invisible {
		opts = append(opts, pdf.WithInvisibleText())
	}
	if clipPath {
		opts = append(opts, pdf.WithClipPathText())
	}
	if clipped {
		opts = append(opts, pdf.WithClippedText())
	}
	if artifacts {
		opts = append(opts, pdf.WithArtifacts())
	}
	return opts
}
