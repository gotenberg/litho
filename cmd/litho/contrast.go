package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

type contrastPageResult struct {
	Page   int                   `json:"page"`
	Report models.ContrastReport `json:"report"`
	Error  string                `json:"error,omitempty"`
}

type contrastResult struct {
	File  string               `json:"file"`
	Pages []contrastPageResult `json:"pages"`
}

// runContrast measures the contrast of each selected page's text against what
// the page shows under it and prints the runs short of WCAG 2.x, Success
// Criterion 1.4.3 (1.4.6 with -enhanced). The check is advisory: PDF/UA sets
// no contrast rule, so a finding never makes the command fail; it exits 1 only
// when a file or page cannot be read.
func runContrast(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("contrast", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("f", "text", "Output format: text or json")
	password := fs.String("p", "", "PDF password")
	pagesFlag := fs.String("pages", "", "Page range (e.g., 1-3,5,last)")
	enhanced := fs.Bool("enhanced", false, "Judge WCAG 2.x 1.4.6 (AAA: 7:1, 4.5:1 large) rather than 1.4.3 (AA: 4.5:1, 3:1 large)")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(stderr, "Usage: litho contrast [-f json] [-p password] [--pages RANGE] [--enhanced] <file> [file...]")
		return exitError
	}
	var opts []pdf.ContrastOption
	if *enhanced {
		opts = append(opts, pdf.WithEnhancedContrast())
	}

	code := exitOK
	var results []contrastResult
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
		pageNums := make([]int, 0, pageCount)
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
		res := contrastResult{File: path}
		for _, p := range pageNums {
			rep, err := r.CheckTextContrast(ctx, p, opts...)
			page := contrastPageResult{Page: p, Report: rep}
			if err != nil {
				fmt.Fprintf(stderr, "%s page %d: %v\n", path, p, err)
				page.Error = err.Error()
				code = exitError
			}
			res.Pages = append(res.Pages, page)
		}
		results = append(results, res)
		closer()
	}

	switch *format {
	case "json":
		var out int
		if len(results) == 1 {
			out = jsonOut(stdout, stderr, results[0])
		} else {
			out = jsonOut(stdout, stderr, results)
		}
		if out != exitOK {
			return out
		}
	default:
		for _, res := range results {
			if len(results) > 1 {
				fmt.Fprintf(stdout, "--- %s ---\n", res.File)
			}
			for _, p := range res.Pages {
				for _, f := range p.Report.Failures {
					fmt.Fprintf(stdout, "page %d: %.2f:1 < %g:1 (%.1fpt%s) #%02x%02x%02x on #%02x%02x%02x%s: %s\n",
						p.Page, f.Ratio, f.Required, f.FontSize, boldMark(f.Bold),
						f.TextColor[0], f.TextColor[1], f.TextColor[2],
						f.BackgroundColor[0], f.BackgroundColor[1], f.BackgroundColor[2],
						variesMark(f.BackgroundVaries), strings.TrimSpace(f.Text))
				}
			}
		}
	}
	return code
}

func boldMark(bold bool) string {
	if bold {
		return " bold"
	}
	return ""
}

func variesMark(varies bool) string {
	if varies {
		return " (varied background)"
	}
	return ""
}
