package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/gotenberg/litho/pkg/models"
)

type tablesPageResult struct {
	Page   int            `json:"page"`
	Tables []models.Table `json:"tables"`
}

type tablesResult struct {
	File  string             `json:"file"`
	Pages []tablesPageResult `json:"pages"`
}

func runTables(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tables", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("f", "text", "Output format: text or json")
	password := fs.String("p", "", "PDF password")
	pagesFlag := fs.String("pages", "", "Page range (e.g., 1-3,5,last)")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(stderr, "Usage: litho tables [-f json] [-p password] [--pages RANGE] <file> [file...]")
		return exitError
	}

	var results []tablesResult
	for _, path := range files {
		r, closer, pageNums, err := resolvePages(ctx, path, *password, *pagesFlag, stderr)
		if err != nil {
			return exitError
		}

		res := tablesResult{File: path}
		for _, p := range pageNums {
			tables, err := r.ExtractTables(ctx, p)
			if err != nil {
				fmt.Fprintf(stderr, "%s page %d: %v\n", path, p, err)
				continue
			}
			if len(tables) == 0 {
				continue
			}
			res.Pages = append(res.Pages, tablesPageResult{Page: p, Tables: tables})
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
			for _, pr := range res.Pages {
				fmt.Fprintf(stdout, "Page %d:\n", pr.Page)
				for i, table := range pr.Tables {
					if i > 0 {
						fmt.Fprintln(stdout)
					}
					for _, row := range table.Rows {
						fmt.Fprintln(stdout, strings.Join(row, "\t"))
					}
				}
			}
		}
	}
	return exitOK
}
