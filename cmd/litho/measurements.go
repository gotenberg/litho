package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/models"
)

type measurementsPageResult struct {
	Page         int                  `json:"page"`
	Measurements []models.Measurement `json:"measurements"`
}

type measurementsResult struct {
	File  string                   `json:"file"`
	Pages []measurementsPageResult `json:"pages"`
}

func runMeasurements(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("measurements", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("f", "text", "Output format: text or json")
	password := fs.String("p", "", "PDF password")
	pagesFlag := fs.String("pages", "", "Page range (e.g., 1-3,5,last)")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return exitError
	}

	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(stderr, "Usage: litho measurements [-f json] [-p password] [--pages RANGE] <file> [file...]")
		return exitError
	}

	var results []measurementsResult
	for _, path := range files {
		r, closer, pageNums, err := resolvePages(ctx, path, *password, *pagesFlag, stderr)
		if err != nil {
			return exitError
		}

		res := measurementsResult{File: path}
		for _, p := range pageNums {
			ms, err := r.ExtractMeasurements(ctx, p)
			if err != nil {
				fmt.Fprintf(stderr, "%s page %d: %v\n", path, p, err)
				continue
			}
			if len(ms) == 0 {
				continue
			}
			res.Pages = append(res.Pages, measurementsPageResult{Page: p, Measurements: ms})
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
			for _, pg := range res.Pages {
				fmt.Fprintf(stdout, "Page %d:\n", pg.Page)
				for _, m := range pg.Measurements {
					if m.Name != "" {
						fmt.Fprintf(stdout, "  %s (%s)\n", m.Source, m.Name)
					} else {
						fmt.Fprintf(stdout, "  %s\n", m.Source)
					}
					fmt.Fprintf(stdout, "    X: 1 pt = %v %s\n", m.ScaleX.ConversionFactor, m.ScaleX.Unit)
					fmt.Fprintf(stdout, "    Y: 1 pt = %v %s\n", m.ScaleY.ConversionFactor, m.ScaleY.Unit)
					if m.AreaUnit != "" {
						fmt.Fprintf(stdout, "    Area: %s\n", m.AreaUnit)
					}
				}
			}
		}
	}
	return exitOK
}
