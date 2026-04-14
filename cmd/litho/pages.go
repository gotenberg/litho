package main

import (
	"context"
	"flag"
	"fmt"
	"io"
)

type pageInfo struct {
	Number      int     `json:"number"`
	Width       float64 `json:"width"`
	Height      float64 `json:"height"`
	Rotate      int     `json:"rotate"`
	UserUnit    float64 `json:"user_unit"`
	IsLandscape bool    `json:"is_landscape"`
}

type pagesResult struct {
	File  string     `json:"file"`
	Pages []pageInfo `json:"pages"`
}

func runPages(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pages", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("f", "text", "Output format: text or json")
	password := fs.String("p", "", "PDF password")

	if err := fs.Parse(args); err != nil {
		return exitError
	}

	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(stderr, "Usage: litho pages [-f json] [-p password] <file> [file...]")
		return exitError
	}

	var results []pagesResult
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

		res := pagesResult{File: path}
		for i := 1; i <= pageCount; i++ {
			p, err := r.Page(ctx, i)
			if err != nil {
				closer()
				fmt.Fprintf(stderr, "%s page %d: %v\n", path, i, err)
				return exitError
			}
			res.Pages = append(res.Pages, pageInfo{
				Number:      p.PageNumber,
				Width:       p.Width,
				Height:      p.Height,
				Rotate:      p.Rotate,
				UserUnit:    p.UserUnit,
				IsLandscape: p.IsLandscape,
			})
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
			fmt.Fprintf(stdout, "%-6s  %10s  %10s  %8s  %10s\n",
				"Page", "Width", "Height", "Rotate", "UserUnit")
			for _, p := range res.Pages {
				fmt.Fprintf(stdout, "%-6d  %10.2f  %10.2f  %8d  %10.2f\n",
					p.Number, p.Width, p.Height, p.Rotate, p.UserUnit)
			}
		}
	}
	return exitOK
}
