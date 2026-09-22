package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/models"
)

type annotationPageResult struct {
	Page        int                 `json:"page"`
	Annotations []models.Annotation `json:"annotations"`
}

type annotationsResult struct {
	File  string                 `json:"file"`
	Pages []annotationPageResult `json:"pages"`
}

func runAnnotations(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("annotations", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("f", "text", "Output format: text or json")
	password := fs.String("p", "", "PDF password")
	pagesFlag := fs.String("pages", "", "Page range (e.g., 1-3,5,last)")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(stderr, "Usage: litho annotations [-f json] [-p password] [--pages RANGE] <file> [file...]")
		return exitError
	}

	var results []annotationsResult
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

		res := annotationsResult{File: path}
		for _, p := range pageNums {
			annots, err := r.ExtractAnnotations(ctx, p)
			if err != nil {
				fmt.Fprintf(stderr, "%s page %d: %v\n", path, p, err)
				annots = nil
			}
			res.Pages = append(res.Pages, annotationPageResult{Page: p, Annotations: annots})
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
				if len(p.Annotations) == 0 {
					continue
				}
				fmt.Fprintf(stdout, "Page %d:\n", p.Page)
				for _, a := range p.Annotations {
					fmt.Fprintf(stdout, "  %s  rect=[%.1f, %.1f, %.1f, %.1f]", a.Subtype, a.Rect[0], a.Rect[1], a.Rect[2], a.Rect[3])
					if a.URI != "" {
						fmt.Fprintf(stdout, "  uri=%s", a.URI)
					}
					if a.Contents != "" {
						fmt.Fprintf(stdout, "  contents=%q", a.Contents)
					}
					fmt.Fprintln(stdout)
				}
			}
		}
	}
	return exitOK
}
