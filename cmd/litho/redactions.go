package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/models"
)

type redactionsPageResult struct {
	Page         int                  `json:"page"`
	RedactedText []models.TextElement `json:"redactedText"`
}

type redactionsResult struct {
	File  string                 `json:"file"`
	Pages []redactionsPageResult `json:"pages"`
}

func runRedactions(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("redactions", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("f", "text", "Output format: text or json")
	password := fs.String("p", "", "PDF password")
	pagesFlag := fs.String("pages", "", "Page range (e.g., 1-3,5,last)")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return exitError
	}

	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(stderr, "Usage: litho redactions [-f json] [-p password] [--pages RANGE] <file> [file...]")
		return exitError
	}

	var results []redactionsResult
	for _, path := range files {
		r, closer, pageNums, err := resolvePages(ctx, path, *password, *pagesFlag, stderr)
		if err != nil {
			return exitError
		}

		res := redactionsResult{File: path}
		for _, p := range pageNums {
			rt, err := r.ExtractRedactedText(ctx, p)
			if err != nil {
				fmt.Fprintf(stderr, "%s page %d: %v\n", path, p, err)
				continue
			}
			if len(rt) == 0 {
				continue
			}
			res.Pages = append(res.Pages, redactionsPageResult{Page: p, RedactedText: rt})
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
				fmt.Fprintf(stdout, "Page %d:\n", p.Page)
				for _, el := range p.RedactedText {
					fmt.Fprintf(stdout, "  (%g, %g) %s\n", el.X, el.Y, el.Text)
				}
			}
		}
	}
	return exitOK
}
