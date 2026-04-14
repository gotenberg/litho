package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
)

type textPageResult struct {
	Page int    `json:"page"`
	Text string `json:"text"`
}

type textResult struct {
	File  string           `json:"file"`
	Pages []textPageResult `json:"pages"`
}

func runText(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("text", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("f", "text", "Output format: text or json")
	password := fs.String("p", "", "PDF password")
	pagesFlag := fs.String("pages", "", "Page range (e.g., 1-3,5,last)")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return exitError
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
			text, err := r.ExtractText(ctx, p)
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
